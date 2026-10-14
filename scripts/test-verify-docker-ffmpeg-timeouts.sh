#!/usr/bin/env bash
# Check curl timeouts + FIX ownership for verify-docker-ffmpeg.sh.
# Uses real mock HTTP/TCP servers and a stubbed `docker` on PATH so the real
# verify script can run without a Docker daemon.
set -euo pipefail

ROOT="$(cd "$(dirname "$0")/.." && pwd)"
VERIFY="$ROOT/scripts/verify-docker-ffmpeg.sh"
TMP_BASE="${TMPDIR:-/tmp}"
TMP_BASE="${TMP_BASE%/}"
STUB_DIR="$(mktemp -d "${TMP_BASE}/teslaxy-docker-stub-XXXXXX")"
LOG_DIR="$(mktemp -d "${TMP_BASE}/teslaxy-verify-test-logs-XXXXXX")"
PASS=0
MOCK_PIDS=()

fail() { echo "FAIL: $*" >&2; exit 1; }
pass() { echo "PASS: $*"; PASS=$((PASS + 1)); }

cleanup() {
	local pid
	for pid in "${MOCK_PIDS[@]:-}"; do
		kill "$pid" >/dev/null 2>&1 || true
		wait "$pid" 2>/dev/null || true
	done
	rm -rf "$STUB_DIR" "$LOG_DIR"
}
trap cleanup EXIT

# --- Stub docker: no daemon required ---
cat >"$STUB_DIR/docker" <<'EOF'
#!/usr/bin/env bash
set -euo pipefail
cmd="${1:-}"
shift || true
case "$cmd" in
	build)
		echo "stub: docker build ok" >&2
		exit 0
		;;
	run)
		for a in "$@"; do
			if [[ "$a" == "--entrypoint" || "$a" == */check-ffmpeg-nvenc.sh ]]; then
				echo "stub: encoder check ok"
				exit 0
			fi
		done
		for a in "$@"; do
			if [[ "$a" == "-d" ]]; then
				echo "stub-cid-$$"
				exit 0
			fi
		done
		echo "stub docker run: unhandled args: $*" >&2
		exit 1
		;;
	rm|logs)
		exit 0
		;;
	*)
		echo "stub docker: unhandled command: $cmd $*" >&2
		exit 1
		;;
esac
EOF
chmod +x "$STUB_DIR/docker"
export PATH="$STUB_DIR:$PATH"

wait_port() {
	local port="$1"
	local i
	for i in $(seq 1 50); do
		if (echo >/dev/tcp/127.0.0.1/"$port") >/dev/null 2>&1; then
			return 0
		fi
		sleep 0.1
	done
	return 1
}

start_hang_server() {
	local port="$1"
	python3 - "$port" <<'PY' &
import socket, sys, time
port = int(sys.argv[1])
s = socket.socket()
s.setsockopt(socket.SOL_SOCKET, socket.SO_REUSEADDR, 1)
s.bind(("127.0.0.1", port))
s.listen(5)
conns = []
try:
	while True:
		s.settimeout(1.0)
		try:
			conn, _ = s.accept()
			conns.append(conn)
		except socket.timeout:
			pass
		time.sleep(0.05)
except Exception:
	pass
finally:
	for c in conns:
		try:
			c.close()
		except Exception:
			pass
	s.close()
PY
	MOCK_PIDS+=($!)
	wait_port "$port" || fail "hang server did not bind on ${port}"
}

start_http_mock() {
	# Args: port delay_sec ping_body version_status
	# Serves /ping after delay_sec with ping_body; /api/version with version_status.
	local port="$1" delay="$2" ping_body="$3" version_status="$4"
	python3 - "$port" "$delay" "$ping_body" "$version_status" <<'PY' &
import socket, sys, time
port = int(sys.argv[1])
delay = float(sys.argv[2])
ping_body = sys.argv[3]
version_status = int(sys.argv[4])
s = socket.socket()
s.setsockopt(socket.SOL_SOCKET, socket.SO_REUSEADDR, 1)
s.bind(("127.0.0.1", port))
s.listen(5)

def read_req(conn):
	data = b""
	conn.settimeout(5.0)
	while b"\r\n\r\n" not in data:
		chunk = conn.recv(4096)
		if not chunk:
			break
		data += chunk
	return data.decode("utf-8", "replace")

try:
	while True:
		conn, _ = s.accept()
		try:
			req = read_req(conn)
			path = "/"
			if req:
				line = req.split("\r\n", 1)[0]
				parts = line.split()
				if len(parts) >= 2:
					path = parts[1].split("?", 1)[0]
			if path.startswith("/ping"):
				if delay > 0:
					time.sleep(delay)
				body = ping_body.encode()
				hdr = (
					"HTTP/1.1 200 OK\r\n"
					f"Content-Length: {len(body)}\r\n"
					"Content-Type: text/plain\r\n"
					"Connection: close\r\n\r\n"
				).encode()
				conn.sendall(hdr + body)
			elif path.startswith("/api/version"):
				if version_status == 0:
					# Hang: accept, never reply (failed version probe).
					time.sleep(60)
				else:
					body = b'{"latestVersion":"v0.0.0"}'
					if version_status >= 400:
						body = b'{"error":"nope"}'
					reason = "OK" if version_status < 400 else "Error"
					hdr = (
						f"HTTP/1.1 {version_status} {reason}\r\n"
						f"Content-Length: {len(body)}\r\n"
						"Content-Type: application/json\r\n"
						"Connection: close\r\n\r\n"
					).encode()
					conn.sendall(hdr + body)
			else:
				conn.sendall(b"HTTP/1.1 404 Not Found\r\nContent-Length: 0\r\nConnection: close\r\n\r\n")
		finally:
			try:
				conn.close()
			except Exception:
				pass
except Exception:
	pass
finally:
	s.close()
PY
	MOCK_PIDS+=($!)
	wait_port "$port" || fail "http mock did not bind on ${port}"
}

# --- 1) curl against hang must exit 28 (operation timeout) ---
PORT="${TEST_PORT:-$((18100 + RANDOM % 200))}"
start_hang_server "$PORT"

CURL_CONNECT_TIMEOUT=2
CURL_MAX_TIME=3
start=$SECONDS
set +e
curl -sf --connect-timeout "$CURL_CONNECT_TIMEOUT" --max-time "$CURL_MAX_TIME" \
	"http://127.0.0.1:${PORT}/ping" >/dev/null 2>&1
curl_ec=$?
set -e
elapsed=$((SECONDS - start))
if [[ "$curl_ec" -eq 28 ]]; then
	pass "hanging server: curl exit 28 in ${elapsed}s (max-time=${CURL_MAX_TIME})"
else
	fail "expected curl exit 28 against hanging server (got ${curl_ec}, elapsed=${elapsed}s)"
fi

# --- 2) Real verify script: overall deadline + owned FIX cleaned ---
before_list="$(mktemp "${TMP_BASE}/teslaxy-before-XXXXXX")"
find "$TMP_BASE" -maxdepth 1 -name 'teslaxy-docker-verify-*' -user "$(id -un)" 2>/dev/null | sort >"$before_list" || true

start=$SECONDS
set +e
PING_DEADLINE_SEC=6 CURL_MAX_TIME=3 CURL_CONNECT_TIMEOUT=2 \
	PORT="$PORT" IMAGE_TAG=stub TMPDIR="$TMP_BASE" \
	"$VERIFY" >"${LOG_DIR}/verify-timeout-out.log" 2>&1
verify_ec=$?
set -e
elapsed=$((SECONDS - start))

after_list="$(mktemp "${TMP_BASE}/teslaxy-after-XXXXXX")"
find "$TMP_BASE" -maxdepth 1 -name 'teslaxy-docker-verify-*' -user "$(id -un)" 2>/dev/null | sort >"$after_list" || true

if [[ "$verify_ec" -ne 0 ]]; then
	pass "real verify script exited non-zero against hang (exit=${verify_ec})"
else
	fail "real verify script unexpectedly succeeded against hanging server"
fi

if (( elapsed >= 6 && elapsed <= 6 + 3 + 2 )); then
	pass "real verify script respected overall deadline (~${elapsed}s for budget=6s)"
else
	fail "real verify script elapsed ${elapsed}s; expected ~6–11s"
fi

if ! comm -13 "$before_list" "$after_list" | grep -q .; then
	pass "OWNED_FIX from real verify script was removed on EXIT"
else
	fail "leftover OWNED_FIX dirs: $(comm -13 "$before_list" "$after_list" | tr '\n' ' ')"
fi
rm -f "$before_list" "$after_list"

# --- 3) Caller-supplied FIX matching the name pattern must survive ---
CALLER_FIX="$(mktemp -d "${TMP_BASE}/teslaxy-docker-verify-caller-XXXXXX")"
echo survive >"$CALLER_FIX/marker"

set +e
PING_DEADLINE_SEC=4 CURL_MAX_TIME=2 CURL_CONNECT_TIMEOUT=1 \
	PORT="$PORT" IMAGE_TAG=stub FIX="$CALLER_FIX" TMPDIR="$TMP_BASE" \
	"$VERIFY" >"${LOG_DIR}/verify-caller-fix-out.log" 2>&1
set -e

if [[ -d "$CALLER_FIX" && -f "$CALLER_FIX/marker" ]]; then
	pass "caller-supplied FIX survived real verify script EXIT ($CALLER_FIX)"
else
	fail "caller-supplied FIX was deleted by verify script"
fi
rm -rf "$CALLER_FIX"

# Stop hang server before HTTP mocks reuse ports.
kill "${MOCK_PIDS[0]}" >/dev/null 2>&1 || true
wait "${MOCK_PIDS[0]}" 2>/dev/null || true
MOCK_PIDS=("${MOCK_PIDS[@]:1}")

# --- 4) Late pong past a sub-second-granularity deadline must fail ---
# Deadline=1s; server returns pong at 1.2s. Whole-second SECONDS + `>` would
# miss this; ns clock + `>=` must reject it.
LATE_PORT=$((PORT + 1))
start_http_mock "$LATE_PORT" "1.2" "pong" 200

set +e
PING_DEADLINE_SEC=1 CURL_MAX_TIME=5 CURL_CONNECT_TIMEOUT=2 \
	PORT="$LATE_PORT" IMAGE_TAG=stub TMPDIR="$TMP_BASE" \
	"$VERIFY" >"${LOG_DIR}/verify-late-pong.log" 2>&1
late_ec=$?
set -e
if [[ "$late_ec" -ne 0 ]] && grep -q 'after the .* deadline\|did not become ready' "${LOG_DIR}/verify-late-pong.log"; then
	pass "late pong past 1s deadline failed (exit=${late_ec})"
else
	fail "late pong should fail deadline (exit=${late_ec}); log: $(tail -5 "${LOG_DIR}/verify-late-pong.log" | tr '\n' ' ')"
fi

kill "${MOCK_PIDS[0]}" >/dev/null 2>&1 || true
wait "${MOCK_PIDS[0]}" 2>/dev/null || true
MOCK_PIDS=("${MOCK_PIDS[@]:1}")

# --- 5) Failed /api/version must exit non-zero (no || true) ---
VER_PORT=$((PORT + 2))
# Immediate pong; version hangs so curl --max-time fails under remaining budget.
start_http_mock "$VER_PORT" "0" "pong" 0

set +e
PING_DEADLINE_SEC=8 CURL_MAX_TIME=2 CURL_CONNECT_TIMEOUT=1 \
	PORT="$VER_PORT" IMAGE_TAG=stub TMPDIR="$TMP_BASE" \
	"$VERIFY" >"${LOG_DIR}/verify-version-fail.log" 2>&1
ver_ec=$?
set -e
if [[ "$ver_ec" -ne 0 ]] && grep -q 'ping ok' "${LOG_DIR}/verify-version-fail.log"; then
	pass "failed /api/version exits non-zero after ping ok (exit=${ver_ec})"
else
	fail "version failure should exit non-zero after ping (exit=${ver_ec}); log: $(tail -8 "${LOG_DIR}/verify-version-fail.log" | tr '\n' ' ')"
fi

# --- 6) Wiring guards ---
if grep -q 'OWNED_FIX' "$VERIFY" \
	&& grep -q 'mktemp -d "${TMP_BASE}/teslaxy-docker-verify-' "$VERIFY" \
	&& grep -q 'now_ns\|deadline_ns' "$VERIFY" \
	&& grep -q 'SECONDS\|now_ns' "$VERIFY" \
	&& grep -q '>= deadline_ns\|>= \$deadline_ns\|deadline_ns ))' "$VERIFY" \
	&& ! grep -q 'curl_get .*/api/version.*|| true' "$VERIFY"; then
	pass "verify-docker-ffmpeg.sh wires OWNED_FIX, TMPDIR mktemp, ns deadline, no version || true"
else
	fail "verify-docker-ffmpeg.sh missing required ownership/deadline/version wiring"
fi

echo "All ${PASS} checks passed."
