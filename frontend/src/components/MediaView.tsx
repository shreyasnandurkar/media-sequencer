import { memo, useCallback, useEffect, useRef, useState } from 'react';

import type { MediaType } from '../api/client';
import type { ServerClock } from '../lib/clock';

interface Props {
  mediaId: string | null;
  mediaType: MediaType | null;
  mediaName: string;
  mediaUrl: string | null;
  /** When this segment started, on the server timeline. */
  startedAtMs: number;
  clock: ServerClock;
  debug: boolean;
}

/** How far a video may drift from the schedule before we seek it back. */
const MAX_DRIFT_SECONDS = 0.5;
const DRIFT_CHECK_MS = 2_000;

/**
 * Renders one piece of media.
 *
 * Two things keep video playback stable:
 *
 *  1. The parent gives this component a `key` that changes only when the
 *     segment changes, so the element is not recreated on every 250ms tick -
 *     which would restart the video four times a second.
 *  2. Props are primitives, not the media object, so React.memo can actually
 *     skip re-renders. (A re-fetch of /api/state builds fresh objects with
 *     identical contents; comparing those by identity would defeat memo.)
 */
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

  // Where in the clip we should be right now, in seconds.
  const expectedSeconds = useCallback(
    () => Math.max(0, (clock.now() - startedAtMs) / 1000),
    [clock, startedAtMs],
  );

  // Join the video at the right moment rather than at 0. This is what makes a
  // page reload during a sync (or mid-item) land in the same place as every
  // other screen.
  const handleLoadedMetadata = useCallback(() => {
    const v = videoRef.current;
    if (!v) return;
    const target = expectedSeconds();
    if (Number.isFinite(v.duration) && target < v.duration) {
      v.currentTime = target;
    }
    // Autoplay is only permitted for muted video; the promise rejects if the
    // browser still refuses, which we ignore rather than crash on.
    void v.play().catch(() => undefined);
  }, [expectedSeconds]);

  // Periodic drift correction. Browsers throttle background tabs and decoders
  // slip, so we nudge the video back onto the schedule instead of trusting it.
  useEffect(() => {
    if (mediaType !== 'video') return;
    const id = window.setInterval(() => {
      const v = videoRef.current;
      if (!v || v.seeking || !Number.isFinite(v.duration)) return;
      const target = expectedSeconds();
      // Past the end of the clip: hold the last frame, do not seek.
      if (target >= v.duration) return;
      if (Math.abs(v.currentTime - target) > MAX_DRIFT_SECONDS) {
        v.currentTime = target;
      }
    }, DRIFT_CHECK_MS);
    return () => window.clearInterval(id);
  }, [mediaType, expectedSeconds]);

  // A blank item, an empty playlist, or media that would not load: all three
  // render the same neutral panel. The schedule carries on regardless.
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
      // muted is not a style choice: browsers block autoplay with sound.
      muted
      playsInline
      autoPlay
      preload="auto"
      // Never loop - the schedule decides when this item ends, not the file.
      onLoadedMetadata={handleLoadedMetadata}
      onError={() => setFailed(true)}
      data-media-id={mediaId ?? undefined}
    />
  );
}

export const MediaView = memo(MediaViewInner);
