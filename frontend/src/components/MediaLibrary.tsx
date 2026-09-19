import { useState } from 'react';

import { api, type Media, type MediaType } from '../api/client';

interface Props {
  media: Media[];
  onDone: () => void;
  onError: (message: string) => void;
}

function probeVideoDuration(url: string): Promise<number> {
  return new Promise((resolve, reject) => {
    const probe = document.createElement('video');
    probe.preload = 'metadata';
    probe.muted = true;
    probe.crossOrigin = 'anonymous';

    const cleanup = () => {
      probe.removeAttribute('src');
      probe.load();
    };
    probe.onloadedmetadata = () => {
      const ms = Math.round(probe.duration * 1000);
      cleanup();
      Number.isFinite(ms) && ms > 0 ? resolve(ms) : reject(new Error('could not read duration'));
    };
    probe.onerror = () => {
      cleanup();
      reject(new Error('could not load the video (check the URL and that it allows cross-origin requests)'));
    };
    probe.src = url;
  });
}

export function MediaLibrary({ media, onDone, onError }: Props) {
  const [name, setName] = useState('');
  const [type, setType] = useState<MediaType>('image');
  const [url, setUrl] = useState('');
  const [durationSec, setDurationSec] = useState('10');
  const [busy, setBusy] = useState(false);
  const [probing, setProbing] = useState(false);

  async function autofillDuration() {
    if (!url.trim()) return;
    setProbing(true);
    try {
      const ms = await probeVideoDuration(url.trim());
      setDurationSec((ms / 1000).toFixed(2));
    } catch (err) {
      onError(err instanceof Error ? err.message : String(err));
    } finally {
      setProbing(false);
    }
  }

  async function submit(e: React.FormEvent) {
    e.preventDefault();
    const durationMs = Math.round(Number(durationSec) * 1000);
    if (!Number.isFinite(durationMs) || durationMs <= 0) {
      onError('Duration must be greater than 0.');
      return;
    }

    setBusy(true);
    try {
      await api.createMedia({
        name: name.trim(),
        type,
        url: type === 'blank' ? null : url.trim(),
        durationMs,
      });
      setName('');
      setUrl('');
      onDone();
    } catch (err) {
      onError(err instanceof Error ? err.message : String(err));
    } finally {
      setBusy(false);
    }
  }

  return (
    <section className="panel">
      <h2 className="panel__title">Media library</h2>

      <ul className="library">
        {media.map((m) => (
          <li key={m.id} className="library__item">
            <span className="library__id">{m.id}</span>
            <span className="library__name">{m.name}</span>
            <span className={`library__type library__type--${m.type}`}>{m.type}</span>
            <span className="library__dur">{(m.durationMs / 1000).toFixed(1)}s</span>
          </li>
        ))}
      </ul>

      <form className="panel__form" onSubmit={submit}>
        <h3 className="panel__subtitle">Create media</h3>

        <label className="field">
          <span>Name</span>
          <input value={name} onChange={(e) => setName(e.target.value)} required placeholder="Promo clip" />
        </label>

        <label className="field">
          <span>Type</span>
          <select value={type} onChange={(e) => setType(e.target.value as MediaType)}>
            <option value="image">image</option>
            <option value="video">video</option>
            <option value="blank">blank</option>
          </select>
        </label>

        {type !== 'blank' && (
          <label className="field">
            <span>URL or /media/ path</span>
            <input
              type="text"
              value={url}
              onChange={(e) => setUrl(e.target.value)}
              required
              placeholder="https://… or /media/clip.mp4"
            />
          </label>
        )}

        <label className="field">
          <span>Duration (seconds)</span>
          <div className="field__row">
            <input
              type="number"
              min={0.1}
              step={0.1}
              value={durationSec}
              onChange={(e) => setDurationSec(e.target.value)}
              required
            />
            {type === 'video' && (
              <button type="button" onClick={autofillDuration} disabled={probing || !url.trim()}>
                {probing ? 'Reading…' : 'Auto'}
              </button>
            )}
          </div>
        </label>

        <button type="submit" disabled={busy}>
          {busy ? 'Creating…' : 'Create media'}
        </button>
      </form>
    </section>
  );
}
