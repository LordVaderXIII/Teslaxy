/**
 * Brake and accelerator on the driving HUD are Tesla SEI samples.
 *
 * There is no touch, pointer, keyboard, or gamepad pedal handler.
 * Player.tsx keyboard shortcuts only play and seek. The values that
 * move the graphics are:
 *
 *   ExtractSEI → ScannerService.aggregateTelemetry
 *   → json.Marshal([]*SeiMetadata) stored on telemetry.full_data_json
 *   → Player passes that string to TelemetryOverlay
 *   → pedalState() below
 *
 * encoding/json writes the Go struct tags (snake_case). A protojson
 * payload would use the protobuf json names (camelCase). Both are read.
 *
 * accelerator_pedal_position is a percent of full pedal travel.
 * Tesla's published SEI sample 0x4179999A decodes to 15.60, i.e. 15.6%.
 * The bar width is that percent. brake_applied is the protobuf bool.
 *
 * @param {Record<string, unknown> | null | undefined} point
 * @returns {{ accelerator: number, brakeApplied: boolean }}
 */
export function pedalState(point) {
  const accelerator = clampUnit(readNumber(point, [
    'accelerator_pedal_position',
    'acceleratorPedalPosition',
  ]) / 100);
  return {
    accelerator,
    brakeApplied: isBrakeApplied(readField(point, ['brake_applied', 'brakeApplied'])),
  };
}

/**
 * @param {Record<string, unknown> | null | undefined} point
 * @param {string[]} keys
 * @returns {unknown}
 */
function readField(point, keys) {
  if (!point || typeof point !== 'object') return undefined;
  for (const key of keys) {
    if (Object.prototype.hasOwnProperty.call(point, key)) return point[key];
  }
  return undefined;
}

/**
 * @param {Record<string, unknown> | null | undefined} point
 * @param {string[]} keys
 * @returns {number}
 */
function readNumber(point, keys) {
  const value = readField(point, keys);
  const number = typeof value === 'string' ? Number(value) : value;
  if (typeof number !== 'number' || !Number.isFinite(number)) return 0;
  return number;
}

/**
 * Protobuf bool, plus 0/1 seen when a JSON writer emits the varint as a number.
 * @param {unknown} value
 * @returns {boolean}
 */
export function isBrakeApplied(value) {
  if (value === true || value === 1) return true;
  if (typeof value === 'string') {
    const text = value.trim().toLowerCase();
    return text === 'true' || text === '1';
  }
  return false;
}

/** @param {number} value */
function clampUnit(value) {
  if (!Number.isFinite(value) || value <= 0) return 0;
  if (value >= 1) return 1;
  return value;
}
