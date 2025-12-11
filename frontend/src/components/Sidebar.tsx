import React, { useState } from 'react';
import { Filter, RefreshCw } from 'lucide-react';
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
}

const Sidebar: React.FC<SidebarProps> = ({ clips, selectedClipId, onClipSelect, loading }) => {
  const [filterType, setFilterType] = useState<string>('All');
  const [selectedDate, setSelectedDate] = useState<Date>(new Date());

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
    <div className="w-96 flex flex-col h-full bg-black border-l border-gray-800 flex-shrink-0">
       {/* Header */}
       <div className="p-4 border-b border-gray-800 flex flex-col gap-4">
          <div className="flex justify-between items-center">
              <h2 className="text-xl font-bold text-white">Library</h2>
              <button className="p-2 bg-gray-900 rounded-full hover:bg-gray-800 transition text-gray-400 hover:text-white" title="Sync">
                 <RefreshCw size={18} />
              </button>
          </div>

          {/* Calendar */}
          <Calendar
            currentDate={selectedDate}
            onDateSelect={setSelectedDate}
            clips={clips}
          />

          {/* Filter */}
          <div className="relative">
             <div className="absolute inset-y-0 left-0 pl-3 flex items-center pointer-events-none">
                <Filter size={14} className="text-gray-500" />
             </div>
             <select
               className="w-full bg-gray-900 border border-gray-800 text-gray-300 text-sm rounded-lg focus:ring-blue-500 focus:border-blue-500 block w-full pl-10 p-2.5"
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
       <div className="flex-1 overflow-y-auto custom-scrollbar">
          {loading ? (
             <div className="p-8 text-center text-gray-500 animate-pulse">Loading footage...</div>
          ) : filteredClips.length === 0 ? (
             <div className="p-8 text-center text-gray-600">
                No clips found for {selectedDate.toLocaleDateString()}
             </div>
          ) : (
            <div className="grid grid-cols-1 divide-y divide-gray-900">
               {filteredClips.map(clip => {
                  const frontVideo = clip.video_files?.find(v => v.camera === 'Front');

                  return (
                  <div
                    key={clip.ID}
                    onClick={() => onClipSelect(clip)}
                    className={`
                       p-4 cursor-pointer hover:bg-gray-900 transition flex gap-3
                       ${selectedClipId === clip.ID ? 'bg-gray-900 border-l-4 border-blue-500' : 'border-l-4 border-transparent'}
                    `}
                  >
                     {/* Thumbnail Placeholder or Icon */}
                     <div className="w-12 h-12 rounded flex-shrink-0 overflow-hidden bg-gray-800 relative">
                        {frontVideo ? (
                           <video
                              src={`/api/video${frontVideo.file_path}#t=0.1`}
                              className="w-full h-full object-cover"
                              preload="metadata"
                              muted
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
                  </div>
                  );
               })}
            </div>
          )}
       </div>
    </div>
  );
};

export default Sidebar;
