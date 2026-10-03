/**
 * @param {Array<{ frame_seq_no?: number }>} samples
 * @param {number} currentTime
 * @param {number} duration
 * @returns {{ index: number, approximate: boolean }}
 */
export function telemetryIndex(samples, currentTime, duration) {
  const length = samples?.length ?? 0;
  if (length === 0) {
    return { index: 0, approximate: false };
  }

  const proportionalIndex = () => {
    let index = duration > 0 ? Math.floor((currentTime / duration) * length) : 0;
    if (index < 0) index = 0;
    if (index >= length) index = length - 1;
    return index;
  };

  let seqOk = true;
  const seqs = new Array(length);
  for (let i = 0; i < length; i++) {
    const seq = samples[i]?.frame_seq_no;
    if (typeof seq !== 'number' || !Number.isFinite(seq)) {
      seqOk = false;
      break;
    }
    if (i > 0 && seq < seqs[i - 1]) {
      seqOk = false;
      break;
    }
    seqs[i] = seq;
  }

  const first = seqs[0];
  const last = seqs[length - 1];
  if (!seqOk || !(last > first)) {
    return { index: proportionalIndex(), approximate: true };
  }

  // Monotonic frame_seq_no is only an index heuristic, not video-time correspondence.
  const ratio = duration > 0 ? currentTime / duration : 0;
  const target = first + ratio * (last - first);
  return { index: nearestIndexBySeq(seqs, target), approximate: true };
}

/**
 * Binary-search the sample whose frame_seq_no is nearest to target.
 * @param {number[]} seqs
 * @param {number} target
 * @returns {number}
 */
function nearestIndexBySeq(seqs, target) {
  let lo = 0;
  let hi = seqs.length - 1;
  while (lo < hi) {
    const mid = (lo + hi) >> 1;
    if (seqs[mid] < target) lo = mid + 1;
    else hi = mid;
  }

  if (lo > 0) {
    const prev = lo - 1;
    if (Math.abs(seqs[prev] - target) <= Math.abs(seqs[lo] - target)) {
      return prev;
    }
  }
  return lo;
}
