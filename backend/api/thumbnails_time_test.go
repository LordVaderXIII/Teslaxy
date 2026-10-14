package api

import (
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"teslaxy/services"
)

func TestParseThumbnailTime_RejectsInvalid(t *testing.T) {
	// L2 unit cases
	cases := []string{"NaN", "nan", "Inf", "+Inf", "-Inf", "-1", "1e308", "abc", ""}
	for _, raw := range cases {
		if raw == "" {
			continue // DefaultQuery supplies 0.1; empty ParseFloat fails anyway
		}
		_, _, err := parseThumbnailTime(raw)
		if err == nil {
			t.Fatalf("expected error for time=%q", raw)
		}
	}
	got, norm, err := parseThumbnailTime("0.1")
	if err != nil {
		t.Fatal(err)
	}
	if got != 0.1 {
		t.Fatalf("expected 0.1, got %v", got)
	}
	if norm != "0.100" {
		t.Fatalf("expected normalized 0.100, got %q", norm)
	}
}

func TestParseThumbnailTime_NormalizesCacheKey(t *testing.T) {
	// M8: 0.1 / 0.10 / 0.100 must share one cache key component
	_, a, err := parseThumbnailTime("0.1")
	if err != nil {
		t.Fatal(err)
	}
	_, b, err := parseThumbnailTime("0.10")
	if err != nil {
		t.Fatal(err)
	}
	_, c, err := parseThumbnailTime("0.100")
	if err != nil {
		t.Fatal(err)
	}
	if a != b || b != c {
		t.Fatalf("normalized times differ: %q %q %q", a, b, c)
	}
}

func TestGetThumbnail_InvalidTimeReturns400(t *testing.T) {
	gin.SetMode(gin.TestMode)
	r := gin.New()
	r.GET("/api/thumbnail/*path", getThumbnail)

	footage := t.TempDir()
	os.Setenv("FOOTAGE_PATH", footage)
	defer os.Unsetenv("FOOTAGE_PATH")
	os.Setenv("CONFIG_PATH", t.TempDir())
	defer os.Unsetenv("CONFIG_PATH")

	video := filepath.Join(footage, "clip-front.mp4")
	makeSyntheticMP4(t, video, 1.0)

	for _, raw := range []string{"NaN", "Inf", "-1", "1e308"} {
		w := httptest.NewRecorder()
		req, _ := http.NewRequest("GET", "/api/thumbnail/clip-front.mp4?time="+raw, nil)
		r.ServeHTTP(w, req)
		if w.Code != http.StatusBadRequest {
			t.Fatalf("time=%s: expected 400, got %d body=%s", raw, w.Code, w.Body.String())
		}
	}
}

func TestGetThumbnail_SeekPastEndReturns400(t *testing.T) {
	gin.SetMode(gin.TestMode)
	r := gin.New()
	r.GET("/api/thumbnail/*path", getThumbnail)

	footage := t.TempDir()
	os.Setenv("FOOTAGE_PATH", footage)
	defer os.Unsetenv("FOOTAGE_PATH")
	os.Setenv("CONFIG_PATH", t.TempDir())
	defer os.Unsetenv("CONFIG_PATH")

	video := filepath.Join(footage, "clip-front.mp4")
	makeSyntheticMP4(t, video, 1.0)

	w := httptest.NewRecorder()
	req, _ := http.NewRequest("GET", "/api/thumbnail/clip-front.mp4?time=3000", nil)
	r.ServeHTTP(w, req)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected 400 for seek past end, got %d body=%s", w.Code, w.Body.String())
	}
}

func TestGetThumbnail_NormalizedCacheCollapsesAliases(t *testing.T) {
	services.SetMaxConcurrentFFmpegForTest(4)
	defer services.SetMaxConcurrentFFmpegForTest(services.DefaultMaxConcurrentFFmpeg)
	ResetDurationFlightForTest()

	gin.SetMode(gin.TestMode)
	r := gin.New()
	r.GET("/api/thumbnail/*path", getThumbnail)

	footage := t.TempDir()
	config := t.TempDir()
	os.Setenv("FOOTAGE_PATH", footage)
	os.Setenv("CONFIG_PATH", config)
	defer os.Unsetenv("FOOTAGE_PATH")
	defer os.Unsetenv("CONFIG_PATH")

	video := filepath.Join(footage, "clip-front.mp4")
	makeSyntheticMP4(t, video, 1.0)

	for _, raw := range []string{"0.1", "0.10", "0.100"} {
		w := httptest.NewRecorder()
		req, _ := http.NewRequest("GET", "/api/thumbnail/clip-front.mp4?time="+raw, nil)
		r.ServeHTTP(w, req)
		if w.Code != http.StatusOK {
			t.Fatalf("time=%s: expected 200, got %d body=%s", raw, w.Code, w.Body.String())
		}
	}

	entries, err := os.ReadDir(filepath.Join(config, "thumbnails"))
	if err != nil {
		t.Fatal(err)
	}
	jpgs := 0
	for _, e := range entries {
		if !e.IsDir() {
			jpgs++
		}
	}
	if jpgs != 1 {
		t.Fatalf("expected 1 cached thumbnail for 0.1/0.10/0.100, got %d", jpgs)
	}
}

func TestGetThumbnail_CachedHitSkipsFFProbe(t *testing.T) {
	// Codex finding #3: at 34fc9e4 ffprobe ran before cache lookup, so 30
	// parallel hits on a warm cache still spawned 30 ffprobes.
	services.SetMaxConcurrentFFmpegForTest(4)
	defer services.SetMaxConcurrentFFmpegForTest(services.DefaultMaxConcurrentFFmpeg)
	ResetDurationFlightForTest()
	defer SetFFProbeDurationCmdForTest(nil)

	gin.SetMode(gin.TestMode)
	r := gin.New()
	r.GET("/api/thumbnail/*path", getThumbnail)

	footage := t.TempDir()
	config := t.TempDir()
	os.Setenv("FOOTAGE_PATH", footage)
	os.Setenv("CONFIG_PATH", config)
	defer os.Unsetenv("FOOTAGE_PATH")
	defer os.Unsetenv("CONFIG_PATH")

	video := filepath.Join(footage, "clip-front.mp4")
	makeSyntheticMP4(t, video, 1.0)

	// Warm the cache with the real ffprobe.
	w := httptest.NewRecorder()
	req, _ := http.NewRequest("GET", "/api/thumbnail/clip-front.mp4?time=0.1", nil)
	r.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("warm: expected 200, got %d body=%s", w.Code, w.Body.String())
	}

	var probes atomic.Int64
	SetFFProbeDurationCmdForTest(func(ctx context.Context, path string) *exec.Cmd {
		probes.Add(1)
		// Counting fake: never invoke a real binary.
		return exec.CommandContext(ctx, "false")
	})
	ResetDurationFlightForTest()

	const n = 30
	var wg sync.WaitGroup
	errs := make(chan int, n)
	for i := 0; i < n; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			rw := httptest.NewRecorder()
			rreq, _ := http.NewRequest("GET", "/api/thumbnail/clip-front.mp4?time=0.1", nil)
			r.ServeHTTP(rw, rreq)
			errs <- rw.Code
		}()
	}
	wg.Wait()
	close(errs)
	for code := range errs {
		if code != http.StatusOK {
			t.Fatalf("cached hit expected 200, got %d", code)
		}
	}
	if probes.Load() != 0 {
		t.Fatalf("cached thumbnail hits must not call ffprobe; got %d calls", probes.Load())
	}
}

func TestGetThumbnail_DurationProbeCoalescedAndCapped(t *testing.T) {
	// Cold cache: many parallel requests for the same file share one ffprobe
	// (singleflight) and that probe takes a ffmpeg slot.
	services.SetMaxConcurrentFFmpegForTest(2)
	defer services.SetMaxConcurrentFFmpegForTest(services.DefaultMaxConcurrentFFmpeg)
	ResetDurationFlightForTest()
	defer SetFFProbeDurationCmdForTest(nil)

	var probes, probeActive, probePeak atomic.Int64
	release := make(chan struct{})
	started := make(chan struct{}, 1)

	SetFFProbeDurationCmdForTest(func(ctx context.Context, path string) *exec.Cmd {
		probes.Add(1)
		cur := probeActive.Add(1)
		for {
			p := probePeak.Load()
			if cur <= p || probePeak.CompareAndSwap(p, cur) {
				break
			}
		}
		select {
		case started <- struct{}{}:
		default:
		}
		if services.FFmpegActiveForTest() > 2 {
			t.Errorf("ffprobe slot active %d exceeds cap 2", services.FFmpegActiveForTest())
		}
		select {
		case <-release:
		case <-ctx.Done():
		}
		probeActive.Add(-1)
		// Return a tiny script via shell that prints a duration.
		return exec.CommandContext(ctx, "sh", "-c", "echo 1.0")
	})

	gin.SetMode(gin.TestMode)
	r := gin.New()
	r.GET("/api/thumbnail/*path", getThumbnail)

	footage := t.TempDir()
	config := t.TempDir()
	os.Setenv("FOOTAGE_PATH", footage)
	os.Setenv("CONFIG_PATH", config)
	defer os.Unsetenv("FOOTAGE_PATH")
	defer os.Unsetenv("CONFIG_PATH")

	video := filepath.Join(footage, "clip-front.mp4")
	makeSyntheticMP4(t, video, 1.0)

	const n = 12
	var wg sync.WaitGroup
	codes := make(chan int, n)
	for i := 0; i < n; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			rw := httptest.NewRecorder()
			rreq, _ := http.NewRequest("GET", "/api/thumbnail/clip-front.mp4?time=0.1", nil)
			r.ServeHTTP(rw, rreq)
			codes <- rw.Code
		}()
	}

	select {
	case <-started:
	case <-time.After(3 * time.Second):
		t.Fatal("ffprobe fake never started")
	}
	close(release)
	wg.Wait()
	close(codes)

	ok := 0
	for code := range codes {
		if code == http.StatusOK {
			ok++
		}
	}
	if ok == 0 {
		t.Fatal("expected at least one successful thumbnail")
	}
	if probes.Load() != 1 {
		t.Fatalf("expected 1 coalesced ffprobe, got %d", probes.Load())
	}
	if probePeak.Load() != 1 {
		t.Fatalf("expected single in-flight ffprobe, peak=%d", probePeak.Load())
	}
	if services.FFmpegPeakConcurrentForTest() > 2 {
		t.Fatalf("peak slots %d exceeds cap 2", services.FFmpegPeakConcurrentForTest())
	}
}
