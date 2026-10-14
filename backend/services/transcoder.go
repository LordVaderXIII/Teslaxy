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
	encoder         string
	hasNvenc        bool

	// probeNVENCFn is swappable in tests.
	probeNVENCFn = probeNVENCRuntime
)

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
	encoder = ""
	hasNvenc = false
}

// SetNVENCProbeForTest overrides the runtime NVENC probe (tests only).
func SetNVENCProbeForTest(fn func() bool) {
	encoderMu.Lock()
	defer encoderMu.Unlock()
	if fn == nil {
		probeNVENCFn = probeNVENCRuntime
	} else {
		probeNVENCFn = fn
	}
	encoderDetected = false
}

// ffmpegHasNVENCEncoder reports whether ffmpeg lists h264_nvenc (compiled-in).
func ffmpegHasNVENCEncoder() bool {
	cmd := exec.Command("ffmpeg", "-hide_banner", "-encoders")
	output, err := cmd.CombinedOutput()
	if err != nil {
		log.Printf("Warning: Failed to check ffmpeg encoders: %v", err)
		return false
	}
	return strings.Contains(string(output), "h264_nvenc")
}

// probeNVENCRuntime verifies NVENC can actually encode a frame.
// Compiled-in encoders still fail on GPU-less hosts ("Cannot load libcuda.so.1").
func probeNVENCRuntime() bool {
	if !ffmpegHasNVENCEncoder() {
		return false
	}
	// Tiny lavfi encode: succeeds only when CUDA/NVENC runtime works.
	cmd := exec.Command(
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
		log.Printf("Transcoder: h264_nvenc listed but runtime probe failed (%v): %s", err, strings.TrimSpace(string(out)))
		return false
	}
	return true
}

// AutoDetectEncoder determines the best available encoder (working NVENC vs CPU).
func AutoDetectEncoder() string {
	encoderMu.Lock()
	defer encoderMu.Unlock()
	if encoderDetected {
		return encoder
	}

	encoder = "libx264"
	hasNvenc = false

	if probeNVENCFn() {
		encoder = "h264_nvenc"
		hasNvenc = true
		log.Println("Transcoder: NVIDIA NVENC runtime probe succeeded; NVENC enabled.")
	} else {
		log.Println("Transcoder: NVIDIA NVENC not usable. Using CPU (libx264).")
	}

	encoderDetected = true
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
	AutoDetectEncoder()

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
