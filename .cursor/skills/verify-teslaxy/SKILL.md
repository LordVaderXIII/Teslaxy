---
name: verify-teslaxy
description: "Verify Teslaxy (Go API + React player) locally: launch backend against a fixture TeslaCam tree, doctor via /ping and /api/version, drive library/retention/scanner endpoints with curl, capture evidence under /opt/cursor/artifacts. Use when proving Teslaxy behavior, retention pruning, scanner reconciliation, or after backend/API changes."
---

# Verify Teslaxy

Agents read this cold. Prefer fixture footage under `/tmp` over any live NAS share. Use IPs only (never `.local`); Valhalla is `192.168.86.41` if a live share is ever required — default to fixtures. Never touch real footage.

## Launch

From repo root with Go 1.24+ and GCC available:

```bash
FIX="/tmp/teslaxy-verify-$$"
mkdir -p "$FIX/footage/RecentClips" "$FIX/footage/SentryClips" "$FIX/config"
# Minimal H.264 seed so the scanner has something real to index
ffmpeg -y -f lavfi -i testsrc=size=320x240:rate=30 -t 1 -c:v libx264 -pix_fmt yuv420p \
  "$FIX/footage/RecentClips/2024-03-10_14-00-00-front.mp4" 2>/dev/null || \
  dd if=/dev/zero of="$FIX/footage/RecentClips/seed.mp4" bs=1024 count=4 status=none
export FOOTAGE_PATH="$FIX/footage" CONFIG_PATH="$FIX/config" PORT=18080 AUTH_ENABLED=false
export RETENTION_ENABLED=false RETENTION_DRY_RUN=true
cd backend && go build -o /tmp/teslaxy-verify ./
/tmp/teslaxy-verify >"$FIX/server.log" 2>&1 &
echo $! >"$FIX/server.pid"
# Ready when /ping returns pong
for i in $(seq 1 30); do
  curl -sf "http://127.0.0.1:18080/ping" | grep -q pong && break
  sleep 0.2
done
```

Teardown: `kill "$(cat "$FIX/server.pid")"` then `rm -rf "$FIX"`. Never `pkill` by name.

For retention-only proofs that do not need the HTTP server, skip Launch and use:

```bash
cd backend && go test ./... -count=1
cd backend && go run ./cmd/retention-dry-run
```

## Doctor

```bash
curl -sf "http://127.0.0.1:18080/ping"
curl -sf "http://127.0.0.1:18080/api/version"
test -f "$FIX/config/teslacam.db"
```

Worth driving only if `/ping` is `{"message":"pong"}`, `/api/version` returns a SemVer `latestVersion`, and the sqlite file exists.

## Drive

Harness: plain HTTP (`curl`) against `127.0.0.1` (never a `.local` hostname).

Retention settings / dry-run:

```bash
curl -sf "http://127.0.0.1:18080/api/retention/settings"
curl -sf -X POST "http://127.0.0.1:18080/api/retention/run" \
  -H 'Content-Type: application/json' \
  -d '{"dry_run":true}'
curl -sf "http://127.0.0.1:18080/api/library"
```

Protect a clip (replace `ID`):

```bash
curl -sf -X POST "http://127.0.0.1:18080/api/clips/ID/protect" \
  -H 'Content-Type: application/json' \
  -d '{"protected":true}'
```

Scanner library / explicit purge (writes always need auth + CSRF):

```bash
curl -sf "http://127.0.0.1:18080/api/library"
curl -sf "http://127.0.0.1:18080/api/library/files?id=CLIP_ID"
# Operator purge after a recreated empty root (auth + CSRF required):
# curl -sf -X POST "http://127.0.0.1:18080/api/scanner/purge-missing" \
#   -H "Authorization: Bearer $TOKEN" -H "X-Requested-With: XMLHttpRequest"
```

Also see `features/retention.md`, `features/scanner.md`, and helper `scripts/verify-retention.sh`.

## Evidence

Write proof under `/opt/cursor/artifacts/` (survives cleanup):

- `go-test-full.log` — full `go test -race ./...`
- `go-vet.log` — `go vet ./...`
- `retention-dry-run.json` — structured dry-run report
- `verify-retention-http.log` — curl transcript when the server path was used
- `verify-scanner-http.log` — curl transcript when the scanner fixture path was used

Proof standards: dry-run must leave fixture files intact; age/size/protect cases are covered by `go test ./services/ -run Retention`.

## Cleanup

```bash
if [ -f "$FIX/server.pid" ]; then kill "$(cat "$FIX/server.pid")" 2>/dev/null || true; fi
rm -rf "$FIX"
# Do NOT delete /opt/cursor/artifacts/*
```

## Helpers

- `scripts/verify-retention.sh` — runs unit tests + fixture dry-run; optional `--http` launches the server path above and curls retention endpoints.
