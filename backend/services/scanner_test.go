package services

import (
	"io/ioutil"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/jinzhu/gorm"
	_ "github.com/jinzhu/gorm/dialects/sqlite"
	"teslaxy/models"
)

func TestScanner_EventTimestamp(t *testing.T) {
	// Setup DB
	db, err := gorm.Open("sqlite3", ":memory:")
	if err != nil {
		t.Fatalf("failed to connect database: %v", err)
	}
	defer db.Close()
	db.AutoMigrate(&models.Clip{}, &models.VideoFile{}, &models.Telemetry{})

	// Setup Temp Dir
	tmpDir, err := ioutil.TempDir("", "scanner_test")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(tmpDir)

	// Create Clip Directory
	// Tesla usually nests in SavedClips/YYYY-MM-DD_HH-MM-SS
	clipDir := filepath.Join(tmpDir, "SavedClips", "2023-10-27_10-00-00")
	if err := os.MkdirAll(clipDir, 0755); err != nil {
		t.Fatal(err)
	}

	// Create Dummy MP4
	// Filename: YYYY-MM-DD_HH-MM-SS-camera.mp4
	mp4Path := filepath.Join(clipDir, "2023-10-27_10-00-00-front.mp4")
	if err := ioutil.WriteFile(mp4Path, []byte("dummy video"), 0644); err != nil {
		t.Fatal(err)
	}

	// Create event.json
	// The video starts at 10:00:00. Let's say the event is at 10:00:30.
	eventTimeStr := "2023-10-27T10:00:30"
	eventJsonContent := `{"timestamp": "` + eventTimeStr + `", "reason": "sentry_aware_object_detection"}`
	eventJsonPath := filepath.Join(clipDir, "event.json")
	if err := ioutil.WriteFile(eventJsonPath, []byte(eventJsonContent), 0644); err != nil {
		t.Fatal(err)
	}

	// Initialize Scanner
	scanner := NewScannerService(tmpDir, db)
	scanner.ScanAll()

	// Verify Clip
	var clip models.Clip
	if err := db.First(&clip).Error; err != nil {
		t.Fatalf("failed to find clip: %v", err)
	}

	// Check Timestamp (Start Time)
	expectedStart, _ := time.Parse("2006-01-02_15-04-05", "2023-10-27_10-00-00")
	if !clip.Timestamp.Equal(expectedStart) {
		t.Errorf("expected clip timestamp %v, got %v", expectedStart, clip.Timestamp)
	}

	// Check EventTimestamp
	if clip.EventTimestamp == nil {
		t.Fatal("expected EventTimestamp to be populated, got nil")
	}
}

func TestScanner_CityFallback(t *testing.T) {
	// Setup DB
	db, err := gorm.Open("sqlite3", ":memory:")
	if err != nil {
		t.Fatalf("failed to connect database: %v", err)
	}
	defer db.Close()
	db.AutoMigrate(&models.Clip{}, &models.VideoFile{}, &models.Telemetry{})

	// Setup Temp Dir
	tmpDir, err := ioutil.TempDir("", "scanner_test_city")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(tmpDir)

	// Create Clip Directory
	clipDir := filepath.Join(tmpDir, "SavedClips", "2024-02-19_10-00-00")
	if err := os.MkdirAll(clipDir, 0755); err != nil {
		t.Fatal(err)
	}

	// Create Dummy MP4
	mp4Path := filepath.Join(clipDir, "2024-02-19_10-00-00-front.mp4")
	ioutil.WriteFile(mp4Path, []byte("dummy"), 0644)

	// Create event.json with missing city but present lat/lon
	eventTimeStr := "2024-02-19T10:00:10"
	eventJsonContent := `{"timestamp": "` + eventTimeStr + `", "city": "", "est_lat": 37.1234, "est_lon": -122.5678, "reason": "test"}`
	ioutil.WriteFile(filepath.Join(clipDir, "event.json"), []byte(eventJsonContent), 0644)

	// Initialize Scanner
	scanner := NewScannerService(tmpDir, db)
	scanner.ScanAll()

	// Verify Clip
	var clip models.Clip
	if err := db.First(&clip).Error; err != nil {
		t.Fatalf("failed to find clip: %v", err)
	}

	// Check City Fallback
	expected := "37.1234, -122.5678"
	if clip.City != expected {
		t.Errorf("expected City '%s', got '%s'", expected, clip.City)
	}
}
