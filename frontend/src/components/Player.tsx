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
          if (p !== player) p.play();
        });
      });

      player.on('pause', () => {
        setIsPlaying(false);
        Object.values(playersRef.current).forEach(p => {
          if (p !== player) p.pause();
        });
      });

      player.on('seeking', () => {
        const time = player.currentTime();
        Object.values(playersRef.current).forEach(p => {
          if (p !== player) p.currentTime(time);
        });
      });
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
  const otherVideos = clip.video_files.filter(v => v.camera !== 'Front');

  // Sort: Left Repeater, Right Repeater, Back
  const sortOrder = ['Left Repeater', 'Right Repeater', 'Back'];
  otherVideos.sort((a, b) => sortOrder.indexOf(a.camera) - sortOrder.indexOf(b.camera));

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
          <div className="flex-1 bg-gray-900 overflow-hidden relative">
             {clip && (
                 <Scene3D
                    frontSrc={getUrl(clip.video_files.find(v => v.camera === 'Front')?.file_path || '')}
                    leftRepeaterSrc={getUrl(clip.video_files.find(v => v.camera === 'Left Repeater')?.file_path || '')}
                    rightRepeaterSrc={getUrl(clip.video_files.find(v => v.camera === 'Right Repeater')?.file_path || '')}
                    backSrc={getUrl(clip.video_files.find(v => v.camera === 'Back')?.file_path || '')}
                 />
             )}
              {/* Timeline Overlay for 3D */}
              <div className="absolute bottom-0 inset-x-0 p-4 bg-gradient-to-t from-black/90 to-transparent">
                  <Timeline
                    currentTime={currentTime}
                    duration={duration}
                    onSeek={handleSeek}
                    markers={markers}
                  />
                  <div className="flex justify-center mt-2">
                       <button onClick={togglePlay} className="px-6 py-1 bg-white/10 hover:bg-white/20 rounded-full text-sm font-medium transition">
                          {isPlaying ? 'Pause' : 'Play'}
                       </button>
                  </div>
              </div>
          </div>
      ) : (
          <div className="flex flex-col h-full overflow-hidden">
              {/* Main View (Front) */}
              <div className="flex-grow relative bg-black overflow-hidden flex flex-col">
                  <div className="flex-grow relative">
                      {frontVideo && (
                          <VideoPlayer
                              src={getUrl(frontVideo.file_path)}
                              className="w-full h-full object-contain"
                              onReady={(p) => handlePlayerReady('Front', p)}
                          />
                      )}
                      {/* Telemetry Overlay */}
                      {clip.telemetry && clip.telemetry.full_data_json && (
                        <TelemetryOverlay
                            dataJson={clip.telemetry.full_data_json}
                            currentTime={currentTime}
                        />
                      )}
                       <div className="absolute top-4 left-1/2 -translate-x-1/2 bg-black/50 px-3 py-1 rounded-full text-xs font-mono backdrop-blur border border-white/10">
                          Front Camera
                      </div>
                  </div>

                  {/* Controls Area */}
                   <div className="p-4 bg-black border-t border-gray-900">
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

              {/* Grid of other cameras (Bottom Strip) */}
              <div className="h-32 grid grid-cols-3 gap-1 bg-black border-t border-gray-900">
                  {otherVideos.map(v => (
                      <div key={v.camera} className="relative bg-gray-900 group/cam cursor-pointer hover:opacity-100 opacity-80 transition-opacity">
                          <VideoPlayer
                              src={getUrl(v.file_path)}
                              className="w-full h-full object-cover"
                              onReady={(p) => handlePlayerReady(v.camera, p)}
                          />
                           <div className="absolute bottom-1 left-1 bg-black/70 px-1.5 py-0.5 rounded text-[10px] font-mono border border-white/10">
                              {v.camera}
                          </div>
                      </div>
                  ))}
              </div>
          </div>
      )}
    </div>
  );
};

export default Player;
