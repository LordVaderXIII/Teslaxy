import React, { useState, useMemo, useRef, useEffect } from 'react';
import { Filter, RefreshCw, Calendar as CalendarIcon, Map as MapIcon, Inbox } from 'lucide-react';
import Calendar from './Calendar';
import { useClickOutside } from '../hooks/useClickOutside';
import type { Clip, VideoFile } from '../utils/clipMerge';
import { DEFAULT_CLIP_FILTERS, type ClipFilterState } from '../utils/clipFilters';

interface SidebarProps {
  clips: Clip[];
  filteredClips: Clip[];
  filters: ClipFilterState;
  onFiltersChange: (filters: ClipFilterState) => void;
  selectedDate: Date;
  onDateSelect: (date: Date) => void;
  selectedClipId: number | null;
  onClipSelect: (clip: Clip) => void;
  onRefresh?: () => void;
  onOpenMap?: () => void;
  loading: boolean;
  fetchError?: boolean;
  className?: string;
}

interface SidebarItemProps {
  clip: Clip;
  isSelected: boolean;
  onClipSelect: (clip: Clip) => void;
}

const padCount = (n: number) => String(n).padStart(2, '0');

const ThumbnailImage = ({ src, clip }: { src: string; clip: Clip }) => {
  const [error, setError] = useState(false);

  if (!src || error) {
    return (
      <div className="w-full h-full flex items-center justify-center bg-[var(--panel-2)] text-[var(--muted)]">
        <div className="text-[16px] font-bold uppercase">{clip.event.substring(0, 2)}</div>
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
    if (!clip.video_files || clip.video_files.length === 0) {
      if (clip.preview_path) {
        const seek = clip.preview_seek_seconds ?? 0;
        return `/api/thumbnail${clip.preview_path}?time=${seek}&w=160`;
      }
      return '';
    }

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
  }, [clip.video_files, clip.event_timestamp, clip.preview_path, clip.preview_seek_seconds]);

  const start = clip.start_time || new Date(clip.timestamp);
  const reason = clip.reason || clip.event;

  return (
    <button
      onClick={() => onClipSelect(clip)}
      aria-current={isSelected ? 'true' : undefined}
      className={`
         w-full text-left outline-none
         grid grid-cols-[66px_minmax(0,1fr)] gap-3 items-center
         px-[18px] py-[13px] min-h-[91px]
         border-0 border-l-[3px] border-b border-b-[var(--ghost)]
         ${isSelected
           ? 'bg-[var(--ghost)] border-l-[var(--accent)]'
           : 'bg-transparent border-l-transparent hover:bg-[var(--panel-2)]'}
      `}
    >
       <div className="h-[49px] relative overflow-hidden bg-[var(--panel-2)] border border-[var(--line)]">
         <ThumbnailImage src={thumbnailUrl} clip={clip} key={thumbnailUrl} />
       </div>

       <div className="min-w-0">
          <span className="block font-[var(--font-display)] font-medium text-[17px] leading-[1.15] text-[var(--ink)] whitespace-nowrap overflow-hidden text-ellipsis">
            {clip.city || 'Unknown location'}
          </span>
          <span className="block mt-[5px] text-[16px] text-[var(--muted)] whitespace-nowrap overflow-hidden text-ellipsis">
            {start.toLocaleDateString()} · {start.toLocaleTimeString([], { hour: '2-digit', minute: '2-digit' })}
          </span>
          <span className={`block mt-1 text-[16px] tracking-[0.08em] uppercase whitespace-nowrap overflow-hidden text-ellipsis ${clip.event === 'Sentry' ? 'text-[var(--sev-crit)]' : 'text-[var(--accent)]'}`}>
            {reason.replaceAll('_', ' ')}
          </span>
       </div>
    </button>
  );
});

const Sidebar: React.FC<SidebarProps> = ({
  clips,
  filteredClips,
  filters,
  onFiltersChange,
  selectedDate,
  onDateSelect,
  selectedClipId,
  onClipSelect,
  onRefresh,
  onOpenMap,
  loading,
  fetchError,
  className,
}) => {
  const [isFilterOpen, setIsFilterOpen] = useState(false);
  const [isCalendarOpen, setIsCalendarOpen] = useState(false);
  const [isDesktop, setIsDesktop] = useState(
    typeof window !== 'undefined' ? window.matchMedia('(min-width: 768px)').matches : true
  );

  useEffect(() => {
    const mq = window.matchMedia('(min-width: 768px)');
    const onChange = () => setIsDesktop(mq.matches);
    onChange();
    mq.addEventListener('change', onChange);
    return () => mq.removeEventListener('change', onChange);
  }, []);

  // Click outside to close filter
  const filterRef = useRef<HTMLDivElement>(null);
  useClickOutside(filterRef, () => setIsFilterOpen(false));

  useEffect(() => {
    const onKey = (e: KeyboardEvent) => {
      if (e.key === 'Escape') {
        setIsFilterOpen(false);
        if (!isDesktop) setIsCalendarOpen(false);
      }
    };
    window.addEventListener('keydown', onKey);
    return () => window.removeEventListener('keydown', onKey);
  }, [isDesktop]);

  // Bolt Optimization: Stabilize the onDateSelect handler to allow Calendar to stay memoized
  const handleDateSelect = React.useCallback((date: Date) => {
    onDateSelect(date);
  }, [onDateSelect]);

  const handleResetFilters = () => {
    onFiltersChange(DEFAULT_CLIP_FILTERS);
    onDateSelect(new Date());
  };

  const isToday = (date: Date) => {
    const today = new Date();
    return date.toDateString() === today.toDateString();
  };

  const showCalendar = isDesktop || isCalendarOpen;
  const dateLabel = selectedDate.toLocaleDateString(undefined, { day: '2-digit', month: 'short', year: 'numeric' }).toUpperCase();

  return (
    <aside className={`flex flex-col min-h-0 w-full ${className || ''}`} aria-label="Event library">
       <div className="px-[19px] pt-[25px] border-b border-dashed border-[var(--line)] flex-none">
          <span className="desk-eyebrow">// EVENT ARCHIVE</span>
          <div className="flex items-end justify-between mt-[5px] mb-[17px]">
              <h1 className="desk-title text-[31px] leading-[1.05]">Library</h1>
              <button
                type="button"
                onClick={onRefresh}
                disabled={loading}
                aria-label="Refresh library"
                data-loading={loading ? "true" : "false"}
                className="desk-iconbtn"
                title="Refresh library"
              >
                 <span className={loading ? 'desk-spin' : undefined}>
                    <RefreshCw size={18} />
                 </span>
              </button>
          </div>

          <div className="flex gap-[7px] pb-[18px] flex-wrap">
             <div className="relative" ref={filterRef}>
               <button
                  type="button"
                  onClick={() => setIsFilterOpen(!isFilterOpen)}
                  aria-expanded={isFilterOpen}
                  aria-controls="filter-dropdown"
                  aria-label={isFilterOpen ? "Hide filters" : "Show filters"}
                  className="desk-lightbtn"
                  aria-pressed={isFilterOpen}
               >
                  <Filter size={18} />
                  FILTER
               </button>

               {isFilterOpen && (
                   <div id="filter-dropdown" className="desk-popover flex flex-col gap-3">
                       <span className="desk-eyebrow">// FILTER EVENTS</span>
                       <h3 className="desk-title text-[22px] m-0">Show in library</h3>
                       <label className="flex items-center gap-[11px] min-h-12 cursor-pointer text-[var(--ink-2)]">
                          <input
                              type="checkbox"
                              checked={filters.recent}
                              onChange={e => onFiltersChange({...filters, recent: e.target.checked})}
                              className="h-5 w-5 accent-[var(--accent)]"
                          />
                          <span>Recent Clips</span>
                       </label>

                       <div className="h-px border-t border-dashed border-[var(--line)]" />

                       <div className="flex flex-col gap-2">
                          <span className="text-[16px] font-semibold text-[var(--muted)] uppercase tracking-[0.08em]">Dashcam</span>
                          <label className="flex items-center gap-[11px] min-h-12 cursor-pointer ml-2">
                              <input type="checkbox" checked={filters.dashcamHonk} onChange={e => onFiltersChange({...filters, dashcamHonk: e.target.checked})} className="h-5 w-5 accent-[var(--accent)]"/>
                              <span>Honk</span>
                          </label>
                          <label className="flex items-center gap-[11px] min-h-12 cursor-pointer ml-2">
                              <input type="checkbox" checked={filters.dashcamSaved} onChange={e => onFiltersChange({...filters, dashcamSaved: e.target.checked})} className="h-5 w-5 accent-[var(--accent)]"/>
                              <span>Saved (Icon/Panel)</span>
                          </label>
                          <label className="flex items-center gap-[11px] min-h-12 cursor-pointer ml-2">
                              <input type="checkbox" checked={filters.dashcamOther} onChange={e => onFiltersChange({...filters, dashcamOther: e.target.checked})} className="h-5 w-5 accent-[var(--accent)]"/>
                              <span>Other (All Saved)</span>
                          </label>
                       </div>

                       <div className="h-px border-t border-dashed border-[var(--line)]" />

                       <div className="flex flex-col gap-2">
                          <span className="text-[16px] font-semibold text-[var(--muted)] uppercase tracking-[0.08em]">Sentry</span>
                          <label className="flex items-center gap-[11px] min-h-12 cursor-pointer ml-2">
                              <input type="checkbox" checked={filters.sentryObject} onChange={e => onFiltersChange({...filters, sentryObject: e.target.checked})} className="h-5 w-5 accent-[var(--accent)]"/>
                              <span>Object Detection</span>
                          </label>
                          <label className="flex items-center gap-[11px] min-h-12 cursor-pointer ml-2">
                              <input type="checkbox" checked={filters.sentryAccel} onChange={e => onFiltersChange({...filters, sentryAccel: e.target.checked})} className="h-5 w-5 accent-[var(--accent)]"/>
                              <span>Acceleration</span>
                          </label>
                          <label className="flex items-center gap-[11px] min-h-12 cursor-pointer ml-2">
                              <input type="checkbox" checked={filters.sentryOther} onChange={e => onFiltersChange({...filters, sentryOther: e.target.checked})} className="h-5 w-5 accent-[var(--accent)]"/>
                              <span>Other (All Sentry)</span>
                          </label>
                       </div>
                   </div>
               )}
             </div>

             <button
                type="button"
                onClick={() => setIsCalendarOpen(!isCalendarOpen)}
                aria-label={isCalendarOpen ? "Hide Calendar" : "Show Calendar"}
                aria-expanded={showCalendar}
                className="desk-lightbtn"
                aria-pressed={showCalendar}
             >
                <CalendarIcon size={18} />
                {dateLabel}
             </button>
             <button
                type="button"
                onClick={onOpenMap}
                aria-label="View Map"
                className="desk-iconbtn"
                title="View Map"
             >
                <MapIcon size={18} />
             </button>
          </div>

          {showCalendar && (
            <div className="pb-[18px]">
              <Calendar
                  currentDate={selectedDate}
                  onDateSelect={handleDateSelect}
                  clips={clips}
              />
            </div>
          )}
       </div>

       <div className="px-[19px] pt-3 pb-2 text-[var(--muted)] tracking-[0.1em] flex justify-between">
          <span>EVENTS / {padCount(filteredClips.length)}</span>
          <span>NEWEST FIRST</span>
       </div>

       <div className="flex-1 overflow-y-auto min-h-0" aria-live="polite">
          {loading ? (
             <div className="desk-state">
                <div className="desk-state-inner">
                  <div className="desk-spin" aria-hidden="true" style={{
                    width: 28,
                    height: 28,
                    border: '2px solid var(--line)',
                    borderTopColor: 'var(--accent)',
                    borderRadius: '50%',
                    margin: '0 auto 20px',
                  }} />
                  <span className="desk-eyebrow">// INDEXING FOOTAGE</span>
                  <p style={{ color: 'var(--muted)', marginTop: 10 }}>Loading footage…</p>
                </div>
             </div>
          ) : fetchError ? (
             <div className="desk-state">
                <div className="desk-state-inner">
                  <span className="desk-eyebrow">// LIBRARY UNAVAILABLE</span>
                  <h3 className="desk-title text-[30px] mt-0 mb-2">Could not load clips.</h3>
                  <p style={{ color: 'var(--muted)', lineHeight: 1.6 }}>The library request failed.</p>
                  <button type="button" className="desk-primary" onClick={onRefresh} style={{ marginTop: 8 }}>
                    RETRY
                  </button>
                </div>
             </div>
          ) : filteredClips.length === 0 ? (
             <div className="desk-state">
                <div className="desk-state-inner">
                  <Inbox size={32} className="mx-auto mb-3 text-[var(--muted)]" />
                  <span className="desk-eyebrow">// NO MATCHES</span>
                  <h3 className="desk-title text-[30px] mt-2 mb-2">No events shown.</h3>
                  <p style={{ color: 'var(--muted)', lineHeight: 1.6 }}>
                    {clips.length === 0
                      ? 'No clips are indexed yet.'
                      : `No clips match your current filters for ${selectedDate.toLocaleDateString()}.`}
                  </p>
                  {(!isToday(selectedDate) || clips.length > 0) && (
                    <button
                      type="button"
                      onClick={handleResetFilters}
                      className="desk-primary"
                      style={{ marginTop: 8 }}
                    >
                      Reset filters
                    </button>
                  )}
                </div>
             </div>
          ) : (
            <div>
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
    </aside>
  );
};

export default React.memo(Sidebar);
