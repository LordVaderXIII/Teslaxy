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

  useEffect(() => {
    if (!playerRef.current) {
      const videoElement = document.createElement("video-js");
      videoElement.classList.add('vjs-big-play-centered');
      videoElement.classList.add('w-full');
      videoElement.classList.add('h-full');
      videoRef.current?.appendChild(videoElement);

      const player = playerRef.current = videojs(videoElement, {
        ...options,
        controls: false,
        autoplay: false,
        preload: 'auto',
        sources: [{
          src: src,
          type: 'video/mp4'
        }]
      }, () => {
        onReady && onReady(player);
      });
    } else {
      const player = playerRef.current;
      player.src({ src: src, type: 'video/mp4' });
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
