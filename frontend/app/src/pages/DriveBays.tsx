// SPDX-FileCopyrightText: 2026 The Homelab Control Authors
// SPDX-License-Identifier: GPL-3.0-or-later

import { useCallback, useState, type KeyboardEvent } from 'react';
import {
  ApiError,
  assignBay,
  createChassis,
  deleteChassis,
  getLayout,
  unassignBay,
  updateChassis,
  type CellKind,
  type Chassis,
  type ChassisKind,
  type ChassisSpec,
  type DriveLayout,
} from '../api';
import { BAY_KINDS, BAY_STATE_TEXT, CELL_KINDS, bayState, unassignedDrives } from '../bayModel';
import { formatBytes } from '../format';
import { driveLabel, type DriveLabelPreference, type StorageNode } from '../storageModel';
import { Section } from '../ui';
import { explain, useData } from '../useData';

// Manual drive-bay layout (M2-5 Stage A). Locations are the administrator's
// statements, shown as "manual, unverified". Nothing here touches the host.

const CHASSIS_KINDS: ChassisKind[] = ['server', 'shelf', 'external', 'desktop'];
type Selected = { chassis: number; row: number; col: number };

function specOf(
  c: Chassis,
  change?: { row: number; col: number; kind: CellKind; label: string },
): ChassisSpec {
  return {
    name: c.name,
    kind: c.kind,
    rows: c.rows,
    cols: c.cols,
    cells: c.cells.map((cell) =>
      change && cell.row === change.row && cell.col === change.col
        ? change
        : { row: cell.row, col: cell.col, kind: cell.kind, label: cell.label },
    ),
  };
}

function bayName(c: Chassis, row: number, col: number) {
  const cell = c.cells.find((x) => x.row === row && x.col === col);
  return `${c.name} → Row ${row + 1} → Column ${col + 1}${cell?.label ? ` → ${cell.label}` : ''}`;
}

export function DriveBays({
  csrf,
  nodes,
  canManage,
  preference,
  onUnauthorized,
}: {
  csrf: string;
  nodes: StorageNode[] | undefined;
  canManage: boolean;
  preference: DriveLabelPreference;
  onUnauthorized: () => void;
}) {
  const { data, error, live } = useData(
    useCallback(() => getLayout(csrf), [csrf]),
    { onUnauthorized },
  );
  const [saved, setSaved] = useState<DriveLayout | null>(null);
  const [selected, setSelected] = useState<Selected | null>(null);
  const [focus, setFocus] = useState<Selected | null>(null);
  const [message, setMessage] = useState('');
  const [busy, setBusy] = useState(false);
  const [draft, setDraft] = useState<ChassisSpec>({ name: '', kind: 'server', rows: 2, cols: 4 });
  const [cellDraft, setCellDraft] = useState<{ kind: CellKind; label: string } | null>(null);
  const [drive, setDrive] = useState('');
  const [resize, setResize] = useState<{ id: number; name: string; rows: number; cols: number } | null>(null);
  const layout = saved ?? data;

  async function run(action: () => Promise<DriveLayout>, done: string) {
    setBusy(true);
    setMessage('');
    try {
      setSaved(await action());
      setMessage(done);
    } catch (err) {
      if (err instanceof ApiError && err.status === 401) onUnauthorized();
      setMessage(
        err instanceof ApiError
          ? err.message
          : 'The server did not respond; nothing is known to have changed.',
      );
    } finally {
      setBusy(false);
    }
  }

  if (!layout) {
    return (
      <Section
        title="Drives & bays (manual layout)"
        id="drive-bays"
        meta={{ status: 'unavailable', sources: [] }}
      >
        <p role={error ? 'alert' : 'status'}>{error ? explain(error) : 'Reading drive-bay layout…'}</p>
      </Section>
    );
  }

  const free = unassignedDrives(nodes ?? [], layout.chassis);
  const chassis = selected ? layout.chassis.find((c) => c.id === selected.chassis) : undefined;
  const cell = chassis?.cells.find((x) => x.row === selected?.row && x.col === selected?.col);
  const current = cell ? bayState(cell, nodes) : undefined;

  function select(next: Selected) {
    setSelected(next);
    setFocus(next);
    setCellDraft(null);
    setDrive('');
  }

  function onGridKey(c: Chassis, event: KeyboardEvent<HTMLDivElement>) {
    const at = focus?.chassis === c.id ? focus : { chassis: c.id, row: 0, col: 0 };
    const moves: Record<string, [number, number]> = {
      ArrowUp: [-1, 0],
      ArrowDown: [1, 0],
      ArrowLeft: [0, -1],
      ArrowRight: [0, 1],
    };
    const move = moves[event.key];
    if (!move) return;
    event.preventDefault();
    const next = {
      chassis: c.id,
      row: Math.min(c.rows - 1, Math.max(0, at.row + move[0])),
      col: Math.min(c.cols - 1, Math.max(0, at.col + move[1])),
    };
    setFocus(next);
    event.currentTarget.querySelector<HTMLButtonElement>(`[data-bay="${next.row}-${next.col}"]`)?.focus();
  }

  return (
    <Section
      title="Drives & bays (manual layout)"
      id="drive-bays"
      live={live}
      meta={{
        status: nodes ? 'ok' : 'partial',
        sources: ['state.db (administrator-entered layout)'],
        notes: nodes ? [] : ['No current storage observation; bay presence is unknown.'],
      }}
    >
      <p className="banner">
        Bay locations are entered by an administrator and are <strong>manual, unverified</strong>. They are
        not read from enclosure or controller hardware. Drives are matched by persistent identity only, never
        by a kernel name such as /dev/sdX.
      </p>
      <p role="status" aria-live="polite">
        {message}
      </p>
      {layout.chassis.length === 0 && <p className="muted">No chassis has been described yet.</p>}
      {layout.chassis.map((c) => (
        <div key={c.id}>
          <h3 className="subhead">
            {c.name}{' '}
            <span className="muted">
              ({c.kind}, {c.rows} × {c.cols})
            </span>
          </h3>
          <div role="grid" aria-label={`${c.name} bays`} onKeyDown={(event) => onGridKey(c, event)}>
            {Array.from({ length: c.rows }, (_, row) => (
              <div role="row" key={row} className="toolbar">
                {Array.from({ length: c.cols }, (_, col) => {
                  const x = c.cells.find((cc) => cc.row === row && cc.col === col);
                  if (!x) return null;
                  const st = bayState(x, nodes).state;
                  const at = focus?.chassis === c.id ? focus : { row: 0, col: 0 };
                  const isFocus = at.row === row && at.col === col;
                  const isSelected =
                    selected?.chassis === c.id && selected.row === row && selected.col === col;
                  return (
                    <div role="gridcell" key={col} aria-selected={isSelected}>
                      <button
                        type="button"
                        className={isSelected ? undefined : 'ghost'}
                        data-bay={`${row}-${col}`}
                        tabIndex={isFocus ? 0 : -1}
                        onClick={() => select({ chassis: c.id, row, col })}
                        aria-label={`${bayName(c, row, col)}: ${BAY_STATE_TEXT[st]}`}
                      >
                        {x.label || `R${row + 1}C${col + 1}`}
                        <span className="muted"> {x.kind}</span>
                        <span className="tag">{BAY_STATE_TEXT[st]}</span>
                      </button>
                    </div>
                  );
                })}
              </div>
            ))}
          </div>
          {canManage && (
            <div className="toolbar">
              {resize?.id === c.id ? (
                <>
                  <label className="inline-field">
                    Name
                    <input
                      maxLength={64}
                      value={resize.name}
                      onChange={(e) => setResize({ ...resize, name: e.target.value })}
                    />
                  </label>
                  <label className="inline-field">
                    Rows
                    <input
                      type="number"
                      min={1}
                      max={24}
                      value={resize.rows}
                      onChange={(e) => setResize({ ...resize, rows: Number(e.target.value) })}
                    />
                  </label>
                  <label className="inline-field">
                    Columns
                    <input
                      type="number"
                      min={1}
                      max={24}
                      value={resize.cols}
                      onChange={(e) => setResize({ ...resize, cols: Number(e.target.value) })}
                    />
                  </label>
                  <button
                    type="button"
                    disabled={busy}
                    onClick={() => {
                      const spec = specOf(c);
                      spec.name = resize.name;
                      spec.rows = resize.rows;
                      spec.cols = resize.cols;
                      spec.cells = (spec.cells ?? []).filter(
                        (x) => x.row < resize.rows && x.col < resize.cols,
                      );
                      setResize(null);
                      setSelected(null);
                      run(() => updateChassis(c.id, spec, csrf), `Saved ${resize.name}.`);
                    }}
                  >
                    Save chassis
                  </button>
                </>
              ) : (
                <button
                  type="button"
                  className="ghost"
                  onClick={() => setResize({ id: c.id, name: c.name, rows: c.rows, cols: c.cols })}
                >
                  Edit chassis {c.name}
                </button>
              )}
              <button
                type="button"
                className="ghost"
                disabled={busy}
                onClick={() => run(() => deleteChassis(c.id, csrf), `Deleted ${c.name}.`)}
              >
                Delete chassis {c.name}
              </button>
            </div>
          )}
        </div>
      ))}

      {chassis && cell && current && selected && (
        <div aria-label="Selected bay">
          <h3 className="subhead">{bayName(chassis, selected.row, selected.col)}</h3>
          <dl className="facts">
            <div>
              <dt>Bay state</dt>
              <dd>{BAY_STATE_TEXT[current.state]}</dd>
            </div>
            <div>
              <dt>Location confidence</dt>
              <dd>
                {cell.assignment
                  ? `Manual, unverified — stated by ${cell.assignment.assigned_by} on ${new Date(cell.assignment.assigned_at).toLocaleString()}`
                  : 'No assignment'}
              </dd>
            </div>
            {cell.assignment && (
              <div>
                <dt>Assigned identity</dt>
                <dd className="mono">{cell.assignment.identity}</dd>
              </div>
            )}
            {current.node && (
              <>
                <div>
                  <dt>Drive</dt>
                  <dd className="mono">{driveLabel(current.node, preference).value}</dd>
                </div>
                <div>
                  <dt>Kernel name (volatile, not an identity)</dt>
                  <dd className="mono">{current.node.name}</dd>
                </div>
                <div>
                  <dt>Observed size</dt>
                  <dd>
                    {current.node.size_bytes === undefined
                      ? 'Not reported'
                      : formatBytes(current.node.size_bytes)}
                  </dd>
                </div>
                <div>
                  <dt>Identity status</dt>
                  <dd>{current.node.identity_status}</dd>
                </div>
                <div>
                  <dt>Identifiers</dt>
                  <dd>
                    <ul>
                      {(current.node.identifiers ?? []).map((id) => (
                        <li key={`${id.kind}:${id.value}`} className="mono">
                          {id.kind}: {id.value}
                          {id.ambiguous ? ' (duplicated, ambiguous)' : ''}
                        </li>
                      ))}
                    </ul>
                  </dd>
                </div>
              </>
            )}
          </dl>
          {current.state === 'missing' && (
            <p>
              The assigned drive is not in the current inventory. Its assignment is kept. A replacement drive
              is never assigned automatically: unassign this bay, then assign the new drive.
            </p>
          )}
          {current.state === 'mismatch' && (
            <p>
              The assigned identity is duplicated or no longer belongs to exactly one whole disk. Verify the
              drive physically before relying on this location.
            </p>
          )}
          {canManage && (
            <div className="toolbar">
              <label className="inline-field">
                Cell type
                <select
                  value={cellDraft?.kind ?? cell.kind}
                  onChange={(e) =>
                    setCellDraft({ kind: e.target.value as CellKind, label: cellDraft?.label ?? cell.label })
                  }
                >
                  {CELL_KINDS.map((k) => (
                    <option key={k} value={k}>
                      {k}
                    </option>
                  ))}
                </select>
              </label>
              <label className="inline-field">
                Label
                <input
                  value={cellDraft?.label ?? cell.label}
                  maxLength={64}
                  onChange={(e) =>
                    setCellDraft({ kind: cellDraft?.kind ?? cell.kind, label: e.target.value })
                  }
                />
              </label>
              <button
                type="button"
                disabled={busy || !cellDraft}
                onClick={() =>
                  cellDraft &&
                  run(
                    () =>
                      updateChassis(
                        chassis.id,
                        specOf(chassis, { row: cell.row, col: cell.col, ...cellDraft }),
                        csrf,
                      ),
                    'Bay saved.',
                  )
                }
              >
                Save bay
              </button>
              {cell.assignment ? (
                <button
                  type="button"
                  disabled={busy}
                  onClick={() =>
                    run(() => unassignBay(chassis.id, cell.row, cell.col, csrf), 'Assignment removed.')
                  }
                >
                  Unassign drive
                </button>
              ) : (
                BAY_KINDS.has(cell.kind) && (
                  <>
                    <label className="inline-field">
                      Drive to assign
                      <select value={drive} onChange={(e) => setDrive(e.target.value)}>
                        <option value="">Choose a drive…</option>
                        {free.eligible.map((node) => {
                          const label = driveLabel(node, preference);
                          return (
                            <option key={node.persistent_id} value={node.persistent_id}>
                              {label.value} ({label.kind}
                              {node.size_bytes === undefined ? '' : `, ${formatBytes(node.size_bytes)}`})
                            </option>
                          );
                        })}
                      </select>
                    </label>
                    <button
                      type="button"
                      disabled={busy || !drive}
                      onClick={() =>
                        run(
                          () => assignBay(chassis.id, cell.row, cell.col, drive, csrf),
                          'Drive assigned (manual, unverified).',
                        )
                      }
                    >
                      Assign drive
                    </button>
                  </>
                )
              )}
            </div>
          )}
        </div>
      )}

      <h3 className="subhead">Drives not assigned to a bay</h3>
      {!nodes ? (
        <p>No current storage observation.</p>
      ) : (
        <ul aria-label="Unassigned drives">
          {free.eligible.map((node) => (
            <li key={node.id} className="mono">
              {driveLabel(node, preference).value}
            </li>
          ))}
          {free.ineligible.map((node) => (
            <li key={node.id}>
              <span className="mono">{driveLabel(node, preference).value}</span> — cannot be assigned: no
              unique WWN, NVMe UUID or serial identity observed
            </li>
          ))}
          {free.eligible.length + free.ineligible.length === 0 && <li>None observed.</li>}
        </ul>
      )}

      {canManage && (
        <form
          aria-label="Add chassis"
          onSubmit={(e) => {
            e.preventDefault();
            run(() => createChassis(draft, csrf), `Added ${draft.name}.`);
          }}
        >
          <h3 className="subhead">Add chassis</h3>
          <div className="toolbar">
            <label className="inline-field">
              Name
              <input
                required
                maxLength={64}
                value={draft.name}
                onChange={(e) => setDraft({ ...draft, name: e.target.value })}
              />
            </label>
            <label className="inline-field">
              Type
              <select
                value={draft.kind}
                onChange={(e) => setDraft({ ...draft, kind: e.target.value as ChassisKind })}
              >
                {CHASSIS_KINDS.map((k) => (
                  <option key={k} value={k}>
                    {k}
                  </option>
                ))}
              </select>
            </label>
            <label className="inline-field">
              Rows
              <input
                type="number"
                min={1}
                max={24}
                value={draft.rows}
                onChange={(e) => setDraft({ ...draft, rows: Number(e.target.value) })}
              />
            </label>
            <label className="inline-field">
              Columns
              <input
                type="number"
                min={1}
                max={24}
                value={draft.cols}
                onChange={(e) => setDraft({ ...draft, cols: Number(e.target.value) })}
              />
            </label>
            <button type="submit" disabled={busy}>
              Add chassis
            </button>
          </div>
        </form>
      )}
    </Section>
  );
}
