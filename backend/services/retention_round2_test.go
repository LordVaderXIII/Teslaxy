package services

import (
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/jinzhu/gorm"
	_ "github.com/jinzhu/gorm/dialects/sqlite"
	"teslaxy/models"
)

// Codex P1: swapping the footage root path for a symlink mid-prune must not
// delete an outside canary when the root fd was pinned at pass start.
func TestRetentionPinnedRootSurvivesSymlinkSwap(t *testing.T) {
	db := setupRetentionTestDB(t)
	base := t.TempDir()
	realRoot := filepath.Join(base, "real_root")
	outside := filepath.Join(base, "outside")
	cfgPath := t.TempDir()
	_ = os.MkdirAll(filepath.Join(realRoot, "RecentClips"), 0755)
	_ = os.MkdirAll(outside, 0755)

	now := time.Date(2026, 10, 14, 12, 0, 0, 0, time.UTC)
	old := now.AddDate(0, 0, -200)
	victim := filepath.Join(realRoot, "RecentClips", "old.mp4")
	canary := filepath.Join(outside, "canary.mp4")
	writeFixtureFile(t, victim, 1024, now.Add(-time.Hour))
	writeFixtureFile(t, canary, 512, now.Add(-time.Hour))

	_ = seedClip(t, db, "Recent", old, false, filepath.Join(realRoot, "RecentClips"), map[string]string{"Front": victim})

	svc := newTestRetention(t, db, realRoot, cfgPath, RetentionConfig{
		Enabled: true, DryRun: false, MaxAgeDays: 90, MaxSizeGB: 0, MinFileAgeMinutes: 0,
	})

	// After resolve succeeds for the victim, swap the path name for a symlink to outside.
	swapped := false
	origResolve := svc.resolveFn
	svc.resolveFn = func(root *footageRoot, path string) (string, os.FileInfo, error) {
		abs, info, err := root.resolve(path)
		if err == nil && !swapped {
			swapped = true
			tmp := realRoot + ".bak"
			if err := os.Rename(realRoot, tmp); err != nil {
				t.Fatalf("rename real root: %v", err)
			}
			if err := os.Symlink(outside, realRoot); err != nil {
				t.Fatalf("symlink root: %v", err)
			}
			t.Cleanup(func() {
				_ = os.Remove(realRoot)
				_ = os.Rename(tmp, realRoot)
			})
		}
		if origResolve != nil {
			return origResolve(root, path)
		}
		return abs, info, err
	}

	report, err := svc.Run()
	if err != nil {
		t.Fatalf("run: %v", err)
	}
	if _, err := os.Stat(canary); err != nil {
		t.Fatalf("outside canary must survive root symlink swap: %v report=%+v", err, report)
	}
	bakVictim := filepath.Join(realRoot+".bak", "RecentClips", "old.mp4")
	if _, err := os.Stat(bakVictim); !os.IsNotExist(err) {
		t.Fatalf("pinned-fd unlink should remove victim on original inode; stat=%v", err)
	}
	if len(report.Deleted) != 1 {
		t.Fatalf("expected victim deleted via pinned fd, report=%+v", report)
	}
}

// Codex P2: hardlink siblings — each path unlinked; bytes counted only on last link;
// DB row kept until that path's dirent is actually removed.
func TestRetentionHardlinkBytesAndRows(t *testing.T) {
	db := setupRetentionTestDB(t)
	root := t.TempDir()
	cfgPath := t.TempDir()
	now := time.Date(2026, 10, 14, 12, 0, 0, 0, time.UTC)
	old := now.AddDate(0, 0, -200)

	a := filepath.Join(root, "RecentClips", "a.mp4")
	b := filepath.Join(root, "RecentClips", "b.mp4")
	writeFixtureFile(t, a, 4096, now.Add(-time.Hour))
	if err := os.Link(a, b); err != nil {
		t.Fatalf("link: %v", err)
	}
	clipA := seedClip(t, db, "Recent", old, false, filepath.Join(root, "RecentClips"), map[string]string{"Front": a})
	clipB := seedClip(t, db, "Recent", old.Add(time.Second), false, filepath.Join(root, "RecentClips"), map[string]string{"Front": b})

	svc := newTestRetention(t, db, root, cfgPath, RetentionConfig{
		Enabled: true, DryRun: false, MaxAgeDays: 90, MaxSizeGB: 0, MinFileAgeMinutes: 0,
	})
	report, err := svc.Run()
	if err != nil {
		t.Fatalf("run: %v", err)
	}
	if report.BytesFreed != 4096 {
		t.Fatalf("bytes_freed=%d want 4096 (only last link frees space)", report.BytesFreed)
	}
	if _, err := os.Stat(a); !os.IsNotExist(err) {
		t.Fatalf("path a should be gone")
	}
	if _, err := os.Stat(b); !os.IsNotExist(err) {
		t.Fatalf("path b should be gone")
	}
	var n int
	db.Model(&models.VideoFile{}).Count(&n)
	if n != 0 {
		t.Fatalf("all videofile rows should be gone, got %d", n)
	}
	db.Model(&models.Clip{}).Where("id IN (?)", []uint{clipA.ID, clipB.ID}).Count(&n)
	if n != 0 {
		t.Fatalf("clips should be gone, got %d", n)
	}
}

// Only the first hardlink name is selected; second path must keep its DB row and file.
func TestRetentionHardlinkPartialKeepsOtherRow(t *testing.T) {
	db := setupRetentionTestDB(t)
	root := t.TempDir()
	cfgPath := t.TempDir()
	now := time.Date(2026, 10, 14, 12, 0, 0, 0, time.UTC)

	a := filepath.Join(root, "RecentClips", "a.mp4")
	b := filepath.Join(root, "RecentClips", "b.mp4")
	writeFixtureFile(t, a, 4096, now.Add(-time.Hour))
	if err := os.Link(a, b); err != nil {
		t.Fatalf("link: %v", err)
	}
	oldA := seedClip(t, db, "Recent", now.AddDate(0, 0, -200), false, filepath.Join(root, "RecentClips"), map[string]string{"Front": a})
	keepB := seedClip(t, db, "Recent", now.AddDate(0, 0, -5), false, filepath.Join(root, "RecentClips"), map[string]string{"Front": b})

	svc := newTestRetention(t, db, root, cfgPath, RetentionConfig{
		Enabled: true, DryRun: false, MaxAgeDays: 90, MaxSizeGB: 0, MinFileAgeMinutes: 0,
	})
	report, err := svc.Run()
	if err != nil {
		t.Fatalf("run: %v", err)
	}
	// First unlink of a hardlink frees 0 bytes; b remains.
	if report.BytesFreed != 0 {
		t.Fatalf("bytes_freed=%d want 0 when a non-last hardlink is removed", report.BytesFreed)
	}
	if _, err := os.Stat(b); err != nil {
		t.Fatalf("remaining hardlink path must survive: %v", err)
	}
	var live int
	db.Model(&models.Clip{}).Where("id = ?", keepB.ID).Count(&live)
	if live != 1 {
		t.Fatalf("clip B must remain, live=%d", live)
	}
	db.Model(&models.VideoFile{}).Where("clip_id = ?", keepB.ID).Count(&live)
	if live != 1 {
		t.Fatalf("videofile B must remain, live=%d", live)
	}
	db.Model(&models.Clip{}).Where("id = ?", oldA.ID).Count(&live)
	if live != 0 {
		t.Fatalf("clip A should be gone")
	}
}

// Codex P2: crash after last video row hard-delete leaves soft-deleted clip/telemetry;
// next run must recover them.
func TestRetentionRecoversOrphanSoftDeletedClip(t *testing.T) {
	db := setupRetentionTestDB(t)
	root := t.TempDir()
	cfgPath := t.TempDir()
	now := time.Date(2026, 10, 14, 12, 0, 0, 0, time.UTC)
	old := now.AddDate(0, 0, -200)
	path := filepath.Join(root, "RecentClips", "gone.mp4")
	writeFixtureFile(t, path, 1024, now.Add(-time.Hour))
	clip := seedClip(t, db, "Recent", old, false, filepath.Join(root, "RecentClips"), map[string]string{"Front": path})

	// Simulate: files unlinked + VF hard-deleted, retention-marked clip+telemetry still soft-deleted.
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
	if err := db.Unscoped().Where("clip_id = ?", clip.ID).First(&tel).Error; err == nil {
		_ = db.Unscoped().Model(&tel).Update("deleted_at", time.Now()).Error
	}

	svc := newTestRetention(t, db, root, cfgPath, RetentionConfig{
		Enabled: true, DryRun: false, MaxAgeDays: 90, MaxSizeGB: 0, MinFileAgeMinutes: 0,
	})
	if _, err := svc.Run(); err != nil {
		t.Fatalf("run: %v", err)
	}
	var n int
	db.Unscoped().Model(&models.Clip{}).Where("id = ?", clip.ID).Count(&n)
	if n != 0 {
		t.Fatalf("orphan soft-deleted clip must be hard-deleted, count=%d", n)
	}
	db.Unscoped().Model(&models.Telemetry{}).Where("clip_id = ?", clip.ID).Count(&n)
	if n != 0 {
		t.Fatalf("orphan telemetry must be hard-deleted, count=%d", n)
	}
}

// Codex P2: scanner paused/held during prune must not recreate rows for deleted files.
func TestRetentionScannerPauseDoesNotResurrect(t *testing.T) {
	// File-backed sqlite: :memory: is not safe across concurrent connections.
	dirDB := t.TempDir()
	db, err := gorm.Open("sqlite3", filepath.Join(dirDB, "test.db"))
	if err != nil {
		t.Fatalf("db: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })
	if err := db.AutoMigrate(&models.Clip{}, &models.VideoFile{}, &models.Telemetry{}).Error; err != nil {
		t.Fatalf("migrate: %v", err)
	}

	root := t.TempDir()
	cfgPath := t.TempDir()
	now := time.Date(2026, 10, 14, 12, 0, 0, 0, time.UTC)
	old := now.AddDate(0, 0, -200)

	dir := filepath.Join(root, "SentryClips", "2024-01-01_00-00-00")
	path := filepath.Join(dir, "2024-01-01_00-00-00-front.mp4")
	writeFixtureFile(t, path, 1024, now.Add(-time.Hour))
	_ = os.WriteFile(filepath.Join(dir, "event.json"), []byte(`{"timestamp":"2024-01-01T00:00:00","city":"X","reason":"test"}`), 0644)
	_ = seedClip(t, db, "Sentry", old, false, dir, map[string]string{"Front": path})

	svc := newTestRetention(t, db, root, cfgPath, RetentionConfig{
		Enabled: true, DryRun: false, MaxAgeDays: 90, MaxSizeGB: 0, MinFileAgeMinutes: 0,
	})
	scanner := NewScannerService(root, db)

	var wg sync.WaitGroup
	svc.unlinkFn = func(fr *footageRoot, p string) (int64, bool, error) {
		// Prune holds media write lock here; scanner must block until unlink returns.
		wg.Add(1)
		go func() {
			defer wg.Done()
			scanner.processEventGroup(dir, []string{path})
		}()
		time.Sleep(30 * time.Millisecond)
		return fr.unlink(p)
	}

	report, err := svc.Run()
	if err != nil {
		t.Fatalf("run: %v", err)
	}
	wg.Wait()

	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Fatalf("file should be deleted, err=%v report=%+v", err, report)
	}
	var live int
	db.Model(&models.Clip{}).Count(&live)
	if live != 0 {
		t.Fatalf("scanner must not resurrect clip rows, live=%d", live)
	}
	db.Model(&models.VideoFile{}).Count(&live)
	if live != 0 {
		t.Fatalf("scanner must not resurrect videofile rows, live=%d", live)
	}
}
