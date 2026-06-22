# Teslaxy - Agent Instructions

This file provides context and instructions for AI coding agents working in this repository.

## General Coding-Agent Discipline

You are a coding agent working in an existing codebase. Your job is to deliver the correct change with the minimum necessary code and the minimum necessary scope.

Repository-specific directives in this file override the generic guidance below where they conflict.

### 1. Think Before Coding

Do not make silent assumptions.

Before making changes:
- Restate the task briefly in your own words.
- State assumptions explicitly.
- If multiple interpretations are possible, present them instead of silently choosing one.
- If something important is unclear, stop and ask.
- If a simpler approach exists, prefer it and say so.

Do not hide confusion behind premature implementation.

### 2. Prefer Simplicity

Choose the simplest solution that fully solves the problem.

Default rules:
- No speculative features.
- No unnecessary abstractions.
- No generalisation "for future flexibility" unless requested.
- No extra configurability unless requested.
- No defensive complexity for unrealistic scenarios.
- No large framework-shaped solutions for small local problems.

If the same result can be achieved with less code and less indirection, do that.

### 3. Make Surgical Changes

Only touch what is required for the task.

When editing existing code:
- Do not refactor unrelated code.
- Do not rename unrelated variables.
- Do not restyle unrelated files.
- Do not "clean up" adjacent code unless the task asks for it.
- Match the existing project style and conventions unless told otherwise.

If you notice unrelated issues:
- Mention them separately.
- Do not fix them without approval.

Every changed line should be traceable to the requested task.

### 4. Define Success Criteria Up Front

Before implementation, define what success looks like.

Use concrete criteria such as:
- the bug is reproduced and then fixed
- the requested feature works end-to-end
- relevant tests pass
- no unrelated behaviour changed
- the diff stays minimal and readable

Prefer verifiable goals over vague goals like "improve" or "clean up".

### 5. Verify, Don't Just Declare Victory

Do not claim success without checking.

When possible:
- run the relevant tests
- reproduce the bug before fixing it
- verify the behaviour after the change
- inspect the diff for scope creep
- confirm no unrelated files were modified accidentally

If you could not verify something, say so plainly.

### 6. Avoid Common Agent Failure Modes

Be especially careful about these failure modes:
- making unstated assumptions
- overengineering
- adding unnecessary abstractions
- touching unrelated code
- writing overly defensive code
- duplicating code when a small local helper would do
- claiming something is done without verification
- continuing down a dubious path instead of surfacing uncertainty

### 7. Communication Style

Be concise and explicit.

For non-trivial tasks, structure your response as:
1. Task
2. Assumptions
3. Success criteria
4. Minimal plan
5. Result
6. Verification status
7. Files changed, with file-level detail

Do not pad. Do not pretend certainty you do not have.

### 8. Decision Rule

When in doubt:
- ask rather than assume
- simplify rather than expand
- edit less rather than more
- verify rather than declare

## Required Workflow

1. **Read this file before making any changes** and re-check it when scope changes.
2. **Document all code changes** in the final response with file-level detail.
3. **Create a release increment for every shipped change** using Semantic Versioning:
   - Patch (`x.y.Z`) for fixes and non-breaking maintenance.
   - Minor (`x.Y.z`) for backward-compatible features.
   - Major (`X.y.z`) for breaking changes.
   - Use Conventional Commits format (e.g. `feat:`, `fix:`, `BREAKING CHANGE:`).
4. **Update release artifacts on every release**:
   - `CHANGELOG.md` must include the release and key changes under `[Unreleased]` or a new version heading.
   - Align `frontend/package.json` version with the changelog entry when shipping frontend-visible releases.
5. Keep security and deployment directives in this file intact.

## Project Overview

Teslaxy is a self-hosted web application for viewing Tesla Sentry and Dashcam clips. It is a single Docker container with a Go backend and React frontend, embedding the built frontend into the Go binary via `go:embed`.

**Capabilities:** multi-camera synchronized playback, 3D/360 views, map/GPS tracking, telemetry overlays (from video SEI data), clip exporting with optional NVIDIA NVENC acceleration.

**Status:** Pre-v1.0, heavy active development. README warns many features may be in a broken state.

**Stack:** Go 1.24 + Gin + GORM (jinzhu/gorm) + SQLite3 | React 19 + TypeScript + Vite + Tailwind CSS 4 + video.js + Three.js/React Three Fiber.

## Codebase Structure

| Path | Role |
|------|------|
| `backend/main.go` | App entrypoint: DB init, scanner start, Gin server, embedded frontend serving |
| `backend/api/` | HTTP routes, auth middleware, thumbnails, version/changelog API |
| `backend/services/` | Scanner (`fsnotify`), SEI telemetry extraction, transcoding, export jobs |
| `backend/models/` | GORM models (`Clip`, `VideoFile`, `Telemetry`) |
| `backend/database/db.go` | SQLite init + AutoMigrate policy |
| `backend/proto/` | Protobuf schema for dashcam SEI metadata |
| `frontend/src/` | React UI: Player, Timeline, Sidebar, Map, 3D scene, telemetry overlay |
| `frontend/src/utils/clipMerge.ts` | Legacy client-side clip grouping fallback only |
| `docker/Dockerfile` | Multi-stage build: Node frontend → Go backend (CGO) → Alpine runtime |
| `docker/docker-compose.yml` | Primary deployment config (footage volume, env vars, optional GPU) |
| `docker/config/` | Persistent DB, logs, exports (mounted at `/config`) |
| `CHANGELOG.md` | Release history; also served via `/api/version` |
| `docs/REVIEW.md` | Architecture/security review reference (not a live task tracker) |
| `.Jules/palette.md` | Agent learnings from prior sessions |
| `scripts/deploy_unraid.sh` | Unraid deployment helper |

## Key Directives

### Security & Auth
- **Auth is off by default.** `AUTH_ENABLED=true` enables JWT middleware on `/api/*` (except `/api/login` and `/api/version`).
- JWT uses `github.com/golang-jwt/jwt/v5` with HMAC verification. Tokens can be passed via `Authorization: Bearer` or `?token=` query param (for `<video>`/`<img>`).
- If `JWT_SECRET` or `ADMIN_PASS` are unset, cryptographically random values are generated at startup and logged once.
- Login is rate-limited per client IP. Trusted proxies default to none (`SetTrustedProxies(nil)`).
- **Sanitize all file paths** to prevent directory traversal before serving video/thumbnails/exports.
- Global security headers and 1 MB request body limit are applied in `api/routes.go`.

### Configuration (environment variables)
| Variable | Default | Purpose |
|----------|---------|---------|
| `FOOTAGE_PATH` | `/footage` | TeslaCam root (`SentryClips`, `SavedClips`, `RecentClips`) |
| `CONFIG_PATH` | `/config` | SQLite DB (`teslacam.db`), logs, exports |
| `PORT` | `80` | HTTP listen port |
| `GIN_MODE` | `release` | Gin framework mode |
| `AUTH_ENABLED` | `false` | Enable JWT auth |
| `JWT_SECRET` | auto-generated | JWT signing key |
| `ADMIN_USER` | `admin` | Login username |
| `ADMIN_PASS` | auto-generated | Login password |

### Core Domain Model — Logical Events vs Physical Clips (Critical)
- A **physical clip** = one 1-minute MP4 from one camera.
- A **logical event** (what the user sees as one row in the sidebar) = one or more physical clips that belong together (a Sentry trigger, a Saved clip, or a continuous Recent drive).
- **Source of truth rule**: the scanner (`services/scanner.go`) owns grouping.
  - For `SentryClips`/`SavedClips`: the directory containing `event.json` + the `SourceDir` field on `Clip` is the stable identity.
  - `event.json` is the primary source for `city`, `reason`, `event_timestamp`.
  - SEI data extracted from Front camera videos (via `aggregateTelemetry`) is the source for speed/steering/autopilot telemetry.
- The frontend `clipMerge.ts` is only a fallback compatibility layer. New code must not duplicate grouping logic on the client.
- When modifying the scanner, always update `SourceDir` and prefer directory + `event.json` over pure timestamp heuristics.

## Backend (Go)

- Use `gin` for the web framework.
- Use `gorm` with `sqlite3` for database interactions.
- **Database migrations are always automatic** via GORM `AutoMigrate` (see `backend/database/db.go` for the full policy and current implementation).
  - Only add new fields to model structs (`models/`).
  - Never write manual SQL migrations, migration files, or raw `ALTER TABLE` statements while using the current AutoMigrate strategy.
  - This rule exists for developer velocity during heavy pre-v1.0 development.
- Use `fsnotify` for directory watching.
- Handle errors gracefully and log them.
- All file paths should be sanitized to prevent traversal attacks.
- Ensure the application can run in a Docker container.
- Follow standard Go conventions (Effective Go).

## Frontend (React/TypeScript)

- Use functional components and Hooks.
- Use Tailwind CSS for styling.
- Follow Apple Design Guidelines (clean, minimal, responsive, glassmorphism).
- Use `video.js` for video playback.
- Use `axios` for API calls.

## Docker

- Use multi-stage builds to keep the final image size small.
- Frontend build output is copied to `backend/public/` and embedded into the Go binary.
- `CGO_ENABLED=1` is required at build time for `go-sqlite3`.
- Ensure `ffmpeg` with NVENC support is available or injected for export/transcode.
- Expose port 80 inside the container (mapped to host port in compose, e.g. `8080:80`).
- Use `.dockerignore` to keep build context lean; `npm ci` requires `package-lock.json` in sync with `package.json`.

## Testing

**Backend (primary test suite):**
```bash
cd backend
go test ./...
```

Tests live alongside source (`*_test.go`) in `backend/api/` and `backend/services/`. Coverage includes auth/security, path traversal, SEI DoS protection, scanner grouping/telemetry, and export validation.

**Frontend:**
- `npm run lint` — ESLint
- `npm run build` — `tsc -b && vite build` (also exercised in Docker build)
- `@playwright/test` is in devDependencies but no Playwright test files are checked in yet.

**No CI pipeline** is configured in `.github/` at present.

## Deployment

**Docker (recommended):**
```bash
docker-compose -f docker/docker-compose.yml up -d --build
```
Access at `http://localhost:8080`.

1. Mount TeslaCam footage read-only at `/footage`.
2. Mount `./config` (or equivalent) at `/config` for persistent DB/logs/exports.
3. Uncomment GPU `deploy` block in compose when NVIDIA Container Toolkit is available.

**Local development:**
- Backend: Go 1.23+ with GCC (CGO/SQLite), set `FOOTAGE_PATH`, run `go run main.go` from `backend/`.
- Frontend: Node.js 18+, `npm install && npm run dev` from `frontend/` (proxies API to backend per Vite config).

**Unraid:** see `scripts/deploy_unraid.sh`.

## Agent Maintenance

- Always verify changes with `read_file` or directory listing after creating/modifying files.
- Run tests where possible before claiming success.
- If unsure about a requirement, check `docs/REVIEW.md` or ask the user.
- Update this file when adding automation-relevant guidance for AI coding agents (new tools, I/O conventions, workflow quirks).