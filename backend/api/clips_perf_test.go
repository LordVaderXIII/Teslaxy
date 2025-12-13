package api

import (
	"testing"
	"teslaxy/database"
	"teslaxy/models"

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

	database.DB.AutoMigrate(&models.Clip{}, &models.Telemetry{})

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

	// Test the optimized query
	var clips []models.Clip
	err = database.DB.Preload("Telemetry", func(db *gorm.DB) *gorm.DB {
		return db.Select("id, clip_id, latitude, longitude") // Only select needed fields
	}).Find(&clips).Error

	if err != nil {
		t.Fatalf("Query failed: %v", err)
	}

	if len(clips) != 1 {
		t.Fatalf("Expected 1 clip, got %d", len(clips))
	}

	tClip := clips[0]
	if tClip.Telemetry.Latitude != 37.7749 {
		t.Errorf("Expected Latitude 37.7749, got %f", tClip.Telemetry.Latitude)
	}
	if tClip.Telemetry.FullDataJson != "" {
		t.Errorf("Expected FullDataJson to be empty, got '%s'", tClip.Telemetry.FullDataJson)
	}
}
