package services

import (
	"io/ioutil"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/jinzhu/gorm"
	_ "github.com/jinzhu/gorm/dialects/sqlite"
	"teslaxy/models"
	pb "teslaxy/proto"
)

func testDB(t *testing.T) *gorm.DB {
	t.Helper()
	db, err := gorm.Open("sqlite3", ":memory:")
	if err != nil {
		t.Fatalf("db: %v", err)
	}
	t.Cleanup(func() { db.Close() })
	db.AutoMigrate(&models.Clip{}, &models.VideoFile{}, &models.Telemetry{})
	return db
}

func noopSEI(path string) ([]*pb.SeiMetadata, error) {
	return nil, nil
}

func TestReadEventJSONFile_RetriesHalfWritten(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "event.json")

	// Start with truncated JSON, then complete it after a short delay so
	// attempt 2+ succeeds (simulates Tesla finishing the write).
	if err := ioutil.WriteFile(path, []byte(`{"city":"Park`), 0644); err != nil {
		t.Fatal(err)
	}
	done := make(chan struct{})
	go func() {
		time.Sleep(600 * time.Millisecond)
		_ = ioutil.WriteFile(path, []byte(`{
			"timestamp":"2026-11-01T20:27:00",
			"city":"Fairview Park",
			"reason":"sentry_aware_object_detection",
			"est_lat":-34.8005,
			"est_lon":138.7207
		}`), 0644)
		close(done)
	}()

	ev, err := readEventJSONFile(path)
	<-done
	if err != nil {
		t.Fatalf("readEventJSONFile: %v", err)
	}
	if ev.City != "Fairview Park" {
		t.Errorf("city=%q", ev.City)
	}
	if ev.Reason != "sentry_aware_object_detection" {
		t.Errorf("reason=%q", ev.Reason)
	}
}

func TestScanner_HalfWrittenEventJSON_ThenBackfill(t *testing.T) {
	os.Setenv("DEFAULT_TIMEZONE", "UTC")
	defer os.Unsetenv("DEFAULT_TIMEZONE")

	db := testDB(t)
	tmp := t.TempDir()
	config := t.TempDir()
	os.Setenv("CONFIG_PATH", config)
	defer os.Unsetenv("CONFIG_PATH")

	clipDir := filepath.Join(tmp, "SentryClips", "2026-11-01_20-27-00")
	if err := os.MkdirAll(clipDir, 0755); err != nil {
		t.Fatal(err)
	}
	if err := ioutil.WriteFile(filepath.Join(clipDir, "2026-11-01_20-27-00-front.mp4"), []byte("dummy"), 0644); err != nil {
		t.Fatal(err)
	}

	scanner := NewScannerService(tmp, db)
	scanner.Geocoder = &StubGeocoder{Names: map[string]string{
		CacheKey(-34.8005, 138.7207): "Fairview Park",
	}}
	scanner.SEIExtractor = noopSEI

	// Simulate a clip that was indexed from a half-written event.json.
	clip := models.Clip{
		Timestamp: time.Date(2026, 11, 1, 20, 27, 0, 0, time.UTC),
		Event:     "Sentry",
		City:      "",
		Reason:    "",
		SourceDir: clipDir,
	}
	if err := db.Create(&clip).Error; err != nil {
		t.Fatal(err)
	}

	// Now Tesla finishes writing event.json.
	full := `{
		"timestamp":"2026-11-01T20:27:10",
		"city":"Fairview Park",
		"reason":"sentry_aware_object_detection",
		"est_lat":-34.8005,
		"est_lon":138.7207
	}`
	if err := ioutil.WriteFile(filepath.Join(clipDir, "event.json"), []byte(full), 0644); err != nil {
		t.Fatal(err)
	}

	scanner.backfillIncompleteMetadata()

	var got models.Clip
	if err := db.First(&got, clip.ID).Error; err != nil {
		t.Fatal(err)
	}
	if got.City != "Fairview Park" {
		t.Errorf("city=%q want Fairview Park", got.City)
	}
	if got.Reason != "sentry_aware_object_detection" {
		t.Errorf("reason=%q", got.Reason)
	}
}

func TestScanner_BackfillCoordinateCity(t *testing.T) {
	db := testDB(t)
	scanner := NewScannerService(t.TempDir(), db)
	scanner.Geocoder = &StubGeocoder{Names: map[string]string{
		CacheKey(-34.8005, 138.7207): "Modbury Heights",
		CacheKey(-34.9450, 138.6130): "Parkside",
	}}
	scanner.SEIExtractor = noopSEI

	c1 := models.Clip{
		Timestamp: time.Now(),
		Event:     "Recent",
		City:      "-34.8005, 138.7207",
	}
	c2 := models.Clip{
		Timestamp: time.Now(),
		Event:     "Sentry",
		City:      "-34.9450, 138.6130",
		Reason:    "sentry_aware_object_detection",
		SourceDir: "/unused",
	}
	db.Create(&c1)
	db.Create(&c2)
	// Stored city strings are not evidence of location. Use each clip's own telemetry.
	for _, point := range []models.Telemetry{
		{ClipID: c1.ID, Latitude: -34.8005, Longitude: 138.7207},
		{ClipID: c2.ID, Latitude: -34.9450, Longitude: 138.6130},
	} {
		if err := db.Create(&point).Error; err != nil {
			t.Fatal(err)
		}
	}

	scanner.backfillIncompleteMetadata()

	var got1, got2 models.Clip
	db.First(&got1, c1.ID)
	db.First(&got2, c2.ID)
	if got1.City != "Modbury Heights" {
		t.Errorf("recent city=%q want Modbury Heights", got1.City)
	}
	if got2.City != "Parkside" {
		t.Errorf("sentry city=%q want Parkside", got2.City)
	}
}

func TestScanner_TeslaCityWinsOverGeocode(t *testing.T) {
	db := testDB(t)
	tmp := t.TempDir()
	clipDir := filepath.Join(tmp, "SentryClips", "2026-02-18_16-28-00")
	os.MkdirAll(clipDir, 0755)
	ioutil.WriteFile(filepath.Join(clipDir, "2026-02-18_16-28-00-front.mp4"), []byte("x"), 0644)
	ioutil.WriteFile(filepath.Join(clipDir, "event.json"), []byte(`{
		"timestamp":"2026-02-18T16:28:00",
		"city":"Parkside",
		"reason":"sentry_aware_object_detection",
		"est_lat":-34.9450,
		"est_lon":138.6130
	}`), 0644)

	scanner := NewScannerService(tmp, db)
	scanner.Geocoder = &StubGeocoder{Names: map[string]string{
		CacheKey(-34.9450, 138.6130): "SHOULD_NOT_WIN",
	}}
	scanner.SEIExtractor = noopSEI
	scanner.ScanAll()

	var clip models.Clip
	if err := db.First(&clip).Error; err != nil {
		t.Fatal(err)
	}
	if clip.City != "Parkside" {
		t.Errorf("Tesla city must win, got %q", clip.City)
	}
}

func TestScanner_GeocodeWhenTeslaCityEmpty(t *testing.T) {
	db := testDB(t)
	tmp := t.TempDir()
	clipDir := filepath.Join(tmp, "SentryClips", "2026-11-01_20-08-00")
	os.MkdirAll(clipDir, 0755)
	ioutil.WriteFile(filepath.Join(clipDir, "2026-11-01_20-08-00-front.mp4"), []byte("x"), 0644)
	ioutil.WriteFile(filepath.Join(clipDir, "event.json"), []byte(`{
		"timestamp":"2026-11-01T20:08:00",
		"city":"",
		"reason":"sentry_aware_object_detection",
		"est_lat":-34.8005,
		"est_lon":138.7207
	}`), 0644)

	scanner := NewScannerService(tmp, db)
	scanner.Geocoder = &StubGeocoder{Names: map[string]string{
		CacheKey(-34.8005, 138.7207): "Fairview Park",
	}}
	scanner.SEIExtractor = noopSEI
	scanner.ScanAll()

	var clip models.Clip
	if err := db.First(&clip).Error; err != nil {
		t.Fatal(err)
	}
	if clip.City != "Fairview Park" {
		t.Errorf("city=%q want Fairview Park (geocoded)", clip.City)
	}
	if clip.Reason != "sentry_aware_object_detection" {
		t.Errorf("reason=%q", clip.Reason)
	}
}
