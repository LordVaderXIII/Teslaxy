#!/bin/sh
# Runtime probe: actually encode one second with h264_nvenc.
# Listing the encoder is not enough — missing libcuda/driver fails only at encode.
#
# Exit codes:
#   0  encode succeeded
#   2  SKIP — explicitly recognised "no GPU": TESLAXY_NVENC_FORCE_GPU=0, or
#      /dev/nvidia* absent and `nvidia-smi -L` printed exactly
#      "No devices were found" (exit 0 or 6) and nothing else
#   1  FAIL — GPU present but encode failed, OR the GPU probe could not
#      positively confirm absence: nvidia-smi missing (from PATH or the
#      explicit path), driver error, timeout, empty/whitespace output, partial
#      ("G", "GU", "GPU ", "GPU"), indented, mixed, or any unrecognised text
#
# SKIP is decided from GPU presence *before* encoding, never from ffmpeg
# error text (mixed/quoted/near-miss strings must not become SKIP).
# Anything the parser does not recognise is FAIL — a missing tool or garbage
# output must never let verify-docker-ffmpeg.sh pass without encoding.
#
# Test overrides:
#   TESLAXY_NVENC_FORCE_GPU=0|1  forces absence/presence
#   TESLAXY_NVENC_NVIDIA_SMI=/path  use this nvidia-smi (missing → FAIL)
#   NVENC_SMI_TIMEOUT_SEC=N  bound nvidia-smi -L (default 5; timeout → FAIL)
#   NVENC_SMI_KILL_GRACE_SEC=N  SIGKILL grace after SIGTERM (default 1)
set -eu

FFMPEG_BIN="${FFMPEG_BIN:-/usr/local/bin/ffmpeg}"
NVENC_SMI_TIMEOUT_SEC="${NVENC_SMI_TIMEOUT_SEC:-5}"
NVENC_SMI_KILL_GRACE_SEC="${NVENC_SMI_KILL_GRACE_SEC:-1}"

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
		# A missing tool cannot confirm absence: the toolkit may simply not
		# have injected it. Only an explicit opt-out may SKIP here.
		if ! command -v nvidia-smi >/dev/null 2>&1; then
			GPU_STATUS=error
			GPU_STATUS_MSG="nvidia-smi not found in PATH; cannot confirm GPU absence (set TESLAXY_NVENC_FORCE_GPU=0 to declare a GPU-less host)"
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
	# -k sends SIGKILL after the grace period so a probe that ignores SIGTERM
	# (and its children, via the process group) still ends by limit+grace.
	set +e
	out="$(timeout -k "$NVENC_SMI_KILL_GRACE_SEC" "$NVENC_SMI_TIMEOUT_SEC" "$smi" -L 2>/dev/null)"
	ec=$?
	set -e

	# GNU timeout uses 124; 137 when the -k SIGKILL had to fire.
	if [ "$ec" -eq 124 ] || [ "$ec" -eq 137 ]; then
		GPU_STATUS=error
		GPU_STATUS_MSG="nvidia-smi -L timed out after ${NVENC_SMI_TIMEOUT_SEC}s (kill grace ${NVENC_SMI_KILL_GRACE_SEC}s)"
		return
	fi

	classify_smi_output "$out" "$ec"
}

# classify_smi_output <stdout> <exit>: strict whitelist parser for `nvidia-smi -L`.
#   present  every non-blank line is "GPU <n>: <name>" and there is at least one
#   absent   the whole output is exactly "No devices were found[.]" (exit 0 or 6)
#   error    anything else (partial / indented / mixed / garbage / empty / exit)
classify_smi_output() {
	out="$1"
	ec="$2"

	# Trim leading/trailing blank lines + surrounding whitespace for the
	# whole-output equality check only; per-line parsing stays column-strict.
	trimmed="$(printf '%s\n' "$out" | sed -e 's/[[:space:]]*$//' -e '/^$/d')"

	if [ -z "$trimmed" ]; then
		GPU_STATUS=error
		if [ "$ec" -ne 0 ]; then
			GPU_STATUS_MSG="nvidia-smi -L failed (exit ${ec}) with no output"
		else
			GPU_STATUS_MSG="nvidia-smi -L returned empty/whitespace output"
		fi
		return
	fi

	case "$trimmed" in
		"No devices were found"|"No devices were found.")
			if [ "$ec" -eq 0 ] || [ "$ec" -eq 6 ]; then
				GPU_STATUS=absent
				return
			fi
			GPU_STATUS=error
			GPU_STATUS_MSG="nvidia-smi -L reported no devices but exited ${ec}"
			return
			;;
	esac

	if [ "$ec" -ne 0 ]; then
		GPU_STATUS=error
		GPU_STATUS_MSG="nvidia-smi -L failed (exit ${ec})"
		return
	fi

	# Present only when every line is a complete device line at column 0.
	# "GPU", "GPU ", "G", "GU", "  GPU 0: ..." and mixed listings all fail.
	if printf '%s\n' "$trimmed" | grep -Eq '^GPU [0-9]+: [^[:space:]].*$' \
		&& ! printf '%s\n' "$trimmed" | grep -Evq '^GPU [0-9]+: [^[:space:]].*$'; then
		GPU_STATUS=present
		return
	fi

	GPU_STATUS=error
	GPU_STATUS_MSG="nvidia-smi -L output unrecognised (expected 'GPU <n>: <name>' lines or 'No devices were found')"
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
