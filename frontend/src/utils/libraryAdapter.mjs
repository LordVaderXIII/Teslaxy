export const MAX_LIBRARY_FILE_IDS = 64;

/**
 * Geographic point usable as a map marker. Rejects non-numbers, non-finite, and (0,0).
 * @param {unknown} lat
 * @param {unknown} lon
 * @returns {boolean}
 */
function isValidMapPoint(lat, lon) {
  if (typeof lat !== 'number' || typeof lon !== 'number') return false;
  if (!Number.isFinite(lat) || !Number.isFinite(lon)) return false;
  if (lat === 0 && lon === 0) return false;
  return true;
}

/**
 * MapModal-compatible plot filter: both coords numeric and neither is 0.
 * @param {{ telemetry?: { latitude?: number, longitude?: number } }} clip
 * @returns {[number, number] | null}
 */
export function clipMapPoint(clip) {
  const lat = clip?.telemetry?.latitude;
  const lng = clip?.telemetry?.longitude;
  if (typeof lat === 'number' && typeof lng === 'number' && lat !== 0 && lng !== 0) {
    return [lat, lng];
  }
  return null;
}

/**
 * Map library JSON (snake_case `id`) onto the internal Clip shape (`ID`).
 * Does not stuff preview_path into video_files.
 * @param {Record<string, unknown>} event
 * @returns {Record<string, unknown>}
 */
export function libraryEventToClip(event) {
  const clip = {
    ID: event.id,
    timestamp: event.timestamp,
    event: event.event,
    city: event.city,
    reason: event.reason,
    source_dir: event.source_dir,
    video_file_count: event.video_file_count,
    preview_camera: event.preview_camera,
    preview_path: event.preview_path,
    preview_timestamp: event.preview_timestamp || undefined,
    preview_seek_seconds: event.preview_seek_seconds,
  };

  if (event.event_timestamp) {
    clip.event_timestamp = event.event_timestamp;
  }

  if (isValidMapPoint(event.latitude, event.longitude)) {
    clip.telemetry = {
      latitude: event.latitude,
      longitude: event.longitude,
    };
  }

  return clip;
}

/**
 * Detail telemetry replaces the library map point only when the detail point is valid.
 * @param {Record<string, unknown> | undefined} libraryTelemetry
 * @param {Record<string, unknown> | undefined} detailTelemetry
 * @returns {Record<string, unknown> | undefined}
 */
export function mergeDetailTelemetry(libraryTelemetry, detailTelemetry) {
  const detail =
    detailTelemetry && typeof detailTelemetry === 'object' ? { ...detailTelemetry } : {};
  const libLat = libraryTelemetry?.latitude;
  const libLon = libraryTelemetry?.longitude;

  if (isValidMapPoint(detail.latitude, detail.longitude)) {
    return detail;
  }
  if (isValidMapPoint(libLat, libLon)) {
    return { ...detail, latitude: libLat, longitude: libLon };
  }
  if (detailTelemetry && typeof detailTelemetry === 'object') {
    return detail;
  }
  return libraryTelemetry;
}

/**
 * @param {Array<{ camera?: string, file_path?: string, timestamp?: string }>} files
 * @returns {Array<{ camera: string, file_path: string, timestamp: string }>}
 */
export function videoFilesFromLibrary(files) {
  if (!Array.isArray(files)) return [];
  return files.map((f) => ({
    camera: f.camera,
    file_path: f.file_path,
    timestamp: f.timestamp,
  }));
}

/**
 * Deduped member ids for GET /api/library/files. Representative ID if none stored.
 * @param {{ ID?: number, member_ids?: number[] }} clip
 * @returns {number[]}
 */
export function uniqueMemberIds(clip) {
  const raw =
    Array.isArray(clip?.member_ids) && clip.member_ids.length > 0
      ? clip.member_ids
      : [clip?.ID];
  const seen = new Set();
  const ids = [];
  for (const value of raw) {
    const id = Number(value);
    if (!Number.isInteger(id) || seen.has(id)) continue;
    seen.add(id);
    ids.push(id);
  }
  return ids;
}

/**
 * Repeated `id` query params for /api/library/files.
 * @param {number[]} ids
 * @returns {string}
 */
export function libraryFilesQuery(ids) {
  const params = new URLSearchParams();
  for (const id of ids) {
    params.append('id', String(id));
  }
  return params.toString();
}
