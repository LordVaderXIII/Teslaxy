/**
 * HW2.5 / HW3 dashcam rig for the six-camera viewer.
 *
 * Generation: the flat main image, strongly curved pillar and rear images,
 * and milder repeater images match the HW2.5/HW3 camera suite (same optics
 * on both; HW3 did not change these lenses). HW4's main camera is a wider
 * ~100° optic and would not look flat next to the pillars. Dashcam "front"
 * is the main camera, not the windshield fisheye (greentheonly: the recorder
 * moved from the narrow camera to the main camera).
 *
 * Lens numbers, each cited:
 * - Main 46° horizontal × 34° vertical, rectilinear. Tesla Driver Assistance
 *   service specification, "MAIN CAMERA" / "Triple Camera Field of View",
 *   exhibit in Benavides v. Tesla, S.D. Fla. 1:21-cv-21940, ECF 542-1
 *   (the HW2.5/HW3 service text). Tesla's 2016 Autopilot page rounds the
 *   same camera to 50°; the service pair is the one used here because it
 *   also gives the vertical angle. 34/46 is the pinhole ratio on the 4:3
 *   sensor (a tan projection predicts ~35.3°), and Tesla names a separate
 *   "fisheye" camera in that housing, so the main lens is rectilinear.
 * - B-pillar 90° × 65.3°, equidistant fisheye. Same service exhibit,
 *   "B-Pillar Camera Field of View". 65.3/90 ≈ 0.726, near the 1280×960
 *   sensor ratio 0.75. A rectilinear 90° lens on that sensor would be
 *   ~74° vertical, which this pair is not.
 * - Repeater 75° × 55.4°, equidistant fisheye. Same exhibit,
 *   "Side Repeater Field of View". 55.4/75 ≈ 0.739. Rectilinear would be
 *   ~60° vertical.
 * - Rear 140° horizontal. TeslaTap, "Undocumented", camera list credited
 *   to verygreen (archived 2023-01-05). The service exhibit calls the
 *   rear camera a wide-angle lens; the degree figure on that page did not
 *   survive text extraction. Vertical 105° is 140 × 960/1280, the HW3
 *   AR0136A 1280×960 sensor (Wikipedia, "Tesla Autopilot hardware", citing
 *   the EE Times HW3 teardown) under the same equidistant model.
 *
 * Optical-axis yaw is NOT in those FOV tables. The service text only says
 * where each camera looks:
 * - Main: windshield centerline, forward. Yaw 0.
 * - Rear-view: behind the vehicle. Yaw 180.
 * - B-pillar: "each side and the front corners" (forward-looking side).
 *   The axis used here is the midpoint of that named sector, halfway from
 *   dead ahead to abeam: ±45°. That is a reading of the words, not a
 *   factory extrinsic.
 * - Repeater: rearward-looking side camera. Its axis is set so the
 *   published 75° fan overlaps the pillar's outer edge by SIDE_OVERLAP_DEG.
 *   It is not a measured aim. Glados rejected this placement on
 *   RecentClips/2026-02-18_17-34-39. The degrees are unchanged: there is
 *   no published replacement. Handoff below does not treat them as extrinsics.
 * Pitch is calibrated per car and is not published as a nominal degree.
 * Every camera uses pitch 0.
 *
 * A per-car extrinsic (yaw, pitch, roll) is what would make one physical
 * edge land on one angle. The clip files and SEI metadata in this app do
 * not carry it. See the note in the pull request.
 */

export const SIDE_OVERLAP_DEG = 6;

const MAIN_HFOV = 46;
const MAIN_VFOV = 34;
const PILLAR_HFOV = 90;
const PILLAR_VFOV = 65.3;
const REPEATER_HFOV = 75;
const REPEATER_VFOV = 55.4;
const REAR_HFOV = 140;
const REAR_VFOV = 105;

const PILLAR_YAW = 45;
const REPEATER_YAW = (PILLAR_YAW + PILLAR_HFOV / 2) - SIDE_OVERLAP_DEG + REPEATER_HFOV / 2;

/** @typedef {'rectilinear' | 'equidistant'} LensProjection */

/**
 * @typedef {object} RigCamera
 * @property {string} name
 * @property {number} hfovDeg
 * @property {number} vfovDeg
 * @property {number} yawDeg Positive yaw is toward the vehicle's left.
 * @property {number} pitchDeg
 * @property {LensProjection} projection
 * @property {'front' | 'pillar' | 'repeater' | 'back'} role
 */

/** @type {readonly RigCamera[]} */
export const HW3_CAMERAS = Object.freeze([
  cam('Front', MAIN_HFOV, MAIN_VFOV, 0, 'rectilinear', 'front'),
  cam('Left Pillar', PILLAR_HFOV, PILLAR_VFOV, PILLAR_YAW, 'equidistant', 'pillar'),
  cam('Right Pillar', PILLAR_HFOV, PILLAR_VFOV, -PILLAR_YAW, 'equidistant', 'pillar'),
  cam('Left Repeater', REPEATER_HFOV, REPEATER_VFOV, REPEATER_YAW, 'equidistant', 'repeater'),
  cam('Right Repeater', REPEATER_HFOV, REPEATER_VFOV, -REPEATER_YAW, 'equidistant', 'repeater'),
  cam('Back', REAR_HFOV, REAR_VFOV, 180, 'equidistant', 'back'),
]);

function cam(name, hfovDeg, vfovDeg, yawDeg, projection, role) {
  return Object.freeze({
    name,
    hfovDeg,
    vfovDeg,
    yawDeg,
    pitchDeg: 0,
    projection,
    role,
  });
}

function rad(deg) {
  return (deg * Math.PI) / 180;
}

function deg(radians) {
  return (radians * 180) / Math.PI;
}

/** Shortest signed angle a-b in radians, wrapped to (-pi, pi]. */
export function angleDelta(a, b) {
  let d = a - b;
  const turn = Math.PI * 2;
  d = ((d + Math.PI) % turn + turn) % turn - Math.PI;
  return d;
}

/**
 * Vehicle basis. Forward is -Z (the default view). Positive yaw turns
 * toward the vehicle's left (-X). Image +X is the vehicle's right when
 * the camera looks forward. Image +Y is up.
 * @param {RigCamera} camera
 */
export function cameraBasis(camera) {
  const yaw = rad(camera.yawDeg);
  const pitch = rad(camera.pitchDeg);
  const cp = Math.cos(pitch);
  const sp = Math.sin(pitch);
  const sy = Math.sin(yaw);
  const cy = Math.cos(yaw);
  const forward = { x: -sy * cp, y: sp, z: -cy * cp };
  const right = { x: cy, y: 0, z: -sy };
  const up = {
    x: sy * sp,
    y: cp,
    z: cy * sp,
  };
  return { forward, right, up };
}

function addScaled(a, b, s) {
  return { x: a.x + b.x * s, y: a.y + b.y * s, z: a.z + b.z * s };
}

function dot(a, b) {
  return a.x * b.x + a.y * b.y + a.z * b.z;
}

function normalize(v) {
  const len = Math.hypot(v.x, v.y, v.z) || 1;
  return { x: v.x / len, y: v.y / len, z: v.z / len };
}

/**
 * World direction for a pixel. u=0 is the left edge of the image,
 * v=0 is the bottom, v=1 is the top.
 * @param {RigCamera} camera
 * @param {number} u
 * @param {number} v
 */
export function imageDirection(camera, u, v) {
  const ndcX = (u - 0.5) * 2;
  const ndcY = (v - 0.5) * 2;
  let x = 0;
  let y = 0;
  let z = 1;
  if (camera.projection === 'rectilinear') {
    x = ndcX * Math.tan(rad(camera.hfovDeg) / 2);
    y = ndcY * Math.tan(rad(camera.vfovDeg) / 2);
    z = 1;
  } else {
    const imgX = ndcX * (rad(camera.hfovDeg) / 2);
    const imgY = ndcY * (rad(camera.vfovDeg) / 2);
    const theta = Math.hypot(imgX, imgY);
    if (theta < 1e-8) {
      x = 0;
      y = 0;
      z = 1;
    } else {
      const s = Math.sin(theta) / theta;
      x = imgX * s;
      y = imgY * s;
      z = Math.cos(theta);
    }
  }
  const basis = cameraBasis(camera);
  return normalize(addScaled(addScaled(addScaled(
    { x: 0, y: 0, z: 0 },
    basis.forward,
    z,
  ), basis.right, x), basis.up, y));
}

/**
 * Inverse of imageDirection. Null when the ray is outside the published FOV.
 * @param {RigCamera} camera
 * @param {number} x
 * @param {number} y
 * @param {number} z
 * @returns {{ u: number, v: number } | null}
 */
export function projectToImage(camera, x, y, z) {
  const dir = normalize({ x, y, z });
  const basis = cameraBasis(camera);
  const cx = dot(dir, basis.right);
  const cy = dot(dir, basis.up);
  const cz = dot(dir, basis.forward);
  if (cz <= 1e-6) return null;
  let u = 0;
  let v = 0;
  if (camera.projection === 'rectilinear') {
    const halfH = Math.tan(rad(camera.hfovDeg) / 2);
    const halfV = Math.tan(rad(camera.vfovDeg) / 2);
    u = 0.5 + 0.5 * ((cx / cz) / halfH);
    v = 0.5 + 0.5 * ((cy / cz) / halfV);
  } else {
    const theta = Math.atan2(Math.hypot(cx, cy), cz);
    const radial = Math.hypot(cx, cy) || 1;
    const imgX = (cx / radial) * theta;
    const imgY = (cy / radial) * theta;
    u = 0.5 + imgX / rad(camera.hfovDeg);
    v = 0.5 + imgY / rad(camera.vfovDeg);
  }
  if (u < -1e-4 || u > 1 + 1e-4 || v < -1e-4 || v > 1 + 1e-4) return null;
  return { u, v };
}

/** Azimuth in degrees. 0 is forward (-Z). Positive is vehicle left. */
export function azimuthDegOf(x, _y, z) {
  return deg(Math.atan2(-x, -z));
}

/**
 * Camera that should draw this direction.
 *
 * Ownership on RecentClips/2026-02-18_17-34-39, from the annotated sheets.
 * These rules choose among cameras whose published lens already contains
 * the ray. They do not move an optical axis.
 * - Front keeps its whole frame: garage door, jambs, eaves, hatchback,
 *   forward carriageway, and the yellow line. A pillar must not replace
 *   a garage corner. No garage corner was verified in the raw pillar frames,
 *   so nothing is invented past that edge.
 * - A pillar keeps its whole frame when a repeater also sees the ray, so
 *   the repeater cannot cut through the SUV. The repeater draws only
 *   outside the pillar fan (driveway and road past the tail).
 * - Where those rules do not choose, the closest optical axis wins.
 *   A direction outside every published fan stays empty. That lower wedge
 *   is a coverage hole, not a splice.
 * @param {number} x
 * @param {number} y
 * @param {number} z
 * @returns {string | null}
 */
export function directionOwner(x, y, z) {
  const dir = normalize({ x, y, z });
  const seen = [];
  for (const camera of HW3_CAMERAS) {
    if (!projectToImage(camera, dir.x, dir.y, dir.z)) continue;
    const axis = imageDirection(camera, 0.5, 0.5);
    const dist = Math.acos(Math.min(1, Math.max(-1, dot(dir, axis))));
    seen.push({ camera, dist });
  }
  if (seen.length === 0) return null;
  if (seen.some((item) => item.camera.role === 'front')) return 'Front';
  const pillarSees = seen.some((item) => item.camera.role === 'pillar');
  let best = null;
  let bestDist = Infinity;
  for (const item of seen) {
    if (pillarSees && item.camera.role === 'repeater') continue;
    if (item.dist < bestDist) {
      bestDist = item.dist;
      best = item.camera.name;
    }
  }
  return best;
}

export function horizontalHalfRad(fovDeg, aspect) {
  const vertical = rad(fovDeg) / 2;
  return Math.atan(Math.tan(vertical) * Math.max(aspect, 1e-3));
}

/**
 * Cameras whose published horizontal fan misses the view.
 * A camera that still reaches the edge of the view is kept.
 * @param {number} forwardX
 * @param {number} forwardY
 * @param {number} forwardZ
 * @param {number} fovDeg Vertical field of the viewer, degrees.
 * @param {number} aspect Width / height.
 * @returns {string[]}
 */
export function hiddenCameraNames(forwardX, forwardY, forwardZ, fovDeg, aspect) {
  const viewAz = Math.atan2(-forwardX, -forwardZ);
  void forwardY;
  const viewHalf = horizontalHalfRad(fovDeg, aspect);
  const hidden = [];
  for (const camera of HW3_CAMERAS) {
    const dist = Math.abs(angleDelta(viewAz, rad(camera.yawDeg)));
    const reaches = dist <= rad(camera.hfovDeg) / 2 + viewHalf;
    if (!reaches) hidden.push(camera.name);
  }
  return hidden;
}
