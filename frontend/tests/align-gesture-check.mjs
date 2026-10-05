/**
 * A sideways swipe on the nudge row must not count as a press, and an
 * alignment export must not revoke its blob URL in the same turn as the click.
 */
import {
  EXPORT_URL_REVOKE_MS,
  NUDGE_MOVE_SLOP_PX,
  classifyNudgeMove,
  revokeDownloadLater,
} from '../src/utils/nudgeGesture.mjs';

function assert(condition, message) {
  if (!condition) {
    console.error(`FAIL: ${message}`);
    process.exit(1);
  }
}

assert(classifyNudgeMove(0, 0) === 'pending', 'a still press is not a scroll');
assert(classifyNudgeMove(NUDGE_MOVE_SLOP_PX, 0) === 'pending', 'movement inside the slop stays a press');
assert(classifyNudgeMove(NUDGE_MOVE_SLOP_PX + 1, 0) === 'scroll', 'a sideways swipe is a scroll');
assert(classifyNudgeMove(0, NUDGE_MOVE_SLOP_PX + 4) === 'scroll', 'a vertical swipe is a scroll');
assert(classifyNudgeMove(-30, 2) === 'scroll', 'a leftward swipe toward yaw is a scroll');

let removed = false;
let revoked = '';
const queued = [];
revokeDownloadLater('blob:align', () => {
  removed = true;
}, {
  schedule: (fn, ms) => {
    queued.push({ fn, ms });
    return 0;
  },
  revoke: (url) => {
    revoked = url;
  },
});

assert(removed === false, 'the download link stays until the timer');
assert(revoked === '', 'the blob URL is not revoked in the click turn');
assert(queued.length === 1 && queued[0].ms === EXPORT_URL_REVOKE_MS, 'revoke waits for Safari to start the download');
assert(EXPORT_URL_REVOKE_MS >= 1000, 'the wait is long enough for an async download');
queued[0].fn();
assert(removed === true && revoked === 'blob:align', 'the timer removes the link and revokes the URL');

console.log('align gesture: scroll does not nudge, export revoke is deferred');
