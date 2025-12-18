package api

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"testing"

	"github.com/gin-gonic/gin"
)

func TestLogin_Security(t *testing.T) {
	gin.SetMode(gin.TestMode)

	t.Run("Default Insecure Password Disabled", func(t *testing.T) {
		// Ensure environment is clean (simulating first run without env vars)
		// Note: init() has already run, so adminPass is already random.
		// We just verify that "tesla" no longer works.

		r := gin.New()
		r.POST("/api/login", Login)

		creds := map[string]string{
			"username": "admin",
			"password": "tesla",
		}
		body, _ := json.Marshal(creds)

		w := httptest.NewRecorder()
		req, _ := http.NewRequest("POST", "/api/login", bytes.NewBuffer(body))
		req.Header.Set("Content-Type", "application/json")

		r.ServeHTTP(w, req)

		if w.Code != 401 {
			t.Errorf("Expected 401 Unauthorized (default insecure password disabled), got %d", w.Code)
		}
	})

	t.Run("Custom Password via Env Var", func(t *testing.T) {
		// Set custom password
		expectedPass := "MySecurePass123!"
		os.Setenv("ADMIN_PASS", expectedPass)
		defer os.Unsetenv("ADMIN_PASS")

		// Reload credentials to pick up the env var
		// Note: loadAdminCreds is available because we are in package api
		loadAdminCreds()

		r := gin.New()
		r.POST("/api/login", Login)

		creds := map[string]string{
			"username": "admin",
			"password": expectedPass,
		}
		body, _ := json.Marshal(creds)

		w := httptest.NewRecorder()
		req, _ := http.NewRequest("POST", "/api/login", bytes.NewBuffer(body))
		req.Header.Set("Content-Type", "application/json")

		r.ServeHTTP(w, req)

		if w.Code != 200 {
			t.Errorf("Expected 200 OK with custom password, got %d. Body: %s", w.Code, w.Body.String())
		}

		// Cleanup: Reload again to restore random state
		os.Unsetenv("ADMIN_PASS")
		loadAdminCreds()
	})
}
