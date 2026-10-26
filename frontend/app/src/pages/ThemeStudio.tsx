// SPDX-FileCopyrightText: 2026 The Homelab Control Authors
// SPDX-License-Identifier: GPL-3.0-or-later

import { useEffect, useMemo, useRef, useState } from 'react';
import { DataTable } from '../DataTable';
import { StatusPill } from '../ui';
import { applyPreferences, loadPreferences } from '../preferences';
import {
  ACTIVE_THEME,
  BASES,
  COLOR_KEYS,
  REQUIREMENTS,
  activateTheme,
  activeTheme,
  applyTheme,
  exportTheme,
  loadThemes,
  loadThemeVersions,
  newTheme,
  parseTheme,
  resolvedColors,
  saveThemes,
  themeIssues,
  type BaseTheme,
  type ThemeDocument,
} from '../theme';

function ColorValue({
  value,
  name,
  onCommit,
}: {
  value: string;
  name: string;
  onCommit: (v: string) => void;
}) {
  const [text, setText] = useState(value);
  const [seen, setSeen] = useState(value);
  // Follow edits made elsewhere (colour picker, undo, reset, import) without remounting the field.
  if (value !== seen) {
    setSeen(value);
    setText(value);
  }
  const valid = /^#[\da-f]{6}([\da-f]{2})?$/i.test(text);
  return (
    <input
      id={`color-${name}`}
      aria-label={`${name} hex colour`}
      value={text}
      maxLength={9}
      aria-invalid={!valid}
      onChange={(e) => {
        const next = e.target.value;
        setText(next);
        // Commit as soon as the value is a complete colour, so Save, Preview and validation never lag
        // behind what is typed (a blur-only commit re-flowed the page between mouse-down and click).
        if (/^#[\da-f]{6}([\da-f]{2})?$/i.test(next) && next.toLowerCase() !== value.toLowerCase())
          onCommit(next);
      }}
      onBlur={() => {
        if (!valid) setText(value);
      }}
    />
  );
}

function Preview({ draft, width }: { draft: ThemeDocument; width: string }) {
  const ref = useRef<HTMLDivElement>(null);
  useEffect(() => {
    if (ref.current) applyTheme(draft, ref.current);
  }, [draft]);
  return (
    <div className={`studio-preview preview-${width}`} ref={ref} aria-label="Theme preview with example data">
      <p className="tag">Example data · preview only</p>
      <h2>Host overview</h2>
      <p>Independent modules. Clear system state.</p>
      <div className="toolbar">
        <StatusPill tone="ok" label="Healthy" />
        <StatusPill tone="warn" label="Warning" />
        <StatusPill tone="crit" label="Critical" />
        <StatusPill tone="unknown" label="Not checked" />
      </div>
      <DataTable
        caption="Example services"
        rows={[
          { name: 'backup.service', status: 'Active' },
          { name: 'media.service', status: 'Not checked' },
        ]}
        rowKey={(r) => r.name}
        columns={[
          { id: 'name', label: 'Unit', render: (r) => <code>{r.name}</code> },
          { id: 'status', label: 'State', render: (r) => r.status },
        ]}
      />
      <label className="studio-field">
        Example input
        <input placeholder="Search services" />
      </label>
      <div className="toolbar">
        <button type="button">Primary action</button>
        <button type="button" className="ghost">
          Secondary action
        </button>
      </div>
      <p className="banner">Warnings always keep their text and icon.</p>
    </div>
  );
}

/** Browser-local authoring. Drafts affect only the preview; activation uses a timed confirmation. */
export function ThemeStudio() {
  const [installed, setInstalled] = useState(loadThemes);
  const [draft, setDraft] = useState<ThemeDocument>(() => newTheme('midnight'));
  const [past, setPast] = useState<ThemeDocument[]>([]);
  const [future, setFuture] = useState<ThemeDocument[]>([]);
  const [json, setJson] = useState('');
  const [advanced, setAdvanced] = useState(false);
  const [message, setMessage] = useState('');
  const [width, setWidth] = useState('desktop');
  const [trial, setTrial] = useState<ThemeDocument | null>(null);
  const [seconds, setSeconds] = useState(15);
  const [current, setCurrent] = useState(() => activeTheme()?.id ?? 'Built-in');
  const previewWrap = useRef<HTMLDivElement>(null);
  const restore = useRef<() => void>(() => undefined);
  const issues = useMemo(() => themeIssues(draft), [draft]);
  const colors = resolvedColors(draft);
  function edit(next: ThemeDocument) {
    setPast((p) => [...p.slice(-199), draft]);
    setDraft(next);
    setFuture([]);
    setMessage('');
  }
  function undo() {
    const prev = past.at(-1);
    if (prev) {
      setFuture((p) => [draft, ...p]);
      setDraft(prev);
      setPast(past.slice(0, -1));
    }
  }
  function redo() {
    const next = future[0];
    if (next) {
      setPast((p) => [...p, draft]);
      setDraft(next);
      setFuture(future.slice(1));
    }
  }
  useEffect(() => {
    const changed = () => {
      setInstalled(loadThemes());
      setCurrent(activeTheme()?.id ?? 'Built-in');
    };
    window.addEventListener('hc-themes-change', changed);
    window.addEventListener('storage', changed);
    return () => {
      window.removeEventListener('hc-themes-change', changed);
      window.removeEventListener('storage', changed);
    };
  }, []);
  useEffect(() => {
    if (!trial) return;
    const previous = activeTheme();
    restore.current = () => {
      applyPreferences(loadPreferences());
      applyTheme(previous);
    };
    applyTheme(trial);
    const end = Date.now() + 15000;
    const tick = window.setInterval(() => {
      const remaining = Math.max(0, Math.ceil((end - Date.now()) / 1000));
      setSeconds(remaining);
      if (!remaining) setTrial(null);
    }, 250);
    const key = (e: KeyboardEvent) => {
      if (e.key === 'Escape') setTrial(null);
    };
    window.addEventListener('keydown', key);
    previewWrap.current?.querySelector<HTMLButtonElement>('[data-revert]')?.focus();
    return () => {
      window.clearInterval(tick);
      window.removeEventListener('keydown', key);
      restore.current();
    };
  }, [trial]);
  function validated(): ThemeDocument {
    return parseTheme(JSON.stringify(draft));
  }
  function save() {
    try {
      let d = validated();
      const previous = installed.find((t) => t.id === d.id);
      if (previous) {
        const numeric = (v: string) => v.split('.').map(Number);
        const old = numeric(previous.version),
          nextVersion = numeric(d.version);
        const newer = nextVersion.some(
          (n, i) => n > (old[i] ?? 0) && nextVersion.slice(0, i).every((v, j) => v === old[j]),
        );
        if (!newer) d = { ...d, version: `${old[0]}.${old[1]}.${(old[2] ?? 0) + 1}` };
      }
      setDraft(d);
      const next = [...installed.filter((t) => t.id !== d.id), d];
      saveThemes(next);
      setInstalled(next);
      setMessage('Theme saved in this browser. Export a backup to keep a copy.');
    } catch (e) {
      setMessage((e as Error).message);
    }
  }
  function begin(d: ThemeDocument) {
    try {
      const checked = parseTheme(JSON.stringify(d));
      if (themeIssues(checked).length)
        throw new Error('Fix contrast issues before previewing on the application.');
      setSeconds(15);
      setTrial(checked);
    } catch (e) {
      setMessage((e as Error).message);
    }
  }
  async function importFile(file?: File) {
    if (!file) return;
    try {
      if (file.size > 65536) throw new Error('Theme exceeds 64 KiB.');
      const d = parseTheme(await file.text());
      edit(d);
      setAdvanced(false);
      setMessage('Imported into the isolated preview. Review validation before saving or activating.');
    } catch (e) {
      setMessage((e as Error).message);
    }
  }
  return (
    <div
      className="page theme-studio"
      onKeyDown={(e) => {
        const target = e.target as HTMLElement;
        if ((e.ctrlKey || e.metaKey) && e.key.toLowerCase() === 'z' && !target.matches('input, textarea')) {
          e.preventDefault();
          if (e.shiftKey) redo();
          else undo();
        }
      }}
    >
      <header className="page-head">
        <h1 className="page-title">Themes & Theme Studio</h1>
        <p className="page-sub">
          Create, validate and share HCIS colours. Themes stay in this browser; density, fonts, text size,
          motion and navigation stay independent.
        </p>
      </header>
      <p className="footnote">{REQUIREMENTS}</p>
      <details>
        <summary>View theme requirements</summary>
        <ul>
          {[
            'Preserve responsive layouts and usable components.',
            'Keep readable text, non-text contrast, keyboard focus and safety warnings.',
            'Respect motion, transparency, font, density and layout preferences.',
            'Use validated .hltheme JSON only; no code, CSS, remote assets or font files.',
            'Never change functionality, permissions or safety controls.',
          ].map((r) => (
            <li key={r}>{r}</li>
          ))}
        </ul>
      </details>
      {message && (
        <p className="banner" role="status">
          {message}
        </p>
      )}
      <section className="panel" aria-label="Theme library">
        <h2>Theme library</h2>
        <p>Active custom theme: {current}</p>
        <div className="toolbar">
          {BASES.map((base) => (
            <button key={base} type="button" className="ghost" onClick={() => edit(newTheme(base))}>
              Create from {base}
            </button>
          ))}
          <button
            type="button"
            className="ghost"
            onClick={() => {
              try {
                activateTheme(null);
              } catch (e) {
                setMessage((e as Error).message);
              }
            }}
          >
            Use built-in appearance
          </button>
          <label className="inline-field">
            Import .hltheme
            <input
              type="file"
              accept=".hltheme,.json,application/json"
              onChange={(e) => {
                void importFile(e.target.files?.[0]);
                e.target.value = '';
              }}
            />
          </label>
        </div>
        <ul className="theme-library">
          {installed.map((d) => (
            <li key={d.id}>
              <strong>{d.name}</strong> <code>{d.id}</code> · {d.version} · {d.license}
              <div className="toolbar">
                <button
                  type="button"
                  className="ghost"
                  onClick={() => {
                    edit(d);
                    setAdvanced(false);
                  }}
                >
                  Edit {d.name}
                </button>
                <button type="button" className="ghost" onClick={() => begin(d)}>
                  Preview {d.name}
                </button>
                <button type="button" className="ghost" onClick={() => exportTheme(d)}>
                  Export {d.name}
                </button>
                <button
                  type="button"
                  className="ghost"
                  onClick={() => {
                    try {
                      if (localStorage.getItem(ACTIVE_THEME) === d.id) activateTheme(null);
                      const next = installed.filter((t) => t.id !== d.id);
                      saveThemes(next);
                      setInstalled(next);
                    } catch (e) {
                      setMessage((e as Error).message);
                    }
                  }}
                >
                  Remove {d.name}
                </button>
              </div>
              {loadThemeVersions(d.id).length > 0 && (
                <details>
                  <summary>Saved versions of {d.name}</summary>
                  <ul>
                    {loadThemeVersions(d.id).map((v, i) => (
                      <li key={`${v.version}-${i}`}>
                        <button
                          type="button"
                          className="ghost"
                          onClick={() => {
                            edit(v);
                            setAdvanced(false);
                            setMessage(
                              'Previous version loaded into the draft. Save to create a new revision.',
                            );
                          }}
                        >
                          Restore {v.version} to editor
                        </button>
                      </li>
                    ))}
                  </ul>
                </details>
              )}
            </li>
          ))}
        </ul>
      </section>
      {trial && (
        <div className="theme-trial panel" role="region" aria-label="Confirm theme preview" ref={previewWrap}>
          <h2>Keep this theme?</h2>
          <p role="timer">Reverts in {seconds} seconds. Escape reverts immediately.</p>
          <div className="toolbar">
            <button type="button" data-revert onClick={() => setTrial(null)}>
              Revert
            </button>
            <button
              type="button"
              onClick={() => {
                try {
                  const next = [...installed.filter((t) => t.id !== trial.id), trial];
                  saveThemes(next);
                  activateTheme(trial);
                  restore.current = () => {
                    applyPreferences(loadPreferences());
                    applyTheme(trial);
                  };
                  setTrial(null);
                  setMessage('Theme activated in this browser.');
                } catch (e) {
                  setMessage((e as Error).message);
                  setTrial(null);
                }
              }}
            >
              Keep theme
            </button>
          </div>
        </div>
      )}
      <section className="panel" aria-label="Theme editor">
        <div className="toolbar">
          <h2>Theme editor</h2>
          <button type="button" className="ghost" disabled={!past.length} onClick={undo}>
            Undo
          </button>
          <button type="button" className="ghost" disabled={!future.length} onClick={redo}>
            Redo
          </button>
          <button type="button" className="ghost" onClick={() => edit({ ...draft, colors: {} })}>
            Reset colours
          </button>
          <button
            type="button"
            className="ghost"
            onClick={() => {
              edit({
                ...draft,
                id: `${draft.id.replace(/-copy$/, '')}-copy`,
                name: `${draft.name.slice(0, 70)} copy`,
                version: '1.0.0',
              });
            }}
          >
            Duplicate
          </button>
          <button
            type="button"
            className="ghost"
            onClick={() => {
              setJson(JSON.stringify(draft, null, 2));
              setAdvanced(!advanced);
            }}
          >
            {advanced ? 'Form editor' : 'Advanced JSON'}
          </button>
        </div>
        <div className="studio-layout">
          <div className="studio-controls">
            {advanced ? (
              <>
                <label className="studio-field">
                  Theme JSON
                  <textarea
                    rows={20}
                    value={json}
                    spellCheck={false}
                    onChange={(e) => setJson(e.target.value)}
                  />
                </label>
                <button
                  type="button"
                  onClick={() => {
                    try {
                      edit(parseTheme(json));
                      setMessage('JSON applied to isolated preview.');
                    } catch (e) {
                      setMessage((e as Error).message);
                    }
                  }}
                >
                  Validate and apply JSON
                </button>
              </>
            ) : (
              <>
                <label className="studio-field">
                  Theme name
                  <input
                    value={draft.name}
                    maxLength={80}
                    onChange={(e) => edit({ ...draft, name: e.target.value })}
                  />
                </label>
                <label className="studio-field">
                  Identifier
                  <input
                    value={draft.id}
                    maxLength={100}
                    onChange={(e) => edit({ ...draft, id: e.target.value })}
                  />
                </label>
                <div className="studio-meta">
                  <label className="studio-field">
                    Version
                    <input
                      value={draft.version}
                      onChange={(e) => edit({ ...draft, version: e.target.value })}
                    />
                  </label>
                  <label className="studio-field">
                    License
                    <input
                      value={draft.license}
                      onChange={(e) => edit({ ...draft, license: e.target.value })}
                    />
                  </label>
                </div>
                <label className="studio-field">
                  Base theme
                  <select
                    value={draft.base}
                    onChange={(e) => edit({ ...draft, base: e.target.value as BaseTheme })}
                  >
                    {BASES.map((b) => (
                      <option key={b}>{b}</option>
                    ))}
                  </select>
                </label>
                {[
                  'surface',
                  'text',
                  'border',
                  'accent',
                  'focus',
                  'status',
                  'input',
                  'glass',
                  'scrim',
                  'meter',
                  'chart',
                  'backdrop',
                ].map((group) => {
                  const keys = COLOR_KEYS.filter((k) => k === group || k.startsWith(`${group}-`));
                  return keys.length ? (
                    <details key={group}>
                      <summary>{group} colours</summary>
                      {keys.map((key) => (
                        <div className="color-control" key={key}>
                          <label htmlFor={`color-${key}`}>{key}</label>
                          <ColorValue
                            value={colors[key] ?? '#000000'}
                            name={key}
                            onCommit={(value) =>
                              edit({ ...draft, colors: { ...draft.colors, [key]: value } })
                            }
                          />
                          <input
                            type="color"
                            aria-label={`${key} colour picker`}
                            value={colors[key]?.slice(0, 7)}
                            onChange={(e) =>
                              edit({ ...draft, colors: { ...draft.colors, [key]: e.target.value } })
                            }
                          />
                          <button
                            type="button"
                            className="ghost small"
                            aria-label={`Reset ${key}`}
                            onClick={() => {
                              const next = { ...draft.colors };
                              Reflect.deleteProperty(next, key);
                              edit({ ...draft, colors: next });
                            }}
                          >
                            Reset
                          </button>
                        </div>
                      ))}
                    </details>
                  ) : null;
                })}
              </>
            )}
          </div>
          <div className="studio-preview-wrap">
            <label className="inline-field">
              Preview width
              <select value={width} onChange={(e) => setWidth(e.target.value)}>
                <option value="desktop">Desktop</option>
                <option value="tablet">Tablet</option>
                <option value="phone">Phone</option>
              </select>
            </label>
            <Preview draft={draft} width={width} />
          </div>
        </div>
        <section className="studio-issues" aria-label="Theme validation">
          <h3>Validation</h3>
          <p role="status">
            {issues.length
              ? `${issues.length} contrast or surface issues block activation.`
              : 'Key contrast pairs pass. This is not a full accessibility audit.'}
          </p>
          <ul>
            {issues.map((issue, i) => (
              <li key={`${issue.path}-${i}`}>
                <button
                  type="button"
                  className="ghost issue-link"
                  onClick={() => {
                    const el = document.getElementById(`color-${issue.path}`);
                    const details = el?.closest('details');
                    if (details) details.open = true;
                    el?.focus();
                  }}
                >
                  {issue.message}
                </button>
              </li>
            ))}
          </ul>
        </section>
        <div className="toolbar">
          <button type="button" onClick={save}>
            Save draft
          </button>
          <button
            type="button"
            className="ghost"
            onClick={() => {
              try {
                exportTheme(validated());
              } catch (e) {
                setMessage((e as Error).message);
              }
            }}
          >
            Export .hltheme
          </button>
          <button type="button" disabled={issues.length > 0} onClick={() => begin(draft)}>
            Preview on application
          </button>
        </div>
      </section>
    </div>
  );
}
