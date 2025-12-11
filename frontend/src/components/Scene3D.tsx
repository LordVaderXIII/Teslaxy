import React, { useEffect, useState } from 'react';
import { Canvas } from '@react-three/fiber';
import { OrbitControls, PerspectiveCamera } from '@react-three/drei';
import * as THREE from 'three';

interface Scene3DProps {
  frontSrc: string;
  leftRepeaterSrc: string;
  rightRepeaterSrc: string;
  backSrc: string;
  leftPillarSrc?: string;
  rightPillarSrc?: string;
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


const Scene3D: React.FC<Scene3DProps> = ({
  frontSrc, leftRepeaterSrc, rightRepeaterSrc, backSrc,
  leftPillarSrc, rightPillarSrc
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
        <PerspectiveCamera makeDefault position={[0, 10, 15]} />
        <OrbitControls enablePan={true} enableZoom={true} enableRotate={true} />

        <ambientLight intensity={0.5} />

        {/* Car Model Placeholder (Just a box for reference) */}
        <mesh position={[0, 0, 0]}>
            <boxGeometry args={[1.8, 1, 4.5]} />
            <meshStandardMaterial color="gray" wireframe />
        </mesh>

        {/* Curved Screens */}

        {/* Back Camera (Rear) - Center 0 */}
        <CurvedScreen
            src={backSrc}
            radius={radius}
            height={height}
            thetaStart={-segmentAngle / 2}
            thetaLength={segmentAngle}
        />

        {/* Right Repeater - Center 60 deg */}
        <CurvedScreen
            src={rightRepeaterSrc}
            radius={radius}
            height={height}
            thetaStart={Math.PI / 3 - segmentAngle / 2}
            thetaLength={segmentAngle}
        />

        {/* Right Pillar - Center 120 deg */}
        <CurvedScreen
            src={rightPillarSrc}
            radius={radius}
            height={height}
            thetaStart={2 * Math.PI / 3 - segmentAngle / 2}
            thetaLength={segmentAngle}
        />

        {/* Front Camera - Center 180 deg */}
        <CurvedScreen
            src={frontSrc}
            radius={radius}
            height={height}
            thetaStart={Math.PI - segmentAngle / 2}
            thetaLength={segmentAngle}
        />

        {/* Left Pillar - Center 240 deg */}
        <CurvedScreen
            src={leftPillarSrc}
            radius={radius}
            height={height}
            thetaStart={4 * Math.PI / 3 - segmentAngle / 2}
            thetaLength={segmentAngle}
        />

        {/* Left Repeater - Center 300 deg */}
        <CurvedScreen
            src={leftRepeaterSrc}
            radius={radius}
            height={height}
            thetaStart={5 * Math.PI / 3 - segmentAngle / 2}
            thetaLength={segmentAngle}
        />

        <gridHelper args={[30, 30]} />
      </Canvas>
    </div>
  );
};

export default Scene3D;
