# Footage retention

Retention prunes old / oversized TeslaCam footage under `FOOTAGE_PATH`, keeps `teslacam.db` consistent, and never deletes SavedClips or user-protected clips.

## Sub-features

- `retention-settings` read/write config via env, `CONFIG_PATH/retention.json`, and `/api/retention/settings`.
- `retention-dry-run` reports candidates and bytes that would be freed without deleting.
- `retention-age` deletes unprotected clips older than `max_age_days` (oldest first).
- `retention-size` deletes unprotected clips until total size ≤ `max_size_gb`.
- `retention-protect` skips `Event=Saved` and `protected=true` clips.
- `retention-safety` skips paths outside the footage root, symlinks, and recent mtimes.

## How to get to it (user POV)

- Operators set env vars / `retention.json` or call `PUT /api/retention/settings`.
- Trigger a one-shot pass with `POST /api/retention/run` (`{"dry_run":true}` first).
- Flag a keep-forever clip with `POST /api/clips/:id/protect` `{"protected":true}`.
- The scheduled job runs on `interval_hours` when enabled.

## Driving it with curl / go test

Preconditions: fixture footage only; do not point at Valhalla unless explicitly asked.

- Unit coverage: `cd backend && go test ./services/ -count=1 -run Retention`
- Fixture dry-run: `cd backend && go run ./cmd/retention-dry-run`
- HTTP dry-run (server launched per `SKILL.md`): obtain a JWT via `/api/login`, then:
  `curl -sf -X POST http://127.0.0.1:18080/api/retention/run -H 'Content-Type: application/json' -H "Authorization: Bearer $TOKEN" -H 'X-Requested-With: XMLHttpRequest' -d '{"dry_run":true}'`
- Proof: report has `"dry_run":true`, `"deleted":[]`, and fixture files still exist on disk. Save JSON under `/opt/cursor/artifacts/retention-dry-run.json`.

## Gotchas

- Docker compose mounts footage `:ro` by default — destructive prune needs a writable mount.
- Write endpoints always need Bearer auth + CSRF header, even when `AUTH_ENABLED=false`.
- Malformed `dry_run` (string/number) returns HTTP 400 and must not delete.
- Symlinked parent directories are refused (`openat` no-follow); only selected files are unlinked.
- Files with mtime newer than `min_file_age_minutes` (default 15) are skipped as in-progress writes.
