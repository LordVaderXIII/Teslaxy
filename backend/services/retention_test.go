package services

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/jinzhu/gorm"
	_ "github.com/jinzhu/gorm/dialects/sqlite"
	"teslaxy/models"
)

func setupRetentionTestDB(t *testing.T) *gorm.DB {
	t.Helper()
	db, err := gorm.Open("sqlite3", ":memory:")
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })
	if err := db.AutoMigrate(&models.Clip{}, &models.VideoFile{}, &models.Telemetry{}).Error; err != nil {
		t.Fatalf("migrate: %v", err)
	}
	return db
}

func writeFixtureFile(t *testing.T, path string, size int, mtime time.Time) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	data := make([]byte, size)
	for i := range data {
		data[i] = byte('A' + (i % 26))
	}
	if err := os.WriteFile(path, data, 0644); err != nil {
		t.Fatalf("write %s: %v", path, err)
	}
	if err := os.Chtimes(path, mtime, mtime); err != nil {
		t.Fatalf("chtimes %s: %v", path, err)
	}
}

func seedClip(t *testing.T, db *gorm.DB, event string, ts time.Time, protected bool, sourceDir string, files map[string]string) models.Clip {
	t.Helper()
	clip := models.Clip{
		Timestamp: ts,
		Event:     event,
		Protected: protected,
		SourceDir: sourceDir,
		City:      "Testville",
		Reason:    "unit_test",
	}
	if err := db.Create(&clip).Error; err != nil {
		t.Fatalf("create clip: %v", err)
	}
	tel := models.Telemetry{ClipID: clip.ID, Speed: 1}
	if err := db.Create(&tel).Error; err != nil {
		t.Fatalf("create telemetry: %v", err)
	}
	_ = db.Model(&clip).Update("telemetry_id", tel.ID)
	for cam, path := range files {
		vf := models.VideoFile{
			ClipID:    clip.ID,
			Camera:    cam,
			FilePath:  path,
			Timestamp: ts,
		}
		if err := db.Create(&vf).Error; err != nil {
			t.Fatalf("create videofile: %v", err)
		}
	}
	var out models.Clip
	if err := db.Preload("VideoFiles").Preload("Telemetry").First(&out, clip.ID).Error; err != nil {
		t.Fatalf("reload clip: %v", err)
	}
	return out
}

func newTestRetention(t *testing.T, db *gorm.DB, root, configPath string, cfg RetentionConfig) *RetentionService {
	t.Helper()
	cfg.FootagePath = root
	normalizeRetentionConfig(&cfg)
	if err := SaveRetentionConfig(configPath, cfg); err != nil {
		t.Fatalf("save config: %v", err)
	}
	svc := NewRetentionService(db, root, configPath)
	svc.nowFn = func() time.Time { return time.Date(2026, 10, 14, 12, 0, 0, 0, time.UTC) }
	return svc
}

func TestRetentionAgePruning(t *testing.T) {
	db := setupRetentionTestDB(t)
	root := t.TempDir()
	cfgPath := t.TempDir()
	now := time.Date(2026, 10, 14, 12, 0, 0, 0, time.UTC)
	oldTS := now.AddDate(0, 0, -120)
	newTS := now.AddDate(0, 0, -10)

	oldPath := filepath.Join(root, "RecentClips", "old-front.mp4")
	newPath := filepath.Join(root, "RecentClips", "new-front.mp4")
	writeFixtureFile(t, oldPath, 1024, now.Add(-time.Hour))
	writeFixtureFile(t, newPath, 2048, now.Add(-time.Hour))

	oldClip := seedClip(t, db, "Recent", oldTS, false, filepath.Join(root, "RecentClips"), map[string]string{"Front": oldPath})
	newClip := seedClip(t, db, "Recent", newTS, false, filepath.Join(root, "RecentClips"), map[string]string{"Front": newPath})

	svc := newTestRetention(t, db, root, cfgPath, RetentionConfig{
		Enabled:           true,
		DryRun:            false,
		MaxAgeDays:        90,
		MaxSizeGB:         0, // disable size
		MinFileAgeMinutes: 0,
	})

	report, err := svc.Run()
	if err != nil {
		t.Fatalf("run: %v", err)
	}
	if len(report.Deleted) != 1 || report.Deleted[0].ClipID != oldClip.ID {
		t.Fatalf("deleted=%v want only old clip %d", report.Deleted, oldClip.ID)
	}
	if report.Deleted[0].Reason != "age" {
		t.Fatalf("reason=%q want age", report.Deleted[0].Reason)
	}
	if _, err := os.Stat(oldPath); !os.IsNotExist(err) {
		t.Fatalf("old file should be gone, err=%v", err)
	}
	if _, err := os.Stat(newPath); err != nil {
		t.Fatalf("new file should remain: %v", err)
	}
	var count int
	db.Model(&models.Clip{}).Where("id = ?", oldClip.ID).Count(&count)
	if count != 0 {
		t.Fatalf("old clip row should be gone")
	}
	db.Model(&models.Clip{}).Where("id = ?", newClip.ID).Count(&count)
	if count != 1 {
		t.Fatalf("new clip should remain")
	}
}

func TestRetentionSizeCapPruning(t *testing.T) {
	db := setupRetentionTestDB(t)
	root := t.TempDir()
	cfgPath := t.TempDir()
	now := time.Date(2026, 10, 14, 12, 0, 0, 0, time.UTC)

	// Three recent clips, 4KB each. Cap at ~5KB so oldest must go.
	paths := []string{}
	var clips []models.Clip
	for i := 0; i < 3; i++ {
		p := filepath.Join(root, "RecentClips", fmt.Sprintf("clip%d.mp4", i))
		writeFixtureFile(t, p, 4096, now.Add(-time.Hour))
		paths = append(paths, p)
		ts := now.AddDate(0, 0, -i) // clip0 newest, clip2 oldest
		clips = append(clips, seedClip(t, db, "Recent", ts, false, filepath.Join(root, "RecentClips"), map[string]string{"Front": p}))
	}

	svc := newTestRetention(t, db, root, cfgPath, RetentionConfig{
		Enabled:           true,
		DryRun:            false,
		MaxAgeDays:        0,
		MaxSizeGB:         5000.0 / (1024.0 * 1024.0 * 1024.0), // ~5000 bytes
		MinFileAgeMinutes: 0,
	})

	report, err := svc.Run()
	if err != nil {
		t.Fatalf("run: %v", err)
	}
	if len(report.Deleted) == 0 {
		t.Fatalf("expected size pruning, report=%+v", report)
	}
	for _, d := range report.Deleted {
		if d.Reason != "size" {
			t.Fatalf("reason=%q want size", d.Reason)
		}
	}
	// Oldest timestamps deleted first: clips[2], then clips[1] as needed.
	if report.Deleted[0].ClipID != clips[2].ID {
		t.Fatalf("first deleted=%d want oldest %d", report.Deleted[0].ClipID, clips[2].ID)
	}
	if _, err := os.Stat(paths[0]); err != nil {
		t.Fatalf("newest should remain: %v", err)
	}
}

func TestRetentionProtectsSavedAndFlagged(t *testing.T) {
	db := setupRetentionTestDB(t)
	root := t.TempDir()
	cfgPath := t.TempDir()
	now := time.Date(2026, 10, 14, 12, 0, 0, 0, time.UTC)
	old := now.AddDate(0, 0, -200)

	savedDir := filepath.Join(root, "SavedClips", "2024-01-01_00-00-00")
	savedPath := filepath.Join(savedDir, "front.mp4")
	writeFixtureFile(t, savedPath, 1024, now.Add(-time.Hour))
	_ = seedClip(t, db, "Saved", old, false, savedDir, map[string]string{"Front": savedPath})

	flagDir := filepath.Join(root, "SentryClips", "flagged")
	flagPath := filepath.Join(flagDir, "front.mp4")
	writeFixtureFile(t, flagPath, 1024, now.Add(-time.Hour))
	_ = seedClip(t, db, "Sentry", old, true, flagDir, map[string]string{"Front": flagPath})

	pruneDir := filepath.Join(root, "SentryClips", "old")
	prunePath := filepath.Join(pruneDir, "front.mp4")
	writeFixtureFile(t, prunePath, 1024, now.Add(-time.Hour))
	pruneClip := seedClip(t, db, "Sentry", old, false, pruneDir, map[string]string{"Front": prunePath})

	svc := newTestRetention(t, db, root, cfgPath, RetentionConfig{
		Enabled:           true,
		DryRun:            false,
		MaxAgeDays:        90,
		MaxSizeGB:         0,
		MinFileAgeMinutes: 0,
	})
	report, err := svc.Run()
	if err != nil {
		t.Fatalf("run: %v", err)
	}
	if len(report.Deleted) != 1 || report.Deleted[0].ClipID != pruneClip.ID {
		t.Fatalf("deleted=%v want only unprotected sentry %d", report.Deleted, pruneClip.ID)
	}
	if _, err := os.Stat(savedPath); err != nil {
		t.Fatalf("saved file must remain: %v", err)
	}
	if _, err := os.Stat(flagPath); err != nil {
		t.Fatalf("flagged file must remain: %v", err)
	}
	if _, err := os.Stat(prunePath); !os.IsNotExist(err) {
		t.Fatalf("unprotected sentry should be deleted")
	}
}

func TestRetentionDryRunDeletesNothing(t *testing.T) {
	db := setupRetentionTestDB(t)
	root := t.TempDir()
	cfgPath := t.TempDir()
	now := time.Date(2026, 10, 14, 12, 0, 0, 0, time.UTC)
	old := now.AddDate(0, 0, -200)
	path := filepath.Join(root, "RecentClips", "old.mp4")
	writeFixtureFile(t, path, 4096, now.Add(-time.Hour))
	clip := seedClip(t, db, "Recent", old, false, filepath.Join(root, "RecentClips"), map[string]string{"Front": path})

	svc := newTestRetention(t, db, root, cfgPath, RetentionConfig{
		Enabled:           true,
		DryRun:            true,
		MaxAgeDays:        90,
		MaxSizeGB:         0,
		MinFileAgeMinutes: 0,
	})
	report, err := svc.Run()
	if err != nil {
		t.Fatalf("run: %v", err)
	}
	if len(report.Candidates) != 1 || report.Candidates[0].ClipID != clip.ID {
		t.Fatalf("candidates=%v", report.Candidates)
	}
	if len(report.Deleted) != 0 {
		t.Fatalf("dry-run must not delete, got %v", report.Deleted)
	}
	if report.BytesFreed != 4096 {
		t.Fatalf("bytes_freed=%d want 4096", report.BytesFreed)
	}
	if _, err := os.Stat(path); err != nil {
		t.Fatalf("file must remain after dry-run: %v", err)
	}
	var count int
	db.Model(&models.Clip{}).Where("id = ?", clip.ID).Count(&count)
	if count != 1 {
		t.Fatalf("db row must remain after dry-run")
	}
}

func TestRetentionDBFileConsistencyAndPartialFailure(t *testing.T) {
	db := setupRetentionTestDB(t)
	root := t.TempDir()
	cfgPath := t.TempDir()
	now := time.Date(2026, 10, 14, 12, 0, 0, 0, time.UTC)
	old := now.AddDate(0, 0, -200)
	dir := filepath.Join(root, "SentryClips", "evt")
	path := filepath.Join(dir, "front.mp4")
	writeFixtureFile(t, path, 2048, now.Add(-time.Hour))
	_ = os.WriteFile(filepath.Join(dir, "event.json"), []byte(`{"city":"X"}`), 0644)
	clip := seedClip(t, db, "Sentry", old, false, dir, map[string]string{"Front": path})

	svc := newTestRetention(t, db, root, cfgPath, RetentionConfig{
		Enabled: true, DryRun: false, MaxAgeDays: 90, MaxSizeGB: 0, MinFileAgeMinutes: 0,
	})
	report, err := svc.Run()
	if err != nil {
		t.Fatalf("run: %v", err)
	}
	if len(report.Deleted) != 1 {
		t.Fatalf("deleted=%v", report.Deleted)
	}
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Fatalf("mp4 should be gone")
	}
	var live int
	db.Model(&models.Clip{}).Where("id = ?", clip.ID).Count(&live)
	if live != 0 {
		t.Fatalf("clip should be hard-deleted after successful prune")
	}
	var vf int
	db.Unscoped().Model(&models.VideoFile{}).Where("clip_id = ?", clip.ID).Count(&vf)
	if vf != 0 {
		t.Fatalf("videofile rows should be hard-deleted, got %d", vf)
	}
}

func TestRetentionSkipsPathTraversalAndSymlinks(t *testing.T) {
	db := setupRetentionTestDB(t)
	root := t.TempDir()
	cfgPath := t.TempDir()
	outside := t.TempDir()
	now := time.Date(2026, 10, 14, 12, 0, 0, 0, time.UTC)
	old := now.AddDate(0, 0, -200)

	secret := filepath.Join(outside, "secret.mp4")
	writeFixtureFile(t, secret, 512, now.Add(-time.Hour))

	// Path traversal style path stored in DB.
	trav := filepath.Join(root, "..", filepath.Base(outside), "secret.mp4")
	_ = seedClip(t, db, "Recent", old, false, root, map[string]string{"Front": trav})

	linkDir := filepath.Join(root, "RecentClips")
	_ = os.MkdirAll(linkDir, 0755)
	linkPath := filepath.Join(linkDir, "link.mp4")
	if err := os.Symlink(secret, linkPath); err != nil {
		t.Fatalf("symlink: %v", err)
	}
	_ = seedClip(t, db, "Recent", old.Add(time.Second), false, linkDir, map[string]string{"Front": linkPath})

	svc := newTestRetention(t, db, root, cfgPath, RetentionConfig{
		Enabled: true, DryRun: false, MaxAgeDays: 90, MaxSizeGB: 0, MinFileAgeMinutes: 0,
	})
	report, err := svc.Run()
	if err != nil {
		t.Fatalf("run: %v", err)
	}
	if len(report.Deleted) != 0 {
		t.Fatalf("must not delete traversal/symlink targets, deleted=%v skipped=%v", report.Deleted, report.Skipped)
	}
	if _, err := os.Stat(secret); err != nil {
		t.Fatalf("outside secret must remain: %v", err)
	}
	var sawOutside, sawSymlink bool
	for _, s := range report.Skipped {
		if s.Reason == "outside_root" {
			sawOutside = true
		}
		if s.Reason == "symlink" {
			sawSymlink = true
		}
	}
	if !sawOutside || !sawSymlink {
		t.Fatalf("expected outside_root and symlink skips, got %#v", report.Skipped)
	}
}

func TestRetentionSkipsRecentMtime(t *testing.T) {
	db := setupRetentionTestDB(t)
	root := t.TempDir()
	cfgPath := t.TempDir()
	now := time.Date(2026, 10, 14, 12, 0, 0, 0, time.UTC)
	old := now.AddDate(0, 0, -200)
	path := filepath.Join(root, "RecentClips", "writing.mp4")
	writeFixtureFile(t, path, 1024, now.Add(-2*time.Minute)) // fresher than 15m gate

	_ = seedClip(t, db, "Recent", old, false, filepath.Join(root, "RecentClips"), map[string]string{"Front": path})
	svc := newTestRetention(t, db, root, cfgPath, RetentionConfig{
		Enabled: true, DryRun: false, MaxAgeDays: 90, MaxSizeGB: 0, MinFileAgeMinutes: 15,
	})
	report, err := svc.Run()
	if err != nil {
		t.Fatalf("run: %v", err)
	}
	if len(report.Deleted) != 0 || len(report.Candidates) != 0 {
		t.Fatalf("in-progress file must be skipped, report=%+v", report)
	}
	var saw bool
	for _, s := range report.Skipped {
		if s.Reason == "recent_mtime" {
			saw = true
		}
	}
	if !saw {
		t.Fatalf("expected recent_mtime skip, got %#v", report.Skipped)
	}
	if _, err := os.Stat(path); err != nil {
		t.Fatalf("file must remain: %v", err)
	}
}

func TestRetentionConfigEnvAndSettingsFile(t *testing.T) {
	cfgPath := t.TempDir()
	t.Setenv("RETENTION_ENABLED", "true")
	t.Setenv("RETENTION_DRY_RUN", "false")
	t.Setenv("RETENTION_MAX_AGE_DAYS", "30")
	t.Setenv("RETENTION_MAX_SIZE_GB", "100")
	t.Setenv("FOOTAGE_PATH", "/tmp/footage-env")

	cfg, err := LoadRetentionConfig(cfgPath)
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	if !cfg.Enabled || cfg.DryRun || cfg.MaxAgeDays != 30 || cfg.MaxSizeGB != 100 {
		t.Fatalf("env cfg unexpected: %+v", cfg)
	}

	patch := `{"dry_run": true, "max_age_days": 7}`
	if err := os.WriteFile(filepath.Join(cfgPath, RetentionSettingsFileName), []byte(patch), 0644); err != nil {
		t.Fatalf("write settings: %v", err)
	}
	cfg2, err := LoadRetentionConfig(cfgPath)
	if err != nil {
		t.Fatalf("load2: %v", err)
	}
	if !cfg2.DryRun || cfg2.MaxAgeDays != 7 || !cfg2.Enabled {
		t.Fatalf("file overlay unexpected: %+v", cfg2)
	}
}

func TestIsClipProtected(t *testing.T) {
	if !IsClipProtected(models.Clip{Event: "Saved"}) {
		t.Fatal("Saved should be protected")
	}
	if !IsClipProtected(models.Clip{Event: "Sentry", Protected: true}) {
		t.Fatal("flagged should be protected")
	}
	if IsClipProtected(models.Clip{Event: "Recent"}) {
		t.Fatal("plain Recent should not be protected")
	}
}

func TestRetentionReportJSONShape(t *testing.T) {
	db := setupRetentionTestDB(t)
	root := t.TempDir()
	cfgPath := t.TempDir()
	now := time.Date(2026, 10, 14, 12, 0, 0, 0, time.UTC)
	path := filepath.Join(root, "RecentClips", "x.mp4")
	writeFixtureFile(t, path, 100, now.Add(-time.Hour))
	_ = seedClip(t, db, "Recent", now.AddDate(0, 0, -200), false, filepath.Join(root, "RecentClips"), map[string]string{"Front": path})

	svc := newTestRetention(t, db, root, cfgPath, RetentionConfig{
		Enabled: true, DryRun: true, MaxAgeDays: 90, MaxSizeGB: 0, MinFileAgeMinutes: 0,
	})
	report, err := svc.Run()
	if err != nil {
		t.Fatalf("run: %v", err)
	}
	raw, err := json.Marshal(report)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	s := string(raw)
	for _, key := range []string{"dry_run", "candidates", "bytes_freed", "footage_root"} {
		if !strings.Contains(s, key) {
			t.Fatalf("report missing %s: %s", key, s)
		}
	}
}
