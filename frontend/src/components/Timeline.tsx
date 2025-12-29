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

  const handleMouseMove = (e: React.MouseEvent) => {
    if (!containerRef.current || duration <= 0) return;
    const rect = containerRef.current.getBoundingClientRect();
    const x = Math.max(0, Math.min(e.clientX - rect.left, rect.width));
    const percentage = x / rect.width;
    setHoverTime(percentage * duration);
  };

  const handleMouseLeave = () => {
    setHoverTime(null);
  };

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
    const handleMouseMove = (e: MouseEvent) => {
      if (isDragging) {
        handleSeek(e);
      }
    };

    const handleMouseUp = () => {
      setIsDragging(false);
    };

    if (isDragging) {
      window.addEventListener('mousemove', handleMouseMove);
      window.addEventListener('mouseup', handleMouseUp);
    }

    return () => {
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
    <div className={`flex flex-col gap-1 select-none ${className}`}>
      <div
        ref={containerRef}
        className="relative h-6 flex items-center cursor-pointer group outline-none focus-visible:ring-2 focus-visible:ring-blue-500 rounded"
        onMouseDown={handleMouseDown}
        onMouseMove={handleMouseMove}
        onMouseLeave={handleMouseLeave}
        onKeyDown={handleKeyDown}
        tabIndex={0}
        role="slider"
        aria-label="Playback timeline"
        aria-valuemin={0}
        aria-valuemax={duration}
        aria-valuenow={currentTime}
        aria-valuetext={formatTime(currentTime)}
      >
        {/* Track Background */}
        <div className="absolute w-full h-1.5 bg-gray-700 rounded-full overflow-hidden group-hover:h-2 transition-all">
           {/* Progress */}
           <div
             className="h-full bg-blue-500 rounded-full"
             style={{ width: `${getPercentage(currentTime)}%` }}
           />
           {/* Hover Ghost Bar */}
           {!isDragging && hoverTime !== null && (
             <div
               className="absolute top-0 h-full bg-white/20 transition-none"
               style={{
                 left: 0,
                 width: `${getPercentage(hoverTime)}%`,
               }}
             />
           )}
        </div>

        {/* Handle */}
        <div
          className="absolute h-4 w-4 bg-white rounded-full shadow-md transform -translate-x-1/2 scale-0 group-hover:scale-100 transition-transform z-10"
          style={{ left: `${getPercentage(currentTime)}%` }}
        />

        {/* Hover Tooltip & Handle */}
        {!isDragging && hoverTime !== null && (
          <>
            <div
              className="absolute h-3 w-3 bg-white/50 rounded-full transform -translate-x-1/2 pointer-events-none top-1/2 -translate-y-1/2"
              style={{ left: `${getPercentage(hoverTime)}%` }}
            />
            <div
              className="absolute bottom-full mb-3 bg-gray-800 text-white text-[10px] font-mono px-1.5 py-0.5 rounded border border-gray-600 shadow-xl transform -translate-x-1/2 pointer-events-none whitespace-nowrap z-20"
              style={{ left: `${getPercentage(hoverTime)}%` }}
            >
              {formatTime(hoverTime)}
            </div>
          </>
        )}

        {/* Markers */}
        {markers.map((marker, idx) => (
          <div
            key={idx}
            className="absolute w-3 h-3 rounded-full transform -translate-x-1/2 -translate-y-1/2 top-1/2 border border-black shadow-sm z-10"
            style={{
              left: `${getPercentage(marker.time)}%`,
              backgroundColor: marker.color || 'red'
            }}
            title={marker.label}
          />
        ))}
      </div>

      <div className="flex justify-between text-xs text-gray-400 font-mono">
        <span>{formatTime(currentTime)}</span>
        <span>{formatTime(duration)}</span>
      </div>
    </div>
  );
};

export default Timeline;
