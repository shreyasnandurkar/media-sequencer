/**
 * TypeScript port of backend/internal/scheduler/scheduler.go.
 *
 * Both implementations are verified against the SAME file,
 * testdata/schedule_vectors.json, so they cannot drift apart.
 *
 * Everything here is pure: `now` is a parameter, never Date.now(). That is what
 * lets any number of tabs, devices and late joiners agree on what is on screen
 * without talking to each other.
 */

export interface SchedulerItem {
  id: number;
  mediaId?: string;
  /** Effective duration: the per-item override if set, else the media's own. */
  durationMs: number;
}

export interface SchedulerWindow {
  cycleEpoch: number;
  anchorAt: number | null;
  anchorIndex: number | null;
}

export interface Resolved {
  /** True only when there is nothing to play at all (empty playlist). */
  blank: boolean;
  /** 0-based playlist position, or -1 when blank. */
  index: number;
  itemId: number | null;
  mediaId: string | null;
  elapsedInItemMs: number;
  /** Capped at the end of the cycle: items are cut off at the boundary. */
  remainingMs: number;
  /** now - elapsedInItemMs. Part of the render identity. */
  startedAtMs: number;
  cycleStartMs: number;
  cycleEndMs: number;
}

/**
 * Start of the cycle containing `now`.
 *
 * Math.floor (not a plain division) matters for `now < cycleEpoch`: cycles must
 * tile evenly in both directions.
 */
export function cycleStart(cycleEpoch: number, cycleMs: number, now: number): number {
  return cycleEpoch + Math.floor((now - cycleEpoch) / cycleMs) * cycleMs;
}

export function totalDuration(items: SchedulerItem[]): number {
  let total = 0;
  for (const it of items) total += it.durationMs;
  return total;
}

export function resolve(
  w: SchedulerWindow,
  items: SchedulerItem[],
  cycleMs: number,
  now: number,
): Resolved {
  const cs = cycleStart(w.cycleEpoch, cycleMs, now);
  const cycleEnd = cs + cycleMs;

  const total = totalDuration(items);
  if (items.length === 0 || total <= 0) {
    return {
      blank: true,
      index: -1,
      itemId: null,
      mediaId: null,
      elapsedInItemMs: now - cs,
      remainingMs: cycleEnd - now,
      startedAtMs: cs,
      cycleStartMs: cs,
      cycleEndMs: cycleEnd,
    };
  }

  // An anchor (left behind by a playlist edit) only counts inside the cycle it
  // was created in and never in the future. Otherwise the cycle restarts at
  // item 0 — that is the 5h restart rule.
  let t0 = cs;
  let idx = 0;
  if (w.anchorAt != null && w.anchorIndex != null && w.anchorAt >= cs && w.anchorAt <= now) {
    t0 = w.anchorAt;
    idx = clamp(w.anchorIndex, 0, items.length - 1);
  }

  let elapsed = now - t0;

  // Pass 1: the partial run from idx to the end of the list.
  for (let i = idx; i < items.length; i++) {
    if (elapsed < items[i].durationMs) return at(items, i, elapsed, now, cs, cycleEnd);
    elapsed -= items[i].durationMs;
  }
  // Pass 2: whole loops from item 0. The modulo skips all complete loops in
  // O(1), so this stays O(n) however long the window has been running.
  elapsed %= total;
  for (let i = 0; i < items.length; i++) {
    if (elapsed < items[i].durationMs) return at(items, i, elapsed, now, cs, cycleEnd);
    elapsed -= items[i].durationMs;
  }

  // Unreachable, but never return undefined.
  return at(items, 0, 0, now, cs, cycleEnd);
}

function at(
  items: SchedulerItem[],
  i: number,
  elapsed: number,
  now: number,
  cs: number,
  cycleEnd: number,
): Resolved {
  const remaining = Math.min(items[i].durationMs - elapsed, cycleEnd - now);
  return {
    blank: false,
    index: i,
    itemId: items[i].id,
    mediaId: items[i].mediaId ?? null,
    elapsedInItemMs: elapsed,
    remainingMs: remaining,
    startedAtMs: now - elapsed,
    cycleStartMs: cs,
    cycleEndMs: cycleEnd,
  };
}

function clamp(v: number, lo: number, hi: number): number {
  return v < lo ? lo : v > hi ? hi : v;
}
