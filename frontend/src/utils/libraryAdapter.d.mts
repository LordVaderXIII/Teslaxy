import type { Clip, VideoFile } from './clipMerge.mjs';

export const MAX_LIBRARY_FILE_IDS: 64;

export function clipMapPoint(
  clip: { telemetry?: { latitude?: number; longitude?: number } }
): [number, number] | null;

export interface LibraryEvent {
  id: number;
  timestamp: string;
  event_timestamp?: string | null;
  event: string;
  city: string;
  reason?: string;
  source_dir?: string;
  latitude?: number | null;
  longitude?: number | null;
  video_file_count?: number;
  preview_camera?: string;
  preview_path?: string;
  preview_timestamp?: string | null;
  preview_seek_seconds?: number;
}

export function libraryEventToClip(event: LibraryEvent): Clip;

export function mergeDetailTelemetry(
  libraryTelemetry: Clip['telemetry'],
  detailTelemetry: Clip['telemetry']
): Clip['telemetry'];

export function videoFilesFromLibrary(
  files: Array<{ camera?: string; file_path?: string; timestamp?: string }>
): VideoFile[];

export function uniqueMemberIds(clip: Pick<Clip, 'ID' | 'member_ids'>): number[];

export function libraryFilesQuery(ids: number[]): string;
