// SPDX-FileCopyrightText: 2026 The Homelab Control Authors
//
// SPDX-License-Identifier: GPL-3.0-or-later

// Package proto defines the hc-api ↔ hc-broker wire protocol (ADR 0008):
// one request per Unix-socket connection, each message framed as a 4-byte
// big-endian length followed by JSON. It contains no privileged code and is
// imported by both sides.
package proto

import (
	"bytes"
	"encoding/binary"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"regexp"
)

// Version is the protocol version carried in every message.
const Version = 1

const (
	// MaxRequest bounds a request frame.
	MaxRequest = 16 << 10
	// MaxResponse bounds a response frame.
	MaxResponse = 4 << 20
	// DefaultSocket is the packaged socket path (systemd socket unit).
	DefaultSocket = "/run/homelab-control/broker.sock"
)

// Actor identifies the end user on whose behalf hc-api calls the broker.
//
// The broker cannot authenticate the end user itself: it authenticates hc-api
// by peer uid and trusts the actor hc-api asserts (ADR 0008, ADR 0011 "limits").
// Session and StepUpAt let plan tokens be bound to one browser session and let
// the broker require a recent step-up for disruptive operations.
type Actor struct {
	User      string   `json:"user"`
	Scopes    []string `json:"scopes"`
	RequestID string   `json:"request_id,omitempty"`
	// Session is the session key (SHA-256 hex of the session ID, which is not
	// itself secret); never the session cookie value.
	Session string `json:"session,omitempty"`
	// StepUpAt is the Unix time of the session's last successful password
	// re-entry (D-3); 0 when there is none.
	StepUpAt int64 `json:"step_up_at,omitempty"`
}

// Request is a single broker call.
type Request struct {
	V     int             `json:"v"`
	Op    string          `json:"op"`
	Args  json.RawMessage `json:"args,omitempty"`
	Actor Actor           `json:"actor"`
}

// Error is a structured broker failure.
type Error struct {
	Code    string `json:"code"`
	Message string `json:"message"`
}

func (e *Error) Error() string { return e.Code + ": " + e.Message }

// Error codes.
const (
	CodeBadRequest  = "bad_request"
	CodeUnknownOp   = "unknown_op"
	CodeNotFound    = "not_found"
	CodeForbidden   = "forbidden"
	CodeUnavailable = "unavailable"
	CodeTimeout     = "timeout"
	CodeBusy        = "busy"
	CodeInternal    = "internal"
	// Write-operation codes (ADR 0011).
	CodeConflict       = "conflict"         // target changed since the plan
	CodeInvalidToken   = "invalid_token"    // plan token unknown, expired, used or not bound to this request
	CodeStepUpRequired = "step_up_required" // disruptive op without a recent password re-entry
	CodePolicyDenied   = "policy_denied"    // protected unit or unsafe dependency (D-5)
)

// Response is the broker's reply.
type Response struct {
	V      int             `json:"v"`
	OK     bool            `json:"ok"`
	Result json.RawMessage `json:"result,omitempty"`
	Error  *Error          `json:"error,omitempty"`
}

var opNameRE = regexp.MustCompile(`^[a-z][a-z0-9]*(\.[a-z][a-z0-9]*){1,3}$`)

// ValidOpName reports whether s is a syntactically valid op name.
func ValidOpName(s string) bool { return opNameRE.MatchString(s) }

// WriteFrame writes one length-prefixed JSON message.
func WriteFrame(w io.Writer, v any, limit int) error {
	b, err := json.Marshal(v)
	if err != nil {
		return err
	}
	if len(b) > limit {
		return fmt.Errorf("frame of %d bytes exceeds limit %d", len(b), limit)
	}
	var hdr [4]byte
	binary.BigEndian.PutUint32(hdr[:], uint32(len(b)))
	if _, err := w.Write(hdr[:]); err != nil {
		return err
	}
	_, err = w.Write(b)
	return err
}

// ErrFrameTooLarge is returned when a peer announces an oversized frame.
var ErrFrameTooLarge = errors.New("frame too large")

// ReadFrame reads one frame into v using strict decoding (unknown fields are
// rejected, trailing data is rejected).
func ReadFrame(r io.Reader, v any, limit int) error {
	var hdr [4]byte
	if _, err := io.ReadFull(r, hdr[:]); err != nil {
		return err
	}
	n := binary.BigEndian.Uint32(hdr[:])
	if n == 0 || int64(n) > int64(limit) {
		return ErrFrameTooLarge
	}
	buf := make([]byte, n)
	if _, err := io.ReadFull(r, buf); err != nil {
		return err
	}
	dec := json.NewDecoder(bytes.NewReader(buf))
	dec.DisallowUnknownFields()
	if err := dec.Decode(v); err != nil {
		return err
	}
	if dec.More() {
		return errors.New("trailing data after JSON value")
	}
	return nil
}

// DecodeArgs strictly decodes op arguments into dst. Absent or empty args are
// treated as {}.
func DecodeArgs(raw json.RawMessage, dst any) error {
	if len(bytes.TrimSpace(raw)) == 0 || string(bytes.TrimSpace(raw)) == "null" {
		raw = json.RawMessage("{}")
	}
	dec := json.NewDecoder(bytes.NewReader(raw))
	dec.DisallowUnknownFields()
	if err := dec.Decode(dst); err != nil {
		return err
	}
	if dec.More() {
		return errors.New("trailing data after args")
	}
	return nil
}
