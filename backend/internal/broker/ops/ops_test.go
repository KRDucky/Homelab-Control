// SPDX-FileCopyrightText: 2026 The Homelab Control Authors
//
// SPDX-License-Identifier: GPL-3.0-or-later

package ops

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"homelab-control/backend/internal/broker/runner"
	"os"
	"path/filepath"
	"slices"
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

// TestRegistryIsClosed is the W1 registry snapshot: the exact set of ops,
// their privilege class, scope and — for write ops — phase, risk classes,
// lock domain and root templates. Changing it requires security review.
func TestRegistryIsClosed(t *testing.T) {
	reg := Registry()
	type want struct {
		priv  Privilege
		scope string
		write string // "" | "plan" | "apply"
	}
	snapshot := map[string]want{
		"broker.info":             {PrivNone, "", ""},
		"hardware.dmi.identity":   {PrivRootRead, "hardware.read", ""},
		"services.systemd.list":   {PrivUnprivilegedExec, "services.read", ""},
		"services.unit.logs":      {PrivUnprivilegedExec, "services.logs", ""},
		"services.unit.plan":      {PrivUnprivilegedExec, "services.operate", "plan"},
		"services.unit.apply":     {PrivRootExec, "services.operate", "apply"},
		"operations.journal.list": {PrivRootRead, "audit.read", ""},
		// M2-3 (docs/m2/M2-3-DESIGN-REVIEW.md).
		"services.timers.list":      {PrivUnprivilegedExec, "services.read", ""},
		"services.sockets.list":     {PrivUnprivilegedExec, "services.read", ""},
		"services.unit.detail":      {PrivUnprivilegedExec, "services.read", ""},
		"services.unit.contents":    {PrivUnprivilegedExec, "services.files", ""},
		"services.enablement.plan":  {PrivRootRead, "services.configure", "plan"},
		"services.enablement.apply": {PrivRootExec, "services.configure", "apply"},
		"services.systemctl.plan":   {PrivNone, "services.systemctl", "plan"},
		"services.systemctl.apply":  {PrivRootExec, "services.systemctl", "apply"},
		"host.settings.plan":        {PrivUnprivilegedExec, "host.settings.apply", "plan"},
		"host.settings.apply":       {PrivRootExec, "host.settings.apply", "apply"},
		// M4-1b (ADR 0008 Amendment 5, Proposed): read-only package queries.
		"packages.installed":  {PrivUnprivilegedExec, "packages.read", ""},
		"packages.updates":    {PrivUnprivilegedExec, "packages.read", ""},
		"packages.sources":    {PrivUnprivilegedExec, "packages.read", ""},
		"packages.search":     {PrivUnprivilegedExec, "packages.read", ""},
		"packages.simulation": {PrivUnprivilegedExec, "packages.read", ""},
		// M4 (owner 2026-11-24): APT settings and mirror writes via hc-apt-apply.
		"packages.aptconfig.read":    {PrivUnprivilegedExec, "packages.read", ""},
		"packages.aptconfig.plan":    {PrivNone, "packages.configure", "plan"},
		"packages.aptconfig.apply":   {PrivUnprivilegedExec, "packages.configure", "apply"},
		"packages.mirrors.inventory": {PrivNone, "packages.read", ""},
		"packages.mirror.plan":       {PrivNone, "packages.configure", "plan"},
		"packages.mirror.apply":      {PrivNone, "packages.configure", "apply"},
		"mergerfs.snapraid.status":   {PrivUnprivilegedExec, "mergerfs.read", ""},
		"mergerfs.pool.plan":         {PrivNone, "mergerfs.configure", "plan"},
		"mergerfs.pool.apply":        {PrivRootRead, "mergerfs.configure", "apply"},
		"mergerfs.maintain.plan":     {PrivNone, "mergerfs.maintain", "plan"},
		"mergerfs.maintain.apply":    {PrivRootRead, "mergerfs.maintain", "apply"},
		// M2-4 O-1 (owner-approved implementation/isolated validation).
		"storage.probe": {PrivRootRead, "storage.probe", ""},
		// M11 (ADR 0008 Amendment 9, Proposed): read-only networking.
		"network.ports.list":      {PrivRootRead, "networking.ports.read", ""},
		"network.firewall.config": {PrivRootRead, "networking.firewall.read", ""},
		// ext4 format: destructive, not granted by ext4.read.
		"ext4.format.plan":        {PrivRootRead, "ext4.format", "plan"},
		"ext4.format.apply":       {PrivRootExec, "ext4.format", "apply"},
		"storage.partition.plan":  {PrivRootRead, "storage.partition", "plan"},
		"storage.partition.apply": {PrivRootExec, "storage.partition", "apply"},
	}
	if len(reg) != len(snapshot) {
		t.Fatalf("registry has %d ops, want %d — new ops need review and this test updated", len(reg), len(snapshot))
	}
	for name, w := range snapshot {
		op, ok := reg[name]
		if !ok || op.Privilege != w.priv || op.Scope != w.scope {
			t.Errorf("%s: got %+v / %q", name, op.Privilege, op.Scope)
			continue
		}
		if op.Timeout <= 0 || op.Description == "" {
			t.Errorf("%s: missing timeout/description", name)
		}
		phase := ""
		if op.Write != nil {
			phase = op.Write.Phase
			wantLock := map[string]string{"services.unit": "service:<unit id>", "services.enablement": "service:<unit id>+unit-files", "host.settings": "host-settings", "services.systemctl": "systemd-manager", "mergerfs.pool": "mergerfs-array", "mergerfs.maintain": "mergerfs-array", "ext4.format": "ext4-format:<identity>+ext4-format-dev:<rdev>", "packages.aptconfig": "apt-files", "packages.mirror": "apt-files", "storage.partition": "storage-partition:<identity>+ext4-format-dev:<rdev>"}[strings.TrimSuffix(strings.TrimSuffix(name, ".plan"), ".apply")]
			if op.Write.LockDomain != wantLock || len(op.Write.Risks) == 0 || op.AuditArgs == nil {
				t.Errorf("%s: incomplete write spec %+v", name, op.Write)
			}
		}
		if phase != w.write {
			t.Errorf("%s: write phase %q, want %q", name, phase, w.write)
		}
		for _, banned := range []string{"exec", "shell", "command", "file", "rpc", "eval", "write", "run"} {
			parts := strings.Split(name, ".")
			if strings.Contains(parts[len(parts)-1], banned) {
				t.Errorf("op name %s suggests a generic capability", name)
			}
		}
	}
	// Only services.unit.apply may hold root templates, and exactly these.
	for name, op := range reg {
		root := op.Privilege == PrivRootExec
		hasTpl := op.Write != nil && len(op.Write.Templates) > 0
		if root != hasTpl {
			t.Errorf("%s: root-exec privilege and templates disagree", name)
		}
	}
	got := reg["services.unit.apply"].Write.Templates
	if len(got) != 4 || got[0] != "systemctl.start" || got[1] != "systemctl.stop" || got[2] != "systemctl.restart" || got[3] != "systemctl.show" {
		t.Errorf("apply templates: %v", got)
	}
	// M2-3: enablement apply holds exactly the enable/disable templates and
	// the read-only queries; it can never start, stop or restart a unit, and
	// start/stop apply can never enable or disable one.
	got = reg["services.enablement.apply"].Write.Templates
	if fmt.Sprint(got) != "[systemctl.enable systemctl.disable systemctl.is-enabled systemctl.show-install systemctl.show systemctl.show-manager]" {
		t.Errorf("enablement apply templates: %v", got)
	}
	if fmt.Sprint(reg["services.enablement.plan"].Write.Risks) != "[disruptive]" || fmt.Sprint(reg["services.enablement.apply"].Write.Risks) != "[disruptive]" {
		t.Error("enablement must be classified disruptive (step-up)")
	}
	got = reg["services.systemctl.apply"].Write.Templates
	if fmt.Sprint(got) != "[systemctl.start-unit systemctl.stop-unit systemctl.restart-unit systemctl.reload systemctl.try-restart systemctl.reload-or-restart systemctl.kill systemctl.reset-failed systemctl.freeze systemctl.thaw systemctl.isolate systemctl.mask systemctl.unmask systemctl.reenable systemctl.preset systemctl.revert systemctl.set-default systemctl.enable-unit systemctl.disable-unit systemctl.daemon-reload systemctl.daemon-reexec systemctl.poweroff systemctl.reboot systemctl.halt systemctl.suspend systemctl.hibernate systemctl.hybrid-sleep systemctl.kexec systemctl.soft-reboot systemctl.reset-failed-all systemctl.add-wants systemctl.add-requires systemctl.edit]" {
		t.Errorf("systemctl apply templates: %v", got)
	}
	if fmt.Sprint(reg["services.systemctl.plan"].Write.Risks) != "[disruptive]" || fmt.Sprint(reg["services.systemctl.apply"].Write.Risks) != "[disruptive]" {
		t.Error("systemctl must be classified disruptive (step-up)")
	}
	for _, old := range []runner.RootTemplate{runner.RootSystemctlStart, runner.RootSystemctlStop, runner.RootSystemctlRestart, runner.RootSystemctlEnable, runner.RootSystemctlDisable} {
		for _, tpl := range got {
			if tpl == old {
				t.Errorf("systemctl apply must not reuse the .service-only template %s", old)
			}
		}
	}
	got = reg["host.settings.apply"].Write.Templates
	if fmt.Sprint(got) != "[hostnamectl.set-static hostnamectl.set-pretty timedatectl.set-timezone hostnamectl.status timedatectl.status timedatectl.list-timezones]" {
		t.Errorf("host.settings apply templates: %v", got)
	}
	if fmt.Sprint(reg["host.settings.plan"].Write.Risks) != "[reversible]" || fmt.Sprint(reg["host.settings.apply"].Write.Risks) != "[reversible]" {
		t.Error("host.settings must stay reversible (no silent step-up)")
	}
	if reg["host.settings.plan"].Scope != "host.settings.apply" || reg["host.settings.apply"].Scope != "host.settings.apply" {
		t.Error("host.settings plan/apply must require host.settings.apply, never host.settings.preview")
	}
	got = reg["ext4.format.apply"].Write.Templates
	if fmt.Sprint(got) != "[mkfs.ext4]" {
		t.Errorf("ext4 format apply templates: %v", got)
	}
	if fmt.Sprint(reg["ext4.format.plan"].Write.Risks) != "[destructive]" || fmt.Sprint(reg["ext4.format.apply"].Write.Risks) != "[destructive]" {
		t.Error("ext4 format must be classified destructive (step-up)")
	}
	if reg["ext4.format.plan"].Scope != "ext4.format" || reg["ext4.format.apply"].Scope != "ext4.format" {
		t.Error("ext4 format must require ext4.format, never ext4.read")
	}
	got = reg["storage.partition.apply"].Write.Templates
	if fmt.Sprint(got) != "[sfdisk.gpt]" {
		t.Errorf("partition apply templates: %v", got)
	}
	if reg["storage.partition.plan"].Scope != "storage.partition" || reg["storage.partition.apply"].Scope != "storage.partition" {
		t.Error("partition must require storage.partition, never storage.read")
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

func TestRunErrIncludesErrnoClass(t *testing.T) {
	err := runErr(&runner.ExecError{Class: "EACCES"})
	var pe *proto.Error
	if !errors.As(err, &pe) || pe.Code != proto.CodeInternal || pe.Message != "systemctl could not be run (EACCES)" {
		t.Fatalf("%v", err)
	}
	if strings.Contains(pe.Message, "/") || strings.Contains(pe.Message, "permission denied") {
		t.Fatalf("leaked: %s", pe.Message)
	}
	jerr := journalErr(&runner.ExecError{Class: "EACCES"})
	if !errors.As(jerr, &pe) || pe.Message != "journalctl could not be run (EACCES)" {
		t.Fatalf("%v", jerr)
	}
	plain := runErr(errors.New("some other failure"))
	if !errors.As(plain, &pe) || pe.Message != "systemctl could not be run" {
		t.Fatalf("%v", plain)
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

// The property list parsed here is exactly the one the runner queries.
func TestShowPropertiesMatchRunner(t *testing.T) {
	want := append(append(append([]string{}, scalarProps...), depProps...), actionProps...)
	got := runner.SystemctlShowProperties()
	slices.Sort(want)
	slices.Sort(got)
	if !slices.Equal(want, got) {
		t.Fatalf("parser %v\nrunner %v", want, got)
	}
}

func TestAdmissionClasses(t *testing.T) {
	want := map[string]AdmissionClass{
		"broker.info":                AdmissionInfo,
		"hardware.dmi.identity":      AdmissionSystemRead,
		"services.systemd.list":      AdmissionServicesRead,
		"services.unit.logs":         AdmissionServicesRead,
		"services.unit.plan":         AdmissionControlPlan,
		"services.unit.apply":        AdmissionControlApply,
		"services.timers.list":       AdmissionServicesRead,
		"services.sockets.list":      AdmissionServicesRead,
		"services.unit.detail":       AdmissionServicesRead,
		"services.unit.contents":     AdmissionServicesRead,
		"services.enablement.plan":   AdmissionControlPlan,
		"services.enablement.apply":  AdmissionControlApply,
		"services.systemctl.plan":    AdmissionControlPlan,
		"services.systemctl.apply":   AdmissionControlApply,
		"operations.journal.list":    AdmissionSystemRead,
		"host.settings.plan":         AdmissionControlPlan,
		"host.settings.apply":        AdmissionControlApply,
		"packages.installed":         AdmissionPackages,
		"packages.updates":           AdmissionPackages,
		"packages.sources":           AdmissionPackages,
		"packages.search":            AdmissionPackages,
		"packages.simulation":        AdmissionPackages,
		"mergerfs.snapraid.status":   AdmissionServicesRead,
		"mergerfs.pool.plan":         AdmissionControlPlan,
		"mergerfs.pool.apply":        AdmissionControlApply,
		"mergerfs.maintain.plan":     AdmissionControlPlan,
		"mergerfs.maintain.apply":    AdmissionControlApply,
		"storage.probe":              AdmissionProbe,
		"ext4.format.plan":           AdmissionControlPlan,
		"ext4.format.apply":          AdmissionControlApply,
		"storage.partition.plan":     AdmissionControlPlan,
		"storage.partition.apply":    AdmissionControlApply,
		"network.ports.list":         AdmissionSystemRead,
		"network.firewall.config":    AdmissionSystemRead,
		"packages.aptconfig.read":    AdmissionPackages,
		"packages.aptconfig.plan":    AdmissionControlPlan,
		"packages.aptconfig.apply":   AdmissionControlApply,
		"packages.mirrors.inventory": AdmissionPackages,
		"packages.mirror.plan":       AdmissionControlPlan,
		"packages.mirror.apply":      AdmissionControlApply,
	}
	reg := Registry()
	if len(reg) != len(want) {
		t.Fatalf("registry has %d ops, test pins %d; update the table when ops change", len(reg), len(want))
	}
	for name, cls := range want {
		op, ok := reg[name]
		if !ok {
			t.Errorf("op %q missing from registry", name)
			continue
		}
		if op.Admission != cls {
			t.Errorf("%s: admission = %q, want %q", name, op.Admission, cls)
		}
	}
	// Every write op is in a control (plan or apply) class: reads never carry
	// a WriteSpec and writes never land in a read class.
	for name, op := range reg {
		if op.Write == nil {
			continue
		}
		if op.Admission != AdmissionControlPlan && op.Admission != AdmissionControlApply {
			t.Errorf("%s: write op in non-control class %q", name, op.Admission)
		}
	}
}
