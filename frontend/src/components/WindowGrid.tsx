import type { Media, SyncState, WindowState } from '../api/client';
import type { ServerClock } from '../lib/clock';
import { WindowPlayer } from './WindowPlayer';

interface Props {
  windows: WindowState[];
  mediaById: Map<string, Media>;
  cycleMs: number;
  activeSync: SyncState | null;
  now: number;
  clock: ServerClock;
  debug: boolean;
  onDeleteItem: (windowId: string, itemId: number) => void;
}

export function WindowGrid({
  windows,
  mediaById,
  cycleMs,
  activeSync,
  now,
  clock,
  debug,
  onDeleteItem,
}: Props) {
  if (windows.length === 0) {
    return <p className="empty">No windows yet.</p>;
  }

  return (
    <div className="grid">
      {windows.map((w) => (
        <WindowPlayer
          key={w.id}
          win={w}
          mediaById={mediaById}
          cycleMs={cycleMs}
          activeSync={activeSync}
          now={now}
          clock={clock}
          debug={debug}
          onDeleteItem={onDeleteItem}
        />
      ))}
    </div>
  );
}
