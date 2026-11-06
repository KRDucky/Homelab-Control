// SPDX-FileCopyrightText: 2026 The Homelab Control Authors
//
// SPDX-License-Identifier: GPL-3.0-or-later

package storageinfo

import (
	"sort"
	"strings"
)

// Identifier retains independent corroborating evidence even when the user
// chooses a different display label. Display labels are computed by the UI
// (frontend/app/src/storageModel.ts) and never authorize writes.
type Identifier struct {
	Kind      string `json:"kind"`
	Value     string `json:"value"`
	Source    string `json:"source"`
	Ambiguous bool   `json:"ambiguous"`
	// Scope is "device" unless the identifier names something wider. An NVMe
	// controller serial is shared by every namespace of that controller.
	Scope string `json:"scope,omitempty"`
}

// Identifier kinds. Only "wwn" (NAA or EUI designators) satisfies the WWN
// display preference. Vendor-derived designators are not globally unique.
const (
	KindWWN              = "wwn"               // validated naa.* (types 2/5/6) or eui.* designator
	KindNamespaceUUID    = "nvme-uuid"         // NVMe namespace UUID (uuid.*)
	KindT10VendorID      = "t10-vendor-id"     // t10.* vendor/model/serial string
	KindNVMeVendorID     = "nvme-vendor-id"    // nvme.* string synthesized by the kernel
	KindWWIDUnclassified = "wwid-unclassified" // raw wwid: unknown prefix or malformed/locally assigned naa./eui./uuid.
	KindSerial           = "serial"
	KindControllerSerial = "controller-serial"
)

// ClassifyWWID maps a sysfs wwid value to an identifier kind; see CanonicalWWID.
func ClassifyWWID(raw string) (kind string, ok bool) {
	kind, _, ok = CanonicalWWID(raw)
	return kind, ok
}

// CanonicalWWID validates a sysfs wwid and returns its kind and the value used
// for display and duplicate detection. Formats follow the kernel writers:
//   - SCSI VPD page 0x83 (drivers/scsi/scsi_lib.c scsi_vpd_lun_id): NAA as
//     "naa.%8phN" or "naa.%16phN", EUI-64 as "eui.%8phN", "%12phN" or
//     "%16phN" (lowercase hex), T10 as "t10.*", and a SCSI name string
//     designator copied verbatim (SPC: "naa."/"eui." with uppercase hex).
//   - NVMe (drivers/nvme/host/sysfs.c wwid_show): "uuid.%pU", NGUID as
//     "eui.%16phN", EUI-64 as "eui.%8phN", else a synthesized "nvme.*".
//
// Only a well-formed, non-zero NAA (types 2, 5 or 6) or EUI designator is a
// world-wide name. NAA type 3 is locally assigned, so it is not globally
// unique. Hex case is canonicalized so equivalent spellings collide; a value
// that fails validation is kept as raw, unclassified evidence and is never in
// the WWN tier.
func CanonicalWWID(raw string) (kind, value string, ok bool) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return "", "", false
	}
	lower := strings.ToLower(raw)
	prefix, rest, _ := strings.Cut(lower, ".")
	switch prefix {
	case "naa":
		if validNAA(rest) {
			return KindWWN, lower, true
		}
	case "eui":
		if hexDigits(rest) && (len(rest) == 16 || len(rest) == 24 || len(rest) == 32) && !allZero(rest) {
			return KindWWN, lower, true
		}
	case "uuid":
		if validUUID(rest) {
			return KindNamespaceUUID, lower, true
		}
	case "t10":
		return KindT10VendorID, raw, true
	case "nvme":
		return KindNVMeVendorID, raw, true
	}
	return KindWWIDUnclassified, raw, true
}

// failedDesignator reports a naa./eui./uuid. value that did not validate. It
// stays identifier evidence but never becomes a persistent identity: an
// all-zero or truncated designator is typical of placeholder firmware values
// shared by unrelated devices.
func failedDesignator(raw string) bool {
	kind, _, ok := CanonicalWWID(raw)
	if !ok || kind != KindWWIDUnclassified {
		return false
	}
	prefix, _, _ := strings.Cut(strings.ToLower(strings.TrimSpace(raw)), ".")
	return prefix == "naa" || prefix == "eui" || prefix == "uuid"
}

// validNAA checks the NAA field (first nibble) against the designator length:
// 8 bytes for types 2 (IEEE extended) and 5 (IEEE registered), 16 bytes for
// type 6 (IEEE registered extended). Type 3 (locally assigned) is refused.
func validNAA(h string) bool {
	if !hexDigits(h) || allZero(h) {
		return false
	}
	switch len(h) {
	case 16:
		return h[0] == '2' || h[0] == '5'
	case 32:
		return h[0] == '6'
	}
	return false
}

func validUUID(u string) bool {
	if len(u) != 36 || u[8] != '-' || u[13] != '-' || u[18] != '-' || u[23] != '-' {
		return false
	}
	h := strings.ReplaceAll(u, "-", "")
	return hexDigits(h) && !allZero(h)
}

func hexDigits(s string) bool {
	if s == "" {
		return false
	}
	for i := 0; i < len(s); i++ {
		c := s[i]
		if !(c >= '0' && c <= '9' || c >= 'a' && c <= 'f') {
			return false
		}
	}
	return true
}

func allZero(s string) bool { return strings.Trim(s, "0") == "" }

func identifiers(b Block) []Identifier {
	out := []Identifier{}
	seen := map[string]bool{}
	add := func(kind, value, source, scope string) {
		if value != "" && !seen[kind+":"+value] {
			out = append(out, Identifier{Kind: kind, Value: value, Source: source, Scope: scope})
			seen[kind+":"+value] = true
		}
	}
	// A partition never inherits whole-device identifiers (review A5).
	if b.Kind != "partition" {
		if kind, value, ok := CanonicalWWID(b.WWID); ok {
			add(kind, value, sourceOr(b.WWIDSource, "sysfs wwid"), "device")
		}
		add(KindSerial, b.Serial, sourceOr(b.SerialSource, "sysfs"), "device")
		add(KindControllerSerial, b.ControllerSerial, "sysfs device/serial (NVMe controller)", "controller")
	}
	add("partuuid", b.PartUUID, "blkid", "device")
	if b.NativeKind == "dm-uuid" || b.NativeKind == "md-uuid" {
		add(b.NativeKind, b.NativeUUID, "sysfs", "device")
	}
	ids := append([]string(nil), b.ByID...)
	sort.Strings(ids)
	for _, id := range ids {
		add("by-id", id, "/dev/disk/by-id", "device")
	}
	return out
}

func sourceOr(s, fallback string) string {
	if s == "" {
		return fallback
	}
	return s
}

// identifierOwner names what an identifier is expected to be unique to. An
// identifier is ambiguous when more than one owner reports it. A controller
// serial is owned by its controller, so several namespaces of one NVMe
// controller are not reported as cloned drives (review A4).
func identifierOwner(b Block, id Identifier) string {
	if id.Kind == KindControllerSerial && b.ControllerKey != "" {
		return "controller\x00" + b.ControllerKey
	}
	return "device\x00" + b.DeviceNumber
}
