package services

import (
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"testing"
	"time"

	"github.com/jinzhu/gorm"
	_ "github.com/jinzhu/gorm/dialects/sqlite"
	"teslaxy/models"
	pb "teslaxy/proto"
)

func telemetryIDFixture(t *testing.T) (*gorm.DB, *ScannerService) {
	t.Helper()
	db, err := gorm.Open("sqlite3", ":memory:")
	if err != nil {
		t.Fatal(err)
	}
	db.DB().SetMaxOpenConns(1)
	t.Cleanup(func() { db.Close() })
	mustTelemetryID(t, db.AutoMigrate(&models.Clip{}, &models.VideoFile{}, &models.Telemetry{}).Error)
	s := NewScannerService(t.TempDir(), db)
	s.Geocoder = nil
	s.SEIExtractor = func(string) ([]*pb.SeiMetadata, error) {
		return []*pb.SeiMetadata{{VehicleSpeedMps: 12, LatitudeDeg: -34.9, LongitudeDeg: 138.6}}, nil
	}
	return db, s
}

func mustTelemetryID(t *testing.T, err error) {
	t.Helper()
	if err != nil {
		t.Fatal(err)
	}
}

func seedTelemetryID(t *testing.T, db *gorm.DB, n int) []models.Clip {
	t.Helper()
	clips := make([]models.Clip, n)
	for i := range clips {
		c := &clips[i]
		c.City = fmt.Sprintf("unchanged-%d", i)
		mustTelemetryID(t, db.Create(c).Error)
		row := models.Telemetry{ClipID: c.ID, Speed: float32(i + 1), FullDataJson: "original"}
		mustTelemetryID(t, db.Create(&row).Error)
		mustTelemetryID(t, db.Model(c).UpdateColumn("telemetry_id", row.ID).Error)
	}
	return clips
}

func assertTelemetryIDUnchanged(t *testing.T, db *gorm.DB, clips []models.Clip, rows []models.Telemetry) {
	t.Helper()
	for i, before := range clips {
		var after models.Clip
		mustTelemetryID(t, db.First(&after, before.ID).Error)
		if after.TelemetryID != before.TelemetryID || after.City != before.City || after.SourceDir != before.SourceDir {
			t.Errorf("clip %d changed: telemetry_id %d -> %d, city %q -> %q, source_dir %q -> %q", before.ID, before.TelemetryID, after.TelemetryID, before.City, after.City, before.SourceDir, after.SourceDir)
		}
		var row models.Telemetry
		mustTelemetryID(t, db.First(&row, before.TelemetryID).Error)
		if !reflect.DeepEqual(row, rows[i]) {
			t.Errorf("clip %d telemetry row changed: %+v", before.ID, row)
		}
	}
}

func snapshotTelemetryID(t *testing.T, db *gorm.DB, clips []models.Clip) []models.Telemetry {
	t.Helper()
	rows := make([]models.Telemetry, len(clips))
	for i, c := range clips {
		mustTelemetryID(t, db.First(&rows[i], c.TelemetryID).Error)
	}
	return rows
}

func TestTelemetryIDRecentDeletedLookback(t *testing.T) {
	db, s := telemetryIDFixture(t)
	clips := seedTelemetryID(t, db, 4)
	rows := snapshotTelemetryID(t, db, clips)
	minTime := time.Date(2026, 11, 1, 12, 1, 0, 0, determineTimezone(0, 0))
	deleted := models.Clip{Event: "Recent", Timestamp: minTime.Add(-time.Minute)}
	mustTelemetryID(t, db.Create(&deleted).Error)
	mustTelemetryID(t, db.Create(&models.VideoFile{ClipID: deleted.ID, Timestamp: deleted.Timestamp, FilePath: "old.mp4"}).Error)
	mustTelemetryID(t, db.Delete(&deleted).Error)
	path := filepath.Join(s.FootagePath, "2026-11-01_12-01-00-front.mp4")
	mustTelemetryID(t, os.WriteFile(path, []byte("fixture"), 0600))
	s.processRecentGroup([]string{path})
	var shared int
	mustTelemetryID(t, db.Model(&models.Clip{}).Where("telemetry_id = (SELECT MAX(id) FROM telemetries)").Count(&shared).Error)
	if shared > 1 {
		t.Errorf("global update reproduced: %d active clips now share the newest telemetry_id", shared)
	}
	assertTelemetryIDUnchanged(t, db, clips, rows)
	var vf models.VideoFile
	mustTelemetryID(t, db.Where("file_path = ?", path).First(&vf).Error)
	if vf.ClipID == 0 || vf.ClipID == deleted.ID {
		t.Errorf("new footage has invalid owner %d", vf.ClipID)
	}
	var c models.Clip
	mustTelemetryID(t, db.First(&c, vf.ClipID).Error)
	if c.TelemetryID == 0 {
		t.Error("new clip has no telemetry")
	}
}

func TestTelemetryIDEventDatabaseErrors(t *testing.T) {
	for _, stage := range []string{"query", "create"} {
		t.Run(stage, func(t *testing.T) {
			db, s := telemetryIDFixture(t)
			clips := seedTelemetryID(t, db, 3)
			rows := snapshotTelemetryID(t, db, clips)
			dir := filepath.Join(s.FootagePath, "SentryClips", "event")
			mustTelemetryID(t, os.MkdirAll(dir, 0700))
			path := filepath.Join(dir, "2026-11-01_12-00-00-front.mp4")
			mustTelemetryID(t, os.WriteFile(path, []byte("fixture"), 0600))
			mustTelemetryID(t, os.WriteFile(filepath.Join(dir, "event.json"), []byte(`{"est_lat":-34.9,"est_lon":138.6}`), 0600))
			fail := func(scope *gorm.Scope) {
				if scope.TableName() == "clips" {
					scope.Err(fmt.Errorf("injected database is locked"))
				}
			}
			if stage == "query" {
				db.Callback().Query().Before("gorm:query").Register("test:clip_error", fail)
			} else {
				db.Callback().Create().Before("gorm:create").Register("test:clip_error", fail)
			}
			s.processEventGroup(dir, []string{path})
			if stage == "query" {
				db.Callback().Query().Remove("test:clip_error")
			} else {
				db.Callback().Create().Remove("test:clip_error")
			}
			assertTelemetryIDUnchanged(t, db, clips, rows)
			var n int
			mustTelemetryID(t, db.Model(&models.VideoFile{}).Where("clip_id = 0").Count(&n).Error)
			if n != 0 {
				t.Errorf("created %d ownerless video files", n)
			}
		})
	}
}

func TestSharedTelemetryOwnership(t *testing.T) {
	db, s := telemetryIDFixture(t)
	clips := seedTelemetryID(t, db, 3)
	rows := snapshotTelemetryID(t, db, clips)
	victim := models.Clip{TelemetryID: clips[0].TelemetryID}
	mustTelemetryID(t, db.Create(&victim).Error)
	s.aggregateTelemetry(&victim, []fileInfo{{path: "2026-11-01_12-00-00-front.mp4"}}, 0, 0)
	assertTelemetryIDUnchanged(t, db, clips, rows)
	if victim.TelemetryID == 0 || victim.TelemetryID == clips[0].TelemetryID {
		t.Errorf("clip still points to foreign telemetry %d", victim.TelemetryID)
	}
	var row models.Telemetry
	mustTelemetryID(t, db.First(&row, victim.TelemetryID).Error)
	if row.ClipID != victim.ID {
		t.Errorf("telemetry owner %d, want %d", row.ClipID, victim.ID)
	}
}
