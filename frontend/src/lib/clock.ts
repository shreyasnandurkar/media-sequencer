export interface ClockInfo {
  offsetMs: number;
  rttMs: number;
  syncedAt: number | null;
  samples: number;
}

type Listener = (info: ClockInfo) => void;

export class ServerClock {
  private offsetMs = 0;
  private rttMs = Number.POSITIVE_INFINITY;
  private syncedAt: number | null = null;
  private samples = 0;
  private listeners = new Set<Listener>();
  private readonly baseUrl: string;

  constructor(baseUrl: string) {
    this.baseUrl = baseUrl;
  }

  now(): number {
    return Date.now() + this.offsetMs;
  }

  info(): ClockInfo {
    return {
      offsetMs: this.offsetMs,
      rttMs: this.rttMs === Number.POSITIVE_INFINITY ? -1 : this.rttMs,
      syncedAt: this.syncedAt,
      samples: this.samples,
    };
  }

  subscribe(fn: Listener): () => void {
    this.listeners.add(fn);
    return () => this.listeners.delete(fn);
  }

  async sync(count = 5): Promise<void> {
    let bestRtt = Number.POSITIVE_INFINITY;
    let bestOffset = this.offsetMs;
    let ok = 0;

    for (let i = 0; i < count; i++) {
      try {
        const t0 = Date.now();
        const res = await fetch(`${this.baseUrl}/api/time`, { cache: 'no-store' });
        const t1 = Date.now();
        if (!res.ok) continue;
        const { serverTimeMs } = (await res.json()) as { serverTimeMs: number };

        const rtt = t1 - t0;
        if (rtt < bestRtt) {
          bestRtt = rtt;
          bestOffset = serverTimeMs - (t0 + t1) / 2;
        }
        ok++;
      } catch {

      }
    }

    if (ok > 0) {
      this.offsetMs = Math.round(bestOffset);
      this.rttMs = bestRtt;
      this.syncedAt = Date.now();
      this.samples = ok;
      this.emit();
    }
  }

  seed(serverTimeMs: number): void {
    if (this.syncedAt !== null) return;
    this.offsetMs = serverTimeMs - Date.now();
    this.emit();
  }

  private emit(): void {
    const info = this.info();
    for (const fn of this.listeners) fn(info);
  }
}
