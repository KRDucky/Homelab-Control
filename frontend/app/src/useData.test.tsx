// SPDX-FileCopyrightText: 2026 The Homelab Control Authors
// SPDX-License-Identifier: GPL-3.0-or-later
import { afterEach, expect, it, vi } from 'vitest';
import { act, cleanup, renderHook, waitFor } from '@testing-library/react';
import { ApiError } from './api';
import { useData } from './useData';
afterEach(cleanup);
it('keeps stale readings after transport failure and clears them after permission loss', async () => {
  const fetcher = vi
    .fn()
    .mockResolvedValueOnce({ value: 7 })
    .mockRejectedValueOnce(new Error('Disconnected'))
    .mockRejectedValueOnce(new ApiError(403, 'forbidden', 'No scope'));
  const { result } = renderHook(() => useData<{ value: number }>(fetcher));
  await waitFor(() => expect(result.current.data?.value).toBe(7));
  expect(result.current.live.state).toBe('live');
  await act(async () => result.current.reload());
  await waitFor(() => expect(result.current.live.state).toBe('stale'));
  expect(result.current.data?.value).toBe(7);
  await act(async () => result.current.reload());
  await waitFor(() => expect(result.current.data).toBeNull());
  expect(result.current.live.state).toBe('disconnected');
});
it('does not overlap a pending refresh', async () => {
  let finish: ((v: number) => void) | undefined;
  const fetcher = vi.fn(
    () =>
      new Promise<number>((resolve) => {
        finish = resolve;
      }),
  );
  const { result } = renderHook(() => useData(fetcher));
  await waitFor(() => expect(fetcher).toHaveBeenCalledTimes(1));
  act(() => {
    result.current.reload();
    result.current.reload();
  });
  expect(fetcher).toHaveBeenCalledTimes(1);
  await act(async () => finish?.(42));
  expect(result.current.data).toBe(42);
});
