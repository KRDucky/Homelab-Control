// SPDX-FileCopyrightText: 2026 The Homelab Control Authors
// SPDX-License-Identifier: GPL-3.0-or-later
import { describe, expect, it } from 'vitest';

// Every shipped UI source file (tests excluded). Vite inlines them as text.
const sources = import.meta.glob(['./**/*.{ts,tsx,css,json}', '!./**/*.test.{ts,tsx}'], {
  query: '?raw',
  import: 'default',
  eager: true,
}) as Record<string, string>;

describe('product branding in the shipped UI', () => {
  it('scans the UI sources', () => {
    expect(Object.keys(sources).length).toBeGreaterThan(50);
  });

  it('names the product "Homelab Control", never "HomeControl"', () => {
    const hits = Object.entries(sources).filter(([, text]) => text.includes('HomeControl'));
    expect(hits.map(([file]) => file)).toEqual([]);
  });

  it('has no Webmin text, links or assets (ADR 0016)', () => {
    const hits = Object.entries(sources).filter(([, text]) => /webmin|miniserv/i.test(text));
    expect(hits.map(([file]) => file)).toEqual([]);
  });
});
