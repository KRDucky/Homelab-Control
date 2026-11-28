// SPDX-FileCopyrightText: 2026 The Homelab Control Authors
// SPDX-License-Identifier: GPL-3.0-or-later

import { afterEach, expect, it, vi } from 'vitest';
import { act, cleanup, fireEvent, render, screen, waitFor } from '@testing-library/react';
import { PartitionPlan } from './PartitionPlan';
import type { StorageNode } from '../storageModel';

const disk: StorageNode = {
  id: 'disk-example',
  kind: 'disk',
  name: 'sda',
  identity_status: 'observed',
  size_bytes: 8 * 1048576,
  evidence: [{ source: 'test', confidence: 'observed' }],
};

function mount() {
  render(
    <PartitionPlan
      disk={disk}
      nodes={new Map()}
      edges={[]}
      csrf="test-csrf"
      onUnauthorized={() => undefined}
    />,
  );
  fireEvent.change(screen.getByLabelText('Slice 1 start (bytes)'), { target: { value: '1048576' } });
  fireEvent.change(screen.getByLabelText('Slice 1 size (bytes)'), { target: { value: '1048576' } });
}

function successfulReport() {
  return new Response(
    JSON.stringify({ ok: true, errors: [], steps: [], executes: false }),
    { status: 200, headers: { 'Content-Type': 'application/json' } },
  );
}

afterEach(() => {
  cleanup();
  vi.unstubAllGlobals();
  vi.restoreAllMocks();
});

it('clears a successful dry run as soon as the proposed layout is edited', async () => {
  vi.stubGlobal('fetch', vi.fn(async () => successfulReport()));
  mount();
  fireEvent.click(screen.getByRole('button', { name: 'Check dry run' }));
  expect(await screen.findByText('Layout passed the dry run.')).toBeTruthy();
  fireEvent.change(screen.getByLabelText('Slice 1 size (bytes)'), { target: { value: '2097152' } });
  expect(screen.queryByText('Layout passed the dry run.')).toBeNull();
});

it('does not display a late validation response for inputs changed while it was running', async () => {
  let finish: (response: Response) => void = () => { throw new Error('not started'); };
  const requested = new Promise<Response>((resolve) => { finish = resolve; });
  const fetcher = vi.fn(() => requested);
  vi.stubGlobal('fetch', fetcher);
  mount();
  fireEvent.click(screen.getByRole('button', { name: 'Check dry run' }));
  await waitFor(() => expect(fetcher).toHaveBeenCalledTimes(1));
  fireEvent.change(screen.getByLabelText('Slice 1 filesystem'), { target: { value: 'xfs' } });
  await act(async () => { finish(successfulReport()); await requested; });
  expect(screen.queryByText('Layout passed the dry run.')).toBeNull();
  expect((screen.getByRole('button', { name: 'Check dry run' }) as HTMLButtonElement).disabled).toBe(false);
});
