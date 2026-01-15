package services

import (
	"testing"
	"time"

	"github.com/jinzhu/gorm"
	_ "github.com/jinzhu/gorm/dialects/sqlite"
	"teslaxy/database"
	"teslaxy/models"
)

func setupTestDB() {
	var err error
	database.DB, err = gorm.Open("sqlite3", ":memory:")
	if err != nil {
		panic(err)
	}
	database.DB.AutoMigrate(&models.Clip{}, &models.VideoFile{}, &models.Telemetry{})
}

func TestExportRequest_Validate(t *testing.T) {
	tests := []struct {
		name    string
		req     ExportRequest
		wantErr bool
	}{
		{
			name: "Valid Request",
			req: ExportRequest{
				ClipID:    1,
				Cameras:   []string{"front"},
				StartTime: 0,
				Duration:  60,
			},
			wantErr: false,
		},
		{
			name: "Negative Duration",
			req: ExportRequest{
				ClipID:    1,
				Cameras:   []string{"front"},
				StartTime: 0,
				Duration:  -10,
			},
			wantErr: true,
		},
		{
			name: "Zero Duration",
			req: ExportRequest{
				ClipID:    1,
				Cameras:   []string{"front"},
				StartTime: 0,
				Duration:  0,
			},
			wantErr: true,
		},
		{
			name: "Duration Too Long (DoS Protection)",
			req: ExportRequest{
				ClipID:    1,
				Cameras:   []string{"front"},
				StartTime: 0,
				Duration:  36000,
			},
			wantErr: true,
		},
		{
			name: "Negative StartTime",
			req: ExportRequest{
				ClipID:    1,
				Cameras:   []string{"front"},
				StartTime: -1,
				Duration:  10,
			},
			wantErr: true,
		},
		{
			name: "No Cameras",
			req: ExportRequest{
				ClipID:    1,
				Cameras:   []string{},
				StartTime: 0,
				Duration:  10,
			},
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if err := tt.req.Validate(); (err != nil) != tt.wantErr {
				t.Errorf("ExportRequest.Validate() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}

func TestQueueExport_ValidationIntegration(t *testing.T) {
	setupTestDB()
	defer database.DB.Close()

	// Seed a clip
	clip := models.Clip{
		Timestamp: time.Now(),
	}
	database.DB.Create(&clip)

	// Attempt with invalid duration
	req := ExportRequest{
		ClipID:    clip.ID,
		Cameras:   []string{"front"},
		StartTime: 0,
		Duration:  -100,
	}

	_, err := QueueExport(req)
	if err == nil {
		t.Error("QueueExport should have rejected invalid request")
	}
}
