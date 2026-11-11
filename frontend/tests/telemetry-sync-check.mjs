import assert from 'node:assert/strict';
import { telemetryIndex } from '../src/utils/telemetryIndex.mjs';

// Shape measured on real four-minute Recent footage: 2156, 2157, 1201,
// then zero SEI samples despite ~2160 video frames per file. Values are synthetic.
const counts = [2156, 2157, 1201, 0];
const samples = [];
const files = counts.map((count, k) => {
  const first = samples.length;
  for (let i = 0; i < count; i++) samples.push({ frame_seq_no: k * 3000 + i });
  return { file_path: `minute-${k}`, first_sample_index: first, sample_count: count,
    sample_times: Array.from({ length: count }, (_, i) => i / 36), frame_duration: 1 / 36 };
});
const pick = (k, t) => telemetryIndex(samples, k * 60 + t, 241.3318,
  { filePath: `minute-${k}`, localTime: t, files });
assert.equal(pick(0, 449 / 36).index, 449, 'first-file brake must use its own media time');
assert.equal(pick(1, 1739 / 36).index, 2156 + 1739, 'second-file brake must not stretch across later missing SEI');
assert.equal(pick(2, 599 / 36).index, 2156 + 2157 + 599, 'partial SEI must keep packet timing, not stretch to file duration');
assert.equal(pick(2, 50).index, -1, 'hide HUD beyond partial SEI coverage');
assert.equal(pick(3, 10).index, -1, 'zero-SEI file must not borrow another file sample');
assert.equal(pick(1, 1739 / 36).approximate, false);
assert.equal(telemetryIndex(samples, 0, 241, { filePath: 'missing', localTime: 0, files }).index, -1);
assert.equal(telemetryIndex(samples, 100, 241).approximate, true, 'legacy arrays remain explicitly approximate');
const gapFiles = [{ file_path: 'gap', first_sample_index: 0, sample_count: 3,
  sample_times: [0, 1 / 36, 1], frame_duration: 1 / 36 }];
assert.equal(telemetryIndex(samples, 0.5, 1, { filePath: 'gap', localTime: 0.5, files: gapFiles }).index, -1);
console.log('telemetry-sync-check: ok');
