package api

import (
	"net/http"
	"net/http/httptest"
	"testing"
	"strings"

	"github.com/gin-gonic/gin"
)

func TestCSPHeader(t *testing.T) {
	gin.SetMode(gin.TestMode)
	r := gin.New()
	SetupRoutes(r)

	w := httptest.NewRecorder()
	req, _ := http.NewRequest("GET", "/api/version", nil)
	r.ServeHTTP(w, req)

	csp := w.Header().Get("Content-Security-Policy")
	if csp == "" {
		t.Error("Expected Content-Security-Policy header, got empty")
	}

	expectedDirectives := []string{
		"default-src 'self'",
		"script-src 'self' 'unsafe-inline'",
		"style-src 'self' 'unsafe-inline'",
		"img-src 'self' data: blob: https://*.basemaps.cartocdn.com",
		"connect-src 'self'",
		"media-src 'self' blob:",
		"worker-src 'self' blob:",
		"object-src 'none'",
		"frame-ancestors 'self'",
	}

	for _, d := range expectedDirectives {
		if !strings.Contains(csp, d) {
			t.Errorf("CSP header missing directive '%s'. Got: %s", d, csp)
		}
	}
}
