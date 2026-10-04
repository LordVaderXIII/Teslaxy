import { readFileSync } from 'node:fs';
import {
  PAN_LIMIT,
  PAN_STEP,
  ZOOM_MAX,
  ZOOM_MIN,
  identityNudge,
  moveNudge,
  sampleUv,
  setCameraNudge,
} from '../src/utils/imageNudge.mjs';

function assert(condition, message) {
  if (!condition) {
    console.error(`FAIL: ${message}`);
    process.exit(1);
  }
}

const identity = identityNudge();
const center = sampleUv(0.5, 0.5, identity);
assert(center.u === 0.5 && center.v === 0.5, 'identity leaves the image center');
const corner = sampleUv(0.2, 0.8, identity);
assert(corner.u === 0.2 && corner.v === 0.8, 'identity leaves every texel where the projection put it');

const right = moveNudge(identity, 'right');
assert(Math.abs(right.panX - PAN_STEP) < 1e-12 && right.panY === 0 && right.zoom === 1, 'right moves only panX');
const sampled = sampleUv(0.5, 0.5, right);
assert(Math.abs(sampled.u - (0.5 - PAN_STEP)) < 1e-12 && sampled.v === 0.5, 'positive panX samples toward the left so the image moves right');

const up = moveNudge(identity, 'up');
assert(up.panY === PAN_STEP && up.panX === 0, 'up moves only panY');
const lifted = sampleUv(0.5, 0.5, up);
assert(lifted.v === 0.5 - PAN_STEP && lifted.u === 0.5, 'positive panY samples downward so the image moves up');

const zoomed = moveNudge(identity, 'zoom-in');
assert(zoomed.zoom > 1 && zoomed.panX === 0, 'zoom-in enlarges without panning');
const wide = sampleUv(0, 0.5, { panX: 0, panY: 0, zoom: 2 });
const tight = sampleUv(1, 0.5, { panX: 0, panY: 0, zoom: 2 });
assert(Math.abs(wide.u - 0.25) < 1e-12 && Math.abs(tight.u - 0.75) < 1e-12, 'zoom 2 uses the middle half of the image');
assert(sampleUv(0.5, 0.5, zoomed).u === 0.5, 'zoom keeps the center fixed');

let nudges = {};
nudges = setCameraNudge(nudges, 'Front', right);
nudges = setCameraNudge(nudges, 'Right Repeater', moveNudge(identity, 'zoom-out'));
assert(nudges.Front.panX === PAN_STEP && nudges.Front.zoom === 1, 'front keeps its own pan');
assert(nudges['Right Repeater'].zoom < 1 && nudges['Right Repeater'].panX === 0, 'repeater keeps its own zoom');
assert(nudges['Left Pillar'] == null && nudges.Back == null, 'untouched cameras have no nudge');
assert(nudges.Front !== right || nudges.Front.panX === PAN_STEP, 'front record survived the repeater edit');

const again = setCameraNudge(nudges, 'Right Repeater', moveNudge(nudges['Right Repeater'], 'left'));
assert(again.Front.panX === nudges.Front.panX && again.Front.zoom === 1, 'nudging the repeater does not change front');
assert(again['Right Repeater'].panX === -PAN_STEP, 'repeater pan is independent');

assert(moveNudge({ panX: PAN_LIMIT, panY: 0, zoom: 1 }, 'right').panX === PAN_LIMIT, 'pan stops at the limit');
assert(moveNudge({ panX: 0, panY: 0, zoom: ZOOM_MAX }, 'zoom-in').zoom === ZOOM_MAX, 'zoom stops at the maximum');
assert(moveNudge({ panX: 0.2, panY: -0.1, zoom: 2 }, 'reset').zoom === 1, 'reset returns to the projection');
assert(moveNudge({ panX: 0, panY: 0, zoom: ZOOM_MIN }, 'zoom-out').zoom === ZOOM_MIN, 'zoom stops at the minimum');

const shader = readFileSync(new URL('../src/components/Scene3D.tsx', import.meta.url), 'utf8');
assert(
  shader.includes('vec2(0.5) + (vUv - vec2(0.5)) / imageZoom - imagePan'),
  'the stitch shader uses the same pan and zoom as sampleUv',
);

console.log('image nudge ok');
