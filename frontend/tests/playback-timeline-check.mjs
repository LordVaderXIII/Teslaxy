import { readFileSync } from 'node:fs';
import { telemetryIndex } from '../src/utils/telemetryIndex.mjs';
import {
  applyPlayerTransport,
  assignPlaybackRate,
  buildCameraSegments,
  findSegmentAtTime,
  localMediaTime,
  peerSeekTargets,
  resolveMediaClock,
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

// Cross-segment seek: 02:10 and 02:20 are inside the minute that starts at 120s.
// Reporting media time 0 must not publish 02:00, and the retry offset is 10s / 20s.
const threeMinutes = buildCameraSegments([
  { camera: 'Front', file_path: '/m0.mp4', timestamp: '2026-02-18T02:00:00Z' },
  { camera: 'Front', file_path: '/m1.mp4', timestamp: '2026-02-18T02:01:00Z' },
  { camera: 'Front', file_path: '/m2.mp4', timestamp: '2026-02-18T02:02:00Z' },
], {});
const at130 = findSegmentAtTime(threeMinutes.front, 130);
const at140 = findSegmentAtTime(threeMinutes.front, 140);
assert(at130 && at130.segment.file_path === '/m2.mp4', '02:10 belongs to the minute starting at 02:00');
assert(at140 && at140.segment.file_path === '/m2.mp4', '02:20 belongs to the same minute');
assert(localMediaTime(at130.segment.startTime, at130.segment.duration, 130) === 10, '02:10 is 10s into that file');
assert(localMediaTime(at140.segment.startTime, at140.segment.duration, 140) === 20, '02:20 is 20s into that file');
assert(localMediaTime(60, 60, 120) === null, 'a time exactly at the segment end belongs to the next minute');
assert(localMediaTime(120, 60, 120) === 0, 'the next minute owns its start');

const notLanded = resolveMediaClock({
  segmentStart: 120,
  segmentDuration: 60,
  mediaTime: 0,
  pendingGlobal: 130,
});
assert(notLanded.publishGlobal === null, 'media time 0 must not publish the start of the loaded minute');
assert(notLanded.retryLocal === 10, `unlanded 02:10 seek must retry local 10, got ${notLanded.retryLocal}`);
assert(notLanded.clearPending === false, 'pending seek stays until the element reports the offset');

const notLandedLater = resolveMediaClock({
  segmentStart: 120,
  segmentDuration: 60,
  mediaTime: 0,
  pendingGlobal: 140,
});
assert(notLandedLater.retryLocal === 20, `unlanded 02:20 seek must retry local 20, got ${notLandedLater.retryLocal}`);
assert(notLandedLater.publishGlobal === null, '02:20 must not collapse to the segment start either');

const landed = resolveMediaClock({
  segmentStart: 120,
  segmentDuration: 60,
  mediaTime: 10,
  pendingGlobal: 130,
});
assert(landed.clearPending === true, 'media time at the requested offset clears the pending seek');
assert(landed.publishGlobal === 130, `landed seek publishes 130, got ${landed.publishGlobal}`);
assert(landed.retryLocal === null, 'a landed seek does not seek again');

const natural = resolveMediaClock({
  segmentStart: 120,
  segmentDuration: 60,
  mediaTime: 12,
  pendingGlobal: null,
});
assert(natural.publishGlobal === 132, 'ordinary playback still publishes the media clock');
assert(natural.retryLocal === null, 'ordinary playback does not invent a seek');
assert(natural.clearPending === false, 'ordinary playback has nothing to clear');

const oldSegment = resolveMediaClock({
  segmentStart: 0,
  segmentDuration: 60,
  mediaTime: 50,
  pendingGlobal: 130,
});
assert(oldSegment.publishGlobal === null, 'the previous minute must not publish over a seek into a later minute');
assert(oldSegment.retryLocal === null, 'the previous minute must not be seeked past its own duration');

const segmentStartSeek = resolveMediaClock({
  segmentStart: 120,
  segmentDuration: 60,
  mediaTime: 0,
  pendingGlobal: 120,
});
assert(segmentStartSeek.clearPending === true, 'seeking exactly to a segment start counts as landed');
assert(segmentStartSeek.publishGlobal === 120, 'a landed start seek publishes that start');

const cross = {
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
};
assert(
  applyPlayerTransport(cross, { playbackRate: 1.5, localTime: 10 }) === true,
  'cross-segment transport must apply'
);
assert(cross.rate === 1.5, `cross-segment seek must keep 1.5x, got ${cross.rate}`);
assert(cross.time === 10, `cross-segment seek must set local time 10, got ${cross.time}`);

const playerSrc = readFileSync(new URL('../src/components/Player.tsx', import.meta.url), 'utf8');
assert(playerSrc.includes('applyPlayerTransport'), 'Player must apply transport when a segment player is ready');
assert(playerSrc.includes('shouldCorrectDrift'), 'Player must correct inter-camera drift');
assert(playerSrc.includes('assignPlaybackRate'), 'Player must set speed through the null-element guard');
assert(playerSrc.includes('buildCameraSegments'), 'Player timeline must use the shared segment builder');
assert(playerSrc.includes('pendingSeekRef'), 'Player must hold a cross-segment seek until the new file lands');
assert(playerSrc.includes('resolveMediaClock'), 'Player must not publish a segment start while a seek is pending');
assert(playerSrc.includes("'canplay'"), 'Player must retry the seek once the new element can play');

const overlaySrc = readFileSync(new URL('../src/components/TelemetryOverlay.tsx', import.meta.url), 'utf8');
assert(overlaySrc.includes('SYNC APPROX'), 'HUD keeps the approximate-sync label');

console.log('playback-timeline-check: ok');
