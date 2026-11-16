// SPDX-FileCopyrightText: 2026 The Homelab Control Authors
// SPDX-License-Identifier: GPL-3.0-or-later
import { afterEach, expect, it, vi } from 'vitest';
import { act, cleanup, renderHook, waitFor } from '@testing-library/react';
import { ApiError, parseRetryAfterSeconds } from './api';
import { useData } from './useData';
afterEach(() => {
  cleanup();
  vi.useRealTimers();
});
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

it('parses Retry-After strictly to integer seconds', () => {
  expect(parseRetryAfterSeconds('5')).toBe(5);
  expect(parseRetryAfterSeconds('  12  ')).toBe(12);
  expect(parseRetryAfterSeconds('3600')).toBe(3600);
  expect(parseRetryAfterSeconds(null)).toBeUndefined();
  expect(parseRetryAfterSeconds('')).toBeUndefined();
  expect(parseRetryAfterSeconds('0')).toBeUndefined();
  expect(parseRetryAfterSeconds('-1')).toBeUndefined();
  expect(parseRetryAfterSeconds('1.5')).toBeUndefined();
  expect(parseRetryAfterSeconds('abc')).toBeUndefined();
  expect(parseRetryAfterSeconds('1e2')).toBeUndefined();
  expect(parseRetryAfterSeconds('0x10')).toBeUndefined();
  expect(parseRetryAfterSeconds('+5')).toBeUndefined();
  expect(parseRetryAfterSeconds('5.0')).toBeUndefined();
  expect(parseRetryAfterSeconds('3601')).toBeUndefined();
  expect(parseRetryAfterSeconds(String(2 ** 53))).toBeUndefined();
  expect(parseRetryAfterSeconds('1e20')).toBeUndefined();
});

it('honors a valid Retry-After before polling resumes', async () => {
  vi.useFakeTimers();
  const fetcher = vi
    .fn()
    .mockResolvedValueOnce(1)
    .mockRejectedValueOnce(new ApiError(429, 'rate_limited', 'slow down', 3))
    .mockResolvedValue(2);
  const { result } = renderHook(() => useData(fetcher, { intervalMs: 1000 }));
  await act(async () => {
    await vi.advanceTimersByTimeAsync(0);
  });
  expect(result.current.data).toBe(1);
  // Interval tick at 1s hits the 429 (Retry-After 3s → cooldown until 4s).
  await act(async () => {
    await vi.advanceTimersByTimeAsync(1000);
  });
  expect(fetcher).toHaveBeenCalledTimes(2);
  expect(result.current.error).toBeInstanceOf(ApiError);
  expect(result.current.live.state).toBe('stale');
  // Ticks at 2s and 3s are inside the cooldown and must not fetch.
  await act(async () => {
    await vi.advanceTimersByTimeAsync(1000);
  });
  await act(async () => {
    await vi.advanceTimersByTimeAsync(1000);
  });
  expect(fetcher).toHaveBeenCalledTimes(2);
  // Tick at 4s is after the cooldown and recovers.
  await act(async () => {
    await vi.advanceTimersByTimeAsync(1000);
  });
  expect(fetcher).toHaveBeenCalledTimes(3);
  expect(result.current.data).toBe(2);
  expect(result.current.error).toBeNull();
});

it('uses bounded fallback backoff when Retry-After is missing', async () => {
  vi.useFakeTimers();
  const fetcher = vi
    .fn()
    .mockResolvedValueOnce(1)
    .mockRejectedValueOnce(new ApiError(429, 'rate_limited', 'slow down'))
    .mockResolvedValue(2);
  const { result } = renderHook(() => useData(fetcher, { intervalMs: 500 }));
  await act(async () => {
    await vi.advanceTimersByTimeAsync(0);
  });
  // Tick at 0.5s hits the 429; first fallback delay is 1s → cooldown until 1.5s.
  await act(async () => {
    await vi.advanceTimersByTimeAsync(500);
  });
  expect(fetcher).toHaveBeenCalledTimes(2);
  expect(result.current.error).toBeInstanceOf(ApiError);
  // Tick at 1.0s is inside the fallback cooldown and must not fetch.
  await act(async () => {
    await vi.advanceTimersByTimeAsync(500);
  });
  expect(fetcher).toHaveBeenCalledTimes(2);
  // Tick at 1.5s recovers.
  await act(async () => {
    await vi.advanceTimersByTimeAsync(500);
  });
  expect(fetcher).toHaveBeenCalledTimes(3);
  expect(result.current.data).toBe(2);
});

it('resumes normal interval polling after the backoff', async () => {
  vi.useFakeTimers();
  const fetcher = vi
    .fn()
    .mockResolvedValueOnce(1)
    .mockRejectedValueOnce(new ApiError(429, 'rate_limited', 'slow down', 2))
    .mockResolvedValue(2);
  const { result } = renderHook(() => useData(fetcher, { intervalMs: 1000 }));
  await act(async () => {
    await vi.advanceTimersByTimeAsync(0);
  });
  // Interval tick at 1s hits the 429 (Retry-After 2s → cooldown until 3s).
  await act(async () => {
    await vi.advanceTimersByTimeAsync(1000);
  });
  expect(fetcher).toHaveBeenCalledTimes(2);
  // Tick at 2s is skipped (still cooling down).
  await act(async () => {
    await vi.advanceTimersByTimeAsync(1000);
  });
  expect(fetcher).toHaveBeenCalledTimes(2);
  // Tick at 3s recovers.
  await act(async () => {
    await vi.advanceTimersByTimeAsync(1000);
  });
  expect(fetcher).toHaveBeenCalledTimes(3);
  expect(result.current.data).toBe(2);
  // Tick at 4s fetches again, proving normal polling resumed.
  await act(async () => {
    await vi.advanceTimersByTimeAsync(1000);
  });
  expect(fetcher).toHaveBeenCalledTimes(4);
});

it('grows and resets the fallback backoff across consecutive rate limits', async () => {
  vi.useFakeTimers();
  const limited = () => new ApiError(429, 'rate_limited', 'slow down');
  const fetcher = vi
    .fn()
    .mockResolvedValueOnce(1) // mount
    .mockRejectedValueOnce(limited()) // 429 → fallback 1s
    .mockRejectedValueOnce(limited()) // 429 → fallback 2s
    .mockResolvedValueOnce(2) // success → reset
    .mockRejectedValueOnce(limited()) // 429 → fallback 1s again
    .mockResolvedValueOnce(3); // success
  const { result } = renderHook(() => useData(fetcher, { intervalMs: 1000 }));
  await act(async () => {
    await vi.advanceTimersByTimeAsync(0);
  });
  // 1s: first 429 (fallback 1s → next fetch at 2s).
  await act(async () => {
    await vi.advanceTimersByTimeAsync(1000);
  });
  expect(fetcher).toHaveBeenCalledTimes(2);
  // 2s: second 429 (fallback 2s → cooldown until 4s).
  await act(async () => {
    await vi.advanceTimersByTimeAsync(1000);
  });
  expect(fetcher).toHaveBeenCalledTimes(3);
  // 3s: skipped (still inside the 2s fallback).
  await act(async () => {
    await vi.advanceTimersByTimeAsync(1000);
  });
  expect(fetcher).toHaveBeenCalledTimes(3);
  // 4s: success resets the backoff.
  await act(async () => {
    await vi.advanceTimersByTimeAsync(1000);
  });
  expect(fetcher).toHaveBeenCalledTimes(4);
  expect(result.current.data).toBe(2);
  expect(result.current.error).toBeNull();
  // 5s: 429 again — reset means fallback 1s (not 4s) → next fetch at 6s.
  await act(async () => {
    await vi.advanceTimersByTimeAsync(1000);
  });
  expect(fetcher).toHaveBeenCalledTimes(5);
  // 6s: recovers, proving the backoff was reset to 1s.
  await act(async () => {
    await vi.advanceTimersByTimeAsync(1000);
  });
  expect(fetcher).toHaveBeenCalledTimes(6);
  expect(result.current.data).toBe(3);
});

it('does not auto-retry without an interval', async () => {
  vi.useFakeTimers();
  const fetcher = vi
    .fn()
    .mockResolvedValueOnce(1)
    .mockRejectedValueOnce(new ApiError(429, 'rate_limited', 'slow down', 1))
    .mockResolvedValueOnce(2);
  const { result } = renderHook(() => useData(fetcher));
  await act(async () => {
    await vi.advanceTimersByTimeAsync(0);
  });
  expect(result.current.data).toBe(1);
  act(() => {
    result.current.reload();
  });
  await act(async () => {
    await vi.advanceTimersByTimeAsync(0);
  });
  expect(fetcher).toHaveBeenCalledTimes(2);
  expect(result.current.error).toBeInstanceOf(ApiError);
  // No interval: past the cooldown, nothing refetches on its own.
  await act(async () => {
    await vi.advanceTimersByTimeAsync(5000);
  });
  expect(fetcher).toHaveBeenCalledTimes(2);
});

it('manual reload during cooldown retries after it elapses', async () => {
  vi.useFakeTimers();
  const fetcher = vi
    .fn()
    .mockResolvedValueOnce(1)
    .mockRejectedValueOnce(new ApiError(429, 'rate_limited', 'slow down', 2))
    .mockResolvedValueOnce(2);
  const { result } = renderHook(() => useData(fetcher));
  await act(async () => {
    await vi.advanceTimersByTimeAsync(0);
  });
  act(() => {
    result.current.reload();
  });
  await act(async () => {
    await vi.advanceTimersByTimeAsync(0);
  });
  expect(fetcher).toHaveBeenCalledTimes(2);
  expect(result.current.error).toBeInstanceOf(ApiError);
  // A reload while still cooling down is deferred, not dropped.
  act(() => {
    result.current.reload();
  });
  await act(async () => {
    await vi.advanceTimersByTimeAsync(1999);
  });
  expect(fetcher).toHaveBeenCalledTimes(2);
  await act(async () => {
    await vi.advanceTimersByTimeAsync(1);
  });
  expect(fetcher).toHaveBeenCalledTimes(3);
  expect(result.current.data).toBe(2);
  expect(result.current.error).toBeNull();
});

it('backs off on 503 service_busy and honors Retry-After', async () => {
  vi.useFakeTimers();
  const fetcher = vi
    .fn()
    .mockResolvedValueOnce(1)
    .mockRejectedValueOnce(new ApiError(503, 'service_busy', 'busy', 2))
    .mockResolvedValue(2);
  const { result } = renderHook(() => useData(fetcher, { intervalMs: 1000 }));
  await act(async () => {
    await vi.advanceTimersByTimeAsync(0);
  });
  // Tick at 1s → 503 service_busy (Retry-After 2s → cooldown until 3s).
  await act(async () => {
    await vi.advanceTimersByTimeAsync(1000);
  });
  expect(fetcher).toHaveBeenCalledTimes(2);
  expect(result.current.error).toBeInstanceOf(ApiError);
  expect(result.current.live.state).toBe('stale');
  // Tick at 2s is inside the cooldown and must not fetch.
  await act(async () => {
    await vi.advanceTimersByTimeAsync(1000);
  });
  expect(fetcher).toHaveBeenCalledTimes(2);
  // Tick at 3s recovers.
  await act(async () => {
    await vi.advanceTimersByTimeAsync(1000);
  });
  expect(fetcher).toHaveBeenCalledTimes(3);
  expect(result.current.data).toBe(2);
  expect(result.current.error).toBeNull();
});

it('uses fallback backoff on 503 service_busy without Retry-After', async () => {
  vi.useFakeTimers();
  const fetcher = vi
    .fn()
    .mockResolvedValueOnce(1)
    .mockRejectedValueOnce(new ApiError(503, 'service_busy', 'busy'))
    .mockResolvedValue(2);
  const { result } = renderHook(() => useData(fetcher, { intervalMs: 500 }));
  await act(async () => {
    await vi.advanceTimersByTimeAsync(0);
  });
  // Tick at 0.5s → 503 service_busy (fallback 1s → cooldown until 1.5s).
  await act(async () => {
    await vi.advanceTimersByTimeAsync(500);
  });
  expect(fetcher).toHaveBeenCalledTimes(2);
  expect(result.current.error).toBeInstanceOf(ApiError);
  // Tick at 1.0s is inside the fallback cooldown and must not fetch.
  await act(async () => {
    await vi.advanceTimersByTimeAsync(500);
  });
  expect(fetcher).toHaveBeenCalledTimes(2);
  // Tick at 1.5s recovers.
  await act(async () => {
    await vi.advanceTimersByTimeAsync(500);
  });
  expect(fetcher).toHaveBeenCalledTimes(3);
  expect(result.current.data).toBe(2);
});

it('does not back off on an unrelated 503', async () => {
  vi.useFakeTimers();
  const fetcher = vi
    .fn()
    .mockResolvedValueOnce(1)
    .mockRejectedValueOnce(new ApiError(503, 'http_error', 'overloaded'))
    .mockResolvedValue(2);
  const { result } = renderHook(() => useData(fetcher, { intervalMs: 1000 }));
  await act(async () => {
    await vi.advanceTimersByTimeAsync(0);
  });
  // Tick at 1s → unrelated 503.
  await act(async () => {
    await vi.advanceTimersByTimeAsync(1000);
  });
  expect(fetcher).toHaveBeenCalledTimes(2);
  expect(result.current.error).toBeInstanceOf(ApiError);
  // Tick at 2s fetches immediately (no cooldown).
  await act(async () => {
    await vi.advanceTimersByTimeAsync(1000);
  });
  expect(fetcher).toHaveBeenCalledTimes(3);
  expect(result.current.data).toBe(2);
});

it('does not double-fetch when an interval recovers while a reload is pending', async () => {
  vi.useFakeTimers();
  const fetcher = vi
    .fn()
    .mockResolvedValueOnce(1)
    .mockRejectedValueOnce(new ApiError(429, 'rate_limited', 'slow down', 2))
    .mockResolvedValue(2);
  const { result } = renderHook(() => useData(fetcher, { intervalMs: 1000 }));
  await act(async () => {
    await vi.advanceTimersByTimeAsync(0);
  });
  // Tick at 1s → 429 (cooldown until 3s).
  await act(async () => {
    await vi.advanceTimersByTimeAsync(1000);
  });
  expect(fetcher).toHaveBeenCalledTimes(2);
  // A reload during cooldown must not schedule a second deferred fetch (the interval recovers).
  act(() => {
    result.current.reload();
  });
  await act(async () => {
    await vi.advanceTimersByTimeAsync(2000);
  });
  // Only the interval recovery at 3s; no extra fetch from a stale wake.
  expect(fetcher).toHaveBeenCalledTimes(3);
  expect(result.current.data).toBe(2);
  // Next tick at 4s is the normal poll.
  await act(async () => {
    await vi.advanceTimersByTimeAsync(1000);
  });
  expect(fetcher).toHaveBeenCalledTimes(4);
});
