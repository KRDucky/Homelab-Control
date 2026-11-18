// SPDX-FileCopyrightText: 2026 The Homelab Control Authors
//
// SPDX-License-Identifier: GPL-3.0-or-later

// Pure helpers for the read-only Files page. Nothing here reads the host or
// decides what is allowed: the server enforces the confined roots (ADR 0012).
// These helpers only shape what the server already returned.

export type EntryKind =
  'directory' | 'file' | 'symlink' | 'block-device' | 'char-device' | 'pipe' | 'socket' | 'other';

export interface ModeInfo {
  kind: EntryKind;
  /** Four-digit octal permission bits including setuid/setgid/sticky, e.g. "0755", "4755". */
  octal: string;
  /** The nine rwx characters, e.g. "rwxr-xr-x". */
  perms: string;
  setuid: boolean;
  setgid: boolean;
  sticky: boolean;
}

export const KIND_LABEL: Record<EntryKind, string> = {
  directory: 'Folder',
  file: 'File',
  symlink: 'Symbolic link (not followed)',
  'block-device': 'Block device',
  'char-device': 'Character device',
  pipe: 'Named pipe',
  socket: 'Socket',
  other: 'Other',
};

const PERMS = /^[r-][w-][x-][r-][w-][x-][r-][w-][x-]$/;

/**
 * Parses Go's os.FileMode.String() as returned by the API (backend
 * filesinfo.unixMode): optional type/attribute letters from "dalTLDpSugct?"
 * followed by nine rwx characters. Returns null when the text does not have
 * that shape, so the UI shows the raw value instead of a guess.
 */
export function parseMode(mode: string): ModeInfo | null {
  if (mode.length < 10) return null;
  const perms = mode.slice(-9);
  if (!PERMS.test(perms)) return null;
  const prefix = mode.slice(0, -9);
  // Go prints "-" when no type or attribute letter applies.
  const letters = prefix === '-' ? '' : prefix;
  if (!/^[dalTLDpSugct?]*$/.test(letters)) return null;
  const has = (c: string) => letters.includes(c);
  let kind: EntryKind = 'file';
  if (has('d')) kind = 'directory';
  else if (has('L')) kind = 'symlink';
  else if (has('D') && has('c')) kind = 'char-device';
  else if (has('D')) kind = 'block-device';
  else if (has('p')) kind = 'pipe';
  else if (has('S')) kind = 'socket';
  else if (has('?')) kind = 'other';
  let bits = 0;
  for (let i = 0; i < 9; i++) if (perms[i] !== '-') bits |= 1 << (8 - i);
  const setuid = has('u');
  const setgid = has('g');
  const sticky = has('t');
  const special = (setuid ? 4 : 0) + (setgid ? 2 : 0) + (sticky ? 1 : 0);
  const octal = `${special}${(bits >> 6) & 7}${(bits >> 3) & 7}${bits & 7}`;
  return { kind, octal, perms, setuid, setgid, sticky };
}

/** Relative path of the parent folder ("" is the root); null at the root. */
export function parentPath(path: string): string | null {
  if (path === '') return null;
  const i = path.lastIndexOf('/');
  return i < 0 ? '' : path.slice(0, i);
}

export function childPath(base: string, name: string): string {
  return base ? `${base}/${name}` : name;
}

/** The server answers the root as "."; the UI keeps it as "". */
export function cleanPath(path: string): string {
  return path === '.' ? '' : path;
}

export interface FilesLocation {
  root: string;
  path: string;
}

/**
 * Reads ?root=&path= from the URL. The values are only identifiers the
 * server validates again; a bad value produces a server error, not access.
 */
export function readLocation(search: string): FilesLocation | null {
  const q = new URLSearchParams(search);
  const root = q.get('root');
  if (!root) return null;
  return { root, path: cleanPath(q.get('path') ?? '') };
}

export function locationSearch(loc: FilesLocation): string {
  const q = new URLSearchParams({ root: loc.root });
  if (loc.path) q.set('path', loc.path);
  return `?${q.toString()}`;
}

/** Case-insensitive substring filter on entry names. */
export function filterByName<T extends { name: string }>(entries: readonly T[], query: string): T[] {
  const q = query.trim().toLowerCase();
  if (!q) return [...entries];
  return entries.filter((e) => e.name.toLowerCase().includes(q));
}

/** Escapes invisible control (Cc) and format (Cf) code points as visible
 * \uXXXX escapes, and a literal backslash as \\, so a hostile name cannot
 * spoof a different name, reorder the surrounding text, or mimic an escape. */
export function displayName(name: string): string {
  const controlOrFormat = /[\p{Cc}\p{Cf}]/u;
  let out = '';
  for (const ch of name) {
    if (ch === '\\') {
      out += '\\\\';
    } else if (controlOrFormat.test(ch)) {
      const cp = ch.codePointAt(0) ?? 0;
      out += `\\u${cp.toString(16).toUpperCase().padStart(4, '0')}`;
    } else {
      out += ch;
    }
  }
  return out;
}

/** Escapes each `/`-separated path segment for display. */
export function displayPath(path: string): string {
  return path.split('/').map(displayName).join('/');
}
