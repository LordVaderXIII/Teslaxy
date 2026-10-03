import { readFileSync } from 'node:fs';
import { telemetryIndex } from '../src/utils/telemetryIndex.mjs';
import {
  applyPlayerTransport,
  assignPlaybackRate,
  buildCameraSegments,
  peerSeekTargets,
  timelineDurationSeconds,
} from '../src/utils/playbackTimeline.mjs';

function assert(condition, message) {
  if (!condition) {
    console.error(`FAIL: ${message}`);
    process.exit(1);
  }
}

const frontPath = '/TeslaCam/SavedClips/2026-02-15_honk/2026-02-15_12-00-00-front.mp4';
const duplicateFront = [
  { camera: 'Front', file_path: frontPath, timestamp: '2026-02-15T01:30:00Z' },
  { camera: 'Front', file_path: frontPath, timestamp: '2026-02-15T01:30:00Z' },
];

// Two member rows for the same short file used to become 1s + 60s = 61s (01:01).
const inflated = buildCameraSegments(duplicateFront, {});
assert(inflated.front.length === 1, `duplicate front must be one segment, got ${inflated.front.length}`);
assert(
  timelineDurationSeconds(inflated) === 60,
  `duplicate without a probe must not be 61s, got ${timelineDurationSeconds(inflated)}`
);

const probed = buildCameraSegments(duplicateFront, { [frontPath]: 6.7218 });
const probedDuration = timelineDurationSeconds(probed);
assert(
  Math.abs(probedDuration - 6.7218) < 0.001,
  `probed short saved clip duration ${probedDuration}, want 6.7218 (not 61)`
);

const samples = Array.from({ length: 243 }, (_, i) => ({ frame_seq_no: i }));
const spreadOnInflated = telemetryIndex(samples, 3, 61);
const spreadOnMedia = telemetryIndex(samples, 3, probedDuration);
assert(spreadOnInflated.approximate === true, 'SYNC APPROX stays on; frame_seq_no is not frame-accurate time');
assert(spreadOnMedia.approximate === true, 'correct duration must not be reported as frame-accurate');
assert(
  spreadOnInflated.index < 30,
  `3s into a 61s timeline must sit near the start, index ${spreadOnInflated.index}`
);
assert(
  spreadOnMedia.index > 80 && spreadOnMedia.index < 160,
  `3s into 6.7218s must not stay at the start, index ${spreadOnMedia.index}`
);

// Distinct minutes of the same camera stay sequential.
const minutes = buildCameraSegments([
  { camera: 'Front', file_path: '/a-front-1.mp4', timestamp: '2026-02-18T00:00:00Z' },
  { camera: 'Front', file_path: '/a-front-2.mp4', timestamp: '2026-02-18T00:01:00Z' },
], { '/a-front-2.mp4': 59.5 });
assert(minutes.front.length === 2, 'two different front files must stay two segments');
assert(Math.abs(minutes.front[0].duration - 60) < 0.001, 'gap to the next minute stays 60s');
assert(Math.abs(minutes.front[1].duration - 59.5) < 0.001, 'last segment uses the probed duration');
assert(Math.abs(timelineDurationSeconds(minutes) - 119.5) < 0.001, 'timeline is gap plus probed tail');

// Review measurement: six cameras, constant ~2.5s offset, same start.
const targets = peerSeekTargets(25.005, [
  { id: 'Front', time: 25.005 },
  { id: 'Right Repeater', time: 22.503 },
  { id: 'Back', time: 22.520 },
]);
const targetIds = targets.map((t) => t.id).sort();
assert(
  targetIds.join(',') === 'Back,Right Repeater',
  `drift correction ids ${targetIds.join(',')}`
);
assert(targets.every((t) => t.time === 25.005), 'peers seek to the master clock');
const held = peerSeekTargets(35.005, [
  { id: 'Right Repeater', time: 32.503 },
  { id: 'Back', time: 32.520 },
]);
assert(held.length === 2, 'a gap that holds ten seconds later is still corrected');
const steady = peerSeekTargets(10, [{ id: 'Back', time: 10.1 }]);
assert(steady.length === 0, 'sub-threshold jitter must not seek');

const nullRateError = new TypeError("Cannot set properties of null (setting 'playbackRate')");
let disposedCalled = false;
const disposed = {
  isDisposed: () => true,
  playbackRate() {
    disposedCalled = true;
    throw nullRateError;
  },
};
assert(assignPlaybackRate(disposed, 1) === false, 'disposed player must not accept a rate');
assert(disposedCalled === false, 'disposed player playbackRate must not be called');
assert(assignPlaybackRate(null, 1.5) === false, 'null player must not throw');

const zombie = {
  isDisposed: () => false,
  playbackRate() {
    throw nullRateError;
  },
};
let zombieThrew = false;
try {
  assignPlaybackRate(zombie, 1);
} catch {
  zombieThrew = true;
}
assert(zombieThrew === false, 'null video element inside playbackRate must not escape');
assert(applyPlayerTransport(zombie, { playbackRate: 1.5, localTime: 0 }) === false, 'zombie transport fails closed');

const fresh = {
  isDisposed: () => false,
  rate: 1,
  time: 0,
  playbackRate(rate) {
    if (rate !== undefined) this.rate = rate;
    return this.rate;
  },
  currentTime(time) {
    if (time !== undefined) this.time = time;
    return this.time;
  },
  play() {
    return Promise.resolve();
  },
};
assert(
  applyPlayerTransport(fresh, { playbackRate: 1.5, localTime: 0, play: false }) === true,
  'new segment player must accept transport'
);
assert(fresh.rate === 1.5, `segment change must apply 1.5x, got ${fresh.rate}`);

const playerSrc = readFileSync(new URL('../src/components/Player.tsx', import.meta.url), 'utf8');
assert(playerSrc.includes('applyPlayerTransport'), 'Player must apply transport when a segment player is ready');
assert(playerSrc.includes('shouldCorrectDrift'), 'Player must correct inter-camera drift');
assert(playerSrc.includes('assignPlaybackRate'), 'Player must set speed through the null-element guard');
assert(playerSrc.includes('buildCameraSegments'), 'Player timeline must use the shared segment builder');

const overlaySrc = readFileSync(new URL('../src/components/TelemetryOverlay.tsx', import.meta.url), 'utf8');
assert(overlaySrc.includes('SYNC APPROX'), 'HUD keeps the approximate-sync label');

console.log('playback-timeline-check: ok');
