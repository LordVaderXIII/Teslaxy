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
  exportStart?: number | null;
  exportEnd?: number | null;
  onExportRangeChange?: (start: number, end: number) => void;
}

const Timeline: React.FC<TimelineProps> = ({
  currentTime,
  duration,
  onSeek,
  markers = [],
  className = "",
  exportStart = null,
  exportEnd = null,
  onExportRangeChange
}) => {
  const containerRef = useRef<HTMLDivElement>(null);
  const [isDragging, setIsDragging] = useState(false);
  const [hoverTime, setHoverTime] = useState<number | null>(null);

  // Export Dragging State
  const [dragTarget, setDragTarget] = useState<'start' | 'end' | null>(null);

  const getPercentage = (time: number) => {
    if (duration <= 0) return 0;
    return Math.min(100, Math.max(0, (time / duration) * 100));
  };

  const getTimeFromEvent = useCallback((e: MouseEvent | React.MouseEvent) => {
    if (!containerRef.current || duration <= 0) return 0;
    const rect = containerRef.current.getBoundingClientRect();
    const x = Math.max(0, Math.min(e.clientX - rect.left, rect.width));
    const percentage = x / rect.width;
    return percentage * duration;
  }, [duration]);

  const handleSeek = useCallback((e: MouseEvent | React.MouseEvent) => {
    const newTime = getTimeFromEvent(e);
    onSeek(newTime);
  }, [getTimeFromEvent, onSeek]);

  const handleHover = useCallback((e: React.MouseEvent) => {
    const time = getTimeFromEvent(e);
    setHoverTime(time);
  }, [getTimeFromEvent]);

  const handleMouseDown = (e: React.MouseEvent) => {
    setIsDragging(true);
    handleSeek(e);
  };

  const handleExportDrag = useCallback((e: MouseEvent) => {
     if (!onExportRangeChange || exportStart === null || exportEnd === null || !dragTarget) return;

     const time = getTimeFromEvent(e);

     if (dragTarget === 'start') {
         // Clamp start: 0 <= start <= end - 1
         const newStart = Math.min(Math.max(0, time), exportEnd - 1);
         if (newStart !== exportStart) {
             onExportRangeChange(newStart, exportEnd);
         }
     } else {
         // Clamp end: start + 1 <= end <= duration
         const newEnd = Math.max(exportStart + 1, Math.min(duration, time));
         if (newEnd !== exportEnd) {
             onExportRangeChange(exportStart, newEnd);
         }
     }
  }, [exportStart, exportEnd, dragTarget, getTimeFromEvent, onExportRangeChange, duration]);

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
      if (dragTarget) {
          e.preventDefault(); // Prevent text selection
          handleExportDrag(e);
      } else if (isDragging) {
          handleSeek(e);
      }
    };

    const handleMouseUp = () => {
      setIsDragging(false);
      setDragTarget(null);
    };

    if (isDragging || dragTarget) {
      window.addEventListener('mousemove', handleMouseMove);
      window.addEventListener('mouseup', handleMouseUp);
    }

    return () => {
      window.removeEventListener('mousemove', handleMouseMove);
      window.removeEventListener('mouseup', handleMouseUp);
    };
  }, [isDragging, dragTarget, handleSeek, handleExportDrag]);

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
        onMouseMove={(e) => !isDragging && !dragTarget && handleHover(e)}
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
        {/* Track Background */}
        <div className="absolute w-full h-1.5 bg-gray-700 rounded-full overflow-hidden group-hover:h-2 transition-all">
           {/* Progress */}
           <div
             className="h-full bg-blue-500 rounded-full"
             style={{ width: `${getPercentage(currentTime)}%` }}
           />
           {/* Export Highlight Region */}
           {exportStart !== null && exportEnd !== null && (
             <div
                className="absolute top-0 bottom-0 bg-green-500/50 pointer-events-none"
                style={{
                    left: `${getPercentage(exportStart)}%`,
                    width: `${getPercentage(exportEnd - exportStart)}%`
                }}
             />
           )}
        </div>

        {/* Hover Preview (Ghost Handle & Tooltip) */}
        {hoverTime !== null && !isDragging && !dragTarget && (
           <>
              {/* Ghost Handle */}
              <div
                  className="absolute h-3 w-3 bg-white/50 rounded-full transform -translate-x-1/2 -translate-y-1/2 top-1/2 pointer-events-none z-10"
                  style={{ left: `${getPercentage(hoverTime)}%` }}
              />
              {/* Tooltip */}
              <div
                  className="absolute bottom-full mb-2 bg-gray-800 text-white text-[10px] font-mono py-1 px-2 rounded border border-gray-700 shadow-xl transform -translate-x-1/2 pointer-events-none whitespace-nowrap z-20"
                  style={{ left: `${getPercentage(hoverTime)}%` }}
              >
                  {formatTime(hoverTime)}
              </div>
           </>
        )}

        {/* Export Brackets */}
        {exportStart !== null && exportEnd !== null && (
            <>
                {/* Start Bracket (Green) */}
                <div
                    className="absolute top-1/2 -translate-y-1/2 w-4 h-6 border-l-4 border-t-2 border-b-2 border-green-500 cursor-ew-resize z-40 hover:scale-110 transition-transform shadow-lg bg-black/20"
                    style={{ left: `${getPercentage(exportStart)}%` }}
                    onMouseDown={(e) => { e.stopPropagation(); setDragTarget('start'); }}
                    title="Drag to adjust start time"
                />

                {/* End Bracket (Red) */}
                <div
                    className="absolute top-1/2 -translate-y-1/2 w-4 h-6 border-r-4 border-t-2 border-b-2 border-red-500 cursor-ew-resize z-40 hover:scale-110 transition-transform transform -translate-x-full shadow-lg bg-black/20"
                    style={{ left: `${getPercentage(exportEnd)}%` }}
                    onMouseDown={(e) => { e.stopPropagation(); setDragTarget('end'); }}
                    title="Drag to adjust end time"
                />
            </>
        )}

        {/* Playhead Handle */}
        <div
          className={`absolute z-30 h-4 w-4 bg-white rounded-full shadow-md transform -translate-x-1/2 transition-transform ${
            isDragging ? 'scale-100' : 'scale-0 group-hover:scale-100 group-focus-visible:scale-100'
          }`}
          style={{ left: `${getPercentage(currentTime)}%` }}
        />

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
