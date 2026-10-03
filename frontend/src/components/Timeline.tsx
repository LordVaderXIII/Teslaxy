import React, { useRef, useState, useEffect, useCallback } from 'react';

interface Marker {
  time: number;
  color?: string;
  label?: string;
}

interface TimelineProps {
  currentTime: number;
  duration: number;
  onSeek: (time: number) => void;
  markers?: Marker[];
  className?: string;
}

const Timeline: React.FC<TimelineProps> = ({
  currentTime,
  duration,
  onSeek,
  markers = [],
  className = ""
}) => {
  const containerRef = useRef<HTMLDivElement>(null);
  const [isDragging, setIsDragging] = useState(false);
  const [hoverTime, setHoverTime] = useState<number | null>(null);

  const getPercentage = (time: number) => {
    if (duration <= 0) return 0;
    return Math.min(100, Math.max(0, (time / duration) * 100));
  };

  const handleSeek = useCallback((e: MouseEvent | React.MouseEvent) => {
    if (!containerRef.current || duration <= 0) return;

    const rect = containerRef.current.getBoundingClientRect();
    const x = Math.max(0, Math.min(e.clientX - rect.left, rect.width));
    const percentage = x / rect.width;
    const newTime = percentage * duration;

    onSeek(newTime);
  }, [duration, onSeek]);

  const handleHover = useCallback((e: React.MouseEvent) => {
    if (!containerRef.current || duration <= 0) return;
    const rect = containerRef.current.getBoundingClientRect();
    const x = Math.max(0, Math.min(e.clientX - rect.left, rect.width));
    const percentage = x / rect.width;
    const time = percentage * duration;
    setHoverTime(time);
  }, [duration]);

  const handleMouseDown = (e: React.MouseEvent) => {
    setIsDragging(true);
    handleSeek(e);
  };

  const handleKeyDown = (e: React.KeyboardEvent) => {
    if (duration <= 0) return;
    const step = 5; // 5 seconds jump

    switch (e.key) {
      case 'ArrowLeft':
      case 'ArrowDown':
        e.preventDefault();
        onSeek(Math.max(0, currentTime - step));
        break;
      case 'ArrowRight':
      case 'ArrowUp':
        e.preventDefault();
        onSeek(Math.min(duration, currentTime + step));
        break;
      case 'Home':
        e.preventDefault();
        onSeek(0);
        break;
      case 'End':
        e.preventDefault();
        onSeek(duration);
        break;
    }
  };

  useEffect(() => {
    let rafId = 0;
    let latestEvent: MouseEvent | null = null;

    const handleMouseMove = (e: MouseEvent) => {
      if (!isDragging) return;
      latestEvent = e;
      if (!rafId) {
        rafId = requestAnimationFrame(() => {
          if (latestEvent) handleSeek(latestEvent);
          rafId = 0;
        });
      }
    };

    const handleMouseUp = () => {
      // Flush any pending seek on release
      if (rafId) {
        cancelAnimationFrame(rafId);
        rafId = 0;
      }
      if (latestEvent) handleSeek(latestEvent);
      setIsDragging(false);
    };

    if (isDragging) {
      window.addEventListener('mousemove', handleMouseMove);
      window.addEventListener('mouseup', handleMouseUp);
    }

    return () => {
      if (rafId) cancelAnimationFrame(rafId);
      window.removeEventListener('mousemove', handleMouseMove);
      window.removeEventListener('mouseup', handleMouseUp);
    };
  }, [isDragging, handleSeek]);

  const formatTime = (seconds: number) => {
    if (!Number.isFinite(seconds) || seconds < 0) return "0:00";
    const mins = Math.floor(seconds / 60);
    const secs = Math.floor(seconds % 60);
    return `${mins}:${secs.toString().padStart(2, '0')}`;
  };

  return (
    <div className={`flex flex-col select-none ${className}`}>
      <div
        ref={containerRef}
        className="relative h-[42px] flex items-center cursor-pointer outline-none"
        onMouseDown={handleMouseDown}
        onMouseMove={(e) => !isDragging && handleHover(e)}
        onMouseLeave={() => setHoverTime(null)}
        onKeyDown={handleKeyDown}
        tabIndex={0}
        role="slider"
        aria-label="Playback timeline"
        aria-valuemin={0}
        aria-valuemax={duration}
        aria-valuenow={currentTime}
        aria-valuetext={formatTime(currentTime)}
      >
        <div className="absolute left-0 right-0 top-[19px] h-1 bg-[var(--line)]" />
        <div
          className="absolute left-0 top-[19px] h-1 bg-[var(--accent)] pointer-events-none"
          style={{ width: `${getPercentage(currentTime)}%` }}
        />
        <div
          className="absolute top-[14px] w-[10px] h-[14px] bg-[var(--ink)] border-2 border-[var(--bg)] pointer-events-none"
          style={{ left: `calc(${getPercentage(currentTime)}% - 5px)` }}
        />

        {hoverTime !== null && !isDragging && (
           <div
               className="absolute bottom-full mb-1 bg-[var(--panel-2)] text-[var(--ink)] text-[16px] font-mono py-1 px-2 border border-[var(--line)] pointer-events-none whitespace-nowrap z-20"
               style={{ left: `${getPercentage(hoverTime)}%`, transform: 'translateX(-50%)' }}
           >
               {formatTime(hoverTime)}
           </div>
        )}

        {markers.map((marker, idx) => (
          <div
            key={idx}
            className="absolute top-2 w-0.5 h-[25px] pointer-events-none z-10"
            style={{
              left: `${getPercentage(marker.time)}%`,
              backgroundColor: marker.color || '#ffb020'
            }}
            title={marker.label}
          >
            <span
              className="absolute -top-1 -left-[3px] w-2 h-2"
              style={{
                backgroundColor: marker.color || '#ffb020',
                transform: 'rotate(45deg)'
              }}
            />
          </div>
        ))}
      </div>
    </div>
  );
};

export default Timeline;
