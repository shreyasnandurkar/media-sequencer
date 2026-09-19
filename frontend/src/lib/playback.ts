/**
 * Turns "window + schedule + active sync" into "what exactly is on screen".
 *
 * The scheduler answers the playlist question; this adds the one rule that sits
 * on top of it: while a sync is running, every window shows the sync media
 * instead. The underlying timeline keeps running throughout, so when the sync
 * ends each window simply shows whatever its own schedule says for that moment
 * (see the README tradeoffs for why we chose wall-clock over pause/resume).
 */

import type { Media, SyncState, WindowState } from '../api/client';
import { resolve, type Resolved, type SchedulerItem } from './scheduler';

export interface Playing {
  /** 'sync' = the global sync media, 'item' = this window's playlist, 'blank' = nothing to play. */
  kind: 'sync' | 'item' | 'blank';
  media: Media | null;
  /** When this segment began, on the server timeline. */
  startedAtMs: number;
  /** Nominal length of the segment (used for the progress bar). */
  durationMs: number;
  remainingMs: number;
  /**
   * Identity of the segment. Passed as a React key, so the <img>/<video> is
   * recreated exactly when the content changes and never on a mere tick.
   */
  playKey: string;
  /** The window's own schedule, regardless of any sync. */
  resolved: Resolved;
}

export function toSchedulerItems(win: WindowState): SchedulerItem[] {
  return win.items.map((it) => ({ id: it.id, mediaId: it.mediaId, durationMs: it.durationMs }));
}

/** True while the sync has started and not yet finished. */
export function syncIsLive(sync: SyncState | null, now: number): boolean {
  return sync !== null && sync.startAt <= now && now < sync.endAt;
}

export function whatToPlay(
  win: WindowState,
  mediaById: Map<string, Media>,
  cycleMs: number,
  sync: SyncState | null,
  now: number,
): Playing {
  const items = toSchedulerItems(win);
  const resolved = resolve(win, items, cycleMs, now);

  if (syncIsLive(sync, now)) {
    const s = sync as SyncState;
    return {
      kind: 'sync',
      media: mediaById.get(s.mediaId) ?? null,
      startedAtMs: s.startAt,
      durationMs: s.endAt - s.startAt,
      remainingMs: s.endAt - now,
      playKey: `sync-${s.id}`,
      resolved,
    };
  }

  if (resolved.blank || resolved.mediaId === null) {
    // Reached only when the playlist is empty. A *blank media item* is a normal
    // item and takes the branch below.
    return {
      kind: 'blank',
      media: null,
      startedAtMs: resolved.startedAtMs,
      durationMs: resolved.elapsedInItemMs + resolved.remainingMs,
      remainingMs: resolved.remainingMs,
      playKey: `${win.id}-empty-${resolved.cycleStartMs}`,
      resolved,
    };
  }

  const item = win.items[resolved.index];
  return {
    kind: 'item',
    media: mediaById.get(resolved.mediaId) ?? null,
    startedAtMs: resolved.startedAtMs,
    durationMs: item?.durationMs ?? resolved.elapsedInItemMs + resolved.remainingMs,
    remainingMs: resolved.remainingMs,
    playKey: `${win.id}-${resolved.itemId}-${resolved.startedAtMs}`,
    resolved,
  };
}

/**
 * The media that will be on screen next, so it can be fetched in advance and
 * the switch has no gap.
 *
 * Looking one millisecond past the end of the current segment is enough: the
 * scheduler is a pure function of time, so "what plays next" is just "what
 * plays then".
 */
export function whatIsNext(
  win: WindowState,
  mediaById: Map<string, Media>,
  cycleMs: number,
  sync: SyncState | null,
  playing: Playing,
  now: number,
): Media | null {
  const at = (playing.kind === 'sync' ? (sync as SyncState).endAt : now + playing.remainingMs) + 1;
  const next = resolve(win, toSchedulerItems(win), cycleMs, at);
  if (next.blank || next.mediaId === null) return null;
  const media = mediaById.get(next.mediaId) ?? null;
  return media && media.id === playing.media?.id ? null : media;
}
