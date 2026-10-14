#!/bin/sh
# Runtime probe: actually encode one second with h264_nvenc.
# Listing the encoder is not enough — missing libcuda/driver fails only at encode.
#
# Exit codes:
#   0  encode succeeded
#   2  SKIP — no GPU / driver not injected (e.g. Cannot load libcuda.so.1)
#   1  FAIL — encode failed for another reason (GPU appears present)
set -eu

FFMPEG_BIN="${FFMPEG_BIN:-/usr/local/bin/ffmpeg}"

if [ ! -x "$FFMPEG_BIN" ]; then
	echo "ERROR: ffmpeg binary missing or not executable: $FFMPEG_BIN" >&2
	exit 1
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

printf '%s\n' "$out" >&2

# Missing driver / no CUDA device only — caller may SKIP (or FAIL if
# REQUIRE_NVENC_ENCODE=1). Broader NVENC init errors (OOM, unsupported
# features, API version, OpenEncodeSessionEx, etc.) must FAIL (exit 1).
if printf '%s\n' "$out" | grep -Eq \
	'Cannot load libcuda\.so\.1|CUDA_ERROR_NO_DEVICE|no CUDA-capable device'; then
	echo "SKIP: h264_nvenc encode unavailable (no GPU driver/device injected)" >&2
	exit 2
fi

echo "ERROR: h264_nvenc encode failed (exit $ec)" >&2
exit 1
