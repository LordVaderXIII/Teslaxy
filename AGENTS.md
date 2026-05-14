# AGENTS.md

## Project Context
Teslaxy is a self-hosted web application for viewing Tesla Sentry and Dashcam clips. It is a single Docker container solution with a Go backend and React frontend. It supports multi-camera playback, 3D/360 views, telemetry overlays, and clip exporting with NVIDIA GPU acceleration.

## Coding Standards

### General
- Follow standard conventions for Go (Effective Go) and React/TypeScript.
- Ensure all code is clean, readable, and well-commented.
- Use meaningful variable and function names.
- Always follow Semantic Versioning (SemVer) for every commit: Analyze changes to bump MAJOR for breaking changes, MINOR for new compatible features, PATCH for bug fixes. Use Conventional Commits format (e.g., 'feat:', 'fix:', 'BREAKING CHANGE:'). Tag releases as 'vX.Y.Z' and update CHANGELOG.md accordingly.

### Backend (Go)
- Use `gin` for the web framework.
- Use `gorm` or `sql` with `sqlite3` for database interactions.
- Use `fsnotify` for directory watching.
- Handle errors gracefully and log them.
- All file paths should be sanitized to prevent traversal attacks.
- Ensure the application can run in a Docker container.

### Core Domain Model — Logical Events vs Physical Clips (Critical)
- A **physical clip** = one 1-minute MP4 from one camera.
- A **logical event** (what the user sees as one row in the sidebar) = one or more physical clips that belong together (a Sentry trigger, a Saved clip, or a continuous Recent drive).
- **Source of truth rule**: The scanner (`services/scanner.go`) owns grouping.
  - For `SentryClips`/`SavedClips`: the directory containing `event.json` + the `SourceDir` field on `Clip` is the stable identity.
  - `event.json` is the primary source for `city`, `reason`, `event_timestamp`.
  - SEI data extracted from Front camera videos (via `aggregateTelemetry`) is the source for speed/steering/autopilot telemetry.
- The frontend `clipMerge.ts` is now only a fallback compatibility layer. New code must not duplicate grouping logic on the client.
- When modifying the scanner, always update `SourceDir` and prefer directory + event.json over pure timestamp heuristics.

### Frontend (React/TypeScript)
- Use functional components and Hooks.
- Use Tailwind CSS for styling.
- Follow Apple Design Guidelines (clean, minimal, responsive, glassmorphism).
- Use `video.js` for video playback.
- Use `axios` for API calls.

### Docker
- Use multi-stage builds to keep the final image size small.
- Ensure `ffmpeg` with NVENC support is available or injected.
- Expose port 80.

## Instructions
- Always verify changes with `read_file` or `ls` after creating/modifying files.
- Run tests where possible.
- If unsure about a requirement, check the plan or ask the user.
- Update this file when adding automation-relevant guidance for AI coding agents. Include any new tools, input/output conventions, or workflow quirks that future agents should know.
- Document code changes in the changelog and bump versions following Semantic Versioning for every release (PATCH for bug fixes, MINOR for additive features, MAJOR for breaking changes). Align package/app versions with the changelog entry.
