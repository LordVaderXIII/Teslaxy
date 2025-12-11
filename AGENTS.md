# AGENTS.md

## Project Context
Teslaxy is a self-hosted web application for viewing Tesla Sentry and Dashcam clips. It is a single Docker container solution with a Go backend and React frontend. It supports multi-camera playback, 3D/360 views, telemetry overlays, and clip exporting with NVIDIA GPU acceleration.

## Coding Standards

### General
- Follow standard conventions for Go (Effective Go) and React/TypeScript.
- Ensure all code is clean, readable, and well-commented.
- Use meaningful variable and function names.

### Backend (Go)
- Use `gin` for the web framework.
- Use `gorm` or `sql` with `sqlite3` for database interactions.
- Use `fsnotify` for directory watching.
- Handle errors gracefully and log them.
- All file paths should be sanitized to prevent traversal attacks.
- Ensure the application can run in a Docker container.

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
