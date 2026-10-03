import React, { useRef, useEffect } from 'react';
import videojs from 'video.js';
import 'video.js/dist/video-js.css';

export interface VideoJsPlayer {
  src: (source: { src: string; type: string }) => void;
  isDisposed: () => boolean;
  dispose: () => void;
  currentSrc: () => string;
  currentTime: (time?: number) => number;
  duration: () => number;
  play: () => Promise<void>;
  pause: () => void;
  paused: () => boolean;
  seeking?: () => boolean;
  readyState?: () => number;
  muted: (mute?: boolean) => boolean;
  playbackRate: (rate?: number) => number;
  on: (event: string, callback: () => void) => void;
  off: (event: string, callback: () => void) => void;
}

interface VideoPlayerProps {
  src: string;
  className?: string;
  onReady?: (player: VideoJsPlayer) => void;
  onDispose?: (player: VideoJsPlayer) => void;
  options?: Record<string, unknown>;
  preload?: 'none' | 'metadata' | 'auto';
}

// Bolt: Memoized to prevent re-renders when parent (Player) updates (e.g. currentTime changes).
// Since onReady is now stable (from Player), and src/options are stable, this avoids 60Hz re-renders.
const VideoPlayer: React.FC<VideoPlayerProps> = React.memo(({ src, className, onReady, onDispose, options, preload = 'metadata' }) => {
  const videoRef = useRef<HTMLDivElement>(null);
  const playerRef = useRef<VideoJsPlayer | null>(null);
  const currentSrcRef = useRef<string | null>(null);
  const onDisposeRef = useRef(onDispose);
  onDisposeRef.current = onDispose;

  useEffect(() => {
    if (!playerRef.current) {
      const videoElement = document.createElement("video");
      videoElement.classList.add('video-js');
      // Prevent Apple Video Player takeover
      videoElement.setAttribute('playsinline', 'true');
      videoElement.setAttribute('webkit-playsinline', 'true');

      videoRef.current?.appendChild(videoElement);

      const player = videojs(videoElement, {
        ...options,
        fill: true,
        controls: false,
        autoplay: false,
        muted: true,
        playsinline: true, // video.js option
        preload,
        sources: [{
          src: src,
          type: 'video/mp4'
        }]
      }, () => {
        if (onReady) onReady(player as unknown as VideoJsPlayer);
      }) as unknown as VideoJsPlayer;
      playerRef.current = player;

      // Video.js resets classes on the tech element during initialization, so we must re-apply
      // layout classes to ensure the video scales correctly within the container.
      videoElement.classList.add('w-full', 'h-full', 'object-contain');

      currentSrcRef.current = src;
    } else {
      const player = playerRef.current;
      // Only update src if it has actually changed
      if (currentSrcRef.current !== src) {
        player.src({ src: src, type: 'video/mp4' });
        currentSrcRef.current = src;
      }
    }
    // Bolt: onReady/preload are constructor-only; updating them has no effect on an existing player.
    // eslint-disable-next-line react-hooks/exhaustive-deps -- onReady/preload are constructor-only
  }, [src, options]);

  useEffect(() => {
    return () => {
      const player = playerRef.current;
      playerRef.current = null;
      if (!player) return;
      try {
        onDisposeRef.current?.(player);
      } finally {
        if (!player.isDisposed()) {
          player.dispose();
        }
      }
    };
  }, []);

  return (
    <div data-vjs-player className={`relative ${className}`}>
      <div ref={videoRef} className="w-full h-full" />
    </div>
  );
});

export default VideoPlayer;
