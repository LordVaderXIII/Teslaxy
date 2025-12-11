import React, { useEffect, useState } from 'react';
import { Canvas } from '@react-three/fiber';
import { OrbitControls, PerspectiveCamera } from '@react-three/drei';
import * as THREE from 'three';

interface Scene3DProps {
  frontSrc: string;
  leftRepeaterSrc: string;
  rightRepeaterSrc: string;
  backSrc: string;
}

const CurvedScreen = ({ src, radius, height, thetaStart, thetaLength }: any) => {
    const [video] = useState(() => {
        const vid = document.createElement('video');
        vid.crossOrigin = 'Anonymous';
        vid.loop = true;
        vid.muted = true;
        return vid;
    });

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
                <videoTexture attach="map" args={[video]} />
            </meshBasicMaterial>
        </mesh>
    );
}


const Scene3D: React.FC<Scene3DProps> = ({ frontSrc, leftRepeaterSrc, rightRepeaterSrc, backSrc }) => {
  const radius = 8;
  const height = 5;
  const segmentAngle = Math.PI / 2; // 90 degrees per camera

  // Mapping:
  // Back: Centered at 0 (Z+). Start = -Pi/4.
  // Right: Centered at Pi/2 (X+). Start = Pi/4.
  // Front: Centered at Pi (Z-). Start = 3Pi/4.
  // Left: Centered at 3Pi/2 (X-). Start = 5Pi/4.

  return (
    <div className="w-full h-full bg-gray-900">
      <Canvas>
        <PerspectiveCamera makeDefault position={[0, 10, 15]} />
        <OrbitControls enablePan={true} enableZoom={true} enableRotate={true} />

        <ambientLight intensity={0.5} />

        {/* Car Model Placeholder (Just a box for reference) */}
        <mesh position={[0, 0, 0]}>
            <boxGeometry args={[1.8, 1, 4.5]} />
            <meshStandardMaterial color="gray" wireframe />
        </mesh>

        {/* Curved Screens */}

        {/* Back Camera (Rear) */}
        <CurvedScreen
            src={backSrc}
            radius={radius}
            height={height}
            thetaStart={-segmentAngle / 2}
            thetaLength={segmentAngle}
        />

        {/* Right Repeater (Right Side) */}
        <CurvedScreen
            src={rightRepeaterSrc}
            radius={radius}
            height={height}
            thetaStart={Math.PI / 2 - segmentAngle / 2}
            thetaLength={segmentAngle}
        />

        {/* Front Camera (Front) */}
        <CurvedScreen
            src={frontSrc}
            radius={radius}
            height={height}
            thetaStart={Math.PI - segmentAngle / 2}
            thetaLength={segmentAngle}
        />

        {/* Left Repeater (Left Side) */}
        <CurvedScreen
            src={leftRepeaterSrc}
            radius={radius}
            height={height}
            thetaStart={(3 * Math.PI / 2) - segmentAngle / 2}
            thetaLength={segmentAngle}
        />

        <gridHelper args={[30, 30]} />
      </Canvas>
    </div>
  );
};

export default Scene3D;
