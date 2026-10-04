export type LensProjection = 'rectilinear' | 'equidistant';

export interface RigCamera {
  name: string;
  hfovDeg: number;
  vfovDeg: number;
  yawDeg: number;
  pitchDeg: number;
  projection: LensProjection;
  role: 'front' | 'pillar' | 'repeater' | 'back';
}

export const SIDE_OVERLAP_DEG: number;
export const HW3_CAMERAS: readonly RigCamera[];

export function angleDelta(a: number, b: number): number;

export function cameraBasis(camera: RigCamera): {
  forward: { x: number; y: number; z: number };
  right: { x: number; y: number; z: number };
  up: { x: number; y: number; z: number };
};

export function imageDirection(
  camera: RigCamera,
  u: number,
  v: number,
): { x: number; y: number; z: number };

export function projectToImage(
  camera: RigCamera,
  x: number,
  y: number,
  z: number,
): { u: number; v: number } | null;

export function azimuthDegOf(x: number, y: number, z: number): number;

export function directionOwner(x: number, y: number, z: number): string | null;

export function horizontalHalfRad(fovDeg: number, aspect: number): number;

export function hiddenCameraNames(
  forwardX: number,
  forwardY: number,
  forwardZ: number,
  fovDeg: number,
  aspect: number,
): string[];
