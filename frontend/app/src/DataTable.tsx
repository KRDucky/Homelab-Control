// SPDX-FileCopyrightText: 2026 The Homelab Control Authors
// SPDX-License-Identifier: GPL-3.0-or-later

import { useId, useMemo, useState, type ReactNode } from 'react';
import { TableRegion } from './ui';

export interface DataTableColumn<Row> {
  id: string;
  label: string;
  render: (row: Row) => ReactNode;
  sortValue?: (row: Row) => string | number;
  rowHeader?: boolean;
}
export interface DataTableProps<Row> {
  caption: string;
  columns: readonly DataTableColumn<Row>[];
  rows: readonly Row[];
  rowKey: (row: Row) => string;
  rowTone?: (row: Row) => 'crit' | 'warn' | undefined;
  selectable?: boolean;
  onSelectionChange?: (keys: string[]) => void;
  bulkActions?: (keys: string[]) => ReactNode;
  emptyState?: ReactNode;
  pageSize?: number;
}

/** Native table semantics survive the container-based card layout. Pagination bounds DOM size. */
export function DataTable<Row>({
  caption,
  columns,
  rows,
  rowKey,
  rowTone,
  selectable,
  onSelectionChange,
  bulkActions,
  emptyState = 'No matching records.',
  pageSize = 100,
}: DataTableProps<Row>) {
  const id = useId();
  const [sort, setSort] = useState<{ id: string; desc: boolean } | null>(null);
  const [selected, setSelected] = useState<string[]>([]);
  const [page, setPage] = useState(0);
  const keys = new Set(rows.map(rowKey));
  const selection = selected.filter((key) => keys.has(key));
  const sorted = useMemo(() => {
    const col = columns.find((c) => c.id === sort?.id);
    if (!col?.sortValue || !sort) return rows;
    const get = col.sortValue;
    return [...rows].sort((a, b) => {
      const av = get(a),
        bv = get(b);
      const cmp =
        typeof av === 'number' && typeof bv === 'number'
          ? av - bv
          : String(av).localeCompare(String(bv), undefined, { numeric: true });
      return sort.desc ? -cmp : cmp;
    });
  }, [rows, columns, sort]);
  const size = Math.max(1, Math.min(500, pageSize));
  const pages = Math.max(1, Math.ceil(rows.length / size));
  const current = Math.min(page, pages - 1);
  const visible = sorted.slice(current * size, (current + 1) * size);
  function select(next: string[]) {
    setSelected(next);
    onSelectionChange?.(next);
  }
  const allVisible = visible.length > 0 && visible.every((r) => selection.includes(rowKey(r)));
  return (
    <div className="data-table">
      {columns.some((c) => c.sortValue) && (
        <div className="card-sort toolbar">
          <label className="inline-field">
            Sort {caption}
            <select
              value={sort?.id ?? ''}
              onChange={(e) => {
                setSort(e.target.value ? { id: e.target.value, desc: false } : null);
                setPage(0);
              }}
            >
              <option value="">Original order</option>
              {columns
                .filter((c) => c.sortValue)
                .map((c) => (
                  <option value={c.id} key={c.id}>
                    {c.label}
                  </option>
                ))}
            </select>
          </label>
          <button
            type="button"
            className="ghost"
            disabled={!sort}
            onClick={() => {
              if (sort) setSort({ ...sort, desc: !sort.desc });
            }}
          >
            {sort?.desc ? 'Descending' : 'Ascending'}
          </button>
        </div>
      )}
      {selectable && (
        <div className="toolbar" role="group" aria-label={`${caption} selection`}>
          <span aria-live="polite">{selection.length} selected</span>
          <label className="card-select">
            <input
              type="checkbox"
              checked={allVisible}
              onChange={() =>
                select(
                  allVisible
                    ? selection.filter((k) => !visible.some((r) => rowKey(r) === k))
                    : [...new Set([...selection, ...visible.map(rowKey)])],
                )
              }
            />
            Select visible rows
          </label>
          {selection.length > 0 && (
            <>
              <button type="button" className="ghost" onClick={() => select([])}>
                Clear selection
              </button>
              {bulkActions?.(selection)}
            </>
          )}
        </div>
      )}
      <TableRegion label={caption}>
        <table role="table" className="hc-data-table">
          <caption>{caption}</caption>
          <thead role="rowgroup">
            <tr role="row">
              {selectable && (
                <th scope="col" role="columnheader">
                  <input
                    type="checkbox"
                    aria-label={`Select visible ${caption}`}
                    checked={allVisible}
                    onChange={() =>
                      select(
                        allVisible
                          ? selection.filter((k) => !visible.some((r) => rowKey(r) === k))
                          : [...new Set([...selection, ...visible.map(rowKey)])],
                      )
                    }
                  />
                </th>
              )}
              {columns.map((col) => (
                <th
                  key={col.id}
                  id={`${id}-${col.id}`}
                  scope="col"
                  role="columnheader"
                  aria-sort={
                    col.sortValue
                      ? sort?.id === col.id
                        ? sort.desc
                          ? 'descending'
                          : 'ascending'
                        : 'none'
                      : undefined
                  }
                >
                  {col.sortValue && <span className="card-column-name">{col.label}</span>}
                  {col.sortValue ? (
                    <button
                      type="button"
                      className="ghost sort-button"
                      onClick={() => {
                        setSort({ id: col.id, desc: sort?.id === col.id && !sort.desc });
                        setPage(0);
                      }}
                    >
                      {col.label}
                      <span aria-hidden="true">{sort?.id === col.id ? (sort.desc ? ' ↓' : ' ↑') : ' ↕'}</span>
                    </button>
                  ) : (
                    col.label
                  )}
                </th>
              ))}
            </tr>
          </thead>
          <tbody role="rowgroup">
            {visible.map((row) => {
              const key = rowKey(row),
                tone = rowTone?.(row);
              return (
                <tr key={key} role="row" className={tone ? `row-${tone}` : undefined}>
                  {selectable && (
                    <td role="cell" data-label="Select">
                      <input
                        type="checkbox"
                        aria-label={`Select ${key}`}
                        checked={selection.includes(key)}
                        onChange={() =>
                          select(
                            selection.includes(key)
                              ? selection.filter((k) => k !== key)
                              : [...selection, key],
                          )
                        }
                      />
                    </td>
                  )}
                  {columns.map((col, index) => {
                    const header = col.rowHeader ?? index === 0;
                    const Cell = header ? 'th' : 'td';
                    return (
                      <Cell
                        key={col.id}
                        role={header ? 'rowheader' : 'cell'}
                        scope={header ? 'row' : undefined}
                        headers={`${id}-${col.id}`}
                        data-label={col.label}
                      >
                        <div className="cell-content">{col.render(row)}</div>
                      </Cell>
                    );
                  })}
                </tr>
              );
            })}
          </tbody>
        </table>
      </TableRegion>
      {rows.length === 0 && (
        <p className="empty" role="status">
          {emptyState}
        </p>
      )}
      {pages > 1 && (
        <div className="toolbar" aria-label={`${caption} pagination`}>
          <button
            type="button"
            className="ghost"
            disabled={current === 0}
            onClick={() => setPage(current - 1)}
          >
            Previous
          </button>
          <span aria-live="polite">
            Page {current + 1} of {pages} · {rows.length} records
          </span>
          <button
            type="button"
            className="ghost"
            disabled={current === pages - 1}
            onClick={() => setPage(current + 1)}
          >
            Next
          </button>
        </div>
      )}
    </div>
  );
}
