package api

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"

	"teslaxy/database"
	"teslaxy/models"
)

type libraryFileDTO struct {
	ClipID    uint      `json:"clip_id"`
	Camera    string    `json:"camera"`
	FilePath  string    `json:"file_path"`
	Timestamp time.Time `json:"timestamp"`
}

type libraryFilesTestResponse struct {
	Files      []libraryFileDTO `json:"files"`
	MissingIDs []uint           `json:"missing_ids"`
}

func libraryFilesQuery(ids ...string) string {
	q := url.Values{}
	for _, id := range ids {
		q.Add("id", id)
	}
	return q.Encode()
}

func doGetLibraryFiles(t *testing.T, r http.Handler, rawQuery string) (int, libraryFilesTestResponse, string) {
	t.Helper()
	path := "/api/library/files"
	if rawQuery != "" {
		path += "?" + rawQuery
	}
	w := httptest.NewRecorder()
	req, _ := http.NewRequest("GET", path, nil)
	r.ServeHTTP(w, req)
	var resp libraryFilesTestResponse
	if w.Body.Len() > 0 {
		_ = json.Unmarshal(w.Body.Bytes(), &resp)
	}
	return w.Code, resp, w.Body.String()
}

func TestLibraryFilesZeroIDs(t *testing.T) {
	r := setupLibraryRouter(t)
	code, _, body := doGetLibraryFiles(t, r, "")
	if code != http.StatusBadRequest {
		t.Fatalf("0 ids status %d, want 400, body %s", code, body)
	}
}

func TestLibraryFilesZeroID(t *testing.T) {
	r := setupLibraryRouter(t)
	clip := createClipWithTelemetry(t, models.Clip{
		Timestamp: time.Date(2024, 6, 15, 14, 0, 0, 0, time.UTC),
		Event:     "Saved",
	}, 37.7749, -122.4194, []models.VideoFile{{
		Camera:    "Front",
		FilePath:  "/synthetic/files/zero-id-front.mp4",
		Timestamp: time.Date(2024, 6, 15, 14, 0, 0, 0, time.UTC),
	}})

	for _, raw := range []string{"0", "00"} {
		code, _, body := doGetLibraryFiles(t, r, libraryFilesQuery(raw))
		if code != http.StatusBadRequest {
			t.Errorf("id=%q status %d, want 400, body %s", raw, code, body)
		}
		if strings.Contains(body, "zero-id-front.mp4") {
			t.Errorf("id=%q must not return files", raw)
		}
	}

	// A zero id invalidates the whole request, same as a non-numeric id.
	mixed := libraryFilesQuery("0", fmt.Sprintf("%d", clip.ID))
	code, _, body := doGetLibraryFiles(t, r, mixed)
	if code != http.StatusBadRequest {
		t.Fatalf("id=0 mixed with a live id status %d, want 400, body %s", code, body)
	}
}

func TestLibraryFilesNonIntegerID(t *testing.T) {
	r := setupLibraryRouter(t)
	for _, raw := range []string{"abc", "1.5", "1e2", "", "-1"} {
		code, _, body := doGetLibraryFiles(t, r, libraryFilesQuery(raw))
		if code != http.StatusBadRequest {
			t.Errorf("id=%q status %d, want 400, body %s", raw, code, body)
		}
	}
}

func TestLibraryFilesTooManyUniqueIDs(t *testing.T) {
	r := setupLibraryRouter(t)
	ids := make([]string, 65)
	for i := 0; i < 65; i++ {
		ids[i] = fmt.Sprintf("%d", i+1)
	}
	code, _, body := doGetLibraryFiles(t, r, libraryFilesQuery(ids...))
	if code != http.StatusBadRequest {
		t.Fatalf("65 unique ids status %d, want 400, body %s", code, body)
	}
}

func TestLibraryFilesDuplicatesDedupedBeforeCap(t *testing.T) {
	r := setupLibraryRouter(t)
	clip := createClipWithTelemetry(t, models.Clip{
		Timestamp: time.Date(2024, 6, 15, 14, 0, 0, 0, time.UTC),
		Event:     "Sentry",
	}, 37.7749, -122.4194, []models.VideoFile{{
		Camera:    "Front",
		FilePath:  "/synthetic/files/dup-front.mp4",
		Timestamp: time.Date(2024, 6, 15, 14, 0, 0, 0, time.UTC),
	}})

	ids := make([]string, 0, 65)
	ids = append(ids, fmt.Sprintf("%d", clip.ID))
	for i := 0; i < 63; i++ {
		ids = append(ids, fmt.Sprintf("%d", 10000+i))
	}
	ids = append(ids, fmt.Sprintf("%d", clip.ID))
	if len(ids) != 65 {
		t.Fatalf("setup: want 65 query values, got %d", len(ids))
	}

	code, resp, body := doGetLibraryFiles(t, r, libraryFilesQuery(ids...))
	if code != http.StatusOK {
		t.Fatalf("65 params unique to 64 status %d, want 200, body %s", code, body)
	}
	if len(resp.Files) != 1 {
		t.Errorf("files = %d, want 1", len(resp.Files))
	}
}

func TestLibraryFilesUnknownIDOmitted(t *testing.T) {
	r := setupLibraryRouter(t)
	clip := createClipWithTelemetry(t, models.Clip{
		Timestamp: time.Date(2024, 6, 15, 14, 0, 0, 0, time.UTC),
		Event:     "Sentry",
	}, 37.7749, -122.4194, []models.VideoFile{{
		Camera:    "Front",
		FilePath:  "/synthetic/files/known-front.mp4",
		Timestamp: time.Date(2024, 6, 15, 14, 0, 0, 0, time.UTC),
	}})

	unknown := uint(99999)
	q := libraryFilesQuery(fmt.Sprintf("%d", unknown), fmt.Sprintf("%d", clip.ID), fmt.Sprintf("%d", unknown))
	code, resp, body := doGetLibraryFiles(t, r, q)
	if code == http.StatusNotFound {
		t.Fatalf("unknown id must not 404, body %s", body)
	}
	if code != http.StatusOK {
		t.Fatalf("status %d, want 200, body %s", code, body)
	}
	if len(resp.Files) != 1 || resp.Files[0].ClipID != clip.ID {
		t.Fatalf("expected files for known clip %d, got %+v", clip.ID, resp.Files)
	}
	if resp.MissingIDs == nil {
		t.Fatalf("missing_ids must be an array, got null")
	}
	if len(resp.MissingIDs) != 1 || resp.MissingIDs[0] != unknown {
		t.Errorf("missing_ids = %v, want [%d] in first-seen-after-dedupe order", resp.MissingIDs, unknown)
	}
}

func TestLibraryFilesSoftDeletedClipOmitted(t *testing.T) {
	r := setupLibraryRouter(t)
	live := createClipWithTelemetry(t, models.Clip{
		Timestamp: time.Date(2024, 6, 15, 14, 0, 0, 0, time.UTC),
		Event:     "Sentry",
	}, 37.7749, -122.4194, []models.VideoFile{{
		Camera:    "Front",
		FilePath:  "/synthetic/files/live-front.mp4",
		Timestamp: time.Date(2024, 6, 15, 14, 0, 0, 0, time.UTC),
	}})
	dead := createClipWithTelemetry(t, models.Clip{
		Timestamp: time.Date(2024, 6, 15, 15, 0, 0, 0, time.UTC),
		Event:     "Sentry",
	}, 37.7749, -122.4194, []models.VideoFile{{
		Camera:    "Front",
		FilePath:  "/synthetic/files/dead-front.mp4",
		Timestamp: time.Date(2024, 6, 15, 15, 0, 0, 0, time.UTC),
	}})
	if err := database.DB.Delete(&dead).Error; err != nil {
		t.Fatalf("soft-delete clip: %v", err)
	}

	q := libraryFilesQuery(fmt.Sprintf("%d", live.ID), fmt.Sprintf("%d", dead.ID))
	code, resp, body := doGetLibraryFiles(t, r, q)
	if code != http.StatusOK {
		t.Fatalf("status %d, want 200, body %s", code, body)
	}
	if len(resp.Files) != 1 || resp.Files[0].ClipID != live.ID || resp.Files[0].FilePath != "/synthetic/files/live-front.mp4" {
		t.Errorf("files = %+v, want only live clip file", resp.Files)
	}
	if len(resp.MissingIDs) != 1 || resp.MissingIDs[0] != dead.ID {
		t.Errorf("missing_ids = %v, want [%d]", resp.MissingIDs, dead.ID)
	}
	if strings.Contains(body, "dead-front.mp4") {
		t.Errorf("soft-deleted clip files must be absent from body")
	}
}

func TestLibraryFilesJSONKeysOnly(t *testing.T) {
	r := setupLibraryRouter(t)
	clip := createClipWithTelemetry(t, models.Clip{
		Timestamp: time.Date(2024, 6, 15, 14, 0, 0, 0, time.UTC),
		Event:     "Sentry",
	}, 37.7749, -122.4194, []models.VideoFile{{
		Camera:    "Front",
		FilePath:  "/synthetic/files/keys-front.mp4",
		Timestamp: time.Date(2024, 6, 15, 14, 0, 0, 0, time.UTC),
	}})

	code, _, body := doGetLibraryFiles(t, r, libraryFilesQuery(fmt.Sprintf("%d", clip.ID)))
	if code != http.StatusOK {
		t.Fatalf("status %d, body %s", code, body)
	}

	var top map[string]json.RawMessage
	if err := json.Unmarshal([]byte(body), &top); err != nil {
		t.Fatalf("top json: %v", err)
	}
	var files []map[string]json.RawMessage
	if err := json.Unmarshal(top["files"], &files); err != nil {
		t.Fatalf("files json: %v", err)
	}
	if len(files) != 1 {
		t.Fatalf("files len = %d, want 1", len(files))
	}
	allowed := map[string]bool{"clip_id": true, "camera": true, "file_path": true, "timestamp": true}
	for k := range files[0] {
		if !allowed[k] {
			t.Errorf("unexpected file key %q", k)
		}
	}
	if len(files[0]) != 4 {
		t.Errorf("file object keys = %d, want 4 (clip_id, camera, file_path, timestamp)", len(files[0]))
	}

	lower := strings.ToLower(body)
	for _, needle := range []string{"telemetry", "full_data_json", "sei", `"id"`, "video_files"} {
		if needle == `"id"` {
			if strings.Contains(body, `"ID"`) {
				t.Errorf("response must not include JSON key ID")
			}
			continue
		}
		if strings.Contains(lower, needle) {
			t.Errorf("response must not contain %s", needle)
		}
	}
}

func TestLibraryFilesOrder(t *testing.T) {
	r := setupLibraryRouter(t)
	// Clip A is created first (lower ids) but has the later clip timestamp.
	clipA := createClipWithTelemetry(t, models.Clip{
		Timestamp: time.Date(2024, 6, 15, 10, 0, 0, 0, time.UTC),
		Event:     "Sentry",
	}, 37.7749, -122.4194, []models.VideoFile{
		{Camera: "Front", FilePath: "/synthetic/files/a-front-1.mp4", Timestamp: time.Date(2024, 6, 15, 10, 0, 0, 0, time.UTC)},
		{Camera: "Front", FilePath: "/synthetic/files/a-front-2.mp4", Timestamp: time.Date(2024, 6, 15, 10, 0, 0, 0, time.UTC)},
	})
	clipB := createClipWithTelemetry(t, models.Clip{
		Timestamp: time.Date(2024, 6, 15, 9, 0, 0, 0, time.UTC),
		Event:     "Sentry",
	}, 37.7749, -122.4194, []models.VideoFile{
		{Camera: "Front", FilePath: "/synthetic/files/b-front.mp4", Timestamp: time.Date(2024, 6, 15, 9, 1, 0, 0, time.UTC)},
		{Camera: "Back", FilePath: "/synthetic/files/b-back.mp4", Timestamp: time.Date(2024, 6, 15, 9, 2, 0, 0, time.UTC)},
	})

	// Request later clip first so response order is not request order.
	q := libraryFilesQuery(fmt.Sprintf("%d", clipA.ID), fmt.Sprintf("%d", clipB.ID))
	code, resp, body := doGetLibraryFiles(t, r, q)
	if code != http.StatusOK {
		t.Fatalf("status %d, body %s", code, body)
	}
	if len(resp.Files) != 4 {
		t.Fatalf("files = %d, want 4, body %s", len(resp.Files), body)
	}

	want := []string{
		"/synthetic/files/b-back.mp4",    // earlier clip ts, camera Back < Front
		"/synthetic/files/b-front.mp4",   // earlier clip ts, camera Front
		"/synthetic/files/a-front-1.mp4", // later clip ts, same camera/file ts, lower file id
		"/synthetic/files/a-front-2.mp4",
	}
	for i, path := range want {
		if resp.Files[i].FilePath != path {
			t.Errorf("files[%d].file_path = %q, want %q", i, resp.Files[i].FilePath, path)
		}
	}
	if resp.Files[0].ClipID != clipB.ID || resp.Files[1].ClipID != clipB.ID {
		t.Errorf("first two files must belong to earlier clip %d", clipB.ID)
	}
	if resp.Files[2].ClipID != clipA.ID || resp.Files[3].ClipID != clipA.ID {
		t.Errorf("last two files must belong to later clip %d", clipA.ID)
	}
}

func TestLibraryFilesDuplicatePathAcrossClips(t *testing.T) {
	r := setupLibraryRouter(t)
	// Two live member ids of one Saved event, same Front path, no footage required.
	const frontPath = "/synthetic/files/2026-02-15_mawson-lakes-front.mp4"
	const backPath = "/synthetic/files/2026-02-15_mawson-lakes-back.mp4"
	ts := time.Date(2026, 2, 15, 1, 30, 0, 0, time.UTC)
	earlier := createClipWithTelemetry(t, models.Clip{
		Timestamp: ts,
		Event:     "Saved",
		City:      "Mawson Lakes",
		Reason:    "user_interaction_honk",
	}, -34.8060, 138.6130, []models.VideoFile{
		{Camera: "Front", FilePath: frontPath, Timestamp: ts},
		{Camera: "Front", FilePath: frontPath, Timestamp: ts},
	})
	later := createClipWithTelemetry(t, models.Clip{
		Timestamp: ts.Add(time.Minute),
		Event:     "Saved",
		City:      "Mawson Lakes",
		Reason:    "user_interaction_honk",
	}, -34.8060, 138.6130, []models.VideoFile{
		{Camera: "front", FilePath: frontPath, Timestamp: ts},
		{Camera: "Back", FilePath: backPath, Timestamp: ts},
	})

	q := libraryFilesQuery(fmt.Sprintf("%d", later.ID), fmt.Sprintf("%d", earlier.ID))
	code, resp, body := doGetLibraryFiles(t, r, q)
	if code != http.StatusOK {
		t.Fatalf("status %d, want 200, body %s", code, body)
	}
	if len(resp.MissingIDs) != 0 {
		t.Fatalf("both member ids are live, missing_ids = %v", resp.MissingIDs)
	}

	frontCount := 0
	backCount := 0
	for _, file := range resp.Files {
		switch file.FilePath {
		case frontPath:
			frontCount++
			if file.ClipID != earlier.ID {
				t.Errorf("kept front clip_id = %d, want earlier member %d", file.ClipID, earlier.ID)
			}
		case backPath:
			backCount++
			if file.ClipID != later.ID {
				t.Errorf("back clip_id = %d, want later member %d", file.ClipID, later.ID)
			}
		default:
			t.Errorf("unexpected file %s", file.FilePath)
		}
	}
	if frontCount != 1 {
		t.Fatalf("front path count = %d, want 1, body %s", frontCount, body)
	}
	if backCount != 1 {
		t.Fatalf("distinct back path count = %d, want 1, body %s", backCount, body)
	}
	if len(resp.Files) != 2 {
		t.Fatalf("files = %d, want 2 (one front, one back), body %s", len(resp.Files), body)
	}
}

func TestLibraryFilesSoftDeletedVideoFileOmitted(t *testing.T) {
	r := setupLibraryRouter(t)
	clip := createClipWithTelemetry(t, models.Clip{
		Timestamp: time.Date(2024, 6, 15, 14, 0, 0, 0, time.UTC),
		Event:     "Sentry",
	}, 37.7749, -122.4194, []models.VideoFile{
		{Camera: "Front", FilePath: "/synthetic/files/vf-live.mp4", Timestamp: time.Date(2024, 6, 15, 14, 0, 0, 0, time.UTC)},
		{Camera: "Back", FilePath: "/synthetic/files/vf-dead.mp4", Timestamp: time.Date(2024, 6, 15, 14, 1, 0, 0, time.UTC)},
	})

	var dead models.VideoFile
	if err := database.DB.Where("clip_id = ? AND file_path = ?", clip.ID, "/synthetic/files/vf-dead.mp4").First(&dead).Error; err != nil {
		t.Fatalf("lookup video file: %v", err)
	}
	if err := database.DB.Delete(&dead).Error; err != nil {
		t.Fatalf("soft-delete video file: %v", err)
	}

	code, resp, body := doGetLibraryFiles(t, r, libraryFilesQuery(fmt.Sprintf("%d", clip.ID)))
	if code != http.StatusOK {
		t.Fatalf("status %d, body %s", code, body)
	}
	if resp.MissingIDs == nil {
		t.Fatalf("missing_ids must be an array, got null")
	}
	if len(resp.MissingIDs) != 0 {
		t.Errorf("live clip with a deleted file is not missing, got missing_ids=%v", resp.MissingIDs)
	}
	if len(resp.Files) != 1 || resp.Files[0].FilePath != "/synthetic/files/vf-live.mp4" {
		t.Errorf("files = %+v, want only live video file", resp.Files)
	}
	if strings.Contains(body, "vf-dead.mp4") {
		t.Errorf("soft-deleted video file must be absent")
	}
}
