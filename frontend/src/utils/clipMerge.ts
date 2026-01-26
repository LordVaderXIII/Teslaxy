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
  telemetry?: any;
  event_timestamp?: string;
  // Bolt Optimization: Pre-calculated values to avoid redundant parsing
  start_time?: Date;
  date_key?: string;
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

    for (let i = len - 2; i >= 0; i--) {
        const prev = currentGroup[currentGroup.length - 1];
        const curr = clips[i];

        const prevTime = new Date(prev.timestamp).getTime();
        const currTime = new Date(curr.timestamp).getTime();
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
        if (group.length === 1) {
            const single = group[0];
            // Bolt Optimization: Ensure single clips are also enriched
            if (!single.start_time) {
                single.start_time = new Date(single.timestamp);
                single.date_key = single.start_time.toDateString();
            }
            return single;
        }

        const first = group[0];

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

        // Bolt Optimization: Pre-calculate date objects and keys
        const startTime = new Date(first.timestamp);
        const dateKey = startTime.toDateString();

        return {
            ...first,
            city: bestCity,
            event_timestamp: bestEventTimestamp,
            video_files: allFiles,
            start_time: startTime,
            date_key: dateKey,
            // Note: Telemetry ID/Data is kept from the first clip.
            // Since we can't easily merge telemetry objects on the client without deep logic,
            // this is a known acceptable limitation for "One Long Timeline" support.
        };
    });
};
