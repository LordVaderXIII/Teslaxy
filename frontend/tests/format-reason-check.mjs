import { FormatReason } from '../src/utils/formatReason.mjs';

function assert(condition, message) {
  if (!condition) {
    console.error(`FAIL: ${message}`);
    process.exit(1);
  }
}

assert(FormatReason.label('sentry_aware_object_detection') === 'Object detected', 'object detection');
assert(FormatReason.label('sentry_aware_accel_x') === 'Bump detected', 'accel prefix');
assert(FormatReason.label('sentry_aware_accel_impact') === 'Bump detected', 'accel impact');
assert(FormatReason.label('user_interaction_honk') === 'Horn save', 'honk');
assert(FormatReason.label('user_interaction_dashcam_panel_save') === 'Saved manually', 'panel save');
assert(FormatReason.label('user_interaction_dashcam_icon_tapped') === 'Saved manually', 'icon save');
assert(FormatReason.label('user_interaction_dashcam_something_new') === 'Saved manually', 'dashcam prefix');
assert(FormatReason.label('', 'Sentry') === 'Sentry', 'event fallback');
assert(FormatReason.label('sentry_aware_weird_case') === 'Sentry Weird Case', 'sentry tidy fallback');
assert(FormatReason.raw('sentry_aware_object_detection') === 'sentry_aware_object_detection', 'raw preserved');
assert(FormatReason.label(null, 'Recent') === 'Recent', 'null reason uses event');

console.log('format-reason-check: ok');
