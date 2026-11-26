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
  empty,
  children,
}: {
  id: string;
  available: boolean;
  reason?: string | undefined;
  filesystems: T[];
  skipped?: SkippedMount[] | undefined;
  truncated?: boolean | undefined;
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
      ) : (
        <p className="empty">{empty}</p>
      )}
      {truncated && (
        <p className="banner" role="status">
          Not every filesystem could be inspected.
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
