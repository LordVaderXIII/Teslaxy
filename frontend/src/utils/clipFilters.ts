import type { Clip } from './clipMerge';

export interface ClipFilterState {
  recent: boolean;
  dashcamHonk: boolean;
  dashcamSaved: boolean;
  dashcamOther: boolean;
  sentryObject: boolean;
  sentryAccel: boolean;
  sentryOther: boolean;
}

export const DEFAULT_CLIP_FILTERS: ClipFilterState = {
  recent: true,
  dashcamHonk: true,
  dashcamSaved: true,
  dashcamOther: true,
  sentryObject: true,
  sentryAccel: true,
  sentryOther: true,
};

export function clipDateKey(clip: Clip): string {
  return clip.date_key || new Date(clip.timestamp).toDateString();
}

/** Existing Sidebar date/reason predicate. Keep Recent / Saved / Sentry keys exact. */
export function matchesClipReasonFilter(clip: Clip, filters: ClipFilterState): boolean {
  // Recent
  if (clip.event === 'Recent') {
    return filters.recent;
  }

  // Saved (Dashcam)
  if (clip.event === 'Saved') {
    // If Other is enabled, show all Saved clips (Override)
    if (filters.dashcamOther) return true;

    const reason = clip.reason || '';
    if (reason === 'user_interaction_honk') {
      return filters.dashcamHonk;
    }
    if (reason === 'user_interaction_dashcam_panel_save' || reason === 'user_interaction_dashcam_icon_tapped') {
      return filters.dashcamSaved;
    }
    // If reason doesn't match known types, it falls under 'Other' (which is disabled here)
    return false;
  }

  // Sentry
  if (clip.event === 'Sentry') {
    // If Other is enabled, show all Sentry clips (Override)
    if (filters.sentryOther) return true;

    const reason = clip.reason || '';
    if (reason === 'sentry_aware_object_detection') {
      return filters.sentryObject;
    }
    if (reason.startsWith('sentry_aware_accel_')) {
      return filters.sentryAccel;
    }
    return false;
  }

  return false;
}

export function filterClipsByDateAndReason(
  clips: Clip[],
  selectedDate: Date,
  filters: ClipFilterState
): Clip[] {
  const targetDateStr = selectedDate.toDateString();
  return clips.filter(
    (clip) => clipDateKey(clip) === targetDateStr && matchesClipReasonFilter(clip, filters)
  );
}
