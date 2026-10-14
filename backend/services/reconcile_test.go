package services

import (
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
