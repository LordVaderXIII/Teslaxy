/**
 * The committed alignment must reproduce the production 3D cylinder.
 *
 * Production Scene3D (commit 73adf67) draws six open cylinders:
 *   radius 8, height 5, 32 radial segments, 1 height segment,
 *   theta length π/3, theta starts below, texture repeat [-1, 1] offset [1, 0],
 *   viewer position [0, 1.2, 0.1], orbit target [0, 1.2, 0].
 * Those literals live in this file on purpose. They are not read back
 * out of camera-alignment.json.
 */
import { readFileSync } from 'node:fs';
import { CylinderGeometry } from 'three';
import {
  CAMERA_IDS,
  TEXTURE_OFFSET,
  TEXTURE_REPEAT,
  alignmentsEqual,
  applyPlacement,
  cameraById,
  cloneAlignment,
  cylinderSpec,
  exportAlignment,
  nudgeCamera,
  parseAlignment,
  resetCamera,
  segmentPlacement,
} from '../src/utils/cameraAlignment.mjs';

function assert(condition, message) {
  if (!condition) {
    console.error(`FAIL: ${message}`);
    process.exit(1);
  }
}

const MAIN_RADIUS = 8;
const MAIN_HEIGHT = 5;
const MAIN_RADIAL = 32;
const MAIN_HEIGHT_SEGMENTS = 1;
const MAIN_THETA_LENGTH = Math.PI / 3;
const MAIN_POSITION = [0, 1.2, 0.1];
const MAIN_TARGET = [0, 1.2, 0];
const MAIN_THETA_START = {
  back: -Math.PI / 6,
  right_repeater: Math.PI / 6,
  right_pillar: Math.PI / 2,
  front: (5 * Math.PI) / 6,
  left_pillar: (7 * Math.PI) / 6,
  left_repeater: (3 * Math.PI) / 2,
};

function sameBuffer(actual, expected, label) {
  assert(actual.length === expected.length, `${label} length ${actual.length} vs ${expected.length}`);
  for (let i = 0; i < actual.length; i += 1) {
    if (actual[i] !== expected[i]) {
      assert(false, `${label}[${i}] ${actual[i]} !== ${expected[i]}`);
    }
  }
}

const raw = JSON.parse(readFileSync(new URL('../src/data/camera-alignment.json', import.meta.url), 'utf8'));
const parsed = parseAlignment(raw);
assert(parsed.ok, parsed.ok ? '' : parsed.message);
const alignment = parsed.alignment;

assert(alignment.schema_version === 1, 'schema version is 1');
assert(alignment.viewer.radius === MAIN_RADIUS, 'radius');
assert(alignment.viewer.height === MAIN_HEIGHT, 'height');
assert(alignment.viewer.radial_segments === MAIN_RADIAL, 'radial segments');
assert(alignment.viewer.height_segments === MAIN_HEIGHT_SEGMENTS, 'height segments');
assert(alignment.viewer.position.every((value, index) => value === MAIN_POSITION[index]), 'viewer position');
assert(alignment.viewer.target.every((value, index) => value === MAIN_TARGET[index]), 'viewer target');
assert(TEXTURE_REPEAT[0] === -1 && TEXTURE_REPEAT[1] === 1, 'texture repeat');
assert(TEXTURE_OFFSET[0] === 1 && TEXTURE_OFFSET[1] === 0, 'texture offset');

for (const id of CAMERA_IDS) {
  const camera = cameraById(alignment, id);
  assert(camera, `missing ${id}`);
  assert(camera.x === 0 && camera.y === 0 && camera.z === 0, `${id} position is the origin`);
  assert(camera.pitch_deg === 0 && camera.roll_deg === 0, `${id} pitch and roll are 0`);
  assert(camera.fov_deg === 60, `${id} fov is the 60° slice`);
  const spec = cylinderSpec(alignment, camera);
  assert(spec.thetaStart === MAIN_THETA_START[id], `${id} theta start ${spec.thetaStart}`);
  assert(spec.thetaLength === MAIN_THETA_LENGTH, `${id} theta length`);
  assert(spec.height === MAIN_HEIGHT, `${id} height`);
  assert(segmentPlacement(alignment, camera).identity, `${id} has no extra transform`);

  const expected = new CylinderGeometry(
    MAIN_RADIUS,
    MAIN_RADIUS,
    MAIN_HEIGHT,
    MAIN_RADIAL,
    MAIN_HEIGHT_SEGMENTS,
    true,
    MAIN_THETA_START[id],
    MAIN_THETA_LENGTH,
  );
  const actual = new CylinderGeometry(...spec.args);
  sameBuffer(actual.attributes.position.array, expected.attributes.position.array, `${id} position`);
  sameBuffer(actual.attributes.uv.array, expected.attributes.uv.array, `${id} uv`);
  sameBuffer(actual.index.array, expected.index.array, `${id} index`);
  expected.dispose();
  actual.dispose();
}

const scene = readFileSync(new URL('../src/components/Scene3D.tsx', import.meta.url), 'utf8');
assert(scene.includes('cylinderSpec('), 'Scene3D builds cylinders from the alignment');
assert(scene.includes('segmentPlacement('), 'Scene3D uses the alignment placement');
assert(scene.includes('TEXTURE_REPEAT'), 'Scene3D uses the production texture flip');
assert(scene.includes("data-alignment-source={usingDefaults ? 'defaults' : 'adjusted'}"), 'defaults are marked on the view');
assert(!scene.includes('1.82'), 'Scene3D does not carry the rejected front mount');
assert(!scene.includes('-45'), 'Scene3D does not carry the rejected pillar yaw');

const front = cameraById(alignment, 'front');
const nudged = nudgeCamera(alignment, 'front', 'x', 1);
assert(cameraById(nudged, 'front').x === 0.01, 'x step is 0.01');
for (const id of CAMERA_IDS) {
  if (id === 'front') continue;
  assert(cameraById(nudged, id) === cameraById(alignment, id), `${id} object is untouched by a front nudge`);
}
const yawed = nudgeCamera(alignment, 'left_repeater', 'yaw_deg', 1);
assert(cameraById(yawed, 'left_repeater').yaw_deg === 300.1, 'yaw step is 0.1°');
assert(cameraById(yawed, 'right_repeater').yaw_deg === 60, 'the other repeater yaw stays');
assert(
  cylinderSpec(yawed, cameraById(yawed, 'left_repeater')).thetaStart !== MAIN_THETA_START.left_repeater,
  'yaw moves the left repeater slice',
);
assert(
  cylinderSpec(yawed, cameraById(yawed, 'front')).thetaStart === MAIN_THETA_START.front,
  'yawing the repeater leaves the front slice',
);
const zoomed = nudgeCamera(alignment, 'back', 'fov_deg', -1);
assert(cameraById(zoomed, 'back').fov_deg === 59.9, 'fov step is 0.1°');
assert(cylinderSpec(zoomed, cameraById(alignment, 'front')).thetaStart === MAIN_THETA_START.front, 'front slice stays when back fov changes');

const placed = segmentPlacement(nudged, cameraById(nudged, 'front'));
assert(!placed.identity, 'a position nudge leaves the identity path');
const moved = applyPlacement([1, 2, 3], placed);
assert(Math.abs(moved[0] - 1.01) < 1e-9 && moved[1] === 2 && Math.abs(moved[2] - 3) < 1e-9, `translation ${moved}`);
const still = applyPlacement([1, 2, 3], segmentPlacement(nudged, cameraById(nudged, 'back')));
assert(still[0] === 1 && still[1] === 2 && still[2] === 3, 'back vertices stay put');

const pitched = nudgeCamera(alignment, 'front', 'pitch_deg', 1);
assert(cameraById(pitched, 'front').pitch_deg === 0.1, 'pitch step is 0.1°');
assert(segmentPlacement(pitched, cameraById(pitched, 'right_pillar')).identity, 'pitching front leaves the pillar on the identity path');

const rolled = nudgeCamera(alignment, 'right_pillar', 'roll_deg', -1);
assert(cameraById(rolled, 'right_pillar').roll_deg === -0.1, 'roll step is 0.1°');

const reset = resetCamera(nudged, alignment, 'front');
assert(alignmentsEqual(reset, alignment), 'reset restores the loaded front camera');
assert(cameraById(resetCamera(yawed, alignment, 'front'), 'left_repeater').yaw_deg === 300.1, 'resetting front does not undo the repeater');

const noViewer = parseAlignment(
  { schema_version: 1, cameras: raw.cameras },
  { fallbackViewer: alignment.viewer },
);
assert(noViewer.ok, noViewer.ok ? '' : noViewer.message);
assert(noViewer.alignment.viewer.radius === MAIN_RADIUS, 'a camera file keeps the current viewer');

const badViewer = parseAlignment({
  schema_version: 1,
  viewer: { radius: -1 },
  cameras: raw.cameras,
});
assert(!badViewer.ok, 'a bad viewer is rejected');

const stringVersion = parseAlignment({ ...raw, schema_version: '1' });
assert(!stringVersion.ok && stringVersion.message.includes('schema version'), stringVersion.message || 'string version');

const wrongVersion = parseAlignment({ ...raw, schema_version: 2 });
assert(!wrongVersion.ok, 'version 2 is rejected');
assert(wrongVersion.message.includes('schema version 2'), wrongVersion.message);
assert(wrongVersion.message.includes('version 1'), wrongVersion.message);

const missing = parseAlignment({ schema_version: 1, cameras: raw.cameras.slice(0, 5), viewer: raw.viewer });
assert(!missing.ok && missing.message.includes('missing'), missing.message || 'missing camera');

const bad = parseAlignment({ schema_version: 1, viewer: raw.viewer, cameras: 'nope' });
assert(!bad.ok, 'a bad camera list is rejected');

const nanCamera = {
  ...raw,
  cameras: raw.cameras.map((camera) => (
    camera.camera === 'front' ? { ...camera, yaw_deg: Number.NaN } : camera
  )),
};
assert(!parseAlignment(nanCamera).ok, 'NaN is rejected');

const roundTrip = parseAlignment(JSON.parse(exportAlignment(nudged)));
assert(roundTrip.ok, roundTrip.ok ? '' : roundTrip.message);
assert(alignmentsEqual(roundTrip.alignment, nudged), 'export then import keeps the nudged values');
assert(alignmentsEqual(cloneAlignment(alignment), alignment), 'the committed values survive a clone');
assert(front.yaw_deg === 180 && front.x === 0, 'front stays the production slice');

console.log('camera alignment matches production cylinder');
