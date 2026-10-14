#!/usr/bin/env bash
# Prove footage retention: unit tests + fixture dry-run (+ optional HTTP).
set -euo pipefail

ROOT="$(cd "$(dirname "$0")/.." && pwd)"
ART="${ARTIFACTS_DIR:-/opt/cursor/artifacts}"
mkdir -p "$ART"
HTTP=0
if [[ "${1:-}" == "--http" ]]; then
  HTTP=1
fi

echo "== go test ./... =="
(
  cd "$ROOT/backend"
  go test ./... -count=1
) | tee "$ART/go-test-full.log"

echo "== retention fixture dry-run =="
(
  cd "$ROOT/backend"
  go run ./cmd/retention-dry-run
) >"$ART/retention-dry-run.json" 2>"$ART/retention-dry-run.stderr"
cat "$ART/retention-dry-run.stderr" >&2
# Ensure dry-run left deleted empty
python3 - <<'PY' "$ART/retention-dry-run.json"
import json, sys
report = json.load(open(sys.argv[1]))
assert report.get("dry_run") is True, report
assert report.get("deleted") == [], report
assert isinstance(report.get("candidates"), list), report
print("dry-run assertion ok: candidates=%d bytes_freed=%s" % (
    len(report["candidates"]), report.get("bytes_freed")))
PY

if [[ "$HTTP" -eq 1 ]]; then
  FIX="${TMPDIR:-/tmp}/teslaxy-verify-$$"
  mkdir -p "$FIX/footage/RecentClips" "$FIX/config"
  dd if=/dev/zero of="$FIX/footage/RecentClips/seed.mp4" bs=1024 count=4 status=none
  (
    cd "$ROOT/backend"
    go build -o "$FIX/teslaxy" .
  )
  export FOOTAGE_PATH="$FIX/footage" CONFIG_PATH="$FIX/config" PORT=18080 AUTH_ENABLED=false
  export RETENTION_ENABLED=false RETENTION_DRY_RUN=true
  "$FIX/teslaxy" >"$FIX/server.log" 2>&1 &
  echo $! >"$FIX/server.pid"
  cleanup() {
    if [[ -f "$FIX/server.pid" ]]; then
      kill "$(cat "$FIX/server.pid")" 2>/dev/null || true
    fi
    rm -rf "$FIX"
  }
  trap cleanup EXIT

  ok=0
  for _ in $(seq 1 50); do
    if curl -sf "http://127.0.0.1:18080/ping" | grep -q pong; then
      ok=1
      break
    fi
    sleep 0.1
  done
  if [[ "$ok" -ne 1 ]]; then
    echo "server failed to become ready" >&2
    cat "$FIX/server.log" >&2 || true
    exit 1
  fi

  {
    echo "GET /ping"
    curl -sf "http://127.0.0.1:18080/ping"
    echo
    echo "GET /api/version"
    curl -sf "http://127.0.0.1:18080/api/version"
    echo
    echo "GET /api/retention/settings"
    curl -sf "http://127.0.0.1:18080/api/retention/settings"
    echo
    echo "POST /api/retention/run dry_run"
    curl -sf -X POST "http://127.0.0.1:18080/api/retention/run" \
      -H 'Content-Type: application/json' \
      -d '{"dry_run":true}'
    echo
  } | tee "$ART/verify-retention-http.log"
fi

echo "verify-retention: OK (artifacts in $ART)"
