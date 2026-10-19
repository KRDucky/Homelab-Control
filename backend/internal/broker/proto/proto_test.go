// SPDX-FileCopyrightText: 2026 The Homelab Control Authors
//
// SPDX-License-Identifier: GPL-3.0-or-later

package proto

import (
	"bytes"
	"encoding/json"
	"testing"
)

func TestOpNames(t *testing.T) {
	for _, ok := range []string{"broker.info", "services.systemd.list", "a.b"} {
		if !ValidOpName(ok) {
			t.Errorf("%q rejected", ok)
		}
	}
	for _, bad := range []string{"", "info", "Broker.info", "a..b", "a.b.c.d.e", "a.b/../c", "a.b;rm", "a.b "} {
		if ValidOpName(bad) {
			t.Errorf("%q accepted", bad)
		}
	}
}

func TestFrameRoundTripAndLimits(t *testing.T) {
	var buf bytes.Buffer
	if err := WriteFrame(&buf, Request{V: 1, Op: "a.b", Actor: Actor{User: "u"}}, MaxRequest); err != nil {
		t.Fatal(err)
	}
	var r Request
	if err := ReadFrame(&buf, &r, MaxRequest); err != nil || r.Op != "a.b" {
		t.Fatalf("%v %+v", err, r)
	}
	if err := WriteFrame(&buf, bytes.Repeat([]byte("x"), MaxRequest), MaxRequest); err == nil {
		t.Fatal("oversized frame written")
	}
	if err := ReadFrame(bytes.NewReader([]byte{0, 0, 0, 0}), &r, MaxRequest); err != ErrFrameTooLarge {
		t.Fatalf("zero-length frame: %v", err)
	}
}

func TestDecodeArgsStrict(t *testing.T) {
	var a struct{}
	for _, ok := range []string{"", "null", "{}", "  {} "} {
		if err := DecodeArgs(json.RawMessage(ok), &a); err != nil {
			t.Errorf("%q: %v", ok, err)
		}
	}
	for _, bad := range []string{`{"x":1}`, `[]`, `{}{}`, `"s"`} {
		if err := DecodeArgs(json.RawMessage(bad), &a); err == nil {
			t.Errorf("%q accepted", bad)
		}
	}
}
