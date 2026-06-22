package services

import (
	"bufio"
	"bytes"
	"context"
	"fmt"
	"io"
	"log"
	"os/exec"
	"strings"
	"sync"
	"time"
)

var (
	encoder      string
	encoderOnce  sync.Once
	hasNvenc     bool

	// Simple in-memory transcoding cache to avoid re-transcoding the same
	// (file + quality) repeatedly during scrubbing or multi-camera playback.
	transcodeCache sync.Map // key: "path|quality" -> *cachedTranscode
)

type cachedTranscode struct {
	data    []byte
	expires time.Time
}

const (
	transcodeCacheTTL   = 3 * time.Minute
	maxTranscodeCacheMB = 800 // safety limit per entry
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

// GetTranscodeStream starts an ffmpeg process to transcode the file and returns the command and stdout pipe.
// It now includes a simple in-memory cache for (file + quality) to dramatically reduce repeated
// transcoding work during scrubbing and multi-camera playback.
func GetTranscodeStream(ctx context.Context, inputPath string, quality string) (*exec.Cmd, io.ReadCloser, error) {
	AutoDetectEncoder()

	q, ok := qualityMap[quality]
	if !ok {
		q = qualityMap["480p"]
	}

	cacheKey := inputPath + "|" + quality

	// Check cache first
	if cached, ok := transcodeCache.Load(cacheKey); ok {
		entry := cached.(*cachedTranscode)
		if time.Now().Before(entry.expires) && len(entry.data) > 0 {
			log.Printf("Transcoder: Cache hit for %s (%s)", inputPath, quality)
			return nil, io.NopCloser(bytes.NewReader(entry.data)), nil
		}
		// Expired entry — remove it
		transcodeCache.Delete(cacheKey)
	}

	// Cache miss — build ffmpeg args
	args := []string{
		"-hide_banner",
		"-loglevel", "error",
	}

	if hasNvenc {
		args = append(args, "-hwaccel", "cuda")
	}

	args = append(args, "-i", inputPath)
	args = append(args, "-vf", fmt.Sprintf("scale=-2:%d", q.Height))
	args = append(args, "-c:v", encoder)
	args = append(args, "-b:v", q.Bitrate)

	if hasNvenc {
		args = append(args, "-preset", "p1")
	} else {
		args = append(args, "-preset", "veryfast")
		args = append(args, "-tune", "zerolatency")
	}

	args = append(args, "-f", "mp4", "-movflags", "frag_keyframe+empty_moov", "-")

	cmd := exec.CommandContext(ctx, "ffmpeg", args...)

	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return nil, nil, err
	}

	// Capture stderr
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

	// Tee the output so we can cache it while streaming to the client
	pr, pw := io.Pipe()

	go func() {
		defer pw.Close()

		buf := &bytes.Buffer{}
		tee := io.TeeReader(stdout, buf)

		// Stream to the actual client via the pipe
		if _, err := io.Copy(pw, tee); err != nil {
			log.Printf("Transcoder stream copy error: %v", err)
		}

		// After streaming completes, store in cache if reasonable size
		if buf.Len() > 0 && buf.Len() < maxTranscodeCacheMB*1024*1024 {
			transcodeCache.Store(cacheKey, &cachedTranscode{
				data:    buf.Bytes(),
				expires: time.Now().Add(transcodeCacheTTL),
			})
			log.Printf("Transcoder: Cached transcoded result for %s (%s) — %d KB",
				inputPath, quality, buf.Len()/1024)
		}
	}()

	return cmd, pr, nil
}
