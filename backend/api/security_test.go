package api

import (
	"bytes"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
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

func TestCSPHeader(t *testing.T) {
	gin.SetMode(gin.TestMode)
	r := gin.New()
	// Use the middleware that should have CSP
	r.Use(SecurityHeadersMiddleware())
	r.GET("/ping", func(c *gin.Context) {
		c.String(200, "pong")
	})

	w := httptest.NewRecorder()
	req, _ := http.NewRequest("GET", "/ping", nil)
	r.ServeHTTP(w, req)

	csp := w.Header().Get("Content-Security-Policy")
	if csp == "" {
		t.Error("Expected Content-Security-Policy header to be set, but it was empty")
	}

	// Verify critical directives if CSP exists
	requiredDirectives := []string{
		"default-src 'self'",
		"script-src 'self' 'unsafe-inline'", // Required for React/Vite without nonces
		"object-src 'none'",
	}

	for _, directive := range requiredDirectives {
		if !strings.Contains(csp, directive) {
			t.Errorf("Expected CSP to contain directive '%s', got: %s", directive, csp)
		}
	}
}

func TestSecurityHeaders(t *testing.T) {
	gin.SetMode(gin.TestMode)
	r := gin.New()
	SetupRoutes(r)

	w := httptest.NewRecorder()
	req, _ := http.NewRequest("GET", "/api/version", nil)
	r.ServeHTTP(w, req)

	expectedHeaders := map[string]string{
		"X-Content-Type-Options": "nosniff",
		"X-Frame-Options":        "SAMEORIGIN",
		"X-XSS-Protection":       "1; mode=block",
		"Referrer-Policy":        "strict-origin-when-cross-origin",
	}

	for key, val := range expectedHeaders {
		if got := w.Header().Get(key); got != val {
			t.Errorf("Expected header %s to be %s, got %s", key, val, got)
		}
	}
}

func TestCORS(t *testing.T) {
	gin.SetMode(gin.TestMode)
	r := gin.New()
	SetupRoutes(r)

	// Case 1: Standard API endpoint should NOT have CORS headers (Security: Restrict access)
	t.Run("API Clips - No CORS", func(t *testing.T) {
		w := httptest.NewRecorder()
		req, _ := http.NewRequest("GET", "/api/clips", nil)
		req.Header.Set("Authorization", "Bearer invalid-but-present") // Middleware check
		r.ServeHTTP(w, req)

		if w.Header().Get("Access-Control-Allow-Origin") != "" {
			t.Errorf("Expected no Access-Control-Allow-Origin header for /api/clips")
		}
	})

	// Case 2: Video endpoint MUST have CORS headers for 3D textures
	t.Run("Video Endpoint - Has CORS", func(t *testing.T) {
		w := httptest.NewRecorder()
		// Setup mock file
		footageDir := "/tmp/footage_cors_test"
		os.MkdirAll(footageDir, 0755)
		defer os.RemoveAll(footageDir)
		os.Setenv("FOOTAGE_PATH", footageDir)
		defer os.Unsetenv("FOOTAGE_PATH")
		os.WriteFile(filepath.Join(footageDir, "test.mp4"), []byte("video"), 0644)

		req, _ := http.NewRequest("GET", "/api/video/test.mp4", nil)
		// We need to bypass auth for this test or provide valid token?
		// AuthMiddleware is enabled by default if AUTH_ENABLED is true.
		// If AUTH_ENABLED is false (default env), AuthMiddleware calls Next().
		// Wait, os.Getenv("AUTH_ENABLED") is empty in tests unless set.
		// So AuthMiddleware is disabled.
		r.ServeHTTP(w, req)

		if w.Header().Get("Access-Control-Allow-Origin") != "*" {
			t.Errorf("Expected Access-Control-Allow-Origin: * for /api/video, got '%s'", w.Header().Get("Access-Control-Allow-Origin"))
		}
	})
}

func TestMaxBodySize(t *testing.T) {
	gin.SetMode(gin.TestMode)
	r := gin.New()
	SetupRoutes(r)

	// We'll test /api/login which uses ShouldBindJSON
	// ShouldBindJSON reads the body
	// Our middleware restricts the body reader

	t.Run("Request within limit", func(t *testing.T) {
		w := httptest.NewRecorder()
		// Small JSON
		body := bytes.NewBufferString(`{"username":"admin", "password":"password"}`)
		req, _ := http.NewRequest("POST", "/api/login", body)
		req.Header.Set("Content-Type", "application/json")
		r.ServeHTTP(w, req)

		// 401 means it processed the body and failed auth (expected)
		// If it failed to read, it would likely be 400
		if w.Code != 401 && w.Code != 200 {
			t.Errorf("Expected 401 (auth failure) or 200, got %d. Body: %s", w.Code, w.Body.String())
		}
	})

	t.Run("Request exceeding limit", func(t *testing.T) {
		w := httptest.NewRecorder()
		// 2MB string
		largeString := strings.Repeat("a", 2*1024*1024)
		body := bytes.NewBufferString(`{"username":"` + largeString + `", "password":"password"}`)
		req, _ := http.NewRequest("POST", "/api/login", body)
		req.Header.Set("Content-Type", "application/json")
		r.ServeHTTP(w, req)

		// Gin's ShouldBindJSON returns error if read fails
		// Our middleware will cause Read to fail or return partial?
		// http.MaxBytesReader: "The server will close the underlying connection if the limit is exceeded."
		// And Read returns "request body too large".
		// Gin ShouldBindJSON will catch this error.
		// The handler says: if err := c.ShouldBindJSON(&creds); err != nil { c.JSON(400, ... }

		if w.Code != 400 {
			t.Errorf("Expected 400 (Bad Request due to body size), got %d. Body: %s", w.Code, w.Body.String())
		}
	})
}
