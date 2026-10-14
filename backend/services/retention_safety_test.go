package services

import (
	"fmt"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/jinzhu/gorm"
	_ "github.com/jinzhu/gorm/dialects/sqlite"
	"teslaxy/models"
)

func TestRetentionRefusesSymlinkedParents(t *testing.T) {
	db := setupRetentionTestDB(t)
	root := t.TempDir()
	outside := t.TempDir()
	cfgPath := t.TempDir()
	now := time.Date(2026, 10, 14, 12, 0, 0, 0, time.UTC)
	old := now.AddDate(0, 0, -200)

	canary := filepath.Join(outside, "canary.mp4")
	writeFixtureFile(t, canary, 512, now.Add(-time.Hour))

	// Nested dir symlink: root/RecentClips -> outside
	link := filepath.Join(root, "RecentClips")
	if err := os.Symlink(outside, link); err != nil {
		t.Fatalf("symlink: %v", err)
	}
	dbPath := filepath.Join(link, "canary.mp4")
	_ = seedClip(t, db, "Recent", old, false, link, map[string]string{"Front": dbPath})

	svc := newTestRetention(t, db, root, cfgPath, RetentionConfig{
		Enabled: true, DryRun: false, MaxAgeDays: 90, MaxSizeGB: 0, MinFileAgeMinutes: 0,
	})
	report, err := svc.Run()
	if err != nil {
		t.Fatalf("run: %v", err)
	}
	if len(report.Deleted) != 0 {
		t.Fatalf("must not delete via symlinked parent: %+v", report.Deleted)
	}
	if _, err := os.Stat(canary); err != nil {
		t.Fatalf("outside canary must survive: %v", err)
	}
}

func TestRetentionNestedDirSymlinkCanary(t *testing.T) {
	db := setupRetentionTestDB(t)
	root := t.TempDir()
	outside := t.TempDir()
	cfgPath := t.TempDir()
	now := time.Date(2026, 10, 14, 12, 0, 0, 0, time.UTC)
	old := now.AddDate(0, 0, -200)

	canary := filepath.Join(outside, "secret.mp4")
	writeFixtureFile(t, canary, 700, now.Add(-time.Hour))

	mid := filepath.Join(root, "SentryClips")
	_ = os.MkdirAll(mid, 0755)
	eventLink := filepath.Join(mid, "evt")
	if err := os.Symlink(outside, eventLink); err != nil {
		t.Fatalf("symlink: %v", err)
	}
	_ = seedClip(t, db, "Sentry", old, false, eventLink, map[string]string{
		"Front": filepath.Join(eventLink, "secret.mp4"),
	})

	svc := newTestRetention(t, db, root, cfgPath, RetentionConfig{
		Enabled: true, DryRun: false, MaxAgeDays: 90, MaxSizeGB: 0, MinFileAgeMinutes: 0,
	})
	_, _ = svc.Run()
	if _, err := os.Stat(canary); err != nil {
		t.Fatalf("canary deleted via nested symlink parent: %v", err)
	}
}

func TestRetentionDoesNotDeleteSiblingFiles(t *testing.T) {
	db := setupRetentionTestDB(t)
	root := t.TempDir()
	cfgPath := t.TempDir()
	now := time.Date(2026, 10, 14, 12, 0, 0, 0, time.UTC)
	old := now.AddDate(0, 0, -200)

	dir := filepath.Join(root, "SentryClips", "evt")
	front := filepath.Join(dir, "front.mp4")
	extra := filepath.Join(dir, "extra_unindexed.mp4")
	thumb := filepath.Join(dir, "thumb.jpg")
	eventJSON := filepath.Join(dir, "event.json")
	writeFixtureFile(t, front, 1024, now.Add(-time.Hour))
	writeFixtureFile(t, extra, 2048, now.Add(-time.Minute)) // fresh
	writeFixtureFile(t, thumb, 100, now.Add(-time.Hour))
	_ = os.WriteFile(eventJSON, []byte(`{}`), 0644)

	_ = seedClip(t, db, "Sentry", old, false, dir, map[string]string{"Front": front})

	svc := newTestRetention(t, db, root, cfgPath, RetentionConfig{
		Enabled: true, DryRun: false, MaxAgeDays: 90, MaxSizeGB: 0, MinFileAgeMinutes: 15,
	})
	svc.nowFn = func() time.Time { return now }
	report, err := svc.Run()
	if err != nil {
		t.Fatalf("run: %v", err)
	}
	if len(report.Deleted) != 1 {
		t.Fatalf("expected front pruned, got %+v", report)
	}
	if _, err := os.Stat(front); !os.IsNotExist(err) {
		t.Fatalf("front should be gone")
	}
	for _, p := range []string{extra, thumb, eventJSON} {
		if _, err := os.Stat(p); err != nil {
			t.Fatalf("sibling %s must survive: %v", p, err)
		}
	}
}

func TestRetentionProtectedSiblingSurvivesSharedDir(t *testing.T) {
	db := setupRetentionTestDB(t)
	root := t.TempDir()
	cfgPath := t.TempDir()
	now := time.Date(2026, 10, 14, 12, 0, 0, 0, time.UTC)
	old := now.AddDate(0, 0, -200)
	dir := filepath.Join(root, "SentryClips", "shared")
	a := filepath.Join(dir, "a.mp4")
	b := filepath.Join(dir, "b.mp4")
	writeFixtureFile(t, a, 1024, now.Add(-time.Hour))
	writeFixtureFile(t, b, 1024, now.Add(-time.Hour))

	_ = seedClip(t, db, "Sentry", old, false, dir, map[string]string{"Front": a})
	_ = seedClip(t, db, "Sentry", old.Add(time.Second), true, dir, map[string]string{"Front": b})

	svc := newTestRetention(t, db, root, cfgPath, RetentionConfig{
		Enabled: true, DryRun: false, MaxAgeDays: 90, MaxSizeGB: 0, MinFileAgeMinutes: 0,
	})
	_, _ = svc.Run()
	if _, err := os.Stat(b); err != nil {
		t.Fatalf("protected sibling file must survive: %v", err)
	}
}

func TestRetentionDryRunEnvTypoStaysOn(t *testing.T) {
	t.Setenv("RETENTION_ENABLED", "true")
	t.Setenv("RETENTION_DRY_RUN", "tru")
	cfg, err := LoadRetentionConfig(t.TempDir())
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	if !cfg.DryRun {
		t.Fatalf("typo must keep dry_run ON, got %+v", cfg)
	}
	if !cfg.Enabled {
		t.Fatalf("enabled should parse true")
	}
}

func TestRetentionConfigClampsOverflow(t *testing.T) {
	cfg := RetentionConfig{
		Enabled: true, DryRun: false,
		MaxAgeDays:        9223372036854775807,
		MaxSizeGB:         1e100,
		IntervalHours:     999999,
		MinFileAgeMinutes: -5,
	}
	normalizeRetentionConfig(&cfg)
	if cfg.MaxAgeDays != MaxRetentionAgeDays {
		t.Fatalf("age clamp: %d", cfg.MaxAgeDays)
	}
	if cfg.MaxSizeGB != MaxRetentionSizeGB {
		t.Fatalf("size clamp: %v", cfg.MaxSizeGB)
	}
	if cfg.MaxSizeBytes() <= 0 {
		t.Fatalf("max size bytes must be positive after clamp")
	}
	if cfg.MinFileAgeMinutes != 0 {
		t.Fatalf("neg min age -> 0, got %d", cfg.MinFileAgeMinutes)
	}

	now := time.Date(2026, 10, 14, 12, 0, 0, 0, time.UTC)
	cutoff := now.AddDate(0, 0, -cfg.MaxAgeDays)
	oneHourOld := now.Add(-time.Hour)
	if !oneHourOld.After(cutoff) {
		t.Fatalf("clamped age must not treat 1h-old as expired")
	}
}

func TestRetentionSizeCountsFreshAndDedupes(t *testing.T) {
	db := setupRetentionTestDB(t)
	root := t.TempDir()
	cfgPath := t.TempDir()
	now := time.Date(2026, 10, 14, 12, 0, 0, 0, time.UTC)

	fresh := filepath.Join(root, "RecentClips", "fresh.mp4")
	old1 := filepath.Join(root, "RecentClips", "old1.mp4")
	writeFixtureFile(t, fresh, 8192, now.Add(-time.Minute))
	writeFixtureFile(t, old1, 3072, now.Add(-time.Hour))
	_ = seedClip(t, db, "Recent", now.AddDate(0, 0, -10), false, filepath.Join(root, "RecentClips"), map[string]string{"Front": fresh})
	_ = seedClip(t, db, "Recent", now.AddDate(0, 0, -20), false, filepath.Join(root, "RecentClips"), map[string]string{"Front": old1})

	svc := newTestRetention(t, db, root, cfgPath, RetentionConfig{
		Enabled: true, DryRun: true, MaxAgeDays: 0,
		MaxSizeGB:         4096.0 / (1024 * 1024 * 1024),
		MinFileAgeMinutes: 15,
	})
	svc.nowFn = func() time.Time { return now }
	report, err := svc.Run()
	if err != nil {
		t.Fatalf("run: %v", err)
	}
	// Total must include fresh (8192) + old (3072) = 11264
	if report.TotalBytesBefore != 11264 {
		t.Fatalf("total=%d want 11264 (fresh counted)", report.TotalBytesBefore)
	}
}

func TestRetentionSizeDedupesHardlinks(t *testing.T) {
	db := setupRetentionTestDB(t)
	root := t.TempDir()
	cfgPath := t.TempDir()
	now := time.Date(2026, 10, 14, 12, 0, 0, 0, time.UTC)

	a := filepath.Join(root, "RecentClips", "a.mp4")
	b := filepath.Join(root, "RecentClips", "b.mp4")
	writeFixtureFile(t, a, 4096, now.Add(-time.Hour))
	if err := os.Link(a, b); err != nil {
		t.Fatalf("hardlink: %v", err)
	}
	_ = seedClip(t, db, "Recent", now.AddDate(0, 0, -30), false, filepath.Join(root, "RecentClips"), map[string]string{"Front": a})
	_ = seedClip(t, db, "Recent", now.AddDate(0, 0, -40), false, filepath.Join(root, "RecentClips"), map[string]string{"Front": b})

	svc := newTestRetention(t, db, root, cfgPath, RetentionConfig{
		Enabled: true, DryRun: true, MaxAgeDays: 0,
		MaxSizeGB:         5000.0 / (1024 * 1024 * 1024),
		MinFileAgeMinutes: 0,
	})
	report, err := svc.Run()
	if err != nil {
		t.Fatalf("run: %v", err)
	}
	if report.TotalBytesBefore != 4096 {
		t.Fatalf("hardlink total=%d want 4096", report.TotalBytesBefore)
	}
	if len(report.Candidates) != 0 {
		t.Fatalf("under unique-byte cap, no prune expected: %+v", report.Candidates)
	}
}

func TestRetentionPartialKeepsRemainingRows(t *testing.T) {
	db := setupRetentionTestDB(t)
	root := t.TempDir()
	cfgPath := t.TempDir()
	now := time.Date(2026, 10, 14, 12, 0, 0, 0, time.UTC)
	old := now.AddDate(0, 0, -200)
	dir := filepath.Join(root, "RecentClips")
	front := filepath.Join(dir, "front.mp4")
	back := filepath.Join(dir, "back.mp4")
	writeFixtureFile(t, front, 1024, now.Add(-time.Hour))
	writeFixtureFile(t, back, 1024, now.Add(-time.Minute)) // fresh — not selected

	clip := seedClip(t, db, "Recent", old, false, dir, map[string]string{
		"Front": front,
		"Back":  back,
	})

	svc := newTestRetention(t, db, root, cfgPath, RetentionConfig{
		Enabled: true, DryRun: false, MaxAgeDays: 90, MaxSizeGB: 0, MinFileAgeMinutes: 15,
	})
	svc.nowFn = func() time.Time { return now }
	_, err := svc.Run()
	if err != nil {
		t.Fatalf("run: %v", err)
	}
	if _, err := os.Stat(back); err != nil {
		t.Fatalf("fresh back must remain: %v", err)
	}
	var live int
	db.Model(&models.Clip{}).Where("id = ?", clip.ID).Count(&live)
	if live != 1 {
		t.Fatalf("clip row must remain while back file exists, live=%d", live)
	}
	var vfs int
	db.Model(&models.VideoFile{}).Where("clip_id = ? AND camera = ?", clip.ID, "Back").Count(&vfs)
	if vfs != 1 {
		t.Fatalf("back videofile row must remain, got %d", vfs)
	}
}

func TestRetentionRecoversSoftDeleted(t *testing.T) {
	db := setupRetentionTestDB(t)
	root := t.TempDir()
	cfgPath := t.TempDir()
	now := time.Date(2026, 10, 14, 12, 0, 0, 0, time.UTC)
	old := now.AddDate(0, 0, -200)
	path := filepath.Join(root, "RecentClips", "x.mp4")
	writeFixtureFile(t, path, 2048, now.Add(-time.Hour))
	clip := seedClip(t, db, "Recent", old, false, filepath.Join(root, "RecentClips"), map[string]string{"Front": path})

	// Simulate interrupted prune: soft-delete without unlink.
	if err := db.Delete(&clip).Error; err != nil {
		t.Fatalf("soft clip: %v", err)
	}
	var vfs []models.VideoFile
	db.Where("clip_id = ?", clip.ID).Find(&vfs)
	for i := range vfs {
		if err := db.Delete(&vfs[i]).Error; err != nil {
			t.Fatalf("soft vf: %v", err)
		}
	}

	svc := newTestRetention(t, db, root, cfgPath, RetentionConfig{
		Enabled: true, DryRun: false, MaxAgeDays: 90, MaxSizeGB: 0, MinFileAgeMinutes: 0,
	})
	report, err := svc.Run()
	if err != nil {
		t.Fatalf("run: %v", err)
	}
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Fatalf("pending file should be unlinked on recovery")
	}
	var tomb int
	db.Unscoped().Model(&models.Clip{}).Where("id = ?", clip.ID).Count(&tomb)
	if tomb != 0 {
		t.Fatalf("tombstone should be hard-deleted, count=%d report=%+v", tomb, report)
	}
}

func TestScannerSkipsPendingDelete(t *testing.T) {
	db := setupRetentionTestDB(t)
	root := t.TempDir()
	now := time.Date(2026, 10, 14, 12, 0, 0, 0, time.UTC)
	dir := filepath.Join(root, "SentryClips", "2024-01-01_00-00-00")
	path := filepath.Join(dir, "2024-01-01_00-00-00-front.mp4")
	writeFixtureFile(t, path, 1024, now.Add(-time.Hour))
	_ = os.WriteFile(filepath.Join(dir, "event.json"), []byte(`{"timestamp":"2024-01-01T00:00:00","city":"X","reason":"test","est_lat":0,"est_lon":0}`), 0644)

	clip := seedClip(t, db, "Sentry", now.AddDate(0, 0, -200), false, dir, map[string]string{"Front": path})
	_ = db.Delete(&clip)
	var vfs []models.VideoFile
	db.Where("clip_id = ?", clip.ID).Find(&vfs)
	for i := range vfs {
		_ = db.Delete(&vfs[i])
	}

	scanner := NewScannerService(root, db)
	scanner.processEventGroup(dir, []string{path})

	var live int
	db.Model(&models.Clip{}).Count(&live)
	if live != 0 {
		t.Fatalf("scanner must not recreate pending-delete clip, live=%d", live)
	}
}

func TestRetentionRechecksProtectInTransaction(t *testing.T) {
	db := setupRetentionTestDB(t)
	root := t.TempDir()
	cfgPath := t.TempDir()
	now := time.Date(2026, 10, 14, 12, 0, 0, 0, time.UTC)
	old := now.AddDate(0, 0, -200)
	path := filepath.Join(root, "RecentClips", "p.mp4")
	writeFixtureFile(t, path, 1024, now.Add(-time.Hour))
	clip := seedClip(t, db, "Recent", old, false, filepath.Join(root, "RecentClips"), map[string]string{"Front": path})

	svc := newTestRetention(t, db, root, cfgPath, RetentionConfig{
		Enabled: true, DryRun: false, MaxAgeDays: 90, MaxSizeGB: 0, MinFileAgeMinutes: 0,
	})
	cfg := svc.Config()
	fr, err := openFootageRoot(root)
	if err != nil {
		t.Fatalf("open root: %v", err)
	}
	defer fr.Close()

	// Simulate race: candidate selected while unprotected, then protect wins the lock before pruneClip.
	cand := RetentionCandidate{
		ClipID: clip.ID, Event: "Recent", Timestamp: old, Reason: "age",
		Bytes: 1024, FilePaths: []string{path},
	}
	svc.WithClipLock(func() {
		if err := db.Model(&models.Clip{}).Where("id = ?", clip.ID).Update("protected", true).Error; err != nil {
			t.Fatalf("protect: %v", err)
		}
	})
	if _, err := svc.pruneClip(fr, cand, cfg); err == nil {
		t.Fatalf("pruneClip must refuse after concurrent protect")
	}
	if _, err := os.Stat(path); err != nil {
		t.Fatalf("file must survive: %v", err)
	}
}

func TestRetentionHardDeleteErrorSurfaced(t *testing.T) {
	// Use file-backed sqlite so we can wrap deletes; simulate by using a path that
	// unlinks OK but then verify errors slice when unlink fails mid-way.
	db := setupRetentionTestDB(t)
	root := t.TempDir()
	cfgPath := t.TempDir()
	now := time.Date(2026, 10, 14, 12, 0, 0, 0, time.UTC)
	old := now.AddDate(0, 0, -200)
	path := filepath.Join(root, "RecentClips", "z.mp4")
	writeFixtureFile(t, path, 1024, now.Add(-time.Hour))
	_ = seedClip(t, db, "Recent", old, false, filepath.Join(root, "RecentClips"), map[string]string{"Front": path})

	svc := newTestRetention(t, db, root, cfgPath, RetentionConfig{
		Enabled: true, DryRun: false, MaxAgeDays: 90, MaxSizeGB: 0, MinFileAgeMinutes: 0,
	})
	svc.unlinkFn = func(root *footageRoot, p string) (int64, bool, error) {
		return 0, false, fmt.Errorf("injected unlink failure")
	}
	report, err := svc.Run()
	if err != nil {
		t.Fatalf("run err: %v", err)
	}
	if len(report.Errors) == 0 {
		t.Fatalf("expected surfaced error, report=%+v", report)
	}
	if _, err := os.Stat(path); err != nil {
		t.Fatalf("file should remain after failed unlink: %v", err)
	}
	var live int
	db.Model(&models.VideoFile{}).Count(&live)
	if live != 1 {
		t.Fatalf("videofile row restored/kept after failed unlink, live=%d", live)
	}
}

// ensure file DB helper used by recovery tests stays compiling with gorm import.
var _ = gorm.ErrRecordNotFound
