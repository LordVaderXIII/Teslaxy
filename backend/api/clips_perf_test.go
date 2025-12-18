package api

import (
	"teslaxy/database"
	"teslaxy/models"
	"testing"

	"github.com/jinzhu/gorm"
	_ "github.com/jinzhu/gorm/dialects/sqlite"
)

func TestGetClipsPerformance(t *testing.T) {
	// Setup in-memory DB
	var err error
	database.DB, err = gorm.Open("sqlite3", ":memory:")
	if err != nil {
		t.Fatalf("Failed to open DB: %v", err)
	}
	defer database.DB.Close()

	database.DB.AutoMigrate(&models.Clip{}, &models.Telemetry{}, &models.VideoFile{})

	// Create test data
	// Large JSON string to simulate heavy payload
	largeJson := "some massive json payload that takes up space..."
	clip := models.Clip{
		Event: "Sentry",
	}
	database.DB.Create(&clip)

	telemetry := models.Telemetry{
		ClipID:       clip.ID,
		Latitude:     37.7749,
		Longitude:    -122.4194,
		FullDataJson: largeJson,
	}
	database.DB.Create(&telemetry)

	videoFile := models.VideoFile{
		ClipID:   clip.ID,
		Camera:   "Front",
		FilePath: "/test/front.mp4",
	}
	database.DB.Create(&videoFile)

	// Test the optimized query
	var clips []models.Clip
	err = database.DB.Select("id, timestamp, event_timestamp, event, city, telemetry_id").
		Preload("VideoFiles", func(db *gorm.DB) *gorm.DB {
			return db.Select("clip_id, camera, file_path, timestamp")
		}).
		Preload("Telemetry", func(db *gorm.DB) *gorm.DB {
			return db.Select("id, clip_id, latitude, longitude") // Only select needed fields
		}).Find(&clips).Error

	if err != nil {
		t.Fatalf("Query failed: %v", err)
	}

	if len(clips) != 1 {
		t.Fatalf("Expected 1 clip, got %d", len(clips))
	}

	tClip := clips[0]

	// Verify Clip optimization
	if !tClip.CreatedAt.IsZero() {
		t.Errorf("Expected Clip.CreatedAt to be zero (not fetched), got %v", tClip.CreatedAt)
	}
	if tClip.Event != "Sentry" {
		t.Errorf("Expected Clip.Event to be Sentry, got %s", tClip.Event)
	}

	// Verify Telemetry optimization
	if tClip.Telemetry.Latitude != 37.7749 {
		t.Errorf("Expected Latitude 37.7749, got %f", tClip.Telemetry.Latitude)
	}
	if tClip.Telemetry.FullDataJson != "" {
		t.Errorf("Expected FullDataJson to be empty, got '%s'", tClip.Telemetry.FullDataJson)
	}
	if !tClip.Telemetry.CreatedAt.IsZero() {
		t.Errorf("Expected Telemetry.CreatedAt to be zero, got %v", tClip.Telemetry.CreatedAt)
	}

	// Verify VideoFile optimization
	if len(tClip.VideoFiles) != 1 {
		t.Fatalf("Expected 1 VideoFile, got %d", len(tClip.VideoFiles))
	}
	tFile := tClip.VideoFiles[0]
	if tFile.FilePath != "/test/front.mp4" {
		t.Errorf("Expected FilePath /test/front.mp4, got %s", tFile.FilePath)
	}
	if !tFile.CreatedAt.IsZero() {
		t.Errorf("Expected VideoFile.CreatedAt to be zero, got %v", tFile.CreatedAt)
	}
}
