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

cleanup() {
	if [[ -n "${CID:-}" ]]; then
		docker rm -f "$CID" >/dev/null 2>&1 || true
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

for i in $(seq 1 60); do
	if curl -sf "http://127.0.0.1:${PORT}/ping" | grep -q pong; then
		echo "ping ok"
		curl -sf "http://127.0.0.1:${PORT}/api/version"
		echo
		exit 0
	fi
	sleep 0.5
done

echo "ERROR: /ping did not become ready" >&2
docker logs "$CID" >&2 || true
exit 1
