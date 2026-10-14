---
name: verify-teslaxy
description: "Verify Teslaxy (Go API + React player) locally: launch backend against a fixture TeslaCam tree, doctor via /ping and /api/version, drive library/scanner endpoints with curl, capture evidence under /opt/cursor/artifacts. Use when proving Teslaxy behavior, scanner reconciliation, or after backend/API changes."
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
  "$FIX/footage/RecentClips/2024-03-10_14-00-00-front.mp4" 2>/dev/null
export FOOTAGE_PATH="$FIX/footage" CONFIG_PATH="$FIX/config" PORT=18080 AUTH_ENABLED=false
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

## Doctor

```bash
curl -sf "http://127.0.0.1:18080/ping"
curl -sf "http://127.0.0.1:18080/api/version"
test -f "$FIX/config/teslacam.db"
```

Worth driving only if `/ping` is `{"message":"pong"}`, `/api/version` returns a SemVer `latestVersion`, and the sqlite file exists.

## Drive

Harness: plain HTTP (`curl`) against `127.0.0.1` (never a `.local` hostname).

```bash
curl -sf "http://127.0.0.1:18080/api/library"
curl -sf "http://127.0.0.1:18080/api/library/files?id=CLIP_ID"
```

Also see `features/scanner.md` for H1/H2/M1/M2/L3 scanner proofs.

## Evidence

Write proof under `/opt/cursor/artifacts/` (survives cleanup):

- `go-test-full.log` — full `go test -race ./...`
- `go-vet.log` — `go vet ./...`
- `verify-scanner-http.log` — curl transcript when the scanner fixture path was used
