/** Playback timeline, speed, and multi-camera drift helpers. */

export const DEFAULT_SEGMENT_SECONDS = 60;
export const SYNC_DRIFT_SECONDS = 0.3;
/** A seek counts as landed once media time is within this of the requested offset. */
export const SEEK_LANDED_SECONDS = 0.5;
/** HTMLMediaElement.HAVE_CURRENT_DATA. Below this, a paused seek never finishes. */
export const HAVE_CURRENT_DATA = 2;

const MIN_SEGMENT_SECONDS = 1;
const MAX_SEGMENT_SECONDS = 120;

export function normalizeCameraName(name) {
  return String(name || '').toLowerCase().replace(/[^a-z0-9]/g, '');
}

function clampDuration(seconds) {
  if (!Number.isFinite(seconds)) return DEFAULT_SEGMENT_SECONDS;
  return Math.min(MAX_SEGMENT_SECONDS, Math.max(MIN_SEGMENT_SECONDS, seconds));
}

/**
 * One segment per camera + path + timestamp. A merged event can list the same
 * file twice (two member clip ids). Treating that pair as back-to-back minutes
 * turns a short file into a ~61s timeline (1s clamped gap + 60s default).
 *
 * @param {Array<{ camera?: string, file_path?: string, timestamp?: string }> | undefined} videoFiles
 * @param {Record<string, number>} [mediaDurations] probed HTMLMediaElement durations keyed by file_path
 */
export function buildCameraSegments(videoFiles, mediaDurations = {}) {
  const grouped = {};
  const seen = new Set();
  const files = Array.isArray(videoFiles) ? videoFiles : [];

  for (const file of files) {
    const cam = normalizeCameraName(file?.camera);
    if (!cam) continue;
    const tsMs = Date.parse(file.timestamp);
    if (!Number.isFinite(tsMs)) continue;
    const filePath = file.file_path || '';
    const key = `${cam}\0${filePath}\0${tsMs}`;
    if (seen.has(key)) continue;
    seen.add(key);
    if (!grouped[cam]) grouped[cam] = [];
    grouped[cam].push({
      file_path: filePath,
      timestamp: tsMs / 1000,
      startTime: 0,
      duration: DEFAULT_SEGMENT_SECONDS,
    });
  }

  for (const cam of Object.keys(grouped)) {
    const camSegments = grouped[cam];
    camSegments.sort((a, b) => a.timestamp - b.timestamp || a.file_path.localeCompare(b.file_path));
    let accumulated = 0;
    for (let i = 0; i < camSegments.length; i++) {
      const seg = camSegments[i];
      const next = camSegments[i + 1];
      seg.startTime = accumulated;
      seg.duration = durationForSegment(seg, next, mediaDurations);
      accumulated += seg.duration;
    }
  }

  return grouped;
}

function durationForSegment(seg, next, mediaDurations) {
  const probed = mediaDurations?.[seg.file_path];
  const hasProbe = typeof probed === 'number' && Number.isFinite(probed) && probed > 0;
  if (!next) {
    return hasProbe ? clampDuration(probed) : DEFAULT_SEGMENT_SECONDS;
  }
  const raw = next.timestamp - seg.timestamp;
  if (!Number.isFinite(raw)) {
    return hasProbe ? clampDuration(probed) : DEFAULT_SEGMENT_SECONDS;
  }
  return clampDuration(raw);
}

/** Front camera timeline, else the first camera that has segments. */
export function timelineDurationSeconds(segments) {
  if (!segments) return 0;
  const cams = Object.keys(segments);
  if (cams.length === 0) return 0;
  const main = segments.front || segments[cams[0]];
  if (!main || main.length === 0) return 0;
  const last = main[main.length - 1];
  return last.startTime + last.duration;
}

export function findSegmentAtTime(camSegments, time) {
  if (!camSegments || camSegments.length === 0) return null;
  let idx = camSegments.findIndex((s) => s.startTime > time);
  if (idx === -1) idx = camSegments.length;
  const index = Math.max(0, idx - 1);
  const segment = camSegments[index];
  if (!segment) return null;
  return { segment, index };
}

export function isControllablePlayer(player) {
  if (!player) return false;
  if (typeof player.isDisposed === 'function') {
    try {
      if (player.isDisposed()) return false;
    } catch {
      return false;
    }
  }
  return true;
}

/**
 * Set playbackRate without touching a disposed video.js tech.
 * After dispose, Html5 sets el_ to null and `el_.playbackRate = rate` throws
 * "Cannot set properties of null (setting 'playbackRate')", which blanks React.
 * @returns {boolean} false when the player is gone or the element is null
 */
export function assignPlaybackRate(player, rate) {
  if (!isControllablePlayer(player)) return false;
  if (typeof player.playbackRate !== 'function') return false;
  if (!Number.isFinite(rate) || rate <= 0) return false;
  try {
    player.playbackRate(rate);
    return true;
  } catch {
    return false;
  }
}

export function elementIsSeeking(player) {
  if (!player || typeof player.seeking !== 'function') return false;
  try {
    return Boolean(player.seeking());
  } catch {
    return false;
  }
}

function elementReadyState(player) {
  if (!player || typeof player.readyState !== 'function') return null;
  try {
    const state = player.readyState();
    return Number.isFinite(state) ? state : null;
  } catch {
    return null;
  }
}

function elementIsPaused(player) {
  if (!player || typeof player.paused !== 'function') return false;
  try {
    return Boolean(player.paused());
  } catch {
    return false;
  }
}

/**
 * True when another currentTime() would restart a seek that has not finished.
 * On a paused element that restart sticks at readyState HAVE_METADATA with
 * seeking left true, and play() never begins decoding.
 * A paused element that only has metadata must wait until it has a frame.
 */
export function mediaSeekBlocked(player) {
  if (!isControllablePlayer(player)) return true;
  if (elementIsSeeking(player)) return true;
  if (!elementIsPaused(player)) return false;
  const ready = elementReadyState(player);
  return ready != null && ready < HAVE_CURRENT_DATA;
}

/** Whether this transport should assign currentTime. Rate changes are separate. */
export function shouldIssueMediaSeek(player, targetLocal) {
  if (!Number.isFinite(targetLocal) || mediaSeekBlocked(player)) return false;
  let now = Number.NaN;
  try {
    now = player.currentTime();
  } catch {
    return false;
  }
  if (Number.isFinite(now) && Math.abs(now - targetLocal) <= SEEK_LANDED_SECONDS) return false;
  return true;
}

/**
 * Apply the current transport to a player that just mounted or loaded metadata.
 * New video elements default to playbackRate 1; a segment change must not leave
 * that default in place while the speed control still shows the previous rate.
 * A paused seek is issued once. Repeating currentTime() while seeking is true
 * leaves every camera stuck and Play does not resume decoding.
 */
export function applyPlayerTransport(player, transport) {
  if (!isControllablePlayer(player)) return false;
  const rate = transport?.playbackRate;
  if (typeof rate === 'number' && !assignPlaybackRate(player, rate)) {
    return false;
  }
  const localTime = transport?.localTime;
  if (typeof localTime === 'number' && Number.isFinite(localTime) && typeof player.currentTime === 'function') {
    try {
      if (shouldIssueMediaSeek(player, localTime)) {
        player.currentTime(Math.max(0, localTime));
      }
    } catch {
      return false;
    }
  }
  if (transport?.play && typeof player.play === 'function') {
    try {
      const pending = player.play();
      if (pending && typeof pending.catch === 'function') pending.catch(() => {});
    } catch {
      /* autoplay rejection is non-fatal */
    }
  }
  return true;
}

/**
 * Offset inside a segment for a global timeline time.
 * A time exactly at the segment end belongs to the next segment.
 * @returns {number | null}
 */
export function localMediaTime(segmentStart, segmentDuration, globalTime) {
  if (!Number.isFinite(segmentStart) || !Number.isFinite(segmentDuration) || !Number.isFinite(globalTime)) {
    return null;
  }
  if (segmentDuration <= 0) return null;
  if (globalTime < segmentStart || globalTime >= segmentStart + segmentDuration) return null;
  return globalTime - segmentStart;
}

/**
 * Decide what a media timeupdate is allowed to do while a seek is outstanding.
 * Publishing start+0 before the new file has seeked is what drops 02:10 and
 * 02:20 onto the start of the loaded minute.
 *
 * @param {{ segmentStart: number, segmentDuration: number, mediaTime: number, pendingGlobal: number | null, seeking?: boolean }} input
 * @returns {{ publishGlobal: number | null, retryLocal: number | null, clearPending: boolean }}
 */
export function resolveMediaClock(input) {
  const segmentStart = input?.segmentStart;
  const segmentDuration = input?.segmentDuration;
  const mediaTime = input?.mediaTime;
  const pendingGlobal = input?.pendingGlobal;
  const idle = { publishGlobal: null, retryLocal: null, clearPending: false };

  if (pendingGlobal == null || !Number.isFinite(pendingGlobal)) {
    if (Number.isFinite(segmentStart) && Number.isFinite(mediaTime)) {
      return { publishGlobal: segmentStart + mediaTime, retryLocal: null, clearPending: false };
    }
    return idle;
  }

  // seeking stays true until the element finishes. Another currentTime() here
  // aborts that seek. Do not publish segment-start + 0 in the meantime.
  if (input.seeking) return idle;

  const local = localMediaTime(segmentStart, segmentDuration, pendingGlobal);
  if (local == null) {
    // The mounted file is not the segment the seek asked for. Leave it alone
    // and do not publish its clock over the requested time.
    return idle;
  }

  if (Number.isFinite(mediaTime) && Math.abs(mediaTime - local) <= SEEK_LANDED_SECONDS) {
    return { publishGlobal: segmentStart + mediaTime, retryLocal: null, clearPending: true };
  }

  return { publishGlobal: null, retryLocal: local, clearPending: false };
}

export function shouldCorrectDrift(driftSeconds, threshold = SYNC_DRIFT_SECONDS) {
  return Number.isFinite(driftSeconds) && Math.abs(driftSeconds) > threshold;
}

/**
 * Peers whose media time is outside the drift window of the master clock.
 * A constant multi-second gap (late-starting cameras) is a miss, not a rate error.
 * @param {number} masterTime
 * @param {Array<{ id: string, time: number }>} peers
 */
export function peerSeekTargets(masterTime, peers, threshold = SYNC_DRIFT_SECONDS) {
  const targets = [];
  if (!Number.isFinite(masterTime) || !Array.isArray(peers)) return targets;
  for (const peer of peers) {
    if (!peer || typeof peer.time !== 'number') continue;
    if (!shouldCorrectDrift(peer.time - masterTime, threshold)) continue;
    targets.push({ id: peer.id, time: masterTime });
  }
  return targets;
}
