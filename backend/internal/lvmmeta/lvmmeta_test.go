// SPDX-FileCopyrightText: 2026 The Homelab Control Authors
//
// SPDX-License-Identifier: GPL-3.0-or-later

package lvmmeta

import (
	"bytes"
	"encoding/binary"
	"strings"
	"testing"
)

const testVGMeta = `contents = "Text Format Volume Group"
version = 1

vg1 {
    id = "aaaaaaaa-bbbb-cccc-dddd-eeeeffff0001"
    seqno = 2
    extent_size = 8192
    status = ["RESIZEABLE", "READ", "WRITE"]
    physical_volumes {
        pv0 {
            id = "abcdef12-3456-7890-abcd-ef1234567890"
            device = "/dev/vdb"
            dev_size = 10485760
            pe_start = 2048
            pe_count = 1024
        }
    }
    logical_volumes {
        lv1 {
            id = "99999999-8888-7777-6666-5555-444444444444"
            status = ["READ", "WRITE", "VISIBLE"]
            segment_count = 1
            segment1 {
                start_extent = 0
                extent_count = 128
                type = "striped"
                stripe_count = 1
                stripes = ["pv0", 0]
            }
        }
    }
}
`

const testPVUUID = "abcdef1234567890abcdef1234567890"

// buildImage builds a synthetic LVM2 device image grounded in lib/label/label.h
// and lib/format_text/layout.h: a 512-byte label sector (CRC over 20..512) and
// one MDA at mdaOffset with a raw_locn copy of the metadata text (CRC-validated).
func buildImage(text string, mutate func(img []byte)) []byte {
	const mdaOffset, mdaSize = 4096, 8192
	img := make([]byte, mdaOffset+mdaSize)

	// Label sector 0.
	s := img[0:512]
	copy(s[0:8], labelID)
	binary.LittleEndian.PutUint64(s[8:16], 0)   // sector_xl
	binary.LittleEndian.PutUint32(s[20:24], 32) // offset_xl -> pv_header
	copy(s[24:32], lvm2Label)
	copy(s[32:64], []byte(testPVUUID))
	binary.LittleEndian.PutUint64(s[64:72], 10485760) // device_size
	// data areas NULL-terminated at 72..88 (zero).
	// one metadata area disk_locn at 88..104.
	binary.LittleEndian.PutUint64(s[88:96], mdaOffset)
	binary.LittleEndian.PutUint64(s[96:104], mdaSize)
	binary.LittleEndian.PutUint32(s[16:20], crc(initCRC, s[20:512]))

	// MDA header at mdaOffset.
	h := img[mdaOffset : mdaOffset+512]
	copy(h[4:20], ftMagic)
	binary.LittleEndian.PutUint32(h[20:24], 1) // version
	binary.LittleEndian.PutUint64(h[24:32], mdaOffset)
	binary.LittleEndian.PutUint64(h[32:40], mdaSize)
	// raw_locn[0] at 40..64: offset=MDAHeaderSize, size=len(text), checksum, flags=0.
	binary.LittleEndian.PutUint64(h[40:48], MDAHeaderSize)
	binary.LittleEndian.PutUint64(h[48:56], uint64(len(text)))
	binary.LittleEndian.PutUint32(h[56:60], crc(initCRC, []byte(text)))
	// text at mdaOffset + MDAHeaderSize.
	copy(img[mdaOffset+MDAHeaderSize:], text)
	binary.LittleEndian.PutUint32(h[0:4], crc(initCRC, h[4:512]))

	if mutate != nil {
		mutate(img)
	}
	return img
}

func TestReadPVValid(t *testing.T) {
	img := buildImage(testVGMeta, nil)
	pv := ReadPV(bytes.NewReader(img), uint64(len(img)), "vdb")
	if pv.Status != StatusOK {
		t.Fatalf("status = %s, want ok", pv.Status)
	}
	if pv.Label == nil || pv.Label.PVUUID != testPVUUID {
		t.Fatalf("label uuid = %+v", pv.Label)
	}
	if pv.Text == nil {
		t.Fatal("no metadata text extracted")
	}
	root, err := parseText(pv.Text)
	if err != nil {
		t.Fatalf("parse text: %v", err)
	}
	vgNode := findVGNode(root)
	if vgNode == nil {
		t.Fatal("no VG section")
	}
	vg, err := ParseVG("vg1", vgNode)
	if err != nil {
		t.Fatalf("ParseVG: %v", err)
	}
	if vg.Name != "vg1" || vg.Seqno != 2 || len(vg.PVs) != 1 || len(vg.LVs) != 1 {
		t.Fatalf("vg = %+v", vg)
	}
	if len(vg.LVs[0].Segments) != 1 || !vg.LVs[0].Segments[0].Parsed || len(vg.LVs[0].Segments[0].Stripes) != 1 {
		t.Fatalf("segment = %+v", vg.LVs[0].Segments)
	}

	inv := Assemble([]PVRead{pv})
	if len(inv.VGs) != 1 || inv.VGs[0].State != StateComplete {
		t.Fatalf("assemble = %+v", inv.VGs)
	}
}

func TestReadPVBadLabelCRC(t *testing.T) {
	img := buildImage(testVGMeta, func(img []byte) {
		img[100] ^= 0xff // corrupt the label sector (within CRC coverage)
	})
	pv := ReadPV(bytes.NewReader(img), uint64(len(img)), "vdb")
	if pv.Status != StatusLabelBadCRC {
		t.Fatalf("status = %s, want label-bad-crc", pv.Status)
	}
}

func TestReadPVBadMDACRC(t *testing.T) {
	img := buildImage(testVGMeta, func(img []byte) {
		img[4096+100] ^= 0xff // corrupt the MDA header (within CRC coverage)
	})
	pv := ReadPV(bytes.NewReader(img), uint64(len(img)), "vdb")
	if pv.Status != StatusMDABadCRC {
		t.Fatalf("status = %s, want mda-bad-crc", pv.Status)
	}
}

func TestReadPVNotLVM(t *testing.T) {
	img := make([]byte, 8192)
	pv := ReadPV(bytes.NewReader(img), uint64(len(img)), "vdb")
	if pv.Status != StatusNotLVM {
		t.Fatalf("status = %s, want not-lvm", pv.Status)
	}
}

// mdaSpec is one metadata area in a multi-MDA fixture.
type mdaSpec struct {
	offset, size uint64
	text         string
}

// buildMultiMDA builds a label with several MDA disk_locn entries and their
// headers/text, grounded in layout.h.
func buildMultiMDA(mdas []mdaSpec) []byte {
	total := uint64(512)
	for _, m := range mdas {
		if m.offset+m.size > total {
			total = m.offset + m.size
		}
	}
	img := make([]byte, total)
	s := img[0:512]
	copy(s[0:8], labelID)
	binary.LittleEndian.PutUint64(s[8:16], 0)
	binary.LittleEndian.PutUint32(s[20:24], 32)
	copy(s[24:32], lvm2Label)
	copy(s[32:64], []byte(testPVUUID))
	binary.LittleEndian.PutUint64(s[64:72], 10485760)
	pos := 88 // data areas NULL at 72..88; then MDA disk_locns
	for _, m := range mdas {
		binary.LittleEndian.PutUint64(s[pos:pos+8], m.offset)
		binary.LittleEndian.PutUint64(s[pos+8:pos+16], m.size)
		pos += 16
	}
	binary.LittleEndian.PutUint32(s[16:20], crc(initCRC, s[20:512]))
	for _, m := range mdas {
		h := img[m.offset : m.offset+512]
		copy(h[4:20], ftMagic)
		binary.LittleEndian.PutUint32(h[20:24], 1)
		binary.LittleEndian.PutUint64(h[24:32], m.offset)
		binary.LittleEndian.PutUint64(h[32:40], m.size)
		binary.LittleEndian.PutUint64(h[40:48], MDAHeaderSize)
		binary.LittleEndian.PutUint64(h[48:56], uint64(len(m.text)))
		binary.LittleEndian.PutUint32(h[56:60], crc(initCRC, []byte(m.text)))
		copy(img[m.offset+MDAHeaderSize:], m.text)
		binary.LittleEndian.PutUint32(h[0:4], crc(initCRC, h[4:512]))
	}
	return img
}

func TestReadPVSelectsHighestSeqno(t *testing.T) {
	stale := strings.Replace(testVGMeta, "seqno = 2", "seqno = 2", 1)
	fresh := strings.Replace(testVGMeta, "seqno = 2", "seqno = 5", 1)
	img := buildMultiMDA([]mdaSpec{{4096, 8192, stale}, {12288, 8192, fresh}})
	pv := ReadPV(bytes.NewReader(img), uint64(len(img)), "vdb")
	if pv.Status != StatusOK {
		t.Fatalf("status = %s", pv.Status)
	}
	root, err := parseText(pv.Text)
	if err != nil {
		t.Fatal(err)
	}
	if s := seq(findVGNode(root)); s != 5 {
		t.Fatalf("selected seqno = %d, want 5", s)
	}
}

func TestReadPVInconsistentEqualSeqno(t *testing.T) {
	a := strings.Replace(testVGMeta, "seqno = 2", "seqno = 3", 1)
	b := strings.Replace(testVGMeta, "seqno = 2", "seqno = 3", 1)
	b = strings.Replace(b, "extent_size = 8192", "extent_size = 4096", 1)
	img := buildMultiMDA([]mdaSpec{{4096, 8192, a}, {12288, 8192, b}})
	pv := ReadPV(bytes.NewReader(img), uint64(len(img)), "vdb")
	if pv.Status != StatusMetadataInconsistent {
		t.Fatalf("status = %s, want metadata-inconsistent", pv.Status)
	}
}

func TestSelectCopyCollision(t *testing.T) {
	// Two same-seqno copies whose text differs but whose CRC is forced equal
	// (a collision) must still be inconsistent: content is compared byte-wise,
	// never by CRC.
	a := []byte("vg1 { seqno = 3 extent_size = 8192 }")
	b := []byte("vg1 { seqno = 3 extent_size = 4096 }")
	copies := []MDACopy{
		{Status: CopyOK, Text: a, TextCRC: 42},
		{Status: CopyOK, Text: b, TextCRC: 42},
	}
	if _, st := selectCopy(copies); st != StatusMetadataInconsistent {
		t.Fatalf("status = %s, want metadata-inconsistent", st)
	}
}

func TestParseTextTokenLimit(t *testing.T) {
	// A syntactically valid prefix followed by more tokens than MaxTokens must
	// error with a typed limit error, not be accepted as a complete document.
	body := "vg1 { " + strings.Repeat("k = 1 ", MaxTokens/3+10) + "}"
	if _, err := parseText([]byte(body)); err == nil || !strings.Contains(err.Error(), "token limit") {
		t.Fatalf("err = %v, want token limit error", err)
	}
}

func TestParseTextMalformed(t *testing.T) {
	for _, bad := range []string{
		"vg1 { id = }",
		"vg1 = [1, 2",
		"vg1 { nested { deeper { deeper { deeper { deeper { deeper { deeper { deeper { deeper { x = 1 } } } } } } } } } }",
	} {
		if _, err := parseText([]byte(bad)); err == nil {
			t.Fatalf("expected error for %q", bad)
		}
	}
}

func TestParseSegmentUnsupported(t *testing.T) {
	root, err := parseText([]byte("vg1 { logical_volumes { lv1 { segment1 { type = \"vdo\" } } } }"))
	if err != nil {
		t.Fatal(err)
	}
	vg, err := ParseVG("vg1", findVGNode(root))
	if err != nil {
		t.Fatal(err)
	}
	if len(vg.LVs) != 1 || len(vg.LVs[0].Segments) != 1 {
		t.Fatalf("vg = %+v", vg)
	}
	seg := vg.LVs[0].Segments[0]
	if seg.Parsed || !strings.HasPrefix(seg.Unsupported, "unsupported-segment-type:") {
		t.Fatalf("segment = %+v, want unsupported", seg)
	}
}

func TestAssembleUnsupportedSegmentPartial(t *testing.T) {
	text := `contents = "Text Format Volume Group"
version = 1
vg1 {
    id = "aaaaaaaa-bbbb-cccc-dddd-eeeeffff0001"
    seqno = 1
    extent_size = 8192
    physical_volumes { pv0 { id = "abcdef12-3456-7890-abcd-ef1234567890" device = "/dev/vdb" } }
    logical_volumes { lv1 { id = "99999999-8888-7777-6666-5555-444444444444" segment_count = 1 segment1 { start_extent = 0 extent_count = 1 type = "vdo" } } }
}`
	img := buildImage(text, nil)
	pv := ReadPV(bytes.NewReader(img), uint64(len(img)), "vdb")
	if pv.Status != StatusOK {
		t.Fatalf("status = %s", pv.Status)
	}
	inv := Assemble([]PVRead{pv})
	if len(inv.VGs) != 1 || inv.VGs[0].State != StateUnsupported {
		t.Fatalf("VG state = %+v, want partial-unsupported-segment", inv.VGs)
	}
}

func TestParseTypedSegments(t *testing.T) {
	text := `vg1 { logical_volumes {
		lv_raid { segment1 { type = "raid1" device_count = 2 region_size = 128 stripe_size = 64 raids = ["pv0", 0, "pv1", 0] } }
		lv_cache { segment1 { type = "cache" cache_pool = "cpool" origin = "originlv" } }
		lv_thin { segment1 { type = "thin" thin_pool = "tpool" device_id = 1 transaction_id = 7 } }
	} }`
	root, err := parseText([]byte(text))
	if err != nil {
		t.Fatal(err)
	}
	vg, err := ParseVG("vg1", findVGNode(root))
	if err != nil {
		t.Fatal(err)
	}
	byName := map[string]LV{}
	for _, lv := range vg.LVs {
		byName[lv.Name] = lv
	}
	raid := byName["lv_raid"].Segments[0]
	if !raid.Parsed || raid.Raid == nil || len(raid.Raid.Images) != 2 || raid.Raid.DeviceCount != 2 {
		t.Fatalf("raid segment = %+v", raid)
	}
	cache := byName["lv_cache"].Segments[0]
	if !cache.Parsed || cache.Cache == nil || cache.Cache.CachePool != "cpool" || cache.Cache.Origin != "originlv" {
		t.Fatalf("cache segment = %+v", cache)
	}
	thin := byName["lv_thin"].Segments[0]
	if !thin.Parsed || thin.Thin == nil || thin.Thin.ThinPool != "tpool" || thin.Thin.DeviceID != 1 {
		t.Fatalf("thin segment = %+v", thin)
	}
}

// FuzzReadPVNoPanic feeds hostile bytes through ReadPV and asserts no panic and
// that any failure is a typed status.
func FuzzReadPVNoPanic(f *testing.F) {
	f.Add([]byte("not lvm at all"))
	f.Add(make([]byte, 512))
	f.Add(buildImage(testVGMeta, nil))
	f.Add(buildImage(testVGMeta, func(img []byte) { img[0] = 'L' }))
	f.Fuzz(func(t *testing.T, data []byte) {
		pv := ReadPV(bytes.NewReader(data), uint64(len(data)), "fuzz")
		_ = pv.Status
		if pv.Label != nil && pv.Text != nil {
			_, _ = parseText(pv.Text)
		}
	})
}

func TestAssembleMalformedVGNode(t *testing.T) {
	for _, text := range []string{
		"vg1 { seqno = 1 }",           // missing id
		"vg1 { id { x = 1 } }",        // wrong-kind id (section)
		"vg1 { id = \"not-a-uuid\" }", // invalid UUID value
	} {
		img := buildImage(text, nil)
		pv := ReadPV(bytes.NewReader(img), uint64(len(img)), "vdb")
		if pv.Status != StatusOK {
			t.Fatalf("status = %s for %q", pv.Status, text)
		}
		inv := Assemble([]PVRead{pv})
		if len(inv.VGs) != 0 || len(inv.Orphans) == 0 {
			t.Fatalf("text %q: VGs=%d Orphans=%d, want orphan (no panic)", text, len(inv.VGs), len(inv.Orphans))
		}
	}
}

func TestAssembleTotalTextCap(t *testing.T) {
	big := bytes.Repeat([]byte("x"), MaxTotalText/2+1)
	pvs := []PVRead{
		{Device: "a", Status: StatusOK, Text: big},
		{Device: "b", Status: StatusOK, Text: big},
		{Device: "c", Status: StatusOK, Text: big},
	}
	inv := Assemble(pvs)
	if len(inv.Notes) == 0 {
		t.Fatal("expected an over-cap note")
	}
	total := 0
	for _, pv := range inv.PVs {
		total += len(pv.Text)
	}
	if total > MaxTotalText {
		t.Fatalf("retained %d bytes > MaxTotalText %d", total, MaxTotalText)
	}
}

func TestReadPVBudgetExhausted(t *testing.T) {
	img := buildImage(testVGMeta, nil)
	budget := int64(10)
	pv := ReadPVBudget(bytes.NewReader(img), uint64(len(img)), "vdb", &budget)
	if pv.Status != StatusLimitExceeded {
		t.Fatalf("status = %s, want limit-exceeded", pv.Status)
	}
	if pv.Text != nil {
		t.Fatalf("text retained despite exhausted budget: %d bytes", len(pv.Text))
	}
	if budget != 0 {
		t.Fatalf("budget = %d, want 0", budget)
	}
}

func TestReadPVBudgetAggregate(t *testing.T) {
	img := buildImage(testVGMeta, nil)
	budget := int64(2000)
	total := 0
	for i := 0; i < 100; i++ {
		pv := ReadPVBudget(bytes.NewReader(img), uint64(len(img)), "vdb", &budget)
		if pv.Text != nil {
			total += len(pv.Text)
		}
	}
	if total > 2000 {
		t.Fatalf("retained %d bytes > budget 2000", total)
	}
	if budget < 0 {
		t.Fatalf("budget went negative: %d", budget)
	}
}

func TestUUIDCharset(t *testing.T) {
	// LVM2 UUID alphabet (lib/id/id.c _c) is alphanumeric + '!' + '#', not hex.
	mixed := strings.Repeat("Ab", 16) // 32 chars, mixed case
	if len(mixed) != IDLen {
		t.Fatalf("test len %d != IDLen %d", len(mixed), IDLen)
	}
	if got := uuidString([]byte(mixed)); got != mixed {
		t.Fatalf("uuidString = %q, want %q (exact case preserved)", got, mixed)
	}
	withSpecial := strings.Repeat("A", 30) + "!#"
	if got := uuidString([]byte(withSpecial)); got != withSpecial {
		t.Fatalf("special-char uuid = %q", got)
	}
	// Out-of-alphabet characters are rejected.
	bad := strings.Repeat("*", IDLen)
	if got := uuidString([]byte(bad)); got != "" {
		t.Fatalf("out-of-alphabet uuid accepted: %q", got)
	}
}

func TestAssembleNoDoubleOrphan(t *testing.T) {
	// A PV already classified (over-cap -> Text nil) must not be re-added to
	// orphans by the parse pass.
	pvs := []PVRead{{Device: "over", Status: StatusLimitExceeded, Text: nil}}
	inv := Assemble(pvs)
	if len(inv.Orphans) != 0 {
		t.Fatalf("orphans = %d, want 0 (no double-add)", len(inv.Orphans))
	}
	if len(inv.PVs) != 1 || inv.PVs[0].Status != StatusLimitExceeded {
		t.Fatalf("PVs = %+v", inv.PVs)
	}
}

func TestParsePVHeaderRequiresTerminator(t *testing.T) {
	b := make([]byte, 512)
	copy(b[0:32], []byte("aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"))
	binary.LittleEndian.PutUint64(b[32:40], 1048576)
	// Fill both disk_locn lists with non-zero entries and no NULL terminator.
	for i := 40; i+16 <= 512; i += 16 {
		binary.LittleEndian.PutUint64(b[i:i+8], 1)
		binary.LittleEndian.PutUint64(b[i+8:i+16], 1)
	}
	_, _, _, _, _, st := parsePVHeader(b, 0)
	if st != StatusPVHeaderOverrun {
		t.Fatalf("status = %s, want pv-header-overrun", st)
	}
}
