// SPDX-FileCopyrightText: 2026 The Homelab Control Authors
//
// SPDX-License-Identifier: GPL-3.0-or-later

import { describe, expect, it } from 'vitest';
import {
  childPath,
  cleanPath,
  displayName,
  filterByName,
  locationSearch,
  parentPath,
  parseMode,
  readLocation,
} from './filesModel';

// Expected strings were produced by Go's os.FileMode.String() for modes built
// the way backend/internal/filesinfo.unixMode builds them (go1.26.10).
const goModes: [string, string, string, string][] = [
  ['-rw-r--r--', 'file', '0644', 'rw-r--r--'],
  ['drwxr-xr-x', 'directory', '0755', 'rwxr-xr-x'],
  ['Lrwxrwxrwx', 'symlink', '0777', 'rwxrwxrwx'],
  ['Drw-rw----', 'block-device', '0660', 'rw-rw----'],
  ['Dcrw--w----', 'char-device', '0620', 'rw--w----'],
  ['prw-------', 'pipe', '0600', 'rw-------'],
  ['Srwxr-xr-x', 'socket', '0755', 'rwxr-xr-x'],
  ['urwxr-xr-x', 'file', '4755', 'rwxr-xr-x'],
  ['dgrwxrwxr-x', 'directory', '2775', 'rwxrwxr-x'],
  ['dtrwxrwxrwx', 'directory', '1777', 'rwxrwxrwx'],
  ['ugtrwxrwxrwx', 'file', '7777', 'rwxrwxrwx'],
  ['----------', 'file', '0000', '---------'],
];

describe('parseMode', () => {
  it.each(goModes)('%s', (mode, kind, octal, perms) => {
    const m = parseMode(mode);
    expect(m).not.toBeNull();
    expect(m?.kind).toBe(kind);
    expect(m?.octal).toBe(octal);
    expect(m?.perms).toBe(perms);
  });

  it('flags special bits', () => {
    expect(parseMode('ugtrwxrwxrwx')).toMatchObject({ setuid: true, setgid: true, sticky: true });
    expect(parseMode('-rw-r--r--')).toMatchObject({ setuid: false, setgid: false, sticky: false });
  });

  it('returns null for text that is not a Go mode string instead of guessing', () => {
    for (const bad of [
      '',
      'rwxr-xr-x',
      'xrwxr-xr-x',
      '-rw-r--r-',
      '-rw-r--r--x',
      'drwxrwxrwz',
      '0755',
      '<b>rwxrwxrwx',
    ]) {
      expect(parseMode(bad), bad).toBeNull();
    }
  });
});

describe('paths and location', () => {
  it('computes parents and children of relative paths', () => {
    expect(parentPath('')).toBeNull();
    expect(parentPath('sub')).toBe('');
    expect(parentPath('a/b/c')).toBe('a/b');
    expect(childPath('', 'x')).toBe('x');
    expect(childPath('a/b', 'x')).toBe('a/b/x');
    expect(cleanPath('.')).toBe('');
    expect(cleanPath('sub')).toBe('sub');
  });

  it('round-trips root and path through the query string', () => {
    expect(readLocation('')).toBeNull();
    expect(readLocation('?path=sub')).toBeNull();
    expect(readLocation(locationSearch({ root: 'lab', path: 'a/b c' }))).toEqual({
      root: 'lab',
      path: 'a/b c',
    });
    expect(locationSearch({ root: 'lab', path: '' })).toBe('?root=lab');
    expect(readLocation('?root=lab&path=.')).toEqual({ root: 'lab', path: '' });
  });

  it('filters names case-insensitively without mutating the input', () => {
    const rows = [{ name: 'README.txt' }, { name: 'sub' }, { name: 'notes.md' }];
    expect(filterByName(rows, 'read').map((r) => r.name)).toEqual(['README.txt']);
    expect(filterByName(rows, '  ').length).toBe(3);
    expect(filterByName(rows, 'zzz')).toEqual([]);
    expect(rows.length).toBe(3);
  });
});

describe('displayName', () => {
  it('leaves ordinary text untouched', () => {
    expect(displayName('readme.txt')).toBe('readme.txt');
    expect(displayName('résumé.pdf')).toBe('résumé.pdf');
    expect(displayName('a b/c')).toBe('a b/c');
  });

  it('escapes bidi, control and zero-width characters to visible escapes', () => {
    expect(displayName('a\u202eb')).toBe('a\\u202Eb'); // U+202E RIGHT-TO-LEFT OVERRIDE
    expect(displayName('a\u200bb')).toBe('a\\u200Bb'); // U+200B ZERO WIDTH SPACE
    expect(displayName('a\u0007b')).toBe('a\\u0007b'); // U+0007 BEL
    expect(displayName('a\u00adb')).toBe('a\\u00ADb'); // U+00AD SOFT HYPHEN
    expect(displayName('a\u2066b')).toBe('a\\u2066b'); // U+2066 LEFT-TO-RIGHT ISOLATE
  });
});
