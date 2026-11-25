// SPDX-FileCopyrightText: 2026 The Homelab Control Authors
//
// SPDX-License-Identifier: GPL-3.0-or-later

// Package storagetools holds the closed protocol of the read-only storage
// helper (hc-storage-probe) and typed parsers for the standard tools it runs:
// lsblk (util-linux), smartctl (smartmontools) and pvs/lvs (LVM2). It executes
// nothing, so hc-api, the broker and the helper can all import it.
//
// Homelab Control does not re-implement these tools (ADR 0016 follow-up,
// owner 2026-11-25): the helper runs them with fixed arguments and JSON output,
// and this package only decodes the fields the UI shows.
package storagetools

import (
	"bytes"
	"encoding/json"
	"fmt"
	"regexp"
	"strconv"
	"strings"
)

// ProtocolVersion is the helper protocol. Version 1 (native LVM reader and
// blkid on caller-named devices) is gone; the helper now enumerates devices
// itself and takes no device names from its caller.
const ProtocolVersion = 2

// Bounds on what the helper returns.
const (
	MaxDisks       = 64
	MaxLsblkBytes  = 256 << 10
	MaxSmartBytes  = 128 << 10 // per disk, smartctl JSON
	MaxLVMBytes    = 512 << 10 // per report (pvs, lvs)
	MaxMessages    = 8
	MaxMessageText = 200
)

// CodeDevicePolicyUnresolved is the typed refusal when a required block-device
// class is missing from /proc/devices (the unit's DeviceAllow= would be empty).
const CodeDevicePolicyUnresolved = "device_policy_unresolved"

// Request is the closed helper input: only the protocol version.
type Request struct {
	Version int `json:"version"`
}

// Result is the closed helper output.
type Result struct {
	Version int              `json:"version"`
	Code    string           `json:"code,omitempty"`
	Blocks  []BlockDevice    `json:"blocks"`
	SMART   map[string]SMART `json:"smart"`
	// PVS and LVS are the raw `pvs`/`lvs --reportformat json` reports, bounded
	// and syntax-checked here, decoded by storageinfo.ParsePVS/ParseLVM.
	PVS     json.RawMessage `json:"pvs,omitempty"`
	LVS     json.RawMessage `json:"lvs,omitempty"`
	Tools   map[string]bool `json:"tools"`
	Errors  []string        `json:"errors,omitempty"`
	Partial bool            `json:"partial"`
}

// LsblkColumns is the fixed lsblk column list. Every column exists in
// util-linux 2.37 (Ubuntu 22.04) and later.
const LsblkColumns = "NAME,KNAME,PKNAME,PATH,TYPE,MAJ:MIN,SIZE,RO,RM,ROTA,TRAN,MODEL,SERIAL,WWN,FSTYPE,UUID,LABEL,PARTUUID,PARTLABEL"

// LsblkArgs is the fixed lsblk template: a flat JSON list in bytes.
func LsblkArgs() []string {
	return []string{"--json", "--list", "--bytes", "--output", LsblkColumns}
}

// SmartctlArgs is the fixed smartctl template for one whole disk: JSON,
// health, identity and attributes, and never wake a disk in standby.
func SmartctlArgs(kname string) []string {
	return []string{"--json=c", "--nocheck=standby", "--health", "--info", "--attributes", "/dev/" + kname}
}

// The pvs/lvs field lists are exactly what storageinfo.ParsePVS/ParseLVM read.
const (
	PVSFields = "pv_uuid,pv_name,vg_uuid,vg_name,pv_size,dev_size,pv_major,pv_minor,pv_attr,pv_missing,pvseg_start,pvseg_size,lv_uuid"
	LVSFields = "lv_uuid,lv_name,vg_uuid,vg_name,lv_parent,lv_size,lv_attr,lv_health_status,raid_sync_action,sync_percent," +
		"cache_total_blocks,cache_used_blocks,cache_dirty_blocks,origin_uuid,pool_lv_uuid,data_lv_uuid,metadata_lv_uuid," +
		"segtype,seg_start,seg_size,cache_mode,cache_policy,devices,metadata_devices"
)

// PVSArgs and LVSArgs are the fixed LVM templates. --readonly reads on-disk
// metadata without device-mapper, so the reports are metadata-only;
// --nolocking keeps LVM from creating lock files on the helper's read-only
// filesystem ("File locking initialisation failed" otherwise, VM finding).
func PVSArgs() []string {
	return []string{"--readonly", "--nolocking", "--reportformat", "json", "--units", "b", "--nosuffix", "--segments", "-o", PVSFields}
}

func LVSArgs() []string {
	return []string{"--readonly", "--nolocking", "--reportformat", "json", "--units", "b", "--nosuffix", "--segments", "-o", LVSFields}
}

// BlockDevice is one lsblk row.
type BlockDevice struct {
	Name       string  `json:"name"`
	KName      string  `json:"kname"`
	PKName     string  `json:"pkname,omitempty"`
	Path       string  `json:"path"`
	Type       string  `json:"type"`
	MajMin     string  `json:"maj_min"`
	Size       *uint64 `json:"size,omitempty"`
	ReadOnly   bool    `json:"ro"`
	Removable  bool    `json:"rm"`
	Rotational bool    `json:"rota"`
	Transport  string  `json:"tran,omitempty"`
	Model      string  `json:"model,omitempty"`
	Serial     string  `json:"serial,omitempty"`
	WWN        string  `json:"wwn,omitempty"`
	FSType     string  `json:"fstype,omitempty"`
	UUID       string  `json:"uuid,omitempty"`
	Label      string  `json:"label,omitempty"`
	PartUUID   string  `json:"partuuid,omitempty"`
	PartLabel  string  `json:"partlabel,omitempty"`
}

// KName is the kernel block-device name grammar the helper accepts from lsblk
// before it names /dev/<kname> on a smartctl command line.
var KName = regexp.MustCompile(`^[a-zA-Z0-9][a-zA-Z0-9_.!:-]{0,63}$`)

// flex decodes lsblk values that util-linux prints as JSON numbers, booleans,
// strings or null depending on version and column.
type flex struct {
	s     string
	set   bool
	isNum bool
}

func (f *flex) UnmarshalJSON(b []byte) error {
	b = bytes.TrimSpace(b)
	switch {
	case bytes.Equal(b, []byte("null")):
		return nil
	case len(b) > 0 && b[0] == '"':
		var s string
		if err := json.Unmarshal(b, &s); err != nil {
			return err
		}
		f.s, f.set = s, true
	default:
		f.s, f.set, f.isNum = string(b), true, true
	}
	return nil
}

func (f flex) text() string { return strings.TrimSpace(f.s) }

func (f flex) boolean() bool { return f.s == "true" || f.s == "1" }

func (f flex) uint() *uint64 {
	if !f.set {
		return nil
	}
	v, err := strconv.ParseUint(strings.TrimSpace(f.s), 10, 64)
	if err != nil {
		return nil
	}
	return &v
}

// ParseLsblk decodes `lsblk --json --list --bytes` output (LsblkArgs).
func ParseLsblk(b []byte) ([]BlockDevice, error) {
	if len(b) > MaxLsblkBytes {
		return nil, fmt.Errorf("lsblk output exceeds %d bytes", MaxLsblkBytes)
	}
	var doc struct {
		Blockdevices []map[string]flex `json:"blockdevices"`
	}
	if err := json.Unmarshal(b, &doc); err != nil {
		return nil, fmt.Errorf("lsblk JSON: %w", err)
	}
	if doc.Blockdevices == nil {
		return nil, fmt.Errorf("lsblk JSON has no blockdevices")
	}
	out := make([]BlockDevice, 0, len(doc.Blockdevices))
	for _, r := range doc.Blockdevices {
		d := BlockDevice{
			Name: r["name"].text(), KName: r["kname"].text(), PKName: r["pkname"].text(), Path: r["path"].text(),
			Type: r["type"].text(), MajMin: r["maj:min"].text(), Size: r["size"].uint(),
			ReadOnly: r["ro"].boolean(), Removable: r["rm"].boolean(), Rotational: r["rota"].boolean(),
			Transport: r["tran"].text(), Model: r["model"].text(), Serial: r["serial"].text(), WWN: r["wwn"].text(),
			FSType: r["fstype"].text(), UUID: r["uuid"].text(), Label: r["label"].text(),
			PartUUID: r["partuuid"].text(), PartLabel: r["partlabel"].text(),
		}
		if !KName.MatchString(d.KName) {
			return nil, fmt.Errorf("lsblk row with invalid kernel name")
		}
		out = append(out, d)
	}
	return out, nil
}

// SMARTCandidate reports whether smartctl should be asked about a block
// device: a whole disk on SATA/SAS/USB (sd*) or NVMe. virtio-blk, md, loop,
// zram, optical and device-mapper devices have no SMART.
func SMARTCandidate(d BlockDevice) bool {
	if d.Type != "disk" || !KName.MatchString(d.KName) {
		return false
	}
	return strings.HasPrefix(d.KName, "sd") || (strings.HasPrefix(d.KName, "nvme") && strings.Contains(d.KName, "n"))
}

// SMART is the typed subset of `smartctl --json` that the UI shows.
type SMART struct {
	Protocol       string  `json:"protocol,omitempty"` // ATA, NVMe, SCSI
	Model          string  `json:"model,omitempty"`
	Serial         string  `json:"serial,omitempty"`
	Firmware       string  `json:"firmware,omitempty"`
	Standby        bool    `json:"standby,omitempty"` // in standby: not woken, no data read
	Available      *bool   `json:"available,omitempty"`
	Enabled        *bool   `json:"enabled,omitempty"`
	Passed         *bool   `json:"passed,omitempty"` // overall-health self-assessment
	TemperatureC   *int64  `json:"temperature_c,omitempty"`
	PowerOnHours   *uint64 `json:"power_on_hours,omitempty"`
	Reallocated    *uint64 `json:"reallocated_sectors,omitempty"`   // ATA 5
	Pending        *uint64 `json:"pending_sectors,omitempty"`       // ATA 197
	Uncorrectable  *uint64 `json:"offline_uncorrectable,omitempty"` // ATA 198
	CRCErrors      *uint64 `json:"udma_crc_errors,omitempty"`       // ATA 199
	CriticalWarn   *uint64 `json:"nvme_critical_warning,omitempty"`
	PercentageUsed *uint64 `json:"nvme_percentage_used,omitempty"`
	MediaErrors    *uint64 `json:"nvme_media_errors,omitempty"`
	AvailableSpare *uint64 `json:"nvme_available_spare,omitempty"`
	ExitStatus     int     `json:"exit_status"` // smartctl exit bitmask (smartctl(8) RETURN VALUES)
	// Unavailable is a closed reason when the health data could not be read
	// by design rather than by fault.
	Unavailable string   `json:"unavailable,omitempty"`
	Messages    []string `json:"messages,omitempty"`
}

// UnavailableNVMeAdmin: the kernel allows the NVMe SMART/Health log page
// (an admin command) only with CAP_SYS_ADMIN, which the helper does not hold.
const UnavailableNVMeAdmin = "nvme-needs-cap-sys-admin"

// Exit-status bits from smartctl(8).
const (
	SmartExitCmdLine     = 1 << 0
	SmartExitOpenFailed  = 1 << 1 // also: device in standby with --nocheck=standby
	SmartExitCommandFail = 1 << 2
	SmartExitDiskFailing = 1 << 3
	SmartExitPrefail     = 1 << 4
	SmartExitUsagePast   = 1 << 5
	SmartExitErrorLog    = 1 << 6
	SmartExitSelfTestLog = 1 << 7
)

// ParseSMART decodes `smartctl --json` output (SmartctlArgs). The exit status
// is taken from the JSON (smartctl.exit_status) and checked against the
// process exit code when that is known (>= 0).
func ParseSMART(b []byte, processExit int) (SMART, error) {
	if len(b) > MaxSmartBytes {
		return SMART{}, fmt.Errorf("smartctl output exceeds %d bytes", MaxSmartBytes)
	}
	var doc struct {
		Smartctl struct {
			ExitStatus *int `json:"exit_status"`
			Messages   []struct {
				String   string `json:"string"`
				Severity string `json:"severity"`
			} `json:"messages"`
		} `json:"smartctl"`
		Device struct {
			Protocol string `json:"protocol"`
		} `json:"device"`
		ModelName       string `json:"model_name"`
		SerialNumber    string `json:"serial_number"`
		FirmwareVersion string `json:"firmware_version"`
		SmartSupport    struct {
			Available *bool `json:"available"`
			Enabled   *bool `json:"enabled"`
		} `json:"smart_support"`
		SmartStatus struct {
			Passed *bool `json:"passed"`
		} `json:"smart_status"`
		Temperature struct {
			Current *int64 `json:"current"`
		} `json:"temperature"`
		PowerOnTime struct {
			Hours *uint64 `json:"hours"`
		} `json:"power_on_time"`
		ATA struct {
			Table []struct {
				ID  int `json:"id"`
				Raw struct {
					Value *uint64 `json:"value"`
				} `json:"raw"`
			} `json:"table"`
		} `json:"ata_smart_attributes"`
		NVMe *struct {
			CriticalWarning *uint64 `json:"critical_warning"`
			PercentageUsed  *uint64 `json:"percentage_used"`
			MediaErrors     *uint64 `json:"media_errors"`
			AvailableSpare  *uint64 `json:"available_spare"`
		} `json:"nvme_smart_health_information_log"`
	}
	if err := json.Unmarshal(b, &doc); err != nil {
		return SMART{}, fmt.Errorf("smartctl JSON: %w", err)
	}
	if doc.Smartctl.ExitStatus == nil {
		return SMART{}, fmt.Errorf("smartctl JSON has no exit_status")
	}
	if processExit >= 0 && processExit != *doc.Smartctl.ExitStatus {
		return SMART{}, fmt.Errorf("smartctl exit status %d does not match its JSON (%d)", processExit, *doc.Smartctl.ExitStatus)
	}
	s := SMART{
		Protocol: clip(doc.Device.Protocol), Model: clip(doc.ModelName), Serial: clip(doc.SerialNumber), Firmware: clip(doc.FirmwareVersion),
		Available: doc.SmartSupport.Available, Enabled: doc.SmartSupport.Enabled, Passed: doc.SmartStatus.Passed,
		TemperatureC: doc.Temperature.Current, PowerOnHours: doc.PowerOnTime.Hours, ExitStatus: *doc.Smartctl.ExitStatus,
	}
	for _, a := range doc.ATA.Table {
		switch a.ID {
		case 5:
			s.Reallocated = a.Raw.Value
		case 197:
			s.Pending = a.Raw.Value
		case 198:
			s.Uncorrectable = a.Raw.Value
		case 199:
			s.CRCErrors = a.Raw.Value
		}
	}
	if doc.NVMe != nil {
		s.CriticalWarn, s.PercentageUsed, s.MediaErrors, s.AvailableSpare =
			doc.NVMe.CriticalWarning, doc.NVMe.PercentageUsed, doc.NVMe.MediaErrors, doc.NVMe.AvailableSpare
	}
	for _, m := range doc.Smartctl.Messages {
		if strings.Contains(strings.ToUpper(m.String), "STANDBY") {
			s.Standby = true
		}
		if s.Protocol == "NVMe" && strings.Contains(m.String, "NVME_IOCTL_ADMIN_CMD") && strings.Contains(m.String, "Permission denied") {
			s.Unavailable = UnavailableNVMeAdmin
		}
		if len(s.Messages) < MaxMessages {
			s.Messages = append(s.Messages, clip(m.String))
		}
	}
	return s, nil
}

// Health summarises a SMART reading for the UI: "failing" when the drive's
// own self-assessment failed or smartctl flags it, "warning" for degraded
// counters, "ok" when it passed, "unavailable" when the data cannot be read by
// design (see Unavailable), "unknown" when nothing could be read.
func (s SMART) Health() string {
	switch {
	case s.Standby:
		return "standby"
	case s.Unavailable != "":
		return "unavailable"
	case s.Passed != nil && !*s.Passed, s.ExitStatus&SmartExitDiskFailing != 0:
		return "failing"
	case s.ExitStatus&(SmartExitPrefail|SmartExitUsagePast) != 0,
		nonZero(s.Reallocated), nonZero(s.Pending), nonZero(s.Uncorrectable), nonZero(s.CriticalWarn), nonZero(s.MediaErrors):
		return "warning"
	case s.Passed != nil && *s.Passed:
		return "ok"
	default:
		return "unknown"
	}
}

func nonZero(v *uint64) bool { return v != nil && *v > 0 }

func clip(s string) string {
	s = strings.TrimSpace(s)
	if len(s) > MaxMessageText {
		return s[:MaxMessageText]
	}
	return s
}

// CheckJSONReport verifies that an LVM report is one bounded JSON object, so
// the helper never forwards truncated or non-JSON output.
func CheckJSONReport(b []byte) error {
	if len(b) > MaxLVMBytes {
		return fmt.Errorf("report exceeds %d bytes", MaxLVMBytes)
	}
	var v map[string]json.RawMessage
	if err := json.Unmarshal(b, &v); err != nil {
		return fmt.Errorf("report is not a JSON object: %w", err)
	}
	if _, ok := v["report"]; !ok {
		return fmt.Errorf("report has no \"report\" member")
	}
	return nil
}
