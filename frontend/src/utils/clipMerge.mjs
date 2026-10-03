/**
 * Compatibility-layer clip grouping. Same-event rows whose start times are
 * under 65s apart become one list row. Does not invent a second grouping algorithm.
 *
 * @param {Array<Record<string, unknown>>} rawClips
 * @returns {Array<Record<string, unknown>>}
 */
export function mergeClips(rawClips) {
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

    const groups = [];
    const len = clips.length;

    // Start with the last item (Oldest)
    let currentGroup = [clips[len - 1]];

    for (let i = len - 2; i >= 0; i--) {
        const prev = currentGroup[currentGroup.length - 1];
        const curr = clips[i];

        // Use pre-calculated Date objects
        const prevTime = prev.start_time.getTime();
        const currTime = curr.start_time.getTime();
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
        const member_ids = memberIdsOfGroup(group);

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
                return clipRichness(curr) > clipRichness(prev) ? curr : prev;
            }, first);

            // Still collect any extra video files that the time merge found but backend missed
            // (this is a safety net during the transition period).
            const extraFiles = [];
            group.forEach(c => {
                if (c.ID !== best.ID && c.video_files) {
                    extraFiles.push(...c.video_files);
                }
            });

            const files = [...(best.video_files || []), ...extraFiles];
            const merged = { ...best, member_ids };
            if (files.length > 0) {
                merged.video_files = files;
            } else {
                delete merged.video_files;
            }
            return merged;
        }

        // --- Legacy path: pure client-side merge for ungrouped 1-min Recent clips ---
        // Concatenate all video files
        let allFiles = [];
        group.forEach(c => {
            if (c.video_files) {
                allFiles = allFiles.concat(c.video_files);
            }
        });

        // Intelligent property selection
        const bestCity = group.find(c => c.city && c.city !== 'Unknown Location')?.city || first.city;
        const bestEventTimestamp = group.find(c => c.event_timestamp)?.event_timestamp || first.event_timestamp;

        const merged = {
            ...first,
            city: bestCity,
            event_timestamp: bestEventTimestamp,
            member_ids,
        };
        if (allFiles.length > 0) {
            merged.video_files = allFiles;
        } else {
            delete merged.video_files;
        }
        // Note: Telemetry is intentionally kept from the first clip in legacy mode.
        // Long-term the backend aggregateTelemetry should be the source of truth.
        return merged;
    });
}

/**
 * Richness when video_files may be absent: file count (+ source_dir bonus).
 * @param {Record<string, unknown>} clip
 * @returns {number}
 */
function clipRichness(clip) {
    const fileCount = (clip.video_files && clip.video_files.length) || clip.video_file_count || 0;
    return fileCount + (clip.source_dir ? 10 : 0);
}

/**
 * @param {Array<Record<string, unknown>>} group
 * @returns {number[]}
 */
function memberIdsOfGroup(group) {
    const ids = [];
    const seen = new Set();
    for (const c of group) {
        const id = c.ID;
        if (id == null || seen.has(id)) continue;
        seen.add(id);
        ids.push(id);
    }
    return ids;
}
