#!/bin/sh
# Runtime probe: actually encode one second with h264_nvenc.
# Listing the encoder is not enough — missing libcuda/driver fails only at encode.
#
# Exit codes:
#   0  encode succeeded
#   2  SKIP — positively confirmed no GPU (/dev/nvidia* absent and nvidia-smi
#      absent, or nvidia-smi -L returned a clean non-GPU listing)
#   1  FAIL — GPU present but encode failed, OR GPU probe errored / was
#      ambiguous (missing probe, driver error, empty/partial/indented output,
#      or nvidia-smi timed out)
#
# SKIP is decided from GPU presence *before* encoding, never from ffmpeg
# error text (mixed/quoted/near-miss strings must not become SKIP).
# Probe errors are FAIL, not SKIP — only positively confirmed absence SKIPs.
#
# Test overrides:
#   TESLAXY_NVENC_FORCE_GPU=0|1  forces absence/presence
#   TESLAXY_NVENC_NVIDIA_SMI=/path  use this nvidia-smi (missing → FAIL)
#   NVENC_SMI_TIMEOUT_SEC=N  bound nvidia-smi -L (default 5; timeout → FAIL)
set -eu

FFMPEG_BIN="${FFMPEG_BIN:-/usr/local/bin/ffmpeg}"
NVENC_SMI_TIMEOUT_SEC="${NVENC_SMI_TIMEOUT_SEC:-5}"

if [ ! -x "$FFMPEG_BIN" ]; then
	echo "ERROR: ffmpeg binary missing or not executable: $FFMPEG_BIN" >&2
	exit 1
fi

# Sets GPU_STATUS to present|absent|error and optional GPU_STATUS_MSG.
probe_gpu_status() {
	GPU_STATUS=error
	GPU_STATUS_MSG=""

	case "${TESLAXY_NVENC_FORCE_GPU:-}" in
		0|false|no) GPU_STATUS=absent; return ;;
		1|true|yes) GPU_STATUS=present; return ;;
	esac

	# Device nodes injected by the NVIDIA Container Toolkit / drivers.
	# Plain sh: unmatched globs stay literal — [ -e '/dev/nvidia*' ] is false.
	for node in /dev/nvidia*; do
		if [ -e "$node" ]; then
			GPU_STATUS=present
			return
		fi
	done

	smi=""
	if [ -n "${TESLAXY_NVENC_NVIDIA_SMI:-}" ]; then
		smi="$TESLAXY_NVENC_NVIDIA_SMI"
		if [ ! -x "$smi" ]; then
			GPU_STATUS=error
			GPU_STATUS_MSG="nvidia-smi probe missing or not executable: $smi"
			return
		fi
	else
		if ! command -v nvidia-smi >/dev/null 2>&1; then
			# No device nodes and no nvidia-smi → positively confirmed absence.
			GPU_STATUS=absent
			return
		fi
		smi="$(command -v nvidia-smi)"
	fi

	if ! command -v timeout >/dev/null 2>&1; then
		GPU_STATUS=error
		GPU_STATUS_MSG="timeout(1) missing; cannot bound nvidia-smi probe"
		return
	fi

	# Bound the probe: a hung nvidia-smi must not block verification forever.
	set +e
	out="$(timeout "$NVENC_SMI_TIMEOUT_SEC" "$smi" -L 2>/dev/null)"
	ec=$?
	set -e

	# GNU timeout uses 124; some environments report 137 (SIGKILL).
	if [ "$ec" -eq 124 ] || [ "$ec" -eq 137 ]; then
		GPU_STATUS=error
		GPU_STATUS_MSG="nvidia-smi -L timed out after ${NVENC_SMI_TIMEOUT_SEC}s"
		return
	fi

	if [ "$ec" -ne 0 ]; then
		GPU_STATUS=error
		GPU_STATUS_MSG="nvidia-smi -L failed (exit ${ec})"
		return
	fi

	# Empty output is ambiguous — not positive confirmation of absence.
	if [ -z "$out" ]; then
		GPU_STATUS=error
		GPU_STATUS_MSG="nvidia-smi -L returned empty output"
		return
	fi

	# Positive presence: one "GPU 0: ..." line per device at column 0.
	if printf '%s\n' "$out" | grep -q '^GPU '; then
		GPU_STATUS=present
		return
	fi

	# Indented or partial GPU-like lines are unparseable → FAIL, not SKIP.
	if printf '%s\n' "$out" | grep -Eq '^[[:space:]]+GPU |^GPU([^ ]|$)'; then
		GPU_STATUS=error
		GPU_STATUS_MSG="nvidia-smi -L output unparseable (expected '^GPU ' lines)"
		return
	fi

	# Non-empty listing with no GPU-like lines (e.g. "No devices were found.")
	# is positive confirmation of absence.
	GPU_STATUS=absent
}

probe_gpu_status
case "$GPU_STATUS" in
	present)
		;;
	absent)
		echo "SKIP: h264_nvenc encode unavailable (no GPU driver/device injected)" >&2
		exit 2
		;;
	*)
		echo "ERROR: GPU probe failed: ${GPU_STATUS_MSG}" >&2
		exit 1
		;;
esac

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
