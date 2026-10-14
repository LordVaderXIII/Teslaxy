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
	// :memory: is per-connection unless the pool stays at one connection.
	// Parallel ScanAll workers would otherwise see an empty database.
	db.DB().SetMaxOpenConns(1)
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

// assertIndexHoldsMediaLock fails when index inserts clips before taking the
// media read lock, or when ReconcileMissing runs while that lock is held.
func assertIndexHoldsMediaLock(t *testing.T, db *gorm.DB, root string, index func()) {
	t.Helper()

	entered := make(chan struct{})
	release := make(chan struct{})
	var once sync.Once
	mediaReadLockedHook = func() {
		once.Do(func() { close(entered) })
		<-release
	}
	released := false
	defer func() {
		mediaReadLockedHook = nil
		if !released {
			close(release)
		}
	}()

	done := make(chan struct{})
	go func() {
		index()
		close(done)
	}()

	select {
	case <-entered:
	case <-time.After(5 * time.Second):
		t.Fatal("indexer did not acquire the media read lock")
	}

	var n int
	db.Model(&models.Clip{}).Count(&n)
	if n != 0 {
		t.Fatalf("indexed %d clips before holding the media read lock", n)
	}

	recDone := make(chan error, 1)
	go func() {
		_, err := ReconcileMissing(db, root, ReconcileOpts{})
		recDone <- err
	}()
	select {
	case err := <-recDone:
		t.Fatalf("reconcile finished while the indexer held the media lock: %v", err)
	case <-time.After(400 * time.Millisecond):
	}

	close(release)
	released = true

	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("indexer did not finish after releasing the media lock")
	}
	select {
	case err := <-recDone:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("reconcile did not finish after the indexer released the lock")
	}

	db.Model(&models.Clip{}).Count(&n)
	if n == 0 {
		t.Fatal("expected clips once the indexer was allowed to finish")
	}
}

// TestScanner_TakesMediaLock (P2): scanDir and ScanAll hold the media read
// lock around indexing, so reconcile cannot delete clips mid-scan.
func TestScanner_TakesMediaLock(t *testing.T) {
	os.Setenv("DEFAULT_TIMEZONE", "UTC")
	defer os.Unsetenv("DEFAULT_TIMEZONE")

	for _, tc := range []struct {
		name  string
		index func(s *ScannerService, recentDir string)
	}{
		{"scanDir", func(s *ScannerService, recentDir string) { s.scanDir(recentDir) }},
		{"ScanAll", func(s *ScannerService, _ string) { s.ScanAll() }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			db := setupFableADB(t)
			defer db.Close()
			tmpDir, err := ioutil.TempDir("", "fable_media_lock")
			if err != nil {
				t.Fatal(err)
			}
			defer os.RemoveAll(tmpDir)
			recentDir := filepath.Join(tmpDir, "RecentClips")
			writeDummyMP4(t, filepath.Join(recentDir, "2024-03-10_14-19-01-front.mp4"))
			scanner := NewScannerService(tmpDir, db)
			assertIndexHoldsMediaLock(t, db, tmpDir, func() { tc.index(scanner, recentDir) })
		})
	}
}

// TestRecentRescan_DeletedFirstMinuteNotDuplicated (P2 regression): deleting
// the first Recent minute and scanning twice must leave the remaining 14:19
// footage as one clip and one file row.
func TestRecentRescan_DeletedFirstMinuteNotDuplicated(t *testing.T) {
	os.Setenv("DEFAULT_TIMEZONE", "UTC")
	defer os.Unsetenv("DEFAULT_TIMEZONE")

	db := setupFableADB(t)
	defer db.Close()

	tmpDir, err := ioutil.TempDir("", "fable_recent_dup")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(tmpDir)

	recentDir := filepath.Join(tmpDir, "RecentClips")
	first := filepath.Join(recentDir, "2024-03-10_14-18-01-front.mp4")
	second := filepath.Join(recentDir, "2024-03-10_14-19-01-front.mp4")
	writeDummyMP4(t, first)
	writeDummyMP4(t, second)

	scanner := NewScannerService(tmpDir, db)
	scanner.ScanAll()

	if err := os.Remove(first); err != nil {
		t.Fatal(err)
	}
	scanner.ScanAll()
	scanner.ScanAll()

	var clips int
	db.Model(&models.Clip{}).Where("event = ?", "Recent").Count(&clips)
	var files []models.VideoFile
	db.Find(&files)
	if clips != 1 || len(files) != 1 {
		t.Fatalf("remaining 14:19 footage duplicated: %d clips, %d file rows; want 1 and 1", clips, len(files))
	}
	if filepath.Base(files[0].FilePath) != "2024-03-10_14-19-01-front.mp4" {
		t.Fatalf("surviving file = %s, want 14:19", files[0].FilePath)
	}
}

// TestHandleFileCreate_ScansNestedEvents (P2): a moved directory tree must
// index nested event folders, not only the outer folder.
func TestHandleFileCreate_ScansNestedEvents(t *testing.T) {
	os.Setenv("DEFAULT_TIMEZONE", "UTC")
	defer os.Unsetenv("DEFAULT_TIMEZONE")

	db := setupFableADB(t)
	defer db.Close()

	tmpDir, err := ioutil.TempDir("", "fable_nested")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(tmpDir)

	if err := os.MkdirAll(filepath.Join(tmpDir, "SentryClips"), 0755); err != nil {
		t.Fatal(err)
	}

	staged, err := ioutil.TempDir("", "fable_nested_staged")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(staged)

	bundle := filepath.Join(staged, "bundle")
	writeDummyMP4(t, filepath.Join(bundle, "2024-04-01_09-00-00-front.mp4"))
	if err := ioutil.WriteFile(filepath.Join(bundle, "event.json"), []byte(`{"timestamp":"2024-04-01T09:00:10","reason":"outer"}`), 0644); err != nil {
		t.Fatal(err)
	}
	nested := filepath.Join(bundle, "nested")
	writeDummyMP4(t, filepath.Join(nested, "2024-04-01_10-00-00-front.mp4"))
	if err := ioutil.WriteFile(filepath.Join(nested, "event.json"), []byte(`{"timestamp":"2024-04-01T10:00:10","reason":"nested"}`), 0644); err != nil {
		t.Fatal(err)
	}

	dest := filepath.Join(tmpDir, "SentryClips", "bundle")
	if err := os.Rename(bundle, dest); err != nil {
		t.Fatal(err)
	}
	nestedDest := filepath.Join(dest, "nested")

	scanner := NewScannerService(tmpDir, db)
	scanner.handleFileCreate(dest)

	deadline := time.Now().Add(5 * time.Second)
	for {
		var outer, inner int
		db.Model(&models.Clip{}).Where("source_dir = ?", dest).Count(&outer)
		db.Model(&models.Clip{}).Where("source_dir = ?", nestedDest).Count(&inner)
		if outer == 1 && inner == 1 {
			return
		}
		if time.Now().After(deadline) {
			t.Fatalf("moved tree indexed outer=%d nested=%d; want 1 and 1", outer, inner)
		}
		time.Sleep(50 * time.Millisecond)
	}
}

// TestScanAll_ConcurrentRecentIsIdempotent (P3): 32 overlapping Recent
// ScanAll calls must not insert extra clips or file rows.
func TestScanAll_ConcurrentRecentIsIdempotent(t *testing.T) {
	os.Setenv("DEFAULT_TIMEZONE", "UTC")
	defer os.Unsetenv("DEFAULT_TIMEZONE")

	db := setupFableADB(t)
	defer db.Close()

	tmpDir, err := ioutil.TempDir("", "fable_recent_race")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(tmpDir)

	recentDir := filepath.Join(tmpDir, "RecentClips")
	names := []string{
		"2024-03-10_14-18-01-front.mp4",
		"2024-03-10_14-18-01-back.mp4",
		"2024-03-10_14-19-01-front.mp4",
		"2024-03-10_14-19-01-back.mp4",
		"2024-03-10_14-19-01-left_repeater.mp4",
	}
	for _, name := range names {
		writeDummyMP4(t, filepath.Join(recentDir, name))
	}

	scanner := NewScannerService(tmpDir, db)
	var wg sync.WaitGroup
	for i := 0; i < 32; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			scanner.ScanAll()
		}()
	}
	wg.Wait()

	var clips, files int
	db.Model(&models.Clip{}).Count(&clips)
	db.Model(&models.VideoFile{}).Count(&files)
	if clips != 1 || files != len(names) {
		t.Fatalf("concurrent Recent ScanAll: %d clips, %d file rows; want 1 and %d", clips, files, len(names))
	}
}
