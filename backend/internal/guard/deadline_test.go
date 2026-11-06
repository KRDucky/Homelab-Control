// SPDX-FileCopyrightText: 2026 The Homelab Control Authors
//
// SPDX-License-Identifier: GPL-3.0-or-later

package guard

import (
	"testing"

	"homelab-control/backend/internal/broker/ops"
	"homelab-control/backend/internal/brokerclient"
)

// TestClientDeadlinesExceedBrokerTimeouts: hc-api must wait longer than the
// broker's own op timeout, so that the broker's answer — including an
// "uncertain" outcome of a write — reaches the user instead of a client
// timeout while the operation completes (found for services.enablement.apply
// in the PR #36 review: broker 55 s, client 15 s).
func TestClientDeadlinesExceedBrokerTimeouts(t *testing.T) {
	for name, op := range ops.Registry() {
		if d := brokerclient.DefaultDeadline(name); d <= op.Timeout {
			t.Errorf("%s: client deadline %v does not exceed the broker timeout %v", name, d, op.Timeout)
		}
	}
}
