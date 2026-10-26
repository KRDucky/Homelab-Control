// SPDX-FileCopyrightText: 2026 The Homelab Control Authors
//
// SPDX-License-Identifier: GPL-3.0-or-later

// Paths from the original HCIS icon set (docs/ui/hcis/icons: 20px grid, 1.5px stroke, currentColor).
const PATHS = {
  check: 'M4 10l4 4 8-8',
  warning: 'M10 2 1 18h18L10 2zM10 7v5M10 15v.01',
  critical: 'M6 2h8l4 4v8l-4 4H6l-4-4V6zM10 6v6M10 15v.01',
  info: 'M10 2a8 8 0 1 1 0 16 8 8 0 0 1 0-16zM10 9v5M10 6v.01',
  overview: 'M3 3h6v6H3zM11 3h6v4h-6zM11 9h6v8h-6zM3 11h6v6H3z',
  system: 'M6 6h8v8H6zM8 2v3M12 2v3M8 15v3M12 15v3M2 8h3M2 12h3M15 8h3M15 12h3',
  hardware: 'M3 5h14v10H3zM6 15v2M14 15v2M6 8h5M6 11h3M14 8.5v.01',
  external: 'M11 3h6v6M17 3l-8 8M14 12v5H3V6h5',
  audit: 'M5 3h10v14H5zM8 7h4M8 10h4M8 13h2',
  unknown:
    'M10 2.5a7.5 7.5 0 1 1 0 15 7.5 7.5 0 0 1 0-15zM7.8 8a2.3 2.3 0 0 1 4.4.8c0 1.5-2.2 1.8-2.2 3.2M10 14.3v.01',
  menu: 'M3 5h14M3 10h14M3 15h14',
  sidebar: 'M3 4h14v12H3zM8 4v12',
  eye: 'M2 10s3-5.5 8-5.5S18 10 18 10s-3 5.5-8 5.5S2 10 2 10zM10 7.5a2.5 2.5 0 1 1 0 5 2.5 2.5 0 0 1 0-5z',
  close: 'M5 5l10 10M15 5 5 15',
} as const;

export type IconName = keyof typeof PATHS;

/** Decorative icon: the accessible name always comes from the surrounding control or text. */
export function Icon({ name }: { name: IconName }) {
  return (
    <svg
      className="icon"
      viewBox="0 0 20 20"
      width="20"
      height="20"
      fill="none"
      stroke="currentColor"
      strokeWidth="1.5"
      strokeLinecap="round"
      strokeLinejoin="round"
      aria-hidden="true"
      focusable="false"
    >
      <path d={PATHS[name]} />
    </svg>
  );
}

/** Navigation icon for a capability id; unknown modules get the neutral "unknown" glyph. */
export const NAV_ICONS: Record<string, IconName> = {
  overview: 'overview',
  services: 'system',
  hardware: 'hardware',
  legacy: 'external',
  audit: 'audit',
};
