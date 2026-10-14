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
# STUB_ENCODE_EXIT: 0=encode OK (default), 2=SKIP no GPU, other=encode FAIL
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
			if [[ "$a" == *check-ffmpeg-nvenc-encode.sh ]]; then
				case "${STUB_ENCODE_EXIT:-0}" in
					0)
						echo "OK: h264_nvenc encode succeeded"
						exit 0
						;;
					2)
						echo "SKIP: h264_nvenc encode unavailable (no GPU driver/device injected)" >&2
						exit 2
						;;
					*)
						echo "ERROR: h264_nvenc encode failed (stub)" >&2
						exit "${STUB_ENCODE_EXIT}"
						;;
				esac
			fi
		done
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
ENCODE_CHECK="$ROOT/scripts/check-ffmpeg-nvenc-encode.sh"

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

# Stop version mock before encode unit tests.
kill "${MOCK_PIDS[0]}" >/dev/null 2>&1 || true
wait "${MOCK_PIDS[0]}" 2>/dev/null || true
MOCK_PIDS=("${MOCK_PIDS[@]:1}")

# --- 6) Encode probe: GPU presence decides SKIP; encode errors are FAIL ---
FFMPEG_STUB="$STUB_DIR/ffmpeg"

write_ffmpeg_msg_stub() {
	local msg="$1"
	cat >"$FFMPEG_STUB" <<EOF
#!/usr/bin/env bash
echo $(printf '%q' "$msg") >&2
exit 1
EOF
	chmod +x "$FFMPEG_STUB"
}

write_ffmpeg_ok_stub() {
	cat >"$FFMPEG_STUB" <<'EOF'
#!/usr/bin/env bash
exit 0
EOF
	chmod +x "$FFMPEG_STUB"
}

write_ffmpeg_empty_fail_stub() {
	cat >"$FFMPEG_STUB" <<'EOF'
#!/usr/bin/env bash
exit 1
EOF
	chmod +x "$FFMPEG_STUB"
}

assert_encode_fail() {
	local label="$1"
	set +e
	TESLAXY_NVENC_FORCE_GPU=1 FFMPEG_BIN="$FFMPEG_STUB" \
		"$ENCODE_CHECK" >"${LOG_DIR}/encode-must-fail.log" 2>&1
	ec=$?
	set -e
	if [[ "$ec" -eq 1 ]] && grep -q 'ERROR: h264_nvenc encode failed' "${LOG_DIR}/encode-must-fail.log" \
		&& ! grep -q '^SKIP:' "${LOG_DIR}/encode-must-fail.log"; then
		pass "encode FAIL (not SKIP): ${label}"
	else
		fail "expected FAIL exit 1 for '${label}' (got ${ec}); log: $(tr '\n' ' ' <"${LOG_DIR}/encode-must-fail.log")"
	fi
}

# OK with GPU present
write_ffmpeg_ok_stub
set +e
TESLAXY_NVENC_FORCE_GPU=1 FFMPEG_BIN="$FFMPEG_STUB" \
	"$ENCODE_CHECK" >"${LOG_DIR}/encode-ok.log" 2>&1
enc_ok=$?
set -e
[[ "$enc_ok" -eq 0 ]] && grep -q 'OK: h264_nvenc encode succeeded' "${LOG_DIR}/encode-ok.log" \
	&& pass "encode OK exits 0" \
	|| fail "encode OK expected exit 0 (got ${enc_ok})"

# Generic fail with GPU present
write_ffmpeg_msg_stub "bogus encoder init failure xyz"
assert_encode_fail "bogus encoder init"

# Classic present-GPU NVENC errors → FAIL
fail_msgs=(
	"Provided device does not support required NVENC features"
	"GPU device 0: encoder initialization failed: out of memory"
	"Driver does not support the required nvenc API version"
	"OpenEncodeSessionEx failed: out of memory"
)
for msg in "${fail_msgs[@]}"; do
	write_ffmpeg_msg_stub "$msg"
	assert_encode_fail "$msg"
done

# Mixed errors (OOM + libcuda, either order) → FAIL when GPU present
write_ffmpeg_msg_stub $'encoder initialization failed: out of memory\nCannot load libcuda.so.1'
assert_encode_fail "OOM then libcuda (mixed)"

write_ffmpeg_msg_stub $'Cannot load libcuda.so.1\nencoder initialization failed: out of memory'
assert_encode_fail "libcuda then OOM (mixed)"

# Quoted / option-like strings that look like no-GPU → still FAIL when GPU present
write_ffmpeg_msg_stub 'filter error: option "Cannot load libcuda.so.1" is invalid'
assert_encode_fail "quoted libcuda in filter error"

write_ffmpeg_msg_stub "Unknown option --hint=no CUDA-capable device"
assert_encode_fail "quoted no-CUDA in option error"

# Near misses must not SKIP when GPU present
write_ffmpeg_msg_stub "Cannot load libcuda.so.10"
assert_encode_fail "near-miss libcuda.so.10"

write_ffmpeg_msg_stub "CUDA_ERROR_NO_DEVICE_FAKE"
assert_encode_fail "near-miss CUDA_ERROR_NO_DEVICE_FAKE"

# Empty stderr + non-zero → FAIL when GPU present
write_ffmpeg_empty_fail_stub
assert_encode_fail "empty output non-zero exit"

# No GPU → SKIP before encode (ffmpeg stub unused / would fail if called)
write_ffmpeg_msg_stub "should not matter"
set +e
TESLAXY_NVENC_FORCE_GPU=0 FFMPEG_BIN="$FFMPEG_STUB" \
	"$ENCODE_CHECK" >"${LOG_DIR}/encode-skip-nogpu.log" 2>&1
enc_skip=$?
set -e
[[ "$enc_skip" -eq 2 ]] && grep -qi 'SKIP:.*no GPU' "${LOG_DIR}/encode-skip-nogpu.log" \
	&& pass "no GPU device → SKIP exit 2 (pre-encode)" \
	|| fail "no GPU expected SKIP exit 2 (got ${enc_skip})"

# Former no-driver ffmpeg strings with GPU forced → FAIL (text no longer drives SKIP)
for msg in "Cannot load libcuda.so.1" "CUDA_ERROR_NO_DEVICE" "no CUDA-capable device"; do
	write_ffmpeg_msg_stub "$msg"
	assert_encode_fail "GPU present + ffmpeg says: ${msg}"
done

# --- 7) Whole verify-docker-ffmpeg.sh exit codes for encode outcomes ---
OK_PORT=$((PORT + 3))
start_http_mock "$OK_PORT" "0" "pong" 200

set +e
STUB_ENCODE_EXIT=2 REQUIRE_NVENC_ENCODE=1 \
	PING_DEADLINE_SEC=4 CURL_MAX_TIME=2 \
	PORT="$OK_PORT" IMAGE_TAG=stub TMPDIR="$TMP_BASE" \
	"$VERIFY" >"${LOG_DIR}/verify-require-skip.log" 2>&1
req_ec=$?
set -e
if [[ "$req_ec" -ne 0 ]] && grep -q 'REQUIRE_NVENC_ENCODE' "${LOG_DIR}/verify-require-skip.log"; then
	pass "verify exit non-zero: REQUIRE_NVENC_ENCODE=1 + SKIP (exit=${req_ec})"
else
	fail "REQUIRE_NVENC_ENCODE=1 should fail on SKIP (exit=${req_ec}); log: $(tail -6 "${LOG_DIR}/verify-require-skip.log" | tr '\n' ' ')"
fi

set +e
STUB_ENCODE_EXIT=2 REQUIRE_NVENC_ENCODE=0 \
	PING_DEADLINE_SEC=4 CURL_MAX_TIME=2 \
	PORT="$OK_PORT" IMAGE_TAG=stub TMPDIR="$TMP_BASE" \
	"$VERIFY" >"${LOG_DIR}/verify-skip-ok.log" 2>&1
skip_ec=$?
set -e
if [[ "$skip_ec" -eq 0 ]] && grep -q '^SKIP: nvenc encode' "${LOG_DIR}/verify-skip-ok.log"; then
	pass "verify exit 0: SKIP without require + smoke ok"
else
	fail "SKIP without require should allow smoke success (exit=${skip_ec})"
fi

set +e
STUB_ENCODE_EXIT=1 REQUIRE_NVENC_ENCODE=0 \
	PING_DEADLINE_SEC=4 CURL_MAX_TIME=2 \
	PORT="$OK_PORT" IMAGE_TAG=stub TMPDIR="$TMP_BASE" \
	"$VERIFY" >"${LOG_DIR}/verify-encode-fail.log" 2>&1
vfail_ec=$?
set -e
if [[ "$vfail_ec" -ne 0 ]] && grep -q 'nvenc encode probe failed\|ERROR: nvenc encode' "${LOG_DIR}/verify-encode-fail.log"; then
	pass "verify exit non-zero: encode FAIL (STUB_ENCODE_EXIT=1, exit=${vfail_ec})"
else
	fail "encode FAIL should make verify exit non-zero (exit=${vfail_ec}); log: $(tail -8 "${LOG_DIR}/verify-encode-fail.log" | tr '\n' ' ')"
fi

set +e
STUB_ENCODE_EXIT=0 REQUIRE_NVENC_ENCODE=1 \
	PING_DEADLINE_SEC=4 CURL_MAX_TIME=2 \
	PORT="$OK_PORT" IMAGE_TAG=stub TMPDIR="$TMP_BASE" \
	"$VERIFY" >"${LOG_DIR}/verify-encode-ok.log" 2>&1
vok_ec=$?
set -e
if [[ "$vok_ec" -eq 0 ]] && grep -q 'OK: nvenc encode probe passed\|OK: h264_nvenc encode succeeded' "${LOG_DIR}/verify-encode-ok.log"; then
	pass "verify exit 0: encode OK + smoke"
else
	fail "encode OK should let verify succeed (exit=${vok_ec})"
fi

# --- 8) Wiring guards ---
if grep -q 'OWNED_FIX' "$VERIFY" \
	&& grep -q 'mktemp -d "${TMP_BASE}/teslaxy-docker-verify-' "$VERIFY" \
	&& grep -q 'now_ns\|deadline_ns' "$VERIFY" \
	&& grep -q 'deadline_ns' "$VERIFY" \
	&& grep -q 'REQUIRE_NVENC_ENCODE' "$VERIFY" \
	&& grep -q 'NVIDIA_VISIBLE_DEVICES' "$VERIFY" \
	&& grep -q 'check-ffmpeg-nvenc-encode.sh' "$VERIFY" \
	&& ! grep -q 'curl_get .*/api/version.*|| true' "$VERIFY" \
	&& grep -q 'NVIDIA_VISIBLE_DEVICES=all' "$ROOT/docker/docker-compose.yml" \
	&& grep -q 'NVIDIA_DRIVER_CAPABILITIES=compute,video,utility' "$ROOT/docker/docker-compose.yml" \
	&& grep -q 'REQUIRE_NVENC_ENCODE=1' "$ROOT/docker/docker-compose.yml" \
	&& grep -q 'gpu_present\|TESLAXY_NVENC_FORCE_GPU\|/dev/nvidia' "$ENCODE_CHECK"; then
	pass "verify + compose wire OWNED_FIX, ns deadline, NVIDIA env, pre-encode GPU check"
else
	fail "missing required ownership/deadline/NVIDIA/encode wiring"
fi

echo "All ${PASS} checks passed."
