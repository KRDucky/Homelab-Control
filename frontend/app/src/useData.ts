// SPDX-FileCopyrightText: 2026 The Homelab Control Authors
//
// SPDX-License-Identifier: GPL-3.0-or-later

import { useCallback, useEffect, useRef, useState } from 'react';
import type { LiveState } from './ui';
import { ApiError } from './api';

const FALLBACK_BASE_MS = 1000;
const FALLBACK_MAX_MS = 30_000;

/** Bounded fallback backoff for a retryable busy/rate-limit whose Retry-After
 * was missing or malformed. Grows 1s, 2s, 4s, … capped at 30s, keyed by the
 * number of consecutive fallbacks since the last success. */
function fallbackBackoffMs(attempts: number): number {
  return Math.min(FALLBACK_MAX_MS, FALLBACK_BASE_MS * 2 ** Math.min(attempts, 10));
}

/** A 429 or a 503 `service_busy` is a transient "try again later" signal that
 * should back off instead of retrying immediately. */
function isRetryableBusy(err: unknown): err is ApiError {
  return (
    err instanceof ApiError && (err.status === 429 || (err.status === 503 && err.code === 'service_busy'))
  );
}

export interface DataState<T> {
  data: T | null;
  error: ApiError | Error | null;
  loading: boolean;
  reload: () => void;
  live: { state: LiveState; updated?: string; onRetry: () => void };
}

/**
 * Fetches on mount and optionally on an interval. A 401 is surfaced through
 * onUnauthorized so the shell can return to the login screen.
 */
export function useData<T>(
  fetcher: () => Promise<T>,
  opts: { intervalMs?: number; onUnauthorized?: () => void } = {},
): DataState<T> {
  const [data, setData] = useState<T | null>(null);
  const [error, setError] = useState<ApiError | Error | null>(null);
  const [loading, setLoading] = useState(true);
  const [updated, setUpdated] = useState<string>();
  const [paused, setPaused] = useState(false);
  const generation = useRef(0);
  const busy = useRef(false);
  const cooldownUntil = useRef(0);
  const backoffAttempts = useRef(0);
  const wakeTimer = useRef<number | undefined>(undefined);
  const { intervalMs, onUnauthorized } = opts;

  const load = useCallback(async () => {
    if (busy.current || Date.now() < cooldownUntil.current) return;
    window.clearTimeout(wakeTimer.current);
    busy.current = true;
    const mine = generation.current;
    setLoading(true);
    try {
      const d = await fetcher();
      if (mine !== generation.current) return;
      setData(d);
      setUpdated(new Date().toISOString());
      setError(null);
      cooldownUntil.current = 0;
      backoffAttempts.current = 0;
    } catch (err) {
      if (mine !== generation.current) return;
      if (err instanceof ApiError && err.status === 401) onUnauthorized?.();
      // Never retain protected data after access is revoked.
      if (err instanceof ApiError && (err.status === 401 || err.status === 403)) setData(null);
      setError(err instanceof Error ? err : new Error(String(err)));
      if (isRetryableBusy(err)) {
        const delayMs =
          typeof err.retryAfterSeconds === 'number'
            ? err.retryAfterSeconds * 1000
            : fallbackBackoffMs(backoffAttempts.current++);
        cooldownUntil.current = Date.now() + delayMs;
      }
    } finally {
      if (mine === generation.current) {
        busy.current = false;
        setLoading(false);
      }
    }
  }, [fetcher, onUnauthorized]);

  useEffect(() => {
    const mountedGeneration = generation.current;
    const first = window.setTimeout(() => void load(), 0);
    const poll = () => {
      const hidden = document.visibilityState === 'hidden';
      setPaused(hidden);
      if (!hidden) void load();
    };
    const t = intervalMs ? window.setInterval(poll, intervalMs) : undefined;
    document.addEventListener('visibilitychange', poll);
    return () => {
      generation.current = mountedGeneration + 1;
      busy.current = false;
      window.clearTimeout(first);
      window.clearTimeout(wakeTimer.current);
      if (t) window.clearInterval(t);
      document.removeEventListener('visibilitychange', poll);
    };
  }, [load, intervalMs]);

  const reload = () => {
    if (Date.now() < cooldownUntil.current) {
      // With an interval, the next poll after the cooldown already recovers;
      // only schedule a deferred wake when there is no interval to do it.
      if (!intervalMs) {
        window.clearTimeout(wakeTimer.current);
        wakeTimer.current = window.setTimeout(() => void load(), cooldownUntil.current - Date.now());
      }
      return;
    }
    void load();
  };
  const state: LiveState = error
    ? data
      ? 'stale'
      : 'disconnected'
    : paused
      ? 'paused'
      : loading
        ? 'refreshing'
        : 'live';
  return { data, error, loading, reload, live: { state, ...(updated ? { updated } : {}), onRetry: reload } };
}

/** Plain-language explanation for an API failure. */
export function explain(err: Error): string {
  if (err instanceof ApiError) {
    if (err.code === 'broker_unavailable') return err.message;
    if (err.code === 'service_busy')
      return 'The server is busy handling other requests. Please wait and try again.';
    if (err.status === 429) return 'The server is rate-limiting requests. Please wait and try again.';
    if (err.status === 403) return 'Your account does not have permission to view this page.';
    if (err.status === 503 || err.status === 504) return err.message;
    if (err.status === 502)
      return 'The privileged helper could not complete the request. Check its journal: journalctl -u homelab-control-broker';
  }
  return 'The server did not respond. It may be restarting; this page retries automatically.';
}
