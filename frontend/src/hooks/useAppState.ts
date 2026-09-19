import { useCallback, useEffect, useRef, useState } from 'react';

import { api, eventsUrl, type AppState } from '../api/client';
import type { ServerClock } from '../lib/clock';

export type ConnectionStatus = 'connecting' | 'live' | 'polling' | 'offline';

interface UseAppState {
  state: AppState | null;
  status: ConnectionStatus;
  error: string | null;
  /** Re-fetch /api/state now (used after a mutation, for snappiness). */
  refresh: () => Promise<void>;
}

/** After this many consecutive SSE failures we give up and poll instead. */
const SSE_FAILURE_LIMIT = 3;
const POLL_INTERVAL_MS = 5_000;
const CLOCK_RESYNC_MS = 60_000;

/**
 * Loads /api/state and keeps it fresh.
 *
 * Realtime is Server-Sent Events: the server only ever pushes a nudge
 * ("something changed"), and we respond by re-fetching the whole state. The
 * payload is small, and "always re-read the truth" is far easier to reason
 * about than applying incremental patches correctly.
 *
 * If SSE cannot be established (a proxy that buffers, a corporate firewall),
 * we fall back to polling so the app still works, just less immediately.
 */
export function useAppState(clock: ServerClock): UseAppState {
  const [state, setState] = useState<AppState | null>(null);
  const [status, setStatus] = useState<ConnectionStatus>('connecting');
  const [error, setError] = useState<string | null>(null);

  // A ref, not state: changing it must not trigger a re-render or re-run the
  // effect that owns the EventSource.
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

  // Initial load plus the periodic clock re-sync.
  useEffect(() => {
    void refresh();
    void clock.sync();

    const id = window.setInterval(() => void clock.sync(), CLOCK_RESYNC_MS);
    return () => window.clearInterval(id);
  }, [clock, refresh]);

  // The SSE subscription, with a polling fallback.
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
        // A reconnect may have missed events while it was down, so re-read.
        void refresh();
      };

      // Every event type means the same thing to us: go and re-read state.
      const onAnyChange = () => void refresh();
      source.addEventListener('window.updated', onAnyChange);
      source.addEventListener('media.created', onAnyChange);
      source.addEventListener('sync.started', onAnyChange);
      source.addEventListener('sync.cancelled', onAnyChange);

      source.onerror = () => {
        failuresRef.current += 1;
        if (failuresRef.current >= SSE_FAILURE_LIMIT) {
          // EventSource retries forever by itself; stop it and poll instead.
          source?.close();
          source = null;
          startPolling();
        } else {
          setStatus('connecting');
        }
      };
    };

    connect();

    // useEffect cleanup: closing the stream and clearing the timer here is what
    // stops React's StrictMode double-mount (and any navigation) from leaking
    // a second connection.
    return () => {
      cancelled = true;
      source?.close();
      if (pollId !== null) window.clearInterval(pollId);
    };
  }, [refresh]);

  return { state, status, error, refresh };
}
