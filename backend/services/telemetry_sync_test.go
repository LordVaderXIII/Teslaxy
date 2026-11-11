package services

import (
	"encoding/json"
	"path/filepath"
	"testing"
	"time"

	"github.com/jinzhu/gorm"
	_ "github.com/jinzhu/gorm/dialects/sqlite"
	"teslaxy/models"
	pb "teslaxy/proto"
)

func TestTelemetryKeepsFileBoundariesIncludingEmptySEI(t *testing.T) {
	db, err := gorm.Open("sqlite3", ":memory:")
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	db.AutoMigrate(&models.Clip{}, &models.Telemetry{})
	scanner := NewScannerService(t.TempDir(), db)
	scanner.SEIExtractor = func(path string) ([]*pb.SeiMetadata, error) {
		switch filepath.Base(path) {
		case "2026-01-01_12-00-00-front.mp4":
			return []*pb.SeiMetadata{{FrameSeqNo: 100}, {FrameSeqNo: 101}}, nil
		case "2026-01-01_12-02-00-front.mp4":
			return []*pb.SeiMetadata{{FrameSeqNo: 900}, {FrameSeqNo: 901}, {FrameSeqNo: 902}}, nil
		default:
			return nil, nil
		}
	}
	start := time.Date(2026, 1, 1, 12, 0, 0, 0, time.UTC)
	files := []fileInfo{}
	for i, name := range []string{"2026-01-01_12-00-00-front.mp4", "2026-01-01_12-01-00-front.mp4", "2026-01-01_12-02-00-front.mp4"} {
		files = append(files, fileInfo{path: filepath.Join(scanner.FootagePath, name), timestamp: start.Add(time.Duration(i) * time.Minute)})
	}
	clip := models.Clip{}
	db.Create(&clip)
	scanner.aggregateTelemetry(&clip, files, 0, 0)
	var telemetry models.Telemetry
	db.First(&telemetry, clip.TelemetryID)
	encoded, _ := json.Marshal(telemetry)
	var fields map[string]interface{}
	json.Unmarshal(encoded, &fields)
	text, ok := fields["file_timing_json"].(string)
	if !ok || text == "" {
		t.Fatal("missing per-file telemetry boundaries")
	}
	var boundaries []struct {
		FilePath string `json:"file_path"`
		First    int    `json:"first_sample_index"`
		Count    int    `json:"sample_count"`
	}
	if err := json.Unmarshal([]byte(text), &boundaries); err != nil {
		t.Fatal(err)
	}
	if len(boundaries) != 3 {
		t.Fatalf("got %d boundaries, want 3", len(boundaries))
	}
	for i, want := range []struct{ first, count int }{{0, 2}, {2, 0}, {2, 3}} {
		if boundaries[i].First != want.first || boundaries[i].Count != want.count || boundaries[i].FilePath != files[i].path {
			t.Fatalf("boundary %d: %+v", i, boundaries[i])
		}
	}
	var flat []*pb.SeiMetadata
	json.Unmarshal([]byte(telemetry.FullDataJson), &flat)
	if len(flat) != 5 || flat[2].FrameSeqNo != 900 {
		t.Fatal("flat array compatibility lost")
	}
}
