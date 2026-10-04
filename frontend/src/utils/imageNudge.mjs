/**
 * Hand alignment for one stitched camera image.
 *
 * This shifts and scales the texture coordinates of that camera's patch.
 * It does not change yaw, pitch, roll, or mount position, and it does not
 * move any other camera. Zoom 1 and pan 0 reproduce the current projection.
 *
 * sampleUv must stay in step with the stitch fragment shader.
 */

export const PAN_STEP = 0.02;
export const ZOOM_STEP = 1.1;
export const ZOOM_MIN = 0.25;
export const ZOOM_MAX = 4;
export const PAN_LIMIT = 1;

/** @typedef {'left' | 'right' | 'up' | 'down' | 'zoom-in' | 'zoom-out' | 'reset'} NudgeDirection */

/**
 * @typedef {object} ImageNudge
 * @property {number} panX Positive moves this image to the right.
 * @property {number} panY Positive moves this image up.
 * @property {number} zoom Above 1 enlarges this image inside its patch.
 */

/** @returns {ImageNudge} */
export function identityNudge() {
  return { panX: 0, panY: 0, zoom: 1 };
}

function clamp(value, min, max) {
  return Math.min(max, Math.max(min, value));
}

/**
 * @param {ImageNudge | null | undefined} nudge
 * @param {NudgeDirection} direction
 * @returns {ImageNudge}
 */
export function moveNudge(nudge, direction) {
  const current = {
    panX: nudge?.panX || 0,
    panY: nudge?.panY || 0,
    zoom: nudge?.zoom > 0 ? nudge.zoom : 1,
  };
  if (direction === 'reset') return identityNudge();
  if (direction === 'left') {
    return { ...current, panX: clamp(current.panX - PAN_STEP, -PAN_LIMIT, PAN_LIMIT) };
  }
  if (direction === 'right') {
    return { ...current, panX: clamp(current.panX + PAN_STEP, -PAN_LIMIT, PAN_LIMIT) };
  }
  if (direction === 'down') {
    return { ...current, panY: clamp(current.panY - PAN_STEP, -PAN_LIMIT, PAN_LIMIT) };
  }
  if (direction === 'up') {
    return { ...current, panY: clamp(current.panY + PAN_STEP, -PAN_LIMIT, PAN_LIMIT) };
  }
  if (direction === 'zoom-in') {
    return { ...current, zoom: clamp(current.zoom * ZOOM_STEP, ZOOM_MIN, ZOOM_MAX) };
  }
  if (direction === 'zoom-out') {
    return { ...current, zoom: clamp(current.zoom / ZOOM_STEP, ZOOM_MIN, ZOOM_MAX) };
  }
  return current;
}

/**
 * Texture coordinate for a patch vertex. u and v are 0..1 on this camera's image.
 * @param {number} u
 * @param {number} v
 * @param {ImageNudge | null | undefined} nudge
 * @returns {{ u: number, v: number }}
 */
export function sampleUv(u, v, nudge) {
  const zoom = nudge?.zoom > 0 ? nudge.zoom : 1;
  const panX = nudge?.panX || 0;
  const panY = nudge?.panY || 0;
  return {
    u: 0.5 + (u - 0.5) / zoom - panX,
    v: 0.5 + (v - 0.5) / zoom - panY,
  };
}

/**
 * Replace one camera's nudge and leave every other camera's record alone.
 * @param {Readonly<Record<string, ImageNudge>>} nudges
 * @param {string} camera
 * @param {ImageNudge} nudge
 * @returns {Record<string, ImageNudge>}
 */
export function setCameraNudge(nudges, camera, nudge) {
  return { ...nudges, [camera]: nudge };
}
