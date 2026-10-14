#!/usr/bin/env bash
# Verify the prod image: build, encoder check, health smoke.
# Usage (from repo root):
#   ./scripts/verify-docker-ffmpeg.sh
#   IMAGE_TAG=teslaxy:local ./scripts/verify-docker-ffmpeg.sh
#   FIX=/path/to/existing/dir ./scripts/verify-docker-ffmpeg.sh   # caller-owned; not deleted
set -euo pipefail

ROOT="$(cd "$(dirname "$0")/.." && pwd)"
IMAGE_TAG="${IMAGE_TAG:-teslaxy:ffmpeg-check}"
PORT="${PORT:-18080}"
# Per-request curl limits (seconds). A server that accepts but never replies
# must not hang the smoke script forever.
CURL_CONNECT_TIMEOUT="${CURL_CONNECT_TIMEOUT:-2}"
CURL_MAX_TIME="${CURL_MAX_TIME:-5}"
# Overall poll budget for /ping becoming ready (seconds).
PING_DEADLINE_SEC="${PING_DEADLINE_SEC:-60}"

TMP_BASE="${TMPDIR:-/tmp}"
TMP_BASE="${TMP_BASE%/}"

# Only a directory this script creates via mktemp is deleted on EXIT.
# A caller-supplied FIX is used as-is and never removed.
OWNED_FIX=""
if [[ -n "${FIX:-}" ]]; then
	:
else
	FIX="$(mktemp -d "${TMP_BASE}/teslaxy-docker-verify-XXXXXX")"
	OWNED_FIX="$FIX"
fi

# Nanosecond monotonic-ish wall clock for sub-second deadline accuracy.
# bash SECONDS is whole seconds only and can miss a late pong.
now_ns() {
	date +%s%N
}

# Set once before polling; curl_get caps --max-time to remaining budget.
deadline_ns=0

remaining_ns() {
	echo $((deadline_ns - $(now_ns)))
}

# Format remaining nanoseconds as a curl --max-time value (seconds, 3dp),
# capped by $1 (usually CURL_MAX_TIME), at least 0.001s.
curl_time_budget() {
	local cap="$1"
	local rem
	rem="$(remaining_ns)"
	if (( rem <= 0 )); then
		echo "0"
		return
	fi
	awk -v ns="$rem" -v cap="$cap" 'BEGIN {
		s = ns / 1e9
		if (s > cap) s = cap
		if (s < 0.001) s = 0.001
		printf "%.3f", s
	}'
}

curl_get() {
	local url="$1"
	local rem
	rem="$(remaining_ns)"
	if (( rem <= 0 )); then
		return 28
	fi
	local max_time connect
	max_time="$(curl_time_budget "$CURL_MAX_TIME")"
	if [[ "$max_time" == "0" ]]; then
		return 28
	fi
	connect="$(curl_time_budget "$CURL_CONNECT_TIMEOUT")"
	curl -sf \
		--connect-timeout "$connect" \
		--max-time "$max_time" \
		"$url"
}

cleanup() {
	if [[ -n "${CID:-}" ]]; then
		docker rm -f "$CID" >/dev/null 2>&1 || true
	fi
	# Delete only the mktemp dir we created — never a caller-supplied FIX.
	if [[ -n "${OWNED_FIX:-}" ]]; then
		rm -rf "$OWNED_FIX"
	fi
}
trap cleanup EXIT

echo "==> docker build -f docker/Dockerfile -t ${IMAGE_TAG} ."
docker build -f "$ROOT/docker/Dockerfile" -t "$IMAGE_TAG" "$ROOT"

echo "==> encoder check inside image"
docker run --rm --entrypoint /usr/local/share/teslaxy/check-ffmpeg-nvenc.sh "$IMAGE_TAG"

echo "==> container smoke (/ping)"
mkdir -p "$FIX/footage/RecentClips" "$FIX/config"
dd if=/dev/zero of="$FIX/footage/RecentClips/seed.mp4" bs=1024 count=4 status=none
CID="$(docker run -d --rm \
	-p "127.0.0.1:${PORT}:80" \
	-e AUTH_ENABLED=false \
	-e GIN_MODE=release \
	-e RETENTION_ENABLED=false \
	-e RETENTION_DRY_RUN=true \
	-v "$FIX/footage:/footage:ro" \
	-v "$FIX/config:/config" \
	"$IMAGE_TAG")"

deadline_ns=$(( $(now_ns) + PING_DEADLINE_SEC * 1000000000 ))
while (( $(now_ns) < deadline_ns )); do
	body=""
	if body="$(curl_get "http://127.0.0.1:${PORT}/ping" 2>/dev/null)"; then
		# Reject a reply that arrives at or after the deadline (ns clock).
		if (( $(now_ns) >= deadline_ns )); then
			echo "ERROR: /ping succeeded after the ${PING_DEADLINE_SEC}s deadline" >&2
			docker logs "$CID" >&2 || true
			exit 1
		fi
		if printf '%s\n' "$body" | grep -q pong; then
			echo "ping ok"
			curl_get "http://127.0.0.1:${PORT}/api/version"
			echo
			exit 0
		fi
	fi
	rem="$(remaining_ns)"
	if (( rem <= 0 )); then
		break
	fi
	# Don't sleep past the deadline (cap at 0.5s).
	sleep_s="$(awk -v ns="$rem" 'BEGIN {
		s = ns / 1e9
		if (s > 0.5) s = 0.5
		if (s < 0.001) s = 0.001
		printf "%.3f", s
	}')"
	sleep "$sleep_s"
done

echo "ERROR: /ping did not become ready within ${PING_DEADLINE_SEC}s" >&2
docker logs "$CID" >&2 || true
exit 1
