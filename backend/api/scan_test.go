package api

import (
	"bytes"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"teslaxy/database"
	"teslaxy/models"
	pb "teslaxy/proto"
	"teslaxy/services"
)

func TestRescanCredentialLogging(t *testing.T) {
	oldUser, oldPass := adminUser, adminPass
	defer func() { adminUser, adminPass = oldUser, oldPass }()
	t.Setenv("ADMIN_PASS", "")
	var output bytes.Buffer
	oldOutput := log.Writer()
	log.SetOutput(&output)
	defer log.SetOutput(oldOutput)
	loadAdminCreds()
	if adminPass == "" {
		t.Fatal("generated credential is empty")
	}
	if strings.Contains(output.String(), adminPass) {
		t.Fatal("startup logger disclosed a credential (value withheld)")
	}
}

func TestRescanAuthentication(t *testing.T) {
	r, _, _, _ := setupRetentionAPI(t, true)
	w := retentionRequest(r, "POST", "/api/scan", "", "127.0.0.1:1234", "http://localhost", true, "")
	if w.Code != http.StatusUnauthorized {
		t.Fatalf("want 401, got %d", w.Code)
	}
}

func TestRescanGuards(t *testing.T) {
	for _, authEnabled := range []bool{true, false} {
		r, _, _, _ := setupRetentionAPI(t, authEnabled)
		token := authToken(t)
		for _, tc := range []struct {
			name, remote, origin string
			csrf                 bool
		}{
			{"missing header and origin", "127.0.0.1:1234", "", false},
			{"missing header", "127.0.0.1:1234", "http://localhost", false},
			{"cross origin", "127.0.0.1:1234", "https://attacker.invalid", true},
			{"non LAN", "203.0.113.10:1234", "http://localhost", true},
		} {
			w := retentionRequest(r, "POST", "/api/scan", "", tc.remote, tc.origin, tc.csrf, token)
			if w.Code != http.StatusForbidden {
				t.Errorf("%s (auth=%t): want 403, got %d", tc.name, authEnabled, w.Code)
			}
		}
	}
}

func TestRescanUnavailable(t *testing.T) {
	r, _, _, _ := setupRetentionAPI(t, true)
	SetScannerService(nil)
	w := retentionRequest(r, "POST", "/api/scan", "", "127.0.0.1:1234", "http://localhost", true, authToken(t))
	if w.Code != http.StatusServiceUnavailable {
		t.Fatalf("want 503, got %d", w.Code)
	}
}

func TestRescanAcceptedAndSingleFlight(t *testing.T) {
	r, _, _, footage := setupRetentionAPI(t, true)
	database.DB.DB().SetMaxOpenConns(1)
	s := services.NewScannerService(footage, database.DB)
	s.Geocoder = nil
	path := filepath.Join(footage, "RecentClips", "2026-11-01_12-00-00-front.mp4")
	if err := os.WriteFile(path, []byte("fixture"), 0600); err != nil {
		t.Fatal(err)
	}
	entered := make(chan struct{}, 2)
	release := make(chan struct{})
	s.SEIExtractor = func(string) ([]*pb.SeiMetadata, error) {
		entered <- struct{}{}
		<-release
		return []*pb.SeiMetadata{{VehicleSpeedMps: 12}}, nil
	}
	SetScannerService(s)
	t.Cleanup(func() { SetScannerService(nil) })
	defer func() {
		close(release)
		deadline := time.Now().Add(5 * time.Second)
		for s.ScanRunning() && time.Now().Before(deadline) {
			time.Sleep(10 * time.Millisecond)
		}
		if s.ScanRunning() {
			t.Error("scan did not finish")
			return
		}
		var clip models.Clip
		if err := database.DB.Where("source_dir = ? AND telemetry_id != 0", filepath.Dir(path)).First(&clip).Error; err != nil {
			t.Errorf("accepted scan did not persist fixture telemetry: %v", err)
			return
		}
		var row models.Telemetry
		if err := database.DB.First(&row, clip.TelemetryID).Error; err != nil || row.ClipID != clip.ID || !strings.Contains(row.FileTimingJson, filepath.Base(path)) {
			t.Error("accepted scan did not persist owned telemetry and file timing")
		}
	}()
	token := authToken(t)
	w := retentionRequest(r, "POST", "/api/scan", "", "127.0.0.1:1234", "http://localhost", true, token)
	if w.Code != http.StatusAccepted {
		t.Fatalf("want 202, got %d", w.Code)
	}
	select {
	case <-entered:
	case <-time.After(5 * time.Second):
		t.Fatal("accepted scan did not start")
	}
	w = retentionRequest(r, "POST", "/api/scan", "", "127.0.0.1:1234", "http://localhost", true, token)
	if w.Code != http.StatusConflict || !strings.Contains(w.Body.String(), "already running") {
		t.Fatalf("concurrent scan: want 409 already running, got %d", w.Code)
	}
	select {
	case <-entered:
		t.Error("second request started another scan")
	default:
	}
}
