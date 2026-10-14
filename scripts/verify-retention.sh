#!/usr/bin/env bash
# Prove footage retention: unit tests + fixture dry-run (+ optional HTTP).
set -euo pipefail

ROOT="$(cd "$(dirname "$0")/.." && pwd)"
if [[ -n "${ARTIFACTS_DIR:-}" ]]; then
  ART="$ARTIFACTS_DIR"
else
  ART="$(mktemp -d "${TMPDIR:-/tmp}/teslaxy-verify-artifacts.XXXXXX")"
  echo "verify-retention: ARTIFACTS_DIR unset; using temp dir $ART"
fi
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

  # Retention writes always require JWT + CSRF, even when AUTH_ENABLED=false.
  PASS="$(grep -oE 'Auto-generated admin password: [0-9a-f]+' "$FIX/server.log" | awk '{print $NF}' | tail -1 || true)"
  TOKEN=""
  if [[ -n "$PASS" ]]; then
    TOKEN="$(curl -sf -X POST "http://127.0.0.1:18080/api/login" \
      -H 'Content-Type: application/json' \
      -d "{\"username\":\"admin\",\"password\":\"$PASS\"}" | python3 -c 'import sys,json; print(json.load(sys.stdin).get("token",""))')"
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
    echo "POST /api/retention/run without auth (expect 401)"
    curl -s -o /tmp/ret_unauth.out -w "%{http_code}" -X POST "http://127.0.0.1:18080/api/retention/run" \
      -H 'Content-Type: application/json' -d '{"dry_run":true}'
    echo
    if [[ -n "$TOKEN" ]]; then
      echo "POST /api/retention/run dry_run (authed)"
      curl -sf -X POST "http://127.0.0.1:18080/api/retention/run" \
        -H 'Content-Type: application/json' \
        -H "Authorization: Bearer $TOKEN" \
        -H 'X-Requested-With: XMLHttpRequest' \
        -d '{"dry_run":true}'
      echo
    else
      echo "SKIP authed run (could not parse admin password from server log)"
    fi
  } | tee "$ART/verify-retention-http.log"
fi

echo "verify-retention: OK (artifacts in $ART)"
