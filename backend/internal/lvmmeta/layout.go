// SPDX-FileCopyrightText: 2026 The Homelab Control Authors
//
// SPDX-License-Identifier: GPL-3.0-or-later

package lvmmeta

import (
	"bytes"
	"context"
	"fmt"
	"io"
)

// Compile-time limits. The per-copy text cap is enforced in ReadPV/ReadPVBudget;
// the aggregate MaxTotalText budget is enforced in ReadPVBudget (and defensively
// in Assemble).
const (
	SectorSize       = 512
	LabelScanSectors = 4
	IDLen            = 32
	MaxDataAreas     = 8
	MaxMetadataAreas = 2
	MDAHeaderSize    = 512
	MaxMetadataText  = 4 << 20
	// MaxDeviceText caps the metadata text a single device can retain,
	// independent of the shared MaxTotalText budget across devices.
	MaxDeviceText = 4 << 20
	MaxTotalText  = 16 << 20
	MaxDepth      = 8
	// MaxTokens bounds the tokenizer's slice: at ~32 bytes/token (plus string
	// data and the AST), 200k tokens stay well under the unit's 64 MiB
	// MemoryMax, so a dense hostile text fails closed with a typed error rather
	// than OOM.
	MaxTokens         = 200_000
	MaxString         = 4096
	MaxArray          = 65536
	MaxVGs            = 64
	MaxPVsPerVG       = 1024
	MaxLVsPerVG       = 4096
	MaxSegmentsPerLV  = 1024
	MaxDataAreasTotal = 32
)

var (
	labelID   = []byte("LABELONE")
	lvm2Label = []byte("LVM2 001")
	// FMTT_MAGIC from lib/format_text/layout.h.
	ftMagic = []byte{0x20, 0x4c, 0x56, 0x4d, 0x32, 0x20, 0x78, 0x5b, 0x35, 0x41, 0x25, 0x72, 0x30, 0x4e, 0x2a, 0x3e}
)

// DiskLocn is the on-disk data/metadata area location.
type DiskLocn struct {
	Offset uint64
	Size   uint64
}

// Label is the parsed PV label header.
type Label struct {
	Sector     uint64
	PVUUID     string // 32 hex chars, undashed
	DevSize    uint64
	Data       []DiskLocn
	MDAs       []DiskLocn
	ExtVersion uint32
}

// PVStatus is a closed per-device status.
type PVStatus string

const (
	StatusNotLVM               PVStatus = "not-lvm"
	StatusOK                   PVStatus = "ok"
	StatusAmbiguousLabel       PVStatus = "ambiguous-label"
	StatusLabelBadCRC          PVStatus = "label-bad-crc"
	StatusUnsupportedType      PVStatus = "unsupported-label-type"
	StatusPVHeaderOverrun      PVStatus = "pv-header-overrun"
	StatusReadError            PVStatus = "read-error"
	StatusMDABadMagic          PVStatus = "mda-bad-magic"
	StatusMDABadCRC            PVStatus = "mda-bad-crc"
	StatusMDAOutOfRange        PVStatus = "mda-out-of-range"
	StatusMetadataIgnored      PVStatus = "metadata-ignored"
	StatusMetadataTooLarge     PVStatus = "metadata-too-large"
	StatusMetadataBadCRC       PVStatus = "metadata-bad-crc"
	StatusMetadataChanging     PVStatus = "metadata-changing"
	StatusMetadataInconsistent PVStatus = "metadata-inconsistent"
	StatusTextParseError       PVStatus = "text-parse-error"
	StatusLimitExceeded        PVStatus = "limit-exceeded"
	StatusCanceled             PVStatus = "canceled"
)

// CopyStatus describes one metadata copy.
type CopyStatus string

const (
	CopyOK       CopyStatus = "ok"
	CopyIgnored  CopyStatus = "ignored"
	CopyBadCRC   CopyStatus = "bad-crc"
	CopyChanging CopyStatus = "changing"
)

// MDACopy is one raw_locn metadata copy.
type MDACopy struct {
	Index    int
	Start    uint64 // MDA start (absolute device offset)
	Size     uint64 // MDA size (metadata area size)
	Offset   uint64 // raw_locn offset (relative to MDA start)
	TextSize uint64 // raw_locn size (metadata text bytes)
	Seqno    uint64
	TextCRC  uint32
	Ignored  bool
	Status   CopyStatus
	Text     []byte `json:"-"` // never serialized to clients
}

// PVRead is the result of reading one device.
type PVRead struct {
	Device string
	Status PVStatus
	Label  *Label
	Copies []MDACopy
	Text   []byte `json:"-"` // never serialized to clients
}

// readAt is a bounded readerAt wrapper that fails on any read past size.
type readAt struct {
	r    io.ReaderAt
	size uint64
}

func (a readAt) ReadAt(p []byte, off int64) (int, error) {
	if off < 0 {
		return 0, fmt.Errorf("read past end of device")
	}
	end, overflow := addU64(uint64(off), uint64(len(p)))
	if overflow || end > a.size {
		return 0, fmt.Errorf("read past end of device")
	}
	return a.r.ReadAt(p, off)
}

// addU64 returns a+b and whether it overflowed (C9 checked arithmetic).
func addU64(a, b uint64) (uint64, bool) {
	sum := a + b
	return sum, sum < a
}

// mulU64 returns a*b and whether it overflowed (C9 checked arithmetic).
func mulU64(a, b uint64) (uint64, bool) {
	if b != 0 && a > ^uint64(0)/b {
		return 0, false
	}
	return a * b, true
}

// ReadPV reads the label, MDA headers and metadata text from r within
// [0,size). It never writes or seeks. device is the kernel name of the probed
// fd (identity only).
// ReadPV reads the label, MDA headers and metadata text from r within
// [0,size) with no aggregate budget (the per-copy MaxMetadataText and
// per-device MaxDeviceText caps still apply). Use ReadPVBudget to bound total
// retained text across devices.
func ReadPV(r io.ReaderAt, size uint64, device string) PVRead {
	return readPV(context.Background(), r, size, device, nil)
}

// ReadPVBudget is ReadPV with a shared remaining-text budget. When budget is
// non-nil and the selected copy's text would exceed it, the result is
// StatusLimitExceeded with no retained Text and the budget is zeroed, so a
// multi-device loop never retains more than MaxTotalText across calls.
func ReadPVBudget(r io.ReaderAt, size uint64, device string, budget *int64) PVRead {
	return readPV(context.Background(), r, size, device, budget)
}

// ReadPVBudgetContext is ReadPVBudget with a context. Cancellation is checked
// before and after every bounded ReadAt and between label/MDA/copy iterations,
// so a canceled context aborts promptly with StatusCanceled. A context cannot
// interrupt a single in-flight ReadAt: a slow or hung ReaderAt still needs the
// service process timeout (or a ReaderAt that bounds its own I/O) as the hard
// stop. The per-device MaxDeviceText cap is enforced in addition to the shared
// budget; either breach yields StatusLimitExceeded.
func ReadPVBudgetContext(ctx context.Context, r io.ReaderAt, size uint64, device string, budget *int64) PVRead {
	return readPV(ctx, r, size, device, budget)
}

func readPV(ctx context.Context, r io.ReaderAt, size uint64, device string, budget *int64) PVRead {
	rd := readAt{r: r, size: size}
	res := PVRead{Device: device, Status: StatusNotLVM}

	// Label is in sectors 0..3.
	var found *Label
	for s := uint64(0); s < LabelScanSectors && s*SectorSize+labelSize() <= size; s++ {
		if canceled(ctx) {
			res.Status = StatusCanceled
			return res
		}
		sect := make([]byte, SectorSize)
		if _, err := rd.ReadAt(sect, int64(s*SectorSize)); err != nil {
			res.Status = StatusReadError
			return res
		}
		if canceled(ctx) {
			res.Status = StatusCanceled
			return res
		}
		if string(sect[0:8]) != string(labelID) {
			continue
		}
		lab, st := parseLabel(sect, s)
		if st != StatusOK {
			if found == nil {
				res.Status = st
			} else {
				res.Status = StatusAmbiguousLabel
			}
			continue
		}
		if found != nil {
			res.Status = StatusAmbiguousLabel
			return res
		}
		found = lab
	}
	if found == nil {
		return res // not-lvm
	}
	res.Label = found
	res.Status = StatusOK

	// Read metadata areas, extract every valid copy's text, and select the
	// highest-seqno copy. Prior-MDA copies must not be re-read with a later
	// MDA's start/size, so iterate only the indices appended this round.
	for i, mda := range found.MDAs {
		if i >= MaxMetadataAreas {
			break
		}
		if canceled(ctx) {
			res.Status = StatusCanceled
			return res
		}
		start := len(res.Copies)
		copies, st := readMDA(ctx, rd, mda)
		res.Copies = append(res.Copies, copies...)
		if st != StatusOK {
			res.Status = st
			return res
		}
		for j := start; j < len(res.Copies); j++ {
			if canceled(ctx) {
				res.Status = StatusCanceled
				return res
			}
			c := &res.Copies[j]
			if c.Ignored {
				c.Status = CopyIgnored
				continue
			}
			text, tst := extractText(ctx, rd, mda.Offset, mda.Size, *c)
			if tst == StatusMetadataBadCRC {
				c.Status = CopyBadCRC
				continue
			}
			if tst != StatusOK {
				res.Status = tst
				return res
			}
			c.Text = text
			c.Status = CopyOK
		}
	}

	if canceled(ctx) {
		res.Status = StatusCanceled
		return res
	}
	if len(res.Copies) == 0 {
		// A PV label with no metadata area (e.g. pvcreate before vgcreate) is a
		// valid, complete read — there is no VG metadata to extract.
		return res
	}
	res.Text, res.Status = selectCopy(res.Copies)
	// Drop every copy's raw text now that the best was chosen; only res.Text is
	// retained, so no unbounded aggregate of per-copy texts is kept.
	for i := range res.Copies {
		res.Copies[i].Text = nil
	}
	if res.Status != StatusOK {
		return res
	}
	res.Status = enforceTextCaps(&res, budget)
	return res
}

// enforceTextCaps applies the per-device MaxDeviceText cap and, when budget is
// non-nil, the shared remaining-text budget to res.Text. Either breach returns
// StatusLimitExceeded with no retained Text. A per-device breach leaves the
// shared budget untouched; a shared-budget breach zeroes it.
func enforceTextCaps(res *PVRead, budget *int64) PVStatus {
	if res.Text == nil {
		return StatusOK
	}
	if int64(len(res.Text)) > MaxDeviceText {
		res.Text = nil
		return StatusLimitExceeded
	}
	if budget != nil {
		if int64(len(res.Text)) > *budget {
			res.Text = nil
			*budget = 0
			return StatusLimitExceeded
		}
		*budget -= int64(len(res.Text))
	}
	return StatusOK
}

// canceled reports whether ctx is done. ctx is never nil (callers pass a
// Background or request context); the nil check is defensive.
func canceled(ctx context.Context) bool {
	return ctx != nil && ctx.Err() != nil
}

// selectCopy picks the highest-seqno valid copy; equal-seqno copies with
// different content mark the device metadata-inconsistent. Content is compared
// byte-for-byte (never by CRC, which can collide).
func selectCopy(copies []MDACopy) ([]byte, PVStatus) {
	var bestText []byte
	bestSeq := uint64(0)
	inconsistent := false
	for i := range copies {
		c := &copies[i]
		if c.Status != CopyOK || c.Text == nil {
			continue
		}
		root, err := parseText(c.Text)
		if err != nil {
			continue
		}
		s := seq(findVGNode(root))
		c.Seqno = s
		if bestText == nil || s > bestSeq {
			bestSeq = s
			bestText = c.Text
			inconsistent = false
		} else if s == bestSeq && !bytes.Equal(c.Text, bestText) {
			inconsistent = true
		}
	}
	if bestText == nil {
		return nil, StatusMetadataIgnored
	}
	if inconsistent {
		return bestText, StatusMetadataInconsistent
	}
	return bestText, StatusOK
}

func labelSize() uint64 { return SectorSize }

// parseLabel validates one 512-byte label sector and returns its header.
func parseLabel(sect []byte, sector uint64) (*Label, PVStatus) {
	if len(sect) < SectorSize {
		return nil, StatusReadError
	}
	if string(sect[24:32]) != string(lvm2Label) {
		return nil, StatusUnsupportedType
	}
	if leUint64(sect[8:16]) != sector {
		return nil, StatusLabelBadCRC // sector_xl mismatch
	}
	offset := uint64(leUint32(sect[20:24]))
	if offset < 32 || offset > 480 {
		return nil, StatusPVHeaderOverrun
	}
	if crc(initCRC, sect[20:512]) != leUint32(sect[16:20]) {
		return nil, StatusLabelBadCRC
	}
	// pv_header at offset.
	uuid, devSize, data, mdas, ext, st := parsePVHeader(sect[offset:], sector*SectorSize)
	if st != StatusOK {
		return nil, st
	}
	return &Label{Sector: sector, PVUUID: uuid, DevSize: devSize, Data: data, MDAs: mdas, ExtVersion: ext}, StatusOK
}

// parsePVHeader parses the pv_header: uuid[32], device_size, then two
// NULL-terminated disk_locn lists (data areas then metadata areas).
func parsePVHeader(b []byte, mdaBase uint64) (uuid string, devSize uint64, data, mdas []DiskLocn, extVer uint32, st PVStatus) {
	if len(b) < IDLen+8 {
		return "", 0, nil, nil, 0, StatusPVHeaderOverrun
	}
	uuid = uuidString(b[0:IDLen])
	if uuid == "" {
		return "", 0, nil, nil, 0, StatusPVHeaderOverrun
	}
	devSize = leUint64(b[IDLen : IDLen+8])
	pos := IDLen + 8

	// Data areas: must be NULL-terminated; a list that runs off the 512-byte
	// sector without a zero disk_locn is malformed (C8).
	term := false
	for len(data) <= MaxDataAreas && pos+16 <= len(b) {
		off := leUint64(b[pos : pos+8])
		sz := leUint64(b[pos+8 : pos+16])
		pos += 16
		if off == 0 && sz == 0 {
			term = true
			break
		}
		data = append(data, DiskLocn{Offset: off, Size: sz})
	}
	if !term || len(data) > MaxDataAreas {
		return "", 0, nil, nil, 0, StatusPVHeaderOverrun
	}
	// Metadata areas: same NULL-termination requirement.
	term = false
	for len(mdas) <= MaxMetadataAreas && pos+16 <= len(b) {
		off := leUint64(b[pos : pos+8])
		sz := leUint64(b[pos+8 : pos+16])
		pos += 16
		if off == 0 && sz == 0 {
			term = true
			break
		}
		mdas = append(mdas, DiskLocn{Offset: off, Size: sz})
	}
	if !term || len(mdas) > MaxMetadataAreas {
		return "", 0, nil, nil, 0, StatusPVHeaderOverrun
	}
	// Optional pv_header_extension: version + flags (8 bytes) if present.
	if pos+8 <= len(b) {
		ver := leUint32(b[pos : pos+4])
		if ver > 0 && ver <= 2 {
			extVer = ver
		}
	}
	return uuid, devSize, data, mdas, extVer, StatusOK
}

// readMDA reads the MDA header and returns the committed raw_locn copy (text
// not yet extracted). mda is a disk_locn pointing at the MDA header.
//
// A normal (non-recovery) read uses only raw_locns[0], the committed slot.
// raw_locns[1] is the precommit slot written mid-transaction by vg_precommit
// and is consulted only under an explicit recovery policy
// (format-text.c _read_metadata_location_vg / _vg_commit_raw_rlocn); this
// read-only probe never enables recovery, so it is ignored.
func readMDA(ctx context.Context, rd readAt, mda DiskLocn) ([]MDACopy, PVStatus) {
	if canceled(ctx) {
		return nil, StatusCanceled
	}
	hdr := make([]byte, MDAHeaderSize)
	if _, err := rd.ReadAt(hdr, int64(mda.Offset)); err != nil {
		return nil, StatusMDAOutOfRange
	}
	if canceled(ctx) {
		return nil, StatusCanceled
	}
	if string(hdr[4:20]) != string(ftMagic) {
		return nil, StatusMDABadMagic
	}
	if leUint32(hdr[20:24]) != 1 { // version
		return nil, StatusMDABadMagic
	}
	if crc(initCRC, hdr[4:512]) != leUint32(hdr[0:4]) {
		return nil, StatusMDABadCRC
	}
	start := leUint64(hdr[24:32])
	size := leUint64(hdr[32:40])
	if start != mda.Offset || size != mda.Size || size < MDAHeaderSize {
		return nil, StatusMDAOutOfRange
	}
	// Slot 0 (committed) raw_locn at offset 40.
	off := leUint64(hdr[40:48])
	sz := leUint64(hdr[48:56])
	csum := leUint32(hdr[56:60])
	flags := leUint32(hdr[60:64])
	if off == 0 && sz == 0 && csum == 0 && flags == 0 {
		// No committed raw_locn copy (e.g. a freshly pvcreate'd PV) is a valid,
		// empty metadata area, not an error.
		return nil, StatusOK
	}
	return []MDACopy{{
		Index:    0,
		Start:    start,
		Size:     size,
		Offset:   off,
		TextSize: sz,
		TextCRC:  csum,
		Ignored:  flags&1 != 0, // RAW_LOCN_IGNORED
	}}, StatusOK
}

// extractText reads one raw_locn copy's metadata text from the circular MDA
// area, handling wrap, and validates its checksum (format-text.c _vg_read_raw).
func extractText(ctx context.Context, rd readAt, mdaStart, mdaSize uint64, copy MDACopy) ([]byte, PVStatus) {
	if canceled(ctx) {
		return nil, StatusCanceled
	}
	// Bounds are overflow-checked (C9).
	if copy.Offset >= mdaSize || mdaSize < MDAHeaderSize {
		return nil, StatusMetadataTooLarge
	}
	avail := mdaSize - MDAHeaderSize
	if copy.TextSize > avail || copy.TextSize > MaxMetadataText {
		return nil, StatusMetadataTooLarge
	}
	end, overflow := addU64(copy.Offset, copy.TextSize)
	if overflow {
		return nil, StatusMetadataTooLarge
	}
	var wrap uint64
	if end > mdaSize {
		wrap = end - mdaSize
	}
	head := copy.TextSize - wrap
	out := make([]byte, 0, copy.TextSize)
	if head > 0 {
		b := make([]byte, head)
		start, overflow := addU64(mdaStart, copy.Offset)
		if overflow {
			return nil, StatusMDAOutOfRange
		}
		if _, err := rd.ReadAt(b, int64(start)); err != nil {
			return nil, StatusReadError
		}
		if canceled(ctx) {
			return nil, StatusCanceled
		}
		out = append(out, b...)
	}
	if wrap > 0 {
		b := make([]byte, wrap)
		start, overflow := addU64(mdaStart, MDAHeaderSize)
		if overflow {
			return nil, StatusMDAOutOfRange
		}
		if _, err := rd.ReadAt(b, int64(start)); err != nil {
			return nil, StatusReadError
		}
		if canceled(ctx) {
			return nil, StatusCanceled
		}
		out = append(out, b...)
	}
	if crc(initCRC, out) != copy.TextCRC {
		return nil, StatusMetadataBadCRC
	}
	return out, StatusOK
}

// uuidString validates IDLen bytes as an LVM2 UUID and returns the exact value
// (preserving case). The LVM2 alphabet (lib/id/id.c _c) is alphanumeric plus
// '!' and '#', not hex.
func uuidString(b []byte) string {
	if len(b) != IDLen {
		return ""
	}
	for _, c := range b {
		if !isIDChar(c) {
			return ""
		}
	}
	return string(b)
}

func isIDChar(c byte) bool {
	return c >= '0' && c <= '9' || c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z' || c == '!' || c == '#'
}
