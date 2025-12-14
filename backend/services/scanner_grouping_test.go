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

func TestScanner_GroupingAndRegex(t *testing.T) {
	// Ensure UTC
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
	tmpDir, err := ioutil.TempDir("", "scanner_grouping_test")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(tmpDir)

	// Create Structure:
	// RecentClips/
	//   2024-01-01/
	//      2024-01-01_10-00-00-front.mp4
	//      2024-01-01_10-00-00-back.mp4
	//      2024-01-01_10-00-00-left_repeater.mp4  (Missing right_repeater -> Incomplete but should process)
	//
	//   2024-01-01/ (Same folder or different, verifying continuity)
	//      2024-01-01_10-01-00_123456-front.mp4  (With milliseconds)
	//      2024-01-01_10-01-00_123456-back.mp4
	//      2024-01-01_10-01-00_123456-left_repeater.mp4
	//      2024-01-01_10-01-00_123456-right_repeater.mp4

    //   Gap > 90s
	//      2024-01-01_12-00-00-front.mp4 (New Clip)

	recentDir := filepath.Join(tmpDir, "RecentClips", "2024-01-01")
	if err := os.MkdirAll(recentDir, 0755); err != nil {
		t.Fatal(err)
	}

	createFile := func(name string) {
		path := filepath.Join(recentDir, name)
		if err := ioutil.WriteFile(path, []byte("dummy"), 0644); err != nil {
			t.Fatal(err)
		}
	}

	// 10:00:00 - Minute 0
	createFile("2024-01-01_10-00-00-front.mp4")
	createFile("2024-01-01_10-00-00-back.mp4")
	createFile("2024-01-01_10-00-00-left_repeater.mp4")

	// 10:01:00 - Minute 1 (Should merge with Minute 0)
    // Testing Milliseconds Regex support
	createFile("2024-01-01_10-01-00_123456-front.mp4")
	createFile("2024-01-01_10-01-00_123456-back.mp4")
    // Testing mixed separator (underscore before camera in filename?)
    // The regex supports it. Let's try one.
    // create "2024-01-01_10-01-00_123456_left_repeater.mp4" (Using _ instead of - before camera)
    // Actually user example was strict on - but regex is flexible. Let's test standard first.
	createFile("2024-01-01_10-01-00_123456-left_repeater.mp4")
	createFile("2024-01-01_10-01-00_123456-right_repeater.mp4")

	// 12:00:00 - Minute 120 (Should be separate)
	createFile("2024-01-01_12-00-00-front.mp4")

	// Run Scanner
	scanner := NewScannerService(tmpDir, db)
	scanner.ScanAll()

	// Assertions
	var clips []models.Clip
	if err := db.Order("timestamp asc").Find(&clips).Error; err != nil {
		t.Fatal(err)
	}

	if len(clips) != 2 {
		t.Errorf("expected 2 clips, got %d", len(clips))
	}

	// Check Clip 1 (Merged 10:00 and 10:01)
	clip1 := clips[0]
	// Should have files for 10:00 and 10:01
	var files1 []models.VideoFile
	db.Where("clip_id = ?", clip1.ID).Find(&files1)

    // 3 files from 10:00 + 4 files from 10:01 = 7 files total
	if len(files1) != 7 {
		t.Errorf("expected 7 files in clip 1, got %d", len(files1))
	}

    // Check timestamps in clip 1
    // 10:00:00
    expectedT1, _ := time.Parse("2006-01-02_15-04-05", "2024-01-01_10-00-00")
    // 10:01:00
    expectedT2, _ := time.Parse("2006-01-02_15-04-05", "2024-01-01_10-01-00")

    countT1 := 0
    countT2 := 0
    for _, f := range files1 {
        if f.Timestamp.Equal(expectedT1) {
            countT1++
        } else if f.Timestamp.Equal(expectedT2) {
            countT2++
        } else {
            t.Errorf("unexpected timestamp in clip 1: %v", f.Timestamp)
        }
    }

    if countT1 != 3 {
        t.Errorf("expected 3 files at 10:00, got %d", countT1)
    }
    if countT2 != 4 {
        t.Errorf("expected 4 files at 10:01, got %d", countT2)
    }

	// Check Clip 2 (12:00)
	clip2 := clips[1]
	var files2 []models.VideoFile
	db.Where("clip_id = ?", clip2.ID).Find(&files2)
	if len(files2) != 1 {
		t.Errorf("expected 1 file in clip 2, got %d", len(files2))
	}
}

func TestScanner_EventGrouping(t *testing.T) {
     // Ensure UTC
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
	tmpDir, err := ioutil.TempDir("", "scanner_event_test")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(tmpDir)

    // SavedClips/DirA/ (Contains 2 distinct events by timestamp)
    savedDir := filepath.Join(tmpDir, "SavedClips", "DirA")
    os.MkdirAll(savedDir, 0755)

    // Event 1: 10:00:00
    ioutil.WriteFile(filepath.Join(savedDir, "2024-01-01_10-00-00-front.mp4"), []byte("data"), 0644)
    ioutil.WriteFile(filepath.Join(savedDir, "2024-01-01_10-00-00-back.mp4"), []byte("data"), 0644)

    // Event 2: 10:05:00
    ioutil.WriteFile(filepath.Join(savedDir, "2024-01-01_10-05-00-front.mp4"), []byte("data"), 0644)

    // event.json (Matches Event 1 timestamp approx)
    // Event Time is 10:00:01 (Within 60s of 10:00:00)
    eventTimeStr := "2024-01-01T10:00:01"
	eventJsonContent := `{"timestamp": "` + eventTimeStr + `", "city": "TestCity"}`
	ioutil.WriteFile(filepath.Join(savedDir, "event.json"), []byte(eventJsonContent), 0644)

    // Run Scanner
    scanner := NewScannerService(tmpDir, db)
    scanner.ScanAll()

    var clips []models.Clip
    db.Order("timestamp asc").Find(&clips)

    if len(clips) != 2 {
        t.Errorf("expected 2 clips, got %d", len(clips))
    }

    // Clip 1 (10:00:00) -> Should have EventTimestamp (Match)
    clip1 := clips[0]
    if clip1.City != "TestCity" {
        t.Errorf("expected Clip 1 City 'TestCity', got '%s'", clip1.City)
    }
    if clip1.EventTimestamp == nil {
        t.Error("expected Clip 1 EventTimestamp to be set")
    }

    // Clip 2 (10:05:00) -> Should NOT have EventTimestamp (Mismatch)
    // 10:05:00 is not within 60s of 10:00:01.
    clip2 := clips[1]
    if clip2.EventTimestamp != nil {
         t.Error("Clip 2 should not match event.json timestamp")
    }
}

func TestScanner_EventGrouping_OffsetTimestamp(t *testing.T) {
     // Ensure UTC
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
	tmpDir, err := ioutil.TempDir("", "scanner_event_offset_test")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(tmpDir)

    // SavedClips/DirB/
    savedDir := filepath.Join(tmpDir, "SavedClips", "DirB")
    os.MkdirAll(savedDir, 0755)

    // Clip starts at 10:00:00
    ioutil.WriteFile(filepath.Join(savedDir, "2024-01-01_10-00-00-front.mp4"), []byte("data"), 0644)

    // Event happened at 10:00:59 (End of clip, but still inside)
    eventTimeStr := "2024-01-01T10:00:59"
	eventJsonContent := `{"timestamp": "` + eventTimeStr + `", "city": "TestCity"}`
	ioutil.WriteFile(filepath.Join(savedDir, "event.json"), []byte(eventJsonContent), 0644)

    // Run Scanner
    scanner := NewScannerService(tmpDir, db)
    scanner.ScanAll()

    var clip models.Clip
    if err := db.First(&clip).Error; err != nil {
        t.Fatal(err)
    }

    if clip.EventTimestamp == nil {
        t.Error("expected Clip to have EventTimestamp (59s offset match)")
    }
}
