import { mergeClips } from '../src/utils/clipMerge.mjs';
import { shouldCommitDetail } from '../src/utils/detailCommit.mjs';
import {
  libraryEventToClip,
  clipMapPoint,
  mergeDetailTelemetry,
} from '../src/utils/libraryAdapter.mjs';

function assert(condition, message) {
  if (!condition) {
    console.error(`FAIL: ${message}`);
    process.exit(1);
  }
}

function sameMembers(actual, expected) {
  if (!Array.isArray(actual) || actual.length !== expected.length) return false;
  const a = [...actual].sort((x, y) => x - y);
  const b = [...expected].sort((x, y) => x - y);
  return a.every((id, i) => id === b[i]);
}

// a) id mapping: library {id: 7, ...} → clip.ID === 7
const mapped = libraryEventToClip({
  id: 7,
  timestamp: '2026-09-24T01:00:00Z',
  event: 'Sentry',
  city: 'Melbourne',
  reason: 'sentry_aware_object_detection',
  source_dir: '/TeslaCam/SentryClips/2026-09-24_01-00-00',
  video_file_count: 4,
  latitude: -37.8,
  longitude: 144.9,
  preview_path: '/media/front.mp4',
  preview_camera: 'Front',
  preview_seek_seconds: 12,
});
assert(mapped.ID === 7, `id mapping: expected ID 7, got ${mapped.ID}`);
assert(mapped.video_files === undefined, 'id mapping must not stuff preview into video_files');

// b) <65s grouping with video_file_count
const older = libraryEventToClip({
  id: 10,
  timestamp: '2026-09-24T01:00:00Z',
  event: 'Sentry',
  city: 'Melbourne',
  source_dir: '/TeslaCam/SentryClips/2026-09-24_01-00-00',
  video_file_count: 4,
});
const newer = libraryEventToClip({
  id: 11,
  timestamp: '2026-09-24T01:01:00Z',
  event: 'Sentry',
  city: 'Melbourne',
  source_dir: '/TeslaCam/SentryClips/2026-09-24_01-01-00',
  video_file_count: 1,
});
// Library (and the previous clips list) arrives newest-first.
const grouped = mergeClips([newer, older]);
assert(grouped.length === 1, `grouping: expected 1 merged clip, got ${grouped.length}`);
assert(grouped[0].ID === 10, `grouping: richest score must win, got ID ${grouped[0].ID}`);
assert(
  sameMembers(grouped[0].member_ids, [10, 11]),
  `grouping: member_ids must include both IDs, got ${JSON.stringify(grouped[0].member_ids)}`
);
assert(grouped[0].video_files === undefined, 'grouping must not invent video_files from preview');

const far = libraryEventToClip({
  id: 12,
  timestamp: '2026-09-24T01:05:00Z',
  event: 'Sentry',
  city: 'Melbourne',
  source_dir: '/TeslaCam/SentryClips/2026-09-24_01-05-00',
  video_file_count: 2,
});
const split = mergeClips([far, newer, older]);
assert(split.length === 2, `grouping: rows 5 min apart must not merge, got ${split.length}`);

// c) stale file response rejection
assert(shouldCommitDetail(1, 2) === false, 'stale files: shouldCommitDetail(1, 2) must be false');
assert(shouldCommitDetail(1, 1) === true, 'stale files: shouldCommitDetail(1, 1) must be true');

// d) map point without video_files
const withPoint = libraryEventToClip({
  id: 20,
  timestamp: '2026-09-24T02:00:00Z',
  event: 'Saved',
  city: 'Geelong',
  latitude: -38.15,
  longitude: 144.36,
  video_file_count: 6,
});
assert(withPoint.video_files === undefined, 'map point: must not require video_files');
assert(withPoint.telemetry?.latitude === -38.15, 'map point: library lat must land on telemetry');
assert(withPoint.telemetry?.longitude === 144.36, 'map point: library lon must land on telemetry');
const plotted = clipMapPoint(withPoint);
assert(plotted !== null && plotted[0] === -38.15 && plotted[1] === 144.36, 'map point: valid coords must plot');

const origin = libraryEventToClip({
  id: 21,
  timestamp: '2026-09-24T02:00:00Z',
  event: 'Saved',
  city: 'Null Island',
  latitude: 0,
  longitude: 0,
  video_file_count: 1,
});
assert(origin.telemetry?.latitude === undefined, 'map point: 0,0 must not be copied onto telemetry');
assert(clipMapPoint(origin) === null, 'map point: 0,0 must not plot');
assert(clipMapPoint({ telemetry: { latitude: 0, longitude: 0 } }) === null, 'map point: telemetry 0,0 must not plot');

const libraryTelemetry = { latitude: -37.8, longitude: 144.9, speed: 12 };
const kept = mergeDetailTelemetry(libraryTelemetry, { latitude: 0, longitude: 0, speed: 40 });
assert(kept.latitude === -37.8 && kept.longitude === 144.9, 'map point: invalid detail must keep library point');
assert(kept.speed === 40, 'map point: other detail telemetry must still apply');

const replaced = mergeDetailTelemetry(libraryTelemetry, { latitude: -33.8, longitude: 151.2, speed: 8 });
assert(
  replaced.latitude === -33.8 && replaced.longitude === 151.2,
  'map point: valid detail point must replace library point'
);

const keptMissing = mergeDetailTelemetry(libraryTelemetry, { speed: 3 });
assert(
  keptMissing.latitude === -37.8 && keptMissing.longitude === 144.9,
  'map point: detail without coords must keep library point'
);

console.log('library-adapter-check: ok');
