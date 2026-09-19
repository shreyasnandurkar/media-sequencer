import { useState } from 'react';

import { api, type Media, type WindowState } from '../api/client';

interface Props {
  windows: WindowState[];
  media: Media[];
  onDone: () => void;
  onError: (message: string) => void;
}

/**
 * "Add media to a window's list" - the dynamic-update requirement.
 *
 * The backend re-anchors inside the same transaction, so whatever is on screen
 * when this is submitted keeps playing to its natural end; the new list takes
 * over from the following item.
 */
export function AddMediaForm({ windows, media, onDone, onError }: Props) {
  const [windowId, setWindowId] = useState('');
  const [mediaId, setMediaId] = useState('');
  const [position, setPosition] = useState('');
  const [busy, setBusy] = useState(false);

  const effectiveWindow = windowId || windows[0]?.id || '';
  const effectiveMedia = mediaId || media[0]?.id || '';

  async function submit(e: React.FormEvent) {
    e.preventDefault();
    if (!effectiveWindow || !effectiveMedia) return;

    setBusy(true);
    try {
      const pos = position.trim() === '' ? undefined : Number(position);
      if (pos !== undefined && (!Number.isInteger(pos) || pos < 0)) {
        onError('Position must be a whole number, 0 or more.');
        return;
      }
      await api.addItem(effectiveWindow, { mediaId: effectiveMedia, position: pos });
      setPosition('');
      onDone();
    } catch (err) {
      onError(err instanceof Error ? err.message : String(err));
    } finally {
      setBusy(false);
    }
  }

  return (
    <form className="panel" onSubmit={submit}>
      <h2 className="panel__title">Add media to a window</h2>

      <label className="field">
        <span>Window</span>
        <select value={effectiveWindow} onChange={(e) => setWindowId(e.target.value)}>
          {windows.map((w) => (
            <option key={w.id} value={w.id}>
              {w.id} — {w.name}
            </option>
          ))}
        </select>
      </label>

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
        <span>Position (optional)</span>
        <input
          type="number"
          min={0}
          placeholder="append to the end"
          value={position}
          onChange={(e) => setPosition(e.target.value)}
        />
      </label>

      <button type="submit" disabled={busy || windows.length === 0 || media.length === 0}>
        {busy ? 'Adding…' : 'Add to playlist'}
      </button>
    </form>
  );
}
