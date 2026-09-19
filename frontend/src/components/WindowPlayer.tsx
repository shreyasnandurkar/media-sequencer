import { memo, useEffect, useMemo } from 'react';
import { Link } from 'react-router-dom';

import type { Media, SyncState, WindowState } from '../api/client';
import type { ServerClock } from '../lib/clock';
import { whatIsNext, whatToPlay } from '../lib/playback';
import { MediaView } from './MediaView';

interface Props {
  win: WindowState;
  mediaById: Map<string, Media>;
  cycleMs: number;
  activeSync: SyncState | null;
  now: number;
  clock: ServerClock;
  debug: boolean;
  /** Full-screen single-window mode hides the playlist and chrome. */
  fullscreen?: boolean;
  onDeleteItem?: (windowId: string, itemId: number) => void;
}

export function WindowPlayer({
  win,
  mediaById,
  cycleMs,
  activeSync,
  now,
  clock,
  debug,
  fullscreen = false,
  onDeleteItem,
}: Props) {
  // Recomputed every tick. It is a handful of arithmetic over a short list, so
  // this is far cheaper than any subscription machinery would be.
  const playing = whatToPlay(win, mediaById, cycleMs, activeSync, now);
  const nextMedia = whatIsNext(win, mediaById, cycleMs, activeSync, playing, now);

  const progress =
    playing.durationMs > 0
      ? Math.min(100, Math.max(0, ((playing.durationMs - playing.remainingMs) / playing.durationMs) * 100))
      : 0;

  const cycleProgress =
    ((now - playing.resolved.cycleStartMs) / (playing.resolved.cycleEndMs - playing.resolved.cycleStartMs)) * 100;

  return (
    <article className={`player${fullscreen ? ' player--full' : ''}`}>
      {!fullscreen && (
        <header className="player__head">
          <div className="player__title">
            <Link className="player__name" to={`/window/${win.id}`} title="Open this window on its own">
              {win.name}
            </Link>
            <span className="player__id">{win.id}</span>
          </div>
          <div className="player__badges">
            {playing.kind === 'sync' && <span className="badge badge--sync">SYNC</span>}
            <span className="badge">
              {playing.media ? playing.media.id : playing.kind === 'blank' ? 'empty' : '—'}
            </span>
          </div>
        </header>
      )}

      <div className="player__stage">
        <MediaView
          key={playing.playKey}
          mediaId={playing.media?.id ?? null}
          mediaType={playing.media?.type ?? null}
          mediaName={playing.media?.name ?? 'blank'}
          mediaUrl={playing.media?.url ?? null}
          startedAtMs={playing.startedAtMs}
          clock={clock}
          debug={debug}
        />

        {fullscreen && playing.kind === 'sync' && <span className="badge badge--sync player__float">SYNC</span>}

        {debug && (
          <pre className="player__debug">
            {`item   ${playing.resolved.index} / ${win.items.length}
media  ${playing.media?.id ?? '-'} (${playing.kind})
elapsed ${Math.round(playing.durationMs - playing.remainingMs)}ms
left    ${Math.round(playing.remainingMs)}ms
cycle   ${cycleProgress.toFixed(1)}%
anchor  ${win.anchorAt ?? '-'} / ${win.anchorIndex ?? '-'}
v${win.version}`}
          </pre>
        )}
      </div>

      <div className="player__progress" aria-hidden="true">
        <div className="player__progress-fill" style={{ width: `${progress}%` }} />
      </div>

      {!fullscreen && (
        <ol className="playlist">
          {win.items.length === 0 && <li className="playlist__empty">Playlist is empty — showing blank.</li>}
          {win.items.map((item, i) => {
            const media = mediaById.get(item.mediaId);
            const current = playing.kind !== 'sync' && i === playing.resolved.index;
            return (
              <li key={item.id} className={`playlist__item${current ? ' playlist__item--current' : ''}`}>
                <span className="playlist__pos">{i}</span>
                <span className="playlist__media">{item.mediaId}</span>
                <span className="playlist__name">{media?.name ?? 'unknown media'}</span>
                <span className="playlist__dur">{(item.durationMs / 1000).toFixed(1)}s</span>
                {onDeleteItem && (
                  <button
                    className="playlist__remove"
                    type="button"
                    title="Remove from this window"
                    onClick={() => onDeleteItem(win.id, item.id)}
                  >
                    ×
                  </button>
                )}
              </li>
            );
          })}
        </ol>
      )}

      {nextMedia && <Preloader media={nextMedia} />}
    </article>
  );
}

/**
 * Warms the browser cache for the next item so the switch is instant.
 *
 * Images go through the Image() constructor (no DOM node needed); videos need a
 * real element for the browser to start buffering, so we render a hidden one.
 */
const Preloader = memo(function Preloader({ media }: { media: Media }) {
  const url = media.url;

  useEffect(() => {
    if (!url || media.type !== 'image') return;
    const img = new Image();
    img.src = url;
    // No cleanup needed: an unreferenced Image simply stops and is collected.
  }, [url, media.type]);

  const videoUrl = useMemo(() => (media.type === 'video' ? url : null), [media.type, url]);
  if (!videoUrl) return null;

  return <video className="preload" src={videoUrl} preload="auto" muted playsInline aria-hidden="true" />;
});
