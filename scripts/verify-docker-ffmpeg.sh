#!/usr/bin/env bash
# Verify the prod image: build, encoder check, health smoke.
# Usage (from repo root):
#   ./scripts/verify-docker-ffmpeg.sh
#   IMAGE_TAG=teslaxy:local ./scripts/verify-docker-ffmpeg.sh
set -euo pipefail

ROOT="$(cd "$(dirname "$0")/.." && pwd)"
IMAGE_TAG="${IMAGE_TAG:-teslaxy:ffmpeg-check}"
PORT="${PORT:-18080}"
FIX="${FIX:-/tmp/teslaxy-docker-verify-$$}"
# Per-request curl limits (seconds). A server that accepts but never replies
# must not hang the smoke script forever.
CURL_CONNECT_TIMEOUT="${CURL_CONNECT_TIMEOUT:-2}"
CURL_MAX_TIME="${CURL_MAX_TIME:-5}"
# Overall poll budget for /ping becoming ready (seconds).
PING_DEADLINE_SEC="${PING_DEADLINE_SEC:-60}"

curl_get() {
	curl -sf \
		--connect-timeout "$CURL_CONNECT_TIMEOUT" \
		--max-time "$CURL_MAX_TIME" \
		"$1"
}

cleanup() {
	if [[ -n "${CID:-}" ]]; then
		docker rm -f "$CID" >/dev/null 2>&1 || true
	fi
	# Always drop the fixture tree we created under /tmp/teslaxy-docker-verify-*.
	if [[ -n "${FIX:-}" && -d "$FIX" && "$FIX" == /tmp/teslaxy-docker-verify-* ]]; then
		rm -rf "$FIX"
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
	if curl_get "http://127.0.0.1:${PORT}/ping" | grep -q pong; then
		echo "ping ok"
		curl_get "http://127.0.0.1:${PORT}/api/version"
		echo
		exit 0
	fi
	sleep 0.5
done

echo "ERROR: /ping did not become ready within ${PING_DEADLINE_SEC}s" >&2
docker logs "$CID" >&2 || true
exit 1
