import React, { useCallback, useEffect, useMemo, useRef, useState } from 'react';
import { Canvas, useThree } from '@react-three/fiber';
import { OrbitControls, PerspectiveCamera } from '@react-three/drei';
import * as THREE from 'three';
import {
  HW3_CAMERAS,
  applyCameraPoses,
  cameraBasis,
  hiddenCameraNames,
  imageDirection,
  type CameraPoseRecord,
  type RigCamera,
} from '../utils/cameraRig.mjs';

const SPHERE_RADIUS = 8;
const GRID_X = 64;
const GRID_Y = 36;

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
  setHeld: (held: boolean) => void;
}

// Video.js-shaped adapter. setHeld pauses a camera the phone viewer cannot
// see without letting Player's play() start it decoding again.
const createPlayerAdapter = (video: HTMLVideoElement): PlayerAdapter => {
  let held = false;
  let wantPlay = false;
  let disposed = false;
  let pendingTime: number | null = null;
  return {
    on: (event, callback) => {
      video.addEventListener(event, callback);
    },
    off: (event, callback) => {
      video.removeEventListener(event, callback);
    },
    currentTime: (time?: number) => {
      if (time !== undefined) {
        if (held) pendingTime = time;
        else video.currentTime = time;
      }
      return video.currentTime;
    },
    duration: () => video.duration,
    play: () => {
      wantPlay = true;
      if (held) return Promise.resolve();
      return video.play();
    },
    pause: () => {
      wantPlay = false;
      video.pause();
    },
    paused: () => video.paused,
    currentSrc: () => video.currentSrc || video.src,
    muted: (mute?: boolean) => {
      if (mute !== undefined) video.muted = mute;
      return video.muted;
    },
    playbackRate: (rate?: number) => {
      try {
        if (rate !== undefined) video.playbackRate = rate;
        return video.playbackRate;
      } catch {
        return 1;
      }
    },
    dispose: () => {
      disposed = true;
      wantPlay = false;
      video.pause();
    },
    src: (source) => {
      video.src = source.src;
    },
    isDisposed: () => disposed,
    setHeld: (next: boolean) => {
      if (next === held) return;
      held = next;
      if (held) {
        video.pause();
        return;
      }
      if (pendingTime != null && Number.isFinite(pendingTime)) {
        try {
          video.currentTime = pendingTime;
        } catch {
          /* The element may not have metadata yet. Player will seek again. */
        }
      }
      pendingTime = null;
      if (wantPlay) video.play().catch(() => {});
    },
  };
};

function makeVideo(): HTMLVideoElement {
  const video = document.createElement('video');
  video.crossOrigin = 'anonymous';
  video.loop = true;
  video.muted = true;
  video.preload = 'metadata';
  video.autoplay = false;
  video.playsInline = true;
  video.setAttribute('playsinline', 'true');
  video.setAttribute('webkit-playsinline', 'true');
  return video;
}

function patchGeometry(camera: RigCamera): THREE.BufferGeometry {
  const positions: number[] = [];
  const uvs: number[] = [];
  const indices: number[] = [];
  for (let y = 0; y <= GRID_Y; y += 1) {
    for (let x = 0; x <= GRID_X; x += 1) {
      const u = x / GRID_X;
      const v = y / GRID_Y;
      const dir = imageDirection(camera, u, v);
      positions.push(dir.x * SPHERE_RADIUS, dir.y * SPHERE_RADIUS, dir.z * SPHERE_RADIUS);
      uvs.push(u, v);
    }
  }
  const stride = GRID_X + 1;
  for (let y = 0; y < GRID_Y; y += 1) {
    for (let x = 0; x < GRID_X; x += 1) {
      const a = y * stride + x;
      const b = a + 1;
      const c = a + stride;
      const d = c + 1;
      indices.push(a, c, b, b, c, d);
    }
  }
  const geometry = new THREE.BufferGeometry();
  geometry.setAttribute('position', new THREE.Float32BufferAttribute(positions, 3));
  geometry.setAttribute('uv', new THREE.Float32BufferAttribute(uvs, 2));
  geometry.setIndex(indices);
  return geometry;
}

const stitchVertex = `
varying vec2 vUv;
varying vec3 vDir;
void main() {
  vUv = uv;
  vDir = position;
  gl_Position = projectionMatrix * modelViewMatrix * vec4(position, 1.0);
}
`;

const stitchFragment = `
uniform sampler2D map;
uniform float camFx[6];
uniform float camFy[6];
uniform float camFz[6];
uniform float camRx[6];
uniform float camRy[6];
uniform float camRz[6];
uniform float camUx[6];
uniform float camUy[6];
uniform float camUz[6];
uniform float camHalfH[6];
uniform float camHalfV[6];
uniform float camFisheye[6];
uniform float camRole[6];
uniform int camCount;
uniform int selfIndex;
varying vec2 vUv;
varying vec3 vDir;

float axisDistance(int i, vec3 dir) {
  vec3 forward = vec3(camFx[i], camFy[i], camFz[i]);
  vec3 right = vec3(camRx[i], camRy[i], camRz[i]);
  vec3 up = vec3(camUx[i], camUy[i], camUz[i]);
  float cx = dot(dir, right);
  float cyv = dot(dir, up);
  float cz = dot(dir, forward);
  if (cz <= 0.000001) return -1.0;
  float u;
  float v;
  if (camFisheye[i] < 0.5) {
    u = 0.5 + 0.5 * ((cx / cz) / tan(camHalfH[i]));
    v = 0.5 + 0.5 * ((cyv / cz) / tan(camHalfV[i]));
  } else {
    float theta = atan(length(vec2(cx, cyv)), cz);
    float radial = max(length(vec2(cx, cyv)), 0.000001);
    u = 0.5 + ((cx / radial) * theta) / (camHalfH[i] * 2.0);
    v = 0.5 + ((cyv / radial) * theta) / (camHalfV[i] * 2.0);
  }
  if (u < 0.0 || u > 1.0 || v < 0.0 || v > 1.0) return -1.0;
  return acos(clamp(cz, -1.0, 1.0));
}

void main() {
  vec3 dir = normalize(vDir);
  float frontSees = 0.0;
  float pillarSees = 0.0;
  for (int i = 0; i < 6; i++) {
    if (i < camCount && axisDistance(i, dir) >= 0.0) {
      // 0 repeater, 1 pillar, 2 front, 3 back.
      if (camRole[i] > 1.5 && camRole[i] < 2.5) frontSees = 1.0;
      if (camRole[i] > 0.5 && camRole[i] < 1.5) pillarSees = 1.0;
    }
  }
  int best = -1;
  float bestDist = 100.0;
  for (int i = 0; i < 6; i++) {
    if (i < camCount) {
      // Front keeps every ray inside its frame, including the garage
      // corners and the yellow line.
      if (frontSees > 0.5 && (camRole[i] < 1.5 || camRole[i] > 2.5)) continue;
      // A pillar keeps the SUV. The repeater draws only outside that fan.
      if (pillarSees > 0.5 && camRole[i] < 0.5) continue;
      float dist = axisDistance(i, dir);
      if (dist >= 0.0 && dist < bestDist) {
        bestDist = dist;
        best = i;
      }
    }
  }
  if (best != selfIndex) discard;
  gl_FragColor = linearToOutputTexel(texture2D(map, vUv));
}
`;

interface OwnerUniform {
  forward: { x: number; y: number; z: number };
  right: { x: number; y: number; z: number };
  up: { x: number; y: number; z: number };
  halfHRad: number;
  halfVRad: number;
  fisheye: number;
  role: number;
}

function StitchMaterial({
  texture,
  owners,
  selfIndex,
}: {
  texture: THREE.VideoTexture;
  owners: OwnerUniform[];
  selfIndex: number;
}) {
  const material = useMemo(() => {
    const fx = new Float32Array(6);
    const fy = new Float32Array(6);
    const fz = new Float32Array(6);
    const rx = new Float32Array(6);
    const ry = new Float32Array(6);
    const rz = new Float32Array(6);
    const ux = new Float32Array(6);
    const uy = new Float32Array(6);
    const uz = new Float32Array(6);
    const halfH = new Float32Array(6);
    const halfV = new Float32Array(6);
    const fisheye = new Float32Array(6);
    const roles = new Float32Array(6);
    owners.forEach((owner, index) => {
      fx[index] = owner.forward.x;
      fy[index] = owner.forward.y;
      fz[index] = owner.forward.z;
      rx[index] = owner.right.x;
      ry[index] = owner.right.y;
      rz[index] = owner.right.z;
      ux[index] = owner.up.x;
      uy[index] = owner.up.y;
      uz[index] = owner.up.z;
      halfH[index] = owner.halfHRad;
      halfV[index] = owner.halfVRad;
      fisheye[index] = owner.fisheye;
      roles[index] = owner.role;
    });
    return new THREE.ShaderMaterial({
      uniforms: {
        map: { value: texture },
        camFx: { value: fx },
        camFy: { value: fy },
        camFz: { value: fz },
        camRx: { value: rx },
        camRy: { value: ry },
        camRz: { value: rz },
        camUx: { value: ux },
        camUy: { value: uy },
        camUz: { value: uz },
        camHalfH: { value: halfH },
        camHalfV: { value: halfV },
        camFisheye: { value: fisheye },
        camRole: { value: roles },
        camCount: { value: owners.length },
        selfIndex: { value: selfIndex },
      },
      vertexShader: stitchVertex,
      fragmentShader: stitchFragment,
      side: THREE.BackSide,
      depthTest: false,
      depthWrite: false,
      toneMapped: false,
    });
  }, [owners, selfIndex, texture]);

  useEffect(() => () => material.dispose(), [material]);

  return <primitive object={material} attach="material" />;
}

function CameraPatch({
  camera,
  video,
  owners,
  selfIndex,
}: {
  camera: RigCamera;
  video: HTMLVideoElement;
  owners: OwnerUniform[];
  selfIndex: number;
}) {
  const invalidate = useThree((state) => state.invalidate);
  const geometry = useMemo(() => patchGeometry(camera), [camera]);
  const texture = useMemo(() => {
    const map = new THREE.VideoTexture(video);
    map.colorSpace = THREE.SRGBColorSpace;
    map.minFilter = THREE.LinearFilter;
    map.magFilter = THREE.LinearFilter;
    map.generateMipmaps = false;
    return map;
  }, [video]);

  useEffect(() => () => {
    geometry.dispose();
    texture.dispose();
  }, [geometry, texture]);

  // Draw when a new video frame is presented, then wait. A paused camera
  // does not present frames, so the demand loop sleeps.
  useEffect(() => {
    let stopped = false;
    let handle = 0;
    const arm = () => {
      if (stopped || typeof video.requestVideoFrameCallback !== 'function') return;
      handle = video.requestVideoFrameCallback(() => {
        if (stopped) return;
        invalidate();
        if (!video.paused) arm();
      });
    };
    const drawOnce = () => invalidate();
    video.addEventListener('play', arm);
    video.addEventListener('loadeddata', drawOnce);
    video.addEventListener('seeked', drawOnce);
    if (!video.paused) arm();
    if (typeof video.requestVideoFrameCallback !== 'function') {
      video.addEventListener('timeupdate', drawOnce);
    }
    return () => {
      stopped = true;
      if (typeof video.cancelVideoFrameCallback === 'function') {
        video.cancelVideoFrameCallback(handle);
      }
      video.removeEventListener('play', arm);
      video.removeEventListener('loadeddata', drawOnce);
      video.removeEventListener('seeked', drawOnce);
      video.removeEventListener('timeupdate', drawOnce);
    };
  }, [invalidate, video]);

  if (selfIndex < 0) return null;

  return (
    <mesh geometry={geometry} frustumCulled={false}>
      <StitchMaterial texture={texture} owners={owners} selfIndex={selfIndex} />
    </mesh>
  );
}

function LookControls({
  economical,
  onHidden,
  cameras,
}: {
  economical: boolean;
  onHidden: (names: string[]) => void;
  cameras: readonly RigCamera[];
}) {
  const camera = useThree((state) => state.camera);
  const controls = useThree((state) => state.controls);
  const gl = useThree((state) => state.gl);
  const invalidate = useThree((state) => state.invalidate);
  const size = useThree((state) => state.size);

  useEffect(() => {
    const apply = () => {
      if (!(camera instanceof THREE.PerspectiveCamera)) return;
      if (!economical) {
        onHidden([]);
      } else {
        const forward = new THREE.Vector3();
        camera.getWorldDirection(forward);
        onHidden(hiddenCameraNames(
          forward.x,
          forward.y,
          forward.z,
          camera.fov,
          camera.aspect || (size.width / Math.max(size.height, 1)),
          cameras,
        ));
      }
      invalidate();
    };

    const onWheel = (event: WheelEvent) => {
      if (!(camera instanceof THREE.PerspectiveCamera)) return;
      const next = camera.fov + event.deltaY * 0.05;
      camera.fov = THREE.MathUtils.clamp(next, 10, 120);
      camera.updateProjectionMatrix();
      apply();
    };

    apply();
    const element = gl.domElement;
    element.addEventListener('wheel', onWheel, { passive: true });
    const orbit = controls as { addEventListener?: (type: string, fn: () => void) => void; removeEventListener?: (type: string, fn: () => void) => void } | null;
    orbit?.addEventListener?.('change', apply);
    return () => {
      element.removeEventListener('wheel', onWheel);
      orbit?.removeEventListener?.('change', apply);
    };
  }, [camera, cameras, controls, economical, gl, invalidate, onHidden, size.height, size.width]);

  return (
    <OrbitControls
      makeDefault
      enablePan={false}
      enableZoom={false}
      enableRotate
      target={[0, 0, 0]}
      rotateSpeed={-0.5}
    />
  );
}

function Feed({
  camera,
  src,
  isPlaying,
  video,
  held,
  onReady,
  onDispose,
}: {
  camera: RigCamera;
  src: string;
  isPlaying?: boolean;
  video: HTMLVideoElement;
  held: boolean;
  onReady: (camera: string, player: PlayerAdapter) => void;
  onDispose: (camera: string, player: PlayerAdapter) => void;
}) {
  const adapterRef = useRef<PlayerAdapter | null>(null);

  useEffect(() => {
    const adapter = createPlayerAdapter(video);
    adapterRef.current = adapter;
    onReady(camera.name, adapter);
    return () => {
      adapter.dispose();
      onDispose(camera.name, adapter);
      adapterRef.current = null;
    };
  }, [camera.name, onDispose, onReady, video]);

  useEffect(() => {
    adapterRef.current?.setHeld(held);
  }, [held]);

  useEffect(() => {
    if (!src) {
      video.pause();
      video.removeAttribute('src');
      video.load();
      return;
    }
    // HTMLVideoElement mutation; not React state.
    // eslint-disable-next-line react-hooks/immutability -- media element API
    video.src = src;
  }, [src, video]);

  useEffect(() => {
    const adapter = adapterRef.current;
    if (!src) {
      video.pause();
      return;
    }
    if (isPlaying) adapter?.play().catch(() => {});
    else adapter?.pause();
  }, [isPlaying, src, video]);

  useEffect(() => () => {
    video.pause();
    video.removeAttribute('src');
    video.load();
  }, [video]);

  return null;
}

interface Scene3DProps {
  frontSrc: string;
  leftRepeaterSrc: string;
  rightRepeaterSrc: string;
  backSrc: string;
  leftPillarSrc?: string;
  rightPillarSrc?: string;
  isPlaying?: boolean;
  economical?: boolean;
  onVideoReady?: (camera: string, player: PlayerAdapter) => void;
  onVideoDispose?: (camera: string, player: PlayerAdapter) => void;
}

const Scene3D: React.FC<Scene3DProps> = ({
  frontSrc,
  leftRepeaterSrc,
  rightRepeaterSrc,
  backSrc,
  leftPillarSrc,
  rightPillarSrc,
  isPlaying,
  economical = false,
  onVideoReady,
  onVideoDispose,
}) => {
  const sources: Record<string, string> = {
    Front: frontSrc,
    'Left Repeater': leftRepeaterSrc,
    'Right Repeater': rightRepeaterSrc,
    Back: backSrc,
    'Left Pillar': leftPillarSrc || '',
    'Right Pillar': rightPillarSrc || '',
  };

  const [rig, setRig] = useState<readonly RigCamera[]>(HW3_CAMERAS);
  useEffect(() => {
    const controller = new AbortController();
    fetch('/api/camera-poses', { signal: controller.signal })
      .then((response) => (response.ok ? response.json() : null))
      .then((body: { cameras?: CameraPoseRecord[] } | null) => {
        if (!body?.cameras?.length) return;
        setRig(applyCameraPoses(body.cameras, HW3_CAMERAS));
      })
      .catch(() => {});
    return () => controller.abort();
  }, []);

  const videos = useMemo(() => {
    const map: Record<string, HTMLVideoElement> = {};
    HW3_CAMERAS.forEach((camera) => {
      map[camera.name] = makeVideo();
    });
    return map;
  }, []);

  const onReadyRef = useRef(onVideoReady);
  useEffect(() => {
    onReadyRef.current = onVideoReady;
  }, [onVideoReady]);
  const onReady = useCallback((name: string, player: PlayerAdapter) => {
    onReadyRef.current?.(name, player);
  }, []);
  const onDisposeRef = useRef(onVideoDispose);
  useEffect(() => {
    onDisposeRef.current = onVideoDispose;
  }, [onVideoDispose]);
  const onDispose = useCallback((name: string, player: PlayerAdapter) => {
    onDisposeRef.current?.(name, player);
  }, []);

  const hiddenKey = useRef('');
  const [hidden, setHidden] = useState<ReadonlySet<string>>(() => new Set());
  const onHidden = useCallback((names: string[]) => {
    const key = names.slice().sort().join('|');
    if (key === hiddenKey.current) return;
    hiddenKey.current = key;
    setHidden(new Set(names));
  }, []);

  const activeCameras = useMemo(() => {
    const byName: Record<string, string> = {
      Front: frontSrc,
      'Left Repeater': leftRepeaterSrc,
      'Right Repeater': rightRepeaterSrc,
      Back: backSrc,
      'Left Pillar': leftPillarSrc || '',
      'Right Pillar': rightPillarSrc || '',
    };
    return rig.filter((camera) => byName[camera.name]);
  }, [backSrc, frontSrc, leftPillarSrc, leftRepeaterSrc, rightPillarSrc, rightRepeaterSrc, rig]);
  const owners = useMemo<OwnerUniform[]>(() => activeCameras.map((camera) => {
    const basis = cameraBasis(camera);
    return {
      forward: basis.forward,
      right: basis.right,
      up: basis.up,
      halfHRad: (camera.hfovDeg * Math.PI) / 180 / 2,
      halfVRad: (camera.vfovDeg * Math.PI) / 180 / 2,
      fisheye: camera.projection === 'equidistant' ? 1 : 0,
      role: camera.role === 'front' ? 2 : camera.role === 'pillar' ? 1 : camera.role === 'back' ? 3 : 0,
    };
  }), [activeCameras]);

  return (
    <div className="w-full h-full bg-[var(--bg)]">
      {rig.map((camera) => (
        <Feed
          key={camera.name}
          camera={camera}
          src={sources[camera.name]}
          isPlaying={isPlaying}
          video={videos[camera.name]}
          held={economical && hidden.has(camera.name)}
          onReady={onReady}
          onDispose={onDispose}
        />
      ))}
      <Canvas
        frameloop="demand"
        dpr={economical ? 1 : [1, 2]}
        gl={{
          antialias: !economical,
          powerPreference: economical ? 'low-power' : 'default',
          alpha: false,
        }}
      >
        <PerspectiveCamera makeDefault position={[0, 0, 0.01]} fov={34} near={0.05} far={50} />
        <LookControls economical={economical} onHidden={onHidden} cameras={rig} />
        {activeCameras.map((camera) => (
          <CameraPatch
            key={camera.name}
            camera={camera}
            video={videos[camera.name]}
            owners={owners}
            selfIndex={activeCameras.findIndex((item) => item.name === camera.name)}
          />
        ))}
      </Canvas>
    </div>
  );
};

export default React.memo(Scene3D);
