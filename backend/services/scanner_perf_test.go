package services

import (
	"fmt"
	"testing"
	"time"

	"github.com/jinzhu/gorm"
	_ "github.com/jinzhu/gorm/dialects/sqlite"
	"teslaxy/models"
)

func TestScanner_AddFilesToClip_Performance(t *testing.T) {
	// Setup DB
	db, err := gorm.Open("sqlite3", ":memory:")
	if err != nil {
		t.Fatalf("failed to connect database: %v", err)
	}
	defer db.Close()
	db.AutoMigrate(&models.Clip{}, &models.VideoFile{})

	scanner := NewScannerService("/tmp", db)

	// Create a Clip
	clip := models.Clip{
		Timestamp: time.Now(),
		Event:     "Sentry",
	}
	db.Create(&clip)

	// Generate 100 fake files
	var files []fileInfo
	baseTime := time.Now()
	for i := 0; i < 100; i++ {
		files = append(files, fileInfo{
			path:      fmt.Sprintf("/tmp/2023-10-27_10-00-%02d-front.mp4", i),
			timestamp: baseTime.Add(time.Duration(i) * time.Second),
		})
	}

	// Measure Time (flaky but indicative for large N)
	start := time.Now()
	scanner.addFilesToClip(clip, files)
	duration := time.Since(start)

	t.Logf("Processed %d files in %v", len(files), duration)

	// Verify all files added
	var count int
	db.Model(&models.VideoFile{}).Where("clip_id = ?", clip.ID).Count(&count)
	if count != 100 {
		t.Errorf("Expected 100 files, got %d", count)
	}
}
