# Changelog

All notable changes to this project will be documented in this file.

The format is based on [Keep a Changelog](https://keepachangelog.com/en/1.0.0/),
and this project adheres to [Semantic Versioning](https://semver.org/spec/v2.0.0.html).

## [0.2.0] - 2026-10-05
### Added
- 3D align mode. Pick one of the six cameras and nudge its view position (x, y, z), yaw, pitch, roll, and field of view. The other five stay put. Reset puts that camera back. Export downloads `camera-alignment.json` (schema version 1). Import loads a file of that schema and leaves the view unchanged if the file is unreadable or a different version.
- The player loads `frontend/src/data/camera-alignment.json` on start. The committed numbers are the production cylinder (six 60° slices, radius 8, height 5, viewer at (0, 1.2, 0.1)). They are a viewing layout, not a vehicle mount. Replacing that file is how a later export becomes the default.

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