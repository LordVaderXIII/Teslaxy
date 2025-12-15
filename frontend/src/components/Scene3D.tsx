import React, { useEffect, useMemo } from 'react';
import { Canvas, useThree } from '@react-three/fiber';
import { OrbitControls, PerspectiveCamera } from '@react-three/drei';
import * as THREE from 'three';

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
    duration: () => number;
    play: () => Promise<void>;
    pause: () => void;
    paused: () => boolean;
    muted: (mute?: boolean) => boolean;
    dispose: () => void;
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
    muted: (mute?: boolean) => {
       if (mute !== undefined) video.muted = mute;
       return video.muted;
    },
    dispose: () => {
       // No-op for raw video element, managed by React lifecycle
    }
  };
};

interface Scene3DProps {
  frontSrc: string;
  leftRepeaterSrc: string;
  rightRepeaterSrc: string;
  backSrc: string;
  leftPillarSrc?: string;
  rightPillarSrc?: string;
  onVideoReady?: (camera: string, player: PlayerAdapter) => void;
}

interface CurvedScreenProps {
    src?: string;
    radius: number;
    height: number;
    thetaStart: number;
    thetaLength: number;
    onReady?: (player: PlayerAdapter) => void;
}

const CurvedScreen = ({ src, radius, height, thetaStart, thetaLength, onReady }: CurvedScreenProps) => {
    // Use useMemo to create a stable video element that doesn't trigger state setters
    const video = useMemo(() => {
        const vid = document.createElement('video');
        vid.crossOrigin = 'Anonymous';
        vid.loop = true;
        vid.muted = true;
        // iOS requires playsinline
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
            video.src = src;
            video.play().catch(e => console.warn("Auto-play prevented", e));
        }
    }, [src, video]);

    useEffect(() => {
        return () => {
            video.pause();
            video.src = "";
            video.load();
        }
    }, [video]);

    if (!src) return null;

    return (
        <mesh>
            {/* CylinderGeometry: radiusTop, radiusBottom, height, radialSegments, heightSegments, openEnded, thetaStart, thetaLength */}
            <cylinderGeometry args={[radius, radius, height, 32, 1, true, thetaStart, thetaLength]} />
            {/* side={THREE.DoubleSide} ensures it's visible from inside and outside */}
            <meshBasicMaterial side={THREE.DoubleSide} toneMapped={false}>
                <videoTexture attach="map" args={[video]} repeat={[-1, 1]} offset={[1, 0]} />
            </meshBasicMaterial>
        </mesh>
    );
}


const Scene3D: React.FC<Scene3DProps> = ({
  frontSrc, leftRepeaterSrc, rightRepeaterSrc, backSrc,
  leftPillarSrc, rightPillarSrc, onVideoReady
}) => {
  const radius = 8;
  const height = 5;
  const segmentAngle = Math.PI / 3; // 60 degrees

  // Layout (Counter-Clockwise from +Z=0):
  // Back: 0. Range [-30, 30] -> Start -Pi/6
  // Right Rep: 60 (Pi/3). Range [30, 90] -> Start Pi/6
  // Right Pillar: 120 (2Pi/3). Range [90, 150] -> Start Pi/2
  // Front: 180 (Pi). Range [150, 210] -> Start 5Pi/6
  // Left Pillar: 240 (4Pi/3). Range [210, 270] -> Start 7Pi/6
  // Left Rep: 300 (5Pi/3). Range [270, 330] -> Start 9Pi/6 (3Pi/2)

  return (
    <div className="w-full h-full bg-gray-900">
      <Canvas>
        <ZoomHandler />
        {/* Camera inside the "car" */}
        <PerspectiveCamera makeDefault position={[0, 1.2, 0.1]} />
        {/* Controls to look around (rotateSpeed negative for "drag to look") */}
        <OrbitControls
            enablePan={false}
            enableZoom={false}
            enableRotate={true}
            target={[0, 1.2, 0]}
            rotateSpeed={-0.5}
        />

        <ambientLight intensity={0.5} />

        {/* Curved Screens */}

        {/* Back Camera (Rear) - Center 0 */}
        <CurvedScreen
            src={backSrc}
            radius={radius}
            height={height}
            thetaStart={-segmentAngle / 2}
            thetaLength={segmentAngle}
            onReady={(p) => onVideoReady && onVideoReady('Back', p)}
        />

        {/* Right Repeater - Center 60 deg */}
        <CurvedScreen
            src={rightRepeaterSrc}
            radius={radius}
            height={height}
            thetaStart={Math.PI / 3 - segmentAngle / 2}
            thetaLength={segmentAngle}
            onReady={(p) => onVideoReady && onVideoReady('Right Repeater', p)}
        />

        {/* Right Pillar - Center 120 deg */}
        <CurvedScreen
            src={rightPillarSrc}
            radius={radius}
            height={height}
            thetaStart={2 * Math.PI / 3 - segmentAngle / 2}
            thetaLength={segmentAngle}
            onReady={(p) => onVideoReady && onVideoReady('Right Pillar', p)}
        />

        {/* Front Camera - Center 180 deg */}
        <CurvedScreen
            src={frontSrc}
            radius={radius}
            height={height}
            thetaStart={Math.PI - segmentAngle / 2}
            thetaLength={segmentAngle}
            onReady={(p) => onVideoReady && onVideoReady('Front', p)}
        />

        {/* Left Pillar - Center 240 deg */}
        <CurvedScreen
            src={leftPillarSrc}
            radius={radius}
            height={height}
            thetaStart={4 * Math.PI / 3 - segmentAngle / 2}
            thetaLength={segmentAngle}
            onReady={(p) => onVideoReady && onVideoReady('Left Pillar', p)}
        />

        {/* Left Repeater - Center 300 deg */}
        <CurvedScreen
            src={leftRepeaterSrc}
            radius={radius}
            height={height}
            thetaStart={5 * Math.PI / 3 - segmentAngle / 2}
            thetaLength={segmentAngle}
            onReady={(p) => onVideoReady && onVideoReady('Left Repeater', p)}
        />
      </Canvas>
    </div>
  );
};

// Bolt Optimization: Prevent re-renders of the 3D scene on every parent update (e.g. timeupdate).
// Since props are stable during segment playback, this saves significant GPU/CPU overhead.
export default React.memo(Scene3D);
