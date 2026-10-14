# Health and version

## Sub-features

- `ping` — `GET /ping` liveness.
- `version` — `GET /api/version` parses `CHANGELOG.md` SemVer.

## How to get to it (user POV)

- Load the app; the UI may show the changelog version from `/api/version`.

## Driving it with curl

- `curl -sf http://127.0.0.1:18080/ping` → `{"message":"pong"}`
- `curl -sf http://127.0.0.1:18080/api/version` → `latestVersion` like `v0.3.0`

## Gotchas

- Version is the first dated `## [X.Y.Z] - YYYY-MM-DD` in `CHANGELOG.md`, not `package.json` alone. Keep them aligned on release.
