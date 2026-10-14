package api

import (
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"testing"

	"github.com/gin-gonic/gin"
	"teslaxy/services"
)

func makeSyntheticMP4(t *testing.T, path string, seconds float64) {
	t.Helper()
	if _, err := exec.LookPath("ffmpeg"); err != nil {
		t.Skip("ffmpeg not installed")
	}
	dur := strconv.FormatFloat(seconds, 'f', 3, 64)
	cmd := exec.Command("ffmpeg", "-y", "-hide_banner", "-loglevel", "error",
		"-f", "lavfi", "-i", "color=c=blue:s=320x240:d="+dur,
		"-c:v", "libx264", "-pix_fmt", "yuv420p", "-t", dur, path)
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("ffmpeg fixture: %v (%s)", err, out)
	}
}

func TestServeVideo_TranscodeUsesLibx264WithoutGPU(t *testing.T) {
	// H3: on a host where nvenc may be listed but CUDA is missing,
	// ?quality=480p must return 200 with >0 bytes (libx264), not empty 200.
	services.ResetEncoderDetectionForTest()
	services.SetNVENCProbeForTest(nil)
	defer services.ResetEncoderDetectionForTest()

	enc := services.AutoDetectEncoder()
	if enc == "h264_nvenc" {
		t.Skip("NVENC works on this host; empty-200 repro needs GPU-less nvenc build")
	}
	if enc != "libx264" {
		t.Fatalf("expected libx264, got %s", enc)
	}

	gin.SetMode(gin.TestMode)
	r := gin.New()
	r.GET("/api/video/*path", serveVideo)

	footage := t.TempDir()
	os.Setenv("FOOTAGE_PATH", footage)
	defer os.Unsetenv("FOOTAGE_PATH")

	video := filepath.Join(footage, "clip-front.mp4")
	makeSyntheticMP4(t, video, 1.0)

	w := httptest.NewRecorder()
	req, _ := http.NewRequest("GET", "/api/video/clip-front.mp4?quality=480p", nil)
	r.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d body=%s", w.Code, w.Body.String())
	}
	if w.Body.Len() == 0 {
		t.Fatal("expected non-empty transcoded body (H3 empty 200 regression)")
	}

	outFile := filepath.Join(t.TempDir(), "out.mp4")
	if err := os.WriteFile(outFile, w.Body.Bytes(), 0644); err != nil {
		t.Fatal(err)
	}
	probe := exec.Command("ffprobe", "-v", "error", "-show_entries", "format=format_name",
		"-of", "default=noprint_wrappers=1:nokey=1", outFile)
	if out, err := probe.CombinedOutput(); err != nil {
		t.Fatalf("ffprobe failed on transcode output: %v (%s)", err, out)
	}
}

func TestServeVideo_ForcedFFmpegFailureReturns5xxNotEmpty200(t *testing.T) {
	// H3: when ffmpeg produces no bytes, response must be 5xx (not 200 with size 0).
	services.ResetEncoderDetectionForTest()
	services.SetNVENCProbeForTest(func() bool { return false })
	defer func() {
		services.SetNVENCProbeForTest(nil)
		services.ResetEncoderDetectionForTest()
	}()

	gin.SetMode(gin.TestMode)
	r := gin.New()
	r.GET("/api/video/*path", serveVideo)

	footage := t.TempDir()
	os.Setenv("FOOTAGE_PATH", footage)
	defer os.Unsetenv("FOOTAGE_PATH")

	bad := filepath.Join(footage, "bad-front.mp4")
	if err := os.WriteFile(bad, []byte("not a video"), 0644); err != nil {
		t.Fatal(err)
	}

	w := httptest.NewRecorder()
	req, _ := http.NewRequest("GET", "/api/video/bad-front.mp4?quality=480p", nil)
	r.ServeHTTP(w, req)

	if w.Code == http.StatusOK && w.Body.Len() == 0 {
		t.Fatal("empty 200 is the H3 bug; expected 5xx")
	}
	if w.Code < 500 {
		t.Fatalf("expected 5xx on forced ffmpeg failure, got %d body=%q", w.Code, w.Body.String())
	}
}

func TestServeVideo_TranscodeRespectsConcurrencyCap(t *testing.T) {
	// M8: many parallel transcodes must not exceed the ffmpeg slot cap.
	if _, err := exec.LookPath("ffmpeg"); err != nil {
		t.Skip("ffmpeg not installed")
	}
	services.ResetEncoderDetectionForTest()
	services.SetNVENCProbeForTest(func() bool { return false })
	services.SetMaxConcurrentFFmpegForTest(2)
	defer func() {
		services.SetMaxConcurrentFFmpegForTest(services.DefaultMaxConcurrentFFmpeg)
		services.SetNVENCProbeForTest(nil)
		services.ResetEncoderDetectionForTest()
	}()

	gin.SetMode(gin.TestMode)
	r := gin.New()
	r.GET("/api/video/*path", serveVideo)

	footage := t.TempDir()
	os.Setenv("FOOTAGE_PATH", footage)
	defer os.Unsetenv("FOOTAGE_PATH")

	video := filepath.Join(footage, "clip-front.mp4")
	makeSyntheticMP4(t, video, 2.0)

	const n = 12
	codes := make(chan int, n)
	for i := 0; i < n; i++ {
		go func() {
			w := httptest.NewRecorder()
			req, _ := http.NewRequest("GET", "/api/video/clip-front.mp4?quality=480p", nil)
			r.ServeHTTP(w, req)
			_, _ = io.Copy(io.Discard, w.Body)
			codes <- w.Code
		}()
	}

	ok := 0
	for i := 0; i < n; i++ {
		if <-codes == http.StatusOK {
			ok++
		}
	}
	if ok == 0 {
		t.Fatal("expected at least one successful transcode")
	}
	if peak := services.FFmpegPeakConcurrentForTest(); peak > 2 {
		t.Fatalf("peak concurrent ffmpeg %d exceeds cap 2", peak)
	}
}
