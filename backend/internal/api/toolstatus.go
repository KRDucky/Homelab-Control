// SPDX-FileCopyrightText: 2026 The Homelab Control Authors
//
// SPDX-License-Identifier: GPL-3.0-or-later

package api

import (
	"context"
	"errors"

	"homelab-control/backend/internal/auth"
	"homelab-control/backend/internal/broker/proto"
)

// toolStatus is a report the read-only storage helper produced with the
// standard tools (zpool/zfs, btrfs-progs), relayed by a broker op. A page
// always gets its unprivileged inventory; this part says why it is missing.
type toolStatus[T any] struct {
	Available bool   `json:"available"`
	Reason    string `json:"reason,omitempty"`
	Report    *T     `json:"report,omitempty"`
}

// toolReport calls a no-argument broker op. It is skipped when the kernel
// lacks the filesystem, and every failure becomes a reason instead of
// failing the page. what names the report in messages ("Pool status").
// It sets no deadline of its own: the broker client's per-op deadline,
// derived from the helper's budget, governs, and the request context still
// cancels when the browser goes away.
func toolReport[T any](ctx context.Context, s *Server, sess *auth.Session, op string, kernelPresent bool, noKernel, what string) toolStatus[T] {
	if !kernelPresent {
		return toolStatus[T]{Reason: noKernel}
	}
	if s.broker == nil {
		return toolStatus[T]{Reason: brokerDownReason}
	}
	var rep T
	if err := s.broker.Call(ctx, op, actorFor(sess), &rep); err != nil {
		reason := what + " is unavailable: the read-only storage helper (homelab-control-storage-probe.socket) did not answer."
		var pe *proto.Error
		if errors.As(err, &pe) && pe.Message != "" {
			reason = what + " is unavailable: " + pe.Message
		}
		return toolStatus[T]{Reason: reason}
	}
	return toolStatus[T]{Available: true, Report: &rep}
}
