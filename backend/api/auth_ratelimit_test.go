package api

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"
)

func TestLogin_RateLimit(t *testing.T) {
	gin.SetMode(gin.TestMode)

	r := gin.New()
	r.POST("/api/login", Login)

	creds := map[string]string{
		"username": "admin",
		"password": "wrongpassword",
	}
	body, _ := json.Marshal(creds)

	// Helper to make a request
	makeRequest := func() int {
		w := httptest.NewRecorder()
		req, _ := http.NewRequest("POST", "/api/login", bytes.NewBuffer(body))
		req.Header.Set("Content-Type", "application/json")
		// Mock Client IP via RemoteAddr (Gin uses this fallback)
		req.RemoteAddr = "192.168.1.100:12345"

		r.ServeHTTP(w, req)
		return w.Code
	}

	// Make 5 requests (allowed)
	for i := 0; i < 5; i++ {
		code := makeRequest()
		// We expect 401 because credentials are wrong, but NOT 429
		if code != 401 {
			t.Errorf("Request %d: Expected 401, got %d", i+1, code)
		}
	}

	// Make 6th request (should be blocked)
	code := makeRequest()
	if code != 429 {
		t.Errorf("Request 6: Expected 429 Too Many Requests, got %d", code)
	}
}
