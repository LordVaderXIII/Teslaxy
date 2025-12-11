import React, { useState, useEffect, useRef, Suspense } from 'react';
import VideoPlayer from './VideoPlayer';
import TelemetryOverlay from './TelemetryOverlay';
import Timeline from './Timeline';
import { Box, Layers, Video } from 'lucide-react';

const Scene3D = React.lazy(() => import('./Scene3D'));

interface Clip {
  ID: number;
  video_files?: {
    camera: string;
    file_path: string;
  }[];
  telemetry?: any;
  event: string;
  timestamp: string;
}

const Player: React.FC<{ clip: Clip | null }> = ({ clip }) => {
  const [isPlaying, setIsPlaying] = useState(false);
  const [currentTime, setCurrentTime] = useState(0);
  const [duration, setDuration] = useState(0);
  const [is3D, setIs3D] = useState(false);
  const [activeCamera, setActiveCamera] = useState<string>('Front');
  const [isCameraMenuOpen, setIsCameraMenuOpen] = useState(false);

  const playersRef = useRef<{ [key: string]: any }>({});
  const mainPlayerRef = useRef<any>(null);

  useEffect(() => {
    // Cleanup
    return () => {
      playersRef.current = {};
      mainPlayerRef.current = null;
    };
  }, [clip]);

  // Reset state when clip changes
  useEffect(() => {
      setCurrentTime(0);
      setIsPlaying(false);
      setDuration(0);
      mainPlayerRef.current = null;
      playersRef.current = {};
      setActiveCamera('Front');
  }, [clip?.ID]);

  const handlePlayerReady = (camera: string, player: any) => {
    if (!player) return;
    playersRef.current[camera] = player;

    const frontExists = clip?.video_files?.some(v => v.camera === 'Front');
    // Set as main if it's Front, OR if Front doesn't exist and we don't have a main player yet.
    const isMain = camera === 'Front' || (!frontExists && !mainPlayerRef.current);

    if (isMain) {
      mainPlayerRef.current = player;

      player.on('timeupdate', () => {
        setCurrentTime(player.currentTime());
      });
      player.on('durationchange', () => {
        setDuration(player.duration());
      });

      // Sync other players
      player.on('play', () => {
        setIsPlaying(true);
        Object.values(playersRef.current).forEach(p => {
          if (p && p !== player) p.play();
        });
      });

      player.on('pause', () => {
        setIsPlaying(false);
        Object.values(playersRef.current).forEach(p => {
          if (p && p !== player) p.pause();
        });
      });

      player.on('seeking', () => {
        const time = player.currentTime();
        Object.values(playersRef.current).forEach(p => {
          if (p && p !== player) p.currentTime(time);
        });
      });

      // Attempt to autoplay
      player.play().catch((e: any) => {
          console.warn("Autoplay prevented:", e);
          setIsPlaying(false);
      });

    } else {
        // Mute other players by default to avoid echo
        if (typeof player.muted === 'function') {
             player.muted(true);
        }
    }
  };

  const togglePlay = () => {
      // Try main player, or fallback to any available player
      const player = mainPlayerRef.current || Object.values(playersRef.current)[0];
      if (player) {
          if (player.paused()) {
              player.play();
          } else {
              player.pause();
          }
      }
  };

  const handleSeek = (time: number) => {
      // Try main player, or fallback
      const player = mainPlayerRef.current || Object.values(playersRef.current)[0];
      if (player) {
          player.currentTime(time);
          // Sync others immediately just in case
          Object.values(playersRef.current).forEach(p => {
              if (p && p !== player) p.currentTime(time);
          });
      }
  };

  const getUrl = (path: string) => {
    return `/api/video${path}`;
  };

  if (!clip) return <div className="flex items-center justify-center h-full text-gray-500">Select a clip to play</div>;
  if (!clip.video_files) return <div className="flex items-center justify-center h-full text-gray-500">No video files available</div>;

  const frontVideo = clip.video_files.find(v => v.camera === 'Front');
  const leftVideo = clip.video_files.find(v => v.camera === 'Left Repeater');
  const rightVideo = clip.video_files.find(v => v.camera === 'Right Repeater');
  const backVideo = clip.video_files.find(v => v.camera === 'Back');
  const leftPillarVideo = clip.video_files.find(v => v.camera === 'Left Pillar');
  const rightPillarVideo = clip.video_files.find(v => v.camera === 'Right Pillar');

  // Determine Incident Marker
  const markers = [];
  if (clip.event === 'Sentry') {
      if (duration > 0) {
          markers.push({ time: duration * 0.8, color: '#ef4444', label: 'Sentry Event' });
      }
  }

  const cameras = ['Front', 'Left Repeater', 'Right Repeater', 'Back', 'Left Pillar', 'Right Pillar'];

  return (
    <div className="flex flex-col h-full bg-black text-white relative group">

      {/* Overlays (3D Toggle & Camera Switcher) */}
      <div className="absolute top-4 right-4 z-20 flex gap-2">
            {/* Camera Switcher (Mobile) */}
            <div className="relative md:hidden">
                <button
                    onClick={() => setIsCameraMenuOpen(!isCameraMenuOpen)}
                    className="p-2 bg-black/50 backdrop-blur border border-white/10 rounded-lg hover:bg-white/10 transition text-white"
                    title="Switch Camera"
                >
                    <Video size={20} />
                </button>
                {isCameraMenuOpen && (
                    <div className="absolute right-0 top-full mt-2 w-48 bg-gray-900 border border-gray-700 rounded-lg shadow-xl overflow-hidden z-30">
                        {cameras.map(cam => (
                            <button
                                key={cam}
                                onClick={() => {
                                    setActiveCamera(cam);
                                    setIsCameraMenuOpen(false);
                                }}
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
                className="p-2 bg-black/50 backdrop-blur border border-white/10 rounded-lg hover:bg-white/10 transition text-white"
                title={is3D ? 'Switch to 2D' : 'Switch to 3D'}
            >
                {is3D ? <Layers size={20} /> : <Box size={20} />}
            </button>
      </div>

      {is3D ? (
          <div className="flex-1 bg-gray-900 overflow-hidden relative flex flex-col">
             <div className="flex-1 relative">
                 {clip && (
                     <Suspense fallback={<div className="flex items-center justify-center h-full text-white">Loading 3D Engine...</div>}>
                         <Scene3D
                            frontSrc={frontVideo ? getUrl(frontVideo.file_path) : ''}
                            leftRepeaterSrc={leftVideo ? getUrl(leftVideo.file_path) : ''}
                            rightRepeaterSrc={rightVideo ? getUrl(rightVideo.file_path) : ''}
                            backSrc={backVideo ? getUrl(backVideo.file_path) : ''}
                            leftPillarSrc={leftPillarVideo ? getUrl(leftPillarVideo.file_path) : ''}
                            rightPillarSrc={rightPillarVideo ? getUrl(rightPillarVideo.file_path) : ''}
                         />
                     </Suspense>
                 )}
             </div>
          </div>
      ) : (
          <div className="flex-1 overflow-hidden grid grid-cols-1 md:grid-cols-3 md:grid-rows-[3fr_1fr_1fr] gap-1 bg-black min-h-0">
              {/* Front Camera (Top, Spans 3 on desktop) */}
              <div className={`relative bg-gray-900 group/cam md:col-span-3 ${activeCamera === 'Front' ? 'block h-full' : 'hidden md:block'}`}>
                  {frontVideo ? (
                      <VideoPlayer
                          key={`${clip.ID}-Front`}
                          src={getUrl(frontVideo.file_path)}
                          className="w-full h-full object-contain"
                          onReady={(p) => handlePlayerReady('Front', p)}
                      />
                  ) : <div className="flex items-center justify-center h-full text-gray-600">No Front Camera</div>}
                  <div className="absolute bottom-4 left-4 bg-black/50 px-3 py-1 rounded-full text-xs font-mono backdrop-blur border border-white/10 pointer-events-none">
                      Front
                  </div>
                   {/* Telemetry Overlay on Front Camera */}
                   {clip.telemetry && clip.telemetry.full_data_json && (
                        <TelemetryOverlay
                            dataJson={clip.telemetry.full_data_json}
                            currentTime={currentTime}
                        />
                   )}
              </div>

               {/* Row 2: Left Repeater, Back, Right Repeater */}

               {/* Left Repeater */}
               <div className={`relative bg-gray-900 group/cam ${activeCamera === 'Left Repeater' ? 'block h-full' : 'hidden md:block'}`}>
                   {leftVideo ? (
                      <VideoPlayer
                          key={`${clip.ID}-LeftRepeater`}
                          src={getUrl(leftVideo.file_path)}
                          className="w-full h-full object-contain"
                          onReady={(p) => handlePlayerReady('Left Repeater', p)}
                      />
                  ) : <div className="flex items-center justify-center h-full text-gray-600">No Left Repeater</div>}
                   <div className="absolute bottom-4 left-4 bg-black/50 px-3 py-1 rounded-full text-xs font-mono backdrop-blur border border-white/10 pointer-events-none">
                      Left Repeater
                  </div>
               </div>

               {/* Back Camera */}
               <div className={`relative bg-gray-900 group/cam ${activeCamera === 'Back' ? 'block h-full' : 'hidden md:block'}`}>
                   {backVideo ? (
                      <VideoPlayer
                          key={`${clip.ID}-Back`}
                          src={getUrl(backVideo.file_path)}
                          className="w-full h-full object-contain"
                          onReady={(p) => handlePlayerReady('Back', p)}
                      />
                  ) : <div className="flex items-center justify-center h-full text-gray-600">No Back Camera</div>}
                   <div className="absolute bottom-4 left-4 bg-black/50 px-3 py-1 rounded-full text-xs font-mono backdrop-blur border border-white/10 pointer-events-none">
                      Back
                  </div>
               </div>

               {/* Right Repeater */}
               <div className={`relative bg-gray-900 group/cam ${activeCamera === 'Right Repeater' ? 'block h-full' : 'hidden md:block'}`}>
                   {rightVideo ? (
                      <VideoPlayer
                          key={`${clip.ID}-RightRepeater`}
                          src={getUrl(rightVideo.file_path)}
                          className="w-full h-full object-contain"
                          onReady={(p) => handlePlayerReady('Right Repeater', p)}
                      />
                  ) : <div className="flex items-center justify-center h-full text-gray-600">No Right Repeater</div>}
                   <div className="absolute bottom-4 left-4 bg-black/50 px-3 py-1 rounded-full text-xs font-mono backdrop-blur border border-white/10 pointer-events-none">
                      Right Repeater
                  </div>
               </div>

               {/* Row 3: Left Pillar, Empty, Right Pillar */}

               {/* Left Pillar */}
               <div className={`relative bg-gray-900 group/cam ${activeCamera === 'Left Pillar' ? 'block h-full' : 'hidden md:block'}`}>
                   {leftPillarVideo ? (
                      <VideoPlayer
                          key={`${clip.ID}-LeftPillar`}
                          src={getUrl(leftPillarVideo.file_path)}
                          className="w-full h-full object-contain"
                          onReady={(p) => handlePlayerReady('Left Pillar', p)}
                      />
                  ) : <div className="flex items-center justify-center h-full text-gray-600">No Left Pillar</div>}
                   <div className="absolute bottom-4 left-4 bg-black/50 px-3 py-1 rounded-full text-xs font-mono backdrop-blur border border-white/10 pointer-events-none">
                      Left Pillar
                  </div>
               </div>

               {/* Empty (Only visible on desktop) */}
               <div className="relative bg-black hidden md:block"></div>

               {/* Right Pillar */}
               <div className={`relative bg-gray-900 group/cam ${activeCamera === 'Right Pillar' ? 'block h-full' : 'hidden md:block'}`}>
                   {rightPillarVideo ? (
                      <VideoPlayer
                          key={`${clip.ID}-RightPillar`}
                          src={getUrl(rightPillarVideo.file_path)}
                          className="w-full h-full object-contain"
                          onReady={(p) => handlePlayerReady('Right Pillar', p)}
                      />
                  ) : <div className="flex items-center justify-center h-full text-gray-600">No Right Pillar</div>}
                   <div className="absolute bottom-4 left-4 bg-black/50 px-3 py-1 rounded-full text-xs font-mono backdrop-blur border border-white/10 pointer-events-none">
                      Right Pillar
                  </div>
               </div>
          </div>
      )}

      {/* Unified Controls Bar */}
       <div className="p-4 bg-black border-t border-gray-900 flex-shrink-0 z-30">
          <Timeline
            currentTime={currentTime}
            duration={duration}
            onSeek={handleSeek}
            markers={markers}
          />
          <div className="flex items-center justify-center gap-4 mt-2">
              <button onClick={togglePlay} className="w-12 h-12 flex items-center justify-center bg-white text-black rounded-full hover:bg-gray-200 transition">
                  {isPlaying ? (
                    <svg xmlns="http://www.w3.org/2000/svg" width="24" height="24" viewBox="0 0 24 24" fill="currentColor" stroke="none"><rect x="6" y="4" width="4" height="16"></rect><rect x="14" y="4" width="4" height="16"></rect></svg>
                  ) : (
                    <svg xmlns="http://www.w3.org/2000/svg" width="24" height="24" viewBox="0 0 24 24" fill="currentColor" stroke="none"><polygon points="5 3 19 12 5 21 5 3"></polygon></svg>
                  )}
              </button>
          </div>
       </div>
    </div>
  );
};

export default Player;
