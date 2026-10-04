import {
  HW3_CAMERAS,
  SIDE_OVERLAP_DEG,
  azimuthDegOf,
  directionOwner,
  hiddenCameraNames,
  imageDirection,
  projectToImage,
} from '../src/utils/cameraRig.mjs';

function assert(condition, message) {
  if (!condition) {
    console.error(`FAIL: ${message}`);
    process.exit(1);
  }
}

function byName(name) {
  const camera = HW3_CAMERAS.find((item) => item.name === name);
  assert(camera, `missing ${name}`);
  return camera;
}

function roundTrip(camera, u, v) {
  const dir = imageDirection(camera, u, v);
  const back = projectToImage(camera, dir.x, dir.y, dir.z);
  assert(back, `${camera.name} pixel ${u},${v} fell outside its own lens`);
  const du = Math.abs(back.u - u);
  const dv = Math.abs(back.v - v);
  assert(du < 1e-3 && dv < 1e-3, `${camera.name} round trip ${u},${v} -> ${back.u},${back.v}`);
}

const front = byName('Front');
const leftPillar = byName('Left Pillar');
const leftRepeater = byName('Left Repeater');
const back = byName('Back');

assert(front.hfovDeg === 46 && front.vfovDeg === 34, 'main camera keeps the service-manual 46×34');
assert(front.projection === 'rectilinear', 'main camera is the rectilinear lens');
assert(leftPillar.hfovDeg === 90 && leftPillar.vfovDeg === 65.3, 'pillar keeps 90×65.3');
assert(leftPillar.projection === 'equidistant', 'pillar is the fisheye model');
assert(leftRepeater.hfovDeg === 75 && leftRepeater.vfovDeg === 55.4, 'repeater keeps 75×55.4');
assert(back.hfovDeg === 140, 'rear keeps the published 140° horizontal');
assert(front.yawDeg === 0 && back.yawDeg === 180, 'main looks forward and rear looks back');

const center = imageDirection(front, 0.5, 0.5);
assert(Math.abs(azimuthDegOf(center.x, center.y, center.z)) < 1e-6, 'front center is yaw 0');
assert(center.z < -0.99, 'front center looks toward -Z');

const frontLeft = imageDirection(front, 0, 0.5);
const frontRight = imageDirection(front, 1, 0.5);
const leftAz = azimuthDegOf(frontLeft.x, frontLeft.y, frontLeft.z);
const rightAz = azimuthDegOf(frontRight.x, frontRight.y, frontRight.z);
assert(leftAz > 22 && leftAz < 24, `image left is vehicle left, got ${leftAz}`);
assert(rightAz < -22 && rightAz > -24, `image right is vehicle right, got ${rightAz}`);
assert(Math.abs((leftAz - rightAz) - 46) < 1e-6, 'front angular width is 46°, not a 60° slice');

// Equidistant: halfway from center to the edge is half the edge angle.
const quarter = imageDirection(leftPillar, 0.75, 0.5);
const quarterOffset = azimuthDegOf(quarter.x, quarter.y, quarter.z) - leftPillar.yawDeg;
assert(Math.abs(quarterOffset + leftPillar.hfovDeg / 4) < 1e-4, `pillar quarter-pixel angle got ${quarterOffset}`);

for (const camera of HW3_CAMERAS) {
  roundTrip(camera, 0.5, 0.5);
  roundTrip(camera, 0.02, 0.5);
  roundTrip(camera, 0.98, 0.5);
  roundTrip(camera, 0.5, 0.05);
  roundTrip(camera, 0.5, 0.95);
  roundTrip(camera, 0.2, 0.8);
}

let covered = 0;
for (let azimuth = -180; azimuth < 180; azimuth += 1) {
  const rads = (azimuth * Math.PI) / 180;
  const owner = directionOwner(-Math.sin(rads), 0, -Math.cos(rads));
  assert(owner, `horizon ${azimuth}° has no camera`);
  covered += 1;
}
assert(covered === 360, 'every horizon degree has one camera');
assert(directionOwner(0, 0, -1) === 'Front', 'dead ahead belongs to the main camera');

const aboveFront = imageDirection(leftPillar, 0.5, 0.98);
assert(
  directionOwner(aboveFront.x, aboveFront.y, aboveFront.z) === 'Left Pillar',
  'a pillar pixel above the main camera stays on the pillar',
);

const pillarOuter = leftPillar.yawDeg + leftPillar.hfovDeg / 2;
const repeaterInner = leftRepeater.yawDeg - leftRepeater.hfovDeg / 2;
assert(Math.abs((pillarOuter - repeaterInner) - SIDE_OVERLAP_DEG) < 1e-9, 'pillar and repeater share the overlap used to seat the repeater');

// Phone-shaped view: vertical 34°, aspect about 9/19.5, looking forward.
// Pillars still reach the forward axis, so they stay. Repeaters and the
// rear camera are behind that view.
const phoneHidden = hiddenCameraNames(0, 0, -1, 34, 9 / 19.5);
assert(!phoneHidden.includes('Front'), 'front stays while looking forward');
assert(!phoneHidden.includes('Left Pillar'), 'left pillar still reaches the forward view');
assert(!phoneHidden.includes('Right Pillar'), 'right pillar still reaches the forward view');
assert(phoneHidden.includes('Left Repeater'), 'left repeater is behind a forward phone view');
assert(phoneHidden.includes('Right Repeater'), 'right repeater is behind a forward phone view');
assert(phoneHidden.includes('Back'), 'rear camera is behind a forward phone view');

// Wide view aimed at the pillar/repeater overlap (outer edge of the left pillar).
const joinDeg = ((leftPillar.yawDeg + leftPillar.hfovDeg / 2) + (leftRepeater.yawDeg - leftRepeater.hfovDeg / 2)) / 2;
const joinRad = (joinDeg * Math.PI) / 180;
const joinHidden = hiddenCameraNames(-Math.sin(joinRad), 0, -Math.cos(joinRad), 34, 16 / 9);
assert(!joinHidden.includes('Left Pillar'), 'join view keeps the pillar');
assert(!joinHidden.includes('Left Repeater'), 'join view keeps the repeater');
assert(joinHidden.includes('Right Repeater'), 'the far repeater stays paused at that join');

const outside = projectToImage(front, 1, 0, 0);
assert(outside === null, 'a ray 90° off the main axis is outside the 46° lens');

console.log('camera rig ok');
