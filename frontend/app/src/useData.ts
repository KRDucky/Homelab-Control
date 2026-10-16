// SPDX-FileCopyrightText: 2026 The Homelab Control Authors
//
// SPDX-License-Identifier: GPL-3.0-or-later

import { useCallback, useEffect, useState } from 'react';
import { ApiError } from './api';

export interface DataState<T> {
  data: T | null;
  error: ApiError | Error | null;
  loading: boolean;
  reload: () => void;
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
  const { intervalMs, onUnauthorized } = opts;

  const load = useCallback(async () => {
    try {
      const d = await fetcher();
      setData(d);
      setError(null);
    } catch (err) {
      if (err instanceof ApiError && err.status === 401) onUnauthorized?.();
      setError(err instanceof Error ? err : new Error(String(err)));
    } finally {
      setLoading(false);
    }
  }, [fetcher, onUnauthorized]);

  useEffect(() => {
    const first = window.setTimeout(() => void load(), 0);
    const t = intervalMs ? window.setInterval(() => void load(), intervalMs) : undefined;
    return () => {
      window.clearTimeout(first);
      if (t) window.clearInterval(t);
    };
  }, [load, intervalMs]);

  return { data, error, loading, reload: () => void load() };
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
