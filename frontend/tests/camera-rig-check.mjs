import {
  HW3_CAMERAS,
  applyCameraPoses,
  azimuthDegOf,
  cameraBasis,
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
assert(front.xM === 1.82 && front.yM === 0 && front.zM === 1.30, 'front mount is the cited (1.82, 0, 1.30) m');
assert(front.pitchDeg === 0 && front.rollDeg === 0, 'front pitch and roll stay at the previous 0');

const leftRepeaterYaw = Math.atan2(0.530, -0.848) * 180 / Math.PI;
const rightRepeaterYaw = Math.atan2(-0.530, -0.848) * 180 / Math.PI;
const leftPillarNorm = Math.hypot(0.341, 0.936, 0.087);
const leftPillarYaw = Math.atan2(0.936, 0.341) * 180 / Math.PI;
const leftPillarPitch = Math.asin(0.087 / leftPillarNorm) * 180 / Math.PI;
assert(Math.abs(leftRepeater.yawDeg - leftRepeaterYaw) < 1e-9, 'left repeater yaw is the cited axis');
assert(leftRepeater.pitchDeg === 0 && leftRepeater.yM === 0.90 && leftRepeater.xM === 0 && leftRepeater.zM === 0, 'left repeater pitch is 0 and only y is cited');
assert(Math.abs(leftPillar.yawDeg - leftPillarYaw) < 1e-9 && Math.abs(leftPillar.pitchDeg - leftPillarPitch) < 1e-9, 'left pillar yaw and pitch are the cited axis');
assert(leftPillar.rollDeg === 0 && leftPillar.xM === 0 && leftPillar.yM === 0 && leftPillar.zM === 0, 'left pillar roll and position stay unsourced');

const rightPillarEarly = byName('Right Pillar');
const rightRepeaterEarly = byName('Right Repeater');
assert(rightPillarEarly.yawDeg === -45 && rightPillarEarly.pitchDeg === 0 && rightPillarEarly.rollDeg === 0, 'right pillar keeps the previous unsourced yaw');
assert(rightPillarEarly.xM === 0 && rightPillarEarly.yM === 0 && rightPillarEarly.zM === 0, 'right pillar position stays at the origin');
assert(Math.abs(rightRepeaterEarly.yawDeg - rightRepeaterYaw) < 1e-9, 'right repeater yaw is the cited -0.530 axis');
assert(rightRepeaterEarly.yawDeg !== -121.5, 'right repeater does not keep the rejected overlap yaw');
assert(rightRepeaterEarly.yM === -0.90 && rightRepeaterEarly.xM === 0 && rightRepeaterEarly.zM === 0, 'right repeater only has the cited lateral position');
assert(back.xM === 0 && back.yM === 0 && back.zM === 0 && back.pitchDeg === 0 && back.rollDeg === 0, 'rear pitch, roll, and position stay unsourced');

const overridden = applyCameraPoses([{
  camera: 'Front',
  yaw_deg: 1,
  pitch_deg: 0,
  roll_deg: 0,
  x_m: 1.82,
  y_m: 0,
  z_m: 1.30,
}], HW3_CAMERAS);
assert(overridden.find((item) => item.name === 'Front').yawDeg === 1, 'API pose records replace yaw');
assert(overridden.find((item) => item.name === 'Back').yawDeg === 180, 'a missing API camera keeps the generation pose');

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

// Equidistant, pitch 0: halfway from center to the edge is half the edge angle.
// The left pillar is pitched, so this check uses the right pillar.
const rightPillarFlat = byName('Right Pillar');
const quarter = imageDirection(rightPillarFlat, 0.75, 0.5);
const quarterOffset = azimuthDegOf(quarter.x, quarter.y, quarter.z) - rightPillarFlat.yawDeg;
assert(Math.abs(quarterOffset + rightPillarFlat.hfovDeg / 4) < 1e-4, `pillar quarter-pixel angle got ${quarterOffset}`);

const leftAxis = imageDirection(leftPillar, 0.5, 0.5);
const leftElevation = Math.asin(leftAxis.y) * 180 / Math.PI;
assert(Math.abs(leftElevation - leftPillar.pitchDeg) < 1e-6, 'left pillar center elevation is its pitch');
assert(Math.abs(azimuthDegOf(leftAxis.x, leftAxis.y, leftAxis.z) - leftPillar.yawDeg) < 1e-4, 'left pillar center azimuth is its yaw');

const level = cameraBasis(leftRepeater);
const rolled = cameraBasis({ ...leftRepeater, rollDeg: 90 });
assert(Math.abs(level.forward.x - rolled.forward.x) < 1e-9, 'roll leaves the lens axis fixed');
assert(Math.abs(rolled.right.x - level.up.x) < 1e-6 && Math.abs(rolled.up.x + level.right.x) < 1e-6, 'a 90° roll turns image up onto the old image right');

for (const camera of HW3_CAMERAS) {
  roundTrip(camera, 0.5, 0.5);
  roundTrip(camera, 0.02, 0.5);
  roundTrip(camera, 0.98, 0.5);
  roundTrip(camera, 0.5, 0.05);
  roundTrip(camera, 0.5, 0.95);
  roundTrip(camera, 0.2, 0.8);
}

assert(directionOwner(0, 0, -1) === 'Front', 'dead ahead belongs to the main camera');

const aboveFront = imageDirection(leftPillar, 0.5, 0.98);
assert(
  directionOwner(aboveFront.x, aboveFront.y, aboveFront.z) === 'Left Pillar',
  'a pillar pixel above the main camera stays on the pillar',
);

// Phone-shaped view: vertical 34°, aspect about 9/19.5, looking forward.
// The sourced left pillar yaw is about 70°, so that narrow view no longer
// reaches it. The right pillar yaw is still the unsourced -45°, so it does.
const phoneHidden = hiddenCameraNames(0, 0, -1, 34, 9 / 19.5);
assert(!phoneHidden.includes('Front'), 'front stays while looking forward');
assert(phoneHidden.includes('Left Pillar'), 'sourced left pillar yaw sits outside a narrow forward view');
assert(!phoneHidden.includes('Right Pillar'), 'unsourced right pillar yaw still reaches the forward view');
assert(phoneHidden.includes('Left Repeater'), 'left repeater is behind a forward phone view');
assert(phoneHidden.includes('Right Repeater'), 'right repeater is behind a forward phone view');
assert(phoneHidden.includes('Back'), 'rear camera is behind a forward phone view');

let leftOverlap = null;
for (let azimuth = 0; azimuth < 180; azimuth += 0.5) {
  const sample = ray(azimuth, 0);
  if (projectToImage(leftPillar, ...sample) && projectToImage(leftRepeater, ...sample)) {
    leftOverlap = sample;
    break;
  }
}
assert(leftOverlap, 'left pillar and left repeater still share some horizon');
assert(directionOwner(...leftOverlap) === 'Left Pillar', 'left pillar keeps the repeater overlap');
const joinAz = azimuthDegOf(leftOverlap[0], leftOverlap[1], leftOverlap[2]);
const joinRad = (joinAz * Math.PI) / 180;
const joinHidden = hiddenCameraNames(-Math.sin(joinRad), 0, -Math.cos(joinRad), 34, 16 / 9);
assert(!joinHidden.includes('Left Pillar'), 'join view keeps the pillar');
assert(!joinHidden.includes('Left Repeater'), 'join view keeps the repeater');
assert(joinHidden.includes('Right Repeater'), 'the far repeater stays paused at that join');

const outside = projectToImage(front, 1, 0, 0);
assert(outside === null, 'a ray 90° off the main axis is outside the 46° lens');

function ray(azimuth, elevation = 0) {
  const a = (azimuth * Math.PI) / 180;
  const e = (elevation * Math.PI) / 180;
  const ce = Math.cos(e);
  return [-Math.sin(a) * ce, Math.sin(e), -Math.cos(a) * ce];
}

const rightPillar = byName('Right Pillar');
const rightRepeater = byName('Right Repeater');

// Right pillar yaw is still -45, so its fan ends near -90°. The cited
// repeater axis sits near -148°, so the old shared azimuth is pillar-only
// and the wedge between the fans stays empty.
const suvAzimuth = ray(-87);
assert(projectToImage(rightPillar, ...suvAzimuth), 'right pillar still sees -87°');
assert(projectToImage(rightRepeater, ...suvAzimuth) === null, 'cited repeater aim does not reach -87°');
assert(directionOwner(...suvAzimuth) === 'Right Pillar', 'right pillar keeps -87°');
let rightShared = false;
for (let azimuth = -180; azimuth < 0; azimuth += 0.5) {
  const sample = ray(azimuth, 0);
  if (projectToImage(rightPillar, ...sample) && projectToImage(rightRepeater, ...sample)) rightShared = true;
}
assert(!rightShared, 'right pillar and repeater fans stay apart until the pillar pose is sourced');
const rightGap = ray(-100);
assert(directionOwner(...rightGap) === null, 'the unsourced gap between right pillar and repeater stays empty');
const repeaterOnly = ray(-150);
assert(projectToImage(rightRepeater, ...repeaterOnly), 'right repeater sees its own axis neighbourhood');
assert(directionOwner(...repeaterOnly) === 'Right Repeater', 'right repeater keeps directions only it sees');

// Front keeps garage corners and the yellow line. A pillar that also
// contains that ray does not replace them.
const frontEdge = imageDirection(front, 0.98, 0.5);
const frontCorner = imageDirection(front, 0.98, 0.02);
assert(directionOwner(frontEdge.x, frontEdge.y, frontEdge.z) === 'Front', 'front edge stays on the front camera');
assert(directionOwner(frontCorner.x, frontCorner.y, frontCorner.z) === 'Front', 'front corner stays on the front camera');

// The lower wedge is still uncovered. Do not fill it with a neighbour.
const hole = ray(0, -34);
assert(directionOwner(...hole) === null, 'the coverage hole under the front stays empty');
const lowerForward = ray(0, -30);
assert(directionOwner(...lowerForward) === null, 'no camera covers -30° elevation dead ahead');

console.log('camera rig ok');
