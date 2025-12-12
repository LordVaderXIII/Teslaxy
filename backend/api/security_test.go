package api

import (
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

	"github.com/gin-gonic/gin"
)

func TestServeVideo_PathTraversal(t *testing.T) {
	// Setup
	gin.SetMode(gin.TestMode)
	r := gin.Default()
	r.GET("/api/video/*path", serveVideo)

	// Create a dummy footage directory and file
	footageDir := "/tmp/footage_test"
	os.MkdirAll(footageDir, 0755)
	defer os.RemoveAll(footageDir)

	allowedFile := filepath.Join(footageDir, "test.mp4")
	os.WriteFile(allowedFile, []byte("video content"), 0644)

	// Create a sensitive file outside footage dir
	sensitiveFile := "/tmp/sensitive_test.txt"
	os.WriteFile(sensitiveFile, []byte("secret password"), 0644)
	defer os.Remove(sensitiveFile)

	// Set env var so the handler knows where footage is allowed
	os.Setenv("FOOTAGE_PATH", footageDir)
	defer os.Unsetenv("FOOTAGE_PATH")

	t.Run("Access allowed file", func(t *testing.T) {
		w := httptest.NewRecorder()
		req, _ := http.NewRequest("GET", "/api/video"+allowedFile, nil)
		r.ServeHTTP(w, req)
		if w.Code != 200 {
			t.Errorf("Expected 200, got %d", w.Code)
		}
		if w.Body.String() != "video content" {
			t.Errorf("Expected 'video content', got '%s'", w.Body.String())
		}
	})

	t.Run("Access sensitive file (Vulnerability Check)", func(t *testing.T) {
		w := httptest.NewRecorder()
		req, _ := http.NewRequest("GET", "/api/video"+sensitiveFile, nil)
		r.ServeHTTP(w, req)

		// The path is cleaned and joined, resulting in a lookup inside the footage directory.
		// Since the file doesn't exist there, we expect 404.
		// If it managed to traverse out, we would expect 200 (if it found the file) or 403 (if the security check caught it).
		// 404 confirms it failed to access the sensitive file.
		if w.Code != 404 {
			t.Errorf("Expected 404, got %d", w.Code)
		}
	})
}
