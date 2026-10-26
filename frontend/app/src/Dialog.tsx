// SPDX-FileCopyrightText: 2026 The Homelab Control Authors
// SPDX-License-Identifier: GPL-3.0-or-later

import { useEffect, useId, useRef, type ReactNode } from 'react';

/** Native modal: browser focus trap, inert background, Escape and focus restoration. */
export function Dialog({
  title,
  children,
  onClose,
}: {
  title: string;
  children: ReactNode;
  onClose: () => void;
}) {
  const ref = useRef<HTMLDialogElement>(null);
  const id = useId();
  useEffect(() => {
    const previous = document.activeElement as HTMLElement | null;
    const el = ref.current;
    el?.showModal();
    el?.querySelector<HTMLInputElement>('[role=combobox]')?.focus();
    return () => {
      el?.close();
      previous?.focus();
    };
  }, []);
  return (
    <dialog
      ref={ref}
      className="hc-dialog"
      aria-labelledby={id}
      onCancel={(e) => {
        e.preventDefault();
        onClose();
      }}
    >
      <header className="panel-head">
        <h2 id={id}>{title}</h2>
        <button type="button" className="ghost" onClick={onClose} aria-label={`Close ${title}`}>
          Close
        </button>
      </header>
      {children}
    </dialog>
  );
}
