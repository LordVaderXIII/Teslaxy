package services

import (
	"encoding/json"
	"io/ioutil"
	"os"
	"path/filepath"
	"testing"

	"github.com/jinzhu/gorm"
	_ "github.com/jinzhu/gorm/dialects/sqlite"
	"teslaxy/models"
	pb "teslaxy/proto"
)

const (
	testEventLat = 37.7749
	testEventLon = -122.4194
	testSEILat   = 37.71
	testSEILon   = -122.51
)

type incidentScanFixture struct {
	db      *gorm.DB
	scanner *ScannerService
	tmpDir  string
}

func setupIncidentScan(t *testing.T, eventData map[string]interface{}, seiLat, seiLon float64) *incidentScanFixture {
	t.Helper()
	os.Setenv("DEFAULT_TIMEZONE", "UTC")
	t.Cleanup(func() { os.Unsetenv("DEFAULT_TIMEZONE") })

	db, err := gorm.Open("sqlite3", ":memory:")
	if err != nil {
		t.Fatalf("failed to connect database: %v", err)
	}
	t.Cleanup(func() { db.Close() })
	db.AutoMigrate(&models.Clip{}, &models.VideoFile{}, &models.Telemetry{})

	tmpDir, err := ioutil.TempDir("", "scanner_incident_pos")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { os.RemoveAll(tmpDir) })

	ts := "2024-03-01_12-00-00"
	clipDir := filepath.Join(tmpDir, "SavedClips", ts)
	if err := os.MkdirAll(clipDir, 0755); err != nil {
		t.Fatal(err)
	}
	mp4Path := filepath.Join(clipDir, ts+"-front.mp4")
	if err := ioutil.WriteFile(mp4Path, []byte("dummy"), 0644); err != nil {
		t.Fatal(err)
	}
	if eventData != nil {
		eventBytes, err := json.Marshal(eventData)
		if err != nil {
			t.Fatal(err)
		}
		if err := ioutil.WriteFile(filepath.Join(clipDir, "event.json"), eventBytes, 0644); err != nil {
			t.Fatal(err)
		}
	}

	scanner := NewScannerService(tmpDir, db)
	scanner.SEIExtractor = func(path string) ([]*pb.SeiMetadata, error) {
		return []*pb.SeiMetadata{
			{
				VehicleSpeedMps:    10,
				GearState:          pb.SeiMetadata_GEAR_DRIVE,
				LatitudeDeg:        seiLat,
				LongitudeDeg:       seiLon,
				SteeringWheelAngle: 12.5,
				AutopilotState:     pb.SeiMetadata_NONE,
			},
		}, nil
	}

	return &incidentScanFixture{db: db, scanner: scanner, tmpDir: tmpDir}
}

func defaultEventJSON(lat, lon interface{}) map[string]interface{} {
	data := map[string]interface{}{
		"timestamp": "2024-03-01T12:00:00",
		"city":      "Test City",
		"reason":    "sentry_aware_object_detection",
	}
	if lat != nil {
		data["est_lat"] = lat
	}
	if lon != nil {
		data["est_lon"] = lon
	}
	return data
}

func loadSingleClip(t *testing.T, db *gorm.DB) models.Clip {
	t.Helper()
	var clip models.Clip
	if err := db.Preload("Telemetry").First(&clip).Error; err != nil {
		t.Fatalf("failed to find clip: %v", err)
	}
	return clip
}

func assertCoords(t *testing.T, gotLat, gotLon, wantLat, wantLon float64) {
	t.Helper()
	if gotLat < wantLat-0.0001 || gotLat > wantLat+0.0001 {
		t.Errorf("expected Latitude %f, got %f", wantLat, gotLat)
	}
	if gotLon < wantLon-0.0001 || gotLon > wantLon+0.0001 {
		t.Errorf("expected Longitude %f, got %f", wantLon, gotLon)
	}
}

func TestScanner_IncidentPosition_ValidEventVsDifferentSEI(t *testing.T) {
	fx := setupIncidentScan(t, defaultEventJSON(testEventLat, testEventLon), testSEILat, testSEILon)
	fx.scanner.ScanAll()

	clip := loadSingleClip(t, fx.db)
	assertCoords(t, clip.Telemetry.Latitude, clip.Telemetry.Longitude, testEventLat, testEventLon)

	if clip.Telemetry.FullDataJson == "" {
		t.Errorf("expected FullDataJson to be non-empty from SEI")
	}
	wantSpeed := float32(10 * 2.23694)
	if clip.Telemetry.Speed < wantSpeed-0.01 || clip.Telemetry.Speed > wantSpeed+0.01 {
		t.Errorf("expected Speed from SEI ~%f, got %f", wantSpeed, clip.Telemetry.Speed)
	}
	if clip.Telemetry.Gear != pb.SeiMetadata_GEAR_DRIVE.String() {
		t.Errorf("expected Gear from SEI %q, got %q", pb.SeiMetadata_GEAR_DRIVE.String(), clip.Telemetry.Gear)
	}
}

func TestScanner_IncidentPosition_ValidEventVsZeroSEI(t *testing.T) {
	fx := setupIncidentScan(t, defaultEventJSON(testEventLat, testEventLon), 0, 0)
	fx.scanner.ScanAll()

	clip := loadSingleClip(t, fx.db)
	assertCoords(t, clip.Telemetry.Latitude, clip.Telemetry.Longitude, testEventLat, testEventLon)
	if clip.City == "0.0000, 0.0000" {
		t.Errorf("must not invent city from a zero SEI midpoint")
	}
}

func TestScanner_IncidentPosition_InvalidEventVsValidSEI(t *testing.T) {
	t.Run("omitted est_lat/est_lon", func(t *testing.T) {
		fx := setupIncidentScan(t, defaultEventJSON(nil, nil), testSEILat, testSEILon)
		fx.scanner.ScanAll()
		clip := loadSingleClip(t, fx.db)
		assertCoords(t, clip.Telemetry.Latitude, clip.Telemetry.Longitude, testSEILat, testSEILon)
	})
	t.Run("zero event coords", func(t *testing.T) {
		fx := setupIncidentScan(t, defaultEventJSON(0, 0), testSEILat, testSEILon)
		fx.scanner.ScanAll()
		clip := loadSingleClip(t, fx.db)
		assertCoords(t, clip.Telemetry.Latitude, clip.Telemetry.Longitude, testSEILat, testSEILon)
	})
}

func TestScanner_IncidentPosition_BothInvalid(t *testing.T) {
	fx := setupIncidentScan(t, defaultEventJSON(0, 0), 0, 0)
	fx.scanner.ScanAll()

	clip := loadSingleClip(t, fx.db)
	assertCoords(t, clip.Telemetry.Latitude, clip.Telemetry.Longitude, 0, 0)
}

func TestScanner_IncidentPosition_RepeatedScanAllIdempotent(t *testing.T) {
	fx := setupIncidentScan(t, defaultEventJSON(testEventLat, testEventLon), testSEILat, testSEILon)
	fx.scanner.ScanAll()

	clip1 := loadSingleClip(t, fx.db)
	var telCount1 int
	fx.db.Model(&models.Telemetry{}).Count(&telCount1)
	if telCount1 != 1 {
		t.Fatalf("expected 1 telemetry row after first scan, got %d", telCount1)
	}
	telemetryID := clip1.TelemetryID
	lat, lon := clip1.Telemetry.Latitude, clip1.Telemetry.Longitude

	fx.scanner.ScanAll()

	clip2 := loadSingleClip(t, fx.db)
	var telCount2 int
	fx.db.Model(&models.Telemetry{}).Count(&telCount2)
	if telCount2 != 1 {
		t.Errorf("expected 1 telemetry row after second ScanAll, got %d", telCount2)
	}
	if clip2.TelemetryID != telemetryID {
		t.Errorf("expected TelemetryID %d unchanged, got %d", telemetryID, clip2.TelemetryID)
	}
	assertCoords(t, clip2.Telemetry.Latitude, clip2.Telemetry.Longitude, lat, lon)
	assertCoords(t, clip2.Telemetry.Latitude, clip2.Telemetry.Longitude, testEventLat, testEventLon)
}

func TestScanner_IncidentPosition_RescanRestoresEventOverStoredSEI(t *testing.T) {
	// Prospective correction path for already-clobbered rows: event.json is re-read on
	// ScanAll and must replace a stored SEI midpoint without creating a second telemetry row.
	// This is the idempotent repair once a reviewed live dry-run is authorised; this test
	// uses synthetic fixtures only.
	fx := setupIncidentScan(t, defaultEventJSON(testEventLat, testEventLon), testSEILat, testSEILon)
	fx.scanner.ScanAll()

	clip := loadSingleClip(t, fx.db)
	if err := fx.db.Model(&models.Telemetry{}).Where("id = ?", clip.TelemetryID).
		Updates(map[string]interface{}{"latitude": testSEILat, "longitude": testSEILon}).Error; err != nil {
		t.Fatal(err)
	}
	clobbered := loadSingleClip(t, fx.db)
	assertCoords(t, clobbered.Telemetry.Latitude, clobbered.Telemetry.Longitude, testSEILat, testSEILon)

	fx.scanner.ScanAll()

	restored := loadSingleClip(t, fx.db)
	assertCoords(t, restored.Telemetry.Latitude, restored.Telemetry.Longitude, testEventLat, testEventLon)
	if restored.TelemetryID != clip.TelemetryID {
		t.Errorf("expected TelemetryID %d unchanged after repair scan, got %d", clip.TelemetryID, restored.TelemetryID)
	}
	var telCount int
	fx.db.Model(&models.Telemetry{}).Count(&telCount)
	if telCount != 1 {
		t.Errorf("expected 1 telemetry row after repair scan, got %d", telCount)
	}
}

func TestScanner_IncidentPosition_SummaryFieldsAreIncidentPoint(t *testing.T) {
	// GET-level summary uses the same telemetry lat/lon columns as /api/clips preload.
	fx := setupIncidentScan(t, defaultEventJSON(testEventLat, testEventLon), testSEILat, testSEILon)
	fx.scanner.ScanAll()

	var clip models.Clip
	if err := fx.db.Select("id, timestamp, event_timestamp, event, city, reason, source_dir, telemetry_id").
		Preload("Telemetry", func(db *gorm.DB) *gorm.DB {
			return db.Select("id, clip_id, latitude, longitude, speed, gear, steering_angle, autopilot_state")
		}).First(&clip).Error; err != nil {
		t.Fatalf("failed to load summary clip: %v", err)
	}
	assertCoords(t, clip.Telemetry.Latitude, clip.Telemetry.Longitude, testEventLat, testEventLon)
	if clip.Telemetry.FullDataJson != "" {
		t.Errorf("summary select must not load full_data_json")
	}
}
