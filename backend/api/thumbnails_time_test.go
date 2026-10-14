package api

import (
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

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
