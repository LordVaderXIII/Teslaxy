export const SCHEMA_VERSION: number;

export const CAMERA_IDS: readonly string[];
export const CAMERA_DRAW_ORDER: readonly string[];
export const CAMERA_LABELS: Readonly<Record<string, string>>;
export const CAMERA_SHORT_LABELS: Readonly<Record<string, string>>;
export const TEXTURE_REPEAT: readonly number[];
export const TEXTURE_OFFSET: readonly number[];

export const POS_STEP: number;
export const ANGLE_STEP: number;
export const FOV_STEP: number;

export interface AlignmentCamera {
  camera: string;
  x: number;
  y: number;
  z: number;
  yaw_deg: number;
  pitch_deg: number;
  roll_deg: number;
  fov_deg: number;
}

export interface AlignmentViewer {
  radius: number;
  height: number;
  radial_segments: number;
  height_segments: number;
  position: number[];
  target: number[];
}

export interface CameraAlignment {
  schema_version: number;
  viewer: AlignmentViewer;
  cameras: AlignmentCamera[];
}

export type AlignmentParse =
  | { ok: true; alignment: CameraAlignment }
  | { ok: false; message: string };

export function parseAlignment(
  input: unknown,
  options?: { fallbackViewer?: AlignmentViewer },
): AlignmentParse;

export function serializeAlignment(alignment: CameraAlignment): unknown;
export function exportAlignment(alignment: CameraAlignment): string;
export function cloneAlignment(alignment: CameraAlignment): CameraAlignment;
export function alignmentsEqual(a: CameraAlignment, b: CameraAlignment): boolean;
export function cameraById(alignment: CameraAlignment, cameraId: string): AlignmentCamera | undefined;

export function nudgeCamera(
  alignment: CameraAlignment,
  cameraId: string,
  field: 'x' | 'y' | 'z' | 'yaw_deg' | 'pitch_deg' | 'roll_deg' | 'fov_deg',
  sign: number,
): CameraAlignment;

export function resetCamera(
  alignment: CameraAlignment,
  baseline: CameraAlignment,
  cameraId: string,
): CameraAlignment;

export interface CylinderSpec {
  radiusTop: number;
  radiusBottom: number;
  height: number;
  radialSegments: number;
  heightSegments: number;
  openEnded: boolean;
  thetaStart: number;
  thetaLength: number;
  args: [number, number, number, number, number, boolean, number, number];
}

export function cylinderSpec(alignment: CameraAlignment, camera: AlignmentCamera): CylinderSpec;

export interface SegmentPlacement {
  identity: boolean;
  position: number[];
  quaternion: number[];
  meshPosition: number[];
}

export function segmentPlacement(alignment: CameraAlignment, camera: AlignmentCamera): SegmentPlacement;
export function applyPlacement(vertex: number[], placement: SegmentPlacement): number[];
export function viewerCamera(alignment: CameraAlignment): { position: number[]; target: number[] };
