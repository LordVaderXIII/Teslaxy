package api

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
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

func TestProbeVideoDuration_WaiterHonorsOwnDeadline(t *testing.T) {
	// A waiter blocked inside sync.Once.Do used to ignore its deadline and
	// return only after the leader's probe finished (100ms came back at 2.78s).
	services.SetMaxConcurrentFFmpegForTest(4)
	defer services.SetMaxConcurrentFFmpegForTest(services.DefaultMaxConcurrentFFmpeg)
	ResetDurationFlightForTest()
	defer SetFFProbeDurationCmdForTest(nil)

	gate := filepath.Join(t.TempDir(), "gate")
	video := filepath.Join(t.TempDir(), "clip.mp4")
	release := func() { _ = os.WriteFile(gate, []byte("1"), 0644) }
	defer release()

	started := make(chan struct{})
	var probes atomic.Int64
	var startedOnce sync.Once
	SetFFProbeDurationCmdForTest(func(ctx context.Context, path string) *exec.Cmd {
		probes.Add(1)
		startedOnce.Do(func() { close(started) })
		script := fmt.Sprintf("while [ ! -f %s ]; do sleep 0.05; done; echo 2.5", strconv.Quote(gate))
		return exec.CommandContext(ctx, "sh", "-c", script)
	})

	type probeResult struct {
		dur float64
		err error
	}
	leaderCh := make(chan probeResult, 1)
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		dur, err := probeVideoDurationSeconds(ctx, video)
		leaderCh <- probeResult{dur, err}
	}()

	select {
	case <-started:
	case <-time.After(2 * time.Second):
		t.Fatal("duration probe never started")
	}

	type waiterResult struct {
		elapsed time.Duration
		err     error
	}
	waiterCh := make(chan waiterResult, 1)
	go func() {
		wctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
		defer cancel()
		t0 := time.Now()
		_, err := probeVideoDurationSeconds(wctx, video)
		waiterCh <- waiterResult{time.Since(t0), err}
	}()

	select {
	case res := <-waiterCh:
		if res.elapsed > time.Second {
			t.Fatalf("waiter returned after %s; a 100ms deadline must not wait out the in-flight probe", res.elapsed)
		}
		if !errors.Is(res.err, context.DeadlineExceeded) {
			t.Fatalf("waiter err = %v, want deadline exceeded", res.err)
		}
	case <-time.After(time.Second):
		t.Error("waiter still blocked after 1s while the shared probe was in flight")
	}

	release()
	select {
	case res := <-leaderCh:
		if res.err != nil {
			t.Fatalf("leader: %v", res.err)
		}
		if res.dur != 2.5 {
			t.Fatalf("leader duration = %v, want 2.5", res.dur)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("leader did not receive the shared probe result")
	}
	if probes.Load() != 1 {
		t.Fatalf("expected 1 shared probe, got %d", probes.Load())
	}
}

func TestGetThumbnail_ReplacedFileDoesNotReuseStaleDuration(t *testing.T) {
	// A duration cached by path alone rejects a valid seek with 400 after the
	// file is replaced by a longer one.
	services.SetMaxConcurrentFFmpegForTest(4)
	defer services.SetMaxConcurrentFFmpegForTest(services.DefaultMaxConcurrentFFmpeg)
	ResetDurationFlightForTest()
	defer SetFFProbeDurationCmdForTest(nil)

	if _, err := exec.LookPath("ffprobe"); err != nil {
		t.Skip("ffprobe not installed")
	}

	var probes atomic.Int64
	SetFFProbeDurationCmdForTest(func(ctx context.Context, path string) *exec.Cmd {
		probes.Add(1)
		return exec.CommandContext(ctx, "ffprobe",
			"-v", "error",
			"-show_entries", "format=duration",
			"-of", "default=noprint_wrappers=1:nokey=1",
			path,
		)
	})

	gin.SetMode(gin.TestMode)
	r := gin.New()
	r.GET("/api/thumbnail/*path", getThumbnail)

	footage := t.TempDir()
	t.Setenv("FOOTAGE_PATH", footage)
	t.Setenv("CONFIG_PATH", t.TempDir())

	video := filepath.Join(footage, "clip-front.mp4")
	makeSyntheticMP4(t, video, 1.0)

	w := httptest.NewRecorder()
	req, _ := http.NewRequest("GET", "/api/thumbnail/clip-front.mp4?time=2.5", nil)
	r.ServeHTTP(w, req)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("short file: expected 400, got %d body=%s", w.Code, w.Body.String())
	}
	if probes.Load() != 1 {
		t.Fatalf("expected 1 probe for the short file, got %d", probes.Load())
	}

	makeSyntheticMP4(t, video, 5.0)

	w = httptest.NewRecorder()
	req, _ = http.NewRequest("GET", "/api/thumbnail/clip-front.mp4?time=2.5", nil)
	r.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("replaced file: expected 200, got %d body=%s", w.Code, w.Body.String())
	}
	if probes.Load() != 2 {
		t.Fatalf("replaced file must be probed again, probes=%d", probes.Load())
	}
	body := w.Body.Bytes()
	if len(body) < 3 || body[0] != 0xFF || body[1] != 0xD8 {
		t.Fatalf("expected a JPEG thumbnail, len=%d", len(body))
	}
}

func TestGetThumbnail_ConcurrentMissesReturnNonEmpty(t *testing.T) {
	// Concurrent cache misses used to share one output path and could publish
	// an empty 200. Each request now writes a temp file and renames it.
	services.SetMaxConcurrentFFmpegForTest(4)
	defer services.SetMaxConcurrentFFmpegForTest(services.DefaultMaxConcurrentFFmpeg)
	ResetDurationFlightForTest()
	defer SetFFProbeDurationCmdForTest(nil)

	gin.SetMode(gin.TestMode)
	r := gin.New()
	r.GET("/api/thumbnail/*path", getThumbnail)

	footage := t.TempDir()
	config := t.TempDir()
	t.Setenv("FOOTAGE_PATH", footage)
	t.Setenv("CONFIG_PATH", config)

	video := filepath.Join(footage, "clip-front.mp4")
	makeSyntheticMP4(t, video, 1.0)

	const n = 4
	var wg sync.WaitGroup
	bodies := make(chan []byte, n)
	codes := make(chan int, n)
	for i := 0; i < n; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			rw := httptest.NewRecorder()
			rreq, _ := http.NewRequest("GET", "/api/thumbnail/clip-front.mp4?time=0.1", nil)
			r.ServeHTTP(rw, rreq)
			codes <- rw.Code
			bodies <- rw.Body.Bytes()
		}()
	}
	wg.Wait()
	close(codes)
	close(bodies)

	for code := range codes {
		if code != http.StatusOK {
			t.Fatalf("concurrent thumbnail expected 200, got %d", code)
		}
	}
	for body := range bodies {
		if len(body) < 3 || body[0] != 0xFF || body[1] != 0xD8 {
			t.Fatalf("concurrent thumbnail was not a JPEG (len=%d)", len(body))
		}
	}

	entries, err := os.ReadDir(filepath.Join(config, "thumbnails"))
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 1 {
		names := make([]string, 0, len(entries))
		for _, e := range entries {
			names = append(names, e.Name())
		}
		t.Fatalf("expected 1 published thumbnail, got %d (%v)", len(entries), names)
	}
}
