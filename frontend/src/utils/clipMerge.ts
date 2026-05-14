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

  // source_dir comes from the backend and is the stable key for a logical Tesla event
  // (the directory containing event.json for Sentry/Saved, or a grouping key for Recent).
  // When present and the clip has multiple video_files, this clip is considered
  // already grouped by the backend and should not be further merged on the client.
  source_dir?: string;
}

export const mergeClips = (rawClips: Clip[]): Clip[] => {
    if (!rawClips || rawClips.length === 0) return [];

    // Optimization: Enrich clips with pre-calculated Date objects and keys (O(N))
    // This avoids repeated date parsing in downstream components (Sidebar, Calendar)
    const clips = rawClips.map(c => {
         const d = new Date(c.timestamp);
         return {
             ...c,
             start_time: d,
             date_key: d.toDateString()
         };
    });

    // Optimization: The backend returns clips sorted by Timestamp DESC.
    // Instead of re-sorting them ASC (O(N log N)), we iterate backwards (O(N)).
    // This effectively processes them in ASC order (Oldest -> Newest).

    const groups: Clip[][] = [];
    const len = clips.length;

    // Start with the last item (Oldest)
    let currentGroup: Clip[] = [clips[len - 1]];

    for (let i = len - 2; i >= 0; i--) {
        const prev = currentGroup[currentGroup.length - 1];
        const curr = clips[i];

        // Use pre-calculated Date objects
        const prevTime = prev.start_time!.getTime();
        const currTime = curr.start_time!.getTime();
        const diffSeconds = (currTime - prevTime) / 1000;

        // Criteria: Same event type, Gap < 5s logic (Start-to-Start < 65s)
        // Since we are comparing CLIP timestamps (start times), if they are continuous 1-min segments:
        // Diff should be ~60s.
        // If we set threshold to 65s, it allows standard continuity.
        if (curr.event === prev.event && diffSeconds < 65 && diffSeconds >= 0) {
            currentGroup.push(curr);
        } else {
            groups.push(currentGroup);
            currentGroup = [curr];
        }
    }
    groups.push(currentGroup);

    // 3. Create Super Clips
    return groups.map(group => {
        if (group.length === 1) return group[0];

        const first = group[0];

        // === ARCHITECTURAL RULE (fixes review points 1.1 + 1.2) ===
        // If ANY clip in this time-based group already has multiple video files OR has a source_dir,
        // it means the backend (scanner) has already performed logical grouping using event.json + directory.
        // In that case we should NOT blindly concatenate — the backend is the source of truth.
        const anyBackendGrouped = group.some(c =>
            (c.video_files && c.video_files.length > 1) || !!c.source_dir
        );

        if (anyBackendGrouped) {
            // Trust the backend-grouped clip(s). Pick the "richest" one (most video files + best metadata).
            const best = group.reduce((prev, curr) => {
                const prevScore = (prev.video_files?.length || 0) + (prev.source_dir ? 10 : 0);
                const currScore = (curr.video_files?.length || 0) + (curr.source_dir ? 10 : 0);
                return currScore > prevScore ? curr : prev;
            }, first);

            // Still collect any extra video files that the time merge found but backend missed
            // (this is a safety net during the transition period).
            const extraFiles: VideoFile[] = [];
            group.forEach(c => {
                if (c.ID !== best.ID && c.video_files) {
                    extraFiles.push(...c.video_files);
                }
            });

            return {
                ...best,
                video_files: [...(best.video_files || []), ...extraFiles],
            };
        }

        // --- Legacy path: pure client-side merge for ungrouped 1-min Recent clips ---
        // Concatenate all video files
        let allFiles: VideoFile[] = [];
        group.forEach(c => {
            if (c.video_files) {
                allFiles = allFiles.concat(c.video_files);
            }
        });

        // Intelligent property selection
        const bestCity = group.find(c => c.city && c.city !== 'Unknown Location')?.city || first.city;
        const bestEventTimestamp = group.find(c => c.event_timestamp)?.event_timestamp || first.event_timestamp;

        return {
            ...first,
            city: bestCity,
            event_timestamp: bestEventTimestamp,
            video_files: allFiles,
            // Note: Telemetry is intentionally kept from the first clip in legacy mode.
            // Long-term the backend aggregateTelemetry should be the source of truth.
        };
    });
};
