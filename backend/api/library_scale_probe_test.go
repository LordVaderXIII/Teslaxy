package api

import (
	"encoding/json"
	"fmt"
	"io/ioutil"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/jinzhu/gorm"
	_ "github.com/jinzhu/gorm/dialects/sqlite"
	"teslaxy/database"
	"teslaxy/models"
)

func setupLibraryFileRouter(t *testing.T) *gin.Engine {
	t.Helper()
	os.Unsetenv("AUTH_ENABLED")
	gin.SetMode(gin.TestMode)

	dir, err := ioutil.TempDir("", "teslaxy-library-scale")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { os.RemoveAll(dir) })

	db, err := gorm.Open("sqlite3", filepath.Join(dir, "probe.db"))
	if err != nil {
		t.Fatalf("failed to open db: %v", err)
	}
	t.Cleanup(func() { db.Close() })
	db.Exec("PRAGMA synchronous = OFF")
	db.Exec("PRAGMA journal_mode = MEMORY")
	db.AutoMigrate(&models.Clip{}, &models.Telemetry{}, &models.VideoFile{})
	database.DB = db

	r := gin.New()
	SetupRoutes(r)
	return r
}

func seedLibraryScaleFast(t *testing.T, nClips, nFiles int) {
	t.Helper()
	tx := database.DB.Begin()
	if tx.Error != nil {
		t.Fatal(tx.Error)
	}
	now := time.Now().UTC()
	for i := 0; i < nClips; i++ {
		ts := time.Date(2024, 6, 10, 0, 0, 0, 0, time.UTC).Add(time.Duration(i) * time.Minute)
		eventTS := ts.Add(90 * time.Second)
		clip := models.Clip{
			Timestamp:      ts,
			EventTimestamp: &eventTS,
			Event:          "Sentry",
			Reason:         "sentry_aware_object_detection",
			SourceDir:      fmt.Sprintf("/synthetic/scale/%d", i),
		}
		if err := tx.Create(&clip).Error; err != nil {
			tx.Rollback()
			t.Fatalf("create clip: %v", err)
		}
		tel := models.Telemetry{
			ClipID:       clip.ID,
			Latitude:     37.7749,
			Longitude:    -122.4194,
			FullDataJson: bulkyTelemetryJSON,
		}
		if err := tx.Create(&tel).Error; err != nil {
			tx.Rollback()
			t.Fatalf("create telemetry: %v", err)
		}
		if err := tx.Model(&clip).Update("telemetry_id", tel.ID).Error; err != nil {
			tx.Rollback()
			t.Fatalf("link telemetry: %v", err)
		}

		var b strings.Builder
		args := make([]interface{}, 0, nFiles*6)
		b.WriteString("INSERT INTO video_files (clip_id, camera, file_path, timestamp, created_at, updated_at) VALUES ")
		for j := 0; j < nFiles; j++ {
			if j > 0 {
				b.WriteByte(',')
			}
			b.WriteString("(?,?,?,?,?,?)")
			cam := fmt.Sprintf("Cam%d", j)
			if j == 0 {
				cam = "Front"
			}
			args = append(args, clip.ID, cam, fmt.Sprintf("/synthetic/scale/%d/%s.mp4", i, cam),
				ts.Add(time.Duration(j)*time.Second), now, now)
		}
		if err := tx.Exec(b.String(), args...).Error; err != nil {
			tx.Rollback()
			t.Fatalf("insert video files: %v", err)
		}
	}
	if err := tx.Commit().Error; err != nil {
		t.Fatalf("commit scale seed: %v", err)
	}
}

func timeRoute(t *testing.T, r http.Handler, path string, repeats int) (avgMs float64, lastBytes int) {
	t.Helper()
	var total time.Duration
	for i := 0; i < repeats; i++ {
		w := httptest.NewRecorder()
		req, _ := http.NewRequest("GET", path, nil)
		start := time.Now()
		r.ServeHTTP(w, req)
		total += time.Since(start)
		lastBytes = w.Body.Len()
		if w.Code != http.StatusOK {
			body := w.Body.String()
			if len(body) > 200 {
				body = body[:200]
			}
			t.Fatalf("%s status %d on sample %d body %s", path, w.Code, i, body)
		}
	}
	avgMs = float64(total.Microseconds()) / 1000.0 / float64(repeats)
	return avgMs, lastBytes
}

func TestLibraryVsClipsScale2000(t *testing.T) {
	if os.Getenv("TESLAXY_LIBRARY_SCALE") != "1" {
		t.Skip("set TESLAXY_LIBRARY_SCALE=1 to run 2000x12 file-sqlite probe")
	}
	r := setupLibraryFileRouter(t)
	nClips := 2000
	if raw := os.Getenv("TESLAXY_LIBRARY_SCALE_N"); raw != "" {
		var n int
		if _, err := fmt.Sscanf(raw, "%d", &n); err == nil && n > 0 {
			nClips = n
		}
	}
	const nFiles = 12
	const repeats = 5
	seedLibraryScaleFast(t, nClips, nFiles)

	libAvg, libBytes := timeRoute(t, r, "/api/library", repeats)
	clipsAvg, clipsBytes := timeRoute(t, r, "/api/clips", repeats)

	w := httptest.NewRecorder()
	req, _ := http.NewRequest("GET", "/api/library", nil)
	r.ServeHTTP(w, req)
	var resp libraryTestResponse
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("library json: %v", err)
	}
	if len(resp.Events) != nClips {
		t.Fatalf("library events = %d, want %d", len(resp.Events), nClips)
	}
	if strings.Contains(w.Body.String(), `"video_files"`) || strings.Contains(w.Body.String(), "full_data_json") {
		t.Errorf("library body must omit video_files and full_data_json")
	}
	if resp.Events[0].VideoFileCount != nFiles {
		t.Errorf("video_file_count = %d, want %d", resp.Events[0].VideoFileCount, nFiles)
	}

	ratio := 0.0
	if clipsBytes > 0 {
		ratio = float64(libBytes) / float64(clipsBytes)
	}
	t.Logf("SCALE_%dx%d repeats=%d library_avg_ms=%.1f clips_avg_ms=%.1f library_bytes=%d clips_bytes=%d ratio=%.4f",
		nClips, nFiles, repeats, libAvg, clipsAvg, libBytes, clipsBytes, ratio)

	if libAvg >= clipsAvg {
		t.Errorf("library_avg_ms=%.1f is not faster than clips_avg_ms=%.1f on %dx%d", libAvg, clipsAvg, nClips, nFiles)
	}
	if nClips == 2000 && libAvg > 150 {
		t.Errorf("library_avg_ms=%.1f exceeds 150ms parent bar vs 277ms QA residual", libAvg)
	}
}
