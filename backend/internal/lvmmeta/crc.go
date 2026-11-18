// SPDX-FileCopyrightText: 2026 The Homelab Control Authors
//
// SPDX-License-Identifier: GPL-3.0-or-later

// Package lvmmeta is a bounded, read-only parser for the LVM2 on-disk metadata
// format: the PV label, metadata-area (MDA) headers and the text metadata they
// wrap. It reads only through an io.ReaderAt within [0,size); it never writes,
// seeks, execs or opens anything.
//
// The byte layouts and CRC are pinned to the upstream LVM2 sources
// (lib/label/label.h, lib/format_text/layout.h, lib/misc/crc.c): a reflected
// CRC-32 (IEEE 0xedb88320 table) with initial value 0xf597a6cf and no final
// xor, the label CRC covers sector bytes 20..512, and the MDA-header CRC covers
// bytes 4..512. Everything else is rejected rather than guessed.
package lvmmeta

import "encoding/binary"

// initCRC is the LVM2 CRC-32 initial value (lib/misc/crc.h INITIAL_CRC).
const initCRC uint32 = 0xf597a6cf

// crcTable is the reflected CRC-32 (IEEE 802.3, polynomial 0xedb88320) table,
// built once at package init.
var crcTable [256]uint32

func init() {
	for i := uint32(0); i < 256; i++ {
		c := i
		for j := 0; j < 8; j++ {
			if c&1 != 0 {
				c = 0xedb88320 ^ (c >> 1)
			} else {
				c >>= 1
			}
		}
		crcTable[i] = c
	}
}

// crc computes the LVM2 checksum over data: crc = table[(crc^b)&0xff]^(crc>>8),
// starting from initCRC and returning without a final xor.
func crc(initial uint32, data []byte) uint32 {
	c := initial
	for _, b := range data {
		c = crcTable[byte(c)^b] ^ (c >> 8)
	}
	return c
}

func leUint64(b []byte) uint64 { return binary.LittleEndian.Uint64(b) }
func leUint32(b []byte) uint32 { return binary.LittleEndian.Uint32(b) }
