import { describe, expect, it } from 'vitest';

import { resolve } from './scheduler';

declare const process: { env: Record<string, string | undefined> };

const API_BASE = process.env.API_BASE;

describe.skipIf(!API_BASE)('TS and Go schedulers agree on live data', () => {
  it('matches /api/windows/{id}/now for every window', async () => {
    const state = await fetch(`${API_BASE}/api/state`).then((r) => r.json());
    expect(state.windows.length).toBeGreaterThan(0);

    for (const win of state.windows) {
      const server = await fetch(`${API_BASE}/api/windows/${win.id}/now`).then((r) => r.json());

      const mine = resolve(
        win,
        win.items.map((i: { id: number; mediaId: string; durationMs: number }) => ({
          id: i.id,
          mediaId: i.mediaId,
          durationMs: i.durationMs,
        })),
        server.cycleMs,
        server.serverTimeMs,
      );

      expect(mine.blank, `${win.id} blank`).toBe(server.resolved.blank);
      expect(mine.index, `${win.id} index`).toBe(server.resolved.index);
      expect(mine.itemId ?? 0, `${win.id} itemId`).toBe(server.resolved.itemId);
      expect(mine.elapsedInItemMs, `${win.id} elapsed`).toBe(server.resolved.elapsedInItemMs);
      expect(mine.remainingMs, `${win.id} remaining`).toBe(server.resolved.remainingMs);
      expect(mine.startedAtMs, `${win.id} startedAt`).toBe(server.resolved.startedAtMs);
    }
  });
});
