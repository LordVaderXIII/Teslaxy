package api

import (
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
)

func TestGetThumbnail(t *testing.T) {
	// Setup Gin
	gin.SetMode(gin.TestMode)
	r := gin.New()
	r.GET("/api/thumbnail/*path", getThumbnail)

	// Setup Temp Footage
	tmpFootage := os.TempDir()
	os.Setenv("FOOTAGE_PATH", tmpFootage)
	defer os.Unsetenv("FOOTAGE_PATH")

	// Setup Temp Config (for thumbnails)
	tmpConfig := os.TempDir()
	os.Setenv("CONFIG_PATH", tmpConfig)
	defer os.Unsetenv("CONFIG_PATH")

	// Create dummy video file
	videoPath := filepath.Join(tmpFootage, "test_thumb.mp4")
	// Creating a real MP4 or just a file? FFmpeg needs a real file or it fails.
	// Since we mock the environment but not FFmpeg, we might fail if ffmpeg is not installed or file is invalid.
	// For this test, we might just check if it attempts to execute FFmpeg or fails gracefully.
	// However, without a real video, ffmpeg will return error.
	// We can check validation logic.

	if err := os.WriteFile(videoPath, []byte("dummy content"), 0644); err != nil {
		t.Fatal(err)
	}
	defer os.Remove(videoPath)

	t.Run("Invalid Time Parameter", func(t *testing.T) {
		w := httptest.NewRecorder()
		req, _ := http.NewRequest("GET", "/api/thumbnail/test_thumb.mp4?time=invalid", nil)
		r.ServeHTTP(w, req)

		assert.Equal(t, http.StatusBadRequest, w.Code)
	})

	t.Run("Invalid Width Parameter Defaults", func(t *testing.T) {
		w := httptest.NewRecorder()
		// Should not fail validation, but fallback to 480.
		// Since file exists (dummy), it should proceed to ffmpeg and likely fail (500) or cache check.
		// We expect 500 because dummy content is not valid video for ffmpeg.
		// But validation should pass (not 400).
		req, _ := http.NewRequest("GET", "/api/thumbnail/test_thumb.mp4?w=invalid", nil)
		r.ServeHTTP(w, req)

		assert.NotEqual(t, http.StatusBadRequest, w.Code)
	})

	// We can't easily test success without ffmpeg and valid video in this environment
	// unless we mock exec.Command which is hard in Go without dependency injection.
	// But we verified the input validation.
}
