package api

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/jinzhu/gorm"
	_ "github.com/jinzhu/gorm/dialects/sqlite"
	"teslaxy/database"
	"teslaxy/models"
)

type libraryTestEvent struct {
	ID                 uint            `json:"id"`
	Timestamp          time.Time       `json:"timestamp"`
	EventTimestamp     *time.Time      `json:"event_timestamp"`
	Event              string          `json:"event"`
	City               string          `json:"city"`
	Reason             string          `json:"reason"`
	SourceDir          string          `json:"source_dir"`
	Latitude           *float64        `json:"latitude"`
	Longitude          *float64        `json:"longitude"`
	VideoFileCount     int             `json:"video_file_count"`
	PreviewCamera      string          `json:"preview_camera"`
	PreviewPath        string          `json:"preview_path"`
	PreviewTimestamp   *time.Time      `json:"preview_timestamp"`
	PreviewSeekSeconds float64         `json:"preview_seek_seconds"`
	VideoFiles         json.RawMessage `json:"video_files"`
	FullDataJSON       json.RawMessage `json:"full_data_json"`
}

type libraryTestResponse struct {
	Events []libraryTestEvent `json:"events"`
	Facets struct {
		Dates []struct {
			Date  string `json:"date"`
			Count int    `json:"count"`
		} `json:"dates"`
		Events []struct {
			Event string `json:"event"`
			Count int    `json:"count"`
		} `json:"events"`
		Reasons []struct {
			Reason string `json:"reason"`
			Count  int    `json:"count"`
		} `json:"reasons"`
	} `json:"facets"`
}

// ~2KiB FullDataJson so payload comparison is not an empty-string toy.
var bulkyTelemetryJSON = `{"samples":"` + strings.Repeat("x", 2048) + `"}`

func setupLibraryRouter(t *testing.T) *gin.Engine {
	t.Helper()
	os.Unsetenv("AUTH_ENABLED")
	gin.SetMode(gin.TestMode)

	db, err := gorm.Open("sqlite3", ":memory:")
	if err != nil {
		t.Fatalf("failed to open db: %v", err)
	}
	t.Cleanup(func() { db.Close() })
	db.AutoMigrate(&models.Clip{}, &models.Telemetry{}, &models.VideoFile{})
	database.DB = db

	r := gin.New()
	SetupRoutes(r)
	return r
}

func createClipWithTelemetry(t *testing.T, clip models.Clip, lat, lon float64, videoFiles []models.VideoFile) models.Clip {
	t.Helper()
	if err := database.DB.Create(&clip).Error; err != nil {
		t.Fatalf("create clip: %v", err)
	}
	tel := models.Telemetry{
		ClipID:       clip.ID,
		Latitude:     lat,
		Longitude:    lon,
		FullDataJson: bulkyTelemetryJSON,
	}
	if err := database.DB.Create(&tel).Error; err != nil {
		t.Fatalf("create telemetry: %v", err)
	}
	if err := database.DB.Model(&clip).Update("telemetry_id", tel.ID).Error; err != nil {
		t.Fatalf("link telemetry: %v", err)
	}
	clip.TelemetryID = tel.ID
	for i := range videoFiles {
		videoFiles[i].ClipID = clip.ID
		if err := database.DB.Create(&videoFiles[i]).Error; err != nil {
			t.Fatalf("create video file: %v", err)
		}
	}
	return clip
}

func sentryCameras(prefix string, n int) []models.VideoFile {
	names := []string{"Front", "Back", "Left Repeater", "Right Repeater", "Left Pillar", "Right Pillar"}
	out := make([]models.VideoFile, 0, n)
	for i := 0; i < n; i++ {
		cam := names[i%len(names)]
		if n > len(names) {
			cam = fmt.Sprintf("%s-%d", names[i%len(names)], i)
		}
		if i == 0 {
			cam = "Front"
		}
		out = append(out, models.VideoFile{
			Camera:    cam,
			FilePath:  fmt.Sprintf("/synthetic/%s/%s.mp4", prefix, strings.ReplaceAll(strings.ToLower(cam), " ", "_")),
			Timestamp: time.Date(2024, 6, 15, 14, 0, 0, 0, time.UTC).Add(time.Duration(i) * time.Second),
		})
	}
	return out
}

func seedLibraryCorpus(t *testing.T) (grouped models.Clip, adj1, adj2, saved, recent models.Clip) {
	t.Helper()
	// Grouping is scanner-owned; this endpoint must not split or merge rows.
	// The adjacent-minute pair below would historically merge on the client
	// (two physical clips, 60s apart, same Event, no source_dir) and MUST
	// remain two library rows.
	grouped = createClipWithTelemetry(t, models.Clip{
		Timestamp: time.Date(2024, 6, 15, 14, 0, 0, 0, time.UTC),
		Event:     "Sentry",
		City:      "Test City",
		Reason:    "sentry_aware_object_detection",
		SourceDir: "/synthetic/sentry/grouped-event",
	}, 37.7749, -122.4194, sentryCameras("grouped", 6))

	adj1 = createClipWithTelemetry(t, models.Clip{
		Timestamp: time.Date(2024, 6, 15, 15, 0, 0, 0, time.UTC),
		Event:     "Sentry",
		Reason:    "sentry_aware_object_detection",
	}, 37.7749, -122.4194, []models.VideoFile{{
		Camera:    "Front",
		FilePath:  "/synthetic/sentry/adj-1-front.mp4",
		Timestamp: time.Date(2024, 6, 15, 15, 0, 0, 0, time.UTC),
	}})
	adj2 = createClipWithTelemetry(t, models.Clip{
		Timestamp: time.Date(2024, 6, 15, 15, 1, 0, 0, time.UTC),
		Event:     "Sentry",
		Reason:    "sentry_aware_object_detection",
	}, 37.7749, -122.4194, []models.VideoFile{{
		Camera:    "Front",
		FilePath:  "/synthetic/sentry/adj-2-front.mp4",
		Timestamp: time.Date(2024, 6, 15, 15, 1, 0, 0, time.UTC),
	}})

	saved = createClipWithTelemetry(t, models.Clip{
		Timestamp: time.Date(2024, 6, 16, 10, 0, 0, 0, time.UTC),
		Event:     "Saved",
		Reason:    "user_saved_clip",
		SourceDir: "/synthetic/saved/one",
	}, 37.7749, -122.4194, []models.VideoFile{{
		Camera:    "Front",
		FilePath:  "/synthetic/saved/front.mp4",
		Timestamp: time.Date(2024, 6, 16, 10, 0, 0, 0, time.UTC),
	}})

	// (0,0) coords must be omitted from lat/lon fields.
	recent = createClipWithTelemetry(t, models.Clip{
		Timestamp: time.Date(2024, 6, 16, 11, 0, 0, 0, time.UTC),
		Event:     "Recent",
		SourceDir: "/synthetic/recent/one",
	}, 0, 0, []models.VideoFile{{
		Camera:    "Back",
		FilePath:  "/synthetic/recent/back.mp4",
		Timestamp: time.Date(2024, 6, 16, 11, 0, 0, 0, time.UTC),
	}})
	return
}

func doGetLibrary(t *testing.T, r http.Handler, rawQuery string) (int, libraryTestResponse, string) {
	t.Helper()
	path := "/api/library"
	if rawQuery != "" {
		path += "?" + rawQuery
	}
	w := httptest.NewRecorder()
	req, _ := http.NewRequest("GET", path, nil)
	r.ServeHTTP(w, req)
	var resp libraryTestResponse
	if w.Body.Len() > 0 {
		_ = json.Unmarshal(w.Body.Bytes(), &resp)
	}
	return w.Code, resp, w.Body.String()
}

func TestGetLibrary(t *testing.T) {
	r := setupLibraryRouter(t)
	grouped, adj1, adj2, saved, recent := seedLibraryCorpus(t)

	code, resp, body := doGetLibrary(t, r, "")
	if code != http.StatusOK {
		t.Fatalf("GET /api/library status %d, body %s", code, body)
	}
	if resp.Events == nil {
		t.Fatalf("events must be an array, got null")
	}
	if len(resp.Events) != 5 {
		t.Fatalf("expected 5 library rows (no client-style merge), got %d", len(resp.Events))
	}

	// newest-first by timestamp
	wantIDs := []uint{recent.ID, saved.ID, adj2.ID, adj1.ID, grouped.ID}
	for i, id := range wantIDs {
		if resp.Events[i].ID != id {
			t.Errorf("events[%d].id = %d, want %d (newest-first)", i, resp.Events[i].ID, id)
		}
	}

	var groupedDTO libraryTestEvent
	for _, ev := range resp.Events {
		if ev.ID == grouped.ID {
			groupedDTO = ev
		}
	}
	if strings.Contains(body, `"video_files"`) {
		t.Errorf("compact events must not include a video_files key")
	}
	if groupedDTO.VideoFiles != nil {
		t.Errorf("video_files must be absent, got %s", string(groupedDTO.VideoFiles))
	}
	if strings.Contains(body, "full_data_json") {
		t.Errorf("compact events must not include full_data_json")
	}
	if groupedDTO.VideoFileCount != 6 {
		t.Errorf("video_file_count = %d, want 6", groupedDTO.VideoFileCount)
	}
	if groupedDTO.PreviewCamera != "Front" {
		t.Errorf("preview_camera = %q, want Front", groupedDTO.PreviewCamera)
	}
	if groupedDTO.PreviewPath == "" {
		t.Errorf("preview_path must be present")
	}
	if groupedDTO.Latitude == nil || groupedDTO.Longitude == nil {
		t.Errorf("valid incident point must include latitude/longitude")
	} else if *groupedDTO.Latitude != 37.7749 || *groupedDTO.Longitude != -122.4194 {
		t.Errorf("incident point = (%v,%v), want fixture", *groupedDTO.Latitude, *groupedDTO.Longitude)
	}

	var recentDTO libraryTestEvent
	for _, ev := range resp.Events {
		if ev.ID == recent.ID {
			recentDTO = ev
		}
	}
	if recentDTO.Latitude != nil || recentDTO.Longitude != nil {
		t.Errorf("(0,0) coords must be omitted, got lat=%v lon=%v", recentDTO.Latitude, recentDTO.Longitude)
	}

	// Adjacent-minute pair stays two rows (scanner-owned grouping; this endpoint does not merge).
	foundAdj := 0
	for _, ev := range resp.Events {
		if ev.ID == adj1.ID || ev.ID == adj2.ID {
			foundAdj++
			if ev.SourceDir != "" {
				t.Errorf("adjacent physical clip %d should have empty source_dir", ev.ID)
			}
		}
	}
	if foundAdj != 2 {
		t.Errorf("expected two unmerged adjacent Sentry rows, found %d", foundAdj)
	}

	t.Run("date filter UTC day", func(t *testing.T) {
		// date= is the UTC calendar day of Clip.Timestamp.
		code, resp, _ := doGetLibrary(t, r, "date=2024-06-16")
		if code != 200 {
			t.Fatalf("status %d", code)
		}
		if len(resp.Events) != 2 {
			t.Fatalf("date=2024-06-16 events = %d, want 2", len(resp.Events))
		}
		for _, ev := range resp.Events {
			if ev.Event != "Saved" && ev.Event != "Recent" {
				t.Errorf("unexpected event %q on 2024-06-16", ev.Event)
			}
		}
		// Date facets are counted AFTER event/reason filters but BEFORE the
		// date filter so the calendar still shows other UTC days.
		sawJun15 := false
		for _, d := range resp.Facets.Dates {
			if d.Date == "2024-06-15" {
				sawJun15 = true
			}
		}
		if !sawJun15 {
			t.Errorf("date facets after date= filter must still include 2024-06-15, got %+v", resp.Facets.Dates)
		}
	})

	t.Run("empty day", func(t *testing.T) {
		code, resp, body := doGetLibrary(t, r, "date=2020-01-01")
		if code != 200 {
			t.Fatalf("status %d body %s", code, body)
		}
		if resp.Events == nil || len(resp.Events) != 0 {
			t.Errorf("empty day must return events:[], got %#v", resp.Events)
		}
	})

	t.Run("event and reason filters", func(t *testing.T) {
		code, resp, _ := doGetLibrary(t, r, "event=Sentry")
		if code != 200 {
			t.Fatalf("status %d", code)
		}
		if len(resp.Events) != 3 {
			t.Fatalf("event=Sentry events = %d, want 3", len(resp.Events))
		}
		code, resp, _ = doGetLibrary(t, r, "reason=sentry_aware_object_detection")
		if code != 200 {
			t.Fatalf("status %d", code)
		}
		if len(resp.Events) != 3 {
			t.Fatalf("reason=sentry_aware_object_detection events = %d, want 3", len(resp.Events))
		}
		code, resp, _ = doGetLibrary(t, r, "reason=not_a_real_reason")
		if code != 200 {
			t.Fatalf("unknown reason should 200, got %d", code)
		}
		if len(resp.Events) != 0 {
			t.Errorf("unknown reason must preserve empty state, got %d events", len(resp.Events))
		}
	})

	t.Run("invalid filters 400", func(t *testing.T) {
		code, _, body := doGetLibrary(t, r, "date=foo")
		if code != 400 {
			t.Errorf("date=foo status %d, want 400", code)
		}
		if !strings.Contains(body, `"error"`) {
			t.Errorf("400 body must include error, got %s", body)
		}
		code, _, _ = doGetLibrary(t, r, "event=Nope")
		if code != 400 {
			t.Errorf("event=Nope status %d, want 400", code)
		}
		code, _, _ = doGetLibrary(t, r, "reason=")
		if code != 400 {
			t.Errorf("empty reason status %d, want 400", code)
		}
	})

	t.Run("limit is ignored not dishonest paging", func(t *testing.T) {
		code, resp, _ := doGetLibrary(t, r, "limit=1")
		if code != 200 {
			t.Fatalf("status %d", code)
		}
		if len(resp.Events) != 5 {
			t.Errorf("limit must not page raw clip rows, got %d events", len(resp.Events))
		}
	})

	t.Run("GET /api/clips still legacy full array", func(t *testing.T) {
		w := httptest.NewRecorder()
		req, _ := http.NewRequest("GET", "/api/clips", nil)
		r.ServeHTTP(w, req)
		if w.Code != 200 {
			t.Fatalf("status %d", w.Code)
		}
		var clips []models.Clip
		if err := json.Unmarshal(w.Body.Bytes(), &clips); err != nil {
			t.Fatalf("clips json: %v", err)
		}
		if len(clips) != 5 {
			t.Fatalf("expected 5 clips, got %d", len(clips))
		}
		var groupedClip models.Clip
		for _, c := range clips {
			if c.ID == grouped.ID {
				groupedClip = c
			}
		}
		if len(groupedClip.VideoFiles) != 6 {
			t.Errorf("/api/clips must still include video_files, got %d", len(groupedClip.VideoFiles))
		}
		if groupedClip.Telemetry.FullDataJson != "" {
			t.Errorf("/api/clips list must omit full_data_json, got %q", groupedClip.Telemetry.FullDataJson)
		}
	})

	t.Run("GET /api/clips/:id includes full_data_json and all files", func(t *testing.T) {
		w := httptest.NewRecorder()
		req, _ := http.NewRequest("GET", fmt.Sprintf("/api/clips/%d", grouped.ID), nil)
		r.ServeHTTP(w, req)
		if w.Code != 200 {
			t.Fatalf("status %d body %s", w.Code, w.Body.String())
		}
		var clip models.Clip
		if err := json.Unmarshal(w.Body.Bytes(), &clip); err != nil {
			t.Fatalf("detail json: %v", err)
		}
		if clip.Telemetry.FullDataJson == "" {
			t.Errorf("detail must include full_data_json")
		}
		if len(clip.VideoFiles) != 6 {
			t.Errorf("detail must include all video files, got %d", len(clip.VideoFiles))
		}
	})

	t.Run("no CORS on /api/library", func(t *testing.T) {
		w := httptest.NewRecorder()
		req, _ := http.NewRequest("GET", "/api/library", nil)
		r.ServeHTTP(w, req)
		if w.Header().Get("Access-Control-Allow-Origin") != "" {
			t.Errorf("Expected no Access-Control-Allow-Origin header for /api/library")
		}
	})
}

func TestLibraryPayloadSmallerThanClips(t *testing.T) {
	// Scale: 80 clips × 12 video files each + ~2KiB FullDataJson.
	// An 11k-row seed is too slow for unit tests; this is enough to prove
	// the compact payload is smaller and omits bulky arrays/json.
	r := setupLibraryRouter(t)
	const nClips = 80
	const nFiles = 12
	for i := 0; i < nClips; i++ {
		ts := time.Date(2024, 6, 10, 0, 0, 0, 0, time.UTC).Add(time.Duration(i) * time.Minute)
		files := make([]models.VideoFile, nFiles)
		for j := 0; j < nFiles; j++ {
			cam := fmt.Sprintf("Cam%d", j)
			if j == 0 {
				cam = "Front"
			}
			files[j] = models.VideoFile{
				Camera:    cam,
				FilePath:  fmt.Sprintf("/synthetic/payload/%d/%s.mp4", i, cam),
				Timestamp: ts,
			}
		}
		createClipWithTelemetry(t, models.Clip{
			Timestamp: ts,
			Event:     "Sentry",
			Reason:    "sentry_aware_object_detection",
			SourceDir: fmt.Sprintf("/synthetic/payload/%d", i),
		}, 37.7749, -122.4194, files)
	}

	wClips := httptest.NewRecorder()
	reqClips, _ := http.NewRequest("GET", "/api/clips", nil)
	r.ServeHTTP(wClips, reqClips)
	if wClips.Code != 200 {
		t.Fatalf("/api/clips status %d", wClips.Code)
	}

	wLib := httptest.NewRecorder()
	reqLib, _ := http.NewRequest("GET", "/api/library", nil)
	r.ServeHTTP(wLib, reqLib)
	if wLib.Code != 200 {
		t.Fatalf("/api/library status %d", wLib.Code)
	}

	clipsBytes := wClips.Body.Len()
	libraryBytes := wLib.Body.Len()
	t.Logf("PAYLOAD_BYTES: clips=%d library=%d", clipsBytes, libraryBytes)
	if libraryBytes >= clipsBytes {
		t.Errorf("library payload %d must be smaller than clips payload %d", libraryBytes, clipsBytes)
	}
	libBody := wLib.Body.String()
	if strings.Contains(libBody, "full_data_json") {
		t.Errorf("library body must not contain full_data_json")
	}
	if strings.Contains(libBody, `"video_files"`) {
		t.Errorf("library body must not contain 12-file video_files arrays")
	}
}

func findLibraryEvent(resp libraryTestResponse, id uint) (libraryTestEvent, bool) {
	for _, ev := range resp.Events {
		if ev.ID == id {
			return ev, true
		}
	}
	return libraryTestEvent{}, false
}

func TestLibraryPreviewMatchesSidebar(t *testing.T) {
	r := setupLibraryRouter(t)

	t.Run("latest Front at or before event_timestamp", func(t *testing.T) {
		eventTS := time.Date(2024, 7, 1, 12, 2, 30, 0, time.UTC)
		clip := createClipWithTelemetry(t, models.Clip{
			Timestamp:      time.Date(2024, 7, 1, 12, 0, 0, 0, time.UTC),
			EventTimestamp: &eventTS,
			Event:          "Sentry",
			Reason:         "sentry_aware_object_detection",
			SourceDir:      "/synthetic/preview/multi-front",
		}, 37.7749, -122.4194, []models.VideoFile{
			{Camera: "Front", FilePath: "/synthetic/preview/front-1200.mp4", Timestamp: time.Date(2024, 7, 1, 12, 0, 0, 0, time.UTC)},
			{Camera: "Back", FilePath: "/synthetic/preview/back-1200.mp4", Timestamp: time.Date(2024, 7, 1, 12, 0, 0, 0, time.UTC)},
			{Camera: "Front", FilePath: "/synthetic/preview/front-1201.mp4", Timestamp: time.Date(2024, 7, 1, 12, 1, 0, 0, time.UTC)},
			{Camera: "Front", FilePath: "/synthetic/preview/front-1202.mp4", Timestamp: time.Date(2024, 7, 1, 12, 2, 0, 0, time.UTC)},
			{Camera: "Front", FilePath: "/synthetic/preview/front-1203.mp4", Timestamp: time.Date(2024, 7, 1, 12, 3, 0, 0, time.UTC)},
		})

		code, resp, body := doGetLibrary(t, r, "")
		if code != http.StatusOK {
			t.Fatalf("status %d body %s", code, body)
		}
		ev, ok := findLibraryEvent(resp, clip.ID)
		if !ok {
			t.Fatalf("clip %d missing from library", clip.ID)
		}
		if ev.PreviewPath != "/synthetic/preview/front-1202.mp4" {
			t.Errorf("preview_path = %q, want latest Front at/before event (front-1202)", ev.PreviewPath)
		}
		if ev.PreviewCamera != "Front" {
			t.Errorf("preview_camera = %q, want Front", ev.PreviewCamera)
		}
		if ev.PreviewTimestamp == nil {
			t.Fatalf("preview_timestamp missing")
		}
		wantTS := time.Date(2024, 7, 1, 12, 2, 0, 0, time.UTC)
		if !ev.PreviewTimestamp.Equal(wantTS) {
			t.Errorf("preview_timestamp = %v, want %v", ev.PreviewTimestamp, wantTS)
		}
		if ev.PreviewSeekSeconds != 30 {
			t.Errorf("preview_seek_seconds = %v, want 30", ev.PreviewSeekSeconds)
		}
		if ev.PreviewSeekSeconds < 0 || ev.PreviewSeekSeconds >= 600 {
			t.Errorf("preview_seek_seconds must be in [0, 600), got %v", ev.PreviewSeekSeconds)
		}
	})

	t.Run("no event_timestamp uses earliest Front", func(t *testing.T) {
		clip := createClipWithTelemetry(t, models.Clip{
			Timestamp: time.Date(2024, 7, 2, 12, 0, 0, 0, time.UTC),
			Event:     "Sentry",
			Reason:    "sentry_aware_object_detection",
			SourceDir: "/synthetic/preview/no-event-ts",
		}, 37.7749, -122.4194, []models.VideoFile{
			{Camera: "Front", FilePath: "/synthetic/preview/no-ts-front-early.mp4", Timestamp: time.Date(2024, 7, 2, 12, 0, 0, 0, time.UTC)},
			{Camera: "Front", FilePath: "/synthetic/preview/no-ts-front-late.mp4", Timestamp: time.Date(2024, 7, 2, 12, 2, 0, 0, time.UTC)},
		})

		_, resp, _ := doGetLibrary(t, r, "date=2024-07-02")
		ev, ok := findLibraryEvent(resp, clip.ID)
		if !ok {
			t.Fatalf("clip %d missing", clip.ID)
		}
		if ev.PreviewPath != "/synthetic/preview/no-ts-front-early.mp4" {
			t.Errorf("preview_path = %q, want earliest Front", ev.PreviewPath)
		}
		if ev.PreviewSeekSeconds != 0 {
			t.Errorf("preview_seek_seconds = %v, want 0 without event_timestamp", ev.PreviewSeekSeconds)
		}
	})

	t.Run("only later Fronts fall back to earliest Front", func(t *testing.T) {
		eventTS := time.Date(2024, 7, 3, 12, 0, 0, 0, time.UTC)
		clip := createClipWithTelemetry(t, models.Clip{
			Timestamp:      time.Date(2024, 7, 3, 12, 0, 0, 0, time.UTC),
			EventTimestamp: &eventTS,
			Event:          "Sentry",
			Reason:         "sentry_aware_object_detection",
			SourceDir:      "/synthetic/preview/later-fronts",
		}, 37.7749, -122.4194, []models.VideoFile{
			{Camera: "Back", FilePath: "/synthetic/preview/later-back.mp4", Timestamp: time.Date(2024, 7, 3, 11, 59, 0, 0, time.UTC)},
			{Camera: "Front", FilePath: "/synthetic/preview/later-front-1201.mp4", Timestamp: time.Date(2024, 7, 3, 12, 1, 0, 0, time.UTC)},
			{Camera: "Front", FilePath: "/synthetic/preview/later-front-1202.mp4", Timestamp: time.Date(2024, 7, 3, 12, 2, 0, 0, time.UTC)},
		})

		_, resp, _ := doGetLibrary(t, r, "date=2024-07-03")
		ev, ok := findLibraryEvent(resp, clip.ID)
		if !ok {
			t.Fatalf("clip %d missing", clip.ID)
		}
		if ev.PreviewPath != "/synthetic/preview/later-front-1201.mp4" {
			t.Errorf("preview_path = %q, want fallback earliest Front", ev.PreviewPath)
		}
		if ev.PreviewSeekSeconds != 0 {
			t.Errorf("preview_seek_seconds = %v, want 0 when event is before selected Front", ev.PreviewSeekSeconds)
		}
	})

	t.Run("Back-only falls back to first file", func(t *testing.T) {
		clip := createClipWithTelemetry(t, models.Clip{
			Timestamp: time.Date(2024, 7, 4, 11, 0, 0, 0, time.UTC),
			Event:     "Recent",
			SourceDir: "/synthetic/preview/back-only",
		}, 0, 0, []models.VideoFile{
			{Camera: "Back", FilePath: "/synthetic/preview/back-only.mp4", Timestamp: time.Date(2024, 7, 4, 11, 0, 0, 0, time.UTC)},
			{Camera: "Left Repeater", FilePath: "/synthetic/preview/left-only.mp4", Timestamp: time.Date(2024, 7, 4, 11, 1, 0, 0, time.UTC)},
		})

		_, resp, _ := doGetLibrary(t, r, "date=2024-07-04")
		ev, ok := findLibraryEvent(resp, clip.ID)
		if !ok {
			t.Fatalf("clip %d missing", clip.ID)
		}
		if ev.PreviewPath != "/synthetic/preview/back-only.mp4" {
			t.Errorf("preview_path = %q, want earliest any-camera file", ev.PreviewPath)
		}
		if ev.PreviewCamera != "Back" {
			t.Errorf("preview_camera = %q, want Back", ev.PreviewCamera)
		}
	})
}

type videoFileScanCounter struct {
	rows int
}

func (c *videoFileScanCounter) hook(scope *gorm.Scope) {
	if scope == nil || scope.Value == nil {
		return
	}
	switch v := scope.Value.(type) {
	case *[]models.VideoFile:
		c.rows += len(*v)
	case *models.VideoFile:
		c.rows++
	}
}

type sqlCaptureLogger struct {
	lines []string
}

func (l *sqlCaptureLogger) Print(v ...interface{}) {
	l.lines = append(l.lines, fmt.Sprint(v...))
}

func TestLibraryDoesNotMaterialiseAllVideoFiles(t *testing.T) {
	r := setupLibraryRouter(t)
	const nClips = 200
	const nFiles = 12
	for i := 0; i < nClips; i++ {
		ts := time.Date(2024, 8, 1, 0, 0, 0, 0, time.UTC).Add(time.Duration(i) * time.Minute)
		files := make([]models.VideoFile, nFiles)
		for j := 0; j < nFiles; j++ {
			cam := fmt.Sprintf("Cam%d", j)
			if j == 0 {
				cam = "Front"
			}
			files[j] = models.VideoFile{
				Camera:    cam,
				FilePath:  fmt.Sprintf("/synthetic/materialise/%d/%s.mp4", i, cam),
				Timestamp: ts.Add(time.Duration(j) * time.Second),
			}
		}
		createClipWithTelemetry(t, models.Clip{
			Timestamp: ts,
			Event:     "Sentry",
			Reason:    "sentry_aware_object_detection",
			SourceDir: fmt.Sprintf("/synthetic/materialise/%d", i),
		}, 37.7749, -122.4194, files)
	}

	counter := &videoFileScanCounter{}
	cbName := "test:count_library_vf"
	database.DB.Callback().Query().After("gorm:query").Register(cbName, counter.hook)
	t.Cleanup(func() {
		database.DB.Callback().Query().Remove(cbName)
	})

	logger := &sqlCaptureLogger{}
	database.DB.SetLogger(logger)
	database.DB.LogMode(true)
	t.Cleanup(func() {
		database.DB.LogMode(false)
	})

	start := time.Now()
	w := httptest.NewRecorder()
	req, _ := http.NewRequest("GET", "/api/library", nil)
	r.ServeHTTP(w, req)
	elapsed := time.Since(start)
	t.Logf("TIMING: 200x12 library %s", elapsed)
	if w.Code != 200 {
		t.Fatalf("/api/library status %d body %s", w.Code, w.Body.String())
	}

	var resp libraryTestResponse
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("library json: %v", err)
	}
	if len(resp.Events) != nClips {
		t.Fatalf("events = %d, want %d", len(resp.Events), nClips)
	}

	totalFiles := nClips * nFiles
	if counter.rows > nClips*2 {
		t.Errorf("library GORM-scanned %d VideoFile rows (O(nFiles)=%d); allow COUNT + one preview per clip only", counter.rows, totalFiles)
	}

	for _, line := range logger.lines {
		if !strings.Contains(line, "video_files") {
			continue
		}
		if strings.Contains(line, " IN (") || strings.Contains(line, " in (") {
			t.Errorf("library must not bind clip IDs as IN (?) lists against video_files: %s", line)
		}
	}
	if strings.Contains(w.Body.String(), `"video_files"`) {
		t.Errorf("library body must not include video_files arrays")
	}
}

func TestLibraryQueryTiming400(t *testing.T) {
	r := setupLibraryRouter(t)
	const nClips = 400
	const nFiles = 12
	for i := 0; i < nClips; i++ {
		ts := time.Date(2024, 9, 1, 0, 0, 0, 0, time.UTC).Add(time.Duration(i) * time.Minute)
		files := make([]models.VideoFile, nFiles)
		for j := 0; j < nFiles; j++ {
			cam := "Front"
			if j != 0 {
				cam = fmt.Sprintf("Cam%d", j)
			}
			files[j] = models.VideoFile{
				Camera:    cam,
				FilePath:  fmt.Sprintf("/synthetic/timing/%d/%s.mp4", i, cam),
				Timestamp: ts,
			}
		}
		createClipWithTelemetry(t, models.Clip{
			Timestamp: ts,
			Event:     "Sentry",
			Reason:    "sentry_aware_object_detection",
			SourceDir: fmt.Sprintf("/synthetic/timing/%d", i),
		}, 37.7749, -122.4194, files)
	}

	start := time.Now()
	w := httptest.NewRecorder()
	req, _ := http.NewRequest("GET", "/api/library", nil)
	r.ServeHTTP(w, req)
	elapsed := time.Since(start)
	t.Logf("TIMING: 400x12 library %s body=%d", elapsed, w.Body.Len())
	if w.Code != 200 {
		t.Fatalf("/api/library status %d", w.Code)
	}
	var resp libraryTestResponse
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("library json: %v", err)
	}
	if len(resp.Events) != nClips {
		t.Fatalf("events = %d, want %d", len(resp.Events), nClips)
	}
}
