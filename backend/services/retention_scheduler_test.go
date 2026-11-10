package services

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/jinzhu/gorm"
)

func TestRetentionSchedulerTogglesWithoutRestart(t *testing.T) {
	db := setupRetentionTestDB(t)
	root, configPath := t.TempDir(), t.TempDir()
	svc := newTestRetention(t, db, root, configPath, RetentionConfig{Enabled: false, DryRun: true, MaxAgeDays: 90, IntervalHours: 24})
	path := filepath.Join(root, "RecentClips", "old.mp4")
	writeFixtureFile(t, path, 1024, time.Now().Add(-time.Hour))
	seedClip(t, db, "Recent", time.Now().AddDate(-1, 0, 0), false, filepath.Dir(path), map[string]string{"Front": path})
	svc.runScheduled("startup")
	if svc.Status().LastRun != nil {
		t.Fatal("disabled startup recorded a run")
	}
	cfg := svc.Config()
	cfg.Enabled = true
	if err := svc.SetConfig(cfg); err != nil {
		t.Fatal(err)
	}
	svc.runScheduled("interval")
	enabled := svc.Status()
	if enabled.LastRun == nil || enabled.LastRun.CandidateCount != 1 || enabled.NextRun == nil {
		t.Fatalf("enable did not schedule dry-run: %+v", enabled)
	}
	history, err := os.ReadFile(filepath.Join(configPath, retentionStatusFileName))
	if err != nil {
		t.Fatal(err)
	}
	cfg.Enabled = false
	if err := svc.SetConfig(cfg); err != nil {
		t.Fatal(err)
	}
	svc.nowFn = func() time.Time { return time.Now().Add(time.Hour) }
	svc.runScheduled("interval")
	disabled := svc.Status()
	if disabled.NextRun != nil {
		t.Fatal("disable still schedules a run")
	}
	if disabled.LastRun == nil || disabled.LastDryRun == nil || !disabled.LastRun.FinishedAt.Equal(enabled.LastRun.FinishedAt) {
		t.Fatal("disable changed history")
	}
	after, err := os.ReadFile(filepath.Join(configPath, retentionStatusFileName))
	if err != nil || string(after) != string(history) {
		t.Fatalf("disabled tick changed persisted history: %v", err)
	}
	if _, err := os.Stat(path); err != nil {
		t.Fatal(err)
	}
}

func TestRetentionSchedulerDisabledDryRunDoesNoWork(t *testing.T) {
	for _, dryRun := range []bool{false, true} {
		t.Run(map[bool]string{false: "live-mode", true: "dry-run"}[dryRun], func(t *testing.T) {
			db := setupRetentionTestDB(t)
			root, configPath := t.TempDir(), t.TempDir()
			svc := newTestRetention(t, db, root, configPath, RetentionConfig{Enabled: false, DryRun: dryRun, MaxAgeDays: 90, IntervalHours: 24})
			path := filepath.Join(root, "RecentClips", "old.mp4")
			writeFixtureFile(t, path, 1024, time.Now().Add(-time.Hour))
			seedClip(t, db, "Recent", time.Now().AddDate(-1, 0, 0), false, filepath.Dir(path), map[string]string{"Front": path})
			walks := 0
			queries := 0
			db.Callback().Query().Before("gorm:query").Register("test:count_retention_queries", func(*gorm.Scope) {
				queries++
			})
			svc.resolveFn = func(root *footageRoot, path string) (string, os.FileInfo, error) {
				walks++
				return root.resolve(path)
			}
			svc.runScheduled("startup")
			svc.runScheduled("interval")
			if walks != 0 {
				t.Errorf("disabled scheduler resolved %d files", walks)
			}
			if queries != 0 {
				t.Errorf("disabled scheduler made %d database queries", queries)
			}
			if svc.Status().LastRun != nil {
				t.Error("disabled scheduler recorded a run")
			}
			if svc.Status().NextRun != nil {
				t.Error("disabled scheduler advertises a next run")
			}
			if _, err := os.Stat(filepath.Join(configPath, retentionStatusFileName)); !os.IsNotExist(err) {
				t.Errorf("disabled scheduler wrote history: %v", err)
			}
		})
	}
}

func TestRetentionSchedulerRechecksDisabledConfigBeforeWork(t *testing.T) {
	// Simulate a disable after the scheduler took an enabled config snapshot.
	// A nil DB and missing root also catch accidental database/filesystem work.
	svc := NewRetentionService(nil, filepath.Join(t.TempDir(), "missing"), t.TempDir())
	cfg := svc.Config()
	cfg.Enabled = false
	if err := svc.SetConfig(cfg); err != nil {
		t.Fatal(err)
	}
	stale := cfg
	stale.Enabled = true
	report, err := svc.runWithConfig(stale, true)
	if err != nil || report != nil || svc.Status().LastRun != nil {
		t.Fatalf("scheduled work used stale enabled config: report=%v err=%v", report, err)
	}
}
