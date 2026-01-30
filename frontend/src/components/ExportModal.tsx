import React, { useState, useEffect, useRef } from 'react';
import { X, Check, Download, AlertTriangle } from 'lucide-react';
import type { Clip } from '../utils/clipMerge';

interface ExportModalProps {
  isOpen: boolean;
  onClose: () => void;
  clip: Clip;
  currentTime: number;
  totalDuration: number;
}

const ExportModal: React.FC<ExportModalProps> = ({ isOpen, onClose, clip, currentTime, totalDuration }) => {
  if (!isOpen) return null;

  const [cameras, setCameras] = useState<string[]>(['front', 'back', 'left_repeater', 'right_repeater']);
  const [startTime, setStartTime] = useState(0);
  const [duration, setDuration] = useState(Math.min(60, totalDuration)); // Default 60s or full length
  const [jobId, setJobId] = useState<string | null>(null);
  const [status, setStatus] = useState<string>('idle'); // idle, pending, processing, completed, failed
  const [progress, setProgress] = useState(0);
  const [downloadUrl, setDownloadUrl] = useState<string>('');
  const [error, setError] = useState<string>('');

  // Initialize start time from current playback time
  useEffect(() => {
    if (isOpen) {
      setStartTime(Math.floor(currentTime));
      // Ensure we don't exceed bounds
      const remaining = totalDuration - currentTime;
      setDuration(Math.floor(Math.min(60, remaining > 0 ? remaining : totalDuration)));
    }
  }, [isOpen, currentTime, totalDuration]);

  const toggleCamera = (cam: string) => {
    setCameras(prev =>
      prev.includes(cam) ? prev.filter(c => c !== cam) : [...prev, cam]
    );
  };

  const handleExport = async () => {
    setStatus('pending');
    setError('');
    setProgress(0);

    try {
      const res = await fetch('/api/export', {
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({
          clip_id: clip.ID,
          cameras,
          start_time: Number(startTime),
          duration: Number(duration)
        })
      });

      if (!res.ok) {
        const data = await res.json();
        throw new Error(data.error || 'Export failed');
      }

      const data = await res.json();
      setJobId(data.job_id);
      setStatus('processing');
    } catch (e: any) {
      setStatus('failed');
      setError(e.message);
    }
  };

  // Polling
  useEffect(() => {
    if (!jobId || (status !== 'processing' && status !== 'pending')) return;

    const interval = setInterval(async () => {
      try {
        const res = await fetch(`/api/export/${jobId}`);
        if (!res.ok) return;
        const data = await res.json();

        setStatus(data.status);
        setProgress(data.progress || 0);

        if (data.status === 'completed') {
           setDownloadUrl(`/api/downloads/${data.file_path}`);
           clearInterval(interval);
        } else if (data.status === 'failed') {
           setError(data.error || 'Unknown error');
           clearInterval(interval);
        }
      } catch (e) {
        console.error("Polling error", e);
      }
    }, 1000);

    return () => clearInterval(interval);
  }, [jobId, status]);


  // Range slider helpers
  const containerRef = useRef<HTMLDivElement>(null);
  const [isDragging, setIsDragging] = useState<'start' | 'end' | null>(null);

  const getPercentage = (time: number) => {
      if (totalDuration === 0) return 0;
      return Math.min(100, Math.max(0, (time / totalDuration) * 100));
  }

  const handleMouseDown = (type: 'start' | 'end') => (e: React.MouseEvent) => {
      e.stopPropagation();
      setIsDragging(type);
  };

  useEffect(() => {
      const handleMouseMove = (e: MouseEvent) => {
          if (!isDragging || !containerRef.current) return;
          const rect = containerRef.current.getBoundingClientRect();
          const x = e.clientX - rect.left;
          const percentage = Math.max(0, Math.min(1, x / rect.width));
          const time = percentage * totalDuration;

          if (isDragging === 'start') {
              const newStart = Math.min(time, startTime + duration - 1); // Maintain at least 1s duration
              // Ensure we don't go below 0
              const safeStart = Math.max(0, newStart);
              // Maintain duration or squeeze it?
              // Standard behavior: Start moves, End stays fixed (so duration changes)
              const endTime = startTime + duration;
              let newDur = endTime - safeStart;
              if (newDur < 1) newDur = 1;

              setStartTime(safeStart);
              setDuration(newDur);
          } else {
              // End dragging
              const newEnd = Math.max(startTime + 1, time);
              setDuration(newEnd - startTime);
          }
      };

      const handleMouseUp = () => setIsDragging(null);

      if (isDragging) {
          window.addEventListener('mousemove', handleMouseMove);
          window.addEventListener('mouseup', handleMouseUp);
      }
      return () => {
          window.removeEventListener('mousemove', handleMouseMove);
          window.removeEventListener('mouseup', handleMouseUp);
      }
  }, [isDragging, startTime, duration, totalDuration]);


  return (
    <div className="fixed inset-0 z-50 flex items-center justify-center bg-black/80 backdrop-blur-sm p-4">
      <div className="bg-gray-900 border border-gray-700 rounded-xl shadow-2xl w-full max-w-lg flex flex-col overflow-hidden">
        {/* Header */}
        <div className="flex items-center justify-between p-4 border-b border-gray-800 bg-gray-950">
          <h2 className="text-lg font-semibold text-white">Export Clip</h2>
          <button onClick={onClose} className="text-gray-400 hover:text-white transition">
            <X size={20} />
          </button>
        </div>

        {/* Body */}
        <div className="p-6 space-y-6">

          {/* Camera Selection */}
          <div>
            <label className="block text-sm font-medium text-gray-400 mb-2">Select Cameras</label>
            <div className="grid grid-cols-2 gap-2">
               {[
                 { id: 'front', label: 'Front' },
                 { id: 'back', label: 'Back' },
                 { id: 'left_repeater', label: 'Left Repeater' },
                 { id: 'right_repeater', label: 'Right Repeater' }
               ].map(cam => (
                 <label key={cam.id} className="flex items-center space-x-2 cursor-pointer p-2 rounded hover:bg-gray-800 transition">
                   <input
                     type="checkbox"
                     checked={cameras.includes(cam.id)}
                     onChange={() => toggleCamera(cam.id)}
                     className="w-4 h-4 rounded border-gray-600 bg-gray-800 text-blue-600 focus:ring-blue-500"
                   />
                   <span className="text-gray-300 text-sm">{cam.label}</span>
                 </label>
               ))}
            </div>
          </div>

          {/* Timeline Visual */}
          <div>
             <div className="flex justify-between text-xs text-gray-400 mb-1">
                 <span>Start: {startTime.toFixed(1)}s</span>
                 <span>End: {(startTime + duration).toFixed(1)}s</span>
                 <span>Duration: {duration.toFixed(1)}s</span>
             </div>
             <div
                ref={containerRef}
                className="relative h-8 bg-gray-800 rounded overflow-hidden cursor-pointer select-none border border-gray-700"
             >
                 {/* Selection Range */}
                 <div
                    className="absolute h-full bg-blue-900/50 border-x-2 border-blue-500"
                    style={{
                        left: `${getPercentage(startTime)}%`,
                        width: `${getPercentage(duration)}%`
                    }}
                 >
                     {/* Handles */}
                     <div
                        onMouseDown={handleMouseDown('start')}
                        className="absolute left-0 top-0 bottom-0 w-3 -ml-1.5 hover:bg-green-500/50 cursor-ew-resize transition z-10"
                        title="Drag to adjust start"
                     />
                     <div
                        onMouseDown={handleMouseDown('end')}
                        className="absolute right-0 top-0 bottom-0 w-3 -mr-1.5 hover:bg-red-500/50 cursor-ew-resize transition z-10"
                         title="Drag to adjust end"
                     />
                 </div>
             </div>
             <div className="mt-2 text-[10px] text-gray-500 flex justify-between">
                <span>0s</span>
                <span>{totalDuration.toFixed(0)}s</span>
             </div>
          </div>

          {/* Manual Inputs */}
           <div className="grid grid-cols-2 gap-4">
               <div>
                  <label className="text-xs text-gray-500">Start Time (s)</label>
                  <input
                    type="number"
                    value={startTime}
                    onChange={(e) => {
                        const v = Number(e.target.value);
                        setStartTime(v);
                        if (v + duration > totalDuration) setDuration(totalDuration - v);
                    }}
                    className="w-full bg-gray-950 border border-gray-700 rounded p-2 text-white text-sm"
                  />
               </div>
               <div>
                  <label className="text-xs text-gray-500">Duration (s)</label>
                  <input
                    type="number"
                    value={duration}
                    onChange={(e) => setDuration(Number(e.target.value))}
                    className="w-full bg-gray-950 border border-gray-700 rounded p-2 text-white text-sm"
                  />
               </div>
           </div>

          {/* Status / Actions */}
          <div className="pt-4 border-t border-gray-800">
             {status === 'idle' && (
               <button
                 onClick={handleExport}
                 disabled={cameras.length === 0 || duration <= 0}
                 className="w-full py-3 bg-blue-600 hover:bg-blue-700 text-white font-bold rounded-lg transition disabled:opacity-50 disabled:cursor-not-allowed"
               >
                 Start Export
               </button>
             )}

             {(status === 'pending' || status === 'processing') && (
                 <div className="space-y-2">
                     <div className="flex justify-between text-sm text-gray-300">
                         <span>Processing...</span>
                         <span>{progress.toFixed(0)}%</span>
                     </div>
                     <div className="h-2 bg-gray-800 rounded-full overflow-hidden">
                         <div className="h-full bg-blue-500 transition-all duration-300" style={{ width: `${progress}%` }} />
                     </div>
                 </div>
             )}

             {status === 'completed' && (
                 <a
                   href={downloadUrl}
                   download
                   className="flex items-center justify-center gap-2 w-full py-3 bg-green-600 hover:bg-green-700 text-white font-bold rounded-lg transition"
                 >
                   <Download size={20} />
                   Download MP4
                 </a>
             )}

             {status === 'failed' && (
                 <div className="flex items-center gap-2 text-red-400 bg-red-900/20 p-3 rounded-lg text-sm">
                     <AlertTriangle size={16} />
                     <span>Error: {error}</span>
                     <button onClick={() => setStatus('idle')} className="ml-auto underline hover:text-red-300">Retry</button>
                 </div>
             )}
          </div>

        </div>
      </div>
    </div>
  );
};

export default ExportModal;
