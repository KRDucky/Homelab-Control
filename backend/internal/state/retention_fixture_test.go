// SPDX-FileCopyrightText: 2026 The Homelab Control Authors
//
// SPDX-License-Identifier: GPL-3.0-or-later

package state

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"testing"
	"time"

	"homelab-control/backend/internal/audit"
)

// TestWriteRetentionFixture writes a disposable state.db with real Go audit
// records for the cross-language retention check
// (tests/retention/go-cross-check.sh). It runs only when
// HC_RETENTION_FIXTURE names the output file. Rows 1..50 are re-dated 400
// days back and re-chained with Go's own record encoding.
func TestWriteRetentionFixture(t *testing.T) {
	out := os.Getenv("HC_RETENTION_FIXTURE")
	if out == "" {
		t.Skip("set HC_RETENTION_FIXTURE to write the fixture")
	}
	db, err := Open(out)
	if err != nil {
		t.Fatal(err)
	}
	l, err := audit.OpenStore(db.Audit())
	if err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 60; i++ {
		if err := l.Log(audit.Event{Event: "storage.inventory.read", User: "admin", Remote: "192.0.2.1", Outcome: "collected", Detail: fmt.Sprintf("nodes=%d <a>&b   ü", i)}); err != nil {
			t.Fatal(err)
		}
	}
	rows, err := db.sql.Query(`SELECT id, seq, time, event, user, remote, outcome, detail FROM audit_events ORDER BY id`)
	if err != nil {
		t.Fatal(err)
	}
	type row struct {
		id int64
		e  audit.Event
	}
	var all []row
	for rows.Next() {
		var r row
		var tm string
		var seq int64
		if err := rows.Scan(&r.id, &seq, &tm, &r.e.Event, &r.e.User, &r.e.Remote, &r.e.Outcome, &r.e.Detail); err != nil {
			t.Fatal(err)
		}
		r.e.Seq = uint64(seq)
		r.e.Time, _ = time.Parse(time.RFC3339Nano, tm)
		all = append(all, r)
	}
	rows.Close()
	prev := audit.ZeroHash
	for i := range all {
		e := &all[i].e
		if i < 50 {
			e.Time = e.Time.AddDate(0, 0, -400)
		}
		e.Prev, e.Hash = prev, ""
		b, _ := json.Marshal(*e)
		sum := sha256.Sum256(b)
		e.Hash = hex.EncodeToString(sum[:])
		prev = e.Hash
		if _, err := db.sql.Exec(`UPDATE audit_events SET time=?, prev=?, hash=? WHERE id=?`, e.Time.UTC().Format(time.RFC3339Nano), e.Prev, e.Hash, all[i].id); err != nil {
			t.Fatal(err)
		}
	}
	if err := db.Close(); err != nil {
		t.Fatal(err)
	}
}
