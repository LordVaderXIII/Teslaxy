package services

import (
	"encoding/json"
	"io/ioutil"
	"os"
	"path/filepath"
	"testing"

	"github.com/jinzhu/gorm"
	_ "github.com/jinzhu/gorm/dialects/sqlite"
	"teslaxy/models"
	pb "teslaxy/proto"
)

func TestScanner_CreateTelemetryFromEventJson(t *testing.T) {
	// Setup DB
	db, err := gorm.Open("sqlite3", ":memory:")
	if err != nil {
		t.Fatalf("failed to connect database: %v", err)
	}
	defer db.Close()
	db.AutoMigrate(&models.Clip{}, &models.VideoFile{}, &models.Telemetry{})

	// Setup Temp Dir
	tmpDir, err := ioutil.TempDir("", "scanner_test_event_telemetry")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(tmpDir)

	// Create Clip Directory
	ts := "2024-03-01_12-00-00"
	clipDir := filepath.Join(tmpDir, "SavedClips", ts)
	if err := os.MkdirAll(clipDir, 0755); err != nil {
		t.Fatal(err)
	}

	// Create Dummy MP4
	mp4Path := filepath.Join(clipDir, ts+"-front.mp4")
	ioutil.WriteFile(mp4Path, []byte("dummy"), 0644)

	// Create event.json with coordinates
	eventJsonPath := filepath.Join(clipDir, "event.json")
	eventData := map[string]interface{}{
		"timestamp": "2024-03-01T12:00:00",
		"city":      "Test City",
		"est_lat":   37.7749,
		"est_lon":   -122.4194,
		"reason":    "sentry_aware_object_detection",
	}
	eventBytes, _ := json.Marshal(eventData)
	ioutil.WriteFile(eventJsonPath, eventBytes, 0644)

	// Initialize Scanner
	scanner := NewScannerService(tmpDir, db)

	// Mock SEI Extractor to FAIL (return no metadata)
	scanner.SEIExtractor = func(path string) ([]*pb.SeiMetadata, error) {
		return nil, nil
	}

	scanner.ScanAll()

	// Verify Clip
	var clip models.Clip
	if err := db.Preload("Telemetry").First(&clip).Error; err != nil {
		t.Fatalf("failed to find clip: %v", err)
	}

	// Check if Telemetry was created
	if clip.TelemetryID == 0 {
		t.Errorf("expected TelemetryID to be set, got 0")
	}

	// Check Telemetry Data
	if clip.Telemetry.ID == 0 {
		t.Errorf("expected Telemetry record to be loaded")
	}

	expectedLat := 37.7749
	expectedLon := -122.4194
	// Allow small float error
	if clip.Telemetry.Latitude < expectedLat-0.0001 || clip.Telemetry.Latitude > expectedLat+0.0001 {
		t.Errorf("expected Latitude %f, got %f", expectedLat, clip.Telemetry.Latitude)
	}
	if clip.Telemetry.Longitude < expectedLon-0.0001 || clip.Telemetry.Longitude > expectedLon+0.0001 {
		t.Errorf("expected Longitude %f, got %f", expectedLon, clip.Telemetry.Longitude)
	}
}
