// SPDX-FileCopyrightText: 2026 The Homelab Control Authors
//
// SPDX-License-Identifier: GPL-3.0-or-later

package ops

import (
	"context"
	"errors"
	"strings"
	"testing"

	"homelab-control/backend/internal/broker/proto"
	"homelab-control/backend/internal/broker/runner"
	"homelab-control/backend/internal/broker/types"
)

func TestSnapraidStatusArgvIsClosed(t *testing.T) {
	if len(snapraidStatusArgv) != 1 || snapraidStatusArgv[0] != "status" {
		t.Fatalf("argv %v; only \"status\" is allowed", snapraidStatusArgv)
	}
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

func TestSnapraidStatusUnavailableWithoutBinary(t *testing.T) {
	_, err := snapraidStatus(context.Background(), &Env{}, nil)
	var pe *proto.Error
	if !errors.As(err, &pe) || pe.Code != proto.CodeUnavailable || strings.Contains(pe.Message, "/") {
		t.Fatalf("%v", err)
	}
}

func TestSnapraidStatusParsesSummaryAndDropsGraph(t *testing.T) {
	const sample = "SnapRAID status report:\n" +
		"\n" +
		"   Files Fragmented Excess  Wasted  Used    Free  Use Name\n" +
		"            Files Fragments  GB      GB      GB\n" +
		"     100       0       0     0.0    10.0    90.0  10% d1\n" +
		" --------------------------------------------------------------------------\n" +
		"   0%|oooooooooooooooooooooooooooooooooooooooooooooooo\n" +
		"     o\n" +
		"  0 days ago of the last scrub/sync                 0\n" +
		"\n" +
		"The oldest block was scrubbed 10 days ago, the median 5, the newest 0.\n" +
		"\n" +
		"No sync is in progress.\n" +
		"No error detected.\n" +
		"WARNING! Content file /mnt/disk1/snapraid.content is not synced.\n" +
		"error: /mnt/secret/private.dat\n"
	env := &Env{SnapraidStatus: func(context.Context) ([]byte, string, int, error) {
		return []byte(sample), "", 0, nil
	}}
	got, err := snapraidStatus(context.Background(), env, nil)
	if err != nil {
		t.Fatal(err)
	}
	st := got.(types.SnapraidStatus)
	if st.SyncInProgress || st.ExitCode != 0 {
		t.Fatalf("flags: %+v", st)
	}
	if st.ScrubAge == "" || !strings.Contains(st.ScrubAge, "10 days ago") {
		t.Fatalf("scrub age: %+v", st)
	}
	joined := strings.Join(st.Summary, "\n")
	for _, want := range []string{"No sync is in progress.", "No error detected.", "oldest block was scrubbed", "WARNING!"} {
		if !strings.Contains(joined, want) {
			t.Fatalf("missing %q in %q", want, joined)
		}
	}
	for _, banned := range []string{"|", "/mnt/secret", "snapraid.content", "10% d1", "Fragmented"} {
		if strings.Contains(joined, banned) {
			t.Fatalf("summary leaked %q: %s", banned, joined)
		}
	}
	if strings.Contains(st.ScrubAge, "|") {
		t.Fatalf("scrub age leaked the graph: %s", st.ScrubAge)
	}
}

func TestSnapraidStatusSyncInProgress(t *testing.T) {
	env := &Env{SnapraidStatus: func(context.Context) ([]byte, string, int, error) {
		return []byte("A sync is in progress.\nNo error detected.\n"), "", 0, nil
	}}
	got, err := snapraidStatus(context.Background(), env, []byte(`{}`))
	if err != nil {
		t.Fatal(err)
	}
	st := got.(types.SnapraidStatus)
	if !st.SyncInProgress {
		t.Fatalf("%+v", st)
	}
	if strings.Contains(strings.Join(st.Summary, "\n"), "No sync is in progress") {
		t.Fatalf("false idle line: %+v", st)
	}
}

func TestSnapraidStatusKeepsExitCode(t *testing.T) {
	env := &Env{SnapraidStatus: func(context.Context) ([]byte, string, int, error) {
		return []byte("No sync is in progress.\nDANGER! Silent errors were found.\n"), "ignored /tmp/x", 2, nil
	}}
	got, err := snapraidStatus(context.Background(), env, nil)
	if err != nil {
		t.Fatal(err)
	}
	st := got.(types.SnapraidStatus)
	if st.ExitCode != 2 || st.SyncInProgress {
		t.Fatalf("%+v", st)
	}
	joined := strings.Join(st.Summary, "\n")
	if !strings.Contains(joined, "DANGER!") || strings.Contains(joined, "/tmp") {
		t.Fatalf("%s", joined)
	}
}

func TestSnapraidStatusStderrWithoutSlashWhenEmpty(t *testing.T) {
	env := &Env{SnapraidStatus: func(context.Context) ([]byte, string, int, error) {
		return []byte("   o\n"), "snapraid status failed", 1, nil
	}}
	got, err := snapraidStatus(context.Background(), env, nil)
	if err != nil {
		t.Fatal(err)
	}
	st := got.(types.SnapraidStatus)
	if len(st.Summary) != 1 || st.Summary[0] != "snapraid status failed" || st.ExitCode != 1 {
		t.Fatalf("%+v", st)
	}
	env.SnapraidStatus = func(context.Context) ([]byte, string, int, error) {
		return nil, "open /etc/snapraid.conf: permission denied", 1, nil
	}
	got, err = snapraidStatus(context.Background(), env, nil)
	if err != nil {
		t.Fatal(err)
	}
	st = got.(types.SnapraidStatus)
	if len(st.Summary) != 0 {
		t.Fatalf("stderr path must not be returned: %+v", st)
	}
}

func TestSnapraidStatusRunError(t *testing.T) {
	env := &Env{SnapraidStatus: func(context.Context) ([]byte, string, int, error) {
		return nil, "", 0, &runner.ExecError{Class: "EACCES"}
	}}
	_, err := snapraidStatus(context.Background(), env, nil)
	var pe *proto.Error
	if !errors.As(err, &pe) || pe.Code != proto.CodeInternal || !strings.Contains(pe.Message, "EACCES") || strings.Contains(pe.Message, "/") {
		t.Fatalf("%v", err)
	}
}

func TestParseSnapraidStatusCapsLines(t *testing.T) {
	var b strings.Builder
	for i := 0; i < 20; i++ {
		b.WriteString("WARNING! check " + strings.Repeat("x", 10) + "\n")
	}
	b.WriteString("No sync is in progress.\n")
	got := ParseSnapraidStatus([]byte(b.String()), 0)
	if len(got.Summary) != snapraidStatusMaxLines || !got.Truncated {
		t.Fatalf("len %d truncated %v", len(got.Summary), got.Truncated)
	}
	long := "WARNING! " + strings.Repeat("y", 400)
	got = ParseSnapraidStatus([]byte(long+"\n"), 0)
	if len(got.Summary) != 1 || len([]rune(got.Summary[0])) != snapraidStatusMaxRunes {
		t.Fatalf("clip %d", len([]rune(got.Summary[0])))
	}
}
