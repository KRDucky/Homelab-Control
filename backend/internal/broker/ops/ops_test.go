package ops

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"homelab-control/backend/internal/broker/proto"
	. "homelab-control/backend/internal/broker/types"
)

func write(t *testing.T, root, rel, content string, mode os.FileMode) {
	t.Helper()
	p := filepath.Join(root, rel)
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(p, []byte(content), mode); err != nil {
		t.Fatal(err)
	}
}

func TestRegistryIsClosedAndReadOnly(t *testing.T) {
	reg := Registry()
	want := map[string]Privilege{
		"broker.info":           PrivNone,
		"legacy.webmin.status":  PrivRootRead,
		"hardware.dmi.identity": PrivRootRead,
		"services.systemd.list": PrivUnprivilegedExec,
	}
	if len(reg) != len(want) {
		t.Fatalf("registry has %d ops, want %d — new ops need review and this test updated", len(reg), len(want))
	}
	for name, priv := range want {
		op, ok := reg[name]
		if !ok || op.Privilege != priv {
			t.Errorf("%s: got %+v", name, op.Privilege)
		}
		if op.Timeout <= 0 || op.Description == "" {
			t.Errorf("%s: missing timeout/description", name)
		}
		if name != "broker.info" && op.Scope == "" {
			t.Errorf("%s must require a scope", name)
		}
		for _, banned := range []string{"exec", "shell", "command", "file", "rpc", "eval", "write"} {
			if strings.Contains(strings.Split(name, ".")[len(strings.Split(name, "."))-1], banned) {
				t.Errorf("op name %s suggests a generic capability", name)
			}
		}
	}
}

func TestAllOpsRejectArguments(t *testing.T) {
	env := &Env{Roots: Roots{Root: t.TempDir()}}
	for name, op := range Registry() {
		_, err := op.Handle(context.Background(), env, json.RawMessage(`{"path":"/etc/shadow"}`))
		var pe *proto.Error
		if !errors.As(err, &pe) || pe.Code != proto.CodeBadRequest {
			t.Errorf("%s accepted unexpected arguments: %v", name, err)
		}
	}
}

func webminFixture(t *testing.T) string {
	root := t.TempDir()
	write(t, root, "etc/webmin/miniserv.conf", "port=10000\nssl=0\nkeyfile=/etc/webmin/miniserv.pem\npidfile=/var/webmin/miniserv.pid\nsession=1\n", 0o600)
	write(t, root, "etc/webmin/version", "2.105\n", 0o644)
	write(t, root, "etc/webmin/config", "referers_none=1\n", 0o600)
	write(t, root, "etc/webmin/miniserv.users", "root:$6$secret-hash:0\nalice:$y$another-hash:0\nbob:x:0\n../evil:x:0\n", 0o600)
	write(t, root, "etc/webmin/alice.acl", "rpc=1\n", 0o600)
	write(t, root, "etc/webmin/bob.acl", "rpc=0\n", 0o600)
	write(t, root, "etc/webmin/miniserv.pem", "-----BEGIN PRIVATE KEY-----\nSECRET\n", 0o600)
	write(t, root, "var/webmin/miniserv.pid", "4242\n", 0o644)
	write(t, root, "proc/4242/cmdline", "/usr/bin/perl\x00/usr/share/webmin/miniserv.pl\x00/etc/webmin/miniserv.conf\x00", 0o644)
	return root
}

func TestWebminStatus(t *testing.T) {
	root := webminFixture(t)
	res, err := webminStatus(context.Background(), &Env{Roots: Roots{Root: root}}, nil)
	if err != nil {
		t.Fatal(err)
	}
	st := res.(WebminStatus)
	if !st.Installed || st.Version != "2.105" || st.Port != 10000 || st.TLS == nil || *st.TLS || !st.AllIfaces {
		t.Fatalf("%+v", st)
	}
	if st.Running == nil || !*st.Running {
		t.Fatal("running miniserv not detected from pidfile + cmdline")
	}
	if st.Users != 3 {
		t.Errorf("user count %d (invalid names must be skipped)", st.Users)
	}
	if strings.Join(st.RPCCapable, ",") != "alice,root" {
		t.Errorf("rpc capable %v (root implicit, alice explicit, bob disabled)", st.RPCCapable)
	}
	levels := map[string]string{}
	for _, c := range st.Checks {
		levels[c.ID] = c.Level
	}
	want := map[string]string{"version": "warn", "tls": "warn", "bind": "warn", "referer": "ok", "rpc": "warn"}
	for k, v := range want {
		if levels[k] != v {
			t.Errorf("check %s: got %q want %q", k, levels[k], v)
		}
	}
	b, _ := json.Marshal(st)
	for _, secret := range []string{"secret-hash", "another-hash", "SECRET", "PRIVATE KEY", "miniserv.pem", "evil"} {
		if strings.Contains(string(b), secret) {
			t.Errorf("result leaks %q: %s", secret, b)
		}
	}
}

func TestWebminNotInstalledAndSymlinkRefused(t *testing.T) {
	root := t.TempDir()
	res, _ := webminStatus(context.Background(), &Env{Roots: Roots{Root: root}}, nil)
	if res.(WebminStatus).Installed {
		t.Fatal("phantom install")
	}
	// A symlinked miniserv.conf (e.g. pointing at /etc/shadow) must not be followed.
	write(t, root, "target", "port=1\n", 0o600)
	if err := os.MkdirAll(filepath.Join(root, "etc/webmin"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(filepath.Join(root, "target"), filepath.Join(root, "etc/webmin/miniserv.conf")); err != nil {
		t.Fatal(err)
	}
	res, _ = webminStatus(context.Background(), &Env{Roots: Roots{Root: root}}, nil)
	st := res.(WebminStatus)
	if st.Installed || st.Port != 0 || len(st.Notes) == 0 {
		t.Fatalf("symlink followed: %+v", st)
	}
}

func TestWebminPidfileConfined(t *testing.T) {
	root := webminFixture(t)
	write(t, root, "etc/webmin/miniserv.conf", "port=10000\nssl=1\nbind=127.0.0.1\npidfile=/etc/shadow\n", 0o600)
	res, _ := webminStatus(context.Background(), &Env{Roots: Roots{Root: root}}, nil)
	st := res.(WebminStatus)
	if st.Running != nil {
		t.Fatal("pidfile outside /var and /run must be ignored")
	}
	if st.AllIfaces || *st.TLS != true {
		t.Fatalf("%+v", st)
	}
}

func TestVersionLess(t *testing.T) {
	cases := []struct {
		a, b string
		less bool
	}{{"2.105", "2.670", true}, {"2.670", "2.670", false}, {"2.680", "2.670", false}, {"2.5", "2.105", false}, {"1.999", "2.0", true}}
	for _, c := range cases {
		if versionLess(c.a, c.b) != c.less {
			t.Errorf("%s < %s should be %v", c.a, c.b, c.less)
		}
	}
}

func TestDMIIdentity(t *testing.T) {
	root := t.TempDir()
	res, _ := dmiIdentity(context.Background(), &Env{Roots: Roots{Root: root}}, nil)
	if id := res.(DMIIdentity); id.Available || id.Reason == "" {
		t.Fatalf("missing DMI must be explained: %+v", id)
	}
	write(t, root, "sys/class/dmi/id/product_serial", "ABC123XYZ\n", 0o400)
	write(t, root, "sys/class/dmi/id/board_serial", "To Be Filled By O.E.M.\n", 0o400)
	write(t, root, "sys/class/dmi/id/product_uuid", "00000000-0000-0000-0000-000000000000\n", 0o400)
	res, _ = dmiIdentity(context.Background(), &Env{Roots: Roots{Root: root}}, nil)
	id := res.(DMIIdentity)
	if !id.Available || id.Fields["product_serial"].Status != "present" || id.Fields["product_serial"].Value != "ABC123XYZ" {
		t.Fatalf("%+v", id)
	}
	if id.Fields["board_serial"].Status != "placeholder" || id.Fields["product_uuid"].Status != "placeholder" {
		t.Fatalf("placeholders must not be presented as identity: %+v", id.Fields)
	}
	if id.Fields["chassis_serial"].Status != "missing" {
		t.Fatalf("%+v", id.Fields["chassis_serial"])
	}
}

func TestParseRealSystemctlOutput(t *testing.T) {
	for _, rel := range []string{"noble", "jammy"} {
		b, err := os.ReadFile("testdata/list-units-" + rel + ".json")
		if err != nil {
			t.Fatal(err)
		}
		units, err := ParseListUnits(b)
		if err != nil || len(units) < 20 {
			t.Fatalf("%s: %d units, %v", rel, len(units), err)
		}
		fb, _ := os.ReadFile("testdata/list-unit-files-" + rel + ".json")
		files, err := ParseListUnitFiles(fb)
		if err != nil || len(files) < 20 {
			t.Fatalf("%s unit files: %d, %v", rel, len(files), err)
		}
		if f, ok := files["systemd-journald.service"]; !ok || f.State != "static" {
			t.Errorf("%s: journald unit file not parsed: %+v", rel, f)
		}
		// systemd 255 calls the field "preset", 249 "vendor_preset"; both must parse.
		presets := 0
		for _, f := range files {
			if (rel == "noble" && f.Preset != nil) || (rel == "jammy" && f.VendorPreset != nil) {
				presets++
			}
		}
		if presets == 0 {
			t.Errorf("%s: no preset values parsed", rel)
		}
		var sawService, sawTimer bool
		for _, u := range units {
			sawService = sawService || u.Type == "service"
			sawTimer = sawTimer || u.Type == "timer"
		}
		if !sawService || !sawTimer {
			t.Errorf("%s: unit types not derived", rel)
		}
	}
}

func TestParseRejectsHostileUnits(t *testing.T) {
	in := `[{"unit":"ok.service","load":"loaded","active":"active","sub":"running","description":"fine"},
	{"unit":"<script>.service","load":"loaded","active":"active","sub":"running","description":"x"},
	{"unit":"bad.service","load":"loaded; rm -rf","active":"active","sub":"running","description":"x"},
	{"unit":"noext","load":"loaded","active":"active","sub":"running","description":"x"},
	{"unit":"ctl.service","load":"loaded","active":"active","sub":"running","description":"line\nbreak\u0007"}]`
	units, err := ParseListUnits([]byte(in))
	if err != nil {
		t.Fatal(err)
	}
	if len(units) != 2 || units[0].Name != "ctl.service" || units[0].Description != "linebreak" {
		t.Fatalf("%+v", units)
	}
}

func TestSystemdListWithoutSystemctl(t *testing.T) {
	_, err := systemdList(context.Background(), &Env{}, nil)
	var pe *proto.Error
	if !errors.As(err, &pe) || pe.Code != proto.CodeUnavailable {
		t.Fatalf("want unavailable, got %v", err)
	}
}

func TestBrokerInfoReportsSystemdBoot(t *testing.T) {
	root := t.TempDir()
	res, _ := brokerInfo(context.Background(), &Env{Roots: Roots{Root: root}}, nil)
	if res.(infoResult).SystemdBooted {
		t.Fatal("systemd reported booted without /run/systemd/system")
	}
	if err := os.MkdirAll(filepath.Join(root, "run/systemd/system"), 0o755); err != nil {
		t.Fatal(err)
	}
	res, _ = brokerInfo(context.Background(), &Env{Roots: Roots{Root: root}}, nil)
	if !res.(infoResult).SystemdBooted {
		t.Fatal("systemd boot not detected")
	}
}
