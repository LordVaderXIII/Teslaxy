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

	// Verify the parsed time
	// Note: event.json timestamp might be in local time or UTC. Assuming scanner parses it correctly.
	// Typically ISO8601 can be parsed by time.Parse(time.RFC3339, ...) if it has T.
	// But Tesla format in JSON is "YYYY-MM-DDTHH:MM:SS" (no Z usually, implies local or matches video)
	// Let's see how I implement it.

	// Since I haven't implemented it yet, this test will FAIL at clip.EventTimestamp == nil.
}
