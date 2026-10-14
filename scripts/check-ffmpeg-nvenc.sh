#!/bin/sh
# Build-time / smoke check: BtbN ffmpeg must actually run, then list h264_nvenc.
# NVENC appears in -encoders without a GPU present; absence means a bad binary or ABI.
set -eu

FFMPEG_BIN="${FFMPEG_BIN:-/usr/local/bin/ffmpeg}"

if [ ! -x "$FFMPEG_BIN" ]; then
	echo "ERROR: ffmpeg binary missing or not executable: $FFMPEG_BIN" >&2
	exit 1
fi

echo "Checking ffmpeg runs: $FFMPEG_BIN -hide_banner -version"
if ! version_out="$("$FFMPEG_BIN" -hide_banner -version 2>&1)"; then
	echo "ERROR: ffmpeg failed to execute (glibc/musl ABI mismatch or missing libs)." >&2
	echo "$version_out" >&2
	exit 1
fi
echo "$version_out" | head -n 3

echo "Checking ffmpeg lists h264_nvenc encoder..."
encoders_out="$("$FFMPEG_BIN" -hide_banner -encoders 2>&1)" || {
	ec=$?
	echo "ERROR: ffmpeg -encoders failed (exit $ec)." >&2
	echo "$encoders_out" >&2
	exit "$ec"
}

if ! printf '%s\n' "$encoders_out" | grep -q 'h264_nvenc'; then
	echo "ERROR: h264_nvenc not found in ffmpeg -encoders output." >&2
	echo "---- ffmpeg -encoders (tail) ----" >&2
	printf '%s\n' "$encoders_out" | tail -n 80 >&2
	exit 1
fi

printf '%s\n' "$encoders_out" | grep 'h264_nvenc'
echo "OK: ffmpeg runs and advertises h264_nvenc"
