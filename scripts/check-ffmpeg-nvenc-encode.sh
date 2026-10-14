#!/bin/sh
# Runtime probe: actually encode one second with h264_nvenc.
# Listing the encoder is not enough — missing libcuda/driver fails only at encode.
#
# Exit codes:
#   0  encode succeeded
#   2  SKIP — no GPU device present (/dev/nvidia* or nvidia-smi -L)
#   1  FAIL — GPU appears present but encode failed
#
# SKIP is decided from GPU presence *before* encoding, never from ffmpeg
# error text (mixed/quoted/near-miss strings must not become SKIP).
#
# Test override: TESLAXY_NVENC_FORCE_GPU=0|1 forces the presence check.
set -eu

FFMPEG_BIN="${FFMPEG_BIN:-/usr/local/bin/ffmpeg}"

if [ ! -x "$FFMPEG_BIN" ]; then
	echo "ERROR: ffmpeg binary missing or not executable: $FFMPEG_BIN" >&2
	exit 1
fi

gpu_present() {
	# Explicit test override (0=absent, 1=present).
	case "${TESLAXY_NVENC_FORCE_GPU:-}" in
		0|false|no) return 1 ;;
		1|true|yes) return 0 ;;
	esac

	# Device nodes injected by the NVIDIA Container Toolkit / drivers.
	# Plain sh: unmatched globs stay literal — [ -e '/dev/nvidia*' ] is false.
	for node in /dev/nvidia*; do
		if [ -e "$node" ]; then
			return 0
		fi
	done

	if command -v nvidia-smi >/dev/null 2>&1; then
		# nvidia-smi -L prints one "GPU 0: ..." line per device; empty/fail = none.
		if nvidia-smi -L 2>/dev/null | grep -q '^GPU '; then
			return 0
		fi
	fi
	return 1
}

if ! gpu_present; then
	echo "SKIP: h264_nvenc encode unavailable (no GPU driver/device injected)" >&2
	exit 2
fi

echo "Probing h264_nvenc encode: $FFMPEG_BIN -f lavfi -i testsrc=... -c:v h264_nvenc -f null -"

set +e
out="$("$FFMPEG_BIN" -hide_banner -loglevel error \
	-f lavfi -i "testsrc=size=320x240:rate=30:duration=1" \
	-frames:v 30 \
	-c:v h264_nvenc \
	-f null - 2>&1)"
ec=$?
set -e

if [ "$ec" -eq 0 ]; then
	echo "OK: h264_nvenc encode succeeded"
	exit 0
fi

# GPU was present — any encode failure is FAIL (do not grep error text for SKIP).
if [ -n "$out" ]; then
	printf '%s\n' "$out" >&2
fi
echo "ERROR: h264_nvenc encode failed (exit $ec)" >&2
exit 1
