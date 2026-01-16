package api

import (
	"bytes"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"
)

func TestCreateExportJob_InputValidation(t *testing.T) {
	gin.SetMode(gin.TestMode)
	r := gin.New()
	// Setup only the route we are testing
	r.POST("/api/export", createExportJob)

	t.Run("Reject Negative StartTime", func(t *testing.T) {
		w := httptest.NewRecorder()
		body := bytes.NewBufferString(`{
			"clip_id": 1,
			"cameras": ["front"],
			"start_time": -10.0,
			"duration": 10.0
		}`)
		req, _ := http.NewRequest("POST", "/api/export", body)
		req.Header.Set("Content-Type", "application/json")
		r.ServeHTTP(w, req)

		if w.Code != 400 {
			t.Errorf("Expected 400 Bad Request, got %d", w.Code)
		}
	})

	t.Run("Reject Excessive Duration", func(t *testing.T) {
		w := httptest.NewRecorder()
		body := bytes.NewBufferString(`{
			"clip_id": 1,
			"cameras": ["front"],
			"start_time": 0.0,
			"duration": 3600.0
		}`)
		req, _ := http.NewRequest("POST", "/api/export", body)
		req.Header.Set("Content-Type", "application/json")
		r.ServeHTTP(w, req)

		if w.Code != 400 {
			t.Errorf("Expected 400 Bad Request for excessive duration, got %d", w.Code)
		}
	})

    t.Run("Reject Empty Cameras", func(t *testing.T) {
		w := httptest.NewRecorder()
		body := bytes.NewBufferString(`{
			"clip_id": 1,
			"cameras": [],
			"start_time": 0.0,
			"duration": 10.0
		}`)
		req, _ := http.NewRequest("POST", "/api/export", body)
		req.Header.Set("Content-Type", "application/json")
		r.ServeHTTP(w, req)

		if w.Code != 400 {
			t.Errorf("Expected 400 Bad Request for empty cameras, got %d", w.Code)
		}
	})

	t.Run("Reject Invalid Camera Name", func(t *testing.T) {
		w := httptest.NewRecorder()
		body := bytes.NewBufferString(`{
			"clip_id": 1,
			"cameras": ["front", "invalid_camera"],
			"start_time": 0.0,
			"duration": 10.0
		}`)
		req, _ := http.NewRequest("POST", "/api/export", body)
		req.Header.Set("Content-Type", "application/json")
		r.ServeHTTP(w, req)

		if w.Code != 400 {
			t.Errorf("Expected 400 Bad Request for invalid camera name, got %d", w.Code)
		}
	})
}
