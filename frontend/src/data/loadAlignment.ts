import raw from './camera-alignment.json';
import { parseAlignment, type CameraAlignment } from '../utils/cameraAlignment.mjs';

const parsed = parseAlignment(raw);

export const committedAlignment: CameraAlignment | null = parsed.ok ? parsed.alignment : null;
export const committedAlignmentError: string | null = parsed.ok ? null : parsed.message;
