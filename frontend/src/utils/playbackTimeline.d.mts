export const DEFAULT_SEGMENT_SECONDS: 60;
export const SYNC_DRIFT_SECONDS: 0.3;
export const SEEK_LANDED_SECONDS: 0.5;
export const HAVE_CURRENT_DATA: 2;

export interface MediaClockDecision {
  publishGlobal: number | null;
  retryLocal: number | null;
  clearPending: boolean;
}

export interface CameraSegment {
  file_path: string;
  timestamp: number;
  startTime: number;
  duration: number;
}

export interface PlaybackPlayer {
  isDisposed?: () => boolean;
  playbackRate?: (rate?: number) => number;
  currentTime?: (time?: number) => number;
  play?: () => Promise<void> | void;
  pause?: () => void;
  paused?: () => boolean;
  seeking?: () => boolean;
  readyState?: () => number;
  duration?: () => number;
  currentSrc?: () => string;
}

export interface PlayerTransport {
  playbackRate?: number;
  localTime?: number;
  play?: boolean;
}

export function normalizeCameraName(name: string): string;

export function buildCameraSegments(
  videoFiles: Array<{ camera?: string; file_path?: string; timestamp?: string }> | undefined,
  mediaDurations?: Record<string, number>
): Record<string, CameraSegment[]>;

export function timelineDurationSeconds(segments: Record<string, CameraSegment[]> | null | undefined): number;

export function findSegmentAtTime(
  camSegments: CameraSegment[] | undefined,
  time: number
): { segment: CameraSegment; index: number } | null;

export function isControllablePlayer(player: PlaybackPlayer | null | undefined): boolean;

export function assignPlaybackRate(player: PlaybackPlayer | null | undefined, rate: number): boolean;

export function elementIsSeeking(player: PlaybackPlayer | null | undefined): boolean;

export function mediaSeekBlocked(player: PlaybackPlayer | null | undefined): boolean;

export function shouldIssueMediaSeek(player: PlaybackPlayer | null | undefined, targetLocal: number): boolean;

export function applyPlayerTransport(
  player: PlaybackPlayer | null | undefined,
  transport: PlayerTransport
): boolean;

export function localMediaTime(
  segmentStart: number,
  segmentDuration: number,
  globalTime: number
): number | null;

export function resolveMediaClock(input: {
  segmentStart: number;
  segmentDuration: number;
  mediaTime: number;
  pendingGlobal: number | null;
  seeking?: boolean;
}): MediaClockDecision;

export function shouldCorrectDrift(driftSeconds: number, threshold?: number): boolean;

export function peerSeekTargets(
  masterTime: number,
  peers: Array<{ id: string; time: number }>,
  threshold?: number
): Array<{ id: string; time: number }>;
