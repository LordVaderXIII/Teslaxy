package services

import (
	"errors"
	"fmt"
	"io/ioutil"
	"os"
	"path/filepath"
	"testing"
	"time"

	"teslaxy/models"
)

func testStateDir(t *testing.T) string {
	t.Helper()
	dir, err := ioutil.TempDir("", "reconcile_state")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { os.RemoveAll(dir) })
	return dir
}

func reconcileOpts(stateDir string) ReconcileOpts {
	return ReconcileOpts{StateDir: stateDir}
}

func TestReconcileMissing_DryRunDoesNotWrite(t *testing.T) {
	db := setupFableADB(t)
	defer db.Close()
	stateDir := testStateDir(t)

	tmpDir, err := ioutil.TempDir("", "reconcile_dry")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(tmpDir)

	if err := ioutil.WriteFile(filepath.Join(tmpDir, "keep.txt"), []byte("x"), 0644); err != nil {
		t.Fatal(err)
	}

	clip := models.Clip{Event: "Sentry", SourceDir: filepath.Join(tmpDir, "gone")}
	db.Create(&clip)
	vf := models.VideoFile{
		ClipID:   clip.ID,
		Camera:   "Front",
		FilePath: filepath.Join(tmpDir, "gone", "x-front.mp4"),
	}
	db.Create(&vf)

	res, err := ReconcileMissing(db, tmpDir, ReconcileOpts{DryRun: true, StateDir: stateDir})
	if err != nil {
		t.Fatal(err)
	}
	if res.SoftDeletedFiles != 1 || res.SoftDeletedClips != 1 {
		t.Fatalf("dry-run counts: %+v", res)
	}

	var live int
	db.Model(&models.Clip{}).Count(&live)
	if live != 1 {
		t.Fatalf("dry-run must not soft-delete; live clips=%d", live)
	}
	if _, err := os.Stat(filepath.Join(stateDir, pendingPurgeFileName)); !os.IsNotExist(err) {
		t.Fatalf("dry-run must not write pending confirm: %v", err)
	}
}

func TestReconcileMissing_IgnoresOutsideRoot(t *testing.T) {
	db := setupFableADB(t)
	defer db.Close()
	stateDir := testStateDir(t)

	tmpDir, err := ioutil.TempDir("", "reconcile_outside")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(tmpDir)

	clip := models.Clip{Event: "Sentry", SourceDir: "/elsewhere"}
	db.Create(&clip)
	vf := models.VideoFile{
		ClipID:   clip.ID,
		Camera:   "Front",
		FilePath: "/elsewhere/not-under-root-front.mp4",
	}
	db.Create(&vf)

	res, err := ReconcileMissing(db, tmpDir, reconcileOpts(stateDir))
	if err != nil {
		t.Fatal(err)
	}
	if res.SoftDeletedFiles != 0 {
		t.Fatalf("must not soft-delete paths outside root: %+v", res)
	}
	var live int
	db.Model(&models.VideoFile{}).Count(&live)
	if live != 1 {
		t.Fatalf("outside-root video file should remain live, got %d", live)
	}
}

// TestReconcileMissing_RefusesMissingRoot: a missing root must never soft-delete.
func TestReconcileMissing_RefusesMissingRoot(t *testing.T) {
	os.Setenv("DEFAULT_TIMEZONE", "UTC")
	defer os.Unsetenv("DEFAULT_TIMEZONE")

	db := setupFableADB(t)
	defer db.Close()
	stateDir := testStateDir(t)

	tmpDir, err := ioutil.TempDir("", "reconcile_refuse_missing")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(tmpDir)

	dir := filepath.Join(tmpDir, "SentryClips", "e1")
	writeDummyMP4(t, filepath.Join(dir, "2024-05-01_12-00-00-front.mp4"))
	if err := ioutil.WriteFile(filepath.Join(dir, "event.json"), []byte(`{"timestamp":"2024-05-01T12:00:10","reason":"test"}`), 0644); err != nil {
		t.Fatal(err)
	}

	scanner := NewScannerService(tmpDir, db)
	os.Setenv("CONFIG_PATH", stateDir)
	defer os.Unsetenv("CONFIG_PATH")
	scanner.ScanAll()

	var before int
	db.Model(&models.Clip{}).Count(&before)
	if before != 1 {
		t.Fatalf("expected 1 clip, got %d", before)
	}

	if err := os.RemoveAll(tmpDir); err != nil {
		t.Fatal(err)
	}
	if _, err := ReconcileMissing(db, tmpDir, reconcileOpts(stateDir)); !errors.Is(err, ErrReconcileRefused) {
		t.Fatalf("missing root: got %v, want ErrReconcileRefused", err)
	}
	var live int
	db.Model(&models.Clip{}).Count(&live)
	if live != 1 {
		t.Fatalf("missing root soft-deleted clips: live=%d, want 1", live)
	}
}

// TestReconcileMissing_SameInodeEmptyPreservesRows (round-4 P1): emptying the
// root in place (same inode) must refuse on first sighting and leave rows.
func TestReconcileMissing_SameInodeEmptyPreservesRows(t *testing.T) {
	os.Setenv("DEFAULT_TIMEZONE", "UTC")
	defer os.Unsetenv("DEFAULT_TIMEZONE")

	db := setupFableADB(t)
	defer db.Close()
	stateDir := testStateDir(t)

	tmpDir, err := ioutil.TempDir("", "reconcile_same_inode")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(tmpDir)

	for i := 1; i <= 4; i++ {
		dir := filepath.Join(tmpDir, "SentryClips", fmt.Sprintf("e%d", i))
		stamp := fmt.Sprintf("2024-05-%02d_12-00-00", i)
		writeDummyMP4(t, filepath.Join(dir, stamp+"-front.mp4"))
		if err := ioutil.WriteFile(filepath.Join(dir, "event.json"), []byte(`{"timestamp":"2024-05-01T12:00:10","reason":"test"}`), 0644); err != nil {
			t.Fatal(err)
		}
	}

	os.Setenv("CONFIG_PATH", stateDir)
	defer os.Unsetenv("CONFIG_PATH")
	NewScannerService(tmpDir, db).ScanAll()

	entries, err := ioutil.ReadDir(tmpDir)
	if err != nil {
		t.Fatal(err)
	}
	for _, e := range entries {
		if err := os.RemoveAll(filepath.Join(tmpDir, e.Name())); err != nil {
			t.Fatal(err)
		}
	}

	if _, err := ReconcileMissing(db, tmpDir, reconcileOpts(stateDir)); !errors.Is(err, ErrReconcilePendingConfirm) {
		t.Fatalf("same-inode empty: got %v, want ErrReconcilePendingConfirm", err)
	}
	var live int
	db.Model(&models.Clip{}).Count(&live)
	if live != 4 {
		t.Fatalf("same-inode empty must preserve rows: live=%d, want 4", live)
	}
}

// TestReconcileMissing_SavedClipsAsidePreservesUntilConfirm (round-4 P1): moving
// SavedClips aside while the root inode stays must not wipe rows on first pass.
func TestReconcileMissing_SavedClipsAsidePreservesUntilConfirm(t *testing.T) {
	os.Setenv("DEFAULT_TIMEZONE", "UTC")
	defer os.Unsetenv("DEFAULT_TIMEZONE")

	db := setupFableADB(t)
	defer db.Close()
	stateDir := testStateDir(t)

	tmpDir, err := ioutil.TempDir("", "reconcile_saved_aside")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(tmpDir)

	saved := filepath.Join(tmpDir, "SavedClips", "evt")
	writeDummyMP4(t, filepath.Join(saved, "2024-05-01_12-00-00-front.mp4"))
	if err := ioutil.WriteFile(filepath.Join(saved, "event.json"), []byte(`{"timestamp":"2024-05-01T12:00:10","reason":"test"}`), 0644); err != nil {
		t.Fatal(err)
	}
	// Sibling keeps the root non-empty.
	if err := os.MkdirAll(filepath.Join(tmpDir, "SentryClips"), 0755); err != nil {
		t.Fatal(err)
	}
	if err := ioutil.WriteFile(filepath.Join(tmpDir, "SentryClips", "keep.txt"), []byte("x"), 0644); err != nil {
		t.Fatal(err)
	}

	os.Setenv("CONFIG_PATH", stateDir)
	defer os.Unsetenv("CONFIG_PATH")
	NewScannerService(tmpDir, db).ScanAll()

	aside, err := ioutil.TempDir("", "saved_aside")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(aside)
	if err := os.Rename(filepath.Join(tmpDir, "SavedClips"), filepath.Join(aside, "SavedClips")); err != nil {
		t.Fatal(err)
	}

	if _, err := ReconcileMissing(db, tmpDir, reconcileOpts(stateDir)); !errors.Is(err, ErrReconcilePendingConfirm) {
		t.Fatalf("SavedClips aside: got %v, want ErrReconcilePendingConfirm", err)
	}
	var live int
	db.Model(&models.Clip{}).Count(&live)
	if live != 1 {
		t.Fatalf("SavedClips aside must preserve clip on first pass: live=%d", live)
	}

	// Restore footage: pending fingerprint should clear / reset; rows stay.
	if err := os.Rename(filepath.Join(aside, "SavedClips"), filepath.Join(tmpDir, "SavedClips")); err != nil {
		t.Fatal(err)
	}
	res, err := ReconcileMissing(db, tmpDir, reconcileOpts(stateDir))
	if err != nil {
		t.Fatalf("after restore: %v", err)
	}
	if res.SoftDeletedFiles != 0 || res.SoftDeletedClips != 0 {
		t.Fatalf("restored footage must not soft-delete: %+v", res)
	}
	db.Model(&models.Clip{}).Count(&live)
	if live != 1 {
		t.Fatalf("restored footage: live=%d, want 1", live)
	}
}

// TestReconcileMissing_ClipCleanupScopedToRoot (P3): reconciling root A must
// not soft-delete an empty clip that belongs to root B after confirmation.
func TestReconcileMissing_ClipCleanupScopedToRoot(t *testing.T) {
	db := setupFableADB(t)
	defer db.Close()
	stateDir := testStateDir(t)

	rootA, err := ioutil.TempDir("", "reconcile_scope_a")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(rootA)
	rootB, err := ioutil.TempDir("", "reconcile_scope_b")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(rootB)

	if err := ioutil.WriteFile(filepath.Join(rootA, "keep.txt"), []byte("x"), 0644); err != nil {
		t.Fatal(err)
	}

	clipA := models.Clip{Event: "Sentry", SourceDir: filepath.Join(rootA, "SentryClips", "only-a")}
	db.Create(&clipA)
	clipB := models.Clip{Event: "Sentry", SourceDir: filepath.Join(rootB, "SentryClips", "only-b")}
	db.Create(&clipB)

	if _, err := ReconcileMissing(db, rootA, reconcileOpts(stateDir)); !errors.Is(err, ErrReconcilePendingConfirm) {
		t.Fatalf("first pass: got %v, want pending", err)
	}
	if err := agePendingPurgeForTest(stateDir, rootA, reconcileConfirmMinInterval+time.Second); err != nil {
		t.Fatal(err)
	}
	res, err := ReconcileMissing(db, rootA, reconcileOpts(stateDir))
	if err != nil {
		t.Fatal(err)
	}
	if res.SoftDeletedClips != 1 {
		t.Fatalf("expected only root A clip to be soft-deleted, got %+v", res)
	}

	var liveA, liveB int
	db.Model(&models.Clip{}).Where("id = ?", clipA.ID).Count(&liveA)
	db.Model(&models.Clip{}).Where("id = ?", clipB.ID).Count(&liveB)
	if liveA != 0 {
		t.Fatalf("empty clip under root A should be soft-deleted, live=%d", liveA)
	}
	if liveB != 1 {
		t.Fatalf("empty clip under root B was soft-deleted while reconciling root A, live=%d", liveB)
	}
}

// TestReconcileMissing_AbortsWhenRootVanishesMidDelete: identity re-check during
// an explicit purge aborts if the root disappears mid-pass.
func TestReconcileMissing_AbortsWhenRootVanishesMidDelete(t *testing.T) {
	db := setupFableADB(t)
	defer db.Close()
	stateDir := testStateDir(t)

	tmpDir, err := ioutil.TempDir("", "reconcile_vanish_mid")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(tmpDir)

	if err := ioutil.WriteFile(filepath.Join(tmpDir, "keep.txt"), []byte("x"), 0644); err != nil {
		t.Fatal(err)
	}

	const n = 4
	for i := 0; i < n; i++ {
		clip := models.Clip{Event: "Sentry", SourceDir: filepath.Join(tmpDir, fmt.Sprintf("gone-%d", i))}
		db.Create(&clip)
		vf := models.VideoFile{
			ClipID:   clip.ID,
			Camera:   "Front",
			FilePath: filepath.Join(tmpDir, fmt.Sprintf("gone-%d", i), "x-front.mp4"),
		}
		db.Create(&vf)
	}

	prevBatch := reconcileDeleteBatchSize
	reconcileDeleteBatchSize = 1
	defer func() { reconcileDeleteBatchSize = prevBatch }()

	batches := 0
	reconcileBeforeDeleteBatch = func() {
		batches++
		if batches == 2 {
			if err := os.RemoveAll(tmpDir); err != nil {
				t.Errorf("remove root mid-delete: %v", err)
			}
		}
	}
	defer func() { reconcileBeforeDeleteBatch = nil }()

	res, err := ReconcileMissing(db, tmpDir, ReconcileOpts{ForceEmptyPurge: true, StateDir: stateDir})
	if !errors.Is(err, ErrReconcileRootChanged) {
		t.Fatalf("vanished mid-delete: got %v, want ErrReconcileRootChanged", err)
	}
	if res.SoftDeletedFiles < 1 || res.SoftDeletedFiles >= n {
		t.Fatalf("partial abort expected, got %+v", res)
	}
}

// TestReconcileMissing_AbortsWhenRootSwappedEmptyMidDelete: empty swap mid-purge aborts.
func TestReconcileMissing_AbortsWhenRootSwappedEmptyMidDelete(t *testing.T) {
	db := setupFableADB(t)
	defer db.Close()
	stateDir := testStateDir(t)

	tmpDir, err := ioutil.TempDir("", "reconcile_swap_mid")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(tmpDir)

	if err := ioutil.WriteFile(filepath.Join(tmpDir, "keep.txt"), []byte("x"), 0644); err != nil {
		t.Fatal(err)
	}

	const n = 4
	for i := 0; i < n; i++ {
		clip := models.Clip{Event: "Sentry", SourceDir: filepath.Join(tmpDir, fmt.Sprintf("gone-%d", i))}
		db.Create(&clip)
		db.Create(&models.VideoFile{
			ClipID:   clip.ID,
			Camera:   "Front",
			FilePath: filepath.Join(tmpDir, fmt.Sprintf("gone-%d", i), "x-front.mp4"),
		})
	}

	prevBatch := reconcileDeleteBatchSize
	reconcileDeleteBatchSize = 1
	defer func() { reconcileDeleteBatchSize = prevBatch }()

	swapDir, err := ioutil.TempDir("", "reconcile_swap_alt")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(swapDir)

	batches := 0
	reconcileBeforeDeleteBatch = func() {
		batches++
		if batches == 2 {
			if err := os.RemoveAll(tmpDir); err != nil {
				t.Errorf("remove root for swap: %v", err)
			}
			if err := os.Rename(swapDir, tmpDir); err != nil {
				t.Errorf("rename empty swap into place: %v", err)
			}
		}
	}
	defer func() { reconcileBeforeDeleteBatch = nil }()

	res, err := ReconcileMissing(db, tmpDir, ReconcileOpts{ForceEmptyPurge: true, StateDir: stateDir})
	if !errors.Is(err, ErrReconcileRootChanged) {
		t.Fatalf("swapped empty mid-delete: got %v, want ErrReconcileRootChanged", err)
	}
	if res.SoftDeletedFiles < 1 || res.SoftDeletedFiles >= n {
		t.Fatalf("partial abort expected, got %+v", res)
	}
}

// TestReconcileMissing_DeleteAllAfterPersistedInterval (round-4 P2): intentional
// delete-all completes after a persisted min interval and survives restart.
func TestReconcileMissing_DeleteAllAfterPersistedInterval(t *testing.T) {
	os.Setenv("DEFAULT_TIMEZONE", "UTC")
	defer os.Unsetenv("DEFAULT_TIMEZONE")

	db := setupFableADB(t)
	defer db.Close()
	stateDir := testStateDir(t)

	tmpDir, err := ioutil.TempDir("", "reconcile_delete_all")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(tmpDir)

	for i := 1; i <= 3; i++ {
		dir := filepath.Join(tmpDir, "SentryClips", fmt.Sprintf("e%d", i))
		stamp := fmt.Sprintf("2024-06-%02d_12-00-00", i)
		writeDummyMP4(t, filepath.Join(dir, stamp+"-front.mp4"))
		if err := ioutil.WriteFile(filepath.Join(dir, "event.json"), []byte(`{"timestamp":"2024-06-01T12:00:10","reason":"test"}`), 0644); err != nil {
			t.Fatal(err)
		}
	}

	os.Setenv("CONFIG_PATH", stateDir)
	defer os.Unsetenv("CONFIG_PATH")
	NewScannerService(tmpDir, db).ScanAll()

	entries, err := ioutil.ReadDir(tmpDir)
	if err != nil {
		t.Fatal(err)
	}
	for _, e := range entries {
		if err := os.RemoveAll(filepath.Join(tmpDir, e.Name())); err != nil {
			t.Fatal(err)
		}
	}

	if _, err := ReconcileMissing(db, tmpDir, reconcileOpts(stateDir)); !errors.Is(err, ErrReconcilePendingConfirm) {
		t.Fatalf("first empty: got %v, want pending", err)
	}

	// Survive "restart": only the on-disk pending file remains.
	if err := agePendingPurgeForTest(stateDir, tmpDir, reconcileConfirmMinInterval+time.Second); err != nil {
		t.Fatal(err)
	}

	res, err := ReconcileMissing(db, tmpDir, reconcileOpts(stateDir))
	if err != nil {
		t.Fatalf("confirmed delete-all: %v", err)
	}
	if res.SoftDeletedFiles < 3 || res.SoftDeletedClips < 3 {
		t.Fatalf("confirmed delete-all should purge, got %+v", res)
	}
	var live int
	db.Model(&models.Clip{}).Count(&live)
	if live != 0 {
		t.Fatalf("want 0 live clips after confirm, got %d", live)
	}
}

// TestReconcileMissing_EmptyRootRecreationPurgePath (round-4 P2): recreating the
// root as a new empty inode is not refused forever; ForceEmptyPurge / confirm works.
func TestReconcileMissing_EmptyRootRecreationPurgePath(t *testing.T) {
	os.Setenv("DEFAULT_TIMEZONE", "UTC")
	defer os.Unsetenv("DEFAULT_TIMEZONE")

	db := setupFableADB(t)
	defer db.Close()
	stateDir := testStateDir(t)

	tmpDir, err := ioutil.TempDir("", "reconcile_recreate")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(tmpDir)

	for i := 1; i <= 3; i++ {
		dir := filepath.Join(tmpDir, "SentryClips", fmt.Sprintf("e%d", i))
		stamp := fmt.Sprintf("2024-07-%02d_12-00-00", i)
		writeDummyMP4(t, filepath.Join(dir, stamp+"-front.mp4"))
		if err := ioutil.WriteFile(filepath.Join(dir, "event.json"), []byte(`{"timestamp":"2024-07-01T12:00:10","reason":"test"}`), 0644); err != nil {
			t.Fatal(err)
		}
	}

	os.Setenv("CONFIG_PATH", stateDir)
	defer os.Unsetenv("CONFIG_PATH")
	scanner := NewScannerService(tmpDir, db)
	scanner.ScanAll()

	if err := os.RemoveAll(tmpDir); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(tmpDir, 0755); err != nil {
		t.Fatal(err)
	}

	// New inode empty root: first pass pending (not permanent refuse).
	if _, err := ReconcileMissing(db, tmpDir, reconcileOpts(stateDir)); !errors.Is(err, ErrReconcilePendingConfirm) {
		t.Fatalf("recreated empty: got %v, want pending", err)
	}
	var live int
	db.Model(&models.Clip{}).Count(&live)
	if live != 3 {
		t.Fatalf("recreated empty must preserve until purge: live=%d", live)
	}

	// Explicit operator purge is reachable and clears ghosts.
	res, err := scanner.PurgeMissing()
	if err != nil {
		t.Fatalf("PurgeMissing: %v", err)
	}
	if res.SoftDeletedFiles < 3 || res.SoftDeletedClips < 3 {
		t.Fatalf("PurgeMissing should clear library, got %+v", res)
	}
	db.Model(&models.Clip{}).Count(&live)
	if live != 0 {
		t.Fatalf("after PurgeMissing want 0 live clips, got %d", live)
	}
}

// TestReconcileMissing_DeletesOnlyTopLevelVideoAfterConfirm: sole top-level
// video removal uses the same persisted two-step confirm.
func TestReconcileMissing_DeletesOnlyTopLevelVideoAfterConfirm(t *testing.T) {
	db := setupFableADB(t)
	defer db.Close()
	stateDir := testStateDir(t)

	tmpDir, err := ioutil.TempDir("", "reconcile_toplevel")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(tmpDir)

	videoPath := filepath.Join(tmpDir, "2024-07-01_12-00-00-front.mp4")
	writeDummyMP4(t, videoPath)

	clip := models.Clip{Event: "Recent", SourceDir: tmpDir}
	db.Create(&clip)
	db.Create(&models.VideoFile{ClipID: clip.ID, Camera: "Front", FilePath: videoPath})

	if err := os.Remove(videoPath); err != nil {
		t.Fatal(err)
	}

	if _, err := ReconcileMissing(db, tmpDir, reconcileOpts(stateDir)); !errors.Is(err, ErrReconcilePendingConfirm) {
		t.Fatalf("first empty: got %v, want pending", err)
	}
	if err := agePendingPurgeForTest(stateDir, tmpDir, reconcileConfirmMinInterval+time.Second); err != nil {
		t.Fatal(err)
	}
	res, err := ReconcileMissing(db, tmpDir, reconcileOpts(stateDir))
	if err != nil {
		t.Fatal(err)
	}
	if res.SoftDeletedFiles != 1 || res.SoftDeletedClips != 1 {
		t.Fatalf("confirmed top-level delete: %+v", res)
	}
}

// TestScanner_WatcherDeleteAllRetriesAfterInterval (round-4 P2): fsnotify-driven
// reconcile schedules a retry so delete-all completes without a manual second scan.
func TestScanner_WatcherDeleteAllRetriesAfterInterval(t *testing.T) {
	os.Setenv("DEFAULT_TIMEZONE", "UTC")
	defer os.Unsetenv("DEFAULT_TIMEZONE")

	db := setupFableADB(t)
	defer db.Close()
	stateDir := testStateDir(t)

	tmpDir, err := ioutil.TempDir("", "reconcile_watcher")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(tmpDir)

	dir := filepath.Join(tmpDir, "SentryClips", "only")
	writeDummyMP4(t, filepath.Join(dir, "2024-08-01_12-00-00-front.mp4"))
	if err := ioutil.WriteFile(filepath.Join(dir, "event.json"), []byte(`{"timestamp":"2024-08-01T12:00:10","reason":"test"}`), 0644); err != nil {
		t.Fatal(err)
	}

	prevDebounce := reconcileDebounce
	prevInterval := reconcileConfirmMinInterval
	reconcileDebounce = 20 * time.Millisecond
	reconcileConfirmMinInterval = 40 * time.Millisecond
	defer func() {
		reconcileDebounce = prevDebounce
		reconcileConfirmMinInterval = prevInterval
	}()

	os.Setenv("CONFIG_PATH", stateDir)
	defer os.Unsetenv("CONFIG_PATH")
	scanner := NewScannerService(tmpDir, db)
	scanner.ScanAll()

	var before int
	db.Model(&models.Clip{}).Count(&before)
	if before != 1 {
		t.Fatalf("expected 1 clip, got %d", before)
	}

	if err := os.RemoveAll(dir); err != nil {
		t.Fatal(err)
	}
	scanner.scheduleReconcile()

	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		var live int
		db.Model(&models.Clip{}).Count(&live)
		if live == 0 {
			return
		}
		time.Sleep(25 * time.Millisecond)
	}
	var live int
	db.Model(&models.Clip{}).Count(&live)
	t.Fatalf("watcher-driven delete-all did not complete after persisted interval; live=%d", live)
}
