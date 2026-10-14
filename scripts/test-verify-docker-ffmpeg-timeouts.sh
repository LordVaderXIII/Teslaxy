#!/usr/bin/env bash
# Check curl timeouts + FIX ownership for verify-docker-ffmpeg.sh.
# Uses a hanging mock TCP server and a stubbed `docker` on PATH so the real
# verify script can run without a Docker daemon.
set -euo pipefail

ROOT="$(cd "$(dirname "$0")/.." && pwd)"
VERIFY="$ROOT/scripts/verify-docker-ffmpeg.sh"
STUB_DIR="$(mktemp -d /tmp/teslaxy-docker-stub-XXXXXX)"
PASS=0

fail() { echo "FAIL: $*" >&2; exit 1; }
pass() { echo "PASS: $*"; PASS=$((PASS + 1)); }

cleanup() {
	if [[ -n "${MOCK_PID:-}" ]]; then
		kill "$MOCK_PID" >/dev/null 2>&1 || true
		wait "$MOCK_PID" 2>/dev/null || true
	fi
	rm -rf "$STUB_DIR"
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
		# Encoder check path: docker run --rm --entrypoint ...
		for a in "$@"; do
			if [[ "$a" == "--entrypoint" || "$a" == */check-ffmpeg-nvenc.sh ]]; then
				echo "stub: encoder check ok"
				exit 0
			fi
		done
		# Detached smoke container: docker run -d ...
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

# --- Hanging mock: accept TCP, never send an HTTP response ---
PORT="${TEST_PORT:-$((18100 + RANDOM % 200))}"
python3 - "$PORT" <<'PY' &
import socket, sys, time
port = int(sys.argv[1])
s = socket.socket()
s.setsockopt(socket.SOL_SOCKET, socket.SO_REUSEADDR, 1)
s.bind(("127.0.0.1", port))
s.listen(5)
# Accept and hold many connections (poll loop issues several curls).
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
MOCK_PID=$!

for _ in $(seq 1 50); do
	if (echo >/dev/tcp/127.0.0.1/"$PORT") >/dev/null 2>&1; then
		break
	fi
	sleep 0.1
done

# --- 1) curl against hang must exit 28 (operation timeout) ---
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
# Capture mktemp dir by wrapping mktemp briefly? Instead: run without FIX so
# the script creates OWNED_FIX; after failure it must be gone. We detect by
# snapshotting /tmp before/after for teslaxy-docker-verify-* owned by us.
before_list="$(mktemp)"
find /tmp -maxdepth 1 -name 'teslaxy-docker-verify-*' -user "$(id -un)" 2>/dev/null | sort >"$before_list" || true

start=$SECONDS
set +e
PING_DEADLINE_SEC=6 CURL_MAX_TIME=3 CURL_CONNECT_TIMEOUT=2 \
	PORT="$PORT" IMAGE_TAG=stub \
	"$VERIFY" >/tmp/verify-timeout-out.log 2>&1
verify_ec=$?
set -e
elapsed=$((SECONDS - start))

after_list="$(mktemp)"
find /tmp -maxdepth 1 -name 'teslaxy-docker-verify-*' -user "$(id -un)" 2>/dev/null | sort >"$after_list" || true

if [[ "$verify_ec" -ne 0 ]]; then
	pass "real verify script exited non-zero against hang (exit=${verify_ec})"
else
	fail "real verify script unexpectedly succeeded against hanging server"
fi

# Budget is 6s; each curl capped to remaining, so total should stay near deadline
# (allow one leftover max-time slack for the in-flight request).
if (( elapsed >= 6 && elapsed <= 6 + 3 + 2 )); then
	pass "real verify script respected overall deadline (~${elapsed}s for budget=6s)"
else
	fail "real verify script elapsed ${elapsed}s; expected ~6–11s"
fi

# No new leftover owned dirs (OWNED_FIX removed on EXIT).
if ! comm -13 "$before_list" "$after_list" | grep -q .; then
	pass "OWNED_FIX from real verify script was removed on EXIT"
else
	fail "leftover OWNED_FIX dirs: $(comm -13 "$before_list" "$after_list" | tr '\n' ' ')"
fi
rm -f "$before_list" "$after_list"

# --- 3) Caller-supplied FIX matching the name pattern must survive ---
CALLER_FIX="/tmp/teslaxy-docker-verify-caller-must-survive-$$"
mkdir -p "$CALLER_FIX"
echo survive >"$CALLER_FIX/marker"

set +e
PING_DEADLINE_SEC=4 CURL_MAX_TIME=2 CURL_CONNECT_TIMEOUT=1 \
	PORT="$PORT" IMAGE_TAG=stub FIX="$CALLER_FIX" \
	"$VERIFY" >/tmp/verify-caller-fix-out.log 2>&1
set -e

if [[ -d "$CALLER_FIX" && -f "$CALLER_FIX/marker" ]]; then
	pass "caller-supplied FIX survived real verify script EXIT ($CALLER_FIX)"
else
	fail "caller-supplied FIX was deleted by verify script"
fi
rm -rf "$CALLER_FIX"

# --- 4) Wiring guards ---
if grep -q 'OWNED_FIX' "$VERIFY" \
	&& grep -q 'mktemp -d /tmp/teslaxy-docker-verify-' "$VERIFY" \
	&& grep -q 'remaining' "$VERIFY" \
	&& grep -q 'succeeded after the' "$VERIFY"; then
	pass "verify-docker-ffmpeg.sh wires OWNED_FIX, mktemp, remaining budget, late-success reject"
else
	fail "verify-docker-ffmpeg.sh missing required ownership/deadline wiring"
fi

echo "All ${PASS} checks passed."
