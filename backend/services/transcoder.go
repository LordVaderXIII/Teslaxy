package services

import (
	"bufio"
	"context"
	"fmt"
	"io"
	"log"
	"os/exec"
	"strings"
	"sync"
)

var (
	encoderMu       sync.Mutex
	encoderDetected bool
	encoderInFlight bool
	encoder         string
	hasNvenc        bool
	encoderReady    chan struct{} // closed when a probe attempt finishes
	encoderProbeErr error         // set if the leader failed before caching a result

	// probeNVENCFn is swappable in tests. Must respect ctx cancellation.
	probeNVENCFn = probeNVENCRuntime
)

func init() {
	encoderReady = make(chan struct{})
}

type TranscodeQuality struct {
	Height  int
	Bitrate string
}

var qualityMap = map[string]TranscodeQuality{
	"1080p": {Height: 1080, Bitrate: "4M"},
	"720p":  {Height: 720, Bitrate: "2M"},
	"480p":  {Height: 480, Bitrate: "1M"},
}

// ResetEncoderDetectionForTest clears the cached encoder probe result.
func ResetEncoderDetectionForTest() {
	encoderMu.Lock()
	defer encoderMu.Unlock()
	encoderDetected = false
	encoderInFlight = false
	encoder = ""
	hasNvenc = false
	encoderProbeErr = nil
	// Recreate ready channel so waiters from a prior test cannot spuriously unblock.
	select {
	case <-encoderReady:
		encoderReady = make(chan struct{})
	default:
		// still open from init or prior incomplete probe — leave it; tests reset between runs
		encoderReady = make(chan struct{})
	}
}

// SetNVENCProbeForTest overrides the runtime NVENC probe (tests only).
func SetNVENCProbeForTest(fn func(context.Context) bool) {
	encoderMu.Lock()
	defer encoderMu.Unlock()
	if fn == nil {
		probeNVENCFn = probeNVENCRuntime
	} else {
		probeNVENCFn = fn
	}
	encoderDetected = false
	encoderInFlight = false
	encoderProbeErr = nil
	select {
	case <-encoderReady:
		encoderReady = make(chan struct{})
	default:
		encoderReady = make(chan struct{})
	}
}

// ffmpegHasNVENCEncoder reports whether ffmpeg lists h264_nvenc (compiled-in).
func ffmpegHasNVENCEncoder(ctx context.Context) bool {
	cmd := exec.CommandContext(ctx, "ffmpeg", "-hide_banner", "-encoders")
	output, err := cmd.CombinedOutput()
	if err != nil {
		if ctx.Err() == nil {
			log.Printf("Warning: Failed to check ffmpeg encoders: %v", err)
		}
		return false
	}
	return strings.Contains(string(output), "h264_nvenc")
}

// probeNVENCRuntime verifies NVENC can actually encode a frame.
// Compiled-in encoders still fail on GPU-less hosts ("Cannot load libcuda.so.1").
func probeNVENCRuntime(ctx context.Context) bool {
	if !ffmpegHasNVENCEncoder(ctx) {
		return false
	}
	// Tiny lavfi encode: succeeds only when CUDA/NVENC runtime works.
	cmd := exec.CommandContext(
		ctx,
		"ffmpeg",
		"-hide_banner",
		"-loglevel", "error",
		"-f", "lavfi",
		"-i", "color=c=black:s=64x64:d=0.04",
		"-frames:v", "1",
		"-c:v", "h264_nvenc",
		"-f", "null",
		"-",
	)
	if out, err := cmd.CombinedOutput(); err != nil {
		if ctx.Err() == nil {
			log.Printf("Transcoder: h264_nvenc listed but runtime probe failed (%v): %s", err, strings.TrimSpace(string(out)))
		}
		return false
	}
	return true
}

// ensureEncoderDetected runs the NVENC probe at most once, under the ffmpeg
// concurrency cap and ctx deadline. Waiters cancel via ctx instead of blocking
// forever on the encoder mutex while ffmpeg runs.
func ensureEncoderDetected(ctx context.Context) error {
	for {
		encoderMu.Lock()
		if encoderDetected {
			encoderMu.Unlock()
			return nil
		}
		if encoderInFlight {
			ready := encoderReady
			encoderMu.Unlock()
			select {
			case <-ready:
				// Loop: leader may have failed without caching; retry or observe result.
				continue
			case <-ctx.Done():
				return ctx.Err()
			}
		}

		// Become the probe leader.
		encoderInFlight = true
		ready := encoderReady
		encoderMu.Unlock()

		err := runEncoderProbe(ctx)

		encoderMu.Lock()
		encoderInFlight = false
		if err != nil {
			encoderProbeErr = err
			// Do not cache on hard failure (slot/timeout) so a later request can retry.
			close(ready)
			encoderReady = make(chan struct{})
			encoderMu.Unlock()
			return err
		}
		encoderDetected = true
		encoderProbeErr = nil
		close(ready)
		encoderReady = make(chan struct{})
		encoderMu.Unlock()
		return nil
	}
}

func runEncoderProbe(ctx context.Context) error {
	probeCtx, cancel := context.WithTimeout(ctx, DefaultEncoderProbeTimeout)
	defer cancel()

	if err := AcquireFFmpeg(probeCtx); err != nil {
		return fmt.Errorf("encoder probe slot: %w", err)
	}
	defer ReleaseFFmpeg()

	ok := probeNVENCFn(probeCtx)
	// Cancellation or the probe deadline is not a "no GPU" answer. Leave the
	// cache empty so a later request runs the check again.
	if !ok && probeCtx.Err() != nil {
		return probeCtx.Err()
	}

	encoderMu.Lock()
	hasNvenc = ok
	if ok {
		encoder = "h264_nvenc"
		log.Println("Transcoder: NVIDIA NVENC runtime probe succeeded; NVENC enabled.")
	} else {
		encoder = "libx264"
		log.Println("Transcoder: NVIDIA NVENC not usable. Using CPU (libx264).")
	}
	encoderMu.Unlock()
	return nil
}

// AutoDetectEncoder determines the best available encoder (working NVENC vs CPU).
// Prefer EnsureEncoderDetected / GetTranscodeStream with a request context when possible.
func AutoDetectEncoder() string {
	ctx, cancel := context.WithTimeout(context.Background(), DefaultEncoderProbeTimeout)
	defer cancel()
	if err := ensureEncoderDetected(ctx); err != nil {
		log.Printf("Transcoder: encoder probe aborted (%v); defaulting to libx264", err)
		encoderMu.Lock()
		defer encoderMu.Unlock()
		if encoder == "" {
			encoder = "libx264"
		}
		return encoder
	}
	encoderMu.Lock()
	defer encoderMu.Unlock()
	return encoder
}

// HasWorkingNVENC reports whether NVENC passed the runtime probe.
func HasWorkingNVENC() bool {
	AutoDetectEncoder()
	encoderMu.Lock()
	defer encoderMu.Unlock()
	return hasNvenc
}

// GetTranscoderStatus returns a user-friendly status string
func GetTranscoderStatus() map[string]interface{} {
	AutoDetectEncoder()
	encoderMu.Lock()
	defer encoderMu.Unlock()
	return map[string]interface{}{
		"encoder":   encoder,
		"hw_accel":  hasNvenc,
		"supported": true, // Assume ffmpeg is always present
	}
}

// GetTranscodeStream starts an ffmpeg process to transcode the file.
// On success, the caller must invoke the returned cleanup func exactly once
// (it waits for the process and releases the ffmpeg concurrency slot).
func GetTranscodeStream(ctx context.Context, inputPath string, quality string) (*exec.Cmd, io.ReadCloser, func(), error) {
	if err := ensureEncoderDetected(ctx); err != nil {
		return nil, nil, nil, fmt.Errorf("encoder detection: %w", err)
	}

	if err := AcquireFFmpeg(ctx); err != nil {
		return nil, nil, nil, fmt.Errorf("ffmpeg concurrency limit: %w", err)
	}
	var once sync.Once
	cleanup := func() {
		once.Do(func() {
			ReleaseFFmpeg()
		})
	}
	fail := func(err error) (*exec.Cmd, io.ReadCloser, func(), error) {
		cleanup()
		return nil, nil, nil, err
	}

	encoderMu.Lock()
	useNvenc := hasNvenc
	enc := encoder
	encoderMu.Unlock()

	q, ok := qualityMap[quality]
	if !ok {
		// Default to 480p if invalid quality passed
		q = qualityMap["480p"]
	}

	// Construct Args
	args := []string{
		"-hide_banner",
		"-loglevel", "error",
	}

	// HW Accel for decoding (try CUDA if NVENC is available, else auto)
	if useNvenc {
		args = append(args, "-hwaccel", "cuda")
	}

	args = append(args, "-i", inputPath)

	// Video Filter (Scaling)
	// usage: scale=-2:HEIGHT (maintains aspect ratio, keeps width even)
	args = append(args, "-vf", fmt.Sprintf("scale=-2:%d", q.Height))

	// Encoder settings
	args = append(args, "-c:v", enc)
	args = append(args, "-b:v", q.Bitrate)

	if useNvenc {
		// NVENC specific presets (p1 = fastest)
		args = append(args, "-preset", "p1")
	} else {
		// CPU specific presets
		args = append(args, "-preset", "ultrafast")
	}

	// Output format: fragmented MP4 to stdout
	args = append(args, "-f", "mp4", "-movflags", "frag_keyframe+empty_moov", "-")

	cmd := exec.CommandContext(ctx, "ffmpeg", args...)

	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return fail(err)
	}

	// Capture stderr for debugging (in a separate goroutine)
	stderr, serr := cmd.StderrPipe()
	if serr != nil {
		return fail(serr)
	}
	go func() {
		scanner := bufio.NewScanner(stderr)
		for scanner.Scan() {
			log.Printf("FFmpeg Error: %s", scanner.Text())
		}
	}()

	if err := cmd.Start(); err != nil {
		return fail(err)
	}

	done := func() {
		once.Do(func() {
			_ = cmd.Wait()
			ReleaseFFmpeg()
		})
	}

	return cmd, stdout, done, nil
}
