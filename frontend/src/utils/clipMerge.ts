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

    const n = clips.length;
    // Cache timestamps to avoid repetitive parsing and optimize sorting
    // Float64Array is efficient for numeric timestamps
    const times = new Float64Array(n);
    let isAscending = true;
    let isDescending = true;

    // First pass: Parse timestamps and check order
    // Handle first element
    const t0 = new Date(clips[0].timestamp).getTime();
    times[0] = isNaN(t0) ? Infinity : t0;

    for (let i = 1; i < n; i++) {
        const t = new Date(clips[i].timestamp).getTime();
        const val = isNaN(t) ? Infinity : t;
        times[i] = val;

        if (val < times[i-1]) isAscending = false;
        if (val > times[i-1]) isDescending = false;
    }

    let sortedClips: Clip[];
    let sortedTimes: Float64Array;

    if (isAscending) {
        // Already ascending (Oldest -> Newest)
        sortedClips = [...clips];
        sortedTimes = times;
    } else if (isDescending) {
        // Reverse (Newest -> Oldest => Oldest -> Newest)
        sortedClips = [...clips].reverse();
        sortedTimes = times.reverse();
    } else {
        // Unsorted or mixed - perform sort
        const indices = new Uint32Array(n);
        for(let i=0; i<n; i++) indices[i] = i;

        indices.sort((a, b) => {
             const tA = times[a];
             const tB = times[b];
             return tA - tB;
        });

        sortedClips = new Array(n);
        sortedTimes = new Float64Array(n);
        for(let i=0; i<n; i++) {
            sortedClips[i] = clips[indices[i]];
            sortedTimes[i] = times[indices[i]];
        }
    }

    // 2. Group clips
    const groups: Clip[][] = [];
    if (sortedClips.length > 0) {
        let currentGroup: Clip[] = [sortedClips[0]];

        for (let i = 1; i < n; i++) {
            const curr = sortedClips[i];
            const currTime = sortedTimes[i];

            // prev is always the immediately preceding element in the sorted list
            const prev = sortedClips[i-1];
            const prevTime = sortedTimes[i-1];

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
