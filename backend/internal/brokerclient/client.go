// SPDX-FileCopyrightText: 2026 The Homelab Control Authors
//
// SPDX-License-Identifier: GPL-3.0-or-later

// Package brokerclient is hc-api's client for the privileged broker. It only
// speaks the wire protocol; it contains no privileged code (guard-tested).
package brokerclient

import (
	"context"
	"encoding/json"
	"errors"
	"net"
	"time"

	"homelab-control/backend/internal/broker/proto"
)

// ErrUnavailable means the broker socket could not be reached.
var ErrUnavailable = errors.New("broker unavailable")

// Client calls the broker over a Unix socket.
type Client struct {
	Socket  string
	Timeout time.Duration
}

// New returns a client for the given socket path.
func New(socket string) *Client { return &Client{Socket: socket, Timeout: 15 * time.Second} }

// Call performs one argument-free operation and decodes the result into out
// (may be nil). Broker-side failures are returned as *proto.Error.
func (c *Client) Call(ctx context.Context, op string, actor proto.Actor, out any) error {
	return c.CallWithArgs(ctx, op, nil, actor, out)
}

// CallWithArgs performs one operation with typed arguments (encoded as JSON).
func (c *Client) CallWithArgs(ctx context.Context, op string, args any, actor proto.Actor, out any) error {
	if c == nil || c.Socket == "" {
		return ErrUnavailable
	}
	var raw json.RawMessage
	if args != nil {
		b, err := json.Marshal(args)
		if err != nil {
			return err
		}
		raw = b
	}
	ctx, cancel := context.WithTimeout(ctx, c.Timeout)
	defer cancel()
	var d net.Dialer
	conn, err := d.DialContext(ctx, "unix", c.Socket)
	if err != nil {
		return ErrUnavailable
	}
	defer conn.Close()
	if dl, ok := ctx.Deadline(); ok {
		_ = conn.SetDeadline(dl)
	}
	// The broker may answer (e.g. reject the peer) and close before reading
	// the request, so a failed write is not final: read the reply regardless.
	_ = proto.WriteFrame(conn, proto.Request{V: proto.Version, Op: op, Args: raw, Actor: actor}, proto.MaxRequest)
	var resp proto.Response
	if err := proto.ReadFrame(conn, &resp, proto.MaxResponse); err != nil {
		return ErrUnavailable
	}
	if !resp.OK {
		if resp.Error == nil {
			return &proto.Error{Code: proto.CodeInternal, Message: "broker returned no result"}
		}
		return resp.Error
	}
	if out == nil {
		return nil
	}
	return json.Unmarshal(resp.Result, out)
}
