import { useCallback, useEffect, useMemo, useState } from 'react';
import { Link, Route, Routes, useParams, useSearchParams } from 'react-router-dom';

import { API_BASE_URL, api, type Media } from './api/client';
import { AddMediaForm } from './components/AddMediaForm';
import { MediaLibrary } from './components/MediaLibrary';
import { SyncControls } from './components/SyncControls';
import { WindowGrid } from './components/WindowGrid';
import { WindowPlayer } from './components/WindowPlayer';
import { useAppState, type ConnectionStatus } from './hooks/useAppState';
import { useNow } from './hooks/useNow';
import { ServerClock, type ClockInfo } from './lib/clock';

const clock = new ServerClock(API_BASE_URL);

export default function App() {
  return (
    <Routes>
      <Route path="/" element={<Dashboard />} />
      <Route path="/window/:id" element={<SingleWindow />} />
      <Route path="*" element={<NotFound />} />
    </Routes>
  );
}

function useClockInfo(): ClockInfo {
  const [info, setInfo] = useState<ClockInfo>(() => clock.info());
  useEffect(() => clock.subscribe(setInfo), []);
  return info;
}

function useDebugFlag(): boolean {
  const [params] = useSearchParams();
  return params.get('debug') === '1';
}

function Dashboard() {
  const debug = useDebugFlag();
  const { state, status, error, refresh } = useAppState(clock);
  const now = useNow(clock);
  const [actionError, setActionError] = useState<string | null>(null);

  const mediaById = useMediaIndex(state?.media);

  const deleteItem = useCallback(
    async (windowId: string, itemId: number) => {
      try {
        await api.deleteItem(windowId, itemId);
        await refresh();
      } catch (e) {
        setActionError(e instanceof Error ? e.message : String(e));
      }
    },
    [refresh],
  );

  const onDone = useCallback(() => {
    setActionError(null);
    void refresh();
  }, [refresh]);

  return (
    <div className="app">
      <TopBar status={status} debug={debug} cycleMs={state?.cycleMs} now={now} />

      {(error || actionError) && (
        <p className="alert" role="alert">
          {actionError ?? error}
          <button type="button" className="alert__close" onClick={() => setActionError(null)}>
            ×
          </button>
        </p>
      )}

      {!state ? (
        <p className="empty">Loading…</p>
      ) : (
        <main className="layout">
          <WindowGrid
            windows={state.windows}
            mediaById={mediaById}
            cycleMs={state.cycleMs}
            activeSync={state.activeSync}
            now={now}
            clock={clock}
            debug={debug}
            onDeleteItem={deleteItem}
          />

          <aside className="side">
            <SyncControls
              media={state.media}
              activeSync={state.activeSync}
              now={now}
              onDone={onDone}
              onError={setActionError}
            />
            <AddMediaForm
              windows={state.windows}
              media={state.media}
              onDone={onDone}
              onError={setActionError}
            />
            <MediaLibrary media={state.media} onDone={onDone} onError={setActionError} />
          </aside>
        </main>
      )}
    </div>
  );
}

function SingleWindow() {
  const { id } = useParams<{ id: string }>();
  const debug = useDebugFlag();
  const { state, status } = useAppState(clock);
  const now = useNow(clock);
  const mediaById = useMediaIndex(state?.media);

  const win = state?.windows.find((w) => w.id === id);

  return (
    <div className="app app--solo">
      <div className="solo__bar">
        <Link to="/" className="solo__back">
          ← all windows
        </Link>
        <span className="solo__name">{win ? `${win.id} — ${win.name}` : id}</span>
        <StatusDot status={status} />
      </div>

      {!state ? (
        <p className="empty">Loading…</p>
      ) : !win ? (
        <p className="empty">No window called “{id}”.</p>
      ) : (
        <WindowPlayer
          win={win}
          mediaById={mediaById}
          cycleMs={state.cycleMs}
          activeSync={state.activeSync}
          now={now}
          clock={clock}
          debug={debug}
          fullscreen
        />
      )}
    </div>
  );
}

function NotFound() {
  return (
    <div className="app">
      <p className="empty">
        Nothing here. <Link to="/">Back to the dashboard</Link>.
      </p>
    </div>
  );
}

function TopBar({
  status,
  debug,
  cycleMs,
  now,
}: {
  status: ConnectionStatus;
  debug: boolean;
  cycleMs: number | undefined;
  now: number;
}) {
  const info = useClockInfo();

  return (
    <header className="topbar">
      <h1 className="topbar__title">Media Sequencer</h1>

      <div className="topbar__meta">
        {debug && cycleMs !== undefined && (
          <span className="chip" title="Server clock estimate">
            offset {info.offsetMs >= 0 ? '+' : ''}
            {info.offsetMs}ms · rtt {info.rttMs < 0 ? '—' : `${info.rttMs}ms`} · cycle {cycleMs / 1000}s ·{' '}
            {new Date(now).toISOString().slice(11, 23)}
          </span>
        )}
        <StatusDot status={status} />
      </div>
    </header>
  );
}

function StatusDot({ status }: { status: ConnectionStatus }) {
  const label: Record<ConnectionStatus, string> = {
    connecting: 'connecting…',
    live: 'live (SSE)',
    polling: 'polling (SSE unavailable)',
    offline: 'offline',
  };
  return (
    <span className={`status status--${status}`} title={label[status]}>
      <span className="status__dot" />
      {label[status]}
    </span>
  );
}

function useMediaIndex(media: Media[] | undefined): Map<string, Media> {
  return useMemo(() => new Map((media ?? []).map((m) => [m.id, m])), [media]);
}
