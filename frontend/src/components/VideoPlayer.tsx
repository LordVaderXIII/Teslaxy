import React, { useRef, useEffect } from 'react';
import videojs from 'video.js';
import 'video.js/dist/video-js.css';

interface VideoPlayerProps {
  src: string;
  className?: string;
  onReady?: (player: any) => void;
  options?: any;
}

const VideoPlayer: React.FC<VideoPlayerProps> = ({ src, className, onReady, options }) => {
  const videoRef = useRef<HTMLDivElement>(null);
  const playerRef = useRef<any>(null);
  const currentSrcRef = useRef<string | null>(null);

  useEffect(() => {
    if (!playerRef.current) {
      const videoElement = document.createElement("video");
      videoElement.classList.add('video-js');
      // Prevent Apple Video Player takeover
      videoElement.setAttribute('playsinline', 'true');
      videoElement.setAttribute('webkit-playsinline', 'true');

      videoRef.current?.appendChild(videoElement);

      const player = playerRef.current = videojs(videoElement, {
        ...options,
        controls: false,
        autoplay: false,
        muted: true,
        playsinline: true, // video.js option
        preload: 'auto',
        sources: [{
          src: src,
          type: 'video/mp4'
        }]
      }, () => {
        onReady && onReady(player);
      });

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
  }, [src, options, onReady]);

  useEffect(() => {
    return () => {
      if (playerRef.current && !playerRef.current.isDisposed()) {
        playerRef.current.dispose();
        playerRef.current = null;
      }
    };
  }, []);

  return (
    <div data-vjs-player className={`relative ${className}`}>
      <div ref={videoRef} className="w-full h-full" />
    </div>
  );
};

export default VideoPlayer;
