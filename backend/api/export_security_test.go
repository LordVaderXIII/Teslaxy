package api

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"
	"teslaxy/services"
)

func TestCreateExportJob_Validation(t *testing.T) {
	gin.SetMode(gin.TestMode)
	r := gin.New()
	r.POST("/api/export", createExportJob)

	tests := []struct {
		name       string
		req        services.ExportRequest
		wantStatus int
	}{
		{
			name: "Valid Request",
			req: services.ExportRequest{
				ClipID:    1,
				Cameras:   []string{"front"},
				StartTime: 0,
				Duration:  10,
			},
			// Expect 500 because QueueExport tries to access DB which isn't mocked here.
			// Ideally we mock QueueExport or DB.
			// But since we just want to test validation passing, we know if it passes validation it proceeds to QueueExport.
			// If QueueExport fails with DB error, it returns 500.
			wantStatus: 500,
		},
		{
			name: "Invalid Duration",
			req: services.ExportRequest{
				ClipID:    1,
				Cameras:   []string{"front"},
				StartTime: 0,
				Duration:  -1,
			},
			wantStatus: 400,
		},
		{
			name: "Invalid StartTime",
			req: services.ExportRequest{
				ClipID:    1,
				Cameras:   []string{"front"},
				StartTime: -1,
				Duration:  10,
			},
			wantStatus: 400,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			body, _ := json.Marshal(tt.req)
			w := httptest.NewRecorder()
			req, _ := http.NewRequest("POST", "/api/export", bytes.NewBuffer(body))
			req.Header.Set("Content-Type", "application/json")
			r.ServeHTTP(w, req)

			if w.Code != tt.wantStatus {
				t.Errorf("Expected status %d, got %d. Body: %s", tt.wantStatus, w.Code, w.Body.String())
			}
		})
	}
}
