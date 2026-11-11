export interface TelemetryFileTiming {
  file_path: string;
  first_sample_index: number;
  sample_count: number;
  sample_times?: number[];
  frame_duration?: number;
}

export function telemetryIndex(
  samples: Array<{ frame_seq_no?: number }>,
  currentTime: number,
  duration: number,
  anchor?: { filePath: string; localTime: number; files: TelemetryFileTiming[] }
): { index: number; approximate: boolean };
