// SPDX-FileCopyrightText: 2026 The Homelab Control Authors
//
// SPDX-License-Identifier: GPL-3.0-or-later

package lvmmeta

import (
	"fmt"
	"io"
)

// Compile-time limits. Aggregate limits are enforced in Assemble.
const (
	SectorSize        = 512
	LabelScanSectors  = 4
	IDLen             = 32
	MaxDataAreas      = 8
	MaxMetadataAreas  = 2
	MaxRawLocns       = 4
	MDAHeaderSize     = 512
	MaxMetadataText   = 4 << 20
	MaxTotalText      = 16 << 20
	MaxDepth          = 8
	MaxTokens         = 2_000_000
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
	StatusNotLVM           PVStatus = "not-lvm"
	StatusOK               PVStatus = "ok"
	StatusAmbiguousLabel   PVStatus = "ambiguous-label"
	StatusLabelBadCRC      PVStatus = "label-bad-crc"
	StatusUnsupportedType  PVStatus = "unsupported-label-type"
	StatusPVHeaderOverrun  PVStatus = "pv-header-overrun"
	StatusReadError        PVStatus = "read-error"
	StatusMDABadMagic      PVStatus = "mda-bad-magic"
	StatusMDABadCRC        PVStatus = "mda-bad-crc"
	StatusMDAOutOfRange    PVStatus = "mda-out-of-range"
	StatusMetadataIgnored  PVStatus = "metadata-ignored"
	StatusMetadataTooLarge PVStatus = "metadata-too-large"
	StatusMetadataBadCRC   PVStatus = "metadata-bad-crc"
	StatusMetadataChanging PVStatus = "metadata-changing"
	StatusTextParseError   PVStatus = "text-parse-error"
	StatusLimitExceeded    PVStatus = "limit-exceeded"
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
	Text     []byte
}

// PVRead is the result of reading one device.
type PVRead struct {
	Device string
	Status PVStatus
	Label  *Label
	Copies []MDACopy
	Text   []byte // chosen copy; never exported to clients
}

// readAt is a bounded readerAt wrapper that fails on any read past size.
type readAt struct {
	r    io.ReaderAt
	size uint64
}

func (a readAt) ReadAt(p []byte, off int64) (int, error) {
	if off < 0 || uint64(off)+uint64(len(p)) > a.size {
		return 0, fmt.Errorf("read past end of device")
	}
	return a.r.ReadAt(p, off)
}

// ReadPV reads the label, MDA headers and metadata text from r within
// [0,size). It never writes or seeks. device is the kernel name of the probed
// fd (identity only).
func ReadPV(r io.ReaderAt, size uint64, device string) PVRead {
	rd := readAt{r: r, size: size}
	res := PVRead{Device: device, Status: StatusNotLVM}

	// Label is in sectors 0..3.
	var found *Label
	for s := uint64(0); s < LabelScanSectors && s*SectorSize+labelSize() <= size; s++ {
		sect := make([]byte, SectorSize)
		if _, err := rd.ReadAt(sect, int64(s*SectorSize)); err != nil {
			res.Status = StatusReadError
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

	// Read metadata areas and extract the first valid metadata text copy.
	for i, mda := range found.MDAs {
		if i >= MaxMetadataAreas {
			break
		}
		copies, st := readMDA(rd, mda)
		res.Copies = append(res.Copies, copies...)
		if st != StatusOK {
			res.Status = st
			return res
		}
		for j := range res.Copies {
			c := &res.Copies[j]
			if c.Ignored {
				c.Status = CopyIgnored
				continue
			}
			text, tst := extractText(rd, mda.Offset, mda.Size, *c)
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
			if res.Text == nil {
				res.Text = text
			}
		}
	}
	return res
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

	// Data areas.
	for len(data) <= MaxDataAreas && pos+16 <= len(b) {
		off := leUint64(b[pos : pos+8])
		sz := leUint64(b[pos+8 : pos+16])
		pos += 16
		if off == 0 && sz == 0 {
			break
		}
		data = append(data, DiskLocn{Offset: off, Size: sz})
	}
	if len(data) > MaxDataAreas {
		return "", 0, nil, nil, 0, StatusLimitExceeded
	}
	// Metadata areas.
	for len(mdas) <= MaxMetadataAreas && pos+16 <= len(b) {
		off := leUint64(b[pos : pos+8])
		sz := leUint64(b[pos+8 : pos+16])
		pos += 16
		if off == 0 && sz == 0 {
			break
		}
		mdas = append(mdas, DiskLocn{Offset: off, Size: sz})
	}
	if len(mdas) > MaxMetadataAreas {
		return "", 0, nil, nil, 0, StatusLimitExceeded
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

// readMDA reads the MDA header and returns the raw_locn copies (text not yet
// extracted). mda is a disk_locn pointing at the MDA header.
func readMDA(rd readAt, mda DiskLocn) ([]MDACopy, PVStatus) {
	hdr := make([]byte, MDAHeaderSize)
	if _, err := rd.ReadAt(hdr, int64(mda.Offset)); err != nil {
		return nil, StatusMDAOutOfRange
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
	var copies []MDACopy
	pos := 40
	for len(copies) < MaxRawLocns && pos+24 <= len(hdr) {
		off := leUint64(hdr[pos : pos+8])
		sz := leUint64(hdr[pos+8 : pos+16])
		csum := leUint32(hdr[pos+16 : pos+20])
		flags := leUint32(hdr[pos+20 : pos+24])
		pos += 24
		if off == 0 && sz == 0 && csum == 0 && flags == 0 {
			break
		}
		copies = append(copies, MDACopy{
			Index:    len(copies),
			Start:    start,
			Size:     size,
			Offset:   off,
			TextSize: sz,
			TextCRC:  csum,
			Ignored:  flags&1 != 0, // RAW_LOCN_IGNORED
		})
	}
	if len(copies) == 0 {
		return nil, StatusMetadataIgnored
	}
	return copies, StatusOK
}

// extractText reads one raw_locn copy's metadata text from the circular MDA
// area, handling wrap, and validates its checksum (format-text.c _vg_read_raw).
func extractText(rd readAt, mdaStart, mdaSize uint64, copy MDACopy) ([]byte, PVStatus) {
	if copy.Offset >= mdaSize || copy.TextSize > mdaSize-MDAHeaderSize || copy.TextSize > MaxMetadataText {
		return nil, StatusMetadataTooLarge
	}
	var wrap uint64
	if copy.Offset+copy.TextSize > mdaSize {
		wrap = copy.Offset + copy.TextSize - mdaSize
	}
	head := copy.TextSize - wrap
	out := make([]byte, 0, copy.TextSize)
	if head > 0 {
		b := make([]byte, head)
		if _, err := rd.ReadAt(b, int64(mdaStart+copy.Offset)); err != nil {
			return nil, StatusReadError
		}
		out = append(out, b...)
	}
	if wrap > 0 {
		b := make([]byte, wrap)
		if _, err := rd.ReadAt(b, int64(mdaStart+MDAHeaderSize)); err != nil {
			return nil, StatusReadError
		}
		out = append(out, b...)
	}
	if crc(initCRC, out) != copy.TextCRC {
		return nil, StatusMetadataBadCRC
	}
	return out, StatusOK
}

// uuidString validates IDLen bytes as an undashed hex UUID and lowercases it.
func uuidString(b []byte) string {
	if len(b) != IDLen {
		return ""
	}
	out := make([]byte, len(b))
	for i, c := range b {
		switch {
		case c >= '0' && c <= '9' || c >= 'a' && c <= 'f':
			out[i] = c
		case c >= 'A' && c <= 'F':
			out[i] = c + ('a' - 'A')
		default:
			return ""
		}
	}
	return string(out)
}
