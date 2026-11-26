// SPDX-FileCopyrightText: 2026 The Homelab Control Authors
//
// SPDX-License-Identifier: GPL-3.0-or-later

package ops

import (
	"context"
	"errors"
	"net"
	"os"
	"strings"
	"syscall"
	"testing"

	"homelab-control/backend/internal/broker/proto"
	"homelab-control/backend/internal/broker/types"
	"homelab-control/backend/internal/mergerfsapply"
)

func readSnapraidFixture(t *testing.T, name string) []byte {
	t.Helper()
	b, err := os.ReadFile("testdata/" + name)
	if err != nil {
		t.Fatal(err)
	}
	return b
}

func TestSnapraidStatusRejectsArguments(t *testing.T) {
	_, err := snapraidStatus(context.Background(), &Env{}, []byte(`{"conf":"/etc/snapraid.conf"}`))
	var pe *proto.Error
	if !errors.As(err, &pe) || pe.Code != proto.CodeBadRequest {
		t.Fatalf("conf arg: %v", err)
	}
	_, err = snapraidStatus(context.Background(), &Env{}, []byte(`{"argv":["sync"]}`))
	if !errors.As(err, &pe) || pe.Code != proto.CodeBadRequest {
		t.Fatalf("argv arg: %v", err)
	}
}

// Found on a VM: SnapRAID takes its array lock for status too, so status run
// as the broker's service user under ProtectSystem=strict failed with "Failed
// to create the lock file ... Read-only file system". The op now asks the
// root helper, with the closed status request only.
func TestSnapraidStatusAsksTheHelper(t *testing.T) {
	var got mergerfsapply.Request
	env := &Env{MergerFSApply: func(_ context.Context, req mergerfsapply.Request) (mergerfsapply.Result, error) {
		got = req
		return mergerfsapply.Result{Version: mergerfsapply.ProtocolVersion, OK: true, Summary: "status read",
			Output: string(readSnapraidFixture(t, "snapraid-13.0-status.txt"))}, nil
	}}
	out, err := snapraidStatus(context.Background(), env, nil)
	if err != nil {
		t.Fatal(err)
	}
	want := mergerfsapply.Request{Version: mergerfsapply.ProtocolVersion, Action: mergerfsapply.ActionStatus}
	if got.Version != want.Version || got.Action != want.Action || got.Mountpoint != "" || len(got.Branches) != 0 || len(got.Files) != 0 {
		t.Fatalf("request %+v", got)
	}
	if st := out.(types.SnapraidStatus); len(st.Disks) != 2 {
		t.Fatalf("%+v", st)
	}
	if spec, err := mergerfsapply.Validate(want); err != nil {
		t.Fatal(err)
	} else if argv, _ := spec.Argv(); strings.Join(argv, " ") != mergerfsapply.SnapRAIDBin+" status" {
		t.Fatalf("argv %v", argv)
	}
}

func TestSnapraidStatusUnavailableWithoutHelper(t *testing.T) {
	env := &Env{MergerFSApply: func(context.Context, mergerfsapply.Request) (mergerfsapply.Result, error) {
		return mergerfsapply.Result{}, &os.SyscallError{Syscall: "connect", Err: syscall.ENOENT}
	}}
	_, err := snapraidStatus(context.Background(), env, nil)
	var pe *proto.Error
	if !errors.As(err, &pe) || pe.Code != proto.CodeUnavailable || strings.Contains(pe.Message, "/") {
		t.Fatalf("%v", err)
	}
}

// Found on a VM: with the enable marker removed the socket still accepts, the
// unit's condition fails, and the connection closes with no reply. That is
// "helper not enabled" (503), not an internal error (502).
func TestSnapraidStatusHelperClosedWithoutReply(t *testing.T) {
	dir := t.TempDir()
	ln, err := net.Listen("unix", dir+"/s")
	if err != nil {
		t.Fatal(err)
	}
	defer ln.Close()
	go func() {
		c, err := ln.Accept()
		if err == nil {
			c.Close()
		}
	}()
	_, err = dialMergerFSAt(context.Background(), dir+"/s", mergerfsapply.Request{Version: 1, Action: mergerfsapply.ActionStatus})
	if !errors.Is(err, errHelperNoReply) {
		t.Fatalf("got %v", err)
	}
	var pe *proto.Error
	if !errors.As(snapraidRunErr(err), &pe) || pe.Code != proto.CodeUnavailable {
		t.Fatalf("%v", snapraidRunErr(err))
	}
}

// Found on an Ubuntu 22.04 VM: when that close comes before the broker has
// written the request, the write fails (EPIPE). Nothing was delivered, so it
// is the same "helper not enabled" case, not a 502.
func TestSnapraidStatusHelperClosedBeforeTheRequest(t *testing.T) {
	dir := t.TempDir()
	ln, err := net.Listen("unix", dir+"/s")
	if err != nil {
		t.Fatal(err)
	}
	defer ln.Close()
	go func() {
		c, err := ln.Accept()
		if err == nil {
			c.Close()
		}
	}()
	// Larger than the socket buffer, so the write is still going when the
	// helper side closes.
	files := make([]string, 40000)
	for i := range files {
		files[i] = "some/long/relative/path/to/a/file/that/is/not/sent"
	}
	_, err = dialMergerFSAt(context.Background(), dir+"/s", mergerfsapply.Request{Version: 1, Action: mergerfsapply.ActionFix, Files: files})
	if !errors.Is(err, errHelperNoReply) {
		t.Fatalf("got %v", err)
	}
}

func TestParseSnapraidStatusReal(t *testing.T) {
	st := ParseSnapraidStatus(readSnapraidFixture(t, "snapraid-13.0-status.txt"), 0)
	if len(st.Disks) != 2 || st.Disks[0].Name != "d1" || st.Disks[1].Name != "d2" {
		t.Fatalf("disks %+v", st.Disks)
	}
	d := st.Disks[0]
	if d.Files != 1 || d.UsedGB != 0 || d.FreeGB == nil || *d.FreeGB != 2 || d.UsePercent == nil || *d.UsePercent != 0 ||
		d.WastedGB == nil || *d.WastedGB != -24.7 {
		t.Fatalf("d1 %+v", d)
	}
	joined := strings.Join(st.Summary, "\n")
	for _, want := range []string{"The oldest block was scrubbed 0 days ago", "No sync is in progress.", "100% of the array is not scrubbed.", "No rehash is in progress or needed.", "No error detected."} {
		if !strings.Contains(joined, want) {
			t.Fatalf("missing %q in %q", want, joined)
		}
	}
	for _, banned := range []string{"|", "/", "Loading state", "days ago of the last"} {
		if strings.Contains(joined, banned) {
			t.Fatalf("summary leaked %q: %s", banned, joined)
		}
	}
	if st.SyncInProgress || st.Locked || st.Truncated || !strings.Contains(st.ScrubAge, "0 days ago") {
		t.Fatalf("%+v", st)
	}
	if strings.Contains(st.Note, "not read") {
		t.Fatalf("status reads the content file; the note must not deny it: %q", st.Note)
	}
}

// status.c prints "You have a sync in progress at N%." The old matcher looked
// for "sync is in progress" and never saw it.
func TestParseSnapraidStatusSyncInProgress(t *testing.T) {
	in := "WARNING! The array is NOT fully synced.\nYou have a sync in progress at 42%.\nNo error detected.\n"
	st := ParseSnapraidStatus([]byte(in), 0)
	if !st.SyncInProgress || !strings.Contains(strings.Join(st.Summary, "\n"), "sync in progress at 42%") {
		t.Fatalf("%+v", st)
	}
}

// Review finding on #181: a row whose used or free size carries a decimal was
// rejected by an integer-only pattern and vanished from the table.
func TestParseSnapraidStatusDecimalSizes(t *testing.T) {
	in := "   Files Fragmented Excess  Wasted  Used    Free  Use Name\n" +
		"     100       0       0     0.0    10.5    90.25  10% d1\n" +
		"      50       1       2       -     3       -     -  d2\n"
	st := ParseSnapraidStatus([]byte(in), 0)
	if len(st.Disks) != 2 {
		t.Fatalf("disks %+v", st.Disks)
	}
	d := st.Disks[0]
	if d.Name != "d1" || d.UsedGB != 10.5 || d.FreeGB == nil || *d.FreeGB != 90.25 || d.WastedGB == nil || *d.WastedGB != 0 {
		t.Fatalf("d1 %+v", d)
	}
	if e := st.Disks[1]; e.Name != "d2" || e.UsedGB != 3 || e.FreeGB != nil || e.WastedGB != nil || e.UsePercent != nil {
		t.Fatalf("d2 %+v", e)
	}
}

// Review finding on #181: when the helper cut snapraid's text at its output
// cap, the broker parsed the prefix and returned it as a complete report.
func TestSnapraidStatusMarksHelperCutOutput(t *testing.T) {
	full := string(readSnapraidFixture(t, "snapraid-13.0-status.txt"))
	prefix := full[:strings.Index(full, "d2")]
	env := &Env{MergerFSApply: func(context.Context, mergerfsapply.Request) (mergerfsapply.Result, error) {
		return mergerfsapply.Result{Version: mergerfsapply.ProtocolVersion, OK: false, ExitCode: -1,
			Summary: "status output exceeded the size limit", Output: prefix, OutputTruncated: true}, nil
	}}
	out, err := snapraidStatus(context.Background(), env, nil)
	if err != nil {
		t.Fatal(err)
	}
	if st := out.(types.SnapraidStatus); !st.Truncated {
		t.Fatalf("a cut report must say so: %+v", st)
	}
}

func TestParseSnapraidStatusLocked(t *testing.T) {
	st := ParseSnapraidStatus(readSnapraidFixture(t, "snapraid-13.0-status-locked.txt"), 1)
	if !st.Locked || st.ExitCode != 1 || len(st.Summary) != 1 || strings.Contains(strings.Join(st.Summary, "\n"), "/") {
		t.Fatalf("%+v", st)
	}
}

func TestParseSnapraidStatusDanger(t *testing.T) {
	in := "DANGER! In the array there are 3 errors!\nThey are at blocks: 10-12\nTo fix them use the command 'snapraid -e fix'.\n"
	st := ParseSnapraidStatus([]byte(in), 1)
	joined := strings.Join(st.Summary, "\n")
	if !strings.Contains(joined, "DANGER! In the array there are 3 errors!") || !strings.Contains(joined, "snapraid -e fix") {
		t.Fatalf("%s", joined)
	}
}

func TestParseSnapraidStatusCapsLines(t *testing.T) {
	var b strings.Builder
	for i := 0; i < 20; i++ {
		b.WriteString("WARNING! check " + strings.Repeat("x", 10) + "\n")
	}
	got := ParseSnapraidStatus([]byte(b.String()), 0)
	if len(got.Summary) != snapraidStatusMaxLines || !got.Truncated {
		t.Fatalf("len %d truncated %v", len(got.Summary), got.Truncated)
	}
	long := "WARNING! " + strings.Repeat("y", 400)
	got = ParseSnapraidStatus([]byte(long+"\n"), 0)
	if len(got.Summary) != 1 || len([]rune(got.Summary[0])) != snapraidStatusMaxRunes {
		t.Fatalf("clip %d", len([]rune(got.Summary[0])))
	}
	b.Reset()
	for i := 0; i < snapraidStatusMaxDisks+3; i++ {
		b.WriteString("       1       0       0     0.0       1       2  50% d" + strings.Repeat("x", i%3+1) + "\n")
	}
	got = ParseSnapraidStatus([]byte(b.String()), 0)
	if len(got.Disks) != snapraidStatusMaxDisks || !got.Truncated {
		t.Fatalf("disks %d truncated %v", len(got.Disks), got.Truncated)
	}
}
