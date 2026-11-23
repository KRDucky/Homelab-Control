// SPDX-FileCopyrightText: 2026 The Homelab Control Authors
// SPDX-License-Identifier: GPL-3.0-or-later

import { useState } from 'react';
import { ApiError, dryRunPartitionPlan, type PartitionPlanReport } from '../api';
import { formatBytes } from '../format';
import type { StorageEdge, StorageNode } from '../storageModel';

const FILESYSTEMS = ['none', 'ext4', 'xfs', 'f2fs', 'btrfs', 'exfat', 'swap'] as const;

interface Row {
  start: string;
  size: string;
  fs: (typeof FILESYSTEMS)[number];
}

const emptyRow = (): Row => ({ start: '', size: '', fs: 'none' });

function allowedFilesystem(value: string | undefined): (typeof FILESYSTEMS)[number] {
  const name = (value ?? '').toLowerCase();
  return FILESYSTEMS.find((item) => item === name) ?? 'none';
}

function wholeBytes(value: string): number | null {
  const text = value.trim();
  if (!/^[0-9]+$/.test(text)) return null;
  const n = Number(text);
  return Number.isSafeInteger(n) ? n : null;
}

export function PartitionPlan({
  disk,
  nodes,
  edges,
  csrf,
  onUnauthorized,
}: {
  disk: StorageNode;
  nodes: Map<string, StorageNode>;
  edges: StorageEdge[];
  csrf: string;
  onUnauthorized: () => void;
}) {
  const [rows, setRows] = useState<Row[]>([emptyRow(), emptyRow(), emptyRow(), emptyRow()]);
  const [report, setReport] = useState<PartitionPlanReport | null>(null);
  const [message, setMessage] = useState('');
  const [busy, setBusy] = useState(false);

  const observed = edges
    .filter((edge) => edge.from === disk.id && edge.kind === 'contains')
    .map((edge) => nodes.get(edge.to))
    .filter((node): node is StorageNode => node !== undefined);
  const diskBytes = disk.size_bytes;
  const geometry =
    typeof diskBytes === 'number' &&
    diskBytes > 0 &&
    observed.length > 0 &&
    observed.every((node) => typeof node.start_bytes === 'number' && typeof node.size_bytes === 'number');

  async function run(parts: { start_bytes: number; size_bytes: number; filesystem: string }[]) {
    if (typeof diskBytes !== 'number' || diskBytes <= 0) {
      setMessage('This disk has no observed size, so the layout cannot be checked.');
      return;
    }
    setBusy(true);
    setMessage('');
    setReport(null);
    try {
      setReport(await dryRunPartitionPlan(csrf, { disk_bytes: diskBytes, parts }));
    } catch (err) {
      if (err instanceof ApiError && err.status === 401) onUnauthorized();
      setMessage(err instanceof ApiError ? err.message : 'Dry run failed.');
    } finally {
      setBusy(false);
    }
  }

  function checkForm() {
    const parts: { start_bytes: number; size_bytes: number; filesystem: string }[] = [];
    for (const row of rows) {
      if (row.start.trim() === '' && row.size.trim() === '') continue;
      const start = wholeBytes(row.start);
      const size = wholeBytes(row.size);
      if (start === null || size === null) {
        setReport(null);
        setMessage('Start and size must be whole numbers of bytes.');
        return;
      }
      parts.push({ start_bytes: start, size_bytes: size, filesystem: row.fs });
    }
    if (parts.length === 0) {
      setReport(null);
      setMessage('Enter a start and size for at least one slice.');
      return;
    }
    void run(parts);
  }

  const barLabel = geometry
    ? `Partition layout: ${observed
        .map((node) => `${node.name} from ${node.start_bytes} bytes, ${node.size_bytes} bytes`)
        .join('; ')}`
    : '';

  return (
    <div className="partition-plan">
      <h3 className="subhead">Partition layout</h3>
      <p className="banner">Dry run only. Nothing is partitioned or formatted.</p>
      {observed.length === 0 && <p>No partitions observed on this disk.</p>}
      {observed.length > 0 && (
        <ul aria-label="Observed partitions">
          {observed.map((node) => (
            <li key={node.id}>
              <span className="mono">{node.name}</span>
              {typeof node.start_bytes === 'number'
                ? ` starts at ${formatBytes(node.start_bytes)}`
                : ' start not observed'}
              {typeof node.size_bytes === 'number'
                ? `, ${formatBytes(node.size_bytes)}`
                : ', size not observed'}
            </li>
          ))}
        </ul>
      )}
      {geometry && typeof diskBytes === 'number' && (
        <div className="partition-bar" role="img" aria-label={barLabel}>
          {observed.map((node) => {
            const start = node.start_bytes ?? 0;
            const size = node.size_bytes ?? 0;
            const left = Math.min(100, (start / diskBytes) * 100);
            const width = Math.max(0, Math.min(100 - left, (size / diskBytes) * 100));
            return (
              <span
                key={node.id}
                style={{ left: `${left}%`, width: `${width}%` }}
                title={`${node.name} ${formatBytes(size)}`}
              />
            );
          })}
        </div>
      )}
      <p>
        <button
          type="button"
          disabled={!geometry || busy}
          onClick={() =>
            void run(
              observed.map((node) => ({
                start_bytes: node.start_bytes ?? 0,
                size_bytes: node.size_bytes ?? 0,
                filesystem: allowedFilesystem(node.filesystem_type),
              })),
            )
          }
        >
          Validate observed layout
        </button>
      </p>
      <form
        onSubmit={(event) => {
          event.preventDefault();
          checkForm();
        }}
      >
        <p className="muted">
          Check a proposal against the observed disk size
          {typeof diskBytes === 'number' ? ` (${formatBytes(diskBytes)})` : ''}. Empty rows are skipped.
        </p>
        <div className="partition-rows">
          {rows.map((row, index) => (
            <div className="partition-row" key={index}>
              <label>
                Slice {index + 1} start (bytes)
                <input
                  value={row.start}
                  inputMode="numeric"
                  onChange={(event) =>
                    setRows((current) =>
                      current.map((item, i) => (i === index ? { ...item, start: event.target.value } : item)),
                    )
                  }
                />
              </label>
              <label>
                Slice {index + 1} size (bytes)
                <input
                  value={row.size}
                  inputMode="numeric"
                  onChange={(event) =>
                    setRows((current) =>
                      current.map((item, i) => (i === index ? { ...item, size: event.target.value } : item)),
                    )
                  }
                />
              </label>
              <label>
                Slice {index + 1} filesystem
                <select
                  value={row.fs}
                  onChange={(event) =>
                    setRows((current) =>
                      current.map((item, i) =>
                        i === index ? { ...item, fs: allowedFilesystem(event.target.value) } : item,
                      ),
                    )
                  }
                >
                  {FILESYSTEMS.map((name) => (
                    <option key={name} value={name}>
                      {name}
                    </option>
                  ))}
                </select>
              </label>
            </div>
          ))}
        </div>
        <button type="submit" disabled={busy || typeof diskBytes !== 'number' || diskBytes <= 0}>
          {busy ? 'Checking…' : 'Check dry run'}
        </button>
      </form>
      {message && (
        <p className="form-error" role="alert">
          {message}
        </p>
      )}
      {report && (
        <div>
          <p role="status">{report.ok ? 'Layout passed the dry run.' : 'Layout was rejected.'}</p>
          {report.errors.length > 0 && (
            <ul aria-label="Dry run errors">
              {report.errors.map((error) => (
                <li key={error}>{error}</li>
              ))}
            </ul>
          )}
          <ul aria-label="Dry run steps">
            {report.steps.map((step) => (
              <li key={`${step.action}:${step.detail}`}>{step.detail}</li>
            ))}
          </ul>
        </div>
      )}
    </div>
  );
}
