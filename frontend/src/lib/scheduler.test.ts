import { describe, expect, it } from 'vitest';

import vectors from '../../../testdata/schedule_vectors.json';
import { cycleStart, resolve, type SchedulerItem } from './scheduler';

/**
 * These are the exact same vectors backend/internal/scheduler/scheduler_test.go
 * runs. If the Go and TS implementations ever disagree, one of them fails here.
 */
interface VectorCase {
  name: string;
  cycleMs: number;
  window: { cycleEpoch: number; anchorAt: number | null; anchorIndex: number | null };
  items: { id: number; durationMs: number }[];
  now: number;
  expected: { blank: boolean; index: number; elapsedInItemMs: number; remainingMs: number };
}

const cases = vectors.cases as VectorCase[];

describe('resolve against the shared vectors', () => {
  it('has vectors to run', () => {
    expect(cases.length).toBeGreaterThan(0);
  });

  for (const c of cases) {
    it(c.name, () => {
      const items: SchedulerItem[] = c.items.map((i) => ({ id: i.id, durationMs: i.durationMs }));
      const got = resolve(c.window, items, c.cycleMs, c.now);

      expect(got.blank).toBe(c.expected.blank);
      expect(got.index).toBe(c.expected.index);
      expect(got.elapsedInItemMs).toBe(c.expected.elapsedInItemMs);
      expect(got.remainingMs).toBe(c.expected.remainingMs);

      // Invariants that must hold for every case.
      expect(got.startedAtMs).toBe(c.now - got.elapsedInItemMs);
      expect(got.remainingMs).toBeGreaterThan(0);
      expect(got.cycleEndMs - got.cycleStartMs).toBe(c.cycleMs);
    });
  }
});

describe('cycleStart', () => {
  const epoch = 1_000_000_000_000;
  const cycle = 18_000_000;

  it('stays put for the whole cycle and then advances', () => {
    expect(cycleStart(epoch, cycle, epoch)).toBe(epoch);
    expect(cycleStart(epoch, cycle, epoch + cycle - 1)).toBe(epoch);
    expect(cycleStart(epoch, cycle, epoch + cycle)).toBe(epoch + cycle);
  });

  it('floors backwards before the epoch', () => {
    expect(cycleStart(epoch, cycle, epoch - 1)).toBe(epoch - cycle);
  });
});

describe('playback rules', () => {
  const epoch = 1_000_000_000_000;
  const cycle = 60_000; // the demo-friendly short cycle
  const noAnchor = { cycleEpoch: epoch, anchorAt: null, anchorIndex: null };
  const items: SchedulerItem[] = [
    { id: 1, durationMs: 10_000 },
    { id: 2, durationMs: 30_000 },
  ];

  it('restarts at item 0 on every cycle boundary', () => {
    for (let c = 0; c < 5; c++) {
      const got = resolve(noAnchor, items, cycle, epoch + c * cycle);
      expect(got.index).toBe(0);
      expect(got.elapsedInItemMs).toBe(0);
    }
  });

  it('never reports blank while the playlist has items', () => {
    for (let t = 0; t < cycle; t += 137) {
      expect(resolve(noAnchor, items, cycle, epoch + t).blank).toBe(false);
    }
  });

  it('is continuous: remainingMs always lands exactly on the next item', () => {
    let t = epoch;
    let steps = 0;
    while (t < epoch + cycle && steps < 100) {
      const got = resolve(noAnchor, items, cycle, t);
      expect(got.blank).toBe(false);
      t += got.remainingMs;
      steps++;
    }
    // Stepping by remainingMs must land exactly on the cycle boundary.
    expect(t).toBe(epoch + cycle);
  });
});
