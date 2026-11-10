package services

import (
 "os"
 "path/filepath"
 "testing"
 "time"
)

func TestRetentionSchedulerDisabledDryRunDoesNoWork(t *testing.T) {
 db := setupRetentionTestDB(t)
 root, configPath := t.TempDir(), t.TempDir()
 svc := newTestRetention(t, db, root, configPath, RetentionConfig{Enabled: false, DryRun: true, MaxAgeDays: 90, IntervalHours: 24})
 path := filepath.Join(root, "RecentClips", "old.mp4")
 writeFixtureFile(t, path, 1024, time.Now().Add(-time.Hour))
 seedClip(t, db, "Recent", time.Now().AddDate(-1, 0, 0), false, filepath.Dir(path), map[string]string{"Front": path})
 walks := 0
 svc.resolveFn = func(root *footageRoot, path string) (string, os.FileInfo, error) {
  walks++
  return root.resolve(path)
 }
 svc.runScheduled("startup")
 if walks != 0 { t.Errorf("disabled scheduler resolved %d files", walks) }
 if svc.Status().LastRun != nil { t.Error("disabled scheduler recorded a run") }
 if svc.Status().NextRun != nil { t.Error("disabled scheduler advertises a next run") }
 if _, err := os.Stat(filepath.Join(configPath, retentionStatusFileName)); !os.IsNotExist(err) { t.Errorf("disabled scheduler wrote history: %v", err) }
}
