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
	"errors"
)

var (
	ErrServerBusy = errors.New("server is busy: too many transcoding sessions")
	transcodeSemaphore = make(chan struct{}, 4)
)

func AcquireTranscodeSlot() error {
	select {
	case transcodeSemaphore <- struct{}{}:
		return nil
	default:
		return ErrServerBusy
	}
}

func ReleaseTranscodeSlot() {
	select {
	case <-transcodeSemaphore:
	default:
		// Should not happen if used correctly
	}
}

var (
	encoder      string
	encoderOnce  sync.Once
	hasNvenc     bool
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

// AutoDetectEncoder determines the best available encoder (NVENC vs CPU)
func AutoDetectEncoder() string {
	encoderOnce.Do(func() {
		// Default to libx264
		encoder = "libx264"
		hasNvenc = false

		// Check ffmpeg encoders
		cmd := exec.Command("ffmpeg", "-hide_banner", "-encoders")
		output, err := cmd.CombinedOutput()
		if err != nil {
			log.Printf("Warning: Failed to check ffmpeg encoders: %v. Defaulting to libx264.", err)
			return
		}

		if strings.Contains(string(output), "h264_nvenc") {
			encoder = "h264_nvenc"
			hasNvenc = true
			log.Println("Transcoder: NVIDIA NVENC detected and enabled.")
		} else {
			log.Println("Transcoder: NVIDIA NVENC not found. Using CPU (libx264).")
		}
	})
	return encoder
}

// GetTranscoderStatus returns a user-friendly status string
func GetTranscoderStatus() map[string]interface{} {
	AutoDetectEncoder()
	return map[string]interface{}{
		"encoder":   encoder,
		"hw_accel":  hasNvenc,
		"supported": true, // Assume ffmpeg is always present
	}
}

// GetTranscodeStream starts an ffmpeg process to transcode the file and returns the command and stdout pipe
func GetTranscodeStream(ctx context.Context, inputPath string, quality string) (*exec.Cmd, io.ReadCloser, error) {
	AutoDetectEncoder()

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
	if hasNvenc {
		args = append(args, "-hwaccel", "cuda")
	}

	args = append(args, "-i", inputPath)

	// Video Filter (Scaling)
	// usage: scale=-2:HEIGHT (maintains aspect ratio, keeps width even)
	args = append(args, "-vf", fmt.Sprintf("scale=-2:%d", q.Height))

	// Encoder settings
	args = append(args, "-c:v", encoder)
	args = append(args, "-b:v", q.Bitrate)

	if hasNvenc {
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
		return nil, nil, err
	}

	// Capture stderr for debugging (in a separate goroutine)
	stderr, _ := cmd.StderrPipe()
	go func() {
		scanner := bufio.NewScanner(stderr)
		for scanner.Scan() {
			log.Printf("FFmpeg Error: %s", scanner.Text())
		}
	}()

	if err := cmd.Start(); err != nil {
		return nil, nil, err
	}

	return cmd, stdout, nil
}
