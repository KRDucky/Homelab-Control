// SPDX-FileCopyrightText: 2026 The Homelab Control Authors
//
// SPDX-License-Identifier: GPL-3.0-or-later

import { useEffect, useId, useRef, useState } from 'react';
import { Icon } from './icons';
import type { Preferences } from './preferences';

type Option<V extends string> = { id: V; label: string; hint?: string };

const themeOptions: Option<Preferences['theme']>[] = [
  {
    id: 'system',
    label: 'System',
    hint: 'Follows your device: Daybreak when light, Midnight when dark, Graphite for high contrast',
  },
  { id: 'midnight', label: 'Midnight', hint: 'Dark, frosted glass' },
  { id: 'daybreak', label: 'Daybreak', hint: 'Light' },
  { id: 'graphite', label: 'Graphite', hint: 'High-contrast dark' },
  { id: 'oled', label: 'OLED', hint: 'True black' },
];
const densityOptions: Option<Preferences['density']>[] = [
  { id: 'compact', label: 'Compact' },
  { id: 'standard', label: 'Standard' },
  { id: 'spacious', label: 'Spacious' },
];
const fontOptions: Option<Preferences['font']>[] = [
  { id: 'inter', label: 'Inter' },
  { id: 'system', label: 'System' },
];
const textOptions: Option<Preferences['textScale']>[] = [
  { id: '90', label: '90%' },
  { id: '100', label: '100%' },
  { id: '112', label: '112%' },
  { id: '125', label: '125%' },
];
const transparencyOptions: Option<Preferences['transparency']>[] = [
  { id: 'full', label: 'Glass' },
  { id: 'reduced', label: 'Solid' },
];
const motionOptions: Option<Preferences['motion']>[] = [
  { id: 'system', label: 'System' },
  { id: 'full', label: 'Full' },
  { id: 'reduced', label: 'Reduced' },
  { id: 'none', label: 'None' },
];

function Choice<V extends string>({
  legend,
  name,
  value,
  options,
  onChange,
  stacked,
}: {
  legend: string;
  name: string;
  value: V;
  options: Option<V>[];
  onChange: (v: V) => void;
  stacked?: boolean;
}) {
  return (
    <fieldset className="choice">
      <legend>{legend}</legend>
      <div className={stacked ? 'choice-list' : 'segmented'}>
        {options.map((o) => (
          <label key={o.id} className={value === o.id ? 'on' : ''}>
            <input
              type="radio"
              name={name}
              value={o.id}
              checked={value === o.id}
              onChange={() => onChange(o.id)}
            />
            <span className="choice-label">{o.label}</span>
            {o.hint && <span className="choice-hint">{o.hint}</span>}
          </label>
        ))}
      </div>
    </fieldset>
  );
}

/**
 * Header popover with the appearance preferences (HCIS AppearanceMenu). A non-modal dialog: Escape
 * or a click outside closes it and returns focus to the button. Changes apply immediately.
 */
export function AppearanceMenu({
  prefs,
  onChange,
  onReset,
}: {
  prefs: Preferences;
  onChange: <K extends keyof Preferences>(key: K, value: Preferences[K]) => void;
  onReset: () => void;
}) {
  const [open, setOpen] = useState(false);
  const wrap = useRef<HTMLDivElement>(null);
  const button = useRef<HTMLButtonElement>(null);
  const panelId = useId();
  const titleId = useId();
  const n = useId();

  useEffect(() => {
    if (!open) return;
    wrap.current?.querySelector<HTMLInputElement>('.appearance input:checked')?.focus();
    const onPointer = (e: PointerEvent) => {
      if (wrap.current && !wrap.current.contains(e.target as Node)) setOpen(false);
    };
    document.addEventListener('pointerdown', onPointer);
    return () => document.removeEventListener('pointerdown', onPointer);
  }, [open]);

  function close() {
    setOpen(false);
    button.current?.focus();
  }

  return (
    <div
      className="menu-wrap"
      ref={wrap}
      onKeyDown={(e) => {
        if (e.key === 'Escape' && open) {
          e.stopPropagation();
          close();
        }
      }}
      onBlur={(e) => {
        // Non-modal: moving focus out of the menu (Tab past the end) closes it.
        if (open && e.relatedTarget && !wrap.current?.contains(e.relatedTarget as Node)) setOpen(false);
      }}
    >
      <button
        ref={button}
        type="button"
        className="ghost icon-btn"
        aria-expanded={open}
        aria-controls={panelId}
        aria-haspopup="dialog"
        onClick={() => setOpen((o) => !o)}
      >
        <Icon name="eye" />
        <span className="btn-text">Appearance</span>
      </button>
      {open && (
        <div className="appearance" id={panelId} role="dialog" aria-labelledby={titleId}>
          <h2 id={titleId} className="appearance-title">
            Appearance
          </h2>
          <Choice
            legend="Theme"
            name={`${n}-theme`}
            value={prefs.theme}
            options={themeOptions}
            onChange={(v) => onChange('theme', v)}
            stacked
          />
          <Choice
            legend="Density"
            name={`${n}-density`}
            value={prefs.density}
            options={densityOptions}
            onChange={(v) => onChange('density', v)}
          />
          <Choice
            legend="Interface font"
            name={`${n}-font`}
            value={prefs.font}
            options={fontOptions}
            onChange={(v) => onChange('font', v)}
          />
          <Choice
            legend="Text size"
            name={`${n}-text`}
            value={prefs.textScale}
            options={textOptions}
            onChange={(v) => onChange('textScale', v)}
          />
          <Choice
            legend="Surfaces"
            name={`${n}-transparency`}
            value={prefs.transparency}
            options={transparencyOptions}
            onChange={(v) => onChange('transparency', v)}
          />
          <Choice
            legend="Motion"
            name={`${n}-motion`}
            value={prefs.motion}
            options={motionOptions}
            onChange={(v) => onChange('motion', v)}
          />
          <p className="appearance-help">
            The theme changes colours only. Density, font, text size, surfaces and motion are separate
            preferences that stay when you switch themes. Saved in this browser.
          </p>
          <div className="appearance-actions">
            <button type="button" className="ghost small" onClick={onReset}>
              Reset to defaults
            </button>
            <button type="button" className="ghost small" onClick={close}>
              Done
            </button>
          </div>
        </div>
      )}
    </div>
  );
}
