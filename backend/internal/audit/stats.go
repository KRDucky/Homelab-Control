// SPDX-FileCopyrightText: 2026 The Homelab Control Authors
//
// SPDX-License-Identifier: GPL-3.0-or-later

package audit

import (
	"bufio"
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"strings"
	"time"
)

// Summary is a size-and-shape summary of an audit log. It aggregates counts
// only and never returns user, remote, detail or data fields.
type Summary struct {
	Records  int       `json:"records"`  // chained records
	Legacy   int       `json:"legacy"`   // unchained records before the first chain segment
	Segments int       `json:"segments"` // audit.chain segments
	Oldest   time.Time `json:"oldest,omitempty"`
	Newest   time.Time `json:"newest,omitempty"`
	// ReadEvents maps an event family (the first dot-separated component of
	// an event name ending in ".read") to a UTC YYYY-MM-DD -> count map.
	ReadEvents map[string]map[string]int `json:"read_events,omitempty"`
}

// Summarize reads JSON Lines and summarises their shape. It does not verify
// the hash chain (use Verify for that) and never returns user, remote,
// detail or data fields. A malformed record or a read error fails the summary
// instead of returning a misleading partial result.
func Summarize(r io.Reader) (Summary, error) {
	var s Summary
	s.ReadEvents = map[string]map[string]int{}
	sc := bufio.NewScanner(r)
	sc.Buffer(make([]byte, 64<<10), 1<<20)
	line := 0
	for sc.Scan() {
		line++
		raw := bytes.TrimSpace(sc.Bytes())
		if len(raw) == 0 {
			continue
		}
		var e Event
		if err := json.Unmarshal(raw, &e); err != nil {
			return s, fmt.Errorf("line %d: not a JSON record", line)
		}
		if e.Seq == 0 {
			s.Legacy++
		} else {
			s.Records++
			if e.Event == "audit.chain" && e.Seq == 1 && e.Prev == ZeroHash {
				s.Segments++
			}
		}
		if e.Time.IsZero() {
			continue
		}
		if s.Oldest.IsZero() || e.Time.Before(s.Oldest) {
			s.Oldest = e.Time
		}
		if e.Time.After(s.Newest) {
			s.Newest = e.Time
		}
		if isReadEvent(e.Event) {
			day := e.Time.UTC().Format("2006-01-02")
			fam := eventFamily(e.Event)
			if s.ReadEvents[fam] == nil {
				s.ReadEvents[fam] = map[string]int{}
			}
			s.ReadEvents[fam][day]++
		}
	}
	if err := sc.Err(); err != nil {
		return s, fmt.Errorf("read error: %w", err)
	}
	return s, nil
}

// eventFamily is the first dot-separated component of an event name, or the
// whole name when there is no dot.
func eventFamily(event string) string {
	if i := strings.IndexByte(event, '.'); i >= 0 {
		return event[:i]
	}
	return event
}

// isReadEvent reports whether an event name denotes a read.
func isReadEvent(event string) bool {
	return strings.HasSuffix(event, ".read")
}
