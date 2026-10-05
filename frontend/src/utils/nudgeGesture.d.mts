export const NUDGE_MOVE_SLOP_PX: number;
export const NUDGE_HOLD_DELAY_MS: number;
export const NUDGE_REPEAT_MS: number;
export const EXPORT_URL_REVOKE_MS: number;

export function classifyNudgeMove(dx: number, dy: number, slop?: number): 'scroll' | 'pending';

export function revokeDownloadLater(
  url: string,
  removeLink: () => void,
  options?: {
    delay?: number;
    schedule?: (fn: () => void, ms: number) => unknown;
    revoke?: (url: string) => void;
  },
): void;
