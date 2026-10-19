// SPDX-FileCopyrightText: 2026 The Homelab Control Authors
//
// SPDX-License-Identifier: GPL-3.0-or-later

package main

import "testing"

func TestValidateLegacyURL(t *testing.T) {
	for _, ok := range []string{"", "https://host:10000/", "http://127.0.0.1:10000"} {
		if err := validateLegacyURL(ok); err != nil {
			t.Errorf("%q rejected: %v", ok, err)
		}
	}
	for _, bad := range []string{"javascript:alert(1)", "data:text/html,x", "//host:10000", "https://user:pw@host/", "ftp://host/", "/relative"} {
		if validateLegacyURL(bad) == nil {
			t.Errorf("%q accepted", bad)
		}
	}
}

func TestIsLoopback(t *testing.T) {
	cases := map[string]bool{"127.0.0.1:9180": true, "[::1]:9180": true, "localhost:1": true, "0.0.0.0:9180": false, "192.168.1.5:9180": false, ":9180": false, "bad": false}
	for in, want := range cases {
		if got := isLoopback(in); got != want {
			t.Errorf("%s: got %v want %v", in, got, want)
		}
	}
}
