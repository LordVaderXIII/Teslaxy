package api

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/jinzhu/gorm"
	_ "github.com/jinzhu/gorm/dialects/sqlite"
	"teslaxy/database"
	"teslaxy/models"
	"teslaxy/services"
)

func setupRetentionAPI(t *testing.T, authEnabled bool) (*gin.Engine, *services.RetentionService, string, string) {
	t.Helper()
	gin.SetMode(gin.TestMode)
	if authEnabled {
		t.Setenv("AUTH_ENABLED", "true")
	} else {
		t.Setenv("AUTH_ENABLED", "false")
	}
	t.Setenv("ADMIN_PASS", "test-pass-retention")
	loadAdminCreds()

	db, err := gorm.Open("sqlite3", ":memory:")
	if err != nil {
		t.Fatalf("db: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })
	_ = db.AutoMigrate(&models.Clip{}, &models.VideoFile{}, &models.Telemetry{})
	database.DB = db

	root := t.TempDir()
	cfgPath := t.TempDir()
	footage := filepath.Join(root, "footage")
	_ = os.MkdirAll(filepath.Join(footage, "RecentClips"), 0755)
	path := filepath.Join(footage, "RecentClips", "old.mp4")
	_ = os.WriteFile(path, make([]byte, 1024), 0644)
	_ = os.Chtimes(path, time.Now().Add(-2*time.Hour), time.Now().Add(-2*time.Hour))

	clip := models.Clip{Timestamp: time.Now().AddDate(0, 0, -200), Event: "Recent", SourceDir: filepath.Join(footage, "RecentClips")}
	_ = db.Create(&clip)
	_ = db.Create(&models.VideoFile{ClipID: clip.ID, Camera: "Front", FilePath: path, Timestamp: clip.Timestamp})

	cfg := services.RetentionConfig{
		Enabled: true, DryRun: true, MaxAgeDays: 90, MaxSizeGB: 300,
		IntervalHours: 24, MinFileAgeMinutes: 0, FootagePath: footage,
	}
	_ = services.SaveRetentionConfig(cfgPath, cfg)
	svc := services.NewRetentionService(db, footage, cfgPath)
	SetRetentionService(svc)

	r := gin.New()
	SetupRoutes(r)
	return r, svc, path, footage
}

func authToken(t *testing.T) string {
	t.Helper()
	tok, err := generateToken("admin")
	if err != nil {
		t.Fatalf("token: %v", err)
	}
	return tok
}

func TestRetentionWritesRequireAuthWhenDisabled(t *testing.T) {
	r, _, _, _ := setupRetentionAPI(t, false)
	cases := []struct {
		method, path string
		body         string
	}{
		{"PUT", "/api/retention/settings", `{"enabled":false,"dry_run":true,"max_age_days":90,"max_size_gb":300,"interval_hours":24,"min_file_age_minutes":15}`},
		{"POST", "/api/retention/run", `{"dry_run":true}`},
		{"POST", "/api/clips/1/protect", `{"protected":true}`},
	}
	for _, tc := range cases {
		w := httptest.NewRecorder()
		req := httptest.NewRequest(tc.method, tc.path, bytes.NewBufferString(tc.body))
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("X-Requested-With", "XMLHttpRequest")
		req.Host = "127.0.0.1:18080"
		r.ServeHTTP(w, req)
		if w.Code != http.StatusUnauthorized {
			t.Fatalf("%s %s without token: got %d body=%s", tc.method, tc.path, w.Code, w.Body.String())
		}
	}
}

func TestRetentionWritesRejectCrossOrigin(t *testing.T) {
	r, _, path, _ := setupRetentionAPI(t, false)
	tok := authToken(t)

	// Force stored config to destructive mode; dry_run override must still be strict.
	svc := getRetentionService()
	cfg := svc.Config()
	cfg.DryRun = false
	cfg.Enabled = true
	_ = svc.SetConfig(cfg)

	w := httptest.NewRecorder()
	req := httptest.NewRequest("POST", "/api/retention/run", bytes.NewBufferString(`{"dry_run":false}`))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+tok)
	req.Header.Set("Origin", "https://attacker.invalid")
	req.Header.Set("X-Requested-With", "XMLHttpRequest")
	req.Host = "127.0.0.1:18080"
	r.ServeHTTP(w, req)
	if w.Code != http.StatusForbidden {
		t.Fatalf("cross-origin must be forbidden, got %d %s", w.Code, w.Body.String())
	}
	if _, err := os.Stat(path); err != nil {
		t.Fatalf("file must remain: %v", err)
	}
}

func TestRetentionMalformedDryRunRejected(t *testing.T) {
	r, svc, path, _ := setupRetentionAPI(t, false)
	tok := authToken(t)
	cfg := svc.Config()
	cfg.DryRun = true
	cfg.Enabled = true
	_ = svc.SetConfig(cfg)

	for _, body := range []string{`{"dry_run":"true"}`, `{"dry_run":1}`, `{"dry_run":null}`} {
		w := httptest.NewRecorder()
		req := httptest.NewRequest("POST", "/api/retention/run", bytes.NewBufferString(body))
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("Authorization", "Bearer "+tok)
		req.Header.Set("X-Requested-With", "XMLHttpRequest")
		req.Host = "127.0.0.1:18080"
		r.ServeHTTP(w, req)
		if w.Code != http.StatusBadRequest {
			t.Fatalf("body %s: want 400 got %d %s", body, w.Code, w.Body.String())
		}
		if _, err := os.Stat(path); err != nil {
			t.Fatalf("malformed dry_run must not delete: %v", err)
		}
	}
}

func TestRetentionValidDryRunWithAuth(t *testing.T) {
	r, _, path, _ := setupRetentionAPI(t, false)
	tok := authToken(t)
	w := httptest.NewRecorder()
	req := httptest.NewRequest("POST", "/api/retention/run", bytes.NewBufferString(`{"dry_run":true}`))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+tok)
	req.Header.Set("X-Requested-With", "XMLHttpRequest")
	req.Host = "127.0.0.1:18080"
	r.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("got %d %s", w.Code, w.Body.String())
	}
	var report map[string]interface{}
	_ = json.Unmarshal(w.Body.Bytes(), &report)
	if report["dry_run"] != true {
		t.Fatalf("report=%v", report)
	}
	if _, err := os.Stat(path); err != nil {
		t.Fatalf("dry-run deleted file: %v", err)
	}
}

func TestRetentionPutRejectsHugeAge(t *testing.T) {
	r, _, _, _ := setupRetentionAPI(t, false)
	tok := authToken(t)
	body := `{"enabled":true,"dry_run":true,"max_age_days":9223372036854775807,"max_size_gb":300,"interval_hours":24,"min_file_age_minutes":15}`
	w := httptest.NewRecorder()
	req := httptest.NewRequest("PUT", "/api/retention/settings", bytes.NewBufferString(body))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+tok)
	req.Header.Set("X-Requested-With", "XMLHttpRequest")
	req.Host = "127.0.0.1:18080"
	r.ServeHTTP(w, req)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("want 400 got %d %s", w.Code, w.Body.String())
	}
}

func TestRetentionMissingCSRFRejected(t *testing.T) {
	r, _, _, _ := setupRetentionAPI(t, false)
	tok := authToken(t)
	w := httptest.NewRecorder()
	req := httptest.NewRequest("POST", "/api/retention/run", bytes.NewBufferString(`{"dry_run":true}`))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+tok)
	req.Host = "127.0.0.1:18080"
	r.ServeHTTP(w, req)
	if w.Code != http.StatusForbidden {
		t.Fatalf("want 403 got %d %s", w.Code, w.Body.String())
	}
}
