export type LensProjection = 'rectilinear' | 'equidistant';

export interface RigCamera {
  name: string;
  hfovDeg: number;
  vfovDeg: number;
  yawDeg: number;
  pitchDeg: number;
  rollDeg: number;
  xM: number;
  yM: number;
  zM: number;
  projection: LensProjection;
  role: 'front' | 'pillar' | 'repeater' | 'back';
}

export interface CameraPoseRecord {
  camera: string;
  yaw_deg: number;
  pitch_deg: number;
  roll_deg: number;
  x_m: number;
  y_m: number;
  z_m: number;
}

export const GENERATION_POSES: readonly CameraPoseRecord[];
export const HW3_CAMERAS: readonly RigCamera[];

export function applyCameraPoses(
  records: readonly CameraPoseRecord[],
  base?: readonly RigCamera[],
): readonly RigCamera[];

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
  cameras?: readonly RigCamera[],
): string[];
