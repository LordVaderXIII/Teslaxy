package services

import (
	"io/ioutil"
	"os"
	"path/filepath"
	"testing"

	"github.com/jinzhu/gorm"
	_ "github.com/jinzhu/gorm/dialects/sqlite"
	"teslaxy/models"
	pb "teslaxy/proto"
)

func TestScanner_CityFallbackFromTelemetry(t *testing.T) {
	// Setup DB
	db, err := gorm.Open("sqlite3", ":memory:")
	if err != nil {
		t.Fatalf("failed to connect database: %v", err)
	}
	defer db.Close()
	db.AutoMigrate(&models.Clip{}, &models.VideoFile{}, &models.Telemetry{})

	// Setup Temp Dir
	tmpDir, err := ioutil.TempDir("", "scanner_test_telemetry")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(tmpDir)

	// Create Clip Directory
	clipDir := filepath.Join(tmpDir, "SavedClips", "2024-03-01_12-00-00")
	if err := os.MkdirAll(clipDir, 0755); err != nil {
		t.Fatal(err)
	}

	// Create Dummy MP4
	mp4Path := filepath.Join(clipDir, "2024-03-01_12-00-00-front.mp4")
	ioutil.WriteFile(mp4Path, []byte("dummy"), 0644)

	// No event.json created, or one without city
	// So city will be "" initially.

	// Initialize Scanner with Mock SEI Extractor
	scanner := NewScannerService(tmpDir, db)

	// Mock SEI Extractor
	scanner.SEIExtractor = func(path string) ([]*pb.SeiMetadata, error) {
		// Return dummy metadata with coordinates
		return []*pb.SeiMetadata{
			{
				LatitudeDeg:  40.7128,
				LongitudeDeg: -74.0060,
			},
		}, nil
	}

	scanner.ScanAll()

	// Verify Clip
	var clip models.Clip
	if err := db.First(&clip).Error; err != nil {
		t.Fatalf("failed to find clip: %v", err)
	}

	// Check City Fallback
	// Expected: "40.7128, -74.0060"
	expected := "40.7128, -74.0060"
	if clip.City != expected {
		t.Errorf("expected City '%s', got '%s'", expected, clip.City)
	}
}
