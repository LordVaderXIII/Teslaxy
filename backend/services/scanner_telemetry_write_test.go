package services

import (
	"fmt"
	"sync"
	"testing"

	"github.com/jinzhu/gorm"
	"teslaxy/models"
)

func TestTelemetryIDRejectsZeroAndMissingClips(t *testing.T) {
	db, s := telemetryIDFixture(t)
	clips := seedTelemetryID(t, db, 3)
	rows := snapshotTelemetryID(t, db, clips)
	for _, clip := range []*models.Clip{nil, {}, {ID: 99999}} {
		if err := s.writeClipTelemetry(clip, func(*models.Telemetry) { t.Error("invalid clip reached update") }); err == nil {
			t.Error("invalid clip accepted")
		}
	}
	s.aggregateTelemetry(nil, nil, 0, 0)
	s.aggregateTelemetry(&models.Clip{}, nil, 0, 0)
	assertTelemetryIDUnchanged(t, db, clips, rows)
}

func TestTelemetryIDPointerFailureRollsBack(t *testing.T) {
	db, s := telemetryIDFixture(t)
	clips := seedTelemetryID(t, db, 3)
	rows := snapshotTelemetryID(t, db, clips)
	victim := models.Clip{TelemetryID: clips[0].TelemetryID}
	mustTelemetryID(t, db.Create(&victim).Error)
	db.Callback().Update().Before("gorm:update").Register("test:pointer_error", func(scope *gorm.Scope) {
		if scope.TableName() == "clips" {
			scope.Err(fmt.Errorf("injected pointer update failure"))
		}
	})
	err := s.writeClipTelemetry(&victim, func(row *models.Telemetry) { row.Speed = 99 })
	db.Callback().Update().Remove("test:pointer_error")
	if err == nil {
		t.Fatal("expected pointer update error")
	}
	var count int
	mustTelemetryID(t, db.Model(&models.Telemetry{}).Count(&count).Error)
	if count != len(clips) || victim.TelemetryID != clips[0].TelemetryID {
		t.Fatal("failed transaction left a new row or changed caller pointer")
	}
	assertTelemetryIDUnchanged(t, db, clips, rows)
}

func TestSharedTelemetryWithoutFront(t *testing.T) {
	db, s := telemetryIDFixture(t)
	clips := seedTelemetryID(t, db, 3)
	rows := snapshotTelemetryID(t, db, clips)
	for _, pointer := range []uint{0, clips[0].TelemetryID} {
		clip := models.Clip{TelemetryID: pointer}
		mustTelemetryID(t, db.Create(&clip).Error)
		s.aggregateTelemetry(&clip, []fileInfo{{path: "2026-11-01_12-00-00-back.mp4"}}, 0, 0)
		if clip.TelemetryID == 0 || clip.TelemetryID == clips[0].TelemetryID {
			t.Errorf("clip without front/coordinates has no owned row: %d", clip.TelemetryID)
			continue
		}
		var row models.Telemetry
		mustTelemetryID(t, db.First(&row, clip.TelemetryID).Error)
		if row.ClipID != clip.ID || row.FullDataJson != "" {
			t.Error("missing-front row has wrong owner or invented playback data")
		}
	}
	assertTelemetryIDUnchanged(t, db, clips, rows)
}

func TestSharedTelemetryConcurrentStalePointers(t *testing.T) {
	db, s := telemetryIDFixture(t)
	clip := models.Clip{}
	mustTelemetryID(t, db.Create(&clip).Error)
	var wg sync.WaitGroup
	errs := make(chan error, 8)
	for i := 0; i < cap(errs); i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			stale := models.Clip{ID: clip.ID}
			errs <- s.writeClipTelemetry(&stale, func(row *models.Telemetry) { row.Speed++ })
		}()
	}
	wg.Wait()
	close(errs)
	for err := range errs {
		mustTelemetryID(t, err)
	}
	var rows []models.Telemetry
	mustTelemetryID(t, db.Find(&rows).Error)
	if len(rows) != 1 || rows[0].ClipID != clip.ID || rows[0].Speed != 8 {
		t.Fatalf("stale callers did not reuse one owned row: %+v", rows)
	}
}
