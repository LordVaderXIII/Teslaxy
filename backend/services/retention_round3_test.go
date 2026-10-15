package services

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"teslaxy/models"
)

// Codex P2: size-cap selection must not stop after the first hardlinked clip.
// Shared inode bytes only count toward the estimate once every known link is selected.
func TestRetentionSizeCapContinuesAcrossHardlinks(t *testing.T) {
	db := setupRetentionTestDB(t)
	root := t.TempDir()
	cfgPath := t.TempDir()
	now := time.Date(2026, 10, 14, 12, 0, 0, 0, time.UTC)

	sharedA := filepath.Join(root, "RecentClips", "shared-a.mp4")
	sharedB := filepath.Join(root, "RecentClips", "shared-b.mp4")
	solo := filepath.Join(root, "RecentClips", "solo.mp4")
	writeFixtureFile(t, sharedA, 1024, now.Add(-time.Hour))
	if err := os.Link(sharedA, sharedB); err != nil {
		t.Fatalf("hardlink: %v", err)
	}
	writeFixtureFile(t, solo, 1024, now.Add(-time.Hour))

	// Oldest first: A and B share one inode; C is unique. Unique total = 2048.
	clipA := seedClip(t, db, "Recent", now.AddDate(0, 0, -40), false, filepath.Join(root, "RecentClips"), map[string]string{"Front": sharedA})
	clipB := seedClip(t, db, "Recent", now.AddDate(0, 0, -30), false, filepath.Join(root, "RecentClips"), map[string]string{"Front": sharedB})
	clipC := seedClip(t, db, "Recent", now.AddDate(0, 0, -20), false, filepath.Join(root, "RecentClips"), map[string]string{"Front": solo})

	// Cap at 1024 unique bytes: must free the shared inode (select A+B) and stop.
	// A naive per-clip estimate would select only A (claiming 1024 freed) and leave
	// 2048 on disk with 0 actually freed.
	svc := newTestRetention(t, db, root, cfgPath, RetentionConfig{
		Enabled: true, DryRun: true, MaxAgeDays: 0,
		MaxSizeGB:         1024.0 / (1024 * 1024 * 1024),
		MinFileAgeMinutes: 0,
	})
	report, err := svc.Run()
	if err != nil {
		t.Fatalf("run: %v", err)
	}
	if report.TotalBytesBefore != 2048 {
		t.Fatalf("total_bytes_before=%d want 2048", report.TotalBytesBefore)
	}
	if report.BytesFreed != 1024 {
		t.Fatalf("bytes_freed=%d want 1024 (shared inode after A+B)", report.BytesFreed)
	}
	got := map[uint]RetentionCandidate{}
	for _, c := range report.Candidates {
		got[c.ClipID] = c
	}
	if _, ok := got[clipA.ID]; !ok {
		t.Fatalf("expected clip A selected, candidates=%+v", report.Candidates)
	}
	if _, ok := got[clipB.ID]; !ok {
		t.Fatalf("expected clip B selected (second hardlink), candidates=%+v", report.Candidates)
	}
	if _, ok := got[clipC.ID]; ok {
		t.Fatalf("clip C must remain under the 1024-byte cap, candidates=%+v", report.Candidates)
	}
	if got[clipA.ID].Bytes != 0 {
		t.Fatalf("clip A incremental bytes=%d want 0 (sibling link remains)", got[clipA.ID].Bytes)
	}
	if got[clipB.ID].Bytes != 1024 {
		t.Fatalf("clip B incremental bytes=%d want 1024", got[clipB.ID].Bytes)
	}
}

// Codex P2: telemetry hard-delete failure must leave the clip soft-deleted, surface
// the error, and let the next run finish with telemetry=0.
func TestRetentionTelemetryDeleteFailureRecoversOnRestart(t *testing.T) {
	db := setupRetentionTestDB(t)
	root := t.TempDir()
	cfgPath := t.TempDir()
	now := time.Date(2026, 10, 14, 12, 0, 0, 0, time.UTC)
	old := now.AddDate(0, 0, -200)
	path := filepath.Join(root, "RecentClips", "gone.mp4")
	writeFixtureFile(t, path, 1024, now.Add(-time.Hour))
	clip := seedClip(t, db, "Recent", old, false, filepath.Join(root, "RecentClips"), map[string]string{"Front": path})

	// Simulate crash after last VF hard-delete: retention-marked clip+telemetry soft-deleted, files gone.
	var vfs []models.VideoFile
	db.Where("clip_id = ?", clip.ID).Find(&vfs)
	for i := range vfs {
		_ = db.Delete(&vfs[i])
		_ = db.Unscoped().Delete(&models.VideoFile{}, vfs[i].ID)
	}
	_ = db.Model(&models.Clip{}).Where("id = ?", clip.ID).Update("retention_pending", true)
	_ = db.Where("id = ?", clip.ID).Delete(&models.Clip{})
	_ = os.Remove(path)
	var tel models.Telemetry
	if err := db.Unscoped().Where("clip_id = ?", clip.ID).First(&tel).Error; err != nil {
		t.Fatalf("telemetry: %v", err)
	}
	_ = db.Unscoped().Model(&tel).Update("deleted_at", time.Now()).Error

	svc := newTestRetention(t, db, root, cfgPath, RetentionConfig{
		Enabled: true, DryRun: false, MaxAgeDays: 90, MaxSizeGB: 0, MinFileAgeMinutes: 0,
	})
	svc.beforeHardDeleteTelemetry = func(clipID uint) error {
		return fmt.Errorf("injected telemetry delete failure")
	}

	report, err := svc.Run()
	if err != nil {
		t.Fatalf("run: %v", err)
	}
	foundErr := false
	for _, e := range report.Errors {
		if strings.Contains(e, "telemetry") && strings.Contains(e, "injected") {
			foundErr = true
			break
		}
	}
	if !foundErr {
		t.Fatalf("expected telemetry error in report.Errors, got %+v", report.Errors)
	}
	var n int
	db.Unscoped().Model(&models.Clip{}).Where("id = ?", clip.ID).Count(&n)
	if n != 1 {
		t.Fatalf("clip soft-delete marker must remain after telemetry failure, count=%d", n)
	}
	db.Unscoped().Model(&models.Telemetry{}).Where("clip_id = ?", clip.ID).Count(&n)
	if n != 1 {
		t.Fatalf("telemetry must remain for retry, count=%d", n)
	}

	// Restart without the failure hook: recovery finishes both rows.
	svc.beforeHardDeleteTelemetry = nil
	report2, err := svc.Run()
	if err != nil {
		t.Fatalf("restart: %v", err)
	}
	db.Unscoped().Model(&models.Telemetry{}).Count(&n)
	if n != 0 {
		t.Fatalf("telemetry must be 0 after restart recovery, count=%d report=%+v", n, report2)
	}
	db.Unscoped().Model(&models.Clip{}).Where("id = ?", clip.ID).Count(&n)
	if n != 0 {
		t.Fatalf("clip must be hard-deleted after restart, count=%d", n)
	}
}

// Stranded telemetry with no clip row (older buggy hard-delete order) is swept.
func TestRetentionOrphanTelemetrySweep(t *testing.T) {
	db := setupRetentionTestDB(t)
	root := t.TempDir()
	cfgPath := t.TempDir()

	tel := models.Telemetry{ClipID: 99999, Speed: 42}
	if err := db.Create(&tel).Error; err != nil {
		t.Fatalf("create orphan telemetry: %v", err)
	}
	_ = db.Unscoped().Model(&tel).Update("deleted_at", time.Now()).Error

	svc := newTestRetention(t, db, root, cfgPath, RetentionConfig{
		Enabled: true, DryRun: false, MaxAgeDays: 90, MaxSizeGB: 0, MinFileAgeMinutes: 0,
	})
	if _, err := svc.Run(); err != nil {
		t.Fatalf("run: %v", err)
	}
	var n int
	db.Unscoped().Model(&models.Telemetry{}).Count(&n)
	if n != 0 {
		t.Fatalf("orphan telemetry sweep must leave telemetry=0, count=%d", n)
	}
}
