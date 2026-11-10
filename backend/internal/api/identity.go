// SPDX-FileCopyrightText: 2026 The Homelab Control Authors
// SPDX-License-Identifier: GPL-3.0-or-later
package api

import (
	"encoding/json"
	"errors"
	"io"
	"mime"
	"net/http"
	"slices"

	"homelab-control/backend/internal/auth"
	"homelab-control/backend/internal/containeridentity"
	"homelab-control/backend/internal/platform"
)

// WithContainerIdentity installs the administrator's identity configuration
// (platform defaults, ID maps, group tables, roles). Optional: without it,
// every platform previews with image defaults and an unknown ID mapping.
func (s *Server) WithContainerIdentity(c containeridentity.Config) *Server {
	s.identity = &c
	return s
}

type identityProfilesResponse struct {
	SchemaVersion        int                         `json:"schemaVersion"`
	Profiles             []containeridentity.Profile `json:"profiles"`
	PrivilegedGroupNames []string                    `json:"privilegedGroupNames"`
	Modes                []containeridentity.Mode    `json:"modes"`
}

func (s *Server) handleIdentityProfiles(w http.ResponseWriter, r *http.Request, sess *auth.Session, _ string) {
	if !s.platformsReady(w, r, sess) {
		return
	}
	if s.auditLog("container.identity.profiles.read", sess.Username, r, "returned", "profiles listed") != nil {
		writeError(w, 503, "audit_unavailable", "Platform access could not be audited.")
		return
	}
	writeJSON(w, 200, identityProfilesResponse{1, containeridentity.Profiles(), slices.Clone(containeridentity.PrivilegedGroupNames),
		[]containeridentity.Mode{containeridentity.ModeImage, containeridentity.ModeInherit, containeridentity.ModeCustom}})
}

// identityPreviewRequest is everything the client may choose. Host
// ownership, ID maps, group tables and observed identity are never accepted
// from the client: they come from administrator configuration and the
// platform adapter.
type identityPreviewRequest struct {
	Platform                    string                     `json:"platform"`
	Mode                        containeridentity.Mode     `json:"mode"`
	Runtime                     json.RawMessage            `json:"runtime"`
	Application                 containeridentity.Settings `json:"application"`
	Profile                     string                     `json:"profile"`
	Override                    containeridentity.Settings `json:"override"`
	AcknowledgePrivilegedGroups []uint32                   `json:"acknowledgePrivilegedGroups"`
	// Workload is an optional server-issued reference key; its platform-
	// reported identity is compared with the desired configuration.
	Workload string `json:"workload"`
}

type identityPreviewResponse struct {
	Platform platform.PublicView                `json:"platform"`
	Preview  containeridentity.Preview          `json:"preview"`
	Runtime  containeridentity.ContainerRuntime `json:"runtime"`
}

// handleIdentityPreview needs platforms.read and the selected platform's
// family scope (enforced by the registry's visibility). It is a pure
// computation: nothing is deployed, changed or inspected on any host.
func (s *Server) handleIdentityPreview(w http.ResponseWriter, r *http.Request, sess *auth.Session, _ string) {
	if !s.platformsReady(w, r, sess) {
		return
	}
	media, _, err := mime.ParseMediaType(r.Header.Get("Content-Type"))
	if err != nil || media != "application/json" {
		writeError(w, 415, "expected_json", "Expected application/json.")
		return
	}
	r.Body = http.MaxBytesReader(w, r.Body, 32<<10)
	dec := json.NewDecoder(r.Body)
	dec.DisallowUnknownFields()
	var req identityPreviewRequest
	if dec.Decode(&req) != nil {
		writeError(w, 400, "invalid_model", "Invalid identity preview request.")
		return
	}
	var extra any
	if dec.Decode(&extra) != io.EOF {
		writeError(w, 400, "invalid_model", "Expected one identity preview request.")
		return
	}
	if !platform.ValidID(req.Platform) || len(req.Profile) > 64 || len(req.Workload) > 512 || len(req.AcknowledgePrivilegedGroups) > containeridentity.MaxSupplementalGroups {
		writeError(w, 400, "invalid_model", "Invalid platform, profile, workload or acknowledgement.")
		return
	}
	rt, err := containeridentity.ParseContainerRuntime(req.Runtime)
	if err != nil {
		writeError(w, 400, "invalid_runtime", "The runtime.container declaration is invalid.")
		return
	}
	// ParseContainerRuntime already validates through ImageSupport, so this
	// cannot fail today; it is checked anyway so a future change to either
	// function can never silently preview with a zero-value declaration.
	img, err := rt.ImageSupport()
	if err != nil {
		writeError(w, 400, "invalid_runtime", "The runtime.container declaration is invalid.")
		return
	}
	// Unknown and unauthorized platforms are indistinguishable.
	view, err := s.platforms.View(req.Platform, sess.HasScope)
	if err != nil {
		writeError(w, 404, "not_found", "Unknown platform.")
		return
	}
	cfg := containeridentity.Config{}
	if s.identity != nil {
		cfg = *s.identity
	}
	cr := containeridentity.Request{Mode: req.Mode, Platform: cfg.Context(view), Image: img, Application: req.Application, Profile: req.Profile,
		Override: req.Override, AcknowledgePrivilegedGroups: req.AcknowledgePrivilegedGroups, Volumes: rt.RequiredVolumes()}
	if req.Workload != "" {
		inv, _, err := s.platforms.Inventory(r.Context(), req.Platform, sess.HasScope)
		if errors.Is(err, platform.ErrNotFound) {
			writeError(w, 404, "not_found", "Unknown platform.")
			return
		} else if err != nil {
			writeError(w, 503, "platform_unavailable", "Platform metadata is unavailable.")
			return
		}
		idx := slices.IndexFunc(inv.Workloads, func(x platform.Workload) bool { return x.Ref.Key() == req.Workload })
		if idx < 0 {
			writeError(w, 400, "invalid_model", "Unknown workload for this platform.")
			return
		}
		if o := inv.Workloads[idx].Identity; o != nil {
			cr.Observed = &containeridentity.Observed{UID: o.UID, GID: o.GID, SupplementalGroups: o.SupplementalGroups, Source: o.Source}
		}
	}
	p := containeridentity.Resolve(cr)
	if !slices.Contains(rt.SupportedPlatforms, string(view.Kind)) {
		p.Issues = append(p.Issues, containeridentity.Issue{Severity: containeridentity.SeverityBlocker, Code: "platform_not_supported_by_plugin", Field: "platform", Message: "The plugin does not declare support for this platform family."})
	}
	if rt.Privileged {
		p.Issues = append(p.Issues, containeridentity.Issue{Severity: containeridentity.SeverityBlocker, Code: "privileged_runtime", Field: "runtime", Message: "The plugin requests a privileged container; this requires a separate administrator review and is never configured automatically."})
	}
	p.Applied, p.Deployable = false, false
	if s.auditLog("platform."+string(view.Kind)+".identity.preview", sess.Username, r, "returned", "platform="+req.Platform+" mode="+string(req.Mode)) != nil {
		writeError(w, 503, "audit_unavailable", "Platform access could not be audited.")
		return
	}
	writeJSON(w, 200, identityPreviewResponse{view, p, rt})
}
