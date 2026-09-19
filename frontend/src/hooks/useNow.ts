import { useEffect, useState } from 'react';

import type { ServerClock } from '../lib/clock';

/**
 * Returns the current server time, re-rendering every `intervalMs`.
 *
 * Everything on screen is derived from this number, so a component that calls
 * useNow() repaints on a fixed heartbeat rather than each component setting up
 * its own timer and drifting apart.
 *
 * 250ms is a deliberate compromise: fast enough that an item boundary is never
 * visibly late, slow enough to be nearly free. The progress bars animate in CSS
 * between ticks, so they still look smooth.
 *
 * The cleanup function returned from useEffect is what stops the interval when
 * the component unmounts - without it every navigation would leak a timer.
 */
export function useNow(clock: ServerClock, intervalMs = 250): number {
  const [now, setNow] = useState(() => clock.now());

  useEffect(() => {
    const id = window.setInterval(() => setNow(clock.now()), intervalMs);
    return () => window.clearInterval(id);
  }, [clock, intervalMs]);

  return now;
}
