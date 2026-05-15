# Changelog

All notable changes to this project will be documented in this file.

The format is based on [Keep a Changelog](https://keepachangelog.com/en/1.0.0/),
and this project adheres to [Semantic Versioning](https://semver.org/spec/v2.0.0.html).

## [Unreleased]

### Security
- Replaced the entire custom hand-rolled JWT implementation (raw HMAC + manual base64 + string header) with the official audited library `github.com/golang-jwt/jwt/v5`.
  - New tokens now use proper `RegisteredClaims` (`iss`, `sub`, `iat`, `exp`, `nbf`).
  - Added explicit `SigningMethodHMAC` verification to prevent algorithm confusion attacks.
  - **Breaking change**: All previously issued tokens are now invalid.

### Architecture / Maintainability
- Formalized the database migration policy: **migrations are always automatic** via GORM `AutoMigrate`.
  - Added explicit rule to `AGENTS.md`.
  - All future model changes must be additive fields only. No manual SQL migrations are permitted under the current strategy (documented in `backend/database/db.go`).

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