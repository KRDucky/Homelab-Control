package api

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"net/http"
	"slices"
	"sync"
	"time"

	"homelab-control/backend/internal/auth"
	"homelab-control/backend/internal/broker/proto"
	"homelab-control/backend/internal/broker/types"
	"homelab-control/backend/internal/brokerclient"
	"homelab-control/backend/internal/hostinfo"
	"homelab-control/backend/internal/hwinfo"
)

// Broker abstracts the broker client (tests use a fake).
type Broker interface {
	Call(ctx context.Context, op string, actor proto.Actor, out any) error
}

// WithBroker attaches a broker client. A nil broker means "not configured".
func (s *Server) WithBroker(b Broker) *Server { s.broker = b; return s }

// WithHardware attaches the hardware inventory collector.
func (s *Server) WithHardware(h *hwinfo.Collector) *Server { s.hw = h; return s }

func actorFor(sess *auth.Session) proto.Actor {
	b := make([]byte, 8)
	_, _ = rand.Read(b)
	return proto.Actor{User: sess.Username, Scopes: sess.Scopes, RequestID: hex.EncodeToString(b)}
}

// brokerState caches broker.info for a few seconds.
type brokerState struct {
	mu   sync.Mutex
	at   time.Time
	info *brokerInfo
	err  error
}

type brokerInfo struct {
	Version string         `json:"version"`
	Ops     []types.OpInfo `json:"ops"`
	Exec    []string       `json:"exec_available"`
	// SystemdBooted is false when systemd is not PID 1 (sd_booted()).
	SystemdBooted bool `json:"systemd_booted"`
}

func (s *Server) brokerInfo(ctx context.Context, sess *auth.Session) (*brokerInfo, error) {
	if s.broker == nil {
		return nil, brokerclient.ErrUnavailable
	}
	s.bstate.mu.Lock()
	defer s.bstate.mu.Unlock()
	if time.Since(s.bstate.at) < 5*time.Second {
		return s.bstate.info, s.bstate.err
	}
	var bi brokerInfo
	err := s.broker.Call(ctx, "broker.info", actorFor(sess), &bi)
	s.bstate.at = time.Now()
	if err != nil {
		s.bstate.info, s.bstate.err = nil, err
	} else {
		s.bstate.info, s.bstate.err = &bi, nil
	}
	return s.bstate.info, s.bstate.err
}

func (bi *brokerInfo) hasOp(name string) bool {
	if bi == nil {
		return false
	}
	return slices.ContainsFunc(bi.Ops, func(o types.OpInfo) bool { return o.Name == name })
}

// Capability statuses.
const (
	CapAvailable   = "available"
	CapDegraded    = "degraded"
	CapUnavailable = "unavailable"
	CapUnsupported = "unsupported"
	CapForbidden   = "forbidden"
)

// Capability is one navigable feature and whether it works right now.
type Capability struct {
	ID     string `json:"id"`
	Label  string `json:"label"`
	Path   string `json:"path"`
	Scope  string `json:"scope"`
	Status string `json:"status"`
	Reason string `json:"reason,omitempty"`
}

type capabilitiesResponse struct {
	Broker struct {
		Status  string `json:"status"` // available | unavailable
		Version string `json:"version,omitempty"`
		Reason  string `json:"reason,omitempty"`
	} `json:"broker"`
	Capabilities []Capability `json:"capabilities"`
}

const brokerDownReason = "The privileged helper (homelab-control-broker) is not running. Start it with: systemctl enable --now homelab-control-broker.socket"

func (s *Server) handleCapabilities(w http.ResponseWriter, r *http.Request, sess *auth.Session, _ string) {
	var resp capabilitiesResponse
	bi, berr := s.brokerInfo(r.Context(), sess)
	if berr == nil {
		resp.Broker.Status, resp.Broker.Version = CapAvailable, bi.Version
	} else {
		resp.Broker.Status, resp.Broker.Reason = CapUnavailable, brokerDownReason
	}
	gate := func(c Capability) Capability {
		if !sess.HasScope(c.Scope) {
			c.Status, c.Reason = CapForbidden, "Your account does not have the "+c.Scope+" permission."
		}
		return c
	}
	overview := Capability{ID: "overview", Label: "Overview", Path: "/", Scope: auth.ScopeSystemRead, Status: CapAvailable}
	services := Capability{ID: "services", Label: "Services", Path: "/services", Scope: auth.ScopeServicesRead}
	switch {
	case berr != nil:
		services.Status, services.Reason = CapUnavailable, brokerDownReason
	case !bi.hasOp("services.systemd.list"):
		services.Status, services.Reason = CapUnavailable, "The running broker does not provide the systemd listing."
	case !slices.Contains(bi.Exec, "systemctl") || !bi.SystemdBooted:
		services.Status, services.Reason = CapUnsupported, "This host is not running systemd, so there are no systemd services to list."
	default:
		services.Status = CapAvailable
	}
	hardware := Capability{ID: "hardware", Label: "Hardware", Path: "/hardware", Scope: auth.ScopeHardwareRead, Status: CapAvailable}
	if s.hw == nil {
		hardware.Status, hardware.Reason = CapUnavailable, "Hardware inventory is not enabled on this server."
	} else if berr != nil || !bi.hasOp("hardware.dmi.identity") {
		hardware.Status, hardware.Reason = CapDegraded, "Serial numbers and the system UUID need the privileged helper; everything else is shown."
	}
	legacy := Capability{ID: "legacy", Label: "Legacy Webmin", Path: "/legacy", Scope: auth.ScopeSystemRead, Status: CapAvailable}
	if berr != nil || !bi.hasOp("legacy.webmin.status") {
		legacy.Status, legacy.Reason = CapDegraded, "Without the privileged helper only Webmin's presence and version can be checked."
	}
	resp.Capabilities = []Capability{gate(overview), gate(services), gate(hardware), gate(legacy)}
	writeJSON(w, http.StatusOK, resp)
}

// brokerError maps broker failures to HTTP responses.
func brokerError(w http.ResponseWriter, err error) {
	var pe *proto.Error
	switch {
	case errors.Is(err, brokerclient.ErrUnavailable):
		writeError(w, http.StatusServiceUnavailable, "broker_unavailable", brokerDownReason)
	case errors.As(err, &pe):
		switch pe.Code {
		case proto.CodeForbidden:
			writeError(w, http.StatusForbidden, "forbidden", pe.Message)
		case proto.CodeUnavailable:
			writeError(w, http.StatusServiceUnavailable, "unsupported", pe.Message)
		case proto.CodeTimeout:
			writeError(w, http.StatusGatewayTimeout, "timeout", pe.Message)
		case proto.CodeBusy:
			writeError(w, http.StatusServiceUnavailable, "busy", "The privileged helper is busy; try again.")
		default:
			writeError(w, http.StatusBadGateway, "broker_error", "The privileged helper could not complete the request.")
		}
	default:
		writeError(w, http.StatusBadGateway, "broker_error", "Unexpected response from the privileged helper.")
	}
}

func (s *Server) handleServices(w http.ResponseWriter, r *http.Request, sess *auth.Session, _ string) {
	var res types.UnitList
	if s.broker == nil {
		brokerError(w, brokerclient.ErrUnavailable)
		return
	}
	if err := s.broker.Call(r.Context(), "services.systemd.list", actorFor(sess), &res); err != nil {
		brokerError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, res)
}

type hardwareResponse struct {
	Inventory hwinfo.Inventory `json:"inventory"`
	Identity  struct {
		Status string             `json:"status"` // ok | unavailable
		Reason string             `json:"reason,omitempty"`
		Data   *types.DMIIdentity `json:"data,omitempty"`
	} `json:"identity"`
}

func (s *Server) handleHardware(w http.ResponseWriter, r *http.Request, sess *auth.Session, _ string) {
	if s.hw == nil {
		writeError(w, http.StatusServiceUnavailable, "unsupported", "Hardware inventory is not enabled on this server.")
		return
	}
	var resp hardwareResponse
	resp.Inventory = s.hw.Collect()
	var id types.DMIIdentity
	if s.broker == nil {
		resp.Identity.Status, resp.Identity.Reason = "unavailable", brokerDownReason
	} else if err := s.broker.Call(r.Context(), "hardware.dmi.identity", actorFor(sess), &id); err != nil {
		resp.Identity.Status, resp.Identity.Reason = "unavailable", brokerDownReason
		var pe *proto.Error
		if errors.As(err, &pe) {
			resp.Identity.Reason = pe.Message
		}
	} else {
		resp.Identity.Status, resp.Identity.Data = "ok", &id
	}
	writeJSON(w, http.StatusOK, resp)
}

type legacyResponse struct {
	Source   string              `json:"source"` // broker | unprivileged
	Reason   string              `json:"reason,omitempty"`
	Status   *types.WebminStatus `json:"status,omitempty"`
	Fallback *hostinfo.Legacy    `json:"fallback,omitempty"`
}

func (s *Server) handleLegacy(w http.ResponseWriter, r *http.Request, sess *auth.Session, _ string) {
	var st types.WebminStatus
	var err error = brokerclient.ErrUnavailable
	if s.broker != nil {
		err = s.broker.Call(r.Context(), "legacy.webmin.status", actorFor(sess), &st)
	}
	if err == nil {
		writeJSON(w, http.StatusOK, legacyResponse{Source: "broker", Status: &st})
		return
	}
	// Fall back to what an unprivileged process can observe.
	l := s.collector.LegacyOnly()
	writeJSON(w, http.StatusOK, legacyResponse{Source: "unprivileged", Reason: brokerDownReason, Fallback: &l})
}
