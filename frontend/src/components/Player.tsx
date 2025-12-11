import React, { useState, useEffect, useRef } from 'react';
import VideoPlayer from './VideoPlayer';
import TelemetryOverlay from './TelemetryOverlay';
import Scene3D from './Scene3D';

interface Clip {
  id: number;
  video_files: {
    camera: string;
    file_path: string;
  }[];
  telemetry: any;
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

  const getUrl = (path: string) => {
    // Basic path handling - in real app use API
    // path is absolute in DB: /footage/SavedClips/...
    // API endpoint: /api/video/footage/SavedClips/...
    return `/api/video${path}`;
  };

  if (!clip) return <div className="flex items-center justify-center h-full text-gray-500">Select a clip to play</div>;

  const frontVideo = clip.video_files.find(v => v.camera === 'Front');
  const otherVideos = clip.video_files.filter(v => v.camera !== 'Front');

  // Sort: Left Repeater, Right Repeater, Back
  const sortOrder = ['Left Repeater', 'Right Repeater', 'Back'];
  otherVideos.sort((a, b) => sortOrder.indexOf(a.camera) - sortOrder.indexOf(b.camera));

  return (
    <div className="flex flex-col h-full bg-black text-white p-4 gap-4">
      {/* Header / Controls */}
      <div className="flex justify-between items-center px-4 py-2 glass rounded-lg">
         <h2 className="text-xl font-semibold">TeslaCam Player</h2>
         <div className="flex gap-2">
            <button
                onClick={() => setIs3D(!is3D)}
                className="px-3 py-1 bg-blue-500 rounded hover:bg-blue-600 transition"
            >
                {is3D ? '2D View' : '3D View'}
            </button>
         </div>
      </div>

      {is3D ? (
          <div className="flex-1 bg-gray-900 rounded-lg overflow-hidden">
             {clip && (
                 <Scene3D
                    frontSrc={getUrl(clip.video_files.find(v => v.camera === 'Front')?.file_path || '')}
                    leftRepeaterSrc={getUrl(clip.video_files.find(v => v.camera === 'Left Repeater')?.file_path || '')}
                    rightRepeaterSrc={getUrl(clip.video_files.find(v => v.camera === 'Right Repeater')?.file_path || '')}
                    backSrc={getUrl(clip.video_files.find(v => v.camera === 'Back')?.file_path || '')}
                 />
             )}
          </div>
      ) : (
          <div className="flex flex-col gap-4 h-full overflow-hidden">
              {/* Main View (Front) */}
              <div className="flex-grow relative rounded-lg overflow-hidden bg-black shadow-2xl border border-gray-800">
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

                  {/* Camera Name Overlay */}
                  <div className="absolute bottom-4 left-4 bg-black bg-opacity-50 px-2 py-1 rounded text-sm font-mono">
                      Front
                  </div>

                  {/* Master Controls Overlay */}
                   <div className="absolute bottom-0 w-full p-4 bg-gradient-to-t from-black to-transparent opacity-0 hover:opacity-100 transition-opacity">
                      <div className="flex gap-4 items-center">
                          <button onClick={() => mainPlayerRef.current?.paused() ? mainPlayerRef.current.play() : mainPlayerRef.current.pause()}>
                              {isPlaying ? 'Pause' : 'Play'}
                          </button>
                          <input
                              type="range"
                              min="0"
                              max={duration || 100}
                              value={currentTime}
                              onChange={(e) => mainPlayerRef.current.currentTime(parseFloat(e.target.value))}
                              className="flex-grow"
                          />
                          <span>{Math.round(currentTime)} / {Math.round(duration)}</span>
                      </div>
                   </div>
              </div>

              {/* Grid of other cameras */}
              <div className="grid grid-cols-3 gap-4 h-48">
                  {otherVideos.map(v => (
                      <div key={v.camera} className="relative rounded-lg overflow-hidden bg-black border border-gray-800">
                          <VideoPlayer
                              src={getUrl(v.file_path)}
                              className="w-full h-full object-cover"
                              onReady={(p) => handlePlayerReady(v.camera, p)}
                          />
                           <div className="absolute bottom-2 left-2 bg-black bg-opacity-50 px-2 py-1 rounded text-xs font-mono">
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
