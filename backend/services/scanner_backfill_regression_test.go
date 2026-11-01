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
		dir := filepath.Join(root, fmt.Sprint(i))
		if err := os.MkdirAll(dir, 0755); err != nil {
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
	scanner.backfillIncompleteMetadata()
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
