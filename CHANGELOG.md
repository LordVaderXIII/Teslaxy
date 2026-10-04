# Changelog

All notable changes to this project will be documented in this file.

The format is based on [Keep a Changelog](https://keepachangelog.com/en/1.0.0/),
and this project adheres to [Semantic Versioning](https://semver.org/spec/v2.0.0.html).

## [Unreleased]

This integration is not a production release. Do not deploy it as one.

### Fixed
- Viewer playback speed no longer blanks the app after a camera is focused. Unmounted cameras left disposed video.js players in the registry, and setting `playbackRate` wrote through a null media element.
- Six-camera playback now seeks a camera back to the feature camera's clock when it drifts by more than 0.3s, and mosaic tiles preload metadata so a late start does not stay a constant offset.
- A segment change applies the selected playback speed to the new video element. The control and the element no longer disagree after a seek onto the next minute.
- Short Saved clips no longer build a timeline from a duplicated file row. The same camera path and timestamp listed twice (two member ids) is one segment, and the timeline uses the decoded media duration instead of assuming 60s. Telemetry is still labeled SYNC APPROX; this does not claim frame-accurate sync.
- `/api/library` and `/api/clips/:id` now publish the incident point stored on `clips.telemetry_id`. Clip detail previously followed GORM's has-one preload on `telemetries.clip_id`, which can be a different row. Stored rows are not rewritten.
- `GET /api/library/files?id=0` returns HTTP 400, consistent with a non-numeric id.
- `GET /api/library/files` no longer lists the same camera file twice when two member clip ids point at one path. The first row in the existing order is kept. Neither member id is reported missing.
- A seek into a later minute waits until the new element reports that offset. Media time 0 on the freshly loaded file no longer pulls the clock back to the start of that minute. The selected playback speed is still applied to the new element.
- A seek while paused is issued once and is not restarted while `seeking` is true. Repeating `currentTime` left every camera at readyState HAVE_METADATA with `seeking` stuck, so Play never resumed decoding. A paused element that only has metadata waits until it has a frame before that single seek. Playback rate is unchanged.
- Fixed Docker build still failing on Unraid after 0.1.18 (`npm ci` aborting with "lock file's three@0.182.0 does not satisfy three@0.170.0").
  - 0.1.18 added `three` to `package.json` but never regenerated `package-lock.json`, leaving the two files out of sync — `npm ci` requires them to match exactly.
  - Pinned `three` to `^0.182.0` (the version already resolved in the lock tree) and regenerated `package-lock.json` so `three` is a proper direct dependency instead of a `peer`-flagged transitive one.

### Security
- Replaced the entire custom hand-rolled JWT implementation (raw HMAC + manual base64 + string header) with the official audited library `github.com/golang-jwt/jwt/v5`.
  - New tokens now use proper `RegisteredClaims` (`iss`, `sub`, `iat`, `exp`, `nbf`).
  - Added explicit `SigningMethodHMAC` verification to prevent algorithm confusion attacks.
  - **Breaking change**: All previously issued tokens are now invalid.

### Architecture / Maintainability
- Formalized the database migration policy: **migrations are always automatic** via GORM `AutoMigrate`.
  - Added explicit rule to `AGENTS.md`.
  - All future model changes must be additive fields only. No manual SQL migrations are permitted under the current strategy (documented in `backend/database/db.go`).

## [0.1.22] - 2026-10-04
### Fixed
- `GET /api/camera-poses` publishes one pose per dashcam camera in the `ground_nominal` frame (x forward, y left, z up, metres, origin on the ground): `camera`, `yaw_deg`, `pitch_deg`, `roll_deg`, `x_m`, `y_m`, `z_m`. The 3D stitch loads that response and projects with its yaw, pitch, and roll. Mount position is carried on the camera and does not slide a pixel, because no range is published.
- Where a number is published it replaces the overlap reading. Left pillar and both repeaters use the StandardE2E NATIX nominal axes (commit `cff77e53`). Front yaw stays 0 and rear yaw stays 180 from the Tesla service direction text. Front position is (1.82, 0, 1.30) m. Repeater lateral positions are ±0.90 m.
- The right pillar yaw stays −45°. No sourced replacement was found, so that previous reading is kept and is not labeled as factory geometry. Every roll stays 0. Translations other than the front mount and the repeater lateral values stay 0. Field-of-view pairs are unchanged.
- Ownership is unchanged. The front camera keeps its 46°×34° frame, a pillar keeps a ray its repeater also sees, and a direction outside every published fan stays empty. The right pillar and right repeater fans no longer meet, so the wedge between them stays empty.

## [0.1.21] - 2026-10-04
### Fixed
- 3D handoff follows the ownership marks on RecentClips/2026-02-18_17-34-39. The front camera keeps every direction inside its published frame, so a pillar cannot replace a garage corner or the forward yellow line. A pillar keeps every direction inside its fan when the matching repeater also sees it, so the repeater does not cut through the SUV; the repeater draws only outside that fan. Optical-axis yaw is unchanged and is still not a factory extrinsic. A pose is still required before the arch, the driveway, or the skyline can meet. Directions outside every published fan stay empty.

## [0.1.20] - 2026-10-04
### Fixed
- The 3D viewer no longer stretches each camera across an equal 60° cylinder slice. Each camera is a spherical patch of the published HW2.5/HW3 field of view (main 46°×34° rectilinear, B-pillar 90°×65.3° equidistant, repeater 75°×55.4° equidistant, rear 140°×105° equidistant). A direction is drawn by the camera whose lens contains it and whose optical axis is closest, so overlapping fans do not ghost. Optical-axis yaw other than forward (0°) and rearward (180°) is a reading of Tesla's direction words, not a factory extrinsic. Per-car yaw, pitch, and roll are not in the clip files.
- Phone 3D draws at 1× with antialiasing off, redraws when a video frame is presented, requests the existing 480p quality, and pauses cameras whose horizontal fan misses the view. A camera that still reaches the edge of the view keeps decoding. Desktop keeps its pixel ratio, antialiasing, and selected quality, and skips redraws when the frame has not changed.

## [0.1.19] - 2026-10-04
### Fixed
- Phone playback uses one 24px bar across the top of the picture for gear, steering, speed, brake, and accelerator. There is no side column. The desktop panel is unchanged: up to 20rem wide, 3rem speed, 4rem steering wheel.
- Brake and accelerator now follow the SEI sample. `accelerator_pedal_position` (percent of travel; Tesla's 15.60 sample is 15.6% of the bar) sets the accelerator bar width. `brake_applied` fills the brake icon and the brake bar. The player still has no pedal keyboard, pointer, or gamepad handler; those graphics read `telemetry.full_data_json`.

## [0.1.18] - 2025-12-22
### Fixed
- Fixed Docker build failure on Unraid (`npm run build` failing during `tsc -b && vite build`).
  - Removed unused `react-router-dom` from Vite manualChunks (was causing Rollup chunk errors in production build).
  - Added explicit `three` dependency (previously only transitive peer) for reliable TypeScript type resolution inside Docker `node:22-alpine`.
  - Removed fragile `go get` step and added `dist/` existence check in Dockerfile for clearer failure messages.
  - Added comprehensive `.dockerignore` to prevent bloated context, stale files, and permission issues during `docker build`.
  - Cleaned up stray nested `frontend/frontend/` directory.

## [0.1.17] - 2025-12-21
### Accessibility
- Added consistent focus indicators for keyboard navigation in Calendar, Map, and Changelog components.

## [0.1.16] - 2025-12-18
### Security
- Fixed critical security vulnerability where a hardcoded default admin password was used if `ADMIN_PASS` was not set. Now generates a cryptographically secure random password on startup in such cases.

## [0.1.15] - 2025-12-16
### Security
- Fixed critical security vulnerability where a hardcoded default JWT secret was used if `JWT_SECRET` was not set. Now generates a cryptographically secure random key on startup in such cases.

## [0.1.14] - 2025-12-15
### Fixed
- Sanitized clip segment offsets to keep the timeline scrubber within realistic bounds and restored reliable dragging behavior.

## [0.1.13] - 2025-12-14
### Security
- Fixed potential JSON injection vulnerability in JWT generation by replacing manual string formatting with secure JSON marshaling.
### Performance
- Further optimized `GET /api/clips` payload by excluding unused GORM model fields (CreatedAt, UpdatedAt, DeletedAt) and unneeded VideoFile columns.
### Fixed
- Fixed mobile layout issue where the video player was pushed off-screen when the clip list was long.

## [0.1.12] - 2025-12-14
### Fixed
- Fixed Docker build failure on Unraid and other environments by switching from rate-limited Amazon ECR Public Gallery to standard Docker Hub base images.

## [0.1.11] - 2025-12-14
### Added
- Mobile-optimized player view with single camera display and camera switcher.
- Real-time file scanning using `fsnotify` to detect new clips and events immediately.
- Support for generating thumbnails at specific timestamps via API.
- Custom Markdown parser for Changelog display in the UI.

### Changed
- Improved version API to return structured release data.

## [0.1.10] - 2025-12-13
### Fixed
- Fixed issue where 1-minute clips were not grouping correctly by implementing time-based grouping for flat directories.
- Fixed "future clips" issue by correctly detecting timezone from `event.json` coordinates or falling back to "Australia/Adelaide".
- Fixed video player stopping after 1 minute by improving segment transition logic.