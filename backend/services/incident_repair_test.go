package services

import (
	"encoding/json"
	"io/ioutil"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/jinzhu/gorm"
	_ "github.com/jinzhu/gorm/dialects/sqlite"
	"teslaxy/models"
)

func TestCountIncidentCoordinateIssues(t *testing.T) {
	db, err := gorm.Open("sqlite3", ":memory:")
	if err != nil {
		t.Fatalf("failed to connect database: %v", err)
	}
	defer db.Close()
	db.AutoMigrate(&models.Clip{}, &models.VideoFile{}, &models.Telemetry{})

	tmpDir, err := ioutil.TempDir("", "incident_repair")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(tmpDir)

	eventDir := filepath.Join(tmpDir, "SavedClips", "2024-03-01_12-00-00")
	if err := os.MkdirAll(eventDir, 0755); err != nil {
		t.Fatal(err)
	}
	eventBytes, _ := json.Marshal(map[string]interface{}{
		"timestamp": "2024-03-01T12:00:00",
		"est_lat":   testEventLat,
		"est_lon":   testEventLon,
		"reason":    "sentry_aware_object_detection",
	})
	if err := ioutil.WriteFile(filepath.Join(eventDir, "event.json"), eventBytes, 0644); err != nil {
		t.Fatal(err)
	}

	wouldChange := models.Clip{
		Timestamp: time.Date(2024, 3, 1, 12, 0, 0, 0, time.UTC),
		Event:     "Saved",
		SourceDir: eventDir,
	}
	if err := db.Create(&wouldChange).Error; err != nil {
		t.Fatal(err)
	}
	wrongTel := models.Telemetry{
		ClipID:    wouldChange.ID,
		Latitude:  testSEILat,
		Longitude: testSEILon,
	}
	if err := db.Create(&wrongTel).Error; err != nil {
		t.Fatal(err)
	}
	db.Model(&wouldChange).Update("telemetry_id", wrongTel.ID)
	wouldChange.TelemetryID = wrongTel.ID

	missingEventDir := filepath.Join(tmpDir, "SavedClips", "missing-event")
	if err := os.MkdirAll(missingEventDir, 0755); err != nil {
		t.Fatal(err)
	}
	unrepairable := models.Clip{
		Timestamp: time.Date(2024, 3, 1, 13, 0, 0, 0, time.UTC),
		Event:     "Saved",
		SourceDir: missingEventDir,
	}
	if err := db.Create(&unrepairable).Error; err != nil {
		t.Fatal(err)
	}

	var beforeTel models.Telemetry
	if err := db.First(&beforeTel, wrongTel.ID).Error; err != nil {
		t.Fatal(err)
	}
	var clipCountBefore, telCountBefore int
	db.Model(&models.Clip{}).Count(&clipCountBefore)
	db.Model(&models.Telemetry{}).Count(&telCountBefore)

	nEventBacked, nWouldChange, nUnrepairable, err := CountIncidentCoordinateIssues(db, tmpDir)
	if err != nil {
		t.Fatalf("CountIncidentCoordinateIssues: %v", err)
	}
	if nEventBacked != 1 {
		t.Errorf("nEventBacked = %d, want 1", nEventBacked)
	}
	if nWouldChange != 1 {
		t.Errorf("nWouldChange = %d, want 1 (stored SEI differs from event point)", nWouldChange)
	}
	if nUnrepairable != 1 {
		t.Errorf("nUnrepairable = %d, want 1 (event.json missing)", nUnrepairable)
	}

	var afterTel models.Telemetry
	if err := db.First(&afterTel, wrongTel.ID).Error; err != nil {
		t.Fatal(err)
	}
	if afterTel.ID != beforeTel.ID || afterTel.Latitude != beforeTel.Latitude || afterTel.Longitude != beforeTel.Longitude {
		t.Errorf("dry-run mutated telemetry coordinates or ID")
	}
	if !afterTel.UpdatedAt.Equal(beforeTel.UpdatedAt) {
		t.Errorf("dry-run mutated telemetry UpdatedAt")
	}
	var clipCountAfter, telCountAfter int
	db.Model(&models.Clip{}).Count(&clipCountAfter)
	db.Model(&models.Telemetry{}).Count(&telCountAfter)
	if clipCountAfter != clipCountBefore || telCountAfter != telCountBefore {
		t.Errorf("dry-run created or deleted rows")
	}

	t.Run("empty footageRoot is unrepairable", func(t *testing.T) {
		_, _, nUnrep, err := CountIncidentCoordinateIssues(db, "")
		if err != nil {
			t.Fatal(err)
		}
		if nUnrep != 2 {
			t.Errorf("empty footageRoot nUnrepairable = %d, want 2", nUnrep)
		}
	})

	t.Run("absolute SourceDir outside root", func(t *testing.T) {
		outside, err := ioutil.TempDir("", "incident_abs_outside")
		if err != nil {
			t.Fatal(err)
		}
		defer os.RemoveAll(outside)
		outsideBytes, _ := json.Marshal(map[string]interface{}{
			"est_lat": testEventLat,
			"est_lon": testEventLon,
		})
		if err := ioutil.WriteFile(filepath.Join(outside, "event.json"), outsideBytes, 0644); err != nil {
			t.Fatal(err)
		}
		extra := models.Clip{
			Timestamp: time.Date(2024, 3, 1, 14, 0, 0, 0, time.UTC),
			Event:     "Saved",
			SourceDir: outside,
		}
		if err := db.Create(&extra).Error; err != nil {
			t.Fatal(err)
		}
		defer db.Unscoped().Delete(&extra)

		nEB, _, nUR, err := CountIncidentCoordinateIssues(db, tmpDir)
		if err != nil {
			t.Fatal(err)
		}
		if nEB != 1 {
			t.Errorf("absolute-outside must not be event_backed; nEventBacked = %d, want 1", nEB)
		}
		if nUR != 2 {
			t.Errorf("absolute-outside nUnrepairable = %d, want 2 (missing + escaped)", nUR)
		}
		if _, statErr := os.Stat(filepath.Join(outside, "event.json")); statErr != nil {
			t.Errorf("fixture event.json should still exist: %v", statErr)
		}
	})

	t.Run("dotdot escape", func(t *testing.T) {
		sibling, err := ioutil.TempDir("", "incident_dotdot")
		if err != nil {
			t.Fatal(err)
		}
		defer os.RemoveAll(sibling)
		sibBytes, _ := json.Marshal(map[string]interface{}{
			"est_lat": testEventLat,
			"est_lon": testEventLon,
		})
		if err := ioutil.WriteFile(filepath.Join(sibling, "event.json"), sibBytes, 0644); err != nil {
			t.Fatal(err)
		}
		rel := filepath.Join("..", filepath.Base(sibling))
		extra := models.Clip{
			Timestamp: time.Date(2024, 3, 1, 15, 0, 0, 0, time.UTC),
			Event:     "Saved",
			SourceDir: rel,
		}
		if err := db.Create(&extra).Error; err != nil {
			t.Fatal(err)
		}
		defer db.Unscoped().Delete(&extra)

		nEB, _, nUR, err := CountIncidentCoordinateIssues(db, tmpDir)
		if err != nil {
			t.Fatal(err)
		}
		if nEB != 1 {
			t.Errorf("../ escape must not be event_backed; nEventBacked = %d, want 1", nEB)
		}
		if nUR != 2 {
			t.Errorf("../ escape nUnrepairable = %d, want 2, source_dir=%q", nUR, rel)
		}
	})

	t.Run("symlink escape", func(t *testing.T) {
		outside, err := ioutil.TempDir("", "incident_symlink_outside")
		if err != nil {
			t.Fatal(err)
		}
		defer os.RemoveAll(outside)
		outBytes, _ := json.Marshal(map[string]interface{}{
			"est_lat": testEventLat,
			"est_lon": testEventLon,
		})
		if err := ioutil.WriteFile(filepath.Join(outside, "event.json"), outBytes, 0644); err != nil {
			t.Fatal(err)
		}
		linkDir := filepath.Join(tmpDir, "symlink-escape")
		if err := os.Symlink(outside, linkDir); err != nil {
			t.Fatal(err)
		}
		defer os.Remove(linkDir)

		extra := models.Clip{
			Timestamp: time.Date(2024, 3, 1, 16, 0, 0, 0, time.UTC),
			Event:     "Saved",
			SourceDir: linkDir,
		}
		if err := db.Create(&extra).Error; err != nil {
			t.Fatal(err)
		}
		defer db.Unscoped().Delete(&extra)

		nEB, _, nUR, err := CountIncidentCoordinateIssues(db, tmpDir)
		if err != nil {
			t.Fatal(err)
		}
		if nEB != 1 {
			t.Errorf("symlink escape must not follow ReadFile; nEventBacked = %d, want 1", nEB)
		}
		if nUR != 2 {
			t.Errorf("symlink escape nUnrepairable = %d, want 2", nUR)
		}
	})

	t.Run("malformed event.json is unrepairable", func(t *testing.T) {
		malDir := filepath.Join(tmpDir, "malformed-event")
		if err := os.MkdirAll(malDir, 0755); err != nil {
			t.Fatal(err)
		}
		if err := ioutil.WriteFile(filepath.Join(malDir, "event.json"), []byte("{not json"), 0644); err != nil {
			t.Fatal(err)
		}
		extra := models.Clip{
			Timestamp: time.Date(2024, 3, 1, 17, 0, 0, 0, time.UTC),
			Event:     "Saved",
			SourceDir: malDir,
		}
		if err := db.Create(&extra).Error; err != nil {
			t.Fatal(err)
		}
		defer db.Unscoped().Delete(&extra)

		nEB, nWC, nUR, err := CountIncidentCoordinateIssues(db, tmpDir)
		if err != nil {
			t.Fatal(err)
		}
		if nEB != 1 {
			t.Errorf("malformed JSON must not be event_backed; nEventBacked = %d, want 1", nEB)
		}
		if nWC != 1 {
			t.Errorf("malformed JSON must not be a geographic point; nWouldChange = %d, want 1", nWC)
		}
		if nUR != 2 {
			t.Errorf("malformed JSON nUnrepairable = %d, want 2 (missing + invalid)", nUR)
		}
	})
}
