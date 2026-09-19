export interface SchedulerItem {
  id: number;
  mediaId?: string;

  durationMs: number;
}

export interface SchedulerWindow {
  cycleEpoch: number;
  anchorAt: number | null;
  anchorIndex: number | null;
}

export interface Resolved {

  blank: boolean;

  index: number;
  itemId: number | null;
  mediaId: string | null;
  elapsedInItemMs: number;

  remainingMs: number;

  startedAtMs: number;
  cycleStartMs: number;
  cycleEndMs: number;
}

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

  let t0 = cs;
  let idx = 0;
  if (w.anchorAt != null && w.anchorIndex != null && w.anchorAt >= cs && w.anchorAt <= now) {
    t0 = w.anchorAt;
    idx = clamp(w.anchorIndex, 0, items.length - 1);
  }

  let elapsed = now - t0;

  for (let i = idx; i < items.length; i++) {
    if (elapsed < items[i].durationMs) return at(items, i, elapsed, now, cs, cycleEnd);
    elapsed -= items[i].durationMs;
  }

  elapsed %= total;
  for (let i = 0; i < items.length; i++) {
    if (elapsed < items[i].durationMs) return at(items, i, elapsed, now, cs, cycleEnd);
    elapsed -= items[i].durationMs;
  }

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
