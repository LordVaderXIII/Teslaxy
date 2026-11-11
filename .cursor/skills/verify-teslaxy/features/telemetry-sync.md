# Telemetry/video sync on private real footage

Keep all footage, API detail JSON, SEI dumps and measurements under `.git/realclips/`, and screenshots under `.git/proof/`. Never commit these files or publish GPS coordinates. Do not touch production except explicitly approved read-only GET/SMB downloads. Do not request a video `quality` parameter on production.

## Measure before changing code

Use at least two consecutive Recent minutes and a Saved/Sentry event, each with front and another camera. For each file record its filename timestamp, the next filename gap, frame count, rate, start PTS and media duration:

```sh
ffprobe -v error -select_streams v:0 -count_frames -show_entries stream=nb_read_frames,r_frame_rate,avg_frame_rate,start_time,duration -of json .git/realclips/RecentClips/<file>.mp4
(cd backend && go run ./cmd/sei-probe ../.git/realclips/RecentClips/<file>.mp4)
ffprobe -v error -select_streams v:0 -show_packets -show_entries packet=pts_time,pos,size -of json .git/realclips/RecentClips/<file>.mp4 > .git/realclips/packets.json
ffprobe -v error -select_streams v:0 -show_frames -show_entries frame=best_effort_timestamp_time -of json .git/realclips/RecentClips/<file>.mp4 > .git/realclips/frames.json
```

`sei-probe` prints counts, sequence endpoints and transition indices only, never coordinates. A sequence number is not a timestamp. Associate each SEI with its containing MP4 packet's PTS; `telemetryPacketTimes` performs and validates this join. Sample count can differ from decoded frame count, and SEI can stop partway through a file. Do not stretch a partial sample array to the media duration.

Pick brake, blinker, or pulling-away transitions. Inspect footage on both sides, and record the decoded frame index/PTS and SEI index independently. A still image alone does not prove when a physical brake press or signal activation occurred. Separate an exact packet/SEI timing proof from an independently visible physical-event proof.

```sh
ffmpeg -v error -ss <media-seconds> -i .git/realclips/RecentClips/<file>.mp4 -frames:v 1 .git/proof/<clip>-<event>-frame.png
```

Compare front/other-camera filename timestamps, start PTS, duration and the same telemetry transition. Do not assume a device-wide fixed offset.

## Verify the mapping

Run the base `telemetryIndex()` using the real `full_data_json`, `buildCameraSegments()` and media durations. At every event record global time, file-local time, correct file/global sample index, selected index, and signed lead `(selected - correct) / samplesPerSecond`. Repeat with `file_timing_json` anchors on the fix. Target at most one frame-time; measure residual rather than assuming it.

```sh
(cd backend && timeout 280 go test ./services/ -run 'Telemetry|SEI|Sei' -count=1 -v)
node frontend/tests/telemetry-index-check.mjs
node frontend/tests/telemetry-sync-check.mjs
node frontend/tests/playback-timeline-check.mjs
```

The additive `file_timing_json` field keeps every front file, including zero/error extractions. `full_data_json` stays a flat array. Old clips retain an explicitly approximate fallback until the next full scan; startup `ScanAll` re-aggregates Saved and Recent clips. Failed/missing packet timing hides the HUD instead of claiming verified sync. GORM adds the timing column automatically; rollback can leave this unused column without deleting telemetry.

## Visible proof

Build the base and branch separately. Use port 18080 only, a fresh config directory inside `.git/`, `FOOTAGE_PATH` pointing to the private TeslaCam tree, `AUTH_ENABLED=false`, `RETENTION_ENABLED=false`, `RETENTION_DRY_RUN=true`. Check `/ping` for `pong` and scan readiness with `grep -c`; never print server logs. `backend/public` is committed and embedded, so copy the built frontend there before building the branch binary.

Use Playwright `chromium.launch()`. Select the actual clip, seek the containing file, pause at the exact media time, wait for `seeked`, and confirm the timeline and HUD update from the real video clock. Save `<clip>-<event>-before.png`, `-after.png`, and the raw `-frame.png`. Inspect all images for GPS/map content before sharing. Keep already-aligned clips as controls; do not describe an unchanged control as a reproduced bug. Record any unproved reported offset explicitly.
