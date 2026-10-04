import { isBrakeApplied, pedalState } from '../src/utils/pedalState.mjs';

function assert(condition, message) {
  if (!condition) {
    console.error(`FAIL: ${message}`);
    process.exit(1);
  }
}

// Tesla SEI sample: IEEE-754 0x4179999A is 15.60 percent, not a 0–1 fraction.
const tesla = pedalState({
  accelerator_pedal_position: 15.6,
  brake_applied: false,
});
assert(Math.abs(tesla.accelerator - 0.156) < 1e-6, `15.60 must be 15.6% of the bar, got ${tesla.accelerator}`);
assert(tesla.brakeApplied === false, 'explicit false brake must stay released');

const pressed = pedalState({
  accelerator_pedal_position: 72,
  brake_applied: true,
});
assert(Math.abs(pressed.accelerator - 0.72) < 1e-6, `72 must fill 72% of the bar, got ${pressed.accelerator}`);
assert(pressed.brakeApplied === true, 'brake_applied true must apply the brake graphic');

const full = pedalState({ accelerator_pedal_position: 140, brake_applied: 1 });
assert(full.accelerator === 1, 'pedal past 100% clamps to a full bar');
assert(full.brakeApplied === true, 'brake varint 1 must apply the brake');

const camel = pedalState({ acceleratorPedalPosition: 40, brakeApplied: 'true' });
assert(Math.abs(camel.accelerator - 0.4) < 1e-6, 'camelCase proto json name must apply');
assert(camel.brakeApplied === true, 'string "true" must apply the brake');

const idle = pedalState({});
assert(idle.accelerator === 0, 'omitted pedal (protobuf omitempty) is 0');
assert(idle.brakeApplied === false, 'omitted brake is released');

assert(isBrakeApplied(false) === false, 'false is released');
assert(isBrakeApplied('0') === false, 'string 0 is released');
assert(pedalState({ accelerator_pedal_position: -4 }).accelerator === 0, 'negative pedal clamps to 0');

console.log('pedal-state-check: ok');
