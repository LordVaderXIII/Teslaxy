export interface VideoFile {
  camera: string;
  file_path: string;
  timestamp: string;
}

export interface Clip {
  ID: number;
  timestamp: string;
  event: string;
  city: string;
  reason?: string;
  video_files?: VideoFile[];
  telemetry?: Record<string, unknown>;
  event_timestamp?: string;
  start_time?: Date;
  date_key?: string;
  source_dir?: string;
  video_file_count?: number;
  member_ids?: number[];
  preview_camera?: string;
  preview_path?: string;
  preview_timestamp?: string;
  preview_seek_seconds?: number;
}

export function mergeClips(rawClips: Clip[]): Clip[];
