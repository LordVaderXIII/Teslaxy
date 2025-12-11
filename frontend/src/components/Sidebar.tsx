import React, { useState } from 'react';
import { Filter, RefreshCw, Calendar as CalendarIcon } from 'lucide-react';
import Calendar from './Calendar';

interface VideoFile {
  camera: string;
  file_path: string;
}

interface Clip {
  ID: number;
  timestamp: string;
  event: string;
  city: string;
  video_files?: VideoFile[];
  telemetry?: any;
}

interface SidebarProps {
  clips: Clip[];
  selectedClipId: number | null;
  onClipSelect: (clip: Clip) => void;
  loading: boolean;
  className?: string;
}

const Sidebar: React.FC<SidebarProps> = ({ clips, selectedClipId, onClipSelect, loading, className }) => {
  const [filterType, setFilterType] = useState<string>('All');
  const [selectedDate, setSelectedDate] = useState<Date>(new Date());
  const [isCalendarOpen, setIsCalendarOpen] = useState(false);

  // Filter Logic
  const filteredClips = clips.filter(clip => {
    // Date Filter
    const clipDate = new Date(clip.timestamp);
    const sameDay = clipDate.toDateString() === selectedDate.toDateString();

    // Type Filter
    const typeMatch = filterType === 'All' || clip.event === filterType;

    return sameDay && typeMatch;
  });

  // Get unique event types for dropdown
  const eventTypes = ['All', ...Array.from(new Set(clips.map(c => c.event)))];

  return (
    <div className={`flex flex-col bg-black border-gray-800 w-full md:w-96 md:h-full md:flex-shrink-0 border-t md:border-l md:border-t-0 ${className || ''}`}>
       {/* Header */}
       <div className="p-4 border-b border-gray-800 flex flex-col gap-4">
          <div className="flex justify-between items-center">
              <h2 className="text-xl font-bold text-white">Library</h2>
              <div className="flex items-center gap-2">
                  <button
                    onClick={() => setIsCalendarOpen(!isCalendarOpen)}
                    className="p-2 bg-gray-900 rounded-full hover:bg-gray-800 transition text-gray-400 hover:text-white md:hidden"
                    title="Toggle Calendar"
                  >
                     <CalendarIcon size={18} />
                  </button>
                  <button
                    aria-label="Refresh library"
                    className="p-2 bg-gray-900 rounded-full hover:bg-gray-800 transition text-gray-400 hover:text-white outline-none focus-visible:ring-2 focus-visible:ring-blue-500"
                    title="Sync"
                  >
                     <RefreshCw size={18} />
                  </button>
              </div>
          </div>

          {/* Calendar (Collapsible on Mobile, always visible on Desktop?)
              Actually, making it collapsible everywhere is cleaner, or just auto-open on desktop.
              For now, let's make it toggleable but open by default on desktop if we wanted,
              but user said "can be opened", implying it might be closed.
          */}
          <div className={`${isCalendarOpen ? 'block' : 'hidden'} md:block transition-all duration-300 ease-in-out`}>
            <Calendar
                currentDate={selectedDate}
                onDateSelect={(date) => {
                    setSelectedDate(date);
                    // Optional: Close calendar on selection on mobile?
                    // setIsCalendarOpen(false);
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
             <div className="p-8 text-center text-gray-600">
                No clips found for {selectedDate.toLocaleDateString()}
             </div>
          ) : (
            <div className="grid grid-cols-1 divide-y divide-gray-900">
               {filteredClips.map(clip => {
                  const frontVideo = clip.video_files?.find(v => v.camera === 'Front');

                  return (
                  <button
                    key={clip.ID}
                    onClick={() => onClipSelect(clip)}
                    aria-current={selectedClipId === clip.ID ? 'true' : undefined}
                    className={`
                       w-full text-left outline-none focus-visible:ring-2 focus-visible:ring-blue-500 focus-visible:ring-inset
                       p-4 hover:bg-gray-900 transition flex gap-3
                       ${selectedClipId === clip.ID ? 'bg-gray-900 border-l-4 border-blue-500' : 'border-l-4 border-transparent'}
                    `}
                  >
                     {/* Thumbnail Placeholder or Icon */}
                     <div className="w-12 h-12 rounded flex-shrink-0 overflow-hidden bg-gray-800 relative">
                        {frontVideo ? (
                           <img
                              src={`/api/thumbnail${frontVideo.file_path}`}
                              alt="Thumbnail"
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
               })}
            </div>
          )}
       </div>
    </div>
  );
};

export default Sidebar;
