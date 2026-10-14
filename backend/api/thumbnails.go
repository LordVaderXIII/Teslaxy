package api

import (
	"context"
	"crypto/md5"
	"encoding/hex"
	"fmt"
	"log"
	"math"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"sync"

	"github.com/gin-gonic/gin"
	"teslaxy/services"
)

// parseThumbnailTime validates and normalizes the seek time query value.
// Rejects NaN/Inf/negatives. Returns the normalized cache-key string (3 decimal places).
func parseThumbnailTime(raw string) (float64, string, error) {
	t, err := strconv.ParseFloat(raw, 64)
	if err != nil {
		return 0, "", fmt.Errorf("invalid time parameter")
	}
	if math.IsNaN(t) || math.IsInf(t, 0) || t < 0 {
		return 0, "", fmt.Errorf("invalid time parameter")
	}
	// Cap absurd magnitudes before they reach ffmpeg (e.g. 1e308).
	const maxSeekSeconds = 24 * 60 * 60 // 24h
	if t > maxSeekSeconds {
		return 0, "", fmt.Errorf("time exceeds maximum")
	}
	normalized := strconv.FormatFloat(t, 'f', 3, 64)
	return t, normalized, nil
}

// durationFlight shares an in-flight ffprobe for a path. The entry is removed
// when the probe finishes, so a later call — including after the file is
// replaced — measures the file again instead of reusing a stale duration.
var durationFlight sync.Map // path -> *durationCall

type durationCall struct {
	dur  float64
	err  error
	done chan struct{}
}

// ffprobeDurationCmd is swappable in tests (counting fakes).
var ffprobeDurationCmd = defaultFFProbeDurationCmd

func defaultFFProbeDurationCmd(ctx context.Context, path string) *exec.Cmd {
	return exec.CommandContext(ctx, "ffprobe",
		"-v", "error",
		"-show_entries", "format=duration",
		"-of", "default=noprint_wrappers=1:nokey=1",
		path,
	)
}

// SetFFProbeDurationCmdForTest overrides the ffprobe runner (tests only).
func SetFFProbeDurationCmdForTest(fn func(context.Context, string) *exec.Cmd) {
	if fn == nil {
		ffprobeDurationCmd = defaultFFProbeDurationCmd
	} else {
		ffprobeDurationCmd = fn
	}
}

// ResetDurationFlightForTest clears coalesced duration probes (tests only).
func ResetDurationFlightForTest() {
	durationFlight = sync.Map{}
}

// probeVideoDurationSeconds returns media duration via ffprobe, under the ffmpeg
// concurrency cap. Concurrent callers for one path share a single in-flight
// probe and each select on that result or their own ctx.Done(). The finished
// duration is not retained.
func probeVideoDurationSeconds(ctx context.Context, path string) (float64, error) {
	if err := ctx.Err(); err != nil {
		return 0, err
	}

	call := &durationCall{done: make(chan struct{})}
	actual, loaded := durationFlight.LoadOrStore(path, call)
	if loaded {
		call = actual.(*durationCall)
	} else {
		go runDurationProbe(call, path)
	}

	select {
	case <-call.done:
		return call.dur, call.err
	case <-ctx.Done():
		return 0, ctx.Err()
	}
}

// runDurationProbe is the single in-flight ffprobe for a path. It uses its own
// timeout so one caller's cancellation does not abort the shared probe or get
// stored as that caller's result. The map entry is dropped before waiters are
// released, so the next call probes again.
func runDurationProbe(call *durationCall, path string) {
	defer func() {
		durationFlight.CompareAndDelete(path, call)
		close(call.done)
	}()

	probeCtx, cancel := context.WithTimeout(context.Background(), services.DefaultThumbnailTimeout)
	defer cancel()

	if err := services.AcquireFFmpeg(probeCtx); err != nil {
		call.err = err
		return
	}
	defer services.ReleaseFFmpeg()

	cmd := ffprobeDurationCmd(probeCtx, path)
	out, err := cmd.Output()
	if err != nil {
		call.err = err
		return
	}
	call.dur, call.err = strconv.ParseFloat(strings.TrimSpace(string(out)), 64)
}

func getThumbnail(c *gin.Context) {
	videoPath := c.Param("path")
	seekTimeRaw := c.DefaultQuery("time", "0.1")
	widthStr := c.DefaultQuery("w", "480")

	seekTime, normalizedTime, err := parseThumbnailTime(seekTimeRaw)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	// Validate width
	width, err := strconv.Atoi(widthStr)
	if err != nil || width < 10 || width > 1920 {
		width = 480
	}

	footagePath := os.Getenv("FOOTAGE_PATH")
	if footagePath == "" {
		footagePath = "/footage"
	}
	cleanFootagePath := filepath.Clean(footagePath)
	cleanRequestPath := filepath.Clean(videoPath)

	// Determine full path
	// If the request path already starts with the footage path, assume it's absolute
	// Otherwise, join it with the footage path
	var fullPath string
	if strings.HasPrefix(cleanRequestPath, cleanFootagePath) {
		fullPath = cleanRequestPath
	} else {
		fullPath = filepath.Join(cleanFootagePath, cleanRequestPath)
	}

	// Final Security Check
	// Ensure the resolved full path is strictly inside the footage path
	if fullPath != cleanFootagePath && !strings.HasPrefix(fullPath, cleanFootagePath+string(os.PathSeparator)) {
		c.JSON(http.StatusForbidden, gin.H{"error": "Access denied"})
		return
	}

	// Verify file exists at the full path
	if _, err := os.Stat(fullPath); os.IsNotExist(err) {
		log.Printf("Thumbnail source not found: %s", fullPath)
		c.JSON(http.StatusNotFound, gin.H{"error": "Video file not found"})
		return
	}

	// Cache setup + lookup BEFORE ffprobe so cached hits never spawn probes.
	configPath := os.Getenv("CONFIG_PATH")
	if configPath == "" {
		configPath = "/config"
	}
	thumbDir := filepath.Join(configPath, "thumbnails")
	if err := os.MkdirAll(thumbDir, 0755); err != nil {
		log.Printf("Failed to create thumbnail dir: %v", err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Internal error"})
		return
	}

	cacheKey := fmt.Sprintf("%s|%s|%d", fullPath, normalizedTime, width)
	hash := md5.Sum([]byte(cacheKey))
	hashStr := hex.EncodeToString(hash[:])
	thumbPath := filepath.Join(thumbDir, hashStr+".jpg")

	if info, err := os.Stat(thumbPath); err == nil {
		if info.Size() > 0 {
			c.File(thumbPath)
			return
		}
		os.Remove(thumbPath)
	}

	ctx, cancel := services.WithFFmpegTimeout(c.Request.Context(), services.DefaultThumbnailTimeout)
	defer cancel()

	// Reject seeks past EOF with a clean 4xx (and log) instead of a bare ffmpeg 404/500.
	if dur, derr := probeVideoDurationSeconds(ctx, fullPath); derr == nil {
		if seekTime >= dur {
			log.Printf("Thumbnail seek %.3fs past duration %.3fs for %s", seekTime, dur, fullPath)
			c.JSON(http.StatusBadRequest, gin.H{"error": "time exceeds video duration"})
			return
		}
	} else {
		log.Printf("Thumbnail duration probe failed for %s: %v (continuing)", fullPath, derr)
	}

	// Generate Thumbnail using FFmpeg (bounded concurrency + timeout).
	// Each request writes a private temp file and renames it into the cache so
	// concurrent misses cannot publish a partial JPEG at the shared path.
	if err := services.AcquireFFmpeg(ctx); err != nil {
		log.Printf("Thumbnail ffmpeg slot: %v", err)
		c.JSON(http.StatusServiceUnavailable, gin.H{"error": "Server busy generating thumbnails"})
		return
	}
	defer services.ReleaseFFmpeg()

	tmp, err := os.CreateTemp(thumbDir, hashStr+".*.jpg")
	if err != nil {
		log.Printf("Failed to create thumbnail temp file: %v", err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to generate thumbnail"})
		return
	}
	tmpPath := tmp.Name()
	if err := tmp.Close(); err != nil {
		os.Remove(tmpPath)
		log.Printf("Failed to close thumbnail temp file: %v", err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to generate thumbnail"})
		return
	}
	defer os.Remove(tmpPath)

	vf := fmt.Sprintf("scale=%d:-1", width)
	cmd := exec.CommandContext(ctx, "ffmpeg", "-y", "-ss", normalizedTime, "-i", fullPath, "-vframes", "1", "-vf", vf, "-q:v", "5", tmpPath)
	if out, err := cmd.CombinedOutput(); err != nil {
		log.Printf("FFmpeg thumbnail error for %s time=%s: %v, Output: %s", fullPath, normalizedTime, err, string(out))
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to generate thumbnail"})
		return
	}
	info, err := os.Stat(tmpPath)
	if err != nil || info.Size() == 0 {
		log.Printf("FFmpeg thumbnail empty for %s time=%s", fullPath, normalizedTime)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to generate thumbnail"})
		return
	}
	if err := os.Rename(tmpPath, thumbPath); err != nil {
		log.Printf("Failed to publish thumbnail %s: %v", thumbPath, err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to generate thumbnail"})
		return
	}

	c.File(thumbPath)
}
