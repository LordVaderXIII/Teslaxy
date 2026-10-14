package services

import (
	"fmt"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/jinzhu/gorm"
	"teslaxy/models"
)

// Codex round-5 P2: a single DELETE ... IN (?) of ≥32,767 ids fails with
// "too many SQL variables". Batched deletes must clear a 40k backlog and leave
// Saved / protected clip telemetry untouched.
func TestRetentionOrphanSweepLargeBacklogBatched(t *testing.T) {
	db := setupRetentionTestDB(t)
	root := t.TempDir()
	cfgPath := t.TempDir()
	now := time.Date(2026, 10, 14, 12, 0, 0, 0, time.UTC)

	savedPath := filepath.Join(root, "SavedClips", "keep.mp4")
	writeFixtureFile(t, savedPath, 128, now.Add(-time.Hour))
	saved := seedClip(t, db, "Saved", now.AddDate(0, 0, -10), false, filepath.Join(root, "SavedClips"), map[string]string{"Front": savedPath})

	protPath := filepath.Join(root, "SentryClips", "prot.mp4")
	writeFixtureFile(t, protPath, 128, now.Add(-time.Hour))
	protected := seedClip(t, db, "Sentry", now.AddDate(0, 0, -10), true, filepath.Join(root, "SentryClips"), map[string]string{"Front": protPath})

	const orphans = 40000
	insertOrphanTelemetry(t, db, orphans, 100000)

	svc := newTestRetention(t, db, root, cfgPath, RetentionConfig{
		Enabled: true, DryRun: false, MaxAgeDays: 0, MaxSizeGB: 0, MinFileAgeMinutes: 0,
	})
	report, err := svc.Run()
	if err != nil {
		t.Fatalf("run: %v", err)
	}
	if len(report.Errors) != 0 {
		t.Fatalf("errors: %v", report.Errors)
	}

	var n int
	db.Unscoped().Model(&models.Telemetry{}).
		Where("NOT EXISTS (SELECT 1 FROM clips WHERE clips.id = telemetries.clip_id)").
		Count(&n)
	if n != 0 {
		t.Fatalf("orphan telemetry remaining=%d want 0", n)
	}
	db.Unscoped().Model(&models.Telemetry{}).Where("clip_id = ?", saved.ID).Count(&n)
	if n != 1 {
		t.Fatalf("Saved clip telemetry count=%d want 1", n)
	}
	db.Unscoped().Model(&models.Telemetry{}).Where("clip_id = ?", protected.ID).Count(&n)
	if n != 1 {
		t.Fatalf("protected clip telemetry count=%d want 1", n)
	}
	db.Unscoped().Model(&models.Telemetry{}).Count(&n)
	if n != 2 {
		t.Fatalf("total telemetry=%d want 2 (saved+protected only)", n)
	}
}

// A failed mid-sweep batch must leave earlier batches deleted and let the next
// run finish the remainder.
func TestRetentionOrphanSweepPartialBatchFailureResumable(t *testing.T) {
	db := setupRetentionTestDB(t)
	root := t.TempDir()
	cfgPath := t.TempDir()

	const orphans = orphanTelemetryDeleteBatchSize*2 + 50 // three batches
	insertOrphanTelemetry(t, db, orphans, 200000)

	svc := newTestRetention(t, db, root, cfgPath, RetentionConfig{
		Enabled: true, DryRun: false, MaxAgeDays: 0, MaxSizeGB: 0, MinFileAgeMinutes: 0,
	})
	batches := 0
	svc.beforeOrphanTelemetryBatch = func(ids []uint) error {
		batches++
		if batches == 2 {
			return fmt.Errorf("injected orphan batch failure")
		}
		return nil
	}

	report, err := svc.Run()
	if err != nil {
		t.Fatalf("run: %v", err)
	}
	foundErr := false
	for _, e := range report.Errors {
		if strings.Contains(e, "orphan telemetry") && strings.Contains(e, "injected") {
			foundErr = true
			break
		}
	}
	if !foundErr {
		t.Fatalf("expected injected orphan error, got %+v", report.Errors)
	}
	var remaining int
	db.Unscoped().Model(&models.Telemetry{}).Count(&remaining)
	if remaining == 0 || remaining >= orphans {
		t.Fatalf("after partial failure remaining=%d want (0, %d)", remaining, orphans)
	}
	if remaining != orphans-orphanTelemetryDeleteBatchSize {
		t.Fatalf("remaining=%d want %d (one successful batch)", remaining, orphans-orphanTelemetryDeleteBatchSize)
	}

	svc.beforeOrphanTelemetryBatch = nil
	report2, err := svc.Run()
	if err != nil {
		t.Fatalf("restart: %v", err)
	}
	if len(report2.Errors) != 0 {
		t.Fatalf("restart errors: %v", report2.Errors)
	}
	db.Unscoped().Model(&models.Telemetry{}).Count(&remaining)
	if remaining != 0 {
		t.Fatalf("after resume remaining=%d want 0", remaining)
	}
}

func insertOrphanTelemetry(t *testing.T, db *gorm.DB, count int, clipIDBase uint) {
	t.Helper()
	// Bulk insert avoids 40k individual GORM Creates (slow under -race).
	const chunk = 500
	for start := 0; start < count; start += chunk {
		end := start + chunk
		if end > count {
			end = count
		}
		var b strings.Builder
		b.WriteString("INSERT INTO telemetries (clip_id, speed, created_at, updated_at) VALUES ")
		args := make([]interface{}, 0, (end-start)*4)
		now := time.Now().UTC().Format("2006-01-02 15:04:05")
		for i := start; i < end; i++ {
			if i > start {
				b.WriteByte(',')
			}
			b.WriteString("(?,?,?,?)")
			args = append(args, clipIDBase+uint(i), float32(1), now, now)
		}
		if err := db.Exec(b.String(), args...).Error; err != nil {
			t.Fatalf("bulk orphan insert [%d:%d]: %v", start, end, err)
		}
	}
}
