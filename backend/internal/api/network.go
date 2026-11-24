// SPDX-FileCopyrightText: 2026 The Homelab Control Authors
//
// SPDX-License-Identifier: GPL-3.0-or-later

package api

import (
	"context"
	"net/http"
	"strconv"
	"time"

	"homelab-control/backend/internal/auth"
	"homelab-control/backend/internal/netinfo"
)

// M11 read-only networking (ADR 0008 Amendment 9, Proposed). Ports and the
// firewall view come from broker root-read ops; bonds are read unprivileged
// from sysfs in hc-api. None of these handlers changes the host. Audit
// records carry counts and the backend ID only, never addresses or rules.

func (s *Server) networkProtectedGET(w http.ResponseWriter, r *http.Request, sess *auth.Session) bool {
	return s.auditedGET(w, r, sess, "Network inventory requires an access audit logger.")
}

// handlePorts serves the host port-use map from network.ports.list.
func (s *Server) handlePorts(w http.ResponseWriter, r *http.Request, sess *auth.Session, _ string) {
	if !s.networkProtectedGET(w, r, sess) {
		return
	}
	if s.broker == nil {
		writeError(w, http.StatusServiceUnavailable, "broker_unavailable", brokerDownReason)
		return
	}
	var out netinfo.PortMap
	if err := s.broker.Call(r.Context(), "network.ports.list", actorFor(sess), &out); err != nil {
		_ = s.auditLog("network.ports.read", sess.Username, r, "unavailable", "")
		brokerError(w, err)
		return
	}
	if out.Sockets == nil {
		out.Sockets = []netinfo.Socket{}
	}
	detail := "sockets=" + strconv.Itoa(len(out.Sockets)) + " hidden=" + strconv.Itoa(out.ProcessesHidden)
	if err := s.auditLog("network.ports.read", sess.Username, r, "returned", detail); err != nil {
		writeError(w, http.StatusServiceUnavailable, "audit_unavailable", "The port map cannot be returned because its access could not be audited.")
		return
	}
	writeJSON(w, http.StatusOK, out)
}

// handleBonds serves the kernel bonding inventory. It reads sysfs and the
// runtime state files of the network stacks; there is no broker op.
func (s *Server) handleBonds(w http.ResponseWriter, r *http.Request, sess *auth.Session, _ string) {
	if !s.networkProtectedGET(w, r, sess) {
		return
	}
	if s.bondsCollect == nil || s.bondsSem == nil {
		writeError(w, http.StatusServiceUnavailable, "bonds_unavailable", "Bond inventory is unavailable.")
		return
	}
	select {
	case s.bondsSem <- struct{}{}:
		defer func() { <-s.bondsSem }()
	default:
		writeError(w, http.StatusServiceUnavailable, "bonds_busy", "Bond inventory is already in progress; retry shortly.")
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 5*time.Second)
	defer cancel()
	snap, err := s.bondsCollect(ctx)
	if err != nil {
		_ = s.auditLog("network.bonds.read", sess.Username, r, "unavailable", "")
		writeError(w, http.StatusServiceUnavailable, "bonds_unavailable", "Bond inventory could not be collected safely.")
		return
	}
	if err := s.auditLog("network.bonds.read", sess.Username, r, "returned", "bonds="+strconv.Itoa(len(snap.Bonds))); err != nil {
		writeError(w, http.StatusServiceUnavailable, "audit_unavailable", "Bond inventory cannot be returned because its access could not be audited.")
		return
	}
	writeJSON(w, http.StatusOK, snap)
}

// handleFirewall serves the active firewall backend and its saved
// configuration from network.firewall.config.
func (s *Server) handleFirewall(w http.ResponseWriter, r *http.Request, sess *auth.Session, _ string) {
	if !s.networkProtectedGET(w, r, sess) {
		return
	}
	if s.broker == nil {
		writeError(w, http.StatusServiceUnavailable, "broker_unavailable", brokerDownReason)
		return
	}
	var out netinfo.FirewallSnapshot
	if err := s.broker.Call(r.Context(), "network.firewall.config", actorFor(sess), &out); err != nil {
		_ = s.auditLog("network.firewall.read", sess.Username, r, "unavailable", "")
		brokerError(w, err)
		return
	}
	if !validFirewallID(out.Active) {
		writeError(w, http.StatusBadGateway, "broker_error", "Unexpected response from the privileged helper.")
		return
	}
	if err := s.auditLog("network.firewall.read", sess.Username, r, "returned", "active="+out.Active); err != nil {
		writeError(w, http.StatusServiceUnavailable, "audit_unavailable", "The firewall view cannot be returned because its access could not be audited.")
		return
	}
	writeJSON(w, http.StatusOK, out)
}

func validFirewallID(id string) bool {
	switch id {
	case netinfo.FirewallFirewalld, netinfo.FirewallUfw, netinfo.FirewallNftables, netinfo.FirewallIptables, netinfo.FirewallNone, netinfo.FirewallUnknown:
		return true
	}
	return false
}
