// Command retention-dry-run builds a fixture TeslaCam tree + sqlite DB,
// runs retention in dry-run mode, and prints the structured report JSON.
//
// Usage (from backend/):
//
//	go run ./cmd/retention-dry-run
package main

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/jinzhu/gorm"
	_ "github.com/jinzhu/gorm/dialects/sqlite"
	"teslaxy/models"
	"teslaxy/services"
)

func main() {
	root, err := os.MkdirTemp("", "teslaxy-retention-fixture-*")
	if err != nil {
		fatal(err)
	}
	defer os.RemoveAll(root)

	configPath := filepath.Join(root, "config")
	footage := filepath.Join(root, "footage")
	if err := os.MkdirAll(configPath, 0755); err != nil {
		fatal(err)
	}
	if err := os.MkdirAll(footage, 0755); err != nil {
		fatal(err)
	}

	dbPath := filepath.Join(configPath, "teslacam.db")
	db, err := gorm.Open("sqlite3", dbPath)
	if err != nil {
		fatal(err)
	}
	defer db.Close()
	if err := db.AutoMigrate(&models.Clip{}, &models.VideoFile{}, &models.Telemetry{}).Error; err != nil {
		fatal(err)
	}

	now := time.Now().UTC()
	oldMtime := now.Add(-2 * time.Hour)

	mustWrite := func(path string, size int, mtime time.Time) {
		if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
			fatal(err)
		}
		data := make([]byte, size)
		if err := os.WriteFile(path, data, 0644); err != nil {
			fatal(err)
		}
		_ = os.Chtimes(path, mtime, mtime)
	}

	seed := func(event string, ts time.Time, protected bool, sourceDir string, files map[string]string) {
		clip := models.Clip{Timestamp: ts, Event: event, Protected: protected, SourceDir: sourceDir, City: "Fixture"}
		if err := db.Create(&clip).Error; err != nil {
			fatal(err)
		}
		for cam, p := range files {
			vf := models.VideoFile{ClipID: clip.ID, Camera: cam, FilePath: p, Timestamp: ts}
			if err := db.Create(&vf).Error; err != nil {
				fatal(err)
			}
		}
	}

	// Age-eligible Recent (120 days old)
	oldRecent := filepath.Join(footage, "RecentClips", "old_front.mp4")
	mustWrite(oldRecent, 8*1024*1024, oldMtime)
	seed("Recent", now.AddDate(0, 0, -120), false, filepath.Join(footage, "RecentClips"), map[string]string{"Front": oldRecent})

	// Fresh Recent (should stay)
	newRecent := filepath.Join(footage, "RecentClips", "new_front.mp4")
	mustWrite(newRecent, 4*1024*1024, oldMtime)
	seed("Recent", now.AddDate(0, 0, -5), false, filepath.Join(footage, "RecentClips"), map[string]string{"Front": newRecent})

	// Protected Saved clip (never pruned)
	savedDir := filepath.Join(footage, "SavedClips", "2024-01-01_00-00-00")
	savedPath := filepath.Join(savedDir, "front.mp4")
	mustWrite(savedPath, 2*1024*1024, oldMtime)
	_ = os.WriteFile(filepath.Join(savedDir, "event.json"), []byte(`{"city":"Fixture"}`), 0644)
	seed("Saved", now.AddDate(0, 0, -200), false, savedDir, map[string]string{"Front": savedPath})

	// Flagged Sentry (protected)
	flagDir := filepath.Join(footage, "SentryClips", "flagged")
	flagPath := filepath.Join(flagDir, "front.mp4")
	mustWrite(flagPath, 2*1024*1024, oldMtime)
	seed("Sentry", now.AddDate(0, 0, -200), true, flagDir, map[string]string{"Front": flagPath})

	// In-progress write (recent mtime) — skipped
	writing := filepath.Join(footage, "RecentClips", "writing.mp4")
	mustWrite(writing, 1024*1024, now.Add(-time.Minute))
	seed("Recent", now.AddDate(0, 0, -150), false, filepath.Join(footage, "RecentClips"), map[string]string{"Front": writing})

	cfg := services.RetentionConfig{
		Enabled:           true,
		DryRun:            true,
		MaxAgeDays:        90,
		MaxSizeGB:         300,
		IntervalHours:     24,
		MinFileAgeMinutes: 15,
		FootagePath:       footage,
	}
	if err := services.SaveRetentionConfig(configPath, cfg); err != nil {
		fatal(err)
	}

	svc := services.NewRetentionService(db, footage, configPath)
	report, err := svc.RunWithConfig(cfg)
	if err != nil {
		fatal(err)
	}

	enc := json.NewEncoder(os.Stdout)
	enc.SetIndent("", "  ")
	if err := enc.Encode(report); err != nil {
		fatal(err)
	}

	for _, p := range []string{oldRecent, newRecent, savedPath, flagPath, writing} {
		if _, err := os.Stat(p); err != nil {
			fatal(fmt.Errorf("dry-run deleted %s: %w", p, err))
		}
	}
	fmt.Fprintf(os.Stderr, "retention-dry-run: fixture=%s candidates=%d bytes_freed=%d deleted_on_disk=0\n",
		footage, len(report.Candidates), report.BytesFreed)
}

func fatal(err error) {
	fmt.Fprintf(os.Stderr, "retention-dry-run: %v\n", err)
	os.Exit(1)
}
