import React, { useState, useMemo, useRef, Suspense } from 'react';
import { Filter, RefreshCw, Calendar as CalendarIcon, Map as MapIcon, Inbox, ShieldAlert, Video, Clock } from 'lucide-react';
import Calendar from './Calendar';
import VersionDisplay from './VersionDisplay';
import { useClickOutside } from '../hooks/useClickOutside';

const MapModal = React.lazy(() => import('./MapModal'));

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
  reason?: string;
  video_files?: VideoFile[];
  telemetry?: Record<string, unknown>;
  start_time?: Date;
  date_key?: string;
}

interface FilterState {
  recent: boolean;
  dashcamHonk: boolean;
  dashcamSaved: boolean;
  dashcamOther: boolean;
  sentryObject: boolean;
  sentryAccel: boolean;
  sentryOther: boolean;
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

const ThumbnailImage = ({ src, clip }: { src: string; clip: Clip }) => {
  const [error, setError] = useState(false);

  if (!src || error) {
    let icon;
    let colorClass;

    if (clip.event === 'Sentry') {
      icon = <ShieldAlert size={20} aria-hidden="true" />;
      colorClass = 'bg-red-900/20 text-red-500';
    } else if (clip.event === 'Saved') {
      icon = <Video size={20} aria-hidden="true" />;
      colorClass = 'bg-green-900/20 text-green-500';
    } else {
      icon = <Clock size={20} aria-hidden="true" />;
      colorClass = 'bg-gray-800 text-gray-400';
    }

    return (
      <div
        className={`w-full h-full flex items-center justify-center ${colorClass}`}
        role="img"
        aria-label={`${clip.event} event thumbnail placeholder`}
      >
        {icon}
      </div>
    );
  }

  return (
    <img
      src={src}
      alt={`Thumbnail for ${clip.event} event at ${clip.city}`}
      className="w-full h-full object-cover"
      loading="lazy"
      onError={() => setError(true)}
    />
  );
};

const SidebarItem = React.memo(({ clip, isSelected, onClipSelect }: SidebarItemProps) => {
  const thumbnailUrl = useMemo(() => {
    if (!clip.video_files || clip.video_files.length === 0) return '';

    // Bolt Optimization: clip.video_files are already sorted by timestamp (ASC).
    // Instead of filtering and sorting (O(N log N) + alloc), we iterate backwards (O(N))
    // to find the latest Front camera segment that starts before the event.

    let targetVideo: VideoFile | null = null;
    let seekTime = 0;
    const eventTime = clip.event_timestamp ? new Date(clip.event_timestamp).getTime() : 0;

    if (clip.event_timestamp) {
      // Find the latest Front video that starts before or at eventTime.
      for (let i = clip.video_files.length - 1; i >= 0; i--) {
        const v = clip.video_files[i];
        if (v.camera === 'Front') {
          const vTime = new Date(v.timestamp).getTime();
          if (vTime <= eventTime) {
            targetVideo = v;
            break;
          }
        }
      }
    }

    // Fallback: If no match found (or no event timestamp), use the FIRST Front video.
    if (!targetVideo) {
      targetVideo = clip.video_files.find(v => v.camera === 'Front') || null;
    }

    if (!targetVideo) return '';

    // Calculate seek time
    if (clip.event_timestamp) {
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
         <ThumbnailImage src={thumbnailUrl} clip={clip} key={thumbnailUrl} />
       </div>

       <div className="flex-1 min-w-0">
          <div className="flex justify-between items-baseline mb-1">
             <span className="font-medium text-gray-200 truncate">{clip.city || 'Unknown Location'}</span>
             <span className="text-xs text-gray-500 font-mono">{(clip.start_time || new Date(clip.timestamp)).toLocaleTimeString([], {hour: '2-digit', minute:'2-digit'})}</span>
          </div>
          <div className="text-xs text-gray-500 truncate">
             {clip.event} Event • {(clip.start_time || new Date(clip.timestamp)).toLocaleDateString()}
          </div>
       </div>
    </button>
  );
});

const Sidebar: React.FC<SidebarProps> = ({ clips, selectedClipId, onClipSelect, onRefresh, loading, className }) => {
  const [filters, setFilters] = useState<FilterState>({
    recent: true,
    dashcamHonk: true,
    dashcamSaved: true,
    dashcamOther: true,
    sentryObject: true,
    sentryAccel: true,
    sentryOther: true,
  });
  const [isFilterOpen, setIsFilterOpen] = useState(false);
  const [selectedDate, setSelectedDate] = useState<Date>(new Date());
  const [isCalendarOpen, setIsCalendarOpen] = useState(false);
  const [isMapOpen, setIsMapOpen] = useState(false);

  // Click outside to close filter
  const filterRef = useRef<HTMLDivElement>(null);
  useClickOutside(filterRef, () => setIsFilterOpen(false));

  // Bolt Optimization: Stabilize the onDateSelect handler to allow Calendar to stay memoized
  const handleDateSelect = React.useCallback((date: Date) => {
    setSelectedDate(date);
  }, []);

  // Bolt Optimization: Group clips by date for O(1) access
  // This reduces filtering complexity from O(N) to O(1) + O(M) where M is clips per day.
  const clipsByDate = useMemo(() => {
    const groups = new Map<string, Clip[]>();
    clips.forEach(clip => {
      // Use pre-calculated date_key if available to avoid parsing overhead
      const dateStr = clip.date_key || new Date(clip.timestamp).toDateString();
      if (!groups.has(dateStr)) {
        groups.set(dateStr, []);
      }
      groups.get(dateStr)!.push(clip);
    });
    return groups;
  }, [clips]);

  // Filter Logic
  const filteredClips = useMemo(() => {
    const targetDateStr = selectedDate.toDateString();
    const dayClips = clipsByDate.get(targetDateStr) || [];

    return dayClips.filter(clip => {
      // Date Filter is implicit via map lookup

      // Inclusive Filter Logic

      // Recent
      if (clip.event === 'Recent') {
          return filters.recent;
      }

      // Saved (Dashcam)
      if (clip.event === 'Saved') {
          // If Other is enabled, show all Saved clips (Override)
          if (filters.dashcamOther) return true;

          const reason = clip.reason || '';
          if (reason === 'user_interaction_honk') {
              return filters.dashcamHonk;
          }
          if (reason === 'user_interaction_dashcam_panel_save' || reason === 'user_interaction_dashcam_icon_tapped') {
              return filters.dashcamSaved;
          }
          // If reason doesn't match known types, it falls under 'Other' (which is disabled here)
          return false;
      }

      // Sentry
      if (clip.event === 'Sentry') {
          // If Other is enabled, show all Sentry clips (Override)
          if (filters.sentryOther) return true;

          const reason = clip.reason || '';
          if (reason === 'sentry_aware_object_detection') {
              return filters.sentryObject;
          }
          if (reason.startsWith('sentry_aware_accel_')) {
              return filters.sentryAccel;
          }
          return false;
      }

      return false;
    });
  }, [clipsByDate, selectedDate, filters]);

  const handleResetFilters = () => {
    setFilters({
        recent: true,
        dashcamHonk: true,
        dashcamSaved: true,
        dashcamOther: true,
        sentryObject: true,
        sentryAccel: true,
        sentryOther: true,
    });
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
                onDateSelect={handleDateSelect}
                clips={clips}
            />
          </div>

          {/* Filter */}
          <div className="relative" ref={filterRef}>
             <button
                onClick={() => setIsFilterOpen(!isFilterOpen)}
                aria-expanded={isFilterOpen}
                aria-controls="filter-dropdown"
                aria-label={isFilterOpen ? "Hide filters" : "Show filters"}
                className="w-full bg-gray-900 border border-gray-800 text-gray-300 text-sm rounded-lg p-2.5 flex justify-between items-center hover:bg-gray-800 transition focus-visible:ring-2 focus-visible:ring-blue-500 outline-none"
             >
                <div className="flex items-center gap-2">
                    <Filter size={14} className="text-gray-500" />
                    <span>Filter Events</span>
                </div>
                <span className="text-xs text-gray-500">{filteredClips.length} shown</span>
             </button>

             {isFilterOpen && (
                 <div id="filter-dropdown" className="absolute top-full left-0 right-0 mt-1 bg-gray-900 border border-gray-800 rounded-lg shadow-xl z-20 p-3 flex flex-col gap-3">
                     {/* Recent */}
                     <label className="flex items-center gap-2 cursor-pointer">
                        <input
                            type="checkbox"
                            checked={filters.recent}
                            onChange={e => setFilters(p => ({...p, recent: e.target.checked}))}
                            className="rounded bg-gray-800 border-gray-700 text-blue-600 focus:ring-blue-500"
                        />
                        <span className="text-sm text-gray-200">Recent Clips</span>
                     </label>

                     <div className="h-px bg-gray-800" />

                     {/* Dashcam Group */}
                     <div className="flex flex-col gap-2">
                        <span className="text-xs font-semibold text-gray-500 uppercase">Dashcam</span>
                        <label className="flex items-center gap-2 cursor-pointer ml-2">
                            <input type="checkbox" checked={filters.dashcamHonk} onChange={e => setFilters(p => ({...p, dashcamHonk: e.target.checked}))} className="rounded bg-gray-800 border-gray-700 text-green-600 focus:ring-green-500"/>
                            <span className="text-sm text-gray-300">Honk</span>
                        </label>
                        <label className="flex items-center gap-2 cursor-pointer ml-2">
                            <input type="checkbox" checked={filters.dashcamSaved} onChange={e => setFilters(p => ({...p, dashcamSaved: e.target.checked}))} className="rounded bg-gray-800 border-gray-700 text-green-600 focus:ring-green-500"/>
                            <span className="text-sm text-gray-300">Saved (Icon/Panel)</span>
                        </label>
                        <label className="flex items-center gap-2 cursor-pointer ml-2">
                            <input type="checkbox" checked={filters.dashcamOther} onChange={e => setFilters(p => ({...p, dashcamOther: e.target.checked}))} className="rounded bg-gray-800 border-gray-700 text-green-600 focus:ring-green-500"/>
                            <span className="text-sm text-gray-300">Other (All Saved)</span>
                        </label>
                     </div>

                     <div className="h-px bg-gray-800" />

                     {/* Sentry Group */}
                     <div className="flex flex-col gap-2">
                        <span className="text-xs font-semibold text-gray-500 uppercase">Sentry</span>
                        <label className="flex items-center gap-2 cursor-pointer ml-2">
                            <input type="checkbox" checked={filters.sentryObject} onChange={e => setFilters(p => ({...p, sentryObject: e.target.checked}))} className="rounded bg-gray-800 border-gray-700 text-red-600 focus:ring-red-500"/>
                            <span className="text-sm text-gray-300">Object Detection</span>
                        </label>
                        <label className="flex items-center gap-2 cursor-pointer ml-2">
                            <input type="checkbox" checked={filters.sentryAccel} onChange={e => setFilters(p => ({...p, sentryAccel: e.target.checked}))} className="rounded bg-gray-800 border-gray-700 text-red-600 focus:ring-red-500"/>
                            <span className="text-sm text-gray-300">Acceleration</span>
                        </label>
                        <label className="flex items-center gap-2 cursor-pointer ml-2">
                            <input type="checkbox" checked={filters.sentryOther} onChange={e => setFilters(p => ({...p, sentryOther: e.target.checked}))} className="rounded bg-gray-800 border-gray-700 text-red-600 focus:ring-red-500"/>
                            <span className="text-sm text-gray-300">Other (All Sentry)</span>
                        </label>
                     </div>
                 </div>
             )}
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
                    No clips match your current filters for {selectedDate.toLocaleDateString()}.
                  </p>
                </div>

                {(!isToday(selectedDate) || clips.length > 0) && (
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

       {/* Bolt: Wrap MapModal in Suspense to support lazy loading */}
       <Suspense fallback={null}>
         {isMapOpen && (
            <MapModal
              isOpen={isMapOpen}
              onClose={() => setIsMapOpen(false)}
              clips={clips}
              onClipSelect={onClipSelect}
            />
         )}
       </Suspense>
    </div>
  );
};

export default React.memo(Sidebar);
