package services

import (
	"errors"
	"fmt"
	"io/ioutil"
	"os"
	"path/filepath"
	"testing"

	"teslaxy/models"
)

func TestReconcileMissing_DryRunDoesNotWrite(t *testing.T) {
	resetReconcileRootState()
	db := setupFableADB(t)
	defer db.Close()

	tmpDir, err := ioutil.TempDir("", "reconcile_dry")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(tmpDir)

	// Root must not be empty: a missing or empty root with clips is refused.
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

	res, err := ReconcileMissing(db, tmpDir, ReconcileOpts{DryRun: true})
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
}

func TestReconcileMissing_IgnoresOutsideRoot(t *testing.T) {
	resetReconcileRootState()
	db := setupFableADB(t)
	defer db.Close()

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

	res, err := ReconcileMissing(db, tmpDir, ReconcileOpts{})
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

// TestReconcileMissing_RefusesMissingOrEmptyRoot (P1): a missing root, or an
// empty directory put in its place, must not soft-delete clips. Restoring the
// files leaves the original rows in place.
func TestReconcileMissing_RefusesMissingOrEmptyRoot(t *testing.T) {
	resetReconcileRootState()
	os.Setenv("DEFAULT_TIMEZONE", "UTC")
	defer os.Unsetenv("DEFAULT_TIMEZONE")

	db := setupFableADB(t)
	defer db.Close()

	tmpDir, err := ioutil.TempDir("", "reconcile_refuse")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(tmpDir)

	var eventDirs []string
	for i := 1; i <= 4; i++ {
		name := []string{"e1", "e2", "e3", "e4"}[i-1]
		dir := filepath.Join(tmpDir, "SentryClips", name)
		eventDirs = append(eventDirs, dir)
		stamp := fmt.Sprintf("2024-05-%02d_12-00-00", i)
		writeDummyMP4(t, filepath.Join(dir, stamp+"-front.mp4"))
		if err := ioutil.WriteFile(filepath.Join(dir, "event.json"), []byte(`{"timestamp":"2024-05-01T12:00:10","reason":"test"}`), 0644); err != nil {
			t.Fatal(err)
		}
	}

	scanner := NewScannerService(tmpDir, db)
	scanner.ScanAll()

	var before int
	db.Model(&models.Clip{}).Count(&before)
	if before != 4 {
		t.Fatalf("expected 4 clips before the root disappeared, got %d", before)
	}

	if err := os.RemoveAll(tmpDir); err != nil {
		t.Fatal(err)
	}
	if _, err := ReconcileMissing(db, tmpDir, ReconcileOpts{}); !errors.Is(err, ErrReconcileRefused) {
		t.Fatalf("missing root: got %v, want ErrReconcileRefused", err)
	}
	var live int
	db.Model(&models.Clip{}).Count(&live)
	if live != 4 {
		t.Fatalf("missing root soft-deleted clips: live=%d, want 4", live)
	}

	if err := os.MkdirAll(tmpDir, 0755); err != nil {
		t.Fatal(err)
	}
	if _, err := ReconcileMissing(db, tmpDir, ReconcileOpts{}); !errors.Is(err, ErrReconcileRefused) {
		t.Fatalf("empty root: got %v, want ErrReconcileRefused", err)
	}
	db.Model(&models.Clip{}).Count(&live)
	if live != 4 {
		t.Fatalf("empty root soft-deleted clips: live=%d, want 4", live)
	}

	for i, dir := range eventDirs {
		stamp := fmt.Sprintf("2024-05-%02d_12-00-00", i+1)
		writeDummyMP4(t, filepath.Join(dir, stamp+"-front.mp4"))
		if err := ioutil.WriteFile(filepath.Join(dir, "event.json"), []byte(`{"timestamp":"2024-05-01T12:00:10","reason":"test"}`), 0644); err != nil {
			t.Fatal(err)
		}
	}
	scanner.ScanAll()

	db.Model(&models.Clip{}).Count(&live)
	var all int
	db.Unscoped().Model(&models.Clip{}).Count(&all)
	if live != 4 || all != 4 {
		t.Fatalf("restored root: live=%d unscoped=%d, want 4 and 4 (rows were wiped or duplicated)", live, all)
	}
}

// TestReconcileMissing_ClipCleanupScopedToRoot (P3): reconciling root A must
// not soft-delete an empty clip that belongs to root B. An empty clip under A
// is still removed.
func TestReconcileMissing_ClipCleanupScopedToRoot(t *testing.T) {
	resetReconcileRootState()
	db := setupFableADB(t)
	defer db.Close()

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

	res, err := ReconcileMissing(db, rootA, ReconcileOpts{})
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

// TestReconcileMissing_AbortsWhenRootVanishesMidDelete (round-3 P1): the root
// identity is re-checked before each delete batch. If the folder disappears
// mid-pass, cleanup aborts with ErrReconcileRootChanged and does not wipe the
// remaining rows.
func TestReconcileMissing_AbortsWhenRootVanishesMidDelete(t *testing.T) {
	resetReconcileRootState()
	db := setupFableADB(t)
	defer db.Close()

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

	res, err := ReconcileMissing(db, tmpDir, ReconcileOpts{})
	if !errors.Is(err, ErrReconcileRootChanged) {
		t.Fatalf("vanished mid-delete: got %v, want ErrReconcileRootChanged", err)
	}
	if res.SoftDeletedFiles < 1 {
		t.Fatalf("expected at least one file soft-deleted before abort, got %+v", res)
	}
	if res.SoftDeletedFiles >= n {
		t.Fatalf("vanished root must not soft-delete every file: %+v", res)
	}

	var liveFiles int
	db.Model(&models.VideoFile{}).Count(&liveFiles)
	if liveFiles == 0 || liveFiles >= n {
		t.Fatalf("expected some live video files to survive abort, live=%d", liveFiles)
	}
}

// TestReconcileMissing_AbortsWhenRootSwappedEmptyMidDelete (round-3 P1): if the
// root is replaced mid-pass with a different empty directory (new inode),
// cleanup aborts instead of soft-deleting every remaining clip.
func TestReconcileMissing_AbortsWhenRootSwappedEmptyMidDelete(t *testing.T) {
	resetReconcileRootState()
	db := setupFableADB(t)
	defer db.Close()

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

	// Pre-create the replacement empty directory so its inode is distinct
	// before we rename it over the original path (MkdirAll after RemoveAll
	// can reuse the same inode on Linux).
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

	res, err := ReconcileMissing(db, tmpDir, ReconcileOpts{})
	if !errors.Is(err, ErrReconcileRootChanged) {
		t.Fatalf("swapped empty mid-delete: got %v, want ErrReconcileRootChanged", err)
	}
	if res.SoftDeletedFiles < 1 {
		t.Fatalf("expected at least one file soft-deleted before abort, got %+v", res)
	}
	if res.SoftDeletedFiles >= n {
		t.Fatalf("swapped empty root must not soft-delete every file: %+v", res)
	}

	var liveFiles int
	db.Model(&models.VideoFile{}).Count(&liveFiles)
	if liveFiles == 0 || liveFiles >= n {
		t.Fatalf("expected some live video files to survive abort, live=%d", liveFiles)
	}
}

// TestReconcileMissing_EmptiesLibraryAfterTwoStableEmptyScans (round-3 P2): when
// the user deletes all footage in place (same device/inode), the first empty
// reconcile refuses and the second with a stable identity soft-deletes the dead
// rows. A ForceEmptyPurge path is also available.
func TestReconcileMissing_EmptiesLibraryAfterTwoStableEmptyScans(t *testing.T) {
	resetReconcileRootState()
	os.Setenv("DEFAULT_TIMEZONE", "UTC")
	defer os.Unsetenv("DEFAULT_TIMEZONE")

	db := setupFableADB(t)
	defer db.Close()

	tmpDir, err := ioutil.TempDir("", "reconcile_empty_all")
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

	scanner := NewScannerService(tmpDir, db)
	scanner.ScanAll()

	var before int
	db.Model(&models.Clip{}).Count(&before)
	if before != 3 {
		t.Fatalf("expected 3 clips before empty, got %d", before)
	}

	entries, err := ioutil.ReadDir(tmpDir)
	if err != nil {
		t.Fatal(err)
	}
	for _, e := range entries {
		if err := os.RemoveAll(filepath.Join(tmpDir, e.Name())); err != nil {
			t.Fatal(err)
		}
	}

	if _, err := ReconcileMissing(db, tmpDir, ReconcileOpts{}); !errors.Is(err, ErrReconcileRefused) {
		t.Fatalf("first empty scan: got %v, want ErrReconcileRefused", err)
	}
	var live int
	db.Model(&models.Clip{}).Count(&live)
	if live != 3 {
		t.Fatalf("first empty scan must keep clips: live=%d", live)
	}

	res, err := ReconcileMissing(db, tmpDir, ReconcileOpts{})
	if err != nil {
		t.Fatalf("second empty scan: %v", err)
	}
	if res.SoftDeletedFiles < 3 || res.SoftDeletedClips < 3 {
		t.Fatalf("second empty scan should purge library, got %+v", res)
	}
	db.Model(&models.Clip{}).Count(&live)
	if live != 0 {
		t.Fatalf("after two stable empty scans want 0 live clips, got %d", live)
	}
}

// TestReconcileMissing_DeletesOnlyTopLevelVideoAfterConfirm (round-3 P2): removing
// the sole top-level video leaves an empty root; two consecutive confirms with a
// stable identity clear the dead clip. ForceEmptyPurge also works in one pass.
func TestReconcileMissing_DeletesOnlyTopLevelVideoAfterConfirm(t *testing.T) {
	resetReconcileRootState()
	db := setupFableADB(t)
	defer db.Close()

	tmpDir, err := ioutil.TempDir("", "reconcile_toplevel")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(tmpDir)

	videoPath := filepath.Join(tmpDir, "2024-07-01_12-00-00-front.mp4")
	writeDummyMP4(t, videoPath)

	clip := models.Clip{Event: "Recent", SourceDir: tmpDir}
	db.Create(&clip)
	vf := models.VideoFile{ClipID: clip.ID, Camera: "Front", FilePath: videoPath}
	db.Create(&vf)

	// Record non-empty identity (same as a prior successful scan).
	if _, err := ReconcileMissing(db, tmpDir, ReconcileOpts{}); err != nil {
		t.Fatal(err)
	}
	var live int
	db.Model(&models.Clip{}).Count(&live)
	if live != 1 {
		t.Fatalf("seed clip should remain live, got %d", live)
	}

	if err := os.Remove(videoPath); err != nil {
		t.Fatal(err)
	}

	if _, err := ReconcileMissing(db, tmpDir, ReconcileOpts{}); !errors.Is(err, ErrReconcileRefused) {
		t.Fatalf("first empty after top-level delete: got %v, want ErrReconcileRefused", err)
	}
	db.Model(&models.Clip{}).Count(&live)
	if live != 1 {
		t.Fatalf("first confirm must keep the clip, live=%d", live)
	}

	res, err := ReconcileMissing(db, tmpDir, ReconcileOpts{})
	if err != nil {
		t.Fatalf("second empty confirm: %v", err)
	}
	if res.SoftDeletedFiles != 1 || res.SoftDeletedClips != 1 {
		t.Fatalf("second confirm should soft-delete the top-level video, got %+v", res)
	}
	db.Model(&models.Clip{}).Count(&live)
	if live != 0 {
		t.Fatalf("want 0 live clips after confirm, got %d", live)
	}
}

// TestReconcileMissing_ForceEmptyPurgeSkipsConfirm covers the explicit purge flag.
func TestReconcileMissing_ForceEmptyPurgeSkipsConfirm(t *testing.T) {
	resetReconcileRootState()
	db := setupFableADB(t)
	defer db.Close()

	tmpDir, err := ioutil.TempDir("", "reconcile_force_purge")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(tmpDir)

	videoPath := filepath.Join(tmpDir, "only-front.mp4")
	writeDummyMP4(t, videoPath)
	clip := models.Clip{Event: "Recent", SourceDir: tmpDir}
	db.Create(&clip)
	db.Create(&models.VideoFile{ClipID: clip.ID, Camera: "Front", FilePath: videoPath})

	if _, err := ReconcileMissing(db, tmpDir, ReconcileOpts{}); err != nil {
		t.Fatal(err)
	}
	if err := os.Remove(videoPath); err != nil {
		t.Fatal(err)
	}

	res, err := ReconcileMissing(db, tmpDir, ReconcileOpts{ForceEmptyPurge: true})
	if err != nil {
		t.Fatalf("ForceEmptyPurge: %v", err)
	}
	if res.SoftDeletedFiles != 1 || res.SoftDeletedClips != 1 {
		t.Fatalf("ForceEmptyPurge should purge in one pass, got %+v", res)
	}
}
