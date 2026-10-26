// SPDX-FileCopyrightText: 2026 The Homelab Control Authors
//
// SPDX-License-Identifier: GPL-3.0-or-later

import { useCallback, useEffect, useRef, useState } from 'react';
import type { LiveState } from './ui';
import { ApiError } from './api';

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
  const { intervalMs, onUnauthorized } = opts;

  const load = useCallback(async () => {
    if (busy.current) return;
    busy.current = true;
    const mine = generation.current;
    setLoading(true);
    try {
      const d = await fetcher();
      if (mine !== generation.current) return;
      setData(d);
      setUpdated(new Date().toISOString());
      setError(null);
    } catch (err) {
      if (mine !== generation.current) return;
      if (err instanceof ApiError && err.status === 401) onUnauthorized?.();
      // Never retain protected data after access is revoked.
      if (err instanceof ApiError && (err.status === 401 || err.status === 403)) setData(null);
      setError(err instanceof Error ? err : new Error(String(err)));
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
      if (t) window.clearInterval(t);
      document.removeEventListener('visibilitychange', poll);
    };
  }, [load, intervalMs]);

  const reload = () => void load();
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
    if (err.status === 403) return 'Your account does not have permission to view this page.';
    if (err.status === 503 || err.status === 504) return err.message;
    if (err.status === 502)
      return 'The privileged helper could not complete the request. Check its journal: journalctl -u homelab-control-broker';
  }
  return 'The server did not respond. It may be restarting; this page retries automatically.';
}
