import { useState } from 'react';

import { api, type Media, type SyncState } from '../api/client';

interface Props {
  media: Media[];
  activeSync: SyncState | null;
  now: number;
  onDone: () => void;
  onError: (message: string) => void;
}

export function SyncControls({ media, activeSync, now, onDone, onError }: Props) {
  const [mediaId, setMediaId] = useState('');
  const [durationSec, setDurationSec] = useState('');
  const [busy, setBusy] = useState(false);

  const effectiveMedia = mediaId || media[0]?.id || '';
  const pending = activeSync !== null && now < activeSync.startAt;
  const live = activeSync !== null && now >= activeSync.startAt && now < activeSync.endAt;

  async function start(e: React.FormEvent) {
    e.preventDefault();
    if (!effectiveMedia) return;

    setBusy(true);
    try {
      const secs = durationSec.trim();
      const durationMs = secs === '' ? undefined : Math.round(Number(secs) * 1000);
      if (durationMs !== undefined && (!Number.isFinite(durationMs) || durationMs <= 0)) {
        onError('Sync duration must be greater than 0.');
        return;
      }
      await api.startSync({ mediaId: effectiveMedia, durationMs });
      onDone();
    } catch (err) {
      onError(err instanceof Error ? err.message : String(err));
    } finally {
      setBusy(false);
    }
  }

  async function cancel() {
    setBusy(true);
    try {
      await api.cancelSync();
      onDone();
    } catch (err) {
      onError(err instanceof Error ? err.message : String(err));
    } finally {
      setBusy(false);
    }
  }

  return (
    <form className="panel" onSubmit={start}>
      <h2 className="panel__title">Sync all windows</h2>

      <label className="field">
        <span>Media</span>
        <select value={effectiveMedia} onChange={(e) => setMediaId(e.target.value)}>
          {media.map((m) => (
            <option key={m.id} value={m.id}>
              {m.id} — {m.name} ({m.type}, {(m.durationMs / 1000).toFixed(1)}s)
            </option>
          ))}
        </select>
      </label>

      <label className="field">
        <span>Duration (seconds, optional)</span>
        <input
          type="number"
          min={1}
          step={0.5}
          placeholder="use the media's own length"
          value={durationSec}
          onChange={(e) => setDurationSec(e.target.value)}
        />
      </label>

      <div className="panel__actions">
        <button type="submit" disabled={busy || media.length === 0}>
          {busy ? 'Working…' : 'Sync all windows'}
        </button>
        <button type="button" onClick={cancel} disabled={busy || activeSync === null}>
          Cancel sync
        </button>
      </div>

      <p className="panel__status">
        {live && (
          <>
            <span className="badge badge--sync">SYNC</span> showing <strong>{activeSync.mediaId}</strong> —{' '}
            {Math.max(0, Math.ceil((activeSync.endAt - now) / 1000))}s left
          </>
        )}
        {pending && (
          <>
            Starting <strong>{activeSync.mediaId}</strong> in{' '}
            {Math.max(0, ((activeSync.startAt - now) / 1000)).toFixed(1)}s…
          </>
        )}
        {!live && !pending && <span className="muted">No sync running. Each window follows its own playlist.</span>}
      </p>
    </form>
  );
}
