package services

import (
	"io/ioutil"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/jinzhu/gorm"
	_ "github.com/jinzhu/gorm/dialects/sqlite"
	"teslaxy/models"
)

func setupFableADB(t *testing.T) *gorm.DB {
	t.Helper()
	db, err := gorm.Open("sqlite3", ":memory:")
	if err != nil {
		t.Fatalf("connect db: %v", err)
	}
	db.AutoMigrate(&models.Clip{}, &models.VideoFile{}, &models.Telemetry{})
	return db
}

func writeDummyMP4(t *testing.T, path string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		t.Fatal(err)
	}
	if err := ioutil.WriteFile(path, []byte("dummy"), 0644); err != nil {
		t.Fatal(err)
	}
}

// TestScanAll_UnreadableSubdirContinues (H1): one chmod 000 directory must not
// empty the library; readable siblings are still indexed.
func TestScanAll_UnreadableSubdirContinues(t *testing.T) {
	os.Setenv("DEFAULT_TIMEZONE", "UTC")
	defer os.Unsetenv("DEFAULT_TIMEZONE")

	db := setupFableADB(t)
	defer db.Close()

	tmpDir, err := ioutil.TempDir("", "fable_h1")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(tmpDir)

	goodDir := filepath.Join(tmpDir, "SentryClips", "2024-03-10_14-00-00")
	writeDummyMP4(t, filepath.Join(goodDir, "2024-03-10_14-00-00-front.mp4"))
	if err := ioutil.WriteFile(filepath.Join(goodDir, "event.json"), []byte(`{"timestamp":"2024-03-10T14:00:10","city":"X","reason":"test"}`), 0644); err != nil {
		t.Fatal(err)
	}

	locked := filepath.Join(tmpDir, "RecentClips", "locked")
	if err := os.MkdirAll(locked, 0755); err != nil {
		t.Fatal(err)
	}
	// Place a file inside, then lock the directory so Walk cannot list it.
	writeDummyMP4(t, filepath.Join(locked, "2024-03-10_14-00-00-front.mp4"))
	if err := os.Chmod(locked, 0000); err != nil {
		t.Fatal(err)
	}
	defer os.Chmod(locked, 0755) // allow cleanup

	scanner := NewScannerService(tmpDir, db)
	scanner.ScanAll()

	var count int
	db.Model(&models.Clip{}).Count(&count)
	if count == 0 {
		t.Fatalf("expected at least one clip from readable SentryClips; got 0 (H1 regression)")
	}
	var sentry models.Clip
	if err := db.Where("event = ?", "Sentry").First(&sentry).Error; err != nil {
		t.Fatalf("expected Sentry clip indexed despite locked sibling: %v", err)
	}
}

// TestRecentFallback_DoesNotAttachToSentry (H2): Recent minute with the same
// timestamp as a Sentry clip must become its own Recent clip, not attach.
func TestRecentFallback_DoesNotAttachToSentry(t *testing.T) {
	os.Setenv("DEFAULT_TIMEZONE", "UTC")
	defer os.Unsetenv("DEFAULT_TIMEZONE")

	db := setupFableADB(t)
	defer db.Close()

	tmpDir, err := ioutil.TempDir("", "fable_h2")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(tmpDir)

	sentryDir := filepath.Join(tmpDir, "SentryClips", "evt-x")
	writeDummyMP4(t, filepath.Join(sentryDir, "2024-03-10_14-18-01-front.mp4"))
	if err := ioutil.WriteFile(filepath.Join(sentryDir, "event.json"), []byte(`{"timestamp":"2024-03-10T14:18:30","city":"Test","reason":"user_interaction_honk"}`), 0644); err != nil {
		t.Fatal(err)
	}

	recentDir := filepath.Join(tmpDir, "RecentClips")
	writeDummyMP4(t, filepath.Join(recentDir, "2024-03-10_14-18-01-front.mp4"))
	// Adjacent minute so Recent forms a 2-minute drive distinct from Sentry.
	writeDummyMP4(t, filepath.Join(recentDir, "2024-03-10_14-19-01-front.mp4"))

	scanner := NewScannerService(tmpDir, db)
	scanner.ScanAll()

	var sentry models.Clip
	if err := db.Where("event = ?", "Sentry").First(&sentry).Error; err != nil {
		t.Fatalf("missing Sentry clip: %v", err)
	}
	var sentryFiles []models.VideoFile
	db.Where("clip_id = ?", sentry.ID).Find(&sentryFiles)
	frontAt1818 := 0
	for _, vf := range sentryFiles {
		if vf.Camera == "Front" && filepath.Base(vf.FilePath) == "2024-03-10_14-18-01-front.mp4" {
			frontAt1818++
			if filepath.Dir(vf.FilePath) != sentryDir {
				t.Errorf("Sentry Front 14:18:01 should be under sentry dir, got %s", vf.FilePath)
			}
		}
	}
	if frontAt1818 != 1 {
		t.Fatalf("Sentry clip should have exactly one Front at 14:18:01, got %d", frontAt1818)
	}

	var recentCount int
	db.Model(&models.Clip{}).Where("event = ?", "Recent").Count(&recentCount)
	if recentCount < 1 {
		t.Fatalf("expected a separate Recent clip; got %d", recentCount)
	}

	// No Recent path should hang off the Sentry clip.
	for _, vf := range sentryFiles {
		if filepath.Dir(vf.FilePath) == recentDir {
			t.Errorf("Recent file %s incorrectly attached to Sentry clip %d", vf.FilePath, sentry.ID)
		}
	}
}

// TestReconcileMissing_SoftDeletesGoneFootage (M1): removing an event directory
// then reconciling must soft-delete the clip so the library omits it.
func TestReconcileMissing_SoftDeletesGoneFootage(t *testing.T) {
	os.Setenv("DEFAULT_TIMEZONE", "UTC")
	defer os.Unsetenv("DEFAULT_TIMEZONE")

	db := setupFableADB(t)
	defer db.Close()

	tmpDir, err := ioutil.TempDir("", "fable_m1")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(tmpDir)

	eventDir := filepath.Join(tmpDir, "SentryClips", "2024-03-15_12-00-00")
	writeDummyMP4(t, filepath.Join(eventDir, "2024-03-15_12-00-00-front.mp4"))
	ioutil.WriteFile(filepath.Join(eventDir, "event.json"), []byte(`{"timestamp":"2024-03-15T12:00:10","reason":"test"}`), 0644)

	recentPath := filepath.Join(tmpDir, "RecentClips", "2024-03-15_13-00-00-front.mp4")
	writeDummyMP4(t, recentPath)

	scanner := NewScannerService(tmpDir, db)
	scanner.ScanAll()

	var before int
	db.Model(&models.Clip{}).Count(&before)
	if before < 2 {
		t.Fatalf("expected >=2 live clips before delete, got %d", before)
	}

	if err := os.RemoveAll(eventDir); err != nil {
		t.Fatal(err)
	}
	if err := os.Remove(recentPath); err != nil {
		t.Fatal(err)
	}

	res, err := ReconcileMissing(db, tmpDir, ReconcileOpts{})
	if err != nil {
		t.Fatalf("ReconcileMissing: %v", err)
	}
	if res.SoftDeletedFiles < 2 {
		t.Fatalf("expected >=2 soft-deleted files, got %+v", res)
	}
	if res.SoftDeletedClips < 1 {
		t.Fatalf("expected soft-deleted clips, got %+v", res)
	}

	var liveClips int
	db.Model(&models.Clip{}).Count(&liveClips)
	if liveClips != 0 {
		t.Fatalf("expected 0 live clips after reconcile, got %d", liveClips)
	}

	// Idempotent second pass.
	res2, err := ReconcileMissing(db, tmpDir, ReconcileOpts{})
	if err != nil {
		t.Fatal(err)
	}
	if res2.SoftDeletedFiles != 0 || res2.SoftDeletedClips != 0 {
		t.Fatalf("second reconcile should be no-op, got %+v", res2)
	}

	// ScanAll after deletion also reconciles (restart path).
	scanner.ScanAll()
	db.Model(&models.Clip{}).Count(&liveClips)
	if liveClips != 0 {
		t.Fatalf("after ScanAll restart path expected 0 live clips, got %d", liveClips)
	}
}

// TestHandleFileCreate_ScansPrepopulatedDir (M2): a new event directory that
// already contains files must be scanned, not only watched.
func TestHandleFileCreate_ScansPrepopulatedDir(t *testing.T) {
	os.Setenv("DEFAULT_TIMEZONE", "UTC")
	defer os.Unsetenv("DEFAULT_TIMEZONE")

	db := setupFableADB(t)
	defer db.Close()

	tmpDir, err := ioutil.TempDir("", "fable_m2")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(tmpDir)

	sentryRoot := filepath.Join(tmpDir, "SentryClips")
	if err := os.MkdirAll(sentryRoot, 0755); err != nil {
		t.Fatal(err)
	}

	// Stage a pre-populated event dir outside the tree, then "mv" it in.
	staged, err := ioutil.TempDir("", "fable_m2_staged")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(staged)
	eventName := "2024-03-15_12-00-00"
	stagedEvent := filepath.Join(staged, eventName)
	writeDummyMP4(t, filepath.Join(stagedEvent, "2024-03-15_12-00-00-front.mp4"))
	ioutil.WriteFile(filepath.Join(stagedEvent, "event.json"), []byte(`{"timestamp":"2024-03-15T12:00:10","reason":"test"}`), 0644)

	dest := filepath.Join(sentryRoot, eventName)
	if err := os.Rename(stagedEvent, dest); err != nil {
		t.Fatal(err)
	}

	scanner := NewScannerService(tmpDir, db)
	// Simulate watcher Create for the new directory (no per-file events).
	scanner.handleFileCreate(dest)

	deadline := time.Now().Add(5 * time.Second)
	for {
		var count int
		db.Model(&models.Clip{}).Where("source_dir = ?", dest).Count(&count)
		if count == 1 {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("expected clip row for moved event dir within 5s; got %d (M2)", count)
		}
		time.Sleep(50 * time.Millisecond)
	}
}

// TestScanDir_ConcurrentNoDuplicateClips (L3): two concurrent scanDir calls on
// one event directory must yield a single Clip for that source_dir.
func TestScanDir_ConcurrentNoDuplicateClips(t *testing.T) {
	os.Setenv("DEFAULT_TIMEZONE", "UTC")
	defer os.Unsetenv("DEFAULT_TIMEZONE")

	db := setupFableADB(t)
	defer db.Close()

	tmpDir, err := ioutil.TempDir("", "fable_l3")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(tmpDir)

	eventDir := filepath.Join(tmpDir, "SentryClips", "2024-04-01_09-00-00")
	writeDummyMP4(t, filepath.Join(eventDir, "2024-04-01_09-00-00-front.mp4"))
	ioutil.WriteFile(filepath.Join(eventDir, "event.json"), []byte(`{"timestamp":"2024-04-01T09:00:10","reason":"test"}`), 0644)

	scanner := NewScannerService(tmpDir, db)

	var wg sync.WaitGroup
	for i := 0; i < 8; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			scanner.scanDir(eventDir)
		}()
	}
	wg.Wait()

	var count int
	db.Model(&models.Clip{}).Where("source_dir = ?", eventDir).Count(&count)
	if count != 1 {
		t.Fatalf("expected 1 clip for source_dir, got %d (L3)", count)
	}
}

func TestIsUnderRoot(t *testing.T) {
	root := "/footage"
	cases := []struct {
		path string
		want bool
	}{
		{"/footage/a.mp4", true},
		{"/footage", true},
		{"/footage/SentryClips/x", true},
		{"/other/a.mp4", false},
		{"/footage_other/a.mp4", false},
	}
	for _, tc := range cases {
		got := isUnderRoot(root, tc.path)
		if got != tc.want {
			t.Errorf("isUnderRoot(%q,%q)=%v want %v", root, tc.path, got, tc.want)
		}
	}
}
