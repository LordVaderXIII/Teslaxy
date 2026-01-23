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
}

export const mergeClips = (clips: Clip[]): Clip[] => {
    if (!clips || clips.length === 0) return [];

    // Optimization: The backend returns clips sorted by Timestamp DESC.
    // Instead of re-sorting them ASC (O(N log N)), we iterate backwards (O(N)).
    // This effectively processes them in ASC order (Oldest -> Newest).

    const groups: Clip[][] = [];
    const len = clips.length;

    // Start with the last item (Oldest)
    let currentGroup: Clip[] = [clips[len - 1]];
    // Bolt: Cache timestamp to avoid redundant parsing in the loop.
    // Represents the timestamp of the LAST added clip (prev in the loop context)
    let prevTime = new Date(clips[len - 1].timestamp).getTime();

    for (let i = len - 2; i >= 0; i--) {
        const curr = clips[i];
        const currTime = new Date(curr.timestamp).getTime();
        const diffSeconds = (currTime - prevTime) / 1000;

        // We still need the actual previous clip object for event comparison
        const prev = currentGroup[currentGroup.length - 1];

        // Criteria: Same event type, Gap < 5s logic (Start-to-Start < 65s)
        // Since we are comparing CLIP timestamps (start times), if they are continuous 1-min segments:
        // Diff should be ~60s.
        // If we set threshold to 65s, it allows standard continuity.
        if (curr.event === prev.event && diffSeconds < 65 && diffSeconds >= 0) {
            currentGroup.push(curr);
            prevTime = currTime; // Update cached timestamp
        } else {
            groups.push(currentGroup);
            currentGroup = [curr];
            prevTime = currTime; // Update cached timestamp
        }
    }
    groups.push(currentGroup);

    // 3. Create Super Clips
    return groups.map(group => {
        if (group.length === 1) return group[0];

        const first = group[0];

        // Concatenate all video files
        // Bolt: Use push instead of concat to avoid O(N^2) array allocation for large groups
        const allFiles: VideoFile[] = [];
        group.forEach(c => {
            if (c.video_files) {
                // Use explicit loop or spread. Spread is fine here as video_files is small per clip.
                allFiles.push(...c.video_files);
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
            // Note: Telemetry ID/Data is kept from the first clip.
            // Since we can't easily merge telemetry objects on the client without deep logic,
            // this is a known acceptable limitation for "One Long Timeline" support.
        };
    });
};
