import React, { useState, useMemo } from 'react';
import { Filter, RefreshCw, Calendar as CalendarIcon, Map as MapIcon, Inbox } from 'lucide-react';
import Calendar from './Calendar';
import MapModal from './MapModal';
import VersionDisplay from './VersionDisplay';

interface VideoFile {
  camera: string;
  file_path: string;
  timestamp: string;
}

interface Clip {
  ID: number;
  timestamp: string;
  event_timestamp?: string;
  event: string;
  city: string;
  video_files?: VideoFile[];
  telemetry?: Record<string, unknown>;
}

interface SidebarProps {
  clips: Clip[];
  selectedClipId: number | null;
  onClipSelect: (clip: Clip) => void;
  onRefresh?: () => void;
  loading: boolean;
  className?: string;
}

interface SidebarItemProps {
  clip: Clip;
  isSelected: boolean;
  onClipSelect: (clip: Clip) => void;
}

const SidebarItem = React.memo(({ clip, isSelected, onClipSelect }: SidebarItemProps) => {
  const thumbnailUrl = useMemo(() => {
    if (!clip.video_files) return '';

    // Find all front videos
    // Note: clip.video_files is already sorted by timestamp (ASC) from the backend/merge logic.
    // Filtering preserves relative order, so frontVideos is also sorted.
    const frontVideos = clip.video_files.filter(v => v.camera === 'Front');
    if (frontVideos.length === 0) return '';

    let targetVideo = frontVideos[0];
    let seekTime = 0;

    if (clip.event_timestamp) {
        const eventTime = new Date(clip.event_timestamp).getTime();
        // Find segment containing event
        // We want the latest segment that starts before or at eventTime
        const match = frontVideos.reduce((prev, curr) => {
             const currTime = new Date(curr.timestamp).getTime();
             if (currTime <= eventTime) return curr;
             return prev;
        }, frontVideos[0]);

        targetVideo = match;
        const startTime = new Date(targetVideo.timestamp).getTime();
        const diff = (eventTime - startTime) / 1000;

        // Only apply offset if it's positive and reasonable (e.g. within 600s)
        if (diff >= 0 && diff < 600) {
            seekTime = diff;
        }
    }

    return `/api/thumbnail${targetVideo.file_path}?time=${seekTime.toFixed(1)}&w=160`;
  }, [clip.video_files, clip.event_timestamp]);

  return (
    <button
      onClick={() => onClipSelect(clip)}
      aria-current={isSelected ? 'true' : undefined}
      className={`
         w-full text-left outline-none focus-visible:ring-2 focus-visible:ring-blue-500 focus-visible:ring-inset
         p-4 hover:bg-gray-900 transition flex gap-3
         ${isSelected ? 'bg-gray-900 border-l-4 border-blue-500' : 'border-l-4 border-transparent'}
      `}
    >
       {/* Thumbnail Placeholder or Icon */}
       <div className="w-12 h-12 rounded flex-shrink-0 overflow-hidden bg-gray-800 relative">
          {thumbnailUrl ? (
             <img
                src={thumbnailUrl}
                alt={`Thumbnail for ${clip.event} event at ${clip.city}`}
                className="w-full h-full object-cover"
                loading="lazy"
             />
          ) : (
              <div className={`
                  w-full h-full flex items-center justify-center
                  ${clip.event === 'Sentry' ? 'bg-red-900/20 text-red-500' :
                  clip.event === 'Saved' ? 'bg-green-900/20 text-green-500' : 'bg-gray-800 text-gray-400'}
              `}>
                  <div className="text-xs font-bold uppercase">{clip.event.substring(0,2)}</div>
              </div>
          )}
       </div>

       <div className="flex-1 min-w-0">
          <div className="flex justify-between items-baseline mb-1">
             <span className="font-medium text-gray-200 truncate">{clip.city || 'Unknown Location'}</span>
             <span className="text-xs text-gray-500 font-mono">{new Date(clip.timestamp).toLocaleTimeString([], {hour: '2-digit', minute:'2-digit'})}</span>
          </div>
          <div className="text-xs text-gray-500 truncate">
             {clip.event} Event • {new Date(clip.timestamp).toLocaleDateString()}
          </div>
       </div>
    </button>
  );
});

const Sidebar: React.FC<SidebarProps> = ({ clips, selectedClipId, onClipSelect, onRefresh, loading, className }) => {
  const [filterType, setFilterType] = useState<string>('All');
  const [selectedDate, setSelectedDate] = useState<Date>(new Date());
  const [isCalendarOpen, setIsCalendarOpen] = useState(false);
  const [isMapOpen, setIsMapOpen] = useState(false);

  // Optimization: Memoize date strings to avoid re-parsing on every render/filter change
  const clipDateMap = useMemo(() => {
    const map = new Map<number, string>();
    clips.forEach(clip => {
      map.set(clip.ID, new Date(clip.timestamp).toDateString());
    });
    return map;
  }, [clips]);

  // Filter Logic
  const filteredClips = useMemo(() => {
    const targetDateStr = selectedDate.toDateString();

    return clips.filter(clip => {
      // Date Filter
      const sameDay = clipDateMap.get(clip.ID) === targetDateStr;

      // Type Filter
      const typeMatch = filterType === 'All' || clip.event === filterType;

      return sameDay && typeMatch;
    });
  }, [clips, clipDateMap, selectedDate, filterType]);

  // Get unique event types for dropdown
  const eventTypes = useMemo(() => ['All', ...Array.from(new Set(clips.map(c => c.event)))], [clips]);

  const handleResetFilters = () => {
    setFilterType('All');
    setSelectedDate(new Date());
  };

  const isToday = (date: Date) => {
    const today = new Date();
    return date.toDateString() === today.toDateString();
  };

  return (
    <div className={`flex flex-col bg-black border-gray-800 w-full md:w-96 md:h-full md:flex-shrink-0 border-t md:border-l md:border-t-0 ${className || ''}`}>
       {/* Header */}
       <div className="p-4 border-b border-gray-800 flex flex-col gap-4">
          <div className="flex justify-between items-center">
              <div className="flex items-center gap-3">
                  <h2 className="text-xl font-bold text-white">Library</h2>
                  <button
                    onClick={() => setIsMapOpen(true)}
                    aria-label="View Map"
                    className="p-1.5 bg-blue-600/20 text-blue-400 hover:bg-blue-600/40 hover:text-white rounded-lg transition-colors focus-visible:ring-2 focus-visible:ring-blue-500 outline-none"
                    title="View Map"
                  >
                     <MapIcon size={18} />
                  </button>
              </div>

              <div className="flex items-center gap-2">
                  <button
                    onClick={() => setIsCalendarOpen(!isCalendarOpen)}
                    aria-label={isCalendarOpen ? "Hide Calendar" : "Show Calendar"}
                    aria-expanded={isCalendarOpen}
                    className="p-2 bg-gray-900 rounded-full hover:bg-gray-800 transition text-gray-400 hover:text-white md:hidden focus-visible:ring-2 focus-visible:ring-blue-500 outline-none"
                    title="Toggle Calendar"
                  >
                     <CalendarIcon size={18} />
                  </button>
                  <button
                    onClick={onRefresh}
                    disabled={loading}
                    aria-label="Refresh library"
                    data-loading={loading ? "true" : "false"}
                    className="p-2 bg-gray-900 rounded-full hover:bg-gray-800 transition text-gray-400 hover:text-white outline-none focus-visible:ring-2 focus-visible:ring-blue-500 disabled:opacity-50 disabled:cursor-not-allowed"
                    title="Sync"
                  >
                     <div className={loading ? 'animate-spin' : undefined}>
                        <RefreshCw size={18} />
                     </div>
                  </button>
              </div>
          </div>

          <div className={`${isCalendarOpen ? 'block' : 'hidden'} md:block transition-all duration-300 ease-in-out`}>
            <Calendar
                currentDate={selectedDate}
                onDateSelect={(date) => {
                    setSelectedDate(date);
                }}
                clips={clips}
            />
          </div>

          {/* Filter */}
          <div className="relative">
             <div className="absolute inset-y-0 left-0 pl-3 flex items-center pointer-events-none">
                <Filter size={14} className="text-gray-500" />
             </div>
             <select
               aria-label="Filter events by type"
               className="bg-gray-900 border border-gray-800 text-gray-300 text-sm rounded-lg focus:ring-blue-500 focus:border-blue-500 block w-full pl-10 p-2.5 outline-none focus-visible:ring-2"
               value={filterType}
               onChange={(e) => setFilterType(e.target.value)}
             >
                {eventTypes.map(type => (
                   <option key={type} value={type}>{type} Events</option>
                ))}
             </select>
          </div>
       </div>

       {/* Clip List */}
       <div className="flex-1 overflow-y-auto custom-scrollbar" aria-live="polite">
          {loading ? (
             <div className="p-8 text-center text-gray-500 animate-pulse flex flex-col items-center gap-2">
                <RefreshCw className="animate-spin" size={24} />
                <span>Loading footage...</span>
             </div>
          ) : filteredClips.length === 0 ? (
             <div className="flex flex-col items-center justify-center p-8 text-center h-full text-gray-500 space-y-4">
                <div className="bg-gray-900 p-4 rounded-full">
                  <Inbox size={32} className="opacity-50" />
                </div>
                <div>
                  <p className="font-medium text-gray-400">No clips found</p>
                  <p className="text-sm mt-1 opacity-70">
                    {filterType !== 'All'
                      ? `No ${filterType} events on this day.`
                      : `No footage available for ${selectedDate.toLocaleDateString()}`}
                  </p>
                </div>

                {(filterType !== 'All' || !isToday(selectedDate)) && (
                  <button
                    onClick={handleResetFilters}
                    className="text-sm text-blue-400 hover:text-blue-300 hover:underline focus-visible:ring-2 focus-visible:ring-blue-500 rounded px-2 py-1 outline-none transition-colors"
                  >
                    Reset filters
                  </button>
                )}
             </div>
          ) : (
            <div className="grid grid-cols-1 divide-y divide-gray-900">
               {filteredClips.map(clip => (
                  <SidebarItem
                    key={clip.ID}
                    clip={clip}
                    isSelected={selectedClipId === clip.ID}
                    onClipSelect={onClipSelect}
                  />
               ))}
            </div>
          )}
       </div>

       {/* Footer / Version */}
       <div className="p-4 border-t border-gray-800 flex justify-center">
            <VersionDisplay />
       </div>

       <MapModal
          isOpen={isMapOpen}
          onClose={() => setIsMapOpen(false)}
          clips={clips}
          onClipSelect={onClipSelect}
       />
    </div>
  );
};

export default React.memo(Sidebar);
