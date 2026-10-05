/**
 * Viewing layout for the six-camera 3D player.
 *
 * The committed file (frontend/src/data/camera-alignment.json) is the
 * production cylinder: six 60° slices, radius 8, height 5, viewer at
 * (0, 1.2, 0.1) looking at (0, 1.2, 0). Those numbers are the Scene3D
 * layout from production commit 73adf67. They are not vehicle mounts
 * and they are not the rejected pose guesses.
 *
 * Yaw is the slice center in degrees, measured the same way as
 * CylinderGeometry theta: 0 is +Z (the back slice), 180 is -Z (front).
 * FOV is the horizontal arc of that slice. 60° is today's slice.
 * Pitch and roll rotate that slice around its own center. x, y, z
 * translate it in the same viewer units. One camera's record never
 * changes another camera's record.
 */

export const SCHEMA_VERSION = 1;

export const CAMERA_IDS = Object.freeze([
  'front',
  'back',
  'left_pillar',
  'right_pillar',
  'left_repeater',
  'right_repeater',
]);

/** Draw order matches the production Scene3D meshes. */
export const CAMERA_DRAW_ORDER = Object.freeze([
  'back',
  'right_repeater',
  'right_pillar',
  'front',
  'left_pillar',
  'left_repeater',
]);

export const CAMERA_LABELS = Object.freeze({
  front: 'Front',
  back: 'Back',
  left_pillar: 'Left Pillar',
  right_pillar: 'Right Pillar',
  left_repeater: 'Left Repeater',
  right_repeater: 'Right Repeater',
});

export const CAMERA_SHORT_LABELS = Object.freeze({
  front: 'Front',
  back: 'Back',
  left_pillar: 'L Pillar',
  right_pillar: 'R Pillar',
  left_repeater: 'L Repeat',
  right_repeater: 'R Repeat',
});

/** Horizontal flip used by the production video texture. */
export const TEXTURE_REPEAT = Object.freeze([-1, 1]);
export const TEXTURE_OFFSET = Object.freeze([1, 0]);

export const POS_STEP = 0.01;
export const ANGLE_STEP = 0.1;
export const FOV_STEP = 0.1;
export const POS_LIMIT = 8;
export const ANGLE_LIMIT = 180;
export const FOV_MIN = 10;
export const FOV_MAX = 180;

const MAIN_SLICE_FOV = 60;
const READ_ERROR = 'This alignment file could not be read. It needs schema version 1 and all six cameras.';

const STEPS = Object.freeze({
  x: POS_STEP,
  y: POS_STEP,
  z: POS_STEP,
  yaw_deg: ANGLE_STEP,
  pitch_deg: ANGLE_STEP,
  roll_deg: ANGLE_STEP,
  fov_deg: FOV_STEP,
});

function fail(message) {
  return { ok: false, message };
}

function finiteNumber(value) {
  return typeof value === 'number' && Number.isFinite(value);
}

function vec3(value, label) {
  if (!Array.isArray(value) || value.length !== 3 || !value.every(finiteNumber)) {
    return fail(`This alignment file has a bad ${label}. It needs three numbers.`);
  }
  return { ok: true, value: [value[0], value[1], value[2]] };
}

function readViewer(viewer, options) {
  if (viewer == null) {
    if (options.fallbackViewer) {
      return { ok: true, viewer: cloneViewer(options.fallbackViewer) };
    }
    return fail('This alignment file is missing the viewer block.');
  }
  if (typeof viewer !== 'object' || Array.isArray(viewer)) {
    return fail('This alignment file has a bad viewer block.');
  }
  if (!finiteNumber(viewer.radius) || viewer.radius <= 0) {
    return fail('This alignment file has a bad viewer radius.');
  }
  if (!finiteNumber(viewer.height) || viewer.height <= 0) {
    return fail('This alignment file has a bad viewer height.');
  }
  if (!Number.isInteger(viewer.radial_segments) || viewer.radial_segments < 3) {
    return fail('This alignment file has a bad viewer radial_segments.');
  }
  if (!Number.isInteger(viewer.height_segments) || viewer.height_segments < 1) {
    return fail('This alignment file has a bad viewer height_segments.');
  }
  const position = vec3(viewer.position, 'viewer position');
  if (!position.ok) return position;
  const target = vec3(viewer.target, 'viewer target');
  if (!target.ok) return target;
  return {
    ok: true,
    viewer: {
      radius: viewer.radius,
      height: viewer.height,
      radial_segments: viewer.radial_segments,
      height_segments: viewer.height_segments,
      position: position.value,
      target: target.value,
    },
  };
}

function readCameras(cameras) {
  if (!Array.isArray(cameras)) {
    return fail(READ_ERROR);
  }
  const byId = new Map();
  for (const camera of cameras) {
    if (typeof camera !== 'object' || camera == null || Array.isArray(camera)) {
      return fail(READ_ERROR);
    }
    const id = camera.camera;
    if (!CAMERA_IDS.includes(id)) {
      return fail(`This alignment file has an unknown camera "${String(id)}".`);
    }
    if (byId.has(id)) {
      return fail(`This alignment file lists ${id} more than once.`);
    }
    const fields = ['x', 'y', 'z', 'yaw_deg', 'pitch_deg', 'roll_deg', 'fov_deg'];
    for (const field of fields) {
      if (!finiteNumber(camera[field])) {
        return fail(`This alignment file has a bad ${field} for ${id}.`);
      }
    }
    if (camera.fov_deg <= 0 || camera.fov_deg > 360) {
      return fail(`This alignment file has a bad fov_deg for ${id}.`);
    }
    byId.set(id, {
      camera: id,
      x: camera.x,
      y: camera.y,
      z: camera.z,
      yaw_deg: camera.yaw_deg,
      pitch_deg: camera.pitch_deg,
      roll_deg: camera.roll_deg,
      fov_deg: camera.fov_deg,
    });
  }
  if (byId.size !== CAMERA_IDS.length) {
    const missing = CAMERA_IDS.filter((id) => !byId.has(id));
    return fail(`This alignment file is missing ${missing.join(', ')}.`);
  }
  return { ok: true, cameras: CAMERA_IDS.map((id) => byId.get(id)) };
}

/**
 * @param {unknown} input
 * @param {{ fallbackViewer?: object }} [options]
 * @returns {{ ok: true, alignment: object } | { ok: false, message: string }}
 */
export function parseAlignment(input, options = {}) {
  if (typeof input !== 'object' || input == null || Array.isArray(input)) {
    return fail(READ_ERROR);
  }
  if (!Object.prototype.hasOwnProperty.call(input, 'schema_version')) {
    return fail('This alignment file has no schema version. This player reads version 1.');
  }
  if (input.schema_version !== SCHEMA_VERSION) {
    return fail(`This alignment file is schema version ${String(input.schema_version)}. This player reads version ${SCHEMA_VERSION}.`);
  }
  const viewer = readViewer(input.viewer, options);
  if (!viewer.ok) return viewer;
  const cameras = readCameras(input.cameras);
  if (!cameras.ok) return cameras;
  return {
    ok: true,
    alignment: {
      schema_version: SCHEMA_VERSION,
      viewer: viewer.viewer,
      cameras: cameras.cameras,
    },
  };
}

function cloneViewer(viewer) {
  return {
    radius: viewer.radius,
    height: viewer.height,
    radial_segments: viewer.radial_segments,
    height_segments: viewer.height_segments,
    position: [...viewer.position],
    target: [...viewer.target],
  };
}

export function serializeAlignment(alignment) {
  return {
    schema_version: SCHEMA_VERSION,
    note: 'Viewing layout for the 3D player. These numbers are not a vehicle mount.',
    viewer: cloneViewer(alignment.viewer),
    cameras: alignment.cameras.map((camera) => ({
      camera: camera.camera,
      x: camera.x,
      y: camera.y,
      z: camera.z,
      yaw_deg: camera.yaw_deg,
      pitch_deg: camera.pitch_deg,
      roll_deg: camera.roll_deg,
      fov_deg: camera.fov_deg,
    })),
  };
}

export function exportAlignment(alignment) {
  return `${JSON.stringify(serializeAlignment(alignment), null, 2)}\n`;
}

export function cloneAlignment(alignment) {
  const parsed = parseAlignment(JSON.parse(exportAlignment(alignment)));
  if (!parsed.ok) {
    throw new Error(parsed.message);
  }
  return parsed.alignment;
}

export function alignmentsEqual(a, b) {
  return exportAlignment(a) === exportAlignment(b);
}

export function cameraById(alignment, cameraId) {
  return alignment.cameras.find((camera) => camera.camera === cameraId);
}

function clamp(value, min, max) {
  return Math.min(max, Math.max(min, value));
}

function wrap360(value) {
  const turned = value % 360;
  return turned < 0 ? turned + 360 : turned;
}

function roundStep(value) {
  return Math.round(value * 1000) / 1000;
}

/**
 * Move one field on one camera. sign is +1 or -1.
 * Every other camera object is the same record.
 */
export function nudgeCamera(alignment, cameraId, field, sign) {
  if (!STEPS[field]) return alignment;
  return {
    ...alignment,
    cameras: alignment.cameras.map((camera) => {
      if (camera.camera !== cameraId) return camera;
      let value = camera[field] + STEPS[field] * sign;
      if (field === 'yaw_deg') value = wrap360(value);
      else if (field === 'pitch_deg' || field === 'roll_deg') value = clamp(value, -ANGLE_LIMIT, ANGLE_LIMIT);
      else if (field === 'fov_deg') value = clamp(value, FOV_MIN, FOV_MAX);
      else value = clamp(value, -POS_LIMIT, POS_LIMIT);
      return { ...camera, [field]: roundStep(value) };
    }),
  };
}

/** Put one camera back to the loaded baseline. The other five stay as they are. */
export function resetCamera(alignment, baseline, cameraId) {
  const source = cameraById(baseline, cameraId);
  if (!source) return alignment;
  return {
    ...alignment,
    cameras: alignment.cameras.map((camera) => (
      camera.camera === cameraId ? { ...source } : camera
    )),
  };
}

/**
 * CylinderGeometry arguments for one camera.
 * At the committed defaults these are the production slices:
 * radius 8, height 5, 32 radial segments, open ended, 60° arc.
 */
export function cylinderSpec(alignment, camera) {
  const { viewer } = alignment;
  const thetaLength = (camera.fov_deg * Math.PI) / 180;
  const thetaStart = (camera.yaw_deg * Math.PI) / 180 - thetaLength / 2;
  const height = viewer.height * (camera.fov_deg / MAIN_SLICE_FOV);
  return {
    radiusTop: viewer.radius,
    radiusBottom: viewer.radius,
    height,
    radialSegments: viewer.radial_segments,
    heightSegments: viewer.height_segments,
    openEnded: true,
    thetaStart,
    thetaLength,
    args: [
      viewer.radius,
      viewer.radius,
      height,
      viewer.radial_segments,
      viewer.height_segments,
      true,
      thetaStart,
      thetaLength,
    ],
  };
}

function axisAngleQuaternion(axis, angle) {
  const half = angle / 2;
  const s = Math.sin(half);
  return { x: axis.x * s, y: axis.y * s, z: axis.z * s, w: Math.cos(half) };
}

function multiplyQuaternion(a, b) {
  return {
    x: a.w * b.x + a.x * b.w + a.y * b.z - a.z * b.y,
    y: a.w * b.y - a.x * b.z + a.y * b.w + a.z * b.x,
    z: a.w * b.z + a.x * b.y - a.y * b.x + a.z * b.w,
    w: a.w * b.w - a.x * b.x - a.y * b.y - a.z * b.z,
  };
}

/**
 * Where the slice sits. identity is true only when x, y, z, pitch, and
 * roll are all zero, which is the production mesh with no extra transform.
 */
export function segmentPlacement(alignment, camera) {
  const identity = camera.x === 0
    && camera.y === 0
    && camera.z === 0
    && camera.pitch_deg === 0
    && camera.roll_deg === 0;
  const theta = (camera.yaw_deg * Math.PI) / 180;
  const centerX = alignment.viewer.radius * Math.sin(theta);
  const centerZ = alignment.viewer.radius * Math.cos(theta);
  if (identity) {
    return {
      identity: true,
      position: [0, 0, 0],
      quaternion: [0, 0, 0, 1],
      meshPosition: [0, 0, 0],
    };
  }
  const tangent = { x: Math.cos(theta), y: 0, z: -Math.sin(theta) };
  const radial = { x: Math.sin(theta), y: 0, z: Math.cos(theta) };
  let quaternion = { x: 0, y: 0, z: 0, w: 1 };
  if (camera.pitch_deg !== 0) {
    quaternion = multiplyQuaternion(
      quaternion,
      axisAngleQuaternion(tangent, (camera.pitch_deg * Math.PI) / 180),
    );
  }
  if (camera.roll_deg !== 0) {
    quaternion = multiplyQuaternion(
      quaternion,
      axisAngleQuaternion(radial, (camera.roll_deg * Math.PI) / 180),
    );
  }
  return {
    identity: false,
    position: [centerX + camera.x, camera.y, centerZ + camera.z],
    quaternion: [quaternion.x, quaternion.y, quaternion.z, quaternion.w],
    meshPosition: [-centerX, 0, -centerZ],
  };
}

function rotateByQuaternion(quaternion, x, y, z) {
  const [qx, qy, qz, qw] = quaternion;
  const tx = 2 * (qy * z - qz * y);
  const ty = 2 * (qz * x - qx * z);
  const tz = 2 * (qx * y - qy * x);
  return [
    x + qw * tx + (qy * tz - qz * ty),
    y + qw * ty + (qz * tx - qx * tz),
    z + qw * tz + (qx * ty - qy * tx),
  ];
}

/** Geometry vertex through the same group transform Scene3D applies. */
export function applyPlacement(vertex, placement) {
  if (placement.identity) return [vertex[0], vertex[1], vertex[2]];
  const shifted = [
    vertex[0] + placement.meshPosition[0],
    vertex[1] + placement.meshPosition[1],
    vertex[2] + placement.meshPosition[2],
  ];
  const rotated = rotateByQuaternion(placement.quaternion, shifted[0], shifted[1], shifted[2]);
  return [
    rotated[0] + placement.position[0],
    rotated[1] + placement.position[1],
    rotated[2] + placement.position[2],
  ];
}

export function viewerCamera(alignment) {
  return {
    position: alignment.viewer.position,
    target: alignment.viewer.target,
  };
}
