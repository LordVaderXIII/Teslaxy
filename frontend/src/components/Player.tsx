import React, { useState, useEffect, useRef, Suspense, useCallback, useMemo } from 'react';
import VideoPlayer from './VideoPlayer';
import TelemetryOverlay from './TelemetryOverlay';
import Timeline from './Timeline';
import { Box, Layers, Video, RotateCw, RotateCcw, Play, Pause } from 'lucide-react';

const Scene3D = React.lazy(() => import('./Scene3D'));

interface VideoFile {
  camera: string;
  file_path: string;
  timestamp: string;
}

interface Clip {
  ID: number;
  video_files?: VideoFile[];
  telemetry?: any;
  event: string;
  timestamp: string;
  event_timestamp?: string;
}

interface CameraSegment {
    file_path: string;
    timestamp: number; // Unix timestamp in seconds
    startTime: number; // Offset from event start in seconds
    duration: number; // Estimated duration (default 60s)
}

const normalizeCameraName = (name: string) => {
  return name.toLowerCase().replace(/[^a-z0-9]/g, '');
};

const Player: React.FC<{ clip: Clip | null }> = ({ clip }) => {
  const [isPlaying, setIsPlaying] = useState(false);
  const [currentTime, setCurrentTime] = useState(0);
  const [is3D, setIs3D] = useState(false);
  const [activeCamera, setActiveCamera] = useState<string>('Front');
  const [isCameraMenuOpen, setIsCameraMenuOpen] = useState(false);

  // Group segments by camera
  const segments = useMemo(() => {
      if (!clip?.video_files) return {};

      const grouped: { [key: string]: CameraSegment[] } = {};

      // 1. Group by normalized camera name
      clip.video_files.forEach(f => {
          const cam = normalizeCameraName(f.camera);
          if (!grouped[cam]) grouped[cam] = [];

          const ts = new Date(f.timestamp).getTime() / 1000;
          grouped[cam].push({
              file_path: f.file_path,
              timestamp: ts,
              startTime: 0,
              duration: 60 // Estimate
          });
      });

      // 2. Sort and calculate offsets
      Object.keys(grouped).forEach(cam => {
          grouped[cam].sort((a, b) => a.timestamp - b.timestamp);

          if (grouped[cam].length > 0) {
              const startTs = grouped[cam][0].timestamp;
              grouped[cam].forEach((seg, idx) => {
                  seg.startTime = seg.timestamp - startTs;
                  // Adjust duration based on next segment if available
                  if (idx < grouped[cam].length - 1) {
                      seg.duration = grouped[cam][idx+1].timestamp - seg.timestamp;
                  }
              });
          }
      });

      return grouped;
  }, [clip]);

  // Calculate total duration based on Front camera (or fallback)
  const totalDuration = useMemo(() => {
      const cams = Object.keys(segments);
      if (cams.length === 0) return 0;
      // Prefer Front
      const main = segments['front'] || segments[cams[0]];
      if (!main || main.length === 0) return 0;
      const last = main[main.length - 1];
      return last.startTime + last.duration;
  }, [segments]);


  const playersRef = useRef<{ [key: string]: any }>({});
  const mainPlayerRef = useRef<any>(null);

  // Track which segment is currently playing to optimize updates
  const [currentSegmentIndex, setCurrentSegmentIndex] = useState(0);

  // Reset state when clip changes
  useEffect(() => {
      setCurrentTime(0);
      setCurrentSegmentIndex(0);
      setIsPlaying(false);
      mainPlayerRef.current = null;
      playersRef.current = {};
      setActiveCamera('Front');

      return () => {
        playersRef.current = {};
        mainPlayerRef.current = null;
      };
  }, [clip?.ID]);

  // Determine current segment based on global time
  const getSegmentAtTime = useCallback((camera: string, time: number) => {
      const camSegments = segments[normalizeCameraName(camera)];
      if (!camSegments) return null;
      // Find segment where startTime <= time < startTime + duration
      // Since they are sorted, we can just find the last one that started before 'time'
      let idx = camSegments.findIndex(s => s.startTime > time);
      if (idx === -1) idx = camSegments.length; // If time is past all starts, it's the last one (or past end)
      return {
          segment: camSegments[Math.max(0, idx - 1)],
          index: Math.max(0, idx - 1)
      };
  }, [segments]);

  // Sync segment index when time changes significantly (seeking)
  useEffect(() => {
      const info = getSegmentAtTime('Front', currentTime);
      if (info && info.index !== currentSegmentIndex) {
          setCurrentSegmentIndex(info.index);
      }
  }, [currentTime, getSegmentAtTime, currentSegmentIndex]);


  const handlePlayerReady = useCallback((camera: string, player: any) => {
    if (!player) return;
    playersRef.current[camera] = player;

    const normCam = normalizeCameraName(camera);
    const frontExists = !!segments['front'];
    // Set as main if it's Front, OR if Front doesn't exist and we don't have a main player yet.
    const isMain = normCam === 'front' || (!frontExists && !mainPlayerRef.current);

    if (isMain) {
      mainPlayerRef.current = player;

      player.on('timeupdate', () => {
        // Map local time to global time
        const camSegments = segments[normCam];
        if (camSegments) {
            // We need to know WHICH segment this player is currently playing
            // We can infer it from the 'currentSegmentIndex' state or passed props?
            // Actually, inside the callback, state might be stale.
            // But we know the source is set.

            // Better: use the pre-calculated offset for the CURRENT segment
            // We can't easily get the segment index from the player object.
            // Let's rely on the outer 'currentSegmentIndex' via a ref if needed, or just calculate:

            // Wait, this closure captures 'segments'.
            // But we need the *active* segment for this player.
            // Since we re-mount the player (via key change) when segment changes,
            // this 'handlePlayerReady' might be called for EACH segment.
            // But 'timeupdate' fires continuously.

            // Actually, we are using the 'key' prop on VideoPlayer to force remount.
            // So 'player' instance is specific to that segment.
            // So we can find which segment corresponds to player.src()
            // But src is a full URL.

            // Simpler: Use a ref to track current segment start time?
            // No, the safest way is: The Player component knows the global time.
            // But the 'timeupdate' drives the global time.

            // Let's look at how we render VideoPlayer:
            // key={`${clip.ID}-${camera}-${segment.file_path}`}

            // So when this callback runs, it runs for a specific segment.
            // We need to know the offset of THAT segment.
            // We can look it up in 'segments' using the src? Or pass it?
            // We can't pass extra args to onReady easily without wrapper.
            // Let's just find the segment in the list that matches the src.

            // NOTE: player.currentSrc() returns full URL.
            const src = player.currentSrc();
            const seg = camSegments.find(s => src.endsWith(s.file_path));
            if (seg) {
                const global = seg.startTime + player.currentTime();
                // Avoid state update loops if close enough?
                if (Math.abs(global - currentTime) > 0.1) {
                     setCurrentTime(global);
                }
            }
        }
      });

      player.on('ended', () => {
         // Auto-advance
         const camSegments = segments[normCam];
         if (camSegments) {
             const src = player.currentSrc();
             const idx = camSegments.findIndex(s => src.endsWith(s.file_path));
             if (idx !== -1 && idx < camSegments.length - 1) {
                 // Advance to next segment
                 const nextSeg = camSegments[idx+1];
                 setCurrentTime(nextSeg.startTime);
                 // This updates state -> Re-renders VideoPlayer with new Src -> New Player -> Autoplay
             } else {
                 setIsPlaying(false);
             }
         }
      });

      // Auto-play if global state is playing
      if (isPlaying) {
          player.play().catch(() => {});
      }

    } else {
        if (typeof player.muted === 'function') {
             player.muted(true);
        }
    }

    // Sync play state
    player.on('play', () => {
       setIsPlaying(true);
       // Sync others
    });
    player.on('pause', () => setIsPlaying(false));

  }, [segments, isPlaying]); // Removed currentTime from deps to avoid re-binding

  const togglePlay = () => {
      const player = mainPlayerRef.current || Object.values(playersRef.current)[0];
      if (player) {
          if (player.paused()) player.play();
          else player.pause();
      }
  };

  const handleSeek = (time: number) => {
      const newTime = Math.max(0, Math.min(time, totalDuration));
      setCurrentTime(newTime);

      // We need to sync the players to this new time
      // 1. Find target segment
      // 2. Calculate local time
      // 3. If segment is different, the render loop will handle the src switch.
      // 4. If segment is SAME, we need to seek the player.

      Object.keys(segments).forEach(cam => {
          const info = getSegmentAtTime(cam, newTime);
          if (info) {
               // Let's just try seeking. If src changes, player is destroyed anyway.
               const p = playersRef.current[cam === 'front' ? 'Front' :
                          cam === 'left_repeater' ? 'Left Repeater' :
                          cam === 'right_repeater' ? 'Right Repeater' :
                          cam === 'back' ? 'Back' :
                          cam === 'left_pillar' ? 'Left Pillar' :
                          cam === 'right_pillar' ? 'Right Pillar' : cam];

               if (p) {
                   // Check if player src matches target segment
                   // If not, don't seek (wait for remount)
                   const src = p.currentSrc();
                   if (src && src.endsWith(info.segment.file_path)) {
                       p.currentTime(newTime - info.segment.startTime);
                   }
               }
          }
      });
  };

  const getUrl = (path: string) => {
    return `/api/video${path}`;
  };

  // Helper to get current segment for a camera
  const getCurrentSegment = (cameraName: string) => {
      const info = getSegmentAtTime(cameraName, currentTime);
      return info ? info.segment : null;
  };

  if (!clip) return <div className="flex items-center justify-center h-full text-gray-500">Select a clip to play</div>;

  // Determine Incident Marker
  const markers = [];
  if (clip.event === 'Sentry' || clip.event === 'Saved') {
      if (clip.event_timestamp) {
          const eventTime = new Date(clip.event_timestamp).getTime() / 1000;
          const clipStart = new Date(clip.timestamp).getTime() / 1000;
          const offset = eventTime - clipStart;
          if (offset >= 0 && offset <= totalDuration) {
              markers.push({ time: offset, color: '#ef4444', label: 'Event' });
          }
      }
  }

  const cameras = ['Front', 'Left Repeater', 'Right Repeater', 'Back', 'Left Pillar', 'Right Pillar'];

  // Render a camera slot
  const renderCamera = (camName: string, className: string) => {
      const seg = getCurrentSegment(camName);
      // We need to calculate start time for the player (if we just mounted it, it needs to seek)
      // Actually, if we mount a new player, we should pass 'startTime' logic?
      // VideoPlayer doesn't accept startTime.
      // We can use 'onReady' to seek.

      const onReady = (p: any) => {
          handlePlayerReady(camName, p);
          if (seg) {
              const localTime = currentTime - seg.startTime;
              if (Math.abs(p.currentTime() - localTime) > 0.5) {
                  p.currentTime(localTime);
              }
              if (isPlaying) p.play().catch(() => {});
          }
      };

      return (
          <div className={`relative bg-gray-900 group/cam overflow-hidden min-w-0 min-h-0 ${className} ${activeCamera === camName ? 'block h-full' : 'hidden md:block'}`}>
              {seg ? (
                  <VideoPlayer
                      key={`${clip.ID}-${camName}-${seg.file_path}`} // Key forces remount on segment change
                      src={getUrl(seg.file_path)}
                      className="w-full h-full object-contain"
                      onReady={onReady}
                  />
              ) : (
                  <div className="flex items-center justify-center h-full text-gray-600">No {camName}</div>
              )}
               <div className="absolute bottom-4 left-4 bg-black/50 px-3 py-1 rounded-full text-xs font-mono backdrop-blur border border-white/10 pointer-events-none">
                  {camName}
              </div>
              {camName === 'Front' && clip.telemetry && clip.telemetry.full_data_json && (
                   <TelemetryOverlay
                       dataJson={clip.telemetry.full_data_json}
                       currentTime={currentTime} // TODO: Telemetry needs syncing with global time? Telemetry currently is 1 record per clip?
                       // If telemetry is also segmented, we need to handle that.
                       // For now, assuming telemetry is attached to the clip object (which is now the Event).
                       // Backend: "Store full thing as JSON".
                       // The 'full_data_json' is likely from the FIRST segment only in my backend implementation.
                       // Ideally, we should stitch telemetry too.
                       // But for now, let's leave it.
                   />
              )}
          </div>
      );
  };

  return (
    <div className="flex flex-col h-full bg-black text-white relative group">

      {/* Overlays */}
      <div className="absolute top-4 right-4 z-20 flex gap-2">
            <div className="relative md:hidden">
                <button
                    onClick={() => setIsCameraMenuOpen(!isCameraMenuOpen)}
                    aria-label={isCameraMenuOpen ? "Close camera menu" : "Open camera menu"}
                    aria-expanded={isCameraMenuOpen}
                    className="p-2 bg-black/50 backdrop-blur border border-white/10 rounded-lg hover:bg-white/10 transition text-white focus-visible:ring-2 focus-visible:ring-blue-500 outline-none"
                >
                    <Video size={20} />
                </button>
                {isCameraMenuOpen && (
                    <div className="absolute right-0 top-full mt-2 w-48 bg-gray-900 border border-gray-700 rounded-lg shadow-xl overflow-hidden z-30">
                        {cameras.map(cam => (
                            <button
                                key={cam}
                                onClick={() => { setActiveCamera(cam); setIsCameraMenuOpen(false); }}
                                className={`w-full text-left px-4 py-3 text-sm hover:bg-gray-800 transition ${activeCamera === cam ? 'text-blue-400 font-bold bg-gray-800' : 'text-gray-300'}`}
                            >
                                {cam}
                            </button>
                        ))}
                    </div>
                )}
            </div>

            <button
                onClick={() => setIs3D(!is3D)}
                aria-label={is3D ? "Switch to 2D view" : "Switch to 3D view"}
                title={is3D ? "Switch to 2D view" : "Switch to 3D view"}
                className="p-2 bg-black/50 backdrop-blur border border-white/10 rounded-lg hover:bg-white/10 transition text-white focus-visible:ring-2 focus-visible:ring-blue-500 outline-none"
            >
                {is3D ? <Layers size={20} /> : <Box size={20} />}
            </button>
      </div>

      {is3D ? (
          <div className="flex-1 bg-gray-900 overflow-hidden relative flex flex-col">
             <div className="flex-1 relative">
                 {/* 3D View requires 6 streams. We grab the current segments for all 6. */}
                 <Suspense fallback={<div className="flex items-center justify-center h-full text-white">Loading 3D...</div>}>
                     <Scene3D
                        frontSrc={getCurrentSegment('Front')?.file_path ? getUrl(getCurrentSegment('Front')!.file_path) : ''}
                        leftRepeaterSrc={getCurrentSegment('Left Repeater')?.file_path ? getUrl(getCurrentSegment('Left Repeater')!.file_path) : ''}
                        rightRepeaterSrc={getCurrentSegment('Right Repeater')?.file_path ? getUrl(getCurrentSegment('Right Repeater')!.file_path) : ''}
                        backSrc={getCurrentSegment('Back')?.file_path ? getUrl(getCurrentSegment('Back')!.file_path) : ''}
                        leftPillarSrc={getCurrentSegment('Left Pillar')?.file_path ? getUrl(getCurrentSegment('Left Pillar')!.file_path) : ''}
                        rightPillarSrc={getCurrentSegment('Right Pillar')?.file_path ? getUrl(getCurrentSegment('Right Pillar')!.file_path) : ''}
                        onVideoReady={handlePlayerReady}
                     />
                 </Suspense>
             </div>
          </div>
      ) : (
          <div className="flex-1 overflow-hidden grid grid-cols-1 md:grid-cols-3 md:grid-rows-[3fr_1fr_1fr] gap-1 bg-black min-h-0">
              {renderCamera('Front', 'md:col-span-3')}
              {renderCamera('Left Pillar', '')}
              {renderCamera('Back', 'md:row-span-2')}
              {renderCamera('Right Pillar', '')}
              {renderCamera('Left Repeater', '')}
              {renderCamera('Right Repeater', '')}
          </div>
      )}

      {/* Controls */}
       <div className="p-4 bg-black border-t border-gray-900 flex-shrink-0 z-30">
          <Timeline
            currentTime={currentTime}
            duration={totalDuration}
            onSeek={handleSeek}
            markers={markers}
          />
          <div className="flex items-center justify-center gap-4 mt-2">
              <button
                  onClick={() => handleSeek(Math.max(0, currentTime - 15))}
                  aria-label="Rewind 15 seconds"
                  title="Rewind 15 seconds"
                  className="w-10 h-10 flex items-center justify-center bg-gray-800 text-white rounded-full hover:bg-gray-700 transition focus-visible:ring-2 focus-visible:ring-blue-500 outline-none"
              >
                  <RotateCcw size={20} />
              </button>
              <button
                  onClick={togglePlay}
                  aria-label={isPlaying ? "Pause" : "Play"}
                  title={isPlaying ? "Pause" : "Play"}
                  className="w-12 h-12 flex items-center justify-center bg-white text-black rounded-full hover:bg-gray-200 transition focus-visible:ring-2 focus-visible:ring-blue-500 outline-none"
              >
                  {isPlaying ? (
                    <Pause size={24} fill="currentColor" stroke="none" />
                  ) : (
                    <Play size={24} fill="currentColor" stroke="none" />
                  )}
              </button>
              <button
                  onClick={() => handleSeek(Math.min(totalDuration, currentTime + 15))}
                  aria-label="Skip forward 15 seconds"
                  title="Skip forward 15 seconds"
                  className="w-10 h-10 flex items-center justify-center bg-gray-800 text-white rounded-full hover:bg-gray-700 transition focus-visible:ring-2 focus-visible:ring-blue-500 outline-none"
              >
                  <RotateCw size={20} />
              </button>
          </div>
       </div>
    </div>
  );
};

export default Player;
