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
  video_files?: VideoFile[];
  telemetry?: any;
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

    for (let i = len - 2; i >= 0; i--) {
        const prev = currentGroup[currentGroup.length - 1];
        const curr = clips[i];

        const prevTime = new Date(prev.timestamp).getTime();
        const currTime = new Date(curr.timestamp).getTime();
        const diffSeconds = (currTime - prevTime) / 1000;

        // Criteria: Same event type, Gap < 90s
        // We treat 'Recent' and 'Saved'/'Sentry' similarly, but usually only Recent needs stitching.
        // However, strictly adhering to event type prevents merging a Sentry event into a Recent drive accidentally.
        if (curr.event === prev.event && diffSeconds < 90 && diffSeconds >= 0) {
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
            // Note: Telemetry ID/Data is kept from the first clip.
            // Since we can't easily merge telemetry objects on the client without deep logic,
            // this is a known acceptable limitation for "One Long Timeline" support.
        };
    });
};
