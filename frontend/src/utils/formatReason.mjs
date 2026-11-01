/**
 * Short Library labels for Tesla event.reason strings.
 * Class with static methods (not loose helpers) so call sites stay consistent.
 */
export class FormatReason {
  static SHORT = Object.freeze({
    sentry_aware_object_detection: 'Object detected',
    user_interaction_honk: 'Horn save',
    user_interaction_dashcam_panel_save: 'Saved manually',
    user_interaction_dashcam_icon_tapped: 'Saved manually',
  });

  /**
   * @param {string | null | undefined} reason
   * @param {string | null | undefined} [eventFallback]
   * @returns {string} raw reason used for title / aria-label
   */
  static raw(reason, eventFallback) {
    const value = (reason && String(reason).trim()) || (eventFallback && String(eventFallback).trim()) || '';
    return value;
  }

  /**
   * @param {string | null | undefined} reason
   * @param {string | null | undefined} [eventFallback]
   * @returns {string} compact UI label
   */
  static label(reason, eventFallback) {
    const raw = FormatReason.raw(reason, eventFallback);
    if (!raw) return '';

    const key = raw.toLowerCase();
    if (Object.prototype.hasOwnProperty.call(FormatReason.SHORT, key)) {
      return FormatReason.SHORT[key];
    }
    if (key.startsWith('sentry_aware_accel_')) {
      return 'Bump detected';
    }
    if (key.startsWith('user_interaction_dashcam_')) {
      return 'Saved manually';
    }
    if (key.startsWith('sentry_aware_')) {
      return FormatReason.tidy(raw.replace(/^sentry_aware_/i, 'Sentry '));
    }
    return FormatReason.tidy(raw);
  }

  /**
   * @param {string} raw
   * @returns {string}
   */
  static tidy(raw) {
    return String(raw)
      .replaceAll('_', ' ')
      .replace(/\s+/g, ' ')
      .trim()
      .replace(/\b\w/g, (c) => c.toUpperCase());
  }
}
