# Changelog

All notable changes to this project will be documented in this file.

The format is based on [Keep a Changelog](https://keepachangelog.com/en/1.0.0/),
and this project adheres to [Semantic Versioning](https://semver.org/spec/v2.0.0.html).

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
- Restored missing location data by prioritizing `event.json` parsing.

## [0.1.9] - 2025-12-13
### Performance
- Optimized clips list API query by excluding large `full_data_json` telemetry field, significantly reducing response payload size.

## [0.1.8] - 2025-12-13
### Changed
- Updated map interface to use dark mode (CartoDB Dark Matter) and custom blue markers for better visibility and aesthetics.

## [0.1.7] - 2025-12-13
### Fixed
- Fixed missing map markers by populating telemetry from `event.json` coordinates when video SEI data is unavailable.
- Fixed potential authentication issues for thumbnails and videos by supporting token query parameters.
- Added detailed error logging for file serving to assist in debugging.

## [0.1.6] - 2025-12-13
### Fixed
- Fixed missing map markers by correctly loading telemetry data for all clips.
- Added map auto-focus to automatically fit all event markers within the view.
- Fixed thumbnail generation issues by validating cache integrity and regenerating corrupted thumbnails.

## [0.1.5] - 2025-12-13
### Fixed
- Fixed mirrored camera feeds in 3D view by horizontally flipping video textures.

## [0.1.4] - 2025-12-13
### Fixed
- Fixed camera feed layout issue where videos appeared zoomed in or cropped by forcing the Video.js player to fill its container.

## [0.1.3] - 2025-12-11
### Changed
- Optimized sidebar rendering by memoizing components and callbacks to reduce unnecessary re-renders.

## [0.1.2] - 2025-12-11
### Fixed
- Prevented non-3D grid layout from blowing out when videos load by enforcing CSS containment on grid items.

## [0.1.1] - 2025-12-11
### Added
- New favicon with 'T' logo style.

## [0.1.0] - 2025-12-11
### Added
- Initial versioning system implementation.
- Version display in the application UI.
- Changelog timeline view.

### Fixed
- Fix Player Zoom, Grid, and Playback Issues (Commit 1de8cfa).
