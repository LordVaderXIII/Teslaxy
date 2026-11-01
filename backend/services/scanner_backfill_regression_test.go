package services

import (
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"testing"
	"time"

	"teslaxy/models"
)

func TestBackfillPerClipEventCoordinates(t *testing.T) {
	db := testDB(t)
	// ScanAll processes event groups concurrently; :memory: is per connection.
	db.DB().SetMaxOpenConns(1)
	root := t.TempDir()
	scanner := NewScannerService(root, db)
	scanner.SEIExtractor = noopSEI
	// Use the shipped GeoNames index, not a mock of the expected suburbs.
	cases := []struct {
		event, name string
		lat, lon    float64
	}{
		{"Sentry", "Salisbury Plain", -34.77265, 138.67071},
		{"Saved", "Parkside", -34.9450, 138.6130},
	}
	var ids []uint
	for i, tc := range cases {
		dir := filepath.Join(root, tc.event+"Clips", fmt.Sprint(i))
		if err := os.MkdirAll(dir, 0755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(dir, "2026-11-01_12-00-00-front.mp4"), []byte("fixture"), 0644); err != nil {
			t.Fatal(err)
		}
		data := fmt.Sprintf(`{"est_lat":%f,"est_lon":%f,"reason":"sentry_aware_object_detection"}`, tc.lat, tc.lon)
		if err := os.WriteFile(filepath.Join(dir, "event.json"), []byte(data), 0644); err != nil {
			t.Fatal(err)
		}
		clip := models.Clip{Timestamp: time.Now(), Event: tc.event, City: "Hope Valley", Reason: "sentry_aware_object_detection", SourceDir: dir}
		if err := db.Create(&clip).Error; err != nil {
			t.Fatal(err)
		}
		ids = append(ids, clip.ID)
	}
	scanner.ScanAll()
	var first []models.Clip
	if err := db.Order("id").Find(&first).Error; err != nil {
		t.Fatal(err)
	}
	for i, tc := range cases {
		if first[i].City != tc.name {
			t.Errorf("clip %d city=%q want %q", ids[i], first[i].City, tc.name)
		}
	}
	// ScanAll must run the same repair without changing any persisted fields.
	scanner.ScanAll()
	var second []models.Clip
	if err := db.Order("id").Find(&second).Error; err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(first, second) {
		t.Errorf("second scan changed clips: before=%+v after=%+v", first, second)
	}
}

func TestBackfillTelemetryNeverStoredCity(t *testing.T) {
	for _, event := range []string{"Sentry", "Saved", "Recent"} {
		for _, storedCity := range []string{"Hope Valley", "-34.8514, 138.6923"} {
			t.Run(event+"/"+storedCity, func(t *testing.T) {
				db := testDB(t)
				root := t.TempDir()
				scanner := NewScannerService(root, db)
				// Empty event coordinates must fall back to this clip's telemetry.
				if err := os.WriteFile(filepath.Join(root, "event.json"), []byte(`{"city":""}`), 0644); err != nil {
					t.Fatal(err)
				}
				clip := models.Clip{Event: event, City: storedCity, SourceDir: root}
				if err := db.Create(&clip).Error; err != nil {
					t.Fatal(err)
				}
				point := models.Telemetry{
					ClipID: clip.ID, Latitude: -34.77265, Longitude: 138.67071,
					Speed: 42, Gear: "DRIVE", SteeringAngle: 3, AutopilotState: "ACTIVE",
					FullDataJson: `[{"latitude_deg":-34.77265,"longitude_deg":138.67071}]`,
				}
				if err := db.Create(&point).Error; err != nil {
					t.Fatal(err)
				}
				var beforeTelemetry models.Telemetry
				if err := db.First(&beforeTelemetry, point.ID).Error; err != nil {
					t.Fatal(err)
				}
				// No TelemetryID: association must be by ownership (clip_id).
				scanner.backfillIncompleteMetadata()
				var afterTelemetry models.Telemetry
				if err := db.First(&afterTelemetry, point.ID).Error; err != nil {
					t.Fatal(err)
				}
				if !reflect.DeepEqual(beforeTelemetry, afterTelemetry) {
					t.Errorf("metadata backfill rewrote telemetry: before=%+v after=%+v", beforeTelemetry, afterTelemetry)
				}
				var first models.Clip
				if err := db.First(&first, clip.ID).Error; err != nil {
					t.Fatal(err)
				}
				if first.City != "Salisbury Plain" {
					t.Fatalf("city=%q want Salisbury Plain", first.City)
				}
				scanner.backfillIncompleteMetadata()
				var second models.Clip
				if err := db.First(&second, clip.ID).Error; err != nil {
					t.Fatal(err)
				}
				if !reflect.DeepEqual(first, second) {
					t.Fatal("second backfill changed persisted fields")
				}
			})
		}
	}
}

func TestBackfillNoOwnCoordinatesLeavesCityUnchanged(t *testing.T) {
	db := testDB(t)
	scanner := NewScannerService(t.TempDir(), db)
	clip := models.Clip{Event: "Recent", City: "-34.8514, 138.6923"}
	if err := db.Create(&clip).Error; err != nil {
		t.Fatal(err)
	}
	// Compare persisted representations (SQLite removes Go's monotonic clock).
	if err := db.First(&clip, clip.ID).Error; err != nil {
		t.Fatal(err)
	}
	// An unrelated clip's valid telemetry must never be used either.
	point := models.Telemetry{ClipID: clip.ID + 1, Latitude: -34.77265, Longitude: 138.67071}
	if err := db.Create(&point).Error; err != nil {
		t.Fatal(err)
	}
	scanner.backfillIncompleteMetadata()
	var got models.Clip
	if err := db.First(&got, clip.ID).Error; err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(clip, got) {
		t.Fatalf("changed clip without own coordinates: %+v", got)
	}
}

func TestBackfillReasonFromEvent(t *testing.T) {
	for _, event := range []string{"Sentry", "Saved"} {
		for _, reason := range []string{"", "sentry_aware_object_detection"} {
			t.Run(event+"/"+reason, func(t *testing.T) {
				db := testDB(t)
				root := t.TempDir()
				scanner := NewScannerService(root, db)
				scanner.SEIExtractor = noopSEI
				if err := os.WriteFile(filepath.Join(root, "2026-11-01_12-00-00-front.mp4"), []byte("fixture"), 0644); err != nil {
					t.Fatal(err)
				}
				data := `{ "city":"Tesla supplied city", "est_lat":-34.77265, "est_lon":138.67071 }`
				if reason != "" {
					data = fmt.Sprintf(`{"city":"Tesla supplied city","reason":%q,"est_lat":-34.77265,"est_lon":138.67071}`, reason)
				}
				path := filepath.Join(root, "event.json")
				if err := os.WriteFile(path, []byte(data), 0644); err != nil {
					t.Fatal(err)
				}
				ev, err := readEventJSONFile(path)
				if err != nil {
					t.Fatal(err)
				}
				if ev.Reason != reason {
					t.Fatalf("parser reason=%q want %q", ev.Reason, reason)
				}
				clip := models.Clip{Event: event, City: "Hope Valley", SourceDir: root}
				if err := db.Create(&clip).Error; err != nil {
					t.Fatal(err)
				}
				scanner.ScanAll()
				var got models.Clip
				if err := db.First(&got, clip.ID).Error; err != nil {
					t.Fatal(err)
				}
				if got.Reason != reason {
					t.Errorf("stored reason=%q want %q", got.Reason, reason)
				}
				if got.City != "Tesla supplied city" {
					t.Errorf("event city lost: %q", got.City)
				}
			})
		}
	}
}
