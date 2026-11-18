// SPDX-FileCopyrightText: 2026 The Homelab Control Authors
//
// SPDX-License-Identifier: GPL-3.0-or-later

package lvmmeta

import (
	"bytes"
	"context"
	"encoding/binary"
	"io"
	"strconv"
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
	root, err := parseText([]byte("vg1 { logical_volumes { lv1 { segment_count = 1 segment1 { start_extent = 0 extent_count = 1 type = \"vdo\" } } } }"))
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
		lv_raid { segment_count = 1 segment1 { start_extent = 0 extent_count = 1 type = "raid1" device_count = 2 region_size = 128 stripe_size = 64 raids = ["pv0", 0, "pv1", 0] } }
		lv_cache { segment_count = 1 segment1 { start_extent = 0 extent_count = 1 type = "cache" cache_pool = "cpool" origin = "originlv" } }
		lv_thin { segment_count = 1 segment1 { start_extent = 0 extent_count = 1 type = "thin" thin_pool = "tpool" device_id = 1 transaction_id = 7 } }
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

// cancelAfterReader wraps a ReaderAt and cancels ctx once it has served more
// than stopAt reads, modeling a request deadline firing mid-device.
type cancelAfterReader struct {
	r      io.ReaderAt
	stopAt int
	stop   func()
	n      int
}

func (c *cancelAfterReader) ReadAt(p []byte, off int64) (int, error) {
	c.n++
	if c.n > c.stopAt {
		c.stop()
	}
	return c.r.ReadAt(p, off)
}

func TestReadPVBudgetContextPreCanceled(t *testing.T) {
	img := buildImage(testVGMeta, nil)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	r := &cancelAfterReader{r: bytes.NewReader(img), stopAt: -1, stop: func() {}}
	pv := ReadPVBudgetContext(ctx, r, uint64(len(img)), "vdb", nil)
	if pv.Status != StatusCanceled {
		t.Fatalf("status = %s, want canceled", pv.Status)
	}
	if r.n != 0 {
		t.Fatalf("reader called %d times on an already-canceled context", r.n)
	}
}

func TestReadPVBudgetContextCancelBetweenReads(t *testing.T) {
	img := buildImage(testVGMeta, nil)
	ctx, cancel := context.WithCancel(context.Background())
	// Cancel after the first label-sector read: the next pre-read check must
	// abort before the MDA header is ever read.
	r := &cancelAfterReader{r: bytes.NewReader(img), stopAt: 1, stop: cancel}
	pv := ReadPVBudgetContext(ctx, r, uint64(len(img)), "vdb", nil)
	if pv.Status != StatusCanceled {
		t.Fatalf("status = %s, want canceled", pv.Status)
	}
	if r.n > 2 {
		t.Fatalf("reader called %d times; want abort within 2 reads", r.n)
	}
}

func TestReadPVBudgetContextPerDeviceCap(t *testing.T) {
	// The per-device cap fires independently of the shared budget and does not
	// zero the shared budget (unlike a shared-budget exhaustion).
	res := &PVRead{Text: make([]byte, MaxDeviceText+1)}
	budget := int64(MaxTotalText)
	if got := enforceTextCaps(res, &budget); got != StatusLimitExceeded {
		t.Fatalf("status = %s, want limit-exceeded", got)
	}
	if res.Text != nil {
		t.Fatalf("text retained despite per-device cap: %d bytes", len(res.Text))
	}
	if budget != MaxTotalText {
		t.Fatalf("shared budget changed on per-device breach: %d", budget)
	}

	// A shared-budget breach zeroes the budget.
	res2 := &PVRead{Text: []byte("x")}
	b2 := int64(0)
	if got := enforceTextCaps(res2, &b2); got != StatusLimitExceeded {
		t.Fatalf("status = %s, want limit-exceeded", got)
	}
	if b2 != 0 || res2.Text != nil {
		t.Fatalf("budget=%d text=%v, want 0/nil", b2, res2.Text)
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

// vgText builds a two-PV VG section (same VG UUID and seqno across copies);
// extra is appended as an extra top-level field so two copies can differ in
// text bytes while still parsing to the same VG.
func vgText(seqno int, extra string) string {
	return "vg1 {\n" +
		"    id = \"aaaaaaaa-bbbb-cccc-dddd-eeeeffff0001\"\n" +
		"    seqno = " + strconv.Itoa(seqno) + "\n" +
		"    extent_size = 8192\n" +
		"    physical_volumes {\n" +
		"        pv0 {\n" +
		"            id = \"11111111-2222-3333-4444-555555555555\"\n" +
		"            device = \"/dev/vdb\"\n" +
		"            dev_size = 10485760\n" +
		"            pe_start = 2048\n" +
		"            pe_count = 1024\n" +
		"        }\n" +
		"        pv1 {\n" +
		"            id = \"22222222-3333-4444-5555-666677778888\"\n" +
		"            device = \"/dev/vdc\"\n" +
		"            dev_size = 10485760\n" +
		"            pe_start = 2048\n" +
		"            pe_count = 1024\n" +
		"        }\n" +
		"    }\n" +
		"    logical_volumes {\n" +
		"        lv1 {\n" +
		"            id = \"99999999-8888-7777-6666-5555-444444444444\"\n" +
		"            status = [\"READ\", \"WRITE\", \"VISIBLE\"]\n" +
		"            segment_count = 1\n" +
		"            segment1 {\n" +
		"                start_extent = 0\n" +
		"                extent_count = 128\n" +
		"                type = \"striped\"\n" +
		"                stripe_count = 1\n" +
		"                stripes = [\"pv0\", 0]\n" +
		"            }\n" +
		"        }\n" +
		"    }\n" +
		extra +
		"}\n"
}

func TestParseVGMalformedDevSize(t *testing.T) {
	// dev_size in sectors must not wrap when multiplied to bytes (C9).
	text := `vg1 {
    id = "aaaaaaaa-bbbb-cccc-dddd-eeeeffff0001"
    seqno = 1
    extent_size = 8192
    physical_volumes {
        pv0 {
            id = "11111111-2222-3333-4444-555555555555"
            device = "/dev/vdb"
            dev_size = 36028797018963969
            pe_start = 2048
            pe_count = 1024
        }
    }
}`
	root, err := parseText([]byte(text))
	if err != nil {
		t.Fatalf("parse text: %v", err)
	}
	vg, err := ParseVG("vg1", findVGNode(root))
	if err != nil {
		t.Fatalf("ParseVG: %v", err)
	}
	if len(vg.PVs) != 1 || !vg.PVs[0].Malformed {
		t.Fatalf("pv = %+v, want Malformed", vg.PVs)
	}
	if vg.PVs[0].DevSize != 0 {
		t.Fatalf("dev size = %d, want 0 (not wrapped)", vg.PVs[0].DevSize)
	}
}

func TestAssembleMalformedDevSize(t *testing.T) {
	text := `vg1 {
    id = "aaaaaaaa-bbbb-cccc-dddd-eeeeffff0001"
    seqno = 1
    extent_size = 8192
    physical_volumes {
        pv0 {
            id = "11111111-2222-3333-4444-555555555555"
            device = "/dev/vdb"
            dev_size = 36028797018963969
            pe_start = 2048
            pe_count = 1024
        }
    }
}`
	pv := PVRead{Device: "vdb", Status: StatusOK, Text: []byte(text), Label: &Label{PVUUID: "11111111222233334444555555555555"}}
	inv := Assemble([]PVRead{pv})
	if len(inv.VGs) != 1 || inv.VGs[0].State != StateMalformed {
		t.Fatalf("vgs = %+v, want partial-malformed", inv.VGs)
	}
}

func TestAssembleInconsistentDeterministic(t *testing.T) {
	// Same VG UUID + same seqno but different content must be inconsistent,
	// regardless of input order. The two devices are distinct PVs.
	a := vgText(5, "")
	b := vgText(5, "    extra_flag = 1\n")
	pvA := PVRead{Device: "a", Status: StatusOK, Text: []byte(a), Label: &Label{PVUUID: "11111111222233334444555555555555"}}
	pvB := PVRead{Device: "b", Status: StatusOK, Text: []byte(b), Label: &Label{PVUUID: "22222222333344445555666677778888"}}

	inv1 := Assemble([]PVRead{pvA, pvB})
	inv2 := Assemble([]PVRead{pvB, pvA})
	if len(inv1.VGs) != 1 || inv1.VGs[0].State != StateInconsistent {
		t.Fatalf("order A,B: vgs = %+v, want inconsistent", inv1.VGs)
	}
	if len(inv2.VGs) != 1 || inv2.VGs[0].State != StateInconsistent {
		t.Fatalf("order B,A: vgs = %+v, want inconsistent", inv2.VGs)
	}
	if inv1.VGs[0].UUID != inv2.VGs[0].UUID {
		t.Fatalf("representative UUID differs by input order: %q vs %q", inv1.VGs[0].UUID, inv2.VGs[0].UUID)
	}
}

func TestAssembleConsistentEqualSeqno(t *testing.T) {
	// Identical same-seqno copies are consistent, not inconsistent.
	a := vgText(5, "")
	pvA := PVRead{Device: "a", Status: StatusOK, Text: []byte(a), Label: &Label{PVUUID: "11111111222233334444555555555555"}}
	pvB := PVRead{Device: "b", Status: StatusOK, Text: []byte(a), Label: &Label{PVUUID: "22222222333344445555666677778888"}}
	inv := Assemble([]PVRead{pvA, pvB})
	if len(inv.VGs) != 1 || inv.VGs[0].State != StateComplete {
		t.Fatalf("vgs = %+v, want complete", inv.VGs)
	}
}

// buildImageTwoSlots builds one MDA whose header has both raw_locn slots:
// slot0 (committed) and slot1 (precommit), each pointing at distinct text.
func buildImageTwoSlots(committed, precommitted string) []byte {
	const mdaOffset, mdaSize = 4096, 8192
	img := make([]byte, mdaOffset+mdaSize)
	s := img[0:512]
	copy(s[0:8], labelID)
	binary.LittleEndian.PutUint64(s[8:16], 0)
	binary.LittleEndian.PutUint32(s[20:24], 32)
	copy(s[24:32], lvm2Label)
	copy(s[32:64], []byte(testPVUUID))
	binary.LittleEndian.PutUint64(s[64:72], 10485760)
	binary.LittleEndian.PutUint64(s[88:96], mdaOffset)
	binary.LittleEndian.PutUint64(s[96:104], mdaSize)
	binary.LittleEndian.PutUint32(s[16:20], crc(initCRC, s[20:512]))

	h := img[mdaOffset : mdaOffset+512]
	copy(h[4:20], ftMagic)
	binary.LittleEndian.PutUint32(h[20:24], 1)
	binary.LittleEndian.PutUint64(h[24:32], mdaOffset)
	binary.LittleEndian.PutUint64(h[32:40], mdaSize)

	slot0off := uint64(MDAHeaderSize)
	slot1off := slot0off + uint64(len(committed))
	// slot0 (committed) raw_locn at 40..64.
	binary.LittleEndian.PutUint64(h[40:48], slot0off)
	binary.LittleEndian.PutUint64(h[48:56], uint64(len(committed)))
	binary.LittleEndian.PutUint32(h[56:60], crc(initCRC, []byte(committed)))
	// slot1 (precommit) raw_locn at 64..88.
	binary.LittleEndian.PutUint64(h[64:72], slot1off)
	binary.LittleEndian.PutUint64(h[72:80], uint64(len(precommitted)))
	binary.LittleEndian.PutUint32(h[80:84], crc(initCRC, []byte(precommitted)))

	copy(img[mdaOffset+int(slot0off):], committed)
	copy(img[mdaOffset+int(slot1off):], precommitted)
	binary.LittleEndian.PutUint32(h[0:4], crc(initCRC, h[4:512]))
	return img
}

func TestReadPVUsesCommittedSlot(t *testing.T) {
	// A precommit slot (raw_locns[1]) with a higher seqno must be ignored by a
	// normal read: only the committed slot0 is used (format-text.c
	// _read_metadata_location_vg). Recovery is out of scope for this probe.
	committed := strings.Replace(testVGMeta, "seqno = 2", "seqno = 2", 1)
	precommitted := strings.Replace(testVGMeta, "seqno = 2", "seqno = 9", 1)
	img := buildImageTwoSlots(committed, precommitted)
	pv := ReadPV(bytes.NewReader(img), uint64(len(img)), "vdb")
	if pv.Status != StatusOK {
		t.Fatalf("status = %s", pv.Status)
	}
	root, err := parseText(pv.Text)
	if err != nil {
		t.Fatal(err)
	}
	if s := seq(findVGNode(root)); s != 2 {
		t.Fatalf("selected seqno = %d, want 2 (committed slot0, not precommit)", s)
	}
}

func TestAssembleMalformedAndUnsupported(t *testing.T) {
	// A VG with both a malformed dev_size and an unsupported segment must keep
	// both conditions: State is the dominant one, but Notes accumulate all.
	text := `vg1 {
    id = "aaaaaaaa-bbbb-cccc-dddd-eeeeffff0001"
    seqno = 1
    extent_size = 8192
    physical_volumes {
        pv0 {
            id = "11111111-2222-3333-4444-555555555555"
            device = "/dev/vdb"
            dev_size = 36028797018963969
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
                type = "vdo"
            }
        }
    }
}`
	pv := PVRead{Device: "vdb", Status: StatusOK, Text: []byte(text), Label: &Label{PVUUID: "11111111222233334444555555555555"}}
	inv := Assemble([]PVRead{pv})
	if len(inv.VGs) != 1 {
		t.Fatalf("vgs = %+v", inv.VGs)
	}
	vg := inv.VGs[0]
	if vg.State != StateUnsupported {
		t.Fatalf("state = %s, want %s (dominant)", vg.State, StateUnsupported)
	}
	hasMalformed, hasUnsupported := false, false
	for _, n := range vg.Notes {
		if strings.Contains(n, "malformed size") {
			hasMalformed = true
		}
		if strings.Contains(n, "unsupported segment") {
			hasUnsupported = true
		}
	}
	if !hasMalformed || !hasUnsupported {
		t.Fatalf("notes = %v, want both malformed size and unsupported segment", vg.Notes)
	}
}

func TestMalformedRequiredScalars(t *testing.T) {
	// A CRC-valid but malformed required scalar (missing or non-numeric) must
	// mark the VG/PV malformed and partial, never a silent zero + complete.
	tests := []struct {
		name      string
		old, repl string
		vgLevel   bool
	}{
		{"seqno-invalid", "seqno = 2", "seqno = xyz", true},
		{"extent_size-invalid", "extent_size = 8192", "extent_size = xyz", true},
		{"pe_start-invalid", "pe_start = 2048", "pe_start = xyz", false},
		{"pe_count-invalid", "pe_count = 1024", "pe_count = xyz", false},
		{"seqno-missing", "    seqno = 2\n", "", true},
		{"extent_size-missing", "    extent_size = 8192\n", "", true},
		{"pe_start-missing", "            pe_start = 2048\n", "", false},
		{"pe_count-missing", "            pe_count = 1024\n", "", false},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			text := strings.Replace(testVGMeta, tc.old, tc.repl, 1)
			root, err := parseText([]byte(text))
			if err != nil {
				t.Fatalf("parse text: %v", err)
			}
			vg, err := ParseVG("vg1", findVGNode(root))
			if err != nil {
				t.Fatalf("ParseVG: %v", err)
			}
			if tc.vgLevel {
				if !vg.Malformed {
					t.Fatalf("VG.Malformed = false for %q", tc.name)
				}
			} else if len(vg.PVs) != 1 || !vg.PVs[0].Malformed {
				t.Fatalf("pv Malformed = false: %+v", vg.PVs)
			}
			pv := PVRead{Device: "vdb", Status: StatusOK, Text: []byte(text), Label: &Label{PVUUID: testPVUUID}}
			inv := Assemble([]PVRead{pv})
			if len(inv.VGs) != 1 || inv.VGs[0].State != StateMalformed {
				t.Fatalf("assemble state = %+v, want partial-malformed", inv.VGs)
			}
		})
	}
}

func malformedSegmentVGText(lvBody string) string {
	return `contents = "Text Format Volume Group"
version = 1
vg1 {
    id = "aaaaaaaa-bbbb-cccc-dddd-eeeeffff0001"
    seqno = 1
    extent_size = 8192
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
` + lvBody + `
        }
    }
}`
}

func TestMalformedSegmentStructure(t *testing.T) {
	// A gap in segment numbering, a missing/invalid segment_count, or a
	// missing/non-numeric start_extent/extent_count must mark the LV/VG
	// malformed, never truncate silently and stay complete.
	tests := []struct{ name, lvBody string }{
		{"gap", `id = "99999999-8888-7777-6666-5555-444444444444" segment_count = 2
            segment1 { start_extent = 0 extent_count = 1 type = "striped" stripe_count = 1 stripes = ["pv0", 0] }
            segment3 { start_extent = 1 extent_count = 1 type = "striped" stripe_count = 1 stripes = ["pv0", 0] }`},
		{"segment_count-missing", `id = "99999999-8888-7777-6666-5555-444444444444"
            segment1 { start_extent = 0 extent_count = 1 type = "striped" stripe_count = 1 stripes = ["pv0", 0] }`},
		{"segment_count-invalid", `id = "99999999-8888-7777-6666-5555-444444444444" segment_count = xyz
            segment1 { start_extent = 0 extent_count = 1 type = "striped" stripe_count = 1 stripes = ["pv0", 0] }`},
		{"start_extent-invalid", `id = "99999999-8888-7777-6666-5555-444444444444" segment_count = 1
            segment1 { start_extent = xyz extent_count = 1 type = "striped" stripe_count = 1 stripes = ["pv0", 0] }`},
		{"extent_count-invalid", `id = "99999999-8888-7777-6666-5555-444444444444" segment_count = 1
            segment1 { start_extent = 0 extent_count = xyz type = "striped" stripe_count = 1 stripes = ["pv0", 0] }`},
		{"start_extent-missing", `id = "99999999-8888-7777-6666-5555-444444444444" segment_count = 1
            segment1 { extent_count = 1 type = "striped" stripe_count = 1 stripes = ["pv0", 0] }`},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			text := malformedSegmentVGText(tc.lvBody)
			root, err := parseText([]byte(text))
			if err != nil {
				t.Fatalf("parse text: %v", err)
			}
			vg, err := ParseVG("vg1", findVGNode(root))
			if err != nil {
				t.Fatalf("ParseVG: %v", err)
			}
			if len(vg.LVs) != 1 || !vg.LVs[0].Malformed {
				t.Fatalf("lv not malformed: %+v", vg.LVs)
			}
			pv := PVRead{Device: "vdb", Status: StatusOK, Text: []byte(text), Label: &Label{PVUUID: testPVUUID}}
			inv := Assemble([]PVRead{pv})
			if len(inv.VGs) != 1 || inv.VGs[0].State != StateMalformed {
				t.Fatalf("assemble state = %+v, want partial-malformed", inv.VGs)
			}
		})
	}
}

func TestAssembleDeviceNumber(t *testing.T) {
	// The probed device's major:minor is threaded from PVRead to the matched
	// VGPV so the graph can nominate a candidate block device.
	img := buildImage(testVGMeta, nil)
	pv := ReadPV(bytes.NewReader(img), uint64(len(img)), "vdb")
	pv.DeviceNumber = "8:0"
	inv := Assemble([]PVRead{pv})
	if len(inv.VGs) != 1 || len(inv.VGs[0].PVs) != 1 {
		t.Fatalf("vgs = %+v", inv.VGs)
	}
	if inv.VGs[0].PVs[0].DeviceNumber != "8:0" {
		t.Fatalf("device number not threaded: %+v", inv.VGs[0].PVs[0])
	}
}
