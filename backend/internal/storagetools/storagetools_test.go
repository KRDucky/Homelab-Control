// SPDX-FileCopyrightText: 2026 The Homelab Control Authors
//
// SPDX-License-Identifier: GPL-3.0-or-later

package storagetools

import (
	"strings"
	"testing"
)

func TestParseLsblkAcrossVersions(t *testing.T) {
	// util-linux 2.37 prints booleans as true/false and sizes as numbers with
	// --bytes; some builds print "0"/"1" strings and string sizes. Both decode.
	newer := `{"blockdevices":[{"name":"sda","kname":"sda","pkname":null,"path":"/dev/sda","type":"disk","maj:min":"8:0","size":1000,"ro":false,"rm":true,"rota":true,"tran":"usb","model":"M","serial":"S","wwn":null,"fstype":null,"uuid":null,"label":null,"partuuid":null,"partlabel":null}]}`
	older := `{"blockdevices":[{"name":"sda","kname":"sda","path":"/dev/sda","type":"disk","maj:min":"8:0","size":"1000","ro":"0","rm":"1","rota":"1","tran":"usb"}]}`
	for name, in := range map[string]string{"newer": newer, "older": older} {
		got, err := ParseLsblk([]byte(in))
		if err != nil {
			t.Fatalf("%s: %v", name, err)
		}
		d := got[0]
		if d.Size == nil || *d.Size != 1000 || !d.Removable || !d.Rotational || d.ReadOnly || d.Transport != "usb" || d.Type != "disk" {
			t.Fatalf("%s: %+v", name, d)
		}
	}
}

func TestParseLsblkRejects(t *testing.T) {
	for name, in := range map[string]string{
		"not json":    `nope`,
		"no list":     `{}`,
		"bad kname":   `{"blockdevices":[{"kname":"../sda"}]}`,
		"empty kname": `{"blockdevices":[{"name":"x"}]}`,
		"oversized":   `{"blockdevices":[` + strings.Repeat(" ", MaxLsblkBytes) + `]}`,
	} {
		if _, err := ParseLsblk([]byte(in)); err == nil {
			t.Errorf("%s accepted", name)
		}
	}
}

func TestSMARTCandidate(t *testing.T) {
	for kname, want := range map[string]bool{"sda": true, "nvme0n1": true, "vda": false, "md0": false, "loop0": false, "zram0": false, "sr0": false, "dm-0": false} {
		if got := SMARTCandidate(BlockDevice{KName: kname, Type: "disk"}); got != want {
			t.Errorf("%s: got %v want %v", kname, got, want)
		}
	}
	if SMARTCandidate(BlockDevice{KName: "sda1", Type: "part"}) {
		t.Error("partition is not a SMART candidate")
	}
}

func TestParseSMARTATA(t *testing.T) {
	in := `{"smartctl":{"exit_status":0},"device":{"protocol":"ATA"},"model_name":"WDC","serial_number":"WD-1","firmware_version":"01","smart_support":{"available":true,"enabled":true},"smart_status":{"passed":true},"temperature":{"current":31},"power_on_time":{"hours":99},"ata_smart_attributes":{"table":[{"id":5,"raw":{"value":0}},{"id":9,"raw":{"value":99}},{"id":197,"raw":{"value":0}},{"id":198,"raw":{"value":0}},{"id":199,"raw":{"value":7}}]}}`
	s, err := ParseSMART([]byte(in), 0)
	if err != nil {
		t.Fatal(err)
	}
	if s.Protocol != "ATA" || *s.TemperatureC != 31 || *s.PowerOnHours != 99 || *s.Reallocated != 0 || *s.CRCErrors != 7 || s.Health() != "ok" {
		t.Fatalf("%+v health=%s", s, s.Health())
	}
}

func TestParseSMARTNVMe(t *testing.T) {
	in := `{"smartctl":{"exit_status":0},"device":{"protocol":"NVMe"},"smart_status":{"passed":true},"nvme_smart_health_information_log":{"critical_warning":0,"percentage_used":5,"media_errors":3,"available_spare":100}}`
	s, err := ParseSMART([]byte(in), 0)
	if err != nil {
		t.Fatal(err)
	}
	if *s.PercentageUsed != 5 || *s.MediaErrors != 3 || s.Health() != "warning" {
		t.Fatalf("%+v health=%s", s, s.Health())
	}
}

func TestParseSMARTStandbyAndFailing(t *testing.T) {
	standby := `{"smartctl":{"exit_status":2,"messages":[{"string":"Device is in STANDBY mode, exit(2)","severity":"information"}]}}`
	s, err := ParseSMART([]byte(standby), 2)
	if err != nil || !s.Standby || s.Health() != "standby" {
		t.Fatalf("standby: %+v %v", s, err)
	}
	failing := `{"smartctl":{"exit_status":8},"smart_status":{"passed":false}}`
	s, err = ParseSMART([]byte(failing), 8)
	if err != nil || s.Health() != "failing" {
		t.Fatalf("failing: %+v %v", s, err)
	}
	unknown := `{"smartctl":{"exit_status":4}}`
	if s, _ := ParseSMART([]byte(unknown), 4); s.Health() != "unknown" {
		t.Fatalf("no data must be unknown, got %s", s.Health())
	}
}

func TestParseSMARTRejects(t *testing.T) {
	if _, err := ParseSMART([]byte(`{"smartctl":{}}`), 0); err == nil {
		t.Error("missing exit_status accepted")
	}
	if _, err := ParseSMART([]byte(`{"smartctl":{"exit_status":0}}`), 4); err == nil {
		t.Error("exit status mismatch accepted")
	}
	if _, err := ParseSMART([]byte(`garbage`), -1); err == nil {
		t.Error("non-JSON accepted")
	}
	many := `{"smartctl":{"exit_status":0,"messages":[` + strings.TrimSuffix(strings.Repeat(`{"string":"`+strings.Repeat("m", 500)+`"},`, 20), ",") + `]}}`
	s, err := ParseSMART([]byte(many), 0)
	if err != nil || len(s.Messages) != MaxMessages || len(s.Messages[0]) != MaxMessageText {
		t.Fatalf("messages not bounded: %d %v", len(s.Messages), err)
	}
}

func TestCheckJSONReport(t *testing.T) {
	if err := CheckJSONReport([]byte(`{"report":[{"pv":[]}]}`)); err != nil {
		t.Fatal(err)
	}
	for _, bad := range []string{`{"report":`, `[]`, `{"other":1}`, `{"report":[` + strings.Repeat(" ", MaxLVMBytes) + `]}`} {
		if CheckJSONReport([]byte(bad)) == nil {
			t.Errorf("accepted %.30q", bad)
		}
	}
}

func TestTemplatesAreFixed(t *testing.T) {
	if got := strings.Join(SmartctlArgs("sda"), " "); got != "--json=c --nocheck=standby --health --info --attributes /dev/sda" {
		t.Fatalf("smartctl template: %s", got)
	}
	for _, args := range [][]string{PVSArgs(), LVSArgs()} {
		if args[0] != "--readonly" {
			t.Fatalf("LVM template must be read-only: %v", args)
		}
	}
}
