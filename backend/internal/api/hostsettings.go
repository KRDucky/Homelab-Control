// SPDX-FileCopyrightText: 2026 The Homelab Control Authors
//
// SPDX-License-Identifier: GPL-3.0-or-later

package api

import (
	"net/http"
	"strings"
	"time"

	"homelab-control/backend/internal/auth"
	"homelab-control/backend/internal/hostinfo"
)

// Host settings (M2-6 skeleton): read-only observation of hostname and
// timezone plus a preview of requested changes. Nothing here changes the
// host: there is no apply endpoint, no broker operation and no program
// execution (hc-api cannot spawn processes; guard tests enforce that).

// hostSettingsNotice is returned with every preview and shown by the UI.
const hostSettingsNotice = "Preview only. Nothing was changed on this host: Homelab Control cannot apply hostname or time zone changes yet."

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
	resp := hostSettingsPreviewResponse{
		PreviewOnly: true, Notice: hostSettingsNotice, Valid: true,
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
