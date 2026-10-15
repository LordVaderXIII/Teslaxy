package services

import (
	"errors"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/jinzhu/gorm"

	"teslaxy/models"
)

// restoredProtectionCase describes one footage layout for the Codex round-2
// X4/X6 repro: index, protect, lose, tombstone, restore, rescan, retention.
type restoredProtectionCase struct {
	name      string
	dir       func(root string) string
	file      string
	eventJSON bool
}

var restoredProtectionCases = []restoredProtectionCase{
	{
		name:      "Sentry",
		dir:       func(root string) string { return filepath.Join(root, "SentryClips", "2024-01-01_00-00-00") },
		file:      "2024-01-01_00-00-00-front.mp4",
		eventJSON: true,
	},
	{
		name: "Recent",
		dir:  func(root string) string { return filepath.Join(root, "RecentClips") },
		file: "2024-01-01_01-00-00-front.mp4",
	},
}

func setupRestoredProtection(t *testing.T, tc restoredProtectionCase) (db *gorm.DB, root, stateDir, videoPath string, clip models.Clip) {
	t.Helper()
	os.Setenv("DEFAULT_TIMEZONE", "UTC")
	t.Cleanup(func() { os.Unsetenv("DEFAULT_TIMEZONE") })

	db = setupFableADB(t)
	t.Cleanup(func() { _ = db.Close() })
	root = t.TempDir()
	stateDir = testStateDir(t)
	os.Setenv("CONFIG_PATH", stateDir)
	t.Cleanup(func() { os.Unsetenv("CONFIG_PATH") })

	dir := tc.dir(root)
	videoPath = filepath.Join(dir, tc.file)
	writeDummyMP4(t, videoPath)
	if tc.eventJSON {
		if err := os.WriteFile(filepath.Join(dir, "event.json"), []byte(`{"timestamp":"2024-01-01T00:00:10","city":"X","reason":"test"}`), 0644); err != nil {
			t.Fatal(err)
		}
	}

	NewScannerService(root, db).ScanAll()

	var clips []models.Clip
	if err := db.Find(&clips).Error; err != nil {
		t.Fatal(err)
	}
	if len(clips) != 1 {
		t.Fatalf("setup: want 1 clip, got %d", len(clips))
	}
	clip = clips[0]
	if clip.Event != tc.name {
		t.Fatalf("setup: want event %s, got %s", tc.name, clip.Event)
	}
	if err := db.Model(&clip).Update("protected", true).Error; err != nil {
		t.Fatal(err)
	}
	return db, root, stateDir, videoPath, clip
}

// tombstoneViaTwoPassReconcile removes the file and runs the real pending ->
// confirm -> soft-delete reconcile path.
func tombstoneViaTwoPassReconcile(t *testing.T, db *gorm.DB, root, stateDir, videoPath string) {
	t.Helper()
	if err := os.Remove(videoPath); err != nil {
		t.Fatal(err)
	}
	if _, err := ReconcileMissing(db, root, reconcileOpts(stateDir)); !errors.Is(err, ErrReconcilePendingConfirm) {
		t.Fatalf("first pass: want pending confirm, got %v", err)
	}
	if err := agePendingPurgeForTest(stateDir, root, reconcileConfirmMinInterval+time.Second); err != nil {
		t.Fatal(err)
	}
	res, err := ReconcileMissing(db, root, reconcileOpts(stateDir))
	if err != nil {
		t.Fatalf("second pass: %v", err)
	}
	if res.SoftDeletedFiles != 1 || res.SoftDeletedClips != 1 {
		t.Fatalf("second pass: want 1 file + 1 clip tombstoned, got %+v", res)
	}
}

// assertRestoredKeepsIdentity rescans and runs retention; the original clip ID
// must come back live, still protected, and its file must survive retention.
func assertRestoredKeepsIdentity(t *testing.T, db *gorm.DB, root, videoPath string, orig models.Clip) {
	t.Helper()
	NewScannerService(root, db).ScanAll()

	var live []models.Clip
	if err := db.Find(&live).Error; err != nil {
		t.Fatal(err)
	}
	if len(live) != 1 {
		t.Fatalf("after rescan: want exactly 1 live clip, got %d (%+v)", len(live), live)
	}
	if live[0].ID != orig.ID {
		t.Fatalf("after rescan: restored footage must revive clip %d, got new clip %d (protected=%v)", orig.ID, live[0].ID, live[0].Protected)
	}
	if !live[0].Protected {
		t.Fatalf("after rescan: clip %d lost protection", live[0].ID)
	}
	var vf models.VideoFile
	if err := db.Where("file_path = ?", videoPath).First(&vf).Error; err != nil {
		t.Fatalf("after rescan: live video row missing: %v", err)
	}
	if vf.ClipID != orig.ID {
		t.Fatalf("after rescan: video row moved to clip %d, want %d", vf.ClipID, orig.ID)
	}
	var tombstones int
	db.Unscoped().Model(&models.Clip{}).Where("deleted_at IS NOT NULL").Count(&tombstones)
	if tombstones != 0 {
		t.Fatalf("after rescan: %d clip tombstone(s) left behind", tombstones)
	}

	cfgPath := t.TempDir()
	svc := newTestRetention(t, db, root, cfgPath, RetentionConfig{
		Enabled: true, DryRun: false, MaxAgeDays: 90, MaxSizeGB: 0, MinFileAgeMinutes: 0,
	})
	report, err := svc.Run()
	if err != nil {
		t.Fatalf("retention: %v", err)
	}
	if report.BytesFreed != 0 || len(report.Deleted) != 0 {
		t.Fatalf("retention pruned protected restored footage: bytes_freed=%d deleted=%+v", report.BytesFreed, report.Deleted)
	}
	if _, err := os.Stat(videoPath); err != nil {
		t.Fatalf("restored protected file was unlinked: %v", err)
	}
	var after models.Clip
	if err := db.First(&after, orig.ID).Error; err != nil {
		t.Fatalf("clip %d gone after retention: %v", orig.ID, err)
	}
	if !after.Protected {
		t.Fatalf("clip %d unprotected after retention", orig.ID)
	}
}

// TestReviewRestoredProtection (Codex round 2 X4): restoring tombstoned footage
// must revive the original clip and its protection; a fresh unprotected clip
// would let retention unlink the file.
func TestReviewRestoredProtection(t *testing.T) {
	for _, tc := range restoredProtectionCases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			db, root, stateDir, videoPath, clip := setupRestoredProtection(t, tc)
			tombstoneViaTwoPassReconcile(t, db, root, stateDir, videoPath)

			var liveFiles int
			db.Model(&models.VideoFile{}).Count(&liveFiles)
			if liveFiles != 0 {
				t.Fatalf("want tombstoned video, live=%d", liveFiles)
			}

			writeDummyMP4(t, videoPath)
			old := time.Now().Add(-time.Hour)
			if err := os.Chtimes(videoPath, old, old); err != nil {
				t.Fatal(err)
			}

			assertRestoredKeepsIdentity(t, db, root, videoPath, clip)
		})
	}
}

// TestReviewLateRestoreRescan (Codex round 1 partial / round 2 X6): a restore
// that lands after the final Stat still leaves a video + clip tombstone. The
// next rescan must clear both and keep the clip ID and protection.
func TestReviewLateRestoreRescan(t *testing.T) {
	for _, tc := range restoredProtectionCases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			db, root, stateDir, videoPath, clip := setupRestoredProtection(t, tc)

			if err := os.Remove(videoPath); err != nil {
				t.Fatal(err)
			}
			if _, err := ReconcileMissing(db, root, reconcileOpts(stateDir)); !errors.Is(err, ErrReconcilePendingConfirm) {
				t.Fatalf("first pass: %v", err)
			}
			if err := agePendingPurgeForTest(stateDir, root, reconcileConfirmMinInterval+time.Second); err != nil {
				t.Fatal(err)
			}

			// Restore after the video batch has already tombstoned the row:
			// the clip batch then sees zero live files and tombstones the clip.
			var calls int
			reconcileBeforeDeleteBatch = func() {
				calls++
				if calls == 2 {
					writeDummyMP4(t, videoPath)
					old := time.Now().Add(-time.Hour)
					_ = os.Chtimes(videoPath, old, old)
				}
			}
			defer func() { reconcileBeforeDeleteBatch = nil }()

			res, err := ReconcileMissing(db, root, reconcileOpts(stateDir))
			if err != nil {
				t.Fatalf("second pass: %v", err)
			}
			if res.SoftDeletedFiles != 1 || res.SoftDeletedClips != 1 {
				t.Fatalf("late restore should still tombstone 1 file + 1 clip, got %+v", res)
			}
			if _, err := os.Stat(videoPath); err != nil {
				t.Fatalf("restored file must exist on disk: %v", err)
			}
			reconcileBeforeDeleteBatch = nil

			assertRestoredKeepsIdentity(t, db, root, videoPath, clip)
		})
	}
}

// TestReviewCancelMapPublicAPIRace (Codex round 2 X5): after a real failed
// cancellation, concurrent ReconcileMissing + PendingConfirmRemaining must be
// clean under -race without any test touching pendingClearBlocked directly.
func TestReviewCancelMapPublicAPIRace(t *testing.T) {
	if os.Getuid() == 0 {
		t.Skip("chmod-based unwritable state dir is bypassed by root")
	}
	resetPendingObservationsForTest()
	t.Cleanup(resetPendingObservationsForTest)

	db := setupFableADB(t)
	defer db.Close()

	tmpDir := t.TempDir()
	stateDir := filepath.Join(t.TempDir(), "cfg")
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

	// Real failed cancellation: footage returns, state dir is unwritable.
	if err := os.Rename(aside, filepath.Join(tmpDir, "SavedClips")); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(stateDir, 0500); err != nil {
		t.Fatal(err)
	}
	defer os.Chmod(stateDir, 0755)
	if _, err := ReconcileMissing(db, tmpDir, reconcileOpts(stateDir)); err == nil {
		t.Fatal("healthy pass with unwritable state must fail the clear")
	}
	if err := os.Chmod(stateDir, 0755); err != nil {
		t.Fatal(err)
	}

	// New outage: every ReconcileMissing now reaches the canceled-state check
	// with candidates present instead of short-circuiting on a healthy root.
	if err := os.Rename(filepath.Join(tmpDir, "SavedClips"), aside); err != nil {
		t.Fatal(err)
	}

	// Public API only: readers of pendingClearBlocked (ReconcileMissing) race
	// against writers (PendingConfirmRemaining -> loadPendingPurge).
	// ReconcileMissing calls serialize on the media write lock and are slow
	// (stat + DB); PendingConfirmRemaining is fast, so its callers keep
	// polling until every reconcile pass has finished to keep the accesses
	// interleaved rather than all landing before the first read.
	const workers = 100
	var reconciles, pollers sync.WaitGroup
	reconcilesDone := make(chan struct{})
	reconciles.Add(workers)
	pollers.Add(workers)
	for i := 0; i < workers; i++ {
		go func() {
			defer reconciles.Done()
			_, _ = ReconcileMissing(db, tmpDir, reconcileOpts(stateDir))
		}()
		go func() {
			defer pollers.Done()
			for {
				_ = PendingConfirmRemaining(stateDir, tmpDir)
				select {
				case <-reconcilesDone:
					return
				default:
				}
			}
		}()
	}
	reconciles.Wait()
	close(reconcilesDone)
	pollers.Wait()

	// Canceled confirmation must still block reuse: nothing may be deleted.
	var liveFiles, liveClips int
	db.Model(&models.VideoFile{}).Count(&liveFiles)
	db.Model(&models.Clip{}).Count(&liveClips)
	if liveFiles != 1 || liveClips != 1 {
		t.Fatalf("canceled confirmation reused: files=%d clips=%d", liveFiles, liveClips)
	}
	if _, err := ReconcileMissing(db, tmpDir, reconcileOpts(stateDir)); !errors.Is(err, ErrReconcilePendingCanceled) {
		t.Fatalf("want canceled error after failed clear, got %v", err)
	}
}
