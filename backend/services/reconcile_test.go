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
