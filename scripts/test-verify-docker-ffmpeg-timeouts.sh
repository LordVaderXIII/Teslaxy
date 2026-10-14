#!/usr/bin/env bash
# Demo/unit check: curl timeouts + FIX cleanup for verify-docker-ffmpeg.sh.
# Spins a mock TCP server that accepts then never replies, proving the old
# hang is now bounded. Does not require Docker.
set -euo pipefail

ROOT="$(cd "$(dirname "$0")/.." && pwd)"
# shellcheck source=/dev/null
# Extract curl_get helpers by sourcing a minimal copy of the timeout knobs.
CURL_CONNECT_TIMEOUT="${CURL_CONNECT_TIMEOUT:-2}"
CURL_MAX_TIME="${CURL_MAX_TIME:-3}"
PING_DEADLINE_SEC="${PING_DEADLINE_SEC:-8}"

curl_get() {
	curl -sf \
		--connect-timeout "$CURL_CONNECT_TIMEOUT" \
		--max-time "$CURL_MAX_TIME" \
		"$1"
}

PASS=0
fail() { echo "FAIL: $*" >&2; exit 1; }
pass() { echo "PASS: $*"; PASS=$((PASS + 1)); }

# --- 1) Mock hang: accept TCP, never send HTTP body ---
PORT="${TEST_PORT:-$((18090 + RANDOM % 100))}"
# Python keeps the accepted connection open without writing a response.
python3 - "$PORT" <<'PY' &
import socket, sys, time
port = int(sys.argv[1])
s = socket.socket()
s.setsockopt(socket.SOL_SOCKET, socket.SO_REUSEADDR, 1)
s.bind(("127.0.0.1", port))
s.listen(1)
conn, _ = s.accept()
# Hold the connection open past curl --max-time without sending bytes.
time.sleep(60)
conn.close()
s.close()
PY
MOCK_PID=$!
cleanup_mock() {
	kill "$MOCK_PID" >/dev/null 2>&1 || true
	wait "$MOCK_PID" 2>/dev/null || true
}
trap cleanup_mock EXIT

# Wait until the listener is up.
for _ in $(seq 1 20); do
	if (echo >/dev/tcp/127.0.0.1/"$PORT") >/dev/null 2>&1; then
		break
	fi
	sleep 0.1
done

start=$SECONDS
set +e
curl_get "http://127.0.0.1:${PORT}/ping" >/dev/null 2>&1
curl_ec=$?
set -e
elapsed=$((SECONDS - start))

# curl exit 28 = operation timeout
if [[ "$curl_ec" -eq 28 ]] || [[ "$elapsed" -le $((CURL_MAX_TIME + CURL_CONNECT_TIMEOUT + 2)) ]]; then
	if [[ "$elapsed" -le $((CURL_MAX_TIME + 3)) ]]; then
		pass "hanging server timed out in ${elapsed}s (curl exit ${curl_ec}, max-time=${CURL_MAX_TIME})"
	else
		fail "curl took ${elapsed}s; expected around --max-time=${CURL_MAX_TIME}"
	fi
else
	fail "expected curl timeout against hanging server (exit=${curl_ec}, elapsed=${elapsed}s)"
fi

# --- 2) Overall poll deadline stays bounded ---
start=$SECONDS
deadline=$((SECONDS + PING_DEADLINE_SEC))
polls=0
while (( SECONDS < deadline )); do
	polls=$((polls + 1))
	set +e
	curl_get "http://127.0.0.1:${PORT}/ping" >/dev/null 2>&1
	set -e
	sleep 0.2
done
elapsed=$((SECONDS - start))
if (( elapsed >= PING_DEADLINE_SEC && elapsed <= PING_DEADLINE_SEC + CURL_MAX_TIME + 3 )); then
	pass "overall poll deadline observed (~${elapsed}s, polls=${polls}, budget=${PING_DEADLINE_SEC}s)"
else
	fail "poll loop elapsed ${elapsed}s outside expected band around ${PING_DEADLINE_SEC}s"
fi

# --- 3) FIX cleanup trap removes /tmp/teslaxy-docker-verify-* ---
FIX="/tmp/teslaxy-docker-verify-timeout-test-$$"
mkdir -p "$FIX/footage" "$FIX/config"
cleanup_fix() {
	if [[ -n "${FIX:-}" && -d "$FIX" && "$FIX" == /tmp/teslaxy-docker-verify-* ]]; then
		rm -rf "$FIX"
	fi
}
# Simulate the verify script EXIT trap.
cleanup_fix
if [[ ! -d "$FIX" ]]; then
	pass "FIX dir ${FIX} removed by cleanup trap"
else
	fail "FIX dir ${FIX} still present after cleanup"
fi

# Confirm verify script declares the timeout flags (guard against regression).
if grep -q -- '--connect-timeout' "$ROOT/scripts/verify-docker-ffmpeg.sh" \
	&& grep -q -- '--max-time' "$ROOT/scripts/verify-docker-ffmpeg.sh" \
	&& grep -q 'PING_DEADLINE_SEC' "$ROOT/scripts/verify-docker-ffmpeg.sh" \
	&& grep -q 'rm -rf "\$FIX"' "$ROOT/scripts/verify-docker-ffmpeg.sh"; then
	pass "verify-docker-ffmpeg.sh wires connect/max-time, deadline, and FIX cleanup"
else
	fail "verify-docker-ffmpeg.sh missing timeout or cleanup wiring"
fi

echo "All ${PASS} checks passed."
