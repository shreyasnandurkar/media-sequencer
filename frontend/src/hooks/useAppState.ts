import { useCallback, useEffect, useRef, useState } from 'react';

import { api, eventsUrl, type AppState } from '../api/client';
import type { ServerClock } from '../lib/clock';

export type ConnectionStatus = 'connecting' | 'live' | 'polling' | 'offline';

interface UseAppState {
  state: AppState | null;
  status: ConnectionStatus;
  error: string | null;

  refresh: () => Promise<void>;
}

const SSE_FAILURE_LIMIT = 3;
const POLL_INTERVAL_MS = 5_000;
const CLOCK_RESYNC_MS = 60_000;

export function useAppState(clock: ServerClock): UseAppState {
  const [state, setState] = useState<AppState | null>(null);
  const [status, setStatus] = useState<ConnectionStatus>('connecting');
  const [error, setError] = useState<string | null>(null);

  const failuresRef = useRef(0);

  const refresh = useCallback(async () => {
    try {
      const next = await api.state();
      clock.seed(next.serverTimeMs);
      setState(next);
      setError(null);
    } catch (e) {
      setError(e instanceof Error ? e.message : String(e));
      setStatus((s) => (s === 'live' ? s : 'offline'));
    }
  }, [clock]);

  useEffect(() => {
    void refresh();
    void clock.sync();

    const id = window.setInterval(() => void clock.sync(), CLOCK_RESYNC_MS);
    return () => window.clearInterval(id);
  }, [clock, refresh]);

  useEffect(() => {
    let source: EventSource | null = null;
    let pollId: number | null = null;
    let cancelled = false;

    const startPolling = () => {
      if (pollId !== null || cancelled) return;
      setStatus('polling');
      pollId = window.setInterval(() => void refresh(), POLL_INTERVAL_MS);
    };

    const connect = () => {
      if (cancelled) return;
      source = new EventSource(eventsUrl);

      source.onopen = () => {
        failuresRef.current = 0;
        setStatus('live');

        void refresh();
      };

      const onAnyChange = () => void refresh();
      source.addEventListener('window.updated', onAnyChange);
      source.addEventListener('media.created', onAnyChange);
      source.addEventListener('sync.started', onAnyChange);
      source.addEventListener('sync.cancelled', onAnyChange);

      source.onerror = () => {
        failuresRef.current += 1;
        if (failuresRef.current >= SSE_FAILURE_LIMIT) {

          source?.close();
          source = null;
          startPolling();
        } else {
          setStatus('connecting');
        }
      };
    };

    connect();

    return () => {
      cancelled = true;
      source?.close();
      if (pollId !== null) window.clearInterval(pollId);
    };
  }, [refresh]);

  return { state, status, error, refresh };
}
