package services

import (
	"fmt"
	"os"
	"path/filepath"
	"testing"

	"github.com/jinzhu/gorm"
	"teslaxy/database"
	"teslaxy/models"
	pb "teslaxy/proto"
)

func TestTelemetryRepairStartupAndScanAll(t *testing.T) {
	config := t.TempDir()
	t.Setenv("CONFIG_PATH", config)
	db, err := gorm.Open("sqlite3", filepath.Join(config, "teslacam.db"))
	mustTelemetryID(t, err)
	t.Cleanup(func() { db.Close() })
	db.DB().SetMaxOpenConns(1)
	mustTelemetryID(t, db.AutoMigrate(&models.Clip{}, &models.VideoFile{}, &models.Telemetry{}).Error)
	root := t.TempDir()
	s := NewScannerService(root, db)
	s.Geocoder = nil
	s.SEIExtractor = func(string) ([]*pb.SeiMetadata, error) { return []*pb.SeiMetadata{{VehicleSpeedMps: 12}}, nil }
	correct := seedTelemetryID(t, db, 3)
	rows := snapshotTelemetryID(t, db, correct)
	var sharedID, ownerID uint
	for i := 0; i < 50; i++ {
		dir := filepath.Join(root, "SentryClips", fmt.Sprintf("event-%d", i))
		mustTelemetryID(t, os.MkdirAll(dir, 0700))
		metadata := `{"city":"Fixture","est_lat":-34.9,"est_lon":138.6}`
		if i%4 == 0 {
			metadata = `{"city":"Fixture"}` // No front camera or coordinates to seed from.
		}
		mustTelemetryID(t, os.WriteFile(filepath.Join(dir, "event.json"), []byte(metadata), 0600))
		camera := "front"
		if i%2 == 0 {
			camera = "back"
		}
		path := filepath.Join(dir, "2026-11-01_12-00-00-"+camera+".mp4")
		mustTelemetryID(t, os.WriteFile(path, []byte("fixture"), 0600))
		c := models.Clip{SourceDir: dir, Event: "Sentry"}
		mustTelemetryID(t, db.Create(&c).Error)
		if i == 0 {
			ownerID = c.ID
			row := models.Telemetry{ClipID: c.ID, Latitude: -34.9, Longitude: 138.6}
			mustTelemetryID(t, db.Create(&row).Error)
			sharedID = row.ID
		}
		mustTelemetryID(t, db.Model(&c).UpdateColumn("telemetry_id", sharedID).Error)
		mustTelemetryID(t, db.Create(&models.VideoFile{ClipID: c.ID, FilePath: path, Camera: normalizeCameraName(camera)}).Error)
	}
	// Exercise the real startup hook, not a test-only repair path.
	database.InitDB()
	t.Cleanup(database.CloseDB)
	database.DB.DB().SetMaxOpenConns(1)
	s.DB = database.DB
	var cleared int
	mustTelemetryID(t, s.DB.Model(&models.Clip{}).Where("telemetry_id = 0").Count(&cleared).Error)
	if cleared != 49 {
		t.Fatalf("startup cleared %d pointers, want 49", cleared)
	}
	var owner models.Clip
	mustTelemetryID(t, s.DB.First(&owner, ownerID).Error)
	if owner.TelemetryID != sharedID {
		t.Fatal("repair changed rightful owner")
	}
	n, err := database.RepairSharedTelemetry(s.DB)
	mustTelemetryID(t, err)
	if n != 0 {
		t.Fatalf("second repair changed %d rows", n)
	}
	s.ScanAll()
	var clips []models.Clip
	mustTelemetryID(t, s.DB.Find(&clips).Error)
	if len(clips) != 53 {
		t.Fatalf("repair/rescan changed clip count: %d", len(clips))
	}
	seen := map[uint]bool{}
	for _, c := range clips {
		if c.TelemetryID == 0 || seen[c.TelemetryID] {
			t.Errorf("invalid or shared pointer: clip %d telemetry %d", c.ID, c.TelemetryID)
		}
		seen[c.TelemetryID] = true
		var row models.Telemetry
		mustTelemetryID(t, s.DB.First(&row, c.TelemetryID).Error)
		if row.ClipID != c.ID {
			t.Errorf("clip %d row owner %d", c.ID, row.ClipID)
		}
	}
	assertTelemetryIDUnchanged(t, s.DB, correct, rows)
	n, err = database.RepairSharedTelemetry(s.DB)
	mustTelemetryID(t, err)
	if n != 0 {
		t.Fatalf("post-scan repair changed %d rows", n)
	}
	var count int
	mustTelemetryID(t, s.DB.Model(&models.VideoFile{}).Count(&count).Error)
	if count != 50 {
		t.Fatalf("repair/rescan changed video count: %d", count)
	}
}
