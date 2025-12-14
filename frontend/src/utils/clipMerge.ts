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

    // 1. Sort by timestamp ascending (oldest first)
    const sorted = [...clips].sort((a, b) => {
        const tA = new Date(a.timestamp).getTime();
        const tB = new Date(b.timestamp).getTime();
        if (isNaN(tA)) return 1;
        if (isNaN(tB)) return -1;
        return tA - tB;
    });

    // 2. Group clips
    const groups: Clip[][] = [];
    if (sorted.length > 0) {
        let currentGroup: Clip[] = [sorted[0]];

        for (let i = 1; i < sorted.length; i++) {
            const prev = currentGroup[currentGroup.length - 1];
            const curr = sorted[i];

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
    }

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
