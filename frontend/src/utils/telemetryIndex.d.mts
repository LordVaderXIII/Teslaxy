export function telemetryIndex(
  samples: Array<{ frame_seq_no?: number }>,
  currentTime: number,
  duration: number
): { index: number; approximate: boolean };
