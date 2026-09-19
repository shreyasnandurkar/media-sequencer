import { useEffect, useState } from 'react';

import type { ServerClock } from '../lib/clock';

export function useNow(clock: ServerClock, intervalMs = 250): number {
  const [now, setNow] = useState(() => clock.now());

  useEffect(() => {
    const id = window.setInterval(() => setNow(clock.now()), intervalMs);
    return () => window.clearInterval(id);
  }, [clock, intervalMs]);

  return now;
}
