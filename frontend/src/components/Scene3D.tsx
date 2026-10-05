import React, { useEffect, useMemo, useRef, useState } from 'react';
import { Canvas, useThree } from '@react-three/fiber';
import { OrbitControls, PerspectiveCamera } from '@react-three/drei';
import * as THREE from 'three';
import AlignControls from './AlignControls';
import { committedAlignment, committedAlignmentError } from '../data/loadAlignment';
import {
  CAMERA_DRAW_ORDER,
  CAMERA_LABELS,
  TEXTURE_OFFSET,
  TEXTURE_REPEAT,
  alignmentsEqual,
  cameraById,
  cloneAlignment,
  cylinderSpec,
  exportAlignment,
  nudgeCamera,
  parseAlignment,
  resetCamera,
  segmentPlacement,
  viewerCamera,
  type CameraAlignment,
  type CylinderSpec,
  type SegmentPlacement,
} from '../utils/cameraAlignment.mjs';

const ZoomHandler = () => {
  const { camera, gl } = useThree();

  useEffect(() => {
    const handleWheel = (e: WheelEvent) => {
      if (camera instanceof THREE.PerspectiveCamera) {
        const zoomSpeed = 0.05;
        const newFov = camera.fov + e.deltaY * zoomSpeed;
        camera.fov = THREE.MathUtils.clamp(newFov, 10, 120);
        camera.updateProjectionMatrix();
      }
    };

    const element = gl.domElement;
    element.addEventListener('wheel', handleWheel, { passive: true });
    return () => element.removeEventListener('wheel', handleWheel);
  }, [camera, gl]);

  return null;
};

interface PlayerAdapter {
  on: (event: string, callback: () => void) => void;
  off: (event: string, callback: () => void) => void;
  currentTime: (time?: number) => number;
  currentSrc: () => string;
  duration: () => number;
  play: () => Promise<void>;
  pause: () => void;
  paused: () => boolean;
  muted: (mute?: boolean) => boolean;
  playbackRate: (rate?: number) => number;
  dispose: () => void;
  src: (source: { src: string; type: string }) => void;
  isDisposed: () => boolean;
}

// Adapter to make HTMLVideoElement compatible with the interface expected by Player.tsx (video.js-like)
const createPlayerAdapter = (video: HTMLVideoElement): PlayerAdapter => {
  return {
    on: (event: string, callback: () => void) => {
      video.addEventListener(event, callback);
    },
    off: (event: string, callback: () => void) => {
      video.removeEventListener(event, callback);
    },
    currentTime: (time?: number) => {
      if (time !== undefined) {
        video.currentTime = time;
      }
      return video.currentTime;
    },
    duration: () => video.duration,
    play: () => video.play(),
    pause: () => video.pause(),
    paused: () => video.paused,
    currentSrc: () => video.currentSrc || video.src,
    muted: (mute?: boolean) => {
      if (mute !== undefined) video.muted = mute;
      return video.muted;
    },
    playbackRate: (rate?: number) => {
      if (!video) return 1;
      try {
        if (rate !== undefined) video.playbackRate = rate;
        return video.playbackRate;
      } catch {
        // A detached element surfaces as a null tech. Leave the caller alive.
        return 1;
      }
    },
    dispose: () => {
      // No-op for raw video element, managed by React lifecycle
    },
    src: (source: { src: string; type: string }) => {
      video.src = source.src;
    },
    isDisposed: () => false,
  };
};

interface Scene3DProps {
  frontSrc: string;
  leftRepeaterSrc: string;
  rightRepeaterSrc: string;
  backSrc: string;
  leftPillarSrc?: string;
  rightPillarSrc?: string;
  isPlaying?: boolean;
  onVideoReady?: (camera: string, player: PlayerAdapter) => void;
}

interface CurvedScreenProps {
  src?: string;
  spec: CylinderSpec;
  placement: SegmentPlacement;
  isPlaying?: boolean;
  onReady?: (player: PlayerAdapter) => void;
}

const CurvedScreen = ({ src, spec, placement, isPlaying, onReady }: CurvedScreenProps) => {
  const video = useMemo(() => {
    const vid = document.createElement('video');
    vid.crossOrigin = 'Anonymous';
    vid.loop = true;
    vid.muted = true;
    vid.preload = 'metadata';
    vid.autoplay = false;
    vid.setAttribute('playsinline', 'true');
    vid.setAttribute('webkit-playsinline', 'true');
    return vid;
  }, []);

  useEffect(() => {
    if (onReady && video) {
      const adapter = createPlayerAdapter(video);
      onReady(adapter);
    }
  }, [video, onReady]);

  useEffect(() => {
    if (src) {
      // HTMLVideoElement mutation; not React state.
      // eslint-disable-next-line react-hooks/immutability -- media element API
      video.src = src;
    }
  }, [src, video]);

  useEffect(() => {
    if (!src) {
      video.pause();
      return;
    }
    if (isPlaying) {
      video.play().catch(() => {});
    } else {
      video.pause();
    }
  }, [isPlaying, src, video]);

  useEffect(() => {
    return () => {
      video.pause();
      video.src = '';
      video.load();
    };
  }, [video]);

  if (!src) return null;

  const meshPosition: [number, number, number] = placement.identity
    ? [0, 0, 0]
    : [placement.meshPosition[0], placement.meshPosition[1], placement.meshPosition[2]];

  const mesh = (
    <mesh position={meshPosition}>
      <cylinderGeometry args={spec.args} />
      <meshBasicMaterial side={THREE.DoubleSide} toneMapped={false}>
        <videoTexture
          attach="map"
          args={[video]}
          repeat={[TEXTURE_REPEAT[0], TEXTURE_REPEAT[1]]}
          offset={[TEXTURE_OFFSET[0], TEXTURE_OFFSET[1]]}
        />
      </meshBasicMaterial>
    </mesh>
  );

  // Zero pitch, roll, and position is the production mesh: no extra group.
  if (placement.identity) return mesh;

  const quaternion = new THREE.Quaternion(
    placement.quaternion[0],
    placement.quaternion[1],
    placement.quaternion[2],
    placement.quaternion[3],
  );
  const groupPosition: [number, number, number] = [
    placement.position[0],
    placement.position[1],
    placement.position[2],
  ];
  return (
    <group position={groupPosition} quaternion={quaternion}>
      {mesh}
    </group>
  );
};

const READ_ERROR = 'This alignment file could not be read. It needs schema version 1 and all six cameras.';

const Scene3D: React.FC<Scene3DProps> = ({
  frontSrc, leftRepeaterSrc, rightRepeaterSrc, backSrc,
  leftPillarSrc, rightPillarSrc, isPlaying, onVideoReady,
}) => {
  const [alignOpen, setAlignOpen] = useState(false);
  const [selected, setSelected] = useState('front');
  const [notice, setNotice] = useState('');
  const [baseline, setBaseline] = useState<CameraAlignment | null>(() => (
    committedAlignment ? cloneAlignment(committedAlignment) : null
  ));
  const [alignment, setAlignment] = useState<CameraAlignment | null>(() => (
    committedAlignment ? cloneAlignment(committedAlignment) : null
  ));

  const onReadyRef = useRef(onVideoReady);
  useEffect(() => {
    onReadyRef.current = onVideoReady;
  }, [onVideoReady]);

  const readyHandlers = useMemo(() => {
    const handlers: Record<string, (player: PlayerAdapter) => void> = {};
    for (const id of CAMERA_DRAW_ORDER) {
      const label = CAMERA_LABELS[id];
      handlers[id] = (player) => onReadyRef.current?.(label, player);
    }
    return handlers;
  }, []);

  const sources: Record<string, string> = {
    front: frontSrc,
    back: backSrc,
    left_pillar: leftPillarSrc || '',
    right_pillar: rightPillarSrc || '',
    left_repeater: leftRepeaterSrc,
    right_repeater: rightRepeaterSrc,
  };

  const onExport = () => {
    if (!alignment) return;
    const blob = new Blob([exportAlignment(alignment)], { type: 'application/json' });
    const url = URL.createObjectURL(blob);
    const link = document.createElement('a');
    link.href = url;
    link.download = 'camera-alignment.json';
    document.body.appendChild(link);
    link.click();
    link.remove();
    URL.revokeObjectURL(url);
    setNotice('Exported camera-alignment.json.');
  };

  const onImport = (file: File) => {
    const reader = new FileReader();
    reader.onload = () => {
      let data: unknown;
      try {
        data = JSON.parse(String(reader.result ?? ''));
      } catch {
        setNotice(READ_ERROR);
        return;
      }
      const result = parseAlignment(data, {
        fallbackViewer: alignment?.viewer,
      });
      if (!result.ok) {
        setNotice(result.message);
        return;
      }
      const next = cloneAlignment(result.alignment);
      setAlignment(next);
      setBaseline(next);
      setNotice('Imported alignment.');
    };
    reader.onerror = () => setNotice(READ_ERROR);
    reader.readAsText(file);
  };

  if (!alignment || !baseline) {
    return (
      <div className="w-full h-full bg-[var(--bg)] p-4" role="alert">
        {committedAlignmentError || READ_ERROR}
      </div>
    );
  }

  const view = viewerCamera(alignment);
  const usingDefaults = committedAlignment != null && alignmentsEqual(alignment, committedAlignment);
  const cameraPosition: [number, number, number] = [
    view.position[0], view.position[1], view.position[2],
  ];
  const cameraTarget: [number, number, number] = [
    view.target[0], view.target[1], view.target[2],
  ];

  return (
    <div
      className={`align-host relative w-full h-full bg-[var(--bg)]${alignOpen ? ' is-open' : ''}`}
      data-alignment-source={usingDefaults ? 'defaults' : 'adjusted'}
    >
      <div className="align-stage">
        <Canvas style={{ width: '100%', height: '100%' }}>
        <ZoomHandler />
        <PerspectiveCamera makeDefault position={cameraPosition} />
        <OrbitControls
          enablePan={false}
          enableZoom={false}
          enableRotate={true}
          target={cameraTarget}
          rotateSpeed={-0.5}
        />
        <ambientLight intensity={0.5} />
        {CAMERA_DRAW_ORDER.map((id) => {
          const camera = cameraById(alignment, id);
          if (!camera) return null;
          return (
            <CurvedScreen
              key={id}
              src={sources[id]}
              spec={cylinderSpec(alignment, camera)}
              placement={segmentPlacement(alignment, camera)}
              isPlaying={isPlaying}
              onReady={readyHandlers[id]}
            />
          );
        })}
        </Canvas>
      </div>
      {alignOpen ? (
        <AlignControls
          alignment={alignment}
          selected={selected}
          usingDefaults={usingDefaults}
          notice={notice}
          onSelect={setSelected}
          onNudge={(field, sign) => {
            setNotice('');
            setAlignment((current) => (current ? nudgeCamera(current, selected, field, sign) : current));
          }}
          onReset={() => {
            setNotice('');
            setAlignment((current) => (current ? resetCamera(current, baseline, selected) : current));
          }}
          onExport={onExport}
          onImport={onImport}
          onClose={() => setAlignOpen(false)}
        />
      ) : (
        <button
          type="button"
          className="align-open"
          onClick={() => setAlignOpen(true)}
        >
          Align
        </button>
      )}
    </div>
  );
};

export default React.memo(Scene3D);
