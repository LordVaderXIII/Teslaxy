import { shouldCommitDetail } from '../src/utils/detailCommit.mjs';
import { telemetryIndex } from '../src/utils/telemetryIndex.mjs';

function assert(condition, message) {
  if (!condition) {
    console.error(`FAIL: ${message}`);
    process.exit(1);
  }
}

// 1. Monotonic seq is an index heuristic, not video-time correspondence.
// t=100 / duration=1000 interpolates to seq 100, nearest sample is 20.
const samples = [
  { frame_seq_no: 0 },
  { frame_seq_no: 10 },
  { frame_seq_no: 20 },
  { frame_seq_no: 1000 },
];
const seqResult = telemetryIndex(samples, 100, 1000);
assert(samples[seqResult.index].frame_seq_no === 20, `expected seq 20, got ${samples[seqResult.index]?.frame_seq_no}`);
assert(seqResult.index !== Math.floor((100 / 1000) * samples.length), 'must not use floor((t/duration)*length)=0');
assert(seqResult.approximate === true, 'monotonic seq without a verified video-time anchor must still be approximate');

// 2. Decreasing seq falls back to proportional index and approximate=true.
const decreasing = [
  { frame_seq_no: 30 },
  { frame_seq_no: 20 },
  { frame_seq_no: 10 },
  { frame_seq_no: 0 },
];
const decResult = telemetryIndex(decreasing, 100, 1000);
const proportional = Math.floor((100 / 1000) * decreasing.length);
assert(decResult.approximate === true, 'decreasing seq must set approximate=true');
assert(decResult.index === proportional, `decreasing seq must use proportional index ${proportional}, got ${decResult.index}`);

// 3. Missing seq falls back and approximate=true.
const missing = [
  { frame_seq_no: 0 },
  {},
  { frame_seq_no: 20 },
  { frame_seq_no: 1000 },
];
const missResult = telemetryIndex(missing, 100, 1000);
assert(missResult.approximate === true, 'missing seq must set approximate=true');
assert(missResult.index === Math.floor((100 / 1000) * missing.length), 'missing seq must use proportional index');

// 4. Detail commit generation guard.
assert(shouldCommitDetail(1, 1) === true, 'shouldCommitDetail(1, 1) must be true');
assert(shouldCommitDetail(1, 2) === false, 'shouldCommitDetail(1, 2) must be false');
assert(shouldCommitDetail(null, 1) === false, 'shouldCommitDetail(null, 1) must be false');

console.log('telemetry-index-check: ok');
