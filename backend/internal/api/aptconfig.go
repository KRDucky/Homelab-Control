// SPDX-FileCopyrightText: 2026 The Homelab Control Authors
//
// SPDX-License-Identifier: GPL-3.0-or-later

package api

import (
	"context"
	"errors"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"sync"
	"time"

	"homelab-control/backend/internal/aptapply"
	"homelab-control/backend/internal/aptmirror"
	"homelab-control/backend/internal/auth"
	"homelab-control/backend/internal/brokerclient"
	"homelab-control/backend/internal/hostinfo"
)

// APT settings and the mirror finder (owner authorization 2026-11-24).
// Reads go through packages.read; plan/apply and the network probe need
// packages.configure. The broker reviews and journals the writes; hc-api
// only measures mirrors.

// aptMirrorState remembers which mirror URIs the last probes offered, so a
// plan can only use a mirror that came from an official list or is already
// configured.
type aptMirrorState struct {
	mu      sync.Mutex
	offered map[string]time.Time
	// list and prober are replaced in tests.
	list   aptmirror.Lister
	prober func(ctx context.Context, cands []aptmirror.Candidate, codename string, current map[string]bool) []aptmirror.Result
}

const aptOfferTTL = 30 * time.Minute

func (s *Server) aptMirrors() *aptMirrorState {
	s.aptMirrorOnce.Do(func() {
		if s.aptMirror == nil {
			s.aptMirror = &aptMirrorState{}
		}
		if s.aptMirror.offered == nil {
			s.aptMirror.offered = map[string]time.Time{}
		}
		if s.aptMirror.list == nil {
			client := aptmirror.NewClient(nil)
			s.aptMirror.list = func(ctx context.Context, u string) ([]byte, error) {
				req, err := http.NewRequestWithContext(ctx, http.MethodGet, u, nil)
				if err != nil {
					return nil, err
				}
				resp, err := client.Do(req)
				if err != nil {
					return nil, err
				}
				defer resp.Body.Close()
				if resp.StatusCode != http.StatusOK {
					return nil, errors.New("mirror list: HTTP " + strconv.Itoa(resp.StatusCode))
				}
				return io.ReadAll(io.LimitReader(resp.Body, 256<<10))
			}
		}
		if s.aptMirror.prober == nil {
			s.aptMirror.prober = func(ctx context.Context, cands []aptmirror.Candidate, codename string, current map[string]bool) []aptmirror.Result {
				client := aptmirror.NewClient(func(host string) bool { return current[host] })
				return aptmirror.Prober{Client: client}.Probe(ctx, cands, codename)
			}
		}
	})
	return s.aptMirror
}

func (m *aptMirrorState) offer(uris []string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	now := time.Now()
	for u, t := range m.offered {
		if now.Sub(t) > aptOfferTTL {
			delete(m.offered, u)
		}
	}
	for _, u := range uris {
		m.offered[u] = now
	}
}

func (m *aptMirrorState) wasOffered(u string) bool {
	m.mu.Lock()
	defer m.mu.Unlock()
	t, ok := m.offered[u]
	return ok && time.Since(t) <= aptOfferTTL
}

func (s *Server) aptSupported(w http.ResponseWriter) bool {
	if s.packageFamily() != "debian" {
		writeError(w, http.StatusServiceUnavailable, "unsupported", "APT settings and mirrors are supported on Debian and Ubuntu only")
		return false
	}
	if s.broker == nil {
		brokerError(w, brokerclient.ErrUnavailable)
		return false
	}
	return true
}

func (s *Server) handleAptConfig(w http.ResponseWriter, r *http.Request, sess *auth.Session, _ string) {
	if !s.packagesProtectedGET(w, r, sess) || !s.aptSupported(w) {
		return
	}
	var out aptapply.AptConfigView
	if err := s.broker.CallWithArgs(r.Context(), "packages.aptconfig.read", struct{}{}, actorFor(sess), &out); err != nil {
		writeOpError(w, err)
		return
	}
	if err := s.auditLog("packages.aptconfig.read", sess.Username, r, "returned", "managed="+strconv.FormatBool(out.ManagedPresent)); err != nil {
		writeError(w, http.StatusServiceUnavailable, "audit_unavailable", "APT settings cannot be returned because their access could not be audited.")
		return
	}
	writeJSON(w, http.StatusOK, out)
}

type aptConfigBody struct {
	Reset    bool              `json:"reset,omitempty"`
	Tunables aptapply.Tunables `json:"tunables"`
	Token    string            `json:"token,omitempty"`
}

func aptConfigDetail(b aptConfigBody) string {
	if b.Reset {
		return "action=" + aptapply.ActionConfRemove
	}
	// Never the proxy URL.
	return "action=" + aptapply.ActionConfWrite + " proxy=" + strconv.FormatBool(b.Tunables.HTTPProxy != "")
}

func (s *Server) handleAptConfigPlan(w http.ResponseWriter, r *http.Request, sess *auth.Session, _ string) {
	var b aptConfigBody
	if !decodeStrict(w, r, &b) {
		return
	}
	if b.Token != "" {
		writeError(w, http.StatusBadRequest, "bad_request", "a plan request takes no token")
		return
	}
	if !s.aptSupported(w) {
		return
	}
	s.aptWrite(w, r, sess, "packages.aptconfig.plan", b, aptConfigDetail(b), &aptapply.AptPlan{})
}

func (s *Server) handleAptConfigApply(w http.ResponseWriter, r *http.Request, sess *auth.Session, _ string) {
	var b aptConfigBody
	if !decodeStrict(w, r, &b) {
		return
	}
	if len(b.Token) > 2048 || !planTokenRE.MatchString(b.Token) {
		writeError(w, http.StatusBadRequest, "bad_request", "the plan token is missing or malformed")
		return
	}
	if !s.aptSupported(w) {
		return
	}
	s.aptWrite(w, r, sess, "packages.aptconfig.apply", b, aptConfigDetail(b), &aptapply.AptOutcome{})
}

// aptWrite calls one plan/apply op and audits it (detail never carries file
// content, proxy URLs or tokens).
func (s *Server) aptWrite(w http.ResponseWriter, r *http.Request, sess *auth.Session, op string, args any, detail string, out any) {
	err := s.broker.CallWithArgs(r.Context(), op, args, actorFor(sess), out)
	if err != nil {
		_ = s.auditLog(op, sess.Username, r, errCode(err), detail)
		writeOpError(w, err)
		return
	}
	result := "success"
	if o, ok := out.(*aptapply.AptOutcome); ok {
		result = o.Outcome
	}
	if aerr := s.auditLog(op, sess.Username, r, result, detail); aerr != nil {
		writeError(w, http.StatusServiceUnavailable, "audit_unavailable", "The audit log cannot be written, so the result cannot be returned.")
		return
	}
	writeJSON(w, http.StatusOK, out)
}

type mirrorsResponse struct {
	aptapply.MirrorInventory
	DefaultCountry string `json:"default_country"`
}

func (s *Server) defaultCountry() string {
	if s.collector == nil {
		return ""
	}
	tz := hostinfo.ReadHostSettings(s.collector.Roots).Timezone.Value
	b, err := os.ReadFile(filepath.Join(hostinfo.ZoneinfoDir(s.collector.Roots.Etc), "zone.tab"))
	if err != nil || tz == "" {
		return ""
	}
	return aptmirror.CountryForZone(string(b), tz)
}

func (s *Server) handleAptMirrors(w http.ResponseWriter, r *http.Request, sess *auth.Session, _ string) {
	if !s.packagesProtectedGET(w, r, sess) || !s.aptSupported(w) {
		return
	}
	var inv aptapply.MirrorInventory
	if err := s.broker.CallWithArgs(r.Context(), "packages.mirrors.inventory", struct{}{}, actorFor(sess), &inv); err != nil {
		writeOpError(w, err)
		return
	}
	if err := s.auditLog("packages.mirrors.read", sess.Username, r, "returned", "entries="+strconv.Itoa(len(inv.Entries))); err != nil {
		writeError(w, http.StatusServiceUnavailable, "audit_unavailable", "Mirrors cannot be returned because their access could not be audited.")
		return
	}
	writeJSON(w, http.StatusOK, mirrorsResponse{MirrorInventory: inv, DefaultCountry: s.defaultCountry()})
}

type mirrorProbeBody struct {
	Country string `json:"country"`
}

type mirrorProbeResponse struct {
	Country  string             `json:"country"`
	Codename string             `json:"codename"`
	Results  []aptmirror.Result `json:"results"`
	Note     string             `json:"note,omitempty"`
}

func (s *Server) handleAptMirrorProbe(w http.ResponseWriter, r *http.Request, sess *auth.Session, _ string) {
	var b mirrorProbeBody
	if !decodeStrict(w, r, &b) {
		return
	}
	if !aptmirror.ValidCountry(b.Country) {
		writeError(w, http.StatusBadRequest, "bad_request", "country must be a two-letter code such as US or DE")
		return
	}
	if !s.aptSupported(w) {
		return
	}
	var inv aptapply.MirrorInventory
	if err := s.broker.CallWithArgs(r.Context(), "packages.mirrors.inventory", struct{}{}, actorFor(sess), &inv); err != nil {
		writeOpError(w, err)
		return
	}
	current := []string{}
	hosts := map[string]bool{}
	for _, e := range inv.Entries {
		if e.Switchable {
			current = append(current, e.URI)
			if h := mirrorURLHost(e.URI); h != "" {
				hosts[h] = true
			}
		}
	}
	m := s.aptMirrors()
	ctx, cancel := context.WithTimeout(r.Context(), 45*time.Second)
	defer cancel()
	resp := mirrorProbeResponse{Country: b.Country, Codename: inv.Distro.Codename, Results: []aptmirror.Result{}}
	cands, err := aptmirror.Candidates(ctx, inv.Distro, b.Country, current, m.list)
	if err != nil {
		resp.Note = err.Error() + "; only the configured mirror was measured."
	}
	if len(cands) > 0 {
		resp.Results = m.prober(ctx, cands, inv.Distro.Codename, hosts)
	}
	offered := []string{}
	for _, c := range cands {
		offered = append(offered, c.URI)
	}
	m.offer(offered)
	ok := 0
	for _, res := range resp.Results {
		if res.Status == aptmirror.StatusOK {
			ok++
		}
	}
	if aerr := s.auditLog("packages.mirrors.probe", sess.Username, r, "returned",
		"country="+b.Country+" candidates="+strconv.Itoa(len(cands))+" usable="+strconv.Itoa(ok)); aerr != nil {
		writeError(w, http.StatusServiceUnavailable, "audit_unavailable", "The probe result cannot be returned because it could not be audited.")
		return
	}
	writeJSON(w, http.StatusOK, resp)
}

func mirrorURLHost(u string) string {
	return aptmirror.Result{URI: u}.HostOf()
}

type mirrorChangeBody struct {
	File  string `json:"file"`
	From  string `json:"from"`
	To    string `json:"to"`
	Token string `json:"token,omitempty"`
}

func mirrorDetail(b mirrorChangeBody) string {
	return "file=" + filepath.Base(b.File) + " to=" + mirrorURLHost(b.To)
}

func (s *Server) handleAptMirrorPlan(w http.ResponseWriter, r *http.Request, sess *auth.Session, _ string) {
	var b mirrorChangeBody
	if !decodeStrict(w, r, &b) {
		return
	}
	if b.Token != "" || aptapply.ValidMirrorURI(b.To) != nil || !aptapply.IsVendorFile(b.File) {
		writeError(w, http.StatusBadRequest, "bad_request", "file must be a vendor APT source file and to a plain http(s) mirror URL")
		return
	}
	if !s.aptSupported(w) {
		return
	}
	if !s.aptMirrors().wasOffered(b.To) {
		writeError(w, http.StatusConflict, "conflict", "Choose a mirror from a recent \"Find fastest mirror\" result; other addresses are not accepted.")
		return
	}
	s.aptWrite(w, r, sess, "packages.mirror.plan", b, mirrorDetail(b), &aptapply.AptPlan{})
}

func (s *Server) handleAptMirrorApply(w http.ResponseWriter, r *http.Request, sess *auth.Session, _ string) {
	var b mirrorChangeBody
	if !decodeStrict(w, r, &b) {
		return
	}
	if len(b.Token) > 2048 || !planTokenRE.MatchString(b.Token) {
		writeError(w, http.StatusBadRequest, "bad_request", "the plan token is missing or malformed")
		return
	}
	if !s.aptSupported(w) {
		return
	}
	s.aptWrite(w, r, sess, "packages.mirror.apply", b, mirrorDetail(b), &aptapply.AptOutcome{})
}
