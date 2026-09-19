import { memo, useCallback, useEffect, useRef, useState } from 'react';

import type { MediaType } from '../api/client';
import type { ServerClock } from '../lib/clock';

interface Props {
  mediaId: string | null;
  mediaType: MediaType | null;
  mediaName: string;
  mediaUrl: string | null;

  startedAtMs: number;
  clock: ServerClock;
  debug: boolean;
}

const MAX_DRIFT_SECONDS = 0.5;
const DRIFT_CHECK_MS = 2_000;

function MediaViewInner({
  mediaId,
  mediaType,
  mediaName,
  mediaUrl,
  startedAtMs,
  clock,
  debug,
}: Props) {
  const [failed, setFailed] = useState(false);
  const videoRef = useRef<HTMLVideoElement>(null);

  const expectedSeconds = useCallback(
    () => Math.max(0, (clock.now() - startedAtMs) / 1000),
    [clock, startedAtMs],
  );

  const handleLoadedMetadata = useCallback(() => {
    const v = videoRef.current;
    if (!v) return;
    const target = expectedSeconds();
    if (Number.isFinite(v.duration) && target < v.duration) {
      v.currentTime = target;
    }

    void v.play().catch(() => undefined);
  }, [expectedSeconds]);

  useEffect(() => {
    if (mediaType !== 'video') return;
    const id = window.setInterval(() => {
      const v = videoRef.current;
      if (!v || v.seeking || !Number.isFinite(v.duration)) return;
      const target = expectedSeconds();

      if (target >= v.duration) return;
      if (Math.abs(v.currentTime - target) > MAX_DRIFT_SECONDS) {
        v.currentTime = target;
      }
    }, DRIFT_CHECK_MS);
    return () => window.clearInterval(id);
  }, [mediaType, expectedSeconds]);

  if (mediaType === null || mediaType === 'blank' || failed || !mediaUrl) {
    return (
      <div className="media media--blank" data-testid="blank">
        {debug && (
          <span className="media__label">
            {failed ? `failed to load ${mediaName}` : mediaType === 'blank' ? '—' : 'no media'}
          </span>
        )}
      </div>
    );
  }

  if (mediaType === 'image') {
    return (
      <img
        className="media"
        src={mediaUrl}
        alt={mediaName}
        onError={() => setFailed(true)}
        draggable={false}
      />
    );
  }

  return (
    <video
      ref={videoRef}
      className="media"
      src={mediaUrl}

      muted
      playsInline
      autoPlay
      preload="auto"

      onLoadedMetadata={handleLoadedMetadata}
      onError={() => setFailed(true)}
      data-media-id={mediaId ?? undefined}
    />
  );
}

export const MediaView = memo(MediaViewInner);
