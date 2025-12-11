import React, { useState, useEffect, useRef } from 'react';
import VideoPlayer from './VideoPlayer';
import TelemetryOverlay from './TelemetryOverlay';
import Scene3D from './Scene3D';
import Timeline from './Timeline';
import { Box, Layers } from 'lucide-react';

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

  const playersRef = useRef<{ [key: string]: any }>({});
  const mainPlayerRef = useRef<any>(null);

  useEffect(() => {
    // Cleanup
    return () => {
      playersRef.current = {};
    };
  }, [clip]);

  // Reset state when clip changes
  useEffect(() => {
      setCurrentTime(0);
      setIsPlaying(false);
  }, [clip?.ID]);

  const handlePlayerReady = (camera: string, player: any) => {
    if (!player) return;
    playersRef.current[camera] = player;

    if (camera === 'Front') {
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
    } else {
        // Mute other players by default to avoid echo
        if (typeof player.muted === 'function') {
             player.muted(true);
        }
    }
  };

  const togglePlay = () => {
      if (mainPlayerRef.current) {
          if (mainPlayerRef.current.paused()) {
              mainPlayerRef.current.play();
          } else {
              mainPlayerRef.current.pause();
          }
      }
  };

  const handleSeek = (time: number) => {
      if (mainPlayerRef.current) {
          mainPlayerRef.current.currentTime(time);
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

  // Determine Incident Marker
  const markers = [];
  if (clip.event === 'Sentry') {
      if (duration > 0) {
          markers.push({ time: duration * 0.8, color: '#ef4444', label: 'Sentry Event' });
      }
  }

  return (
    <div className="flex flex-col h-full bg-black text-white relative group">

      {/* 3D/2D Toggle Overlay */}
      <div className="absolute top-4 right-4 z-20 flex gap-2">
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
                     <Scene3D
                        frontSrc={frontVideo ? getUrl(frontVideo.file_path) : ''}
                        leftRepeaterSrc={leftVideo ? getUrl(leftVideo.file_path) : ''}
                        rightRepeaterSrc={rightVideo ? getUrl(rightVideo.file_path) : ''}
                        backSrc={backVideo ? getUrl(backVideo.file_path) : ''}
                     />
                 )}
             </div>
          </div>
      ) : (
          <div className="flex-1 overflow-hidden grid grid-cols-2 grid-rows-2 gap-1 bg-black">
              {/* Front Camera (Top Left) */}
              <div className="relative bg-gray-900 group/cam">
                  {frontVideo ? (
                      <VideoPlayer
                          src={getUrl(frontVideo.file_path)}
                          className="w-full h-full object-contain"
                          onReady={(p) => handlePlayerReady('Front', p)}
                      />
                  ) : <div className="flex items-center justify-center h-full text-gray-600">No Front Camera</div>}
                  <div className="absolute top-4 left-4 bg-black/50 px-3 py-1 rounded-full text-xs font-mono backdrop-blur border border-white/10 pointer-events-none">
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

               {/* Back Camera (Top Right) */}
               <div className="relative bg-gray-900 group/cam">
                   {backVideo ? (
                      <VideoPlayer
                          src={getUrl(backVideo.file_path)}
                          className="w-full h-full object-contain"
                          onReady={(p) => handlePlayerReady('Back', p)}
                      />
                  ) : <div className="flex items-center justify-center h-full text-gray-600">No Back Camera</div>}
                   <div className="absolute top-4 left-4 bg-black/50 px-3 py-1 rounded-full text-xs font-mono backdrop-blur border border-white/10 pointer-events-none">
                      Back
                  </div>
               </div>

               {/* Left Repeater (Bottom Left) */}
               <div className="relative bg-gray-900 group/cam">
                   {leftVideo ? (
                      <VideoPlayer
                          src={getUrl(leftVideo.file_path)}
                          className="w-full h-full object-contain"
                          onReady={(p) => handlePlayerReady('Left Repeater', p)}
                      />
                  ) : <div className="flex items-center justify-center h-full text-gray-600">No Left Repeater</div>}
                   <div className="absolute top-4 left-4 bg-black/50 px-3 py-1 rounded-full text-xs font-mono backdrop-blur border border-white/10 pointer-events-none">
                      Left Repeater
                  </div>
               </div>

               {/* Right Repeater (Bottom Right) */}
               <div className="relative bg-gray-900 group/cam">
                   {rightVideo ? (
                      <VideoPlayer
                          src={getUrl(rightVideo.file_path)}
                          className="w-full h-full object-contain"
                          onReady={(p) => handlePlayerReady('Right Repeater', p)}
                      />
                  ) : <div className="flex items-center justify-center h-full text-gray-600">No Right Repeater</div>}
                   <div className="absolute top-4 left-4 bg-black/50 px-3 py-1 rounded-full text-xs font-mono backdrop-blur border border-white/10 pointer-events-none">
                      Right Repeater
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
