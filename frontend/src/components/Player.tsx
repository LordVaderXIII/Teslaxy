import React, { useState, useEffect, useRef, Suspense, useCallback, useMemo } from 'react';
import VideoPlayer, { type VideoJsPlayer } from './VideoPlayer';
import TelemetryOverlay from './TelemetryOverlay';
import Timeline from './Timeline';
import { Box, Layers, Settings } from 'lucide-react';
import { useClickOutside } from '../hooks/useClickOutside';
import {
  applyPlayerTransport,
  assignPlaybackRate,
  buildCameraSegments,
  findSegmentAtTime,
  isControllablePlayer,
  localMediaTime,
  mediaSeekBlocked,
  normalizeCameraName,
  resolveMediaClock,
  shouldCorrectDrift,
  timelineDurationSeconds,
} from '../utils/playbackTimeline.mjs';

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

const formatClock = (seconds: number) => {
  if (!Number.isFinite(seconds) || seconds < 0) return '00:00';
  const mins = Math.floor(seconds / 60);
  const secs = Math.floor(seconds % 60);
  return `${String(mins).padStart(2, '0')}:${String(secs).padStart(2, '0')}`;
};

function playerSrcPath(player: VideoJsPlayer): string {
  let src = '';
  try {
    src = player.currentSrc() || '';
  } catch {
    return '';
  }
  try {
    src = decodeURIComponent(src);
  } catch {
    /* video.js may already have decoded the URL */
  }
  return src.split('?')[0];
}

// Bolt: Extracted CameraView to a separate component to fix Hooks violation.
// This allows `useCallback` to be used correctly at the top level.
const CameraView = React.memo(({
    camName,
    className,
    seg,
    clip,
    quality,
    handlePlayerReady,
    releasePlayer,
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
    releasePlayer: (cam: string, p: VideoJsPlayer) => void,
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

    const onDispose = useCallback((p: VideoJsPlayer) => {
        releasePlayer(camName, p);
    }, [camName, releasePlayer]);

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
                    onDispose={onDispose}
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
  // Phone 3D asks for the lowest quality the menu already offers, then
  // puts the previous choice back when the 3D view closes.
  const qualityBeforePhone3D = useRef<string | null>(null);
  const qualityRef = useRef(quality);
  qualityRef.current = quality;

  useEffect(() => {
    if (isPhone && is3D) {
      if (qualityBeforePhone3D.current == null) {
        qualityBeforePhone3D.current = qualityRef.current;
        if (qualityRef.current !== '480p') setQuality('480p');
      }
      return;
    }
    if (qualityBeforePhone3D.current != null) {
      const restore = qualityBeforePhone3D.current;
      qualityBeforePhone3D.current = null;
      if (qualityRef.current === '480p') setQuality(restore);
    }
  }, [isPhone, is3D]);
  const [encoderStatus, setEncoderStatus] = useState<{encoder: string, hw_accel: boolean} | null>(null);
  const [isQualityMenuOpen, setIsQualityMenuOpen] = useState(false);

  const qualityMenuRef = useRef<HTMLDivElement>(null);
  useClickOutside(qualityMenuRef, () => setIsQualityMenuOpen(false));

  // Bolt: Ref to track current time without triggering re-renders in callbacks
  const currentTimeRef = useRef(0);
  // Holds a requested global time until the mounted element reports that offset.
  // Publishing segmentStart+0 before the seek sticks is what drops 02:10 onto 02:01.
  const pendingSeekRef = useRef<number | null>(null);
  const seekApplyDepth = useRef(0);
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

  // Probed media durations replace the 60s estimate once a file's metadata loads.
  // Short Saved clips are otherwise drawn as a full minute.
  const [mediaDurations, setMediaDurations] = useState<Record<string, number>>({});
  const mediaDurationsRef = useRef(mediaDurations);
  useEffect(() => {
    mediaDurationsRef.current = mediaDurations;
  }, [mediaDurations]);

  const segments = useMemo(() => {
      return buildCameraSegments(clip?.video_files, mediaDurations) as Record<string, CameraSegment[]>;
  }, [clip, mediaDurations]);

  const totalDuration = useMemo(() => timelineDurationSeconds(segments), [segments]);


  const playersRef = useRef<{ [key: string]: VideoJsPlayer }>({});
  const mainPlayerRef = useRef<VideoJsPlayer | null>(null);
  // Track registered listeners so we can clean them up
  const listenerCleanups = useRef<Array<() => void>>([]);
  const segmentsRef = useRef(segments);
  const playbackSpeedRef = useRef(playbackSpeed);
  const activeCameraRef = useRef(activeCamera);
  const isPlayingRef = useRef(isPlaying);
  const lastDriftSeekAt = useRef<Record<string, number>>({});
  useEffect(() => {
    segmentsRef.current = segments;
    playbackSpeedRef.current = playbackSpeed;
    activeCameraRef.current = activeCamera;
    isPlayingRef.current = isPlaying;
  }, [segments, playbackSpeed, activeCamera, isPlaying]);

  useEffect(() => {
      return () => {
        listenerCleanups.current.forEach(cleanup => cleanup());
        listenerCleanups.current = [];
        playersRef.current = {};
        mainPlayerRef.current = null;
      };
  }, []);

  const getSegmentAtTime = useCallback((camera: string, time: number) => {
      return findSegmentAtTime(segments[normalizeCameraName(camera)], time);
  }, [segments]);

  const releasePlayer = useCallback((camera: string, player: VideoJsPlayer) => {
      if (playersRef.current[camera] === player) {
          delete playersRef.current[camera];
      }
      if (mainPlayerRef.current === player) {
          mainPlayerRef.current = null;
      }
  }, []);

  const dropIfDead = useCallback((camera: string, player: VideoJsPlayer | null | undefined) => {
      if (player && isControllablePlayer(player)) return player;
      if (player && playersRef.current[camera] === player) {
          delete playersRef.current[camera];
      }
      if (player && mainPlayerRef.current === player) {
          mainPlayerRef.current = null;
      }
      return null;
  }, []);

  const rememberMediaDuration = useCallback((filePath: string, player: VideoJsPlayer) => {
      if (!filePath || !isControllablePlayer(player)) return;
      let dur = Number.NaN;
      try {
          dur = player.duration();
      } catch {
          return;
      }
      if (!Number.isFinite(dur) || dur <= 0 || dur === Number.POSITIVE_INFINITY) return;
      const existing = mediaDurationsRef.current[filePath];
      if (existing !== undefined && Math.abs(existing - dur) < 0.05) return;
      const next = { ...mediaDurationsRef.current, [filePath]: dur };
      mediaDurationsRef.current = next;
      setMediaDurations(next);
  }, []);

  const segmentForPlayer = useCallback((camera: string, player: VideoJsPlayer) => {
      const camSegments = segmentsRef.current[normalizeCameraName(camera)];
      if (!camSegments) return null;
      const src = playerSrcPath(player);
      if (!src) return null;
      return camSegments.find(s => src.endsWith(s.file_path)) ?? null;
  }, []);

  // Keep every mounted camera on the feature camera's clock. A tile that
  // becomes ready after play used to stay a constant offset behind.
  const syncPeers = useCallback((masterCamera: string, globalTime: number) => {
      const rate = playbackSpeedRef.current;
      for (const [cam, player] of Object.entries(playersRef.current)) {
          if (cam === masterCamera) continue;
          if (!dropIfDead(cam, player)) continue;
          let peerRate = Number.NaN;
          try {
              peerRate = player.playbackRate();
          } catch {
              releasePlayer(cam, player);
              continue;
          }
          if (!Number.isFinite(peerRate) || Math.abs(peerRate - rate) > 0.01) {
              if (!assignPlaybackRate(player, rate)) {
                  releasePlayer(cam, player);
                  continue;
              }
          }
          const seg = segmentForPlayer(cam, player);
          if (!seg) continue;
          const local = localMediaTime(seg.startTime, seg.duration, globalTime);
          if (local == null) continue;
          let now = Number.NaN;
          try {
              now = player.currentTime();
          } catch {
              continue;
          }
          if (!shouldCorrectDrift(now - local)) continue;
          if (mediaSeekBlocked(player)) continue;
          const at = performance.now();
          if (at - (lastDriftSeekAt.current[cam] || 0) < 400) continue;
          lastDriftSeekAt.current[cam] = at;
          try {
              player.currentTime(local);
          } catch {
              releasePlayer(cam, player);
          }
      }
  }, [dropIfDead, releasePlayer, segmentForPlayer]);

  useEffect(() => {
    const rate = playbackSpeed;
    for (const [cam, player] of Object.entries(playersRef.current)) {
      if (!dropIfDead(cam, player)) continue;
      if (!assignPlaybackRate(player, rate)) {
        releasePlayer(cam, player);
      }
    }
  }, [playbackSpeed, dropIfDead, releasePlayer]);

  // Sync play/pause state across all players
  useEffect(() => {
      for (const [cam, player] of Object.entries(playersRef.current)) {
          if (!dropIfDead(cam, player)) continue;
          try {
              if (isPlaying) {
                  if (player.paused()) player.play().catch(() => {});
              } else if (!player.paused()) {
                  player.pause();
              }
          } catch {
              dropIfDead(cam, player);
          }
      }
  }, [isPlaying, dropIfDead]);

  const handlePlayerReady = useCallback((camera: string, player: VideoJsPlayer) => {
    if (!isControllablePlayer(player)) return;
    playersRef.current[camera] = player;
    if (camera === activeCameraRef.current || !mainPlayerRef.current || !isControllablePlayer(mainPlayerRef.current)) {
      mainPlayerRef.current = player;
    }

    const align = () => {
      if (!isControllablePlayer(player)) return;
      if (seekApplyDepth.current > 0) return;
      const seg = segmentForPlayer(camera, player);
      const pending = pendingSeekRef.current;
      const clock = pending != null ? pending : currentTimeRef.current;
      // Null means this element is still showing a different minute. Do not
      // seek it past its own duration; the remounted element applies the offset.
      const localTime = seg ? localMediaTime(seg.startTime, seg.duration, clock) : null;
      seekApplyDepth.current += 1;
      let applied = false;
      try {
        applied = applyPlayerTransport(player, {
          playbackRate: playbackSpeedRef.current,
          localTime: localTime == null ? undefined : localTime,
          play: isPlayingRef.current,
        });
      } finally {
        seekApplyDepth.current -= 1;
      }
      if (!applied) {
        releasePlayer(camera, player);
        return;
      }
      if (seg) rememberMediaDuration(seg.file_path, player);
    };
    align();

    const normCam = normalizeCameraName(camera);
    if (normCam !== 'front' && typeof player.muted === 'function') {
      try {
        player.muted(true);
      } catch {
        /* element already gone */
      }
    }

    const checkAdvance = () => {
      const camSegments = segmentsRef.current[normCam];
      const seg = segmentForPlayer(camera, player);
      if (!camSegments || !seg) {
        setIsPlaying(false);
        return;
      }
      const idx = camSegments.findIndex(s => s.file_path === seg.file_path && s.timestamp === seg.timestamp);
      if (idx !== -1 && idx < camSegments.length - 1) {
        const nextTime = camSegments[idx + 1].startTime;
        currentTimeRef.current = nextTime;
        setCurrentTime(nextTime);
      } else {
        setIsPlaying(false);
      }
    };

    const onTimeUpdate = () => {
      if (!isControllablePlayer(player)) return;
      const seg = segmentForPlayer(camera, player);
      if (!seg) return;
      let local = Number.NaN;
      try {
        local = player.currentTime();
      } catch {
        return;
      }
      let seeking = false;
      try {
        seeking = typeof player.seeking === 'function' && Boolean(player.seeking());
      } catch {
        seeking = false;
      }
      const decision = resolveMediaClock({
        segmentStart: seg.startTime,
        segmentDuration: seg.duration,
        mediaTime: local,
        pendingGlobal: pendingSeekRef.current,
        seeking,
      });
      if (decision.retryLocal != null && seekApplyDepth.current === 0) {
        seekApplyDepth.current += 1;
        try {
          const applied = applyPlayerTransport(player, {
            playbackRate: playbackSpeedRef.current,
            localTime: decision.retryLocal,
          });
          if (!applied) releasePlayer(camera, player);
        } finally {
          seekApplyDepth.current -= 1;
        }
      }

      // Only the feature camera publishes the clock, and only after a pending
      // cross-segment seek has actually landed. Peers still retry above.
      if (camera !== activeCameraRef.current) return;
      if (decision.clearPending) pendingSeekRef.current = null;
      if (decision.publishGlobal == null) return;
      if (Math.abs(decision.publishGlobal - currentTimeRef.current) > 0.1) {
        currentTimeRef.current = decision.publishGlobal;
        setCurrentTime(decision.publishGlobal);
      }
      syncPeers(camera, decision.publishGlobal);
      rememberMediaDuration(seg.file_path, player);
      if (pendingSeekRef.current != null) return;
      let duration = 0;
      try {
        duration = player.duration();
      } catch {
        duration = 0;
      }
      if (duration > 0 && local >= duration - 0.2 && !player.paused()) {
        checkAdvance();
      }
    };

    const onEnded = () => {
      if (pendingSeekRef.current != null) return;
      if (camera === activeCameraRef.current) checkAdvance();
    };
    const onMediaReady = () => align();

    player.on('timeupdate', onTimeUpdate);
    player.on('seeked', onTimeUpdate);
    player.on('ended', onEnded);
    player.on('loadedmetadata', onMediaReady);
    player.on('loadeddata', onMediaReady);
    player.on('canplay', onMediaReady);
    const off = (event: string, fn: () => void) => {
      try {
        player.off(event, fn);
      } catch {
        /* disposed with the camera tile */
      }
    };
    listenerCleanups.current.push(
      () => off('timeupdate', onTimeUpdate),
      () => off('seeked', onTimeUpdate),
      () => off('ended', onEnded),
      () => off('loadedmetadata', onMediaReady),
      () => off('loadeddata', onMediaReady),
      () => off('canplay', onMediaReady),
    );

    const onPlay = () => {
      if (camera === activeCameraRef.current) setIsPlaying(true);
    };
    const onPause = () => {
      if (camera === activeCameraRef.current) setIsPlaying(false);
    };
    player.on('play', onPlay);
    player.on('pause', onPause);
    listenerCleanups.current.push(
      () => off('play', onPlay),
      () => off('pause', onPause),
    );
  }, [releasePlayer, rememberMediaDuration, segmentForPlayer, syncPeers]);

  const cyclePlaybackSpeed = useCallback(() => {
    const speeds = [0.5, 1, 1.5, 2, 4];
    const nextIndex = (speeds.indexOf(playbackSpeed) + 1) % speeds.length;
    setPlaybackSpeed(speeds[nextIndex]);
  }, [playbackSpeed]);

  const togglePlay = useCallback(() => {
      const preferred = playersRef.current[activeCameraRef.current];
      const player = (preferred && isControllablePlayer(preferred))
        ? preferred
        : Object.values(playersRef.current).find(p => isControllablePlayer(p));
      if (!player) return;
      try {
          if (player.paused()) player.play().catch(() => {});
          else player.pause();
      } catch {
          /* disposed between focus and the click */
      }
  }, []);

  const handleSeek = useCallback((time: number) => {
      const newTime = Math.max(0, Math.min(time, totalDuration));
      pendingSeekRef.current = newTime;
      currentTimeRef.current = newTime;
      setCurrentTime(newTime);

      // Players already showing the target file can seek now. A minute change
      // remounts the element; pendingSeekRef is applied once that file can play.
      Object.keys(segments).forEach(cam => {
          const info = getSegmentAtTime(cam, newTime);
          if (!info) return;
          const displayName = cam === 'front' ? 'Front' :
                     cam === 'leftrepeater' ? 'Left Repeater' :
                     cam === 'rightrepeater' ? 'Right Repeater' :
                     cam === 'back' ? 'Back' :
                     cam === 'leftpillar' ? 'Left Pillar' :
                     cam === 'rightpillar' ? 'Right Pillar' : cam;
          const p = playersRef.current[displayName];
          if (!isControllablePlayer(p)) return;
          try {
              const src = playerSrcPath(p);
              const localTime = localMediaTime(info.segment.startTime, info.segment.duration, newTime);
              const onTarget = Boolean(src && src.endsWith(info.segment.file_path) && localTime != null);
              if (onTarget) {
                  if (!applyPlayerTransport(p, {
                      playbackRate: playbackSpeedRef.current,
                      localTime: localTime as number,
                  })) {
                      releasePlayer(displayName, p);
                  }
              } else if (!assignPlaybackRate(p, playbackSpeedRef.current)) {
                  releasePlayer(displayName, p);
              }
          } catch {
              releasePlayer(displayName, p);
          }
      });
  }, [totalDuration, segments, getSegmentAtTime, releasePlayer]);

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
      activeCameraRef.current = camName;
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
                    economical={isPhone}
                    onVideoReady={handlePlayerReady}
                    onVideoDispose={releasePlayer}
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
                  releasePlayer={releasePlayer}
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
                          releasePlayer={releasePlayer}
                          getUrl={getUrl}
                          onClick={() => focusCamera(camName)}
                          preload="metadata"
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
