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
 * Optical-axis yaw is NOT in those FOV tables. Pose comes from
 * GET /api/camera-poses (backend/services/camerapose.go), in the
 * ground_nominal FLU frame: x forward, y left, z up, metres, origin on
 * the ground. Yaw is positive toward the vehicle's left. Pitch is
 * positive up. The numbers below match that response.
 *
 * Cited:
 * - Main yaw 0 and rear yaw 180. Service text: the main camera looks
 *   forward, the rear-view camera looks behind the vehicle. Same exhibit
 *   as the field-of-view pairs.
 * - Front position (1.82, 0, 1.30) m. StandardE2E NATIX geometry, commit
 *   cff77e53: the front camera sits at (182, 0, 130) cm.
 *   https://github.com/stepankonev/StandardE2E/blob/cff77e53c557906f76365bb9b547210ebed22806/standard_e2e/caching/src_datasets/natix_multicam/_natix_geometry.py
 * - Left repeater lens axis (-0.848, +0.530, 0) and right repeater
 *   (-0.848, -0.530, 0). The same file states both repeater axes as
 *   (-0.848, +/-0.530, 0), and the left one as the +0.530 case.
 *   Pitch is 0 because that z component is 0. Lateral position is
 *   ty=+90 cm and ty=-90 cm.
 * - Left pillar lens axis (+0.341, +0.936, +0.087) from the same file.
 *
 * Not cited, so the previous value stays. These are not factory extrinsics:
 * - Right pillar yaw -45°. That is the rejected reading of the service
 *   words. No sourced replacement was found. Its pitch, roll, and
 *   position stay 0.
 * - Every roll stays 0. A lens axis does not determine roll, and the
 *   NATIX unit-test matrix is synthetic.
 * - Front and rear pitch stay 0. Repeater and pillar translations other
 *   than the repeater lateral values stay 0. The synthetic rear tx=-88
 *   fixture is not a mount.
 *
 * The direction sphere uses yaw, pitch, and roll. Mount position is stored
 * for the pose contract and is not used to move a pixel: no range is
 * published, so an offset would be an assumed depth.
 */

const MAIN_HFOV = 46;
const MAIN_VFOV = 34;
const PILLAR_HFOV = 90;
const PILLAR_VFOV = 65.3;
const REPEATER_HFOV = 75;
const REPEATER_VFOV = 55.4;
const REAR_HFOV = 140;
const REAR_VFOV = 105;

/** Previous right-pillar yaw. Unsourced. Not a factory extrinsic. */
const UNSOURCED_RIGHT_PILLAR_YAW = -45;

const RAD2DEG = 180 / Math.PI;

/** Lens axis (forward, left, up) to yaw and pitch. Roll is not in the axis. */
function yawPitch(forwardX, leftY, upZ) {
  const norm = Math.hypot(forwardX, leftY, upZ);
  return {
    yawDeg: Math.atan2(leftY, forwardX) * RAD2DEG,
    pitchDeg: Math.asin(upZ / norm) * RAD2DEG,
  };
}

const leftRepeaterAim = yawPitch(-0.848, 0.530, 0);
const rightRepeaterAim = yawPitch(-0.848, -0.530, 0);
const leftPillarAim = yawPitch(0.341, 0.936, 0.087);

/** @typedef {'rectilinear' | 'equidistant'} LensProjection */

/**
 * @typedef {object} RigCamera
 * @property {string} name
 * @property {number} hfovDeg
 * @property {number} vfovDeg
 * @property {number} yawDeg Positive yaw is toward the vehicle's left.
 * @property {number} pitchDeg Positive pitch looks up.
 * @property {number} rollDeg About the lens axis. 0 keeps image up vertical.
 * @property {number} xM Forward, metres, ground_nominal.
 * @property {number} yM Left, metres.
 * @property {number} zM Up, metres.
 * @property {LensProjection} projection
 * @property {'front' | 'pillar' | 'repeater' | 'back'} role
 */

/**
 * @typedef {object} CameraPoseRecord
 * @property {string} camera File token (front) or display name (Front).
 * @property {number} yaw_deg
 * @property {number} pitch_deg
 * @property {number} roll_deg
 * @property {number} x_m
 * @property {number} y_m
 * @property {number} z_m
 */

const CAMERA_TOKENS = Object.freeze({
  Front: 'front',
  'Left Pillar': 'left_pillar',
  'Right Pillar': 'right_pillar',
  'Left Repeater': 'left_repeater',
  'Right Repeater': 'right_repeater',
  Back: 'back',
});

/** @type {readonly RigCamera[]} */
const INTRINSICS = Object.freeze([
  intrinsic('Front', MAIN_HFOV, MAIN_VFOV, 'rectilinear', 'front'),
  intrinsic('Left Pillar', PILLAR_HFOV, PILLAR_VFOV, 'equidistant', 'pillar'),
  intrinsic('Right Pillar', PILLAR_HFOV, PILLAR_VFOV, 'equidistant', 'pillar'),
  intrinsic('Left Repeater', REPEATER_HFOV, REPEATER_VFOV, 'equidistant', 'repeater'),
  intrinsic('Right Repeater', REPEATER_HFOV, REPEATER_VFOV, 'equidistant', 'repeater'),
  intrinsic('Back', REAR_HFOV, REAR_VFOV, 'equidistant', 'back'),
]);

/**
 * Same numbers as GET /api/camera-poses. The viewer replaces these with
 * the response when the request succeeds.
 * @type {readonly CameraPoseRecord[]}
 */
export const GENERATION_POSES = Object.freeze([
  poseRecord('front', 0, 0, 0, 1.82, 0, 1.30),
  poseRecord('left_pillar', leftPillarAim.yawDeg, leftPillarAim.pitchDeg, 0, 0, 0, 0),
  poseRecord('right_pillar', UNSOURCED_RIGHT_PILLAR_YAW, 0, 0, 0, 0, 0),
  poseRecord('left_repeater', leftRepeaterAim.yawDeg, leftRepeaterAim.pitchDeg, 0, 0, 0.90, 0),
  poseRecord('right_repeater', rightRepeaterAim.yawDeg, rightRepeaterAim.pitchDeg, 0, 0, -0.90, 0),
  poseRecord('back', 180, 0, 0, 0, 0, 0),
]);

/** @type {readonly RigCamera[]} */
export const HW3_CAMERAS = applyCameraPoses(GENERATION_POSES);

function intrinsic(name, hfovDeg, vfovDeg, projection, role) {
  return Object.freeze({
    name,
    hfovDeg,
    vfovDeg,
    yawDeg: 0,
    pitchDeg: 0,
    rollDeg: 0,
    xM: 0,
    yM: 0,
    zM: 0,
    projection,
    role,
  });
}

function poseRecord(camera, yawDeg, pitchDeg, rollDeg, xM, yM, zM) {
  return Object.freeze({
    camera,
    yaw_deg: yawDeg,
    pitch_deg: pitchDeg,
    roll_deg: rollDeg,
    x_m: xM,
    y_m: yM,
    z_m: zM,
  });
}

function finiteOr(value, fallback) {
  return typeof value === 'number' && Number.isFinite(value) ? value : fallback;
}

function poseToken(record) {
  const raw = String(record.camera || record.name || '').trim().toLowerCase().replace(/ /g, '_');
  return raw === 'rear' ? 'back' : raw;
}

/**
 * Overlay API pose records on a rig. Omitted cameras keep `base`.
 * The generation rig passes the intrinsic shells. The viewer passes
 * HW3_CAMERAS so a short response does not wipe a sourced aim.
 * @param {readonly CameraPoseRecord[]} records
 * @param {readonly RigCamera[]} [base]
 * @returns {readonly RigCamera[]}
 */
export function applyCameraPoses(records, base = INTRINSICS) {
  const byToken = new Map();
  for (const record of records || []) {
    byToken.set(poseToken(record), record);
  }
  return Object.freeze(base.map((camera) => {
    const record = byToken.get(CAMERA_TOKENS[camera.name]);
    if (!record) return camera;
    return Object.freeze({
      ...camera,
      yawDeg: finiteOr(record.yaw_deg, camera.yawDeg),
      pitchDeg: finiteOr(record.pitch_deg, camera.pitchDeg),
      rollDeg: finiteOr(record.roll_deg, camera.rollDeg),
      xM: finiteOr(record.x_m, camera.xM),
      yM: finiteOr(record.y_m, camera.yM),
      zM: finiteOr(record.z_m, camera.zM),
    });
  }));
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
  const roll = rad(camera.rollDeg || 0);
  const cp = Math.cos(pitch);
  const sp = Math.sin(pitch);
  const sy = Math.sin(yaw);
  const cy = Math.cos(yaw);
  const cr = Math.cos(roll);
  const sr = Math.sin(roll);
  const forward = { x: -sy * cp, y: sp, z: -cy * cp };
  const right0 = { x: cy, y: 0, z: -sy };
  const up0 = {
    x: sy * sp,
    y: cp,
    z: cy * sp,
  };
  // Positive roll turns image up toward image left. Roll 0 is the old basis.
  const right = {
    x: right0.x * cr + up0.x * sr,
    y: right0.y * cr + up0.y * sr,
    z: right0.z * cr + up0.z * sr,
  };
  const up = {
    x: -right0.x * sr + up0.x * cr,
    y: -right0.y * sr + up0.y * cr,
    z: -right0.z * sr + up0.z * cr,
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
 * @param {readonly RigCamera[]} [cameras]
 * @returns {string[]}
 */
export function hiddenCameraNames(forwardX, forwardY, forwardZ, fovDeg, aspect, cameras = HW3_CAMERAS) {
  const viewAz = Math.atan2(-forwardX, -forwardZ);
  void forwardY;
  const viewHalf = horizontalHalfRad(fovDeg, aspect);
  const hidden = [];
  for (const camera of cameras) {
    const dist = Math.abs(angleDelta(viewAz, rad(camera.yawDeg)));
    const reaches = dist <= rad(camera.hfovDeg) / 2 + viewHalf;
    if (!reaches) hidden.push(camera.name);
  }
  return hidden;
}
