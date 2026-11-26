// SPDX-FileCopyrightText: 2026 The Homelab Control Authors
//
// SPDX-License-Identifier: GPL-3.0-or-later

import type { ReactNode } from 'react';
import type { SkippedMount } from './api';

/**
 * The "Filesystems" panel shared by the ext4, XFS and f2fs pages: one card per
 * filesystem a standard tool (or the kernel) reported, the reason when the
 * report is unavailable, and each mount the helper skipped with its reason.
 */
export function FilesystemReport<T>({
  id,
  available,
  reason,
  filesystems,
  skipped,
  truncated,
  hidden,
  empty,
  children,
}: {
  id: string;
  available: boolean;
  reason?: string | undefined;
  filesystems: T[];
  skipped?: SkippedMount[] | undefined;
  truncated?: boolean | undefined;
  /** Mounts the helper cannot see (hiddenMounts). */
  hidden?: string[] | undefined;
  empty: string;
  children: (fs: T) => ReactNode;
}) {
  return (
    <section className="panel" aria-labelledby={`${id}-fs-heading`}>
      <header className="panel-head">
        <h2 id={`${id}-fs-heading`}>Filesystems</h2>
        {available && <span className="tag">observed</span>}
      </header>
      {!available ? (
        <p className="banner" role="status">
          {reason || 'Filesystem status is unavailable.'}
        </p>
      ) : filesystems.length > 0 ? (
        filesystems.map(children)
      ) : truncated || (skipped?.length ?? 0) > 0 || (hidden?.length ?? 0) > 0 ? null : (
        // "Nothing mounted" only when no mount was skipped, hidden or cut off.
        <p className="empty">{empty}</p>
      )}
      {truncated && (
        <p className="banner" role="status">
          Not every filesystem could be inspected.
        </p>
      )}
      {hidden && hidden.length > 0 && (
        <p className="banner" role="status">
          Not inspected: {hidden.join(', ')}. The read-only storage helper cannot see mounts under /home,
          /root or /run/user.
        </p>
      )}
      {skipped?.map((s) => (
        <p key={s.mountpoint} className="banner" role="status">
          <code>{s.mountpoint}</code>: {s.reason}
        </p>
      ))}
    </section>
  );
}

const hiddenPrefixes = ['/home', '/root', '/run/user'];

/**
 * Mountpoints this page lists that no reported filesystem covers and that lie
 * where the helper's ProtectHome=yes hides them (its namespace has no mounts
 * there, so it cannot even list them as skipped).
 */
export function hiddenMounts(mountpoints: string[], covered: string[]): string[] {
  return [
    ...new Set(
      mountpoints.filter(
        (m) => !covered.includes(m) && hiddenPrefixes.some((h) => m === h || m.startsWith(`${h}/`)),
      ),
    ),
  ];
}

/** One fact row; omitted when the tool did not report the value. */
export function Fact({ label, value }: { label: string; value: ReactNode }) {
  if (value === undefined || value === null || value === '') return null;
  return (
    <div>
      <dt>{label}</dt>
      <dd>{value}</dd>
    </div>
  );
}
