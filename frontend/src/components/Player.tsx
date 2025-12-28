import React, { useState, useEffect, useRef, Suspense, useCallback, useMemo } from 'react';
import VideoPlayer from './VideoPlayer';
import TelemetryOverlay from './TelemetryOverlay';
import Timeline from './Timeline';
import { Box, Layers, Video, RotateCw, RotateCcw, Play, Pause, Settings } from 'lucide-react';

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
  const [playbackSpeed, setPlaybackSpeed] = useState(1);

  // Transcoding State
  const [quality, setQuality] = useState<string>('original');
  const [encoderStatus, setEncoderStatus] = useState<{encoder: string, hw_accel: boolean} | null>(null);
  const [isQualityMenuOpen, setIsQualityMenuOpen] = useState(false);

  // Fetch transcoder status on mount
  useEffect(() => {
    fetch('/api/transcode/status')
      .then(res => res.json())
      .then(data => setEncoderStatus(data))
      .catch(err => console.error("Failed to fetch encoder status", err));
  }, []);

  // Group segments by camera
  const segments = useMemo(() => {
      if (!clip?.video_files) return {};

      const grouped: { [key: string]: CameraSegment[] } = {};

      // 1. Group by normalized camera name
      clip.video_files.forEach(f => {
          const cam = normalizeCameraName(f.camera);
          if (!grouped[cam]) grouped[cam] = [];

          const tsMs = Date.parse(f.timestamp);
          if (!Number.isFinite(tsMs)) return;

          const tsSeconds = tsMs / 1000;

          grouped[cam].push({
              file_path: f.file_path,
              timestamp: tsSeconds,
              startTime: 0,
              duration: 60 // Estimate
          });
      });

      // 2. Sort and calculate offsets using sanitized durations to avoid runaway timelines
      Object.keys(grouped).forEach(cam => {
          const camSegments = grouped[cam];
          camSegments.sort((a, b) => a.timestamp - b.timestamp);

          let accumulatedStart = 0;
          camSegments.forEach((seg, idx) => {
              seg.startTime = accumulatedStart;

              const next = camSegments[idx + 1];
              const rawDuration = next ? next.timestamp - seg.timestamp : seg.duration;
              const safeDuration = Number.isFinite(rawDuration)
                ? Math.min(120, Math.max(1, rawDuration))
                : 60;

              seg.duration = safeDuration;
              accumulatedStart += safeDuration;
          });
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

  useEffect(() => {
    Object.values(playersRef.current).forEach((p: any) => {
      if (p && typeof p.playbackRate === 'function') {
        p.playbackRate(playbackSpeed);
      }
    });

    if (mainPlayerRef.current && typeof mainPlayerRef.current.playbackRate === 'function') {
        mainPlayerRef.current.playbackRate(playbackSpeed);
    }
  }, [playbackSpeed]);

  // Sync play/pause state across all players
  useEffect(() => {
      Object.values(playersRef.current).forEach((p: any) => {
          if (!p) return;
          if (isPlaying) {
              if (p.paused()) {
                  p.play().catch(() => {});
              }
          } else {
              if (!p.paused()) {
                  p.pause();
              }
          }
      });
  }, [isPlaying]);

  const handlePlayerReady = useCallback((camera: string, player: any) => {
    if (!player) return;
    playersRef.current[camera] = player;

    if (typeof player.playbackRate === 'function') {
        player.playbackRate(playbackSpeed);
    }

    const normCam = normalizeCameraName(camera);
    const frontExists = !!segments['front'];
    // Set as main if it's Front, OR if Front doesn't exist and we don't have a main player yet.
    const isMain = normCam === 'front' || (!frontExists && !mainPlayerRef.current);

    if (isMain) {
      mainPlayerRef.current = player;

      const checkAdvance = () => {
         const camSegments = segments[normCam];
         if (camSegments) {
             let src = player.currentSrc();
             // Try to decode in case video.js encoded it
             try { src = decodeURIComponent(src); } catch (e) {}

             // Remove query params for matching
             src = src.split('?')[0];

             const idx = camSegments.findIndex(s => src.endsWith(s.file_path));
             if (idx !== -1 && idx < camSegments.length - 1) {
                 // Advance to next segment
                 const nextSeg = camSegments[idx+1];
                 console.log("Advancing to next segment:", nextSeg.file_path);
                 setCurrentTime(nextSeg.startTime);
             } else {
                 setIsPlaying(false);
             }
         }
      };

      player.on('timeupdate', () => {
        const camSegments = segments[normCam];
        if (camSegments) {
            let src = player.currentSrc();
            try { src = decodeURIComponent(src); } catch (e) {}
            // Remove query params
            src = src.split('?')[0];

            const seg = camSegments.find(s => src.endsWith(s.file_path));
            if (seg) {
                const global = seg.startTime + player.currentTime();
                // Avoid state update loops if close enough?
                if (Math.abs(global - currentTime) > 0.1) {
                     setCurrentTime(global);
                }
            }

            // Check for end of segment manually (fallback for 'ended' event)
            if (player.duration() > 0 && player.currentTime() >= player.duration() - 0.2) {
                // Debounce or ensure we don't spam?
                // Actually, if we advance, component remounts.
                if (!player.paused()) {
                    checkAdvance();
                }
            }
        }
      });

      player.on('ended', () => {
         checkAdvance();
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
    });
    player.on('pause', () => setIsPlaying(false));

  }, [segments, isPlaying]); // Removed currentTime from deps to avoid re-binding

  const cyclePlaybackSpeed = () => {
    const speeds = [0.5, 1, 1.5, 2, 4];
    const nextIndex = (speeds.indexOf(playbackSpeed) + 1) % speeds.length;
    setPlaybackSpeed(speeds[nextIndex]);
  };

  const togglePlay = useCallback(() => {
      const player = mainPlayerRef.current || Object.values(playersRef.current)[0];
      if (player) {
          if (player.paused()) player.play();
          else player.pause();
      }
  }, []);

  const handleSeek = useCallback((time: number) => {
      const newTime = Math.max(0, Math.min(time, totalDuration));
      setCurrentTime(newTime);

      // We need to sync the players to this new time
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
                   let src = p.currentSrc();
                   try { src = decodeURIComponent(src); } catch (e) {}
                   src = src.split('?')[0];

                   if (src && src.endsWith(info.segment.file_path)) {
                       const localTime = newTime - info.segment.startTime;
                       // Only seek if difference is significant to avoid stutter
                       if (Math.abs(p.currentTime() - localTime) > 0.5) {
                           p.currentTime(localTime);
                       }
                   }
               }
          }
      });
  }, [totalDuration, segments, getSegmentAtTime]);

  // Global Keyboard Shortcuts
  const currentTimeRef = useRef(currentTime);
  useEffect(() => { currentTimeRef.current = currentTime; }, [currentTime]);

  useEffect(() => {
    const handleKeyDown = (e: KeyboardEvent) => {
        if (['INPUT', 'TEXTAREA'].includes((e.target as HTMLElement).tagName)) return;
        // Skip if timeline is focused (it handles its own keys)
        if ((e.target as HTMLElement).getAttribute('role') === 'slider') return;

        switch(e.key.toLowerCase()) {
            case ' ':
            case 'k':
                e.preventDefault();
                togglePlay();
                break;
            case 'arrowleft':
                e.preventDefault();
                handleSeek(currentTimeRef.current - 5);
                break;
            case 'arrowright':
                e.preventDefault();
                handleSeek(currentTimeRef.current + 5);
                break;
            case 'j':
                e.preventDefault();
                handleSeek(currentTimeRef.current - 15);
                break;
            case 'l':
                e.preventDefault();
                handleSeek(currentTimeRef.current + 15);
                break;
        }
    };

    window.addEventListener('keydown', handleKeyDown);
    return () => window.removeEventListener('keydown', handleKeyDown);
  }, [togglePlay, handleSeek]);

  const getUrl = (path: string) => {
    let url = `/api/video${path}`;
    if (quality !== 'original') {
        url += `?quality=${quality}`;
    }
    return url;
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
  const qualities = ['original', '1080p', '720p', '480p'];

  // Render a camera slot
  const renderCamera = (camName: string, className: string) => {
      const seg = getCurrentSegment(camName);

      const onReady = (p: any) => {
          handlePlayerReady(camName, p);
          if (seg) {
              const localTime = currentTime - seg.startTime;
              // Seek if needed (initial load of segment)
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
                      key={`${clip.ID}-${camName}-${seg.file_path}-${quality}`} // Key forces remount on segment change OR quality change
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
                       currentTime={currentTime}
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
                  title="Rewind 15s (J)"
                  className="w-10 h-10 flex items-center justify-center bg-gray-800 text-white rounded-full hover:bg-gray-700 transition focus-visible:ring-2 focus-visible:ring-blue-500 outline-none"
              >
                  <RotateCcw size={20} />
              </button>
              <button
                  onClick={togglePlay}
                  aria-label={isPlaying ? "Pause" : "Play"}
                  title={isPlaying ? "Pause (Space/K)" : "Play (Space/K)"}
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
                  title="Skip 15s (L)"
                  className="w-10 h-10 flex items-center justify-center bg-gray-800 text-white rounded-full hover:bg-gray-700 transition focus-visible:ring-2 focus-visible:ring-blue-500 outline-none"
              >
                  <RotateCw size={20} />
              </button>
              <button
                  onClick={cyclePlaybackSpeed}
                  aria-label={`Playback speed: ${playbackSpeed}x`}
                  title="Change playback speed"
                  className="w-10 h-10 flex items-center justify-center bg-gray-800 text-white rounded-full hover:bg-gray-700 transition focus-visible:ring-2 focus-visible:ring-blue-500 outline-none text-xs font-bold font-mono"
              >
                  {playbackSpeed}x
              </button>

              {/* Quality Selector */}
              <div className="relative">
                  <button
                      onClick={() => setIsQualityMenuOpen(!isQualityMenuOpen)}
                      aria-label={`Quality: ${quality}`}
                      title={`Quality: ${quality}`}
                      className="w-10 h-10 flex items-center justify-center bg-gray-800 text-white rounded-full hover:bg-gray-700 transition focus-visible:ring-2 focus-visible:ring-blue-500 outline-none group/settings"
                  >
                      <Settings size={20} />
                  </button>
                  {isQualityMenuOpen && (
                      <div className="absolute bottom-full mb-2 left-1/2 -translate-x-1/2 bg-gray-900 border border-gray-700 rounded-lg shadow-xl overflow-hidden min-w-[120px]">
                           {encoderStatus && (
                               <div className="px-4 py-2 text-[10px] text-gray-500 border-b border-gray-800 uppercase font-bold tracking-wider">
                                   Encoder: {encoderStatus.encoder}
                               </div>
                           )}
                           {qualities.map(q => (
                               <button
                                   key={q}
                                   onClick={() => { setQuality(q); setIsQualityMenuOpen(false); }}
                                   className={`w-full text-left px-4 py-2 text-xs hover:bg-gray-800 transition ${quality === q ? 'text-blue-400 font-bold bg-gray-800' : 'text-gray-300'}`}
                               >
                                   {q.toUpperCase()}
                               </button>
                           ))}
                      </div>
                  )}
              </div>
          </div>
       </div>
    </div>
  );
};

export default Player;
