// SPDX-FileCopyrightText: 2026 The Homelab Control Authors
//
// SPDX-License-Identifier: GPL-3.0-or-later

package api

import (
	"context"
	"errors"
	"net/http"
	"slices"
	"strings"
	"time"

	"homelab-control/backend/internal/auth"
	"homelab-control/backend/internal/broker/proto"
	"homelab-control/backend/internal/broker/types"
	"homelab-control/backend/internal/brokerclient"
	"homelab-control/backend/internal/hostinfo"
)

// Host settings (M2-6): unprivileged GET and preview in hc-api; plan/apply
// are broker ops (ADR 0011). hc-api never executes hostnamectl/timedatectl.

const hostSettingsNotice = "Preview only. Nothing was changed on this host."
const hostSettingsNoticeNoApply = "Preview only. Nothing was changed on this host: apply is not available from this server right now."

type hostSettingsResponse struct {
	CollectedAt time.Time             `json:"collected_at"`
	ReadOnly    bool                  `json:"read_only"`
	Settings    hostinfo.HostSettings `json:"settings"`
}

// hostSettingsPreviewRequest: a nil field is not requested. An empty
// pretty_hostname requests its removal.
type hostSettingsPreviewRequest struct {
	StaticHostname *string `json:"static_hostname"`
	PrettyHostname *string `json:"pretty_hostname"`
	Timezone       *string `json:"timezone"`
}

// Preview outcomes for one field.
const (
	PreviewUnchanged   = "unchanged"
	PreviewWouldChange = "would-change"
	PreviewInvalid     = "invalid"
	PreviewUnknown     = "cannot-compare"
)

type hostSettingChange struct {
	Field     string `json:"field"`
	Current   string `json:"current"`
	Requested string `json:"requested"`
	Outcome   string `json:"outcome"`
	Reason    string `json:"reason,omitempty"`
}

type hostSettingsPreviewResponse struct {
	// PreviewOnly, Executed and HostChanged are constants of this skeleton:
	// they let any client verify that nothing was applied.
	PreviewOnly    bool                  `json:"preview_only"`
	Executed       bool                  `json:"executed"`
	HostChanged    bool                  `json:"host_changed"`
	ApplyAvailable bool                  `json:"apply_available"`
	Notice         string                `json:"notice"`
	Valid          bool                  `json:"valid"`
	CollectedAt    time.Time             `json:"collected_at"`
	Current        hostinfo.HostSettings `json:"current"`
	Changes        []hostSettingChange   `json:"changes"`
	Warnings       []string              `json:"warnings"`
}

func (s *Server) handleHostSettings(w http.ResponseWriter, r *http.Request, _ *auth.Session, _ string) {
	if s.collector == nil {
		writeError(w, http.StatusServiceUnavailable, "host_settings_unavailable", "Host settings cannot be observed on this server.")
		return
	}
	writeJSON(w, http.StatusOK, hostSettingsResponse{CollectedAt: time.Now().UTC(), ReadOnly: true, Settings: s.collector.HostSettings()})
}

// handleHostSettingsPreview compares requested values with the observed ones
// and validates them. It reads the host only (current values and the tz
// database) and never writes it.
func (s *Server) handleHostSettingsPreview(w http.ResponseWriter, r *http.Request, sess *auth.Session, _ string) {
	if s.collector == nil {
		writeError(w, http.StatusServiceUnavailable, "host_settings_unavailable", "Host settings cannot be observed on this server.")
		return
	}
	var req hostSettingsPreviewRequest
	if !decodeStrict(w, r, &req) {
		return
	}
	if req.StaticHostname == nil && req.PrettyHostname == nil && req.Timezone == nil {
		writeError(w, http.StatusBadRequest, "bad_request", "Request at least one of static_hostname, pretty_hostname or timezone.")
		return
	}
	cur := s.collector.HostSettings()
	applyOK := s.hostSettingsApplyAvailable(r.Context(), sess)
	notice := hostSettingsNoticeNoApply
	if applyOK {
		notice = hostSettingsNotice
	}
	resp := hostSettingsPreviewResponse{
		PreviewOnly: true, Notice: notice, Valid: true, ApplyAvailable: applyOK,
		CollectedAt: time.Now().UTC(), Current: cur, Changes: []hostSettingChange{}, Warnings: []string{},
	}
	fields := []string{}
	add := func(field string, current hostinfo.Setting, requested, invalid string) {
		fields = append(fields, field)
		c := hostSettingChange{Field: field, Current: current.Value, Requested: requested}
		switch {
		case invalid != "":
			c.Outcome, c.Reason = PreviewInvalid, invalid
			resp.Valid = false
		case current.Status == hostinfo.SettingUnavailable:
			c.Outcome, c.Reason = PreviewUnknown, "The current value could not be read."
		case current.Value == requested:
			c.Outcome = PreviewUnchanged
		default:
			c.Outcome = PreviewWouldChange
		}
		resp.Changes = append(resp.Changes, c)
	}
	if req.StaticHostname != nil {
		v := *req.StaticHostname
		add("static_hostname", cur.StaticHostname, v, hostinfo.ValidStaticHostname(v))
		if v == "localhost" || strings.HasPrefix(v, "localhost.") {
			resp.Warnings = append(resp.Warnings, "A static host name of localhost makes this host hard to tell apart on the network.")
		}
	}
	if req.PrettyHostname != nil {
		add("pretty_hostname", cur.PrettyHostname, *req.PrettyHostname, hostinfo.ValidPrettyHostname(*req.PrettyHostname))
	}
	if req.Timezone != nil {
		v := *req.Timezone
		reason := hostinfo.ValidTimezoneName(v)
		if reason == "" {
			ok, err := hostinfo.TimezoneExists(s.collector.Roots.Etc, v)
			switch {
			case err != nil:
				reason = "The time zone database on this host could not be read, so the name cannot be checked."
			case !ok:
				reason = "This time zone is not in the time zone database on this host."
			}
		}
		add("timezone", cur.Timezone, v, reason)
	}
	outcome := "previewed"
	if !resp.Valid {
		outcome = "invalid"
	}
	// Field names only: requested values are not written to the audit log.
	s.auditLog("host.settings.preview", sess.Username, r, outcome, "fields="+strings.Join(fields, ","))
	writeJSON(w, http.StatusOK, resp)
}

func (s *Server) hostSettingsApplyAvailable(ctx context.Context, sess *auth.Session) bool {
	bi, err := s.brokerInfo(ctx, sess)
	if err != nil || bi == nil || !bi.WritesAvailable || !bi.hasOp("host.settings.apply") {
		return false
	}
	return slices.Contains(bi.Exec, "hostnamectl") && slices.Contains(bi.Exec, "timedatectl")
}

type hostSettingsApplyRequest struct {
	StaticHostname *string `json:"static_hostname"`
	PrettyHostname *string `json:"pretty_hostname"`
	Timezone       *string `json:"timezone"`
	Token          string  `json:"token"`
}

func hostSettingsWriteArgs(req hostSettingsPreviewRequest) (map[string]any, []string, bool) {
	args := map[string]any{}
	var fields []string
	if req.StaticHostname != nil {
		args["static_hostname"] = *req.StaticHostname
		fields = append(fields, "static_hostname")
	}
	if req.PrettyHostname != nil {
		args["pretty_hostname"] = *req.PrettyHostname
		fields = append(fields, "pretty_hostname")
	}
	if req.Timezone != nil {
		args["timezone"] = *req.Timezone
		fields = append(fields, "timezone")
	}
	return args, fields, len(fields) > 0
}

func (s *Server) handleHostSettingsPlan(w http.ResponseWriter, r *http.Request, sess *auth.Session, _ string) {
	var req hostSettingsPreviewRequest
	if !decodeStrict(w, r, &req) {
		return
	}
	args, fields, ok := hostSettingsWriteArgs(req)
	if !ok {
		writeError(w, http.StatusBadRequest, "bad_request", "Request at least one of static_hostname, pretty_hostname or timezone.")
		return
	}
	if s.broker == nil {
		brokerError(w, brokerclient.ErrUnavailable)
		return
	}
	var plan types.HostSettingsPlan
	err := s.broker.CallWithArgs(r.Context(), "host.settings.plan", args, actorFor(sess), &plan)
	if err != nil {
		s.auditLog("host.settings.plan", sess.Username, r, errCode(err), "fields="+strings.Join(fields, ","))
		writeOpError(w, err)
		return
	}
	if aerr := s.auditLog("host.settings.plan", sess.Username, r, "success", "fields="+strings.Join(fields, ",")+" risk="+plan.Risk); aerr != nil {
		writeError(w, http.StatusServiceUnavailable, "audit_unavailable", "The audit log cannot be written, so the operation cannot be reviewed.")
		return
	}
	writeJSON(w, http.StatusOK, plan)
}

func (s *Server) handleHostSettingsApply(w http.ResponseWriter, r *http.Request, sess *auth.Session, _ string) {
	var req hostSettingsApplyRequest
	if !decodeStrict(w, r, &req) {
		return
	}
	args, fields, ok := hostSettingsWriteArgs(hostSettingsPreviewRequest{StaticHostname: req.StaticHostname, PrettyHostname: req.PrettyHostname, Timezone: req.Timezone})
	if !ok {
		writeError(w, http.StatusBadRequest, "bad_request", "Request at least one of static_hostname, pretty_hostname or timezone.")
		return
	}
	if len(req.Token) > 2048 || !planTokenRE.MatchString(req.Token) {
		writeError(w, http.StatusBadRequest, "bad_request", "the plan token is missing or malformed")
		return
	}
	if s.broker == nil {
		brokerError(w, brokerclient.ErrUnavailable)
		return
	}
	args["token"] = req.Token
	actor := actorFor(sess)
	at, stepped := s.stepups.At(sess.Key, sess.Username)
	if stepped {
		actor.StepUpAt = at.Unix()
	}
	if aerr := s.auditLog("host.settings.apply", sess.Username, r, "requested", "fields="+strings.Join(fields, ",")+" req="+actor.RequestID); aerr != nil {
		writeError(w, http.StatusServiceUnavailable, "audit_unavailable", "The audit log cannot be written, so nothing was executed.")
		return
	}
	var out types.HostSettingsOutcome
	err := s.broker.CallWithArgs(context.WithoutCancel(r.Context()), "host.settings.apply", args, actor, &out)
	if err != nil {
		var pe *proto.Error
		if !errors.As(err, &pe) {
			s.auditLog("host.settings.apply", sess.Username, r, "outcome_unknown", "fields="+strings.Join(fields, ",")+" req="+actor.RequestID)
			writeError(w, http.StatusBadGateway, "outcome_unknown", "The privileged helper did not return a result, so the operation may or may not have run. Check Host settings and the operation journal on the Audit page.")
			return
		}
		s.auditLog("host.settings.apply", sess.Username, r, errCode(err), "fields="+strings.Join(fields, ",")+" req="+actor.RequestID)
		writeOpError(w, err)
		return
	}
	s.auditLog("host.settings.apply", sess.Username, r, out.Outcome, "fields="+strings.Join(fields, ",")+" op="+out.OperationID)
	writeJSON(w, http.StatusOK, out)
}
