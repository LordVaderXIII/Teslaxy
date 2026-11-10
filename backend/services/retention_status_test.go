package services

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"golang.org/x/sys/unix"
)

func TestRetentionDisabledSafetyAndPersistedSummary(t *testing.T) {
	for _, dryRun := range []bool{false, true} {
		t.Run(map[bool]string{false: "disabled-noop", true: "disabled-dry-run"}[dryRun], func(t *testing.T) {
			db := setupRetentionTestDB(t)
			root := t.TempDir()
			configPath := t.TempDir()
			now := time.Date(2026, 10, 14, 12, 0, 0, 0, time.UTC)
			path := filepath.Join(root, "RecentClips", "old.mp4")
			writeFixtureFile(t, path, 4096, now.Add(-time.Hour))
			seedClip(t, db, "Recent", now.AddDate(0, 0, -200), false, filepath.Dir(path), map[string]string{"Front": path})
			svc := newTestRetention(t, db, root, configPath, RetentionConfig{Enabled: false, DryRun: dryRun, MaxAgeDays: 90, IntervalHours: 24})
			report, err := svc.RunWithConfig(svc.Config())
			if err != nil {
				t.Fatal(err)
			}
			want := 0
			if dryRun {
				want = 1
			}
			if len(report.Candidates) != want || len(report.Deleted) != 0 {
				t.Fatalf("unsafe report: %+v", report)
			}
			if _, err := os.Stat(path); err != nil {
				t.Fatal(err)
			}
			restarted := NewRetentionService(db, root, configPath)
			status := restarted.Status()
			if status.LastRun == nil || status.LastRun.CandidateCount != want {
				t.Fatalf("lost last run: %+v", status)
			}
			if dryRun {
				if status.LastDryRun == nil || status.LastDryRun.CandidateBytes != 4096 || status.LastDryRun.CandidateGiB != 4096.0/(1<<30) {
					t.Fatalf("lost summary: %+v", status)
				}
				if status.NextRun == nil || !status.NextRun.Equal(report.FinishedAt.Add(24*time.Hour)) {
					t.Fatalf("wrong next run: %+v", status)
				}
				cfg := svc.Config()
				cfg.DryRun = false
				if _, err := svc.RunWithConfig(cfg); err != nil {
					t.Fatal(err)
				}
				restarted = NewRetentionService(db, root, configPath)
				if restarted.Status().LastDryRun.CandidateBytes != 4096 {
					t.Fatal("non-dry run erased dry-run history")
				}
			} else if status.NextRun != nil {
				t.Fatal("disabled non-dry service scheduled deletes")
			}
		})
	}
}

func TestRetentionStorageFixtureAndFakeStatfs(t *testing.T) {
	root := t.TempDir()
	configPath := t.TempDir()
	for dir, size := range map[string]int{"SentryClips": 100, "RecentClips": 200, "SavedClips": 300} {
		writeFixtureFile(t, filepath.Join(root, dir, "event", "front.mp4"), size, time.Now())
	}
	outside := t.TempDir()
	writeFixtureFile(t, filepath.Join(outside, "outside.mp4"), 1000, time.Now())
	if err := os.Symlink(outside, filepath.Join(root, "SentryClips", "external")); err != nil {
		t.Fatal(err)
	}
	svc := NewRetentionService(nil, root, configPath)
	svc.statfsFn = func(path string, stat *unix.Statfs_t) error {
		if path != root {
			t.Fatalf("statfs escaped fixture: %s", path)
		}
		stat.Blocks = 100
		stat.Bfree = 40
		stat.Bavail = 30
		stat.Bsize = 1024
		return nil
	}
	storage, err := svc.Storage()
	if err != nil {
		t.Fatal(err)
	}
	if storage.ByTypeBytes["Sentry"] != 100 || storage.ByTypeBytes["Recent"] != 200 || storage.ByTypeBytes["Saved"] != 300 {
		t.Fatalf("wrong types/symlink traversal: %+v", storage)
	}
	if storage.VolumeUsedBytes != 60*1024 || storage.VolumeFreeBytes != 40*1024 || storage.VolumeTotalBytes != 100*1024 {
		t.Fatalf("wrong volume: %+v", storage)
	}
}

func TestRetentionStatusPersistenceFailureIsReported(t *testing.T) {
	root := t.TempDir()
	configPath := t.TempDir()
	svc := NewRetentionService(nil, root, configPath)
	if err := os.WriteFile(filepath.Join(configPath, "retention-status.json"), []byte("not-json"), 0600); err != nil {
		t.Fatal(err)
	}
	svc = NewRetentionService(nil, root, configPath)
	if svc.Status().LastRun != nil {
		t.Fatal("corrupt history became a fabricated run")
	}
	// An existing directory at the destination deterministically makes rename fail.
	if err := os.Remove(filepath.Join(configPath, "retention-status.json")); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(filepath.Join(configPath, "retention-status.json"), 0700); err != nil {
		t.Fatal(err)
	}
	report, err := svc.RunWithConfig(RetentionConfig{Enabled: false, DryRun: false})
	if err == nil || len(report.Errors) == 0 {
		t.Fatal("history persistence error silently swallowed")
	}
}
