export function pedalState(point: object | null | undefined): {
  accelerator: number;
  brakeApplied: boolean;
};

export function isBrakeApplied(value: unknown): boolean;
