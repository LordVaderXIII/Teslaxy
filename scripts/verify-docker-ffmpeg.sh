#!/usr/bin/env bash
# Verify the prod image: build, encoder list, real nvenc encode, health smoke.
# Usage (from repo root):
#   ./scripts/verify-docker-ffmpeg.sh
#   IMAGE_TAG=teslaxy:local ./scripts/verify-docker-ffmpeg.sh
#   FIX=/path/to/existing/dir ./scripts/verify-docker-ffmpeg.sh   # caller-owned; not deleted
#   REQUIRE_NVENC_ENCODE=0 ./scripts/verify-docker-ffmpeg.sh    # allow SKIP (GPU-less CI); default is 1
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
# Default 1: a no-GPU nvenc SKIP fails the script. Explicit opt-out (0/false/no)
# is required to allow SKIP on GPU-less hosts. Unset or empty → require.
REQUIRE_NVENC_ENCODE="${REQUIRE_NVENC_ENCODE:-1}"
# Bound for the in-container nvenc encode probe (seconds). PING_DEADLINE_SEC
# only covers the /ping poll; a hung probe container must not stall forever.
# SIGTERM at the limit, SIGKILL after the grace. A timeout is a FAIL.
NVENC_PROBE_TIMEOUT_SEC="${NVENC_PROBE_TIMEOUT_SEC:-120}"
NVENC_PROBE_KILL_GRACE_SEC="${NVENC_PROBE_KILL_GRACE_SEC:-5}"
# Explicit GPU declaration forwarded to check-ffmpeg-nvenc-encode.sh. Unset
# means "probe"; 0 is the only way a host without nvidia-smi may SKIP.
TESLAXY_NVENC_FORCE_GPU="${TESLAXY_NVENC_FORCE_GPU:-}"
NVENC_SMI_TIMEOUT_SEC="${NVENC_SMI_TIMEOUT_SEC:-5}"
NVENC_SMI_KILL_GRACE_SEC="${NVENC_SMI_KILL_GRACE_SEC:-1}"
# Extra docker run flags for GPU (e.g. --gpus all or --runtime=nvidia).
NVENC_DOCKER_GPU_ARGS="${NVENC_DOCKER_GPU_ARGS:-}"
NVIDIA_VISIBLE_DEVICES="${NVIDIA_VISIBLE_DEVICES:-all}"
NVIDIA_DRIVER_CAPABILITIES="${NVIDIA_DRIVER_CAPABILITIES:-compute,video,utility}"

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

PROBE_NAME="teslaxy-nvenc-probe-$$-$RANDOM"

cleanup() {
	if [[ -n "${CID:-}" ]]; then
		docker rm -f "$CID" >/dev/null 2>&1 || true
	fi
	# A probe container that outlived its timeout must not linger.
	docker rm -f "$PROBE_NAME" >/dev/null 2>&1 || true
	# Delete only the mktemp dir we created — never a caller-supplied FIX.
	if [[ -n "${OWNED_FIX:-}" ]]; then
		rm -rf "$OWNED_FIX"
	fi
}
trap cleanup EXIT

echo "==> docker build -f docker/Dockerfile -t ${IMAGE_TAG} ."
docker build -f "$ROOT/docker/Dockerfile" -t "$IMAGE_TAG" "$ROOT"

echo "==> encoder list check inside image"
docker run --rm --entrypoint /usr/local/share/teslaxy/check-ffmpeg-nvenc.sh "$IMAGE_TAG"

echo "==> nvenc encode probe inside image"
# Listing h264_nvenc is not enough — missing NVIDIA_* env / toolkit yields
# "Cannot load libcuda.so.1" only when encoding. Exit 2 = SKIP (no GPU).
if ! command -v timeout >/dev/null 2>&1; then
	echo "ERROR: timeout(1) missing; cannot bound the nvenc encode probe" >&2
	exit 1
fi
probe_env=(
	-e NVIDIA_VISIBLE_DEVICES="$NVIDIA_VISIBLE_DEVICES"
	-e NVIDIA_DRIVER_CAPABILITIES="$NVIDIA_DRIVER_CAPABILITIES"
	-e NVENC_SMI_TIMEOUT_SEC="$NVENC_SMI_TIMEOUT_SEC"
	-e NVENC_SMI_KILL_GRACE_SEC="$NVENC_SMI_KILL_GRACE_SEC"
)
if [[ -n "$TESLAXY_NVENC_FORCE_GPU" ]]; then
	probe_env+=(-e TESLAXY_NVENC_FORCE_GPU="$TESLAXY_NVENC_FORCE_GPU")
fi
set +e
# shellcheck disable=SC2086
encode_out="$(timeout -k "$NVENC_PROBE_KILL_GRACE_SEC" "$NVENC_PROBE_TIMEOUT_SEC" \
	docker run --rm --name "$PROBE_NAME" \
	"${probe_env[@]}" \
	$NVENC_DOCKER_GPU_ARGS \
	-v "$ROOT/scripts/check-ffmpeg-nvenc-encode.sh:/tmp/check-ffmpeg-nvenc-encode.sh:ro" \
	--entrypoint /bin/sh \
	"$IMAGE_TAG" \
	/tmp/check-ffmpeg-nvenc-encode.sh 2>&1)"
encode_ec=$?
set -e
printf '%s\n' "$encode_out"
case "$encode_ec" in
	0)
		echo "OK: nvenc encode probe passed"
		;;
	124|137)
		echo "ERROR: nvenc encode probe timed out after ${NVENC_PROBE_TIMEOUT_SEC}s (kill grace ${NVENC_PROBE_KILL_GRACE_SEC}s)" >&2
		docker rm -f "$PROBE_NAME" >/dev/null 2>&1 || true
		exit 1
		;;
	2)
		case "$REQUIRE_NVENC_ENCODE" in
			0|false|no)
				echo "SKIP: nvenc encode (no GPU/driver); REQUIRE_NVENC_ENCODE=${REQUIRE_NVENC_ENCODE} allows SKIP"
				;;
			*)
				echo "ERROR: REQUIRE_NVENC_ENCODE=${REQUIRE_NVENC_ENCODE} but encode was SKIP (no GPU/driver); set REQUIRE_NVENC_ENCODE=0 to allow on GPU-less hosts" >&2
				exit 1
				;;
		esac
		;;
	*)
		echo "ERROR: nvenc encode probe failed (exit ${encode_ec})" >&2
		exit "$encode_ec"
		;;
esac

echo "==> container smoke (/ping)"
mkdir -p "$FIX/footage/RecentClips" "$FIX/config"
dd if=/dev/zero of="$FIX/footage/RecentClips/seed.mp4" bs=1024 count=4 status=none
CID="$(docker run -d --rm \
	-p "127.0.0.1:${PORT}:80" \
	-e AUTH_ENABLED=false \
	-e GIN_MODE=release \
	-e RETENTION_ENABLED=false \
	-e RETENTION_DRY_RUN=true \
	-e NVIDIA_VISIBLE_DEVICES="$NVIDIA_VISIBLE_DEVICES" \
	-e NVIDIA_DRIVER_CAPABILITIES="$NVIDIA_DRIVER_CAPABILITIES" \
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
