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
- **Database migrations are always automatic** via GORM `AutoMigrate` (see `backend/database/db.go` for the full policy and current implementation). 
  - Only add new fields to model structs (`models/`). 
  - Never write manual SQL migrations, migration files, or raw `ALTER TABLE` statements while using the current AutoMigrate strategy.
  - This rule exists for developer velocity during heavy pre-v1.0 development.
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
- `ReconcileMissing` holds `mediaOpMu`'s write lock. Scan indexing (`ScanAll`, `scanDir`, `scanTree`) holds the read lock and releases it before reconcile. Refuse to reconcile when the footage root is missing or an empty directory while clips for that root remain (`ErrReconcileRefused`); scope clip cleanup to the requested root. A moved directory is walked recursively. Recent find-or-create is serialized and matches an existing Recent clip by file path so deleting the first minute cannot duplicate the rest.

### 3D camera alignment
- `frontend/src/data/camera-alignment.json` (schema version 1) is the viewing layout the 3D player loads. The frontend imports it; it is not served from the Go API.
- The committed values are the production cylinder slices (60° each, radius 8, height 5). They are not vehicle mounts. Do not replace them with unsourced pose numbers. A later hand-aligned export is what gets committed over this file.
- `frontend/tests/camera-alignment-check.mjs` compares that file's cylinder geometry with the production slice angles. Run it with `node frontend/tests/camera-alignment-check.mjs`.
- On a phone the align panel is a short stack of horizontal rows: cameras, nudges, then Reset / Export / Import / Close. Do not wrap those actions into a column, and do not cap the panel with a percentage max-height. That clips the 44px controls at 390×844. On a phone the panel is in the document flow under the canvas (`.align-host.is-open`). Do not put it back to a full-stage overlay: the overlay covers the cylinder and the nudge has no visible preview.
- Nudge buttons must not fire on `pointerdown`. A sideways swipe of `.align-nudge` is a scroll (`classifyNudgeMove`). A tap fires once on click. A hold that stays inside the slop repeats. Export must not call `URL.revokeObjectURL` in the same turn as `link.click()`; Safari drops the file. `revokeDownloadLater` waits. Run `node frontend/tests/align-gesture-check.mjs`.

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
- Prod runtime is `debian:bookworm-slim` (glibc), not Alpine. The pinned BtbN linux64 ffmpeg is a glibc binary and does not run under Alpine/`gcompat` (build-time `h264_nvenc` check exits 1). Backend builder is `golang:1.24-bookworm` so CGO matches. Keep the sha256 pin and run `scripts/check-ffmpeg-nvenc.sh` at build time (binary must execute, then list `h264_nvenc`). Smoke: `scripts/verify-docker-ffmpeg.sh` (real one-second `h264_nvenc` encode; SKIP only on an explicitly recognised "no GPU" result: `TESLAXY_NVENC_FORCE_GPU=0` or `nvidia-smi -L` printing exactly `No devices were found` — a missing `nvidia-smi`, ffmpeg error text, whitespace/partial/indented/mixed/garbage output, driver errors and timeouts are all FAIL (exit 1); `REQUIRE_NVENC_ENCODE` defaults to `1`, explicit `0` only accepts a recognised SKIP). Both probes run under `timeout -k` (`NVENC_SMI_TIMEOUT_SEC`/`NVENC_SMI_KILL_GRACE_SEC`, `NVENC_PROBE_TIMEOUT_SEC`/`NVENC_PROBE_KILL_GRACE_SEC`); a timeout is FAIL. Test suite: `bash scripts/test-verify-docker-ffmpeg-timeouts.sh` (binds loopback sockets, stubs `docker`; `KEEP_GOING=1` lists every failure).
- CI is `.github/workflows/ci.yml`: Go vet + `-race` tests, the shell verifier suite, frontend `tests/*.mjs` + build, and `docker compose config` with the `teslaxy-net` subnet pin asserted. Keep it green before reporting.
- Compose/runtime must set `NVIDIA_VISIBLE_DEVICES=all` and `NVIDIA_DRIVER_CAPABILITIES=compute,video,utility` with `runtime: nvidia` / toolkit, or encode fails with `Cannot load libcuda.so.1`.

### Footage retention
- Implemented in `backend/services/retention.go` (+ `retention_config.go`, `retention_path.go`, `retention_scheduler.go`).
- Defaults: disabled, dry-run on, 90 days, 300 GB, 24h interval, 15m min file age.
- Never prune `Event == "Saved"` or `Clip.Protected == true`. Re-check protect inside the prune transaction under `clipMu`.
- Soft-delete only selected `VideoFile` rows before `unlinkat`, setting `retention_pending` on those rows (and the clip when fully soft-deleted). Recovery unlinks only `retention_pending` tombstones — reconcile soft-deletes must not be unlinked if footage was restored. Hard-delete telemetry before the clip; on telemetry failure keep the clip soft-deleted and record `errors[]`; sweep stranded telemetry with no clip in fixed-size batches (large backlogs / mid-sweep failure resume on the next run). Scanner skips retention-pending paths/source dirs, clears non-retention tombstones on rescan when files exist, holds `mediaOpMu` RLock while indexing (no nested RLock in workers), and re-stats before insert.
- Safety: pin `footageRoot` fd (`openFootageRoot`) for the whole pass; resolve/unlink via that fd only; never delete whole event folders; skip recent mtimes at delete time; size-cap estimates count a hardlinked inode only when all known links are selected; unlink freed bytes only on last link.
- Write APIs always need auth + CSRF headers; malformed `dry_run` → 400; unknown `RETENTION_DRY_RUN` keeps dry-run ON.
- Verify: `scripts/verify-retention.sh` and `.cursor/skills/verify-teslaxy/`. Fixture dry-run: `go run ./cmd/retention-dry-run` from `backend/`.
- Do not target live NAS hosts in agents unless asked; use IPs only (Valhalla `192.168.86.41`), never `.local`.

### Cloud agent environment
- `.cursor/environment.json` is the build setup: `.cursor/Dockerfile` (Node 22, Go 1.24, gcc, ffmpeg) plus `.cursor/install.sh` (`npm ci` in `frontend/`, `go mod download` in `backend/`). No snapshot id. Do not put secrets in that file.

## Instructions
- Always verify changes with `read_file` or `ls` after creating/modifying files.
- Run tests where possible.
- If unsure about a requirement, check the plan or ask the user.
- Update this file when adding automation-relevant guidance for AI coding agents. Include any new tools, input/output conventions, or workflow quirks that future agents should know.
- Document code changes in the changelog and bump versions following Semantic Versioning for every release (PATCH for bug fixes, MINOR for additive features, MAJOR for breaking changes). Align package/app versions with the changelog entry.
