package services

import (
	"io/ioutil"
	"os"
	"path/filepath"
	"testing"

	"github.com/jinzhu/gorm"
	_ "github.com/jinzhu/gorm/dialects/sqlite"
	"teslaxy/models"
)

func TestScanner_RecentMerge(t *testing.T) {
	// Ensure UTC for test consistency
	os.Setenv("DEFAULT_TIMEZONE", "UTC")
	defer os.Unsetenv("DEFAULT_TIMEZONE")

	// Setup DB
	db, err := gorm.Open("sqlite3", ":memory:")
	if err != nil {
		t.Fatalf("failed to connect database: %v", err)
	}
	defer db.Close()
	db.AutoMigrate(&models.Clip{}, &models.VideoFile{}, &models.Telemetry{})

	// Setup Temp Dir
	tmpDir, err := ioutil.TempDir("", "scanner_test_merge")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(tmpDir)

	// Create RecentClips Dir
	recentDir := filepath.Join(tmpDir, "RecentClips")

	// Create Folder A (Time 12:00:00)
	dirA := filepath.Join(recentDir, "FolderA")
	if err := os.MkdirAll(dirA, 0755); err != nil {
		t.Fatal(err)
	}
	fA := filepath.Join(dirA, "2025-12-14_12-00-00-front.mp4")
	ioutil.WriteFile(fA, []byte("dummy"), 0644)

	// Create Folder B (Time 12:01:00)
	// 60 seconds diff. Should merge.
	dirB := filepath.Join(recentDir, "FolderB")
	if err := os.MkdirAll(dirB, 0755); err != nil {
		t.Fatal(err)
	}
	fB := filepath.Join(dirB, "2025-12-14_12-01-00-front.mp4")
	ioutil.WriteFile(fB, []byte("dummy"), 0644)

	// Initialize Scanner
	scanner := NewScannerService(tmpDir, db)
	scanner.ScanAll()

	// Verify Clips
	var clips []models.Clip
	db.Find(&clips)

	if len(clips) != 1 {
		t.Errorf("expected 1 clip, got %d", len(clips))
		for _, c := range clips {
			t.Logf("Clip: ID=%d Timestamp=%v Event=%s", c.ID, c.Timestamp, c.Event)
		}
	} else {
		// Check if both files are in the same clip
		var videoFiles []models.VideoFile
		db.Where("clip_id = ?", clips[0].ID).Find(&videoFiles)
		if len(videoFiles) != 2 {
			t.Errorf("expected 2 video files in clip, got %d", len(videoFiles))
		}
	}
}

func TestScanner_RecentSplit(t *testing.T) {
	// Test that a large gap DOES split
	os.Setenv("DEFAULT_TIMEZONE", "UTC")
	defer os.Unsetenv("DEFAULT_TIMEZONE")

	db, err := gorm.Open("sqlite3", ":memory:")
	if err != nil {
		t.Fatalf("failed to connect database: %v", err)
	}
	defer db.Close()
	db.AutoMigrate(&models.Clip{}, &models.VideoFile{}, &models.Telemetry{})

	tmpDir, err := ioutil.TempDir("", "scanner_test_split")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(tmpDir)

	recentDir := filepath.Join(tmpDir, "RecentClips")

	// File A: 12:00:00
	dirA := filepath.Join(recentDir, "FolderA")
	if err := os.MkdirAll(dirA, 0755); err != nil {
		t.Fatal(err)
	}
	fA := filepath.Join(dirA, "2025-12-14_12-00-00-front.mp4")
	ioutil.WriteFile(fA, []byte("dummy"), 0644)

	// File B: 12:05:00 (5 mins later > 90s)
	dirB := filepath.Join(recentDir, "FolderB")
	if err := os.MkdirAll(dirB, 0755); err != nil {
		t.Fatal(err)
	}
	fB := filepath.Join(dirB, "2025-12-14_12-05-00-front.mp4")
	ioutil.WriteFile(fB, []byte("dummy"), 0644)

	scanner := NewScannerService(tmpDir, db)
	scanner.ScanAll()

	var clips []models.Clip
	db.Find(&clips)

	if len(clips) != 2 {
		t.Errorf("expected 2 clips, got %d", len(clips))
	}
}
