import React, { useState, useEffect, useRef, Suspense, useCallback, useMemo } from 'react';
import VideoPlayer, { type VideoJsPlayer } from './VideoPlayer';
import TelemetryOverlay from './TelemetryOverlay';
import Timeline from './Timeline';
import { Box, Layers, Settings } from 'lucide-react';
import { useClickOutside } from '../hooks/useClickOutside';

const Scene3D = React.lazy(() => import('./Scene3D'));

interface VideoFile {
  camera: string;
  file_path: string;
  timestamp: string;
}

interface Clip {
  ID: number;
  video_files?: VideoFile[];
  telemetry?: {
    full_data_json?: string;
    latitude?: number;
    longitude?: number;
  };
  event: string;
  timestamp: string;
  event_timestamp?: string;
  city?: string;
  reason?: string;
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

const formatClock = (seconds: number) => {
  if (!Number.isFinite(seconds) || seconds < 0) return '00:00';
  const mins = Math.floor(seconds / 60);
  const secs = Math.floor(seconds % 60);
  return `${String(mins).padStart(2, '0')}:${String(secs).padStart(2, '0')}`;
};

// Bolt: Extracted CameraView to a separate component to fix Hooks violation.
// This allows `useCallback` to be used correctly at the top level.
const CameraView = React.memo(({
    camName,
    className,
    seg,
    clip,
    quality,
    handlePlayerReady,
    getUrl,
    onClick,
    feature,
    preload
}: {
    camName: string,
    className: string,
    seg: CameraSegment | null,
    clip: Clip,
    quality: string,
    handlePlayerReady: (cam: string, p: VideoJsPlayer) => void,
    getUrl: (path: string) => string,
    onClick: () => void,
    feature?: boolean,
    preload: 'none' | 'metadata' | 'auto'
}) => {
    // Bolt: Use useCallback to create a STABLE handler for onReady.
    // This combined with React.memo(VideoPlayer) prevents re-renders.
    const onReady = useCallback((p: VideoJsPlayer) => {
        handlePlayerReady(camName, p);
    }, [camName, handlePlayerReady]);

    return (
        <button
            type="button"
            onClick={onClick}
            aria-label={`Focus ${camName} camera`}
            className={`desk-camera ${feature ? 'is-feature' : ''} ${className}`}
        >
            {seg ? (
                <VideoPlayer
                    key={`${clip.ID}-${camName}-${seg.file_path}-${quality}`} // Key forces remount on segment change OR quality change
                    src={getUrl(seg.file_path)}
                    className="w-full h-full object-contain pointer-events-none"
                    onReady={onReady}
                    preload={preload}
                />
            ) : (
                <div className="desk-camera-missing">No {camName}</div>
            )}
             <span className="absolute bottom-3 left-3 font-[var(--font-display)] font-medium text-[var(--ink)] pointer-events-none">
                {camName}
            </span>
        </button>
    );
});

const DEFAULT_CAMERAS = ['Front', 'Left Pillar', 'Back', 'Right Pillar', 'Left Repeater', 'Right Repeater'];
const MOSAIC_SECONDARY = ['Left Repeater', 'Right Repeater', 'Left Pillar', 'Right Pillar', 'Back'];

const Player: React.FC<{ clip: Clip | null; onOpenMap?: () => void }> = ({ clip, onOpenMap }) => {
  const [isPlaying, setIsPlaying] = useState(false);
  const [currentTime, setCurrentTime] = useState(0);
  const [is3D, setIs3D] = useState(false);
  const [activeCamera, setActiveCamera] = useState<string>('Front');
  const [viewMode, setViewMode] = useState<'six' | 'focus'>('six');
  const [playbackSpeed, setPlaybackSpeed] = useState(1);
  const chipRefs = useRef<Record<string, HTMLButtonElement | null>>({});
  const [isPhone, setIsPhone] = useState(
    typeof window !== 'undefined' ? window.matchMedia('(max-width: 767px)').matches : false
  );

  useEffect(() => {
    const mq = window.matchMedia('(max-width: 767px)');
    const onChange = () => setIsPhone(mq.matches);
    onChange();
    mq.addEventListener('change', onChange);
    return () => mq.removeEventListener('change', onChange);
  }, []);

  // Transcoding State
  const [quality, setQuality] = useState<string>('original');
  const [encoderStatus, setEncoderStatus] = useState<{encoder: string, hw_accel: boolean} | null>(null);
  const [isQualityMenuOpen, setIsQualityMenuOpen] = useState(false);

  const qualityMenuRef = useRef<HTMLDivElement>(null);
  useClickOutside(qualityMenuRef, () => setIsQualityMenuOpen(false));

  // Bolt: Ref to track current time without triggering re-renders in callbacks
  const currentTimeRef = useRef(0);
  useEffect(() => {
    currentTimeRef.current = currentTime;
  }, [currentTime]);

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


  const playersRef = useRef<{ [key: string]: VideoJsPlayer }>({});
  const mainPlayerRef = useRef<VideoJsPlayer | null>(null);
  // Track registered listeners so we can clean them up
  const listenerCleanups = useRef<Array<() => void>>([]);

  useEffect(() => {
      return () => {
        listenerCleanups.current.forEach(cleanup => cleanup());
        listenerCleanups.current = [];
        playersRef.current = {};
        mainPlayerRef.current = null;
      };
  }, []);

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

  useEffect(() => {
    Object.values(playersRef.current).forEach((p: VideoJsPlayer) => {
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
      Object.values(playersRef.current).forEach((p: VideoJsPlayer) => {
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

  const handlePlayerReady = useCallback((camera: string, player: VideoJsPlayer) => {
    if (!player) return;
    playersRef.current[camera] = player;

    if (typeof player.playbackRate === 'function') {
        // Bolt: Use ref for current playback speed? Or just current state?
        // State is fine here as it's not changing frequently.
        // Actually, playbackSpeed is in deps, so this recreates when speed changes.
        // That's acceptable.
    }

    // Bolt: Perform INITIAL SEEK here instead of creating a transient closure in render.
    // This allows onReady to be stable.
    const normCam = normalizeCameraName(camera);
    const camSegments = segments[normCam];
    if (camSegments) {
         let src = player.currentSrc();
         try { src = decodeURIComponent(src); } catch { src = player.currentSrc(); }
         src = src.split('?')[0];

         // Find which segment this player loaded
         const seg = camSegments.find(s => src.endsWith(s.file_path));
         if (seg) {
             const globalTime = currentTimeRef.current;
             const localTime = globalTime - seg.startTime;
             // Only seek if needed (initial load)
             if (Math.abs(player.currentTime() - localTime) > 0.5) {
                 player.currentTime(localTime);
             }
             if (isPlaying) player.play().catch(() => {});
         }
    }

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
             try { src = decodeURIComponent(src); } catch { src = player.currentSrc(); }

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

      const onTimeUpdate = () => {
        const camSegments = segments[normCam];
        if (camSegments) {
            let src = player.currentSrc();
            try { src = decodeURIComponent(src); } catch { src = player.currentSrc(); }
            // Remove query params
            src = src.split('?')[0];

            const seg = camSegments.find(s => src.endsWith(s.file_path));
            if (seg) {
                const global = seg.startTime + player.currentTime();
                if (Math.abs(global - currentTimeRef.current) > 0.1) {
                     setCurrentTime(global);
                }
            }

            // Check for end of segment manually (fallback for 'ended' event)
            if (player.duration() > 0 && player.currentTime() >= player.duration() - 0.2) {
                if (!player.paused()) {
                    checkAdvance();
                }
            }
        }
      };

      const onEnded = () => { checkAdvance(); };

      player.on('timeupdate', onTimeUpdate);
      player.on('ended', onEnded);
      listenerCleanups.current.push(
        () => { player.off('timeupdate', onTimeUpdate); },
        () => { player.off('ended', onEnded); }
      );

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
    const onPlay = () => { setIsPlaying(true); };
    const onPause = () => { setIsPlaying(false); };
    player.on('play', onPlay);
    player.on('pause', onPause);
    listenerCleanups.current.push(
      () => { player.off('play', onPlay); },
      () => { player.off('pause', onPause); }
    );

  }, [segments, isPlaying]); // Removed currentTime from deps to avoid re-binding

  const cyclePlaybackSpeed = useCallback(() => {
    const speeds = [0.5, 1, 1.5, 2, 4];
    const nextIndex = (speeds.indexOf(playbackSpeed) + 1) % speeds.length;
    setPlaybackSpeed(speeds[nextIndex]);
  }, [playbackSpeed]);

  const togglePlay = useCallback(() => {
      const player = mainPlayerRef.current || Object.values(playersRef.current)[0];
      if (player) {
          if (player.paused()) player.play().catch(() => {});
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
                   try { src = decodeURIComponent(src); } catch { src = p.currentSrc(); }
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
  useEffect(() => {
    const handleKeyDown = (e: KeyboardEvent) => {
      // Ignore if user is typing in an input
      if (document.activeElement instanceof HTMLInputElement ||
          document.activeElement instanceof HTMLTextAreaElement) {
        return;
      }

      switch(e.key) {
        case ' ':
        case 'k':
        case 'K':
          e.preventDefault();
          togglePlay();
          break;
        case 'ArrowLeft':
           if (!e.defaultPrevented) handleSeek(currentTimeRef.current - 5);
           break;
        case 'ArrowRight':
           if (!e.defaultPrevented) handleSeek(currentTimeRef.current + 5);
           break;
        case 'j':
        case 'J':
           e.preventDefault();
           handleSeek(currentTimeRef.current - 15);
           break;
        case 'l':
        case 'L':
           e.preventDefault();
           handleSeek(currentTimeRef.current + 15);
           break;
      }
    };

    window.addEventListener('keydown', handleKeyDown);
    return () => window.removeEventListener('keydown', handleKeyDown);
  }, [togglePlay, handleSeek]);

  const getUrl = useCallback((path: string) => {
    let url = `/api/video${path}`;
    if (quality !== 'original') {
        url += `?quality=${quality}`;
    }
    return url;
  }, [quality]);

  // Helper to get current segment for a camera
  const getCurrentSegment = (cameraName: string) => {
      const info = getSegmentAtTime(cameraName, currentTime);
      return info ? info.segment : null;
  };

  const secondaryCameras = useMemo(() => {
      return MOSAIC_SECONDARY.map(name => name === activeCamera ? 'Front' : name);
  }, [activeCamera]);

  const focusCamera = useCallback((camName: string) => {
      setActiveCamera(camName);
      setViewMode('focus');
  }, []);

  useEffect(() => {
      const el = chipRefs.current[activeCamera];
      if (!el) return;
      const reduce = window.matchMedia('(prefers-reduced-motion: reduce)').matches;
      const id = window.requestAnimationFrame(() => {
        el.scrollIntoView({ inline: 'center', block: 'nearest', behavior: reduce ? 'auto' : 'smooth' });
      });
      return () => window.cancelAnimationFrame(id);
  }, [activeCamera]);

  if (!clip) return (
    <div className="desk-state">
      <div className="desk-state-inner">
        <span className="desk-eyebrow">// VIEWER</span>
        <h2 className="desk-title" style={{ fontSize: 30, margin: '10px 0' }}>Select a clip to play</h2>
      </div>
    </div>
  );

  // Determine Incident Marker — amber tick from a real event_timestamp only.
  const markers = [];
  if (clip.event_timestamp) {
      const eventTime = new Date(clip.event_timestamp).getTime() / 1000;
      const clipStart = new Date(clip.timestamp).getTime() / 1000;
      const offset = eventTime - clipStart;
      if (offset >= 0 && offset <= totalDuration) {
          markers.push({ time: offset, color: '#ffb020', label: 'Incident' });
      }
  }

  const qualities = ['original', '1080p', '720p', '480p'];
  const start = new Date(clip.timestamp);
  const title = clip.city?.trim() ? clip.city : 'Unknown location';
  const subtitle = [
      start.toLocaleDateString(),
      start.toLocaleTimeString([], { hour: '2-digit', minute: '2-digit' }),
      clip.reason || clip.event,
  ].join(' · ');
  const focused = viewMode === 'focus';
  const incident = markers[0];
  const showMosaic = !is3D && !focused && !isPhone;
  const telemetryJson = clip.telemetry?.full_data_json;
  const hudLayer = telemetryJson ? (
    <div
      className="pointer-events-none absolute inset-0 z-50"
      style={{ gridColumn: '1 / -1', gridRow: '1 / -1' }}
    >
      <TelemetryOverlay
        dataJson={telemetryJson}
        currentTime={currentTime}
        duration={totalDuration}
      />
    </div>
  ) : null;

  return (
    <div className="flex flex-col h-full min-h-0 bg-[var(--bg)] text-[var(--ink-2)] relative">
      <div className="flex items-end justify-between gap-3 px-6 pt-5 pb-3 flex-none max-md:px-4 max-md:pt-4 max-md:items-start">
        <div className="min-w-0">
          <div className="desk-eyebrow">// {clip.event.toUpperCase()}</div>
          <h2 className="desk-title text-[clamp(24px,2.5vw,35px)] leading-[1.1] my-1.5">{title}</h2>
          <div className="text-[16px] text-[var(--muted)]">{subtitle}</div>
        </div>
        <div className="flex gap-2 items-center flex-none">
          <div className="mode-tabs flex gap-1 max-md:hidden" role="group" aria-label="View mode">
            <button
              type="button"
              className="desk-tabbtn"
              aria-selected={!focused}
              onClick={() => { setViewMode('six'); setIs3D(false); }}
            >
              SIX CAMERAS
            </button>
            <button
              type="button"
              className="desk-tabbtn"
              aria-selected={focused}
              onClick={() => { setViewMode('focus'); setIs3D(false); }}
            >
              FOCUS
            </button>
          </div>
          <button type="button" className="desk-lightbtn" onClick={onOpenMap} aria-label="Open map">
            MAP
          </button>
          <button
            type="button"
            onClick={() => setIs3D(!is3D)}
            aria-label={is3D ? "Switch to 2D view" : "Switch to 3D view"}
            title={is3D ? "Switch to 2D view" : "Switch to 3D view"}
            className="desk-iconbtn"
            aria-pressed={is3D}
          >
            {is3D ? <Layers size={20} /> : <Box size={20} />}
          </button>
        </div>
      </div>

      <div className="flex-1 min-h-0 px-6 pb-3 flex flex-col max-md:px-4">
      {is3D ? (
          <div className="desk-stage is-3d relative">
             <Suspense fallback={<div className="desk-state"><span className="desk-eyebrow">// INDEXING FOOTAGE</span></div>}>
                 <Scene3D
                    frontSrc={getCurrentSegment('Front')?.file_path ? getUrl(getCurrentSegment('Front')!.file_path) : ''}
                    leftRepeaterSrc={getCurrentSegment('Left Repeater')?.file_path ? getUrl(getCurrentSegment('Left Repeater')!.file_path) : ''}
                    rightRepeaterSrc={getCurrentSegment('Right Repeater')?.file_path ? getUrl(getCurrentSegment('Right Repeater')!.file_path) : ''}
                    backSrc={getCurrentSegment('Back')?.file_path ? getUrl(getCurrentSegment('Back')!.file_path) : ''}
                    leftPillarSrc={getCurrentSegment('Left Pillar')?.file_path ? getUrl(getCurrentSegment('Left Pillar')!.file_path) : ''}
                    rightPillarSrc={getCurrentSegment('Right Pillar')?.file_path ? getUrl(getCurrentSegment('Right Pillar')!.file_path) : ''}
                    isPlaying={isPlaying}
                    onVideoReady={handlePlayerReady}
                 />
             </Suspense>
             {hudLayer}
          </div>
      ) : (
          <div className={`desk-stage relative ${focused || isPhone ? 'is-focus' : ''}`}>
              <CameraView
                  camName={activeCamera}
                  className="h-full"
                  feature
                  seg={getCurrentSegment(activeCamera)}
                  clip={clip}
                  quality={quality}
                  handlePlayerReady={handlePlayerReady}
                  getUrl={getUrl}
                  onClick={() => focusCamera(activeCamera)}
                  preload="metadata"
              />
              {showMosaic && (
                <div className="desk-camera-grid">
                  {secondaryCameras.map((camName) => (
                      <CameraView
                          key={camName}
                          camName={camName}
                          className="h-full"
                          seg={getCurrentSegment(camName)}
                          clip={clip}
                          quality={quality}
                          handlePlayerReady={handlePlayerReady}
                          getUrl={getUrl}
                          onClick={() => focusCamera(camName)}
                          preload="none"
                      />
                  ))}
                </div>
              )}
              {hudLayer}
          </div>
      )}

      <div className="desk-chips" aria-label="Choose camera; scroll horizontally for more">
        {DEFAULT_CAMERAS.map(cam => (
          <button
            key={cam}
            type="button"
            ref={(el) => { chipRefs.current[cam] = el; }}
            className="desk-tabbtn"
            aria-selected={activeCamera === cam}
            aria-label={`Show ${cam} camera`}
            onClick={() => focusCamera(cam)}
          >
            {cam}
          </button>
        ))}
      </div>
      </div>

      <section className="flex-none bg-[var(--panel)] border-t border-[var(--line)] px-6 pt-3 pb-4 max-md:px-4" aria-label="Playback controls">
          {incident && (
            <div className="flex justify-between items-center text-[16px] tracking-[0.05em] text-[var(--muted)] mb-1">
              <span style={{ color: 'var(--accent)' }}>◆ INCIDENT MARKER {formatClock(incident.time)}</span>
            </div>
          )}
          <Timeline
            currentTime={currentTime}
            duration={totalDuration}
            onSeek={handleSeek}
            markers={markers}
          />
          <div className="flex items-center justify-between gap-3 mt-2 flex-wrap">
              <div className="flex items-center gap-2">
                <button
                    type="button"
                    onClick={() => handleSeek(Math.max(0, currentTime - 15))}
                    aria-label="Rewind 15 seconds"
                    title="Rewind 15 seconds (J)"
                    className="desk-iconbtn"
                >
                    −15
                </button>
                <button
                    type="button"
                    onClick={togglePlay}
                    aria-label={isPlaying ? "Pause" : "Play"}
                    title={isPlaying ? "Pause (Space)" : "Play (Space)"}
                    className="desk-play"
                >
                    {isPlaying ? 'Ⅱ' : '▶'}
                </button>
                <button
                    type="button"
                    onClick={() => handleSeek(Math.min(totalDuration, currentTime + 15))}
                    aria-label="Skip forward 15 seconds"
                    title="Skip forward 15 seconds (L)"
                    className="desk-iconbtn"
                >
                    +15
                </button>
                <button
                    type="button"
                    onClick={cyclePlaybackSpeed}
                    aria-label={`Playback speed: ${playbackSpeed}x`}
                    title="Change playback speed"
                    className="desk-lightbtn"
                >
                    {playbackSpeed}×
                </button>
              </div>

              <span className="font-medium text-[var(--ink)] tabular-nums whitespace-nowrap">
                <b>{formatClock(currentTime)}</b>
                <span className="text-[var(--muted)]"> / {formatClock(totalDuration)}</span>
              </span>

              <div className="relative" ref={qualityMenuRef}>
                  <button
                      type="button"
                      onClick={() => setIsQualityMenuOpen(!isQualityMenuOpen)}
                      aria-label={`Quality: ${quality}`}
                      title={`Quality: ${quality}`}
                      aria-expanded={isQualityMenuOpen}
                      aria-haspopup="true"
                      className="desk-lightbtn"
                  >
                      <Settings size={18} />
                      {quality.toUpperCase()}
                  </button>
                  {isQualityMenuOpen && (
                      <div className="desk-popover desk-popover-up min-w-[140px] flex flex-col">
                           {encoderStatus && (
                               <div className="pb-2 mb-2 border-b border-dashed border-[var(--line)] text-[var(--muted)] uppercase tracking-wider">
                                   Encoder: {encoderStatus.encoder}
                               </div>
                           )}
                           {qualities.map(q => (
                               <button
                                   key={q}
                                   type="button"
                                   onClick={() => { setQuality(q); setIsQualityMenuOpen(false); }}
                                   className={`w-full text-left min-h-11 px-2 ${quality === q ? 'text-[var(--accent)]' : 'text-[var(--ink-2)]'}`}
                               >
                                   {q.toUpperCase()}
                               </button>
                           ))}
                      </div>
                  )}
              </div>
          </div>
       </section>
    </div>
  );
};

export default Player;
