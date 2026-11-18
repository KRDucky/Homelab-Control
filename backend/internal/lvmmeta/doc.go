// SPDX-FileCopyrightText: 2026 The Homelab Control Authors
//
// SPDX-License-Identifier: GPL-3.0-or-later

// Package lvmmeta parses the LVM2 on-disk metadata format. The byte layouts
// and CRC are grounded in the upstream LVM2 sources (GPL-2.0 tools / LGPL-2.1
// libraries), referenced for format facts only:
//
//	lib/label/label.h           — label_header, LABEL_ID, LABEL_SIZE
//	lib/format_text/layout.h    — pv_header, disk_locn, raw_locn, mda_header,
//	                              FMTT_MAGIC, MDA_HEADER_SIZE, RAW_LOCN_IGNORED
//	lib/misc/crc.c / crc.h      — reflected CRC-32, INITIAL_CRC 0xf597a6cf
//	lib/format_text/format-text.c — MDA text extraction and circular wrap
//
// No LVM2 source is copied; the struct layouts are on-disk format facts.
//
// The Go library github.com/masahiro331/go-lvm (MIT, commit
// 5e2f66cf522dfa14548900250d750fa707ff3191) was evaluated and NOT reused: it
// performs no CRC validation, has unbounded descriptor loops, ignores Seek
// errors, depends on participle v2.0.0-beta.5, and its ParseMainSection
// dereferences unvalidated union pointers. Its type layer is weaker than the
// typed invariants required here (Segment stores raw []string stripes with no
// Parsed/Unsupported classification), so adopting it would lose required
// invariants rather than add them.
package lvmmeta
