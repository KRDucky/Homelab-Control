// SPDX-FileCopyrightText: 2026 The Homelab Control Authors
// SPDX-License-Identifier: GPL-3.0-or-later

import { useId, useRef, useState } from 'react';
import type { Capability } from './api';
import { navigate } from './router';
import { Dialog } from './Dialog';

export function CommandPalette({ pages, accountPath }: { pages: Capability[]; accountPath: string }) {
  const [open, setOpen] = useState(false);
  const [query, setQuery] = useState('');
  const [active, setActive] = useState(0);
  const trigger = useRef<HTMLButtonElement>(null);
  const id = useId();
  const commands = [
    ...pages
      .filter((p) => p.status === 'available' || p.status === 'degraded')
      .map((p) => ({ label: p.label, path: p.path })),
    { label: 'Sessions', path: accountPath },
    { label: 'Theme Studio', path: '/appearance/themes' },
  ];
  const results = commands.filter((c) =>
    query
      .toLowerCase()
      .trim()
      .split(/\s+/)
      .every((word) => c.label.toLowerCase().includes(word)),
  );
  const index = Math.min(active, Math.max(0, results.length - 1));
  useEffect(() => {
    if (open) document.getElementById(`${id}-${index}`)?.scrollIntoView?.({ block: 'nearest' });
  }, [open, index, id]);
  function show() {
    setQuery('');
    setActive(0);
    setOpen(true);
  }
  function choose(path: string) {
    setOpen(false);
    navigate(path);
    requestAnimationFrame(() => document.getElementById('main')?.focus());
  }
  return (
    <>
      <button
        type="button"
        ref={trigger}
        className="ghost"
        onClick={show}
        aria-keyshortcuts="Control+k Meta+k"
      >
        Search pages
      </button>
      <PaletteShortcut onOpen={show} />
      {open && (
        <Dialog title="Go to page" onClose={() => setOpen(false)}>
          <label htmlFor={`${id}-query`}>Search permitted pages</label>
          <input
            id={`${id}-query`}
            role="combobox"
            aria-expanded="true"
            aria-controls={`${id}-results`}
            aria-autocomplete="list"
            aria-activedescendant={results.length ? `${id}-${index}` : undefined}
            value={query}
            onChange={(e) => {
              setQuery(e.target.value);
              setActive(0);
            }}
            onKeyDown={(e) => {
              if (e.key === 'ArrowDown' || e.key === 'ArrowUp') {
                e.preventDefault();
                setActive(
                  (index + (e.key === 'ArrowDown' ? 1 : -1) + Math.max(1, results.length)) %
                    Math.max(1, results.length),
                );
              }
              if (e.key === 'Enter' && results[index]) {
                e.preventDefault();
                choose(results[index].path);
              }
            }}
          />
          <ul id={`${id}-results`} role="listbox" aria-label="Pages" className="palette-results">
            {results.map((c, i) => (
              <li
                key={c.path}
                id={`${id}-${i}`}
                role="option"
                aria-selected={i === index}
                onPointerMove={() => setActive(i)}
                onClick={() => choose(c.path)}
              >
                {c.label}
              </li>
            ))}
          </ul>
          <p role="status">
            {results.length ? `${results.length} pages. Use arrow keys and Enter.` : 'No matching pages.'}
          </p>
        </Dialog>
      )}
    </>
  );
}

import { useEffect } from 'react';
function PaletteShortcut({ onOpen }: { onOpen: () => void }) {
  useEffect(() => {
    const key = (e: KeyboardEvent) => {
      if (
        (e.ctrlKey || e.metaKey) &&
        e.key.toLowerCase() === 'k' &&
        !e.altKey &&
        !document.querySelector('dialog[open]')
      ) {
        e.preventDefault();
        onOpen();
      }
    };
    window.addEventListener('keydown', key);
    return () => window.removeEventListener('keydown', key);
  }, [onOpen]);
  return null;
}
