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

func setupScannerAPI(t *testing.T, authEnabled bool) (*gin.Engine, *services.ScannerService, string) {
	t.Helper()
	gin.SetMode(gin.TestMode)
	if authEnabled {
		t.Setenv("AUTH_ENABLED", "true")
	} else {
		t.Setenv("AUTH_ENABLED", "false")
	}
	t.Setenv("ADMIN_PASS", "test-pass-scanner")
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
	t.Setenv("CONFIG_PATH", cfgPath)

	footage := filepath.Join(root, "footage")
	_ = os.MkdirAll(filepath.Join(footage, "RecentClips"), 0755)
	gone := filepath.Join(footage, "RecentClips", "2024-01-01_00-00-00-front.mp4")
	_ = os.WriteFile(gone, []byte("x"), 0644)

	clip := models.Clip{Timestamp: time.Now(), Event: "Recent", SourceDir: filepath.Join(footage, "RecentClips")}
	_ = db.Create(&clip)
	_ = db.Create(&models.VideoFile{ClipID: clip.ID, Camera: "Front", FilePath: gone, Timestamp: clip.Timestamp})
	_ = os.Remove(gone)

	svc := services.NewScannerService(footage, db)
	SetScannerService(svc)
	t.Cleanup(func() { SetScannerService(nil) })

	r := gin.New()
	SetupRoutes(r)
	return r, svc, footage
}

// TestPurgeMissingReachableViaAPI (Codex Medium #6): production route exists
// and invokes ScannerService.PurgeMissing under auth + CSRF.
func TestPurgeMissingReachableViaAPI(t *testing.T) {
	r, _, _ := setupScannerAPI(t, false)
	tok := authToken(t)

	// Route must be registered.
	routes := r.Routes()
	found := false
	for _, rt := range routes {
		if rt.Method == http.MethodPost && rt.Path == "/api/scanner/purge-missing" {
			found = true
			break
		}
	}
	if !found {
		t.Fatal("POST /api/scanner/purge-missing is not registered")
	}

	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/api/scanner/purge-missing", bytes.NewBufferString(""))
	req.Header.Set("Authorization", "Bearer "+tok)
	req.Header.Set("X-Requested-With", "XMLHttpRequest")
	req.Host = "127.0.0.1:18080"
	r.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("purge-missing: want 200 got %d body=%s", w.Code, w.Body.String())
	}
	var res services.ReconcileResult
	if err := json.Unmarshal(w.Body.Bytes(), &res); err != nil {
		t.Fatal(err)
	}
	if res.SoftDeletedFiles < 1 || res.SoftDeletedClips < 1 {
		t.Fatalf("purge-missing should soft-delete ghosts: %+v", res)
	}
}

func TestPurgeMissingRequiresAuth(t *testing.T) {
	r, _, _ := setupScannerAPI(t, false)
	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/api/scanner/purge-missing", nil)
	req.Header.Set("X-Requested-With", "XMLHttpRequest")
	req.Host = "127.0.0.1:18080"
	r.ServeHTTP(w, req)
	if w.Code != http.StatusUnauthorized {
		t.Fatalf("want 401 got %d %s", w.Code, w.Body.String())
	}
}
