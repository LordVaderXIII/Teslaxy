package services

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"teslaxy/models"
)

// TestRCReconcileRetentionBoundary: retention recovery must not unlink restored
// footage that only has a reconcile tombstone (no retention_pending marker).
func TestRCReconcileRetentionBoundary(t *testing.T) {
	db := setupRetentionTestDB(t)
	root := t.TempDir()
	cfgPath := t.TempDir()
	now := time.Date(2026, 10, 14, 12, 0, 0, 0, time.UTC)
	mtime := now.Add(-time.Hour) // 1h-old restored files

	cases := []struct {
		name  string
		event string
		dir   string
		file  string
	}{
		{"sentry", "Sentry", filepath.Join(root, "SentryClips", "2024-01-01_00-00-00"), "2024-01-01_00-00-00-front.mp4"},
		{"recent", "Recent", filepath.Join(root, "RecentClips"), "2024-01-01_01-00-00-front.mp4"},
	}

	var clips []models.Clip
	for _, tc := range cases {
		path := filepath.Join(tc.dir, tc.file)
		writeFixtureFile(t, path, 2048, mtime)
		if tc.event == "Sentry" {
			_ = os.WriteFile(filepath.Join(tc.dir, "event.json"), []byte(`{"timestamp":"2024-01-01T00:00:00","city":"X","reason":"test"}`), 0644)
		}
		clip := seedClip(t, db, tc.event, now.AddDate(0, 0, -30), false, tc.dir, map[string]string{"Front": path})
		// Reconcile-only tombstone: soft-delete WITHOUT retention_pending.
		var vfs []models.VideoFile
		db.Where("clip_id = ?", clip.ID).Find(&vfs)
		for i := range vfs {
			if err := db.Delete(&vfs[i]).Error; err != nil {
				t.Fatalf("%s soft vf: %v", tc.name, err)
			}
		}
		if err := db.Delete(&clip).Error; err != nil {
			t.Fatalf("%s soft clip: %v", tc.name, err)
		}
		clips = append(clips, clip)
	}

	svc := newTestRetention(t, db, root, cfgPath, RetentionConfig{
		Enabled: true, DryRun: false, MaxAgeDays: 90, MaxSizeGB: 0, MinFileAgeMinutes: 0,
	})
	report, err := svc.Run()
	if err != nil {
		t.Fatalf("retention run: %v", err)
	}

	for i, tc := range cases {
		path := filepath.Join(tc.dir, tc.file)
		if _, err := os.Stat(path); err != nil {
			t.Fatalf("%s restored 1h-old file must survive retention recovery: %v report=%+v", tc.name, err, report)
		}
		var n int
		db.Unscoped().Model(&models.VideoFile{}).Where("clip_id = ?", clips[i].ID).Count(&n)
		if n == 0 {
			t.Fatalf("%s video row must not be hard-deleted by retention", tc.name)
		}
	}
}

// TestRCScannerDeadlock: ScanAll holds the media RLock while indexing event
// dirs; a queued writer (ReconcileMissing) must not deadlock nested RLock in
// processEventGroup. Completes within 1s after the indexer finishes.
func TestRCScannerDeadlock(t *testing.T) {
	os.Setenv("DEFAULT_TIMEZONE", "UTC")
	defer os.Unsetenv("DEFAULT_TIMEZONE")

	db := setupFableADB(t)
	defer db.Close()

	tmpDir := t.TempDir()
	dir := filepath.Join(tmpDir, "SentryClips", "2024-03-10_14-00-00")
	writeDummyMP4(t, filepath.Join(dir, "2024-03-10_14-00-00-front.mp4"))
	_ = os.WriteFile(filepath.Join(dir, "event.json"), []byte(`{"timestamp":"2024-03-10T14:00:10","city":"X","reason":"test"}`), 0644)

	entered := make(chan struct{})
	release := make(chan struct{})
	var once sync.Once
	mediaReadLockedHook = func() {
		once.Do(func() { close(entered) })
		<-release
	}
	defer func() { mediaReadLockedHook = nil }()

	scanner := NewScannerService(tmpDir, db)
	scanDone := make(chan struct{})
	go func() {
		scanner.ScanAll()
		close(scanDone)
	}()

	select {
	case <-entered:
	case <-time.After(2 * time.Second):
		t.Fatal("ScanAll did not acquire media read lock")
	}

	// Queue a writer while ScanAll (and its workers) hold/need the read lock.
	recDone := make(chan error, 1)
	go func() {
		_, err := ReconcileMissing(db, tmpDir, ReconcileOpts{StateDir: t.TempDir()})
		recDone <- err
	}()
	time.Sleep(100 * time.Millisecond) // let Lock() queue behind readers

	close(release)

	select {
	case <-scanDone:
	case <-time.After(1 * time.Second):
		t.Fatal("ScanAll deadlocked with queued writer (nested RLock)")
	}
	select {
	case <-recDone:
	case <-time.After(1 * time.Second):
		t.Fatal("ReconcileMissing did not finish within 1s after ScanAll")
	}
}

// TestRCRestartConfirmationIgnoresPersistedTime: after a simulated restart,
// backdated mtime / JSON (+31s, +1h) must not authorize soft-delete.
func TestRCRestartConfirmationIgnoresPersistedTime(t *testing.T) {
	db := setupFableADB(t)
	defer db.Close()
	stateDir := testStateDir(t)
	tmpDir := t.TempDir()

	videoPath := filepath.Join(tmpDir, "gone-front.mp4")
	clip := models.Clip{Event: "Sentry", SourceDir: tmpDir}
	db.Create(&clip)
	db.Create(&models.VideoFile{ClipID: clip.ID, Camera: "Front", FilePath: videoPath})

	if _, err := ReconcileMissing(db, tmpDir, reconcileOpts(stateDir)); !errors.Is(err, ErrReconcilePendingConfirm) {
		t.Fatalf("first: %v", err)
	}
	path := filepath.Join(stateDir, pendingPurgeFileName)

	for _, ago := range []time.Duration{31 * time.Second, time.Hour} {
		t.Run(ago.String(), func(t *testing.T) {
			resetPendingObservationsForTest()

			raw, err := os.ReadFile(path)
			if err != nil {
				t.Fatal(err)
			}
			var st pendingPurgeState
			if err := json.Unmarshal(raw, &st); err != nil {
				t.Fatal(err)
			}
			when := time.Now().UTC().Add(-ago)
			st.FirstSeen = when
			body, err := json.MarshalIndent(&st, "", "  ")
			if err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(path, body, 0644); err != nil {
				t.Fatal(err)
			}
			if err := os.Chtimes(path, when, when); err != nil {
				t.Fatal(err)
			}

			res, err := ReconcileMissing(db, tmpDir, reconcileOpts(stateDir))
			var live int
			db.Model(&models.VideoFile{}).Count(&live)
			if live != 1 {
				t.Fatalf("restart + backdated %s deleted rows: live=%d res=%+v err=%v", ago, live, res, err)
			}
			if !errors.Is(err, ErrReconcilePendingConfirm) {
				t.Fatalf("want fresh pending after restart, got %v", err)
			}
		})
	}

	t.Run("mtime-only", func(t *testing.T) {
		resetPendingObservationsForTest()
		when := time.Now().Add(-time.Hour)
		if err := os.Chtimes(path, when, when); err != nil {
			t.Fatal(err)
		}
		res, err := ReconcileMissing(db, tmpDir, reconcileOpts(stateDir))
		var live int
		db.Model(&models.VideoFile{}).Count(&live)
		if live != 1 {
			t.Fatalf("mtime-only restart deleted: live=%d res=%+v err=%v", live, res, err)
		}
		if !errors.Is(err, ErrReconcilePendingConfirm) {
			t.Fatalf("want pending, got %v", err)
		}
	})
}

// TestRCNaturalReplayAndClear: failed clear + restart must not reuse a mature
// confirmation; rows stay live.
func TestRCNaturalReplayAndClear(t *testing.T) {
	db := setupFableADB(t)
	defer db.Close()

	tmpDir := t.TempDir()
	stateParent := t.TempDir()
	stateDir := filepath.Join(stateParent, "cfg")
	if err := os.MkdirAll(stateDir, 0755); err != nil {
		t.Fatal(err)
	}

	saved := filepath.Join(tmpDir, "SavedClips", "evt")
	videoPath := filepath.Join(saved, "2024-09-02_12-00-00-front.mp4")
	writeDummyMP4(t, videoPath)
	clip := models.Clip{Event: "Saved", SourceDir: saved}
	db.Create(&clip)
	db.Create(&models.VideoFile{ClipID: clip.ID, Camera: "Front", FilePath: videoPath})

	aside := filepath.Join(tmpDir, "SavedClips.aside")
	if err := os.Rename(filepath.Join(tmpDir, "SavedClips"), aside); err != nil {
		t.Fatal(err)
	}
	if _, err := ReconcileMissing(db, tmpDir, reconcileOpts(stateDir)); !errors.Is(err, ErrReconcilePendingConfirm) {
		t.Fatalf("first: %v", err)
	}
	if err := agePendingPurgeForTest(stateDir, tmpDir, reconcileConfirmMinInterval+time.Second); err != nil {
		t.Fatal(err)
	}

	// Restore footage, then make state dir unwritable so clear fails.
	if err := os.Rename(aside, filepath.Join(tmpDir, "SavedClips")); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(stateDir, 0500); err != nil {
		t.Fatal(err)
	}
	defer os.Chmod(stateDir, 0755)

	_, err := ReconcileMissing(db, tmpDir, reconcileOpts(stateDir))
	if err == nil {
		t.Fatal("healthy pass with unwritable state must surface clear failure")
	}

	// Restart variant: drop in-memory blocks; mature pending may remain on disk.
	resetPendingObservationsForTest()
	if err := os.Rename(filepath.Join(tmpDir, "SavedClips"), aside); err != nil {
		t.Fatal(err)
	}
	_ = os.Chmod(stateDir, 0755)

	res, err := ReconcileMissing(db, tmpDir, reconcileOpts(stateDir))
	var liveFiles, liveClips int
	db.Model(&models.VideoFile{}).Count(&liveFiles)
	db.Model(&models.Clip{}).Count(&liveClips)
	if liveFiles != 1 || liveClips != 1 {
		t.Fatalf("restart replay deleted rows: files=%d clips=%d res=%+v err=%v", liveFiles, liveClips, res, err)
	}
	if err == nil {
		t.Fatal("expected error or pending after failed-clear restart; got nil")
	}
}

// TestRCBatchNearMiss: EACCES, ELOOP, and late restore must not soft-delete.
func TestRCBatchNearMiss(t *testing.T) {
	t.Run("eacces", func(t *testing.T) {
		db := setupFableADB(t)
		defer db.Close()
		stateDir := testStateDir(t)
		tmpDir := t.TempDir()

		locked := filepath.Join(tmpDir, "locked")
		if err := os.MkdirAll(locked, 0755); err != nil {
			t.Fatal(err)
		}
		videoPath := filepath.Join(locked, "2024-01-01_00-00-00-front.mp4")
		writeDummyMP4(t, videoPath)
		clip := models.Clip{Event: "Sentry", SourceDir: locked}
		db.Create(&clip)
		db.Create(&models.VideoFile{ClipID: clip.ID, Camera: "Front", FilePath: videoPath})

		aside := filepath.Join(tmpDir, "aside-front.mp4")
		if err := os.Rename(videoPath, aside); err != nil {
			t.Fatal(err)
		}
		// Collect sees ENOENT. Before delete, lock parent so Stat → EACCES.
		reconcileBeforeDeleteBatch = func() {
			_ = os.Chmod(locked, 0000)
		}
		defer func() {
			reconcileBeforeDeleteBatch = nil
			_ = os.Chmod(locked, 0755)
		}()

		res, err := ReconcileMissing(db, tmpDir, ReconcileOpts{ForceEmptyPurge: true, StateDir: stateDir})
		_ = os.Chmod(locked, 0755)
		if err != nil {
			t.Fatalf("force purge: %v", err)
		}
		if res.SoftDeletedFiles != 0 {
			t.Fatalf("EACCES must not soft-delete: %+v", res)
		}
		var live int
		db.Model(&models.VideoFile{}).Count(&live)
		if live != 1 {
			t.Fatalf("live files=%d want 1", live)
		}
	})

	t.Run("eloop", func(t *testing.T) {
		db := setupFableADB(t)
		defer db.Close()
		stateDir := testStateDir(t)
		tmpDir := t.TempDir()

		videoPath := filepath.Join(tmpDir, "loop-front.mp4")
		clip := models.Clip{Event: "Sentry", SourceDir: tmpDir}
		db.Create(&clip)
		db.Create(&models.VideoFile{ClipID: clip.ID, Camera: "Front", FilePath: videoPath})

		// Path missing at collect (ENOENT). Before delete, plant a symlink loop.
		reconcileBeforeDeleteBatch = func() {
			_ = os.Remove(videoPath)
			_ = os.Symlink("loop-front.mp4", videoPath)
		}
		defer func() { reconcileBeforeDeleteBatch = nil }()

		res, err := ReconcileMissing(db, tmpDir, ReconcileOpts{ForceEmptyPurge: true, StateDir: stateDir})
		_ = os.Remove(videoPath)
		if err != nil {
			t.Fatalf("force purge: %v", err)
		}
		if res.SoftDeletedFiles != 0 {
			t.Fatalf("ELOOP must not soft-delete: %+v", res)
		}
		var live int
		db.Model(&models.VideoFile{}).Count(&live)
		if live != 1 {
			t.Fatalf("live files=%d want 1", live)
		}
	})

	t.Run("late-restore", func(t *testing.T) {
		db := setupFableADB(t)
		defer db.Close()
		stateDir := testStateDir(t)
		tmpDir := t.TempDir()

		videoPath := filepath.Join(tmpDir, "2024-01-01_00-00-00-front.mp4")
		writeDummyMP4(t, videoPath)
		clip := models.Clip{Event: "Sentry", SourceDir: tmpDir}
		db.Create(&clip)
		db.Create(&models.VideoFile{ClipID: clip.ID, Camera: "Front", FilePath: videoPath})

		aside := videoPath + ".aside"
		if err := os.Rename(videoPath, aside); err != nil {
			t.Fatal(err)
		}
		reconcileBeforeDeleteBatch = func() {
			_ = os.Rename(aside, videoPath)
		}
		defer func() { reconcileBeforeDeleteBatch = nil }()

		res, err := ReconcileMissing(db, tmpDir, ReconcileOpts{ForceEmptyPurge: true, StateDir: stateDir})
		if err != nil {
			t.Fatalf("force purge: %v", err)
		}
		if res.SoftDeletedFiles != 0 {
			t.Fatalf("late restore must not soft-delete: %+v", res)
		}
		var live int
		db.Model(&models.VideoFile{}).Count(&live)
		if live != 1 {
			t.Fatalf("live files=%d want 1", live)
		}
		if _, err := os.Stat(videoPath); err != nil {
			t.Fatalf("restored file missing: %v", err)
		}
	})
}

// TestRCRestoreRescan: reconcile tombstone + restored file is cleared by rescan.
func TestRCRestoreRescan(t *testing.T) {
	os.Setenv("DEFAULT_TIMEZONE", "UTC")
	defer os.Unsetenv("DEFAULT_TIMEZONE")

	db := setupFableADB(t)
	defer db.Close()
	stateDir := testStateDir(t)
	tmpDir := t.TempDir()

	dir := filepath.Join(tmpDir, "SentryClips", "2024-05-01_10-00-00")
	videoPath := filepath.Join(dir, "2024-05-01_10-00-00-front.mp4")
	writeDummyMP4(t, videoPath)
	_ = os.WriteFile(filepath.Join(dir, "event.json"), []byte(`{"timestamp":"2024-05-01T10:00:10","city":"X","reason":"test"}`), 0644)

	os.Setenv("CONFIG_PATH", stateDir)
	defer os.Unsetenv("CONFIG_PATH")
	NewScannerService(tmpDir, db).ScanAll()

	var live int
	db.Model(&models.VideoFile{}).Count(&live)
	if live != 1 {
		t.Fatalf("setup live=%d", live)
	}

	aside := dir + ".aside"
	if err := os.Rename(dir, aside); err != nil {
		t.Fatal(err)
	}
	if _, err := ReconcileMissing(db, tmpDir, ReconcileOpts{ForceEmptyPurge: true, StateDir: stateDir}); err != nil {
		t.Fatalf("purge: %v", err)
	}
	db.Model(&models.VideoFile{}).Count(&live)
	if live != 0 {
		t.Fatalf("want tombstone, live=%d", live)
	}

	if err := os.Rename(aside, dir); err != nil {
		t.Fatal(err)
	}
	NewScannerService(tmpDir, db).ScanAll()
	db.Model(&models.VideoFile{}).Count(&live)
	if live != 1 {
		t.Fatalf("rescan must clear reconcile tombstone, live=%d", live)
	}
	db.Model(&models.Clip{}).Count(&live)
	if live != 1 {
		t.Fatalf("rescan must restore clip, live=%d", live)
	}
}

// TestRCPendingMapRace: concurrent forget/remember under -race must stay clean.
func TestRCPendingMapRace(t *testing.T) {
	const root = "/tmp/teslaxy-race-root"
	var wg sync.WaitGroup
	var ops atomic.Int64
	for i := 0; i < 8; i++ {
		wg.Add(1)
		go func(id int) {
			defer wg.Done()
			fp := fmt.Sprintf("fp-%d", id)
			for j := 0; j < 200; j++ {
				rememberPendingObservation(root, fp, time.Now(), uint64(id+1))
				forgetPendingObservation(root, fp)
				forgetPendingObservation(root, "")
				ops.Add(1)
			}
		}(i)
	}
	wg.Wait()
	if ops.Load() == 0 {
		t.Fatal("no ops")
	}
	resetPendingObservationsForTest()
}
