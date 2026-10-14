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

# Only a directory this script creates via mktemp is deleted on EXIT.
# A caller-supplied FIX is used as-is and never removed.
OWNED_FIX=""
if [[ -n "${FIX:-}" ]]; then
	:
else
	FIX="$(mktemp -d /tmp/teslaxy-docker-verify-XXXXXX)"
	OWNED_FIX="$FIX"
fi

# Set once before polling; curl_get caps --max-time to remaining budget.
deadline=0

curl_get() {
	local url="$1"
	local remaining=$((deadline - SECONDS))
	if (( remaining < 1 )); then
		return 28
	fi
	local max_time="$CURL_MAX_TIME"
	if (( remaining < max_time )); then
		max_time=$remaining
	fi
	if (( max_time < 1 )); then
		max_time=1
	fi
	local connect="$CURL_CONNECT_TIMEOUT"
	if (( connect > max_time )); then
		connect=$max_time
	fi
	if (( connect < 1 )); then
		connect=1
	fi
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

deadline=$((SECONDS + PING_DEADLINE_SEC))
while (( SECONDS < deadline )); do
	body=""
	if body="$(curl_get "http://127.0.0.1:${PORT}/ping" 2>/dev/null)"; then
		# Cap each request to the remaining budget; still reject a late reply.
		if (( SECONDS > deadline )); then
			echo "ERROR: /ping succeeded after the ${PING_DEADLINE_SEC}s deadline" >&2
			docker logs "$CID" >&2 || true
			exit 1
		fi
		if printf '%s\n' "$body" | grep -q pong; then
			echo "ping ok"
			curl_get "http://127.0.0.1:${PORT}/api/version" || true
			echo
			exit 0
		fi
	fi
	remaining=$((deadline - SECONDS))
	if (( remaining <= 0 )); then
		break
	fi
	# Don't sleep past the deadline.
	if (( remaining > 0 )); then
		sleep 0.5
	fi
done

echo "ERROR: /ping did not become ready within ${PING_DEADLINE_SEC}s" >&2
docker logs "$CID" >&2 || true
exit 1
