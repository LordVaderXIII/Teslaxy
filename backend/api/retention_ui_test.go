package api

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"testing"

	"teslaxy/database"
	"teslaxy/models"
	"teslaxy/services"
)

var retentionWrites = []struct{ method, path, body string }{
	{"PUT", "/api/retention/settings", `{"enabled":false,"dry_run":true,"max_age_days":30,"max_size_gb":300,"interval_hours":24,"min_file_age_minutes":15}`},
	{"POST", "/api/retention/run", `{"dry_run":true}`},
	{"POST", "/api/clips/1/protect", `{"protected":true}`},
	{"POST", "/api/clips/1/protect", `{"protected":true,"ids":[1]}`},
}

func retentionRequest(r http.Handler, method, path, body, remote, origin string, csrf bool, token string) *httptest.ResponseRecorder {
	req := httptest.NewRequest(method, path, bytes.NewBufferString(body))
	req.RemoteAddr = remote
	req.Host = "localhost"
	req.Header.Set("Content-Type", "application/json")
	if origin != "" {
		req.Header.Set("Origin", origin)
	}
	if csrf {
		req.Header.Set("X-Requested-With", "XMLHttpRequest")
	}
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	// Forwarded addresses must never bypass the direct-client LAN restriction.
	req.Header.Set("X-Forwarded-For", "127.0.0.1")
	req.Header.Set("X-Real-IP", "192.168.1.1")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	return w
}

func TestRetentionWriteAccessMatrix(t *testing.T) {
	cases := []struct {
		name              string
		auth, token, csrf bool
		remote, origin    string
		want              int
	}{
		{"auth-off-loopback", false, false, true, "127.0.0.1:1234", "http://localhost", 200},
		{"auth-off-LAN-10", false, false, true, "10.0.0.1:1234", "http://localhost", 200},
		{"auth-off-LAN-172", false, false, true, "172.16.0.1:1234", "http://localhost", 200},
		{"auth-off-LAN-192", false, false, true, "192.168.1.1:1234", "http://localhost", 200},
		{"auth-off-ipv6-loopback", false, false, true, "[::1]:1234", "http://localhost", 200},
		{"cross-origin", false, false, true, "127.0.0.1:1234", "https://attacker.invalid", 403},
		{"off-LAN", false, false, true, "8.8.8.8:1234", "http://localhost", 403},
		{"CGNAT", false, false, true, "100.64.0.1:1234", "http://localhost", 403},
		{"ipv6-private-not-RFC1918", false, false, true, "[fd00::1]:1234", "http://localhost", 403},
		{"auth-on-missing-bearer", true, false, true, "127.0.0.1:1234", "http://localhost", 401},
		{"auth-on-valid-bearer", true, true, true, "192.168.1.1:1234", "http://localhost", 200},
		{"auth-on-off-LAN", true, true, true, "8.8.8.8:1234", "http://localhost", 403},
		{"missing-CSRF", false, false, false, "127.0.0.1:1234", "http://localhost", 403},
		{"auth-on-missing-CSRF", true, true, false, "127.0.0.1:1234", "http://localhost", 403},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			for _, op := range retentionWrites {
				t.Run(op.path, func(t *testing.T) {
					r, svc, path, _ := setupRetentionAPI(t, tc.auth)
					before := svc.Config()
					token := ""
					if tc.token {
						token = authToken(t)
					}
					w := retentionRequest(r, op.method, op.path, op.body, tc.remote, tc.origin, tc.csrf, token)
					if w.Code != tc.want {
						t.Fatalf("want %d got %d %s", tc.want, w.Code, w.Body.String())
					}
					var clip models.Clip
					if err := database.DB.First(&clip, 1).Error; err != nil {
						t.Fatal(err)
					}
					if tc.want != 200 {
						if svc.Config() != before || clip.Protected {
							t.Fatal("rejected write changed state")
						}
						if _, err := os.Stat(path); err != nil {
							t.Fatal(err)
						}
						if _, err := os.Stat(svc.ConfigPath + "/retention-status.json"); !os.IsNotExist(err) {
							t.Fatal("rejected run changed history")
						}
					} else {
						switch op.path {
						case "/api/retention/settings":
							if svc.Config().Enabled || svc.Config().MaxAgeDays != 30 {
								t.Fatal("PUT had no effect")
							}
							persisted, err := services.LoadRetentionConfig(svc.ConfigPath)
							if err != nil || persisted != svc.Config() {
								t.Fatalf("persisted settings: %+v %v", persisted, err)
							}
						case "/api/clips/1/protect":
							if !clip.Protected {
								t.Fatal("protect had no effect")
							}
						case "/api/retention/run":
							var report services.RetentionReport
							if err := json.Unmarshal(w.Body.Bytes(), &report); err != nil {
								t.Fatal(err)
							}
							if !report.DryRun || len(report.Candidates) != 1 || report.BytesFreed != 1024 {
								t.Fatalf("dry-run had no effect: %+v", report)
							}
							if svc.Config() != before {
								t.Fatal("run persisted override")
							}
						}
						if _, err := os.Stat(path); err != nil {
							t.Fatal(err)
						}
					}
				})
			}
		})
	}
}

func TestRetentionSettingsStatusSurvivesRestart(t *testing.T) {
	r, svc, path, footage := setupRetentionAPI(t, false)
	cfg := svc.Config()
	cfg.Enabled = false
	if err := svc.SetConfig(cfg); err != nil {
		t.Fatal(err)
	}
	w := retentionRequest(r, "POST", "/api/retention/run", `{"dry_run":true}`, "127.0.0.1:1234", "http://localhost", true, "")
	if w.Code != 200 {
		t.Fatalf("run: %d %s", w.Code, w.Body.String())
	}
	SetRetentionService(services.NewRetentionService(database.DB, footage, svc.ConfigPath))
	w = retentionRequest(r, "GET", "/api/retention/settings", "", "127.0.0.1:1234", "", false, "")
	if w.Code != 200 {
		t.Fatalf("GET: %d %s", w.Code, w.Body.String())
	}
	var settings struct {
		Enabled bool `json:"enabled"`
		DryRun  bool `json:"dry_run"`
		Status  struct {
			LastRun *struct {
				CandidateCount int `json:"candidate_count"`
			} `json:"last_run"`
			NextRun    *string `json:"next_run"`
			LastDryRun *struct {
				CandidateCount int     `json:"candidate_count"`
				CandidateBytes int64   `json:"candidate_bytes"`
				CandidateGiB   float64 `json:"candidate_gib"`
			} `json:"last_dry_run"`
		} `json:"status"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &settings); err != nil {
		t.Fatal(err)
	}
	if settings.Enabled || !settings.DryRun || settings.Status.LastRun == nil || settings.Status.NextRun != nil || settings.Status.LastDryRun == nil {
		t.Fatalf("missing status or changed config: %s", w.Body.String())
	}
	dry := settings.Status.LastDryRun
	if dry.CandidateCount != 1 || dry.CandidateBytes != 1024 || dry.CandidateGiB != 1024.0/(1<<30) {
		t.Fatalf("wrong dry-run summary: %s", w.Body.String())
	}
	if bytes.Contains(w.Body.Bytes(), []byte("old.mp4")) {
		t.Fatal("GET dumped candidate paths")
	}
	if _, err := os.Stat(path); err != nil {
		t.Fatal(err)
	}
}

func TestRetentionSettingsCannotRelocateFootage(t *testing.T) {
	r, svc, _, footage := setupRetentionAPI(t, false)
	body := `{"enabled":false,"dry_run":true,"max_age_days":30,"max_size_gb":300,"interval_hours":24,"min_file_age_minutes":15,"footage_path":"/elsewhere"}`
	w := retentionRequest(r, "PUT", "/api/retention/settings", body, "127.0.0.1:1234", "http://localhost", true, authToken(t))
	if w.Code != 200 {
		t.Fatalf("PUT: %d %s", w.Code, w.Body.String())
	}
	if svc.Config().FootagePath != footage {
		t.Fatal("PUT relocated footage")
	}
}

func TestRetentionStorageGET(t *testing.T) {
	r, _, _, _ := setupRetentionAPI(t, false)
	w := retentionRequest(r, "GET", "/api/retention/storage", "", "127.0.0.1:1234", "", false, "")
	if w.Code != 200 {
		t.Fatalf("storage: %d %s", w.Code, w.Body.String())
	}
	var storage struct {
		ByType map[string]int64 `json:"by_type_bytes"`
		Used   uint64           `json:"volume_used_bytes"`
		Free   uint64           `json:"volume_free_bytes"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &storage); err != nil {
		t.Fatal(err)
	}
	if storage.ByType["Recent"] != 1024 || storage.Free == 0 || storage.Used == 0 {
		t.Fatalf("storage: %s", w.Body.String())
	}
}
