export type NudgeDirection = 'left' | 'right' | 'up' | 'down' | 'zoom-in' | 'zoom-out' | 'reset';

export interface ImageNudge {
  panX: number;
  panY: number;
  zoom: number;
}

export const PAN_STEP: number;
export const ZOOM_STEP: number;
export const ZOOM_MIN: number;
export const ZOOM_MAX: number;
export const PAN_LIMIT: number;

export function identityNudge(): ImageNudge;

export function moveNudge(
  nudge: ImageNudge | null | undefined,
  direction: NudgeDirection,
): ImageNudge;

export function sampleUv(
  u: number,
  v: number,
  nudge: ImageNudge | null | undefined,
): { u: number; v: number };

export function setCameraNudge(
  nudges: Readonly<Record<string, ImageNudge>>,
  camera: string,
  nudge: ImageNudge,
): Record<string, ImageNudge>;
