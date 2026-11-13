// SPDX-FileCopyrightText: 2026 The Homelab Control Authors
// SPDX-License-Identifier: GPL-3.0-or-later

import { Dialog } from './Dialog';
import { linkHandler } from './router';

/** P0 install-plan review: Software Center search only. Never POSTs a package. */
export function InstallPlanDialog({
  title,
  pkg,
  onClose,
}: {
  title: string;
  pkg: string;
  onClose: () => void;
}) {
  const path = `/software?q=${encodeURIComponent(pkg)}`;
  return (
    <Dialog title={title} onClose={onClose}>
      <p>
        This preview does not install packages. ADR 0014 package transactions are not implemented; Software
        Center search only.
      </p>
      <p>
        Package: <code>{pkg}</code>
      </p>
      <p>
        <a href={path} onClick={linkHandler(path)}>
          Open Software Center
        </a>
      </p>
    </Dialog>
  );
}
