// SPDX-FileCopyrightText: 2026 The Homelab Control Authors
//
// SPDX-License-Identifier: GPL-3.0-or-later

package mergerfsapply

import (
	"os"
	"strings"
	"testing"
)

func TestApplyUnitIsOptIn(t *testing.T) {
	svc, err := os.ReadFile("../../../packaging/common/homelab-control-mergerfs-apply@.service")
	if err != nil {
		t.Fatal(err)
	}
	text := string(svc)
	for _, want := range []string{
		"ExecStart=/usr/lib/homelab-control/hc-mergerfs-apply",
		"ConditionPathExists=/etc/homelab-control/mergerfs-apply.enabled",
		"RefuseManualStart=yes",
		"PrivateDevices=no",
		"DeviceAllow=/dev/fuse rwm",
		"User=root",
		"CapabilityBoundingSet=CAP_SYS_ADMIN CAP_DAC_OVERRIDE CAP_DAC_READ_SEARCH CAP_FOWNER CAP_CHOWN",
		"SystemCallFilter=@system-service @mount",
		"KillMode=process",
		"PrivateMounts=no",
		"PrivateNetwork=no",
	} {
		if !strings.Contains(text, want) {
			t.Errorf("unit missing %s", want)
		}
	}
	if profiles := settingValues(text, "AppArmorProfile"); len(profiles) != 0 {
		t.Errorf("shared unit sets AppArmorProfile %q; that belongs in the Debian drop-in", profiles)
	}
	drop, err := os.ReadFile("../../../packaging/debian/homelab-control-mergerfs-apply@.service.d/apparmor.conf")
	if err != nil {
		t.Fatal(err)
	}
	profiles := settingValues(string(drop), "AppArmorProfile")
	if len(profiles) != 1 || profiles[0] != "hc-mergerfs-apply" {
		t.Errorf("AppArmorProfile assignments %q, want one hc-mergerfs-apply", profiles)
	}
	for _, banned := range []string{"[Install]", "SELinuxContext=", "AppArmorProfile=hc-storage-probe", "homelab-control-storage-helper", "~@mount", "ProtectSystem=", "ProtectHome=", "PrivateTmp=", "PrivateNetwork=yes", "ExecStart=/bin/false"} {
		if strings.Contains(text, banned) {
			t.Errorf("unit contains %s", banned)
		}
	}
	sock, err := os.ReadFile("../../../packaging/common/homelab-control-mergerfs-apply.socket")
	if err != nil {
		t.Fatal(err)
	}
	s := string(sock)
	if strings.Contains(s, "[Install]") || !strings.Contains(s, "SocketMode=0600") || !strings.Contains(s, "mergerfs-apply.enabled") {
		t.Fatalf("socket:\n%s", s)
	}
	profile, err := os.ReadFile("../../../packaging/debian/apparmor/usr.lib.homelab-control.hc-mergerfs-apply")
	if err != nil {
		t.Fatal(err)
	}
	p := withoutComments(string(profile))
	for _, want := range []string{
		"profile hc-mergerfs-apply /usr/lib/homelab-control/hc-mergerfs-apply flags=(enforce) {",
		"/usr/bin/mergerfs ix,",
		"/usr/bin/snapraid ix,",
		"/usr/bin/systemd-run ix,",
		"deny network inet,",
		"deny /etc/homelab-control/** rwklmx,",
		"/dev/disk/by-uuid/ r,",
		"/dev/disk/by-uuid/* r,",
		"/var/snapraid/** rwlk,",
		"/mnt/** rwlk,",
	} {
		if !hasLine(p, want) {
			t.Errorf("profile missing rule %s", want)
		}
	}
	for _, subject := range []string{"mount", "capability"} {
		if deniesSubject(p, subject) {
			t.Errorf("profile denies %s", subject)
		}
	}
	for _, banned := range []string{"/bin/sh", "/bin/bash", "unconfined", "complain"} {
		if hasToken(p, banned) {
			t.Errorf("profile contains %s", banned)
		}
	}
	marker, err := os.ReadFile("../../../packaging/common/mergerfs-apply.enabled")
	if err != nil {
		t.Fatalf("enable marker: %v", err)
	}
	// The unit gate is ConditionPathExists, so an empty file would still pass
	// that condition. The file we ship has to carry the removal note.
	if !strings.Contains(string(marker), "Remove this file to make ConditionPathExists fail.") {
		t.Fatal("enable marker: packaged file must say to remove it to fail the condition")
	}
}

func settingValues(text, key string) []string {
	var out []string
	for _, line := range strings.Split(text, "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") || strings.HasPrefix(line, ";") {
			continue
		}
		name, val, ok := strings.Cut(line, "=")
		if !ok || strings.TrimSpace(name) != key {
			continue
		}
		out = append(out, strings.TrimSpace(val))
	}
	return out
}

// deniesSubject reports an AppArmor deny of subject, including extra spaces
// and a path or flag after it. An allow of the same subject is not a deny.
func deniesSubject(text, subject string) bool {
	for _, line := range strings.Split(text, "\n") {
		fields := strings.Fields(strings.ReplaceAll(line, ",", " "))
		if len(fields) < 2 || fields[0] != "deny" {
			continue
		}
		for _, f := range fields[1:] {
			if f == subject {
				return true
			}
		}
	}
	return false
}

func TestDenySubjectCatchesVariants(t *testing.T) {
	for _, line := range []string{
		"deny mount,",
		"deny mount ,",
		"deny  mount,",
		"deny mount /tmp/** ,",
		"deny capability,",
		"deny capability sys_admin,",
		"deny capability ,",
	} {
		subject := "mount"
		if strings.Contains(line, "capability") {
			subject = "capability"
		}
		if !deniesSubject(line, subject) {
			t.Errorf("missed %q", line)
		}
	}
	for _, line := range []string{
		"mount,",
		"capability sys_admin,",
		"deny network inet,",
		"deny /bin/mount mr,",
		"# deny mount,",
	} {
		if deniesSubject(line, "mount") || deniesSubject(line, "capability") {
			t.Errorf("false positive %q", line)
		}
	}
}

func hasLine(text, want string) bool {
	for _, line := range strings.Split(text, "\n") {
		if strings.TrimSpace(line) == want {
			return true
		}
	}
	return false
}

func hasToken(text, token string) bool {
	for _, line := range strings.Split(text, "\n") {
		for _, field := range strings.FieldsFunc(line, func(r rune) bool {
			return r == ' ' || r == '\t' || r == ',' || r == '(' || r == ')'
		}) {
			if field == token {
				return true
			}
		}
	}
	return false
}

func withoutComments(text string) string {
	var b strings.Builder
	for _, line := range strings.Split(text, "\n") {
		if i := strings.IndexByte(line, '#'); i >= 0 {
			line = line[:i]
		}
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		b.WriteString(line)
		b.WriteByte('\n')
	}
	return b.String()
}
