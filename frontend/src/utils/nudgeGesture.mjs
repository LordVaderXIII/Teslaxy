/** Pixels of pointer travel before a nudge press is a scroll, not a hold. */
export const NUDGE_MOVE_SLOP_PX = 8;

/** Stillness required before the first repeat. A tap fires on click instead. */
export const NUDGE_HOLD_DELAY_MS = 250;

export const NUDGE_REPEAT_MS = 90;

/**
 * Safari and iOS start <a download> after the click turn. Revoking the blob
 * URL in that same turn drops the file.
 */
export const EXPORT_URL_REVOKE_MS = 1500;

/**
 * @param {number} dx
 * @param {number} dy
 * @returns {'scroll' | 'pending'}
 */
export function classifyNudgeMove(dx, dy, slop = NUDGE_MOVE_SLOP_PX) {
  if (Math.hypot(dx, dy) > slop) return 'scroll';
  return 'pending';
}

/**
 * Keep the anchor and the blob URL until `delay` ms after click.
 * @param {string} url
 * @param {() => void} removeLink
 * @param {{ delay?: number, schedule?: (fn: () => void, ms: number) => unknown, revoke?: (url: string) => void }} [options]
 */
export function revokeDownloadLater(url, removeLink, options = {}) {
  const delay = options.delay ?? EXPORT_URL_REVOKE_MS;
  const schedule = options.schedule ?? ((fn, ms) => setTimeout(fn, ms));
  const revoke = options.revoke ?? ((objectUrl) => URL.revokeObjectURL(objectUrl));
  schedule(() => {
    removeLink();
    revoke(url);
  }, delay);
}
