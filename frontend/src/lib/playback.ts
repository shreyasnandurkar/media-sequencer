import type { Media, SyncState, WindowState } from '../api/client';
import { resolve, type Resolved, type SchedulerItem } from './scheduler';

export interface Playing {

  kind: 'sync' | 'item' | 'blank';
  media: Media | null;

  startedAtMs: number;

  durationMs: number;
  remainingMs: number;

  playKey: string;

  resolved: Resolved;
}

export function toSchedulerItems(win: WindowState): SchedulerItem[] {
  return win.items.map((it) => ({ id: it.id, mediaId: it.mediaId, durationMs: it.durationMs }));
}

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
