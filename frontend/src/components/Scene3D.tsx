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

const VideoMesh = ({ src, position, rotation, size }: any) => {
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

    return (
        <mesh position={position} rotation={rotation}>
            <planeGeometry args={size} />
            <meshBasicMaterial side={THREE.DoubleSide} toneMapped={false}>
                <videoTexture attach="map" args={[video]} />
            </meshBasicMaterial>
        </mesh>
    );
}


const Scene3D: React.FC<Scene3DProps> = ({ frontSrc, leftRepeaterSrc, rightRepeaterSrc, backSrc }) => {
  return (
    <div className="w-full h-full bg-gray-900">
      <Canvas>
        <PerspectiveCamera makeDefault position={[0, 5, 10]} />
        <OrbitControls enablePan={true} enableZoom={true} enableRotate={true} />

        <ambientLight intensity={0.5} />

        {/* Car Model Placeholder (Just a box for reference) */}
        <mesh position={[0, 0, 0]}>
            <boxGeometry args={[1.8, 1, 4.5]} />
            <meshStandardMaterial color="gray" wireframe />
        </mesh>

        {/*
            Mapping based on Tesla camera positions roughly:
            We place them "around" the car facing OUTWARDS.
        */}

        {/* Front - Large */}
        {frontSrc && <VideoMesh src={frontSrc} position={[0, 1.5, -4]} rotation={[0, 0, 0]} size={[8, 4.5]} />}

        {/* Left Repeater - Side facing back/left */}
        {leftRepeaterSrc && <VideoMesh src={leftRepeaterSrc} position={[-4, 1.5, 0]} rotation={[0, Math.PI / 2, 0]} size={[6, 4]} />}

        {/* Right Repeater */}
        {rightRepeaterSrc && <VideoMesh src={rightRepeaterSrc} position={[4, 1.5, 0]} rotation={[0, -Math.PI / 2, 0]} size={[6, 4]} />}

        {/* Back */}
        {backSrc && <VideoMesh src={backSrc} position={[0, 1.5, 4]} rotation={[0, Math.PI, 0]} size={[6, 4]} />}

        <gridHelper args={[20, 20]} />
      </Canvas>
    </div>
  );
};

export default Scene3D;
