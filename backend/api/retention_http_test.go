package api

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"testing"

	"teslaxy/database"
	"teslaxy/models"
	"teslaxy/services"
)

// This exercises a real loopback HTTP listener through routes, middleware,
// SQLite, fixture footage, settings persistence, and reconstructed service state.
func TestRetentionHTTPWorkflow(t *testing.T) {
	r, svc, path, footage := setupRetentionAPI(t, false)
	server := httptest.NewServer(r)
	defer server.Close()
	request := func(method, url, body string) []byte {
		t.Helper()
		req, err := http.NewRequest(method, server.URL+url, bytes.NewBufferString(body))
		if err != nil {
			t.Fatal(err)
		}
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("Origin", server.URL)
		req.Header.Set("X-Teslaxy-CSRF", "1")
		// No Authorization header: this is the auth-off UI path.
		response, err := server.Client().Do(req)
		if err != nil {
			t.Fatal(err)
		}
		defer response.Body.Close()
		data, err := io.ReadAll(response.Body)
		if err != nil {
			t.Fatal(err)
		}
		if response.StatusCode != 200 {
			t.Fatalf("%s %s: %d %s", method, url, response.StatusCode, data)
		}
		return data
	}
	decode := func(data []byte, target interface{}) {
		t.Helper()
		if err := json.Unmarshal(data, target); err != nil {
			t.Fatal(err)
		}
	}
	request("PUT", "/api/retention/settings", retentionWrites[0].body)
	var report services.RetentionReport
	decode(request("POST", "/api/retention/run", `{"dry_run":true}`), &report)
	if len(report.Candidates) != 1 || report.BytesFreed != 1024 || len(report.Deleted) != 0 {
		t.Fatalf("dry run: %+v", report)
	}
	SetRetentionService(services.NewRetentionService(database.DB, footage, svc.ConfigPath))
	var settings struct {
		services.RetentionConfig
		Status services.RetentionStatus `json:"status"`
	}
	decode(request("GET", "/api/retention/settings", ""), &settings)
	if settings.Enabled || !settings.DryRun || settings.Status.LastDryRun == nil || settings.Status.LastDryRun.CandidateBytes != 1024 {
		t.Fatalf("restart lost safe settings/summary: %+v", settings)
	}
	var storage services.RetentionStorage
	decode(request("GET", "/api/retention/storage", ""), &storage)
	if storage.ByTypeBytes["Recent"] != 1024 {
		t.Fatalf("wrong storage: %+v", storage)
	}
	request("POST", "/api/clips/1/protect", `{"protected":true}`)
	var clips []models.Clip
	decode(request("GET", "/api/clips", ""), &clips)
	if len(clips) != 1 || !clips[0].Protected {
		t.Fatalf("clip read did not expose protection: %+v", clips)
	}
	var library libraryResponse
	decode(request("GET", "/api/library", ""), &library)
	if len(library.Events) != 1 || !library.Events[0].Protected {
		t.Fatalf("library lost protected flag: %+v", library)
	}
	decode(request("POST", "/api/retention/run", `{"dry_run":true}`), &report)
	if len(report.Candidates) != 0 {
		t.Fatalf("protected clip was a candidate: %+v", report)
	}
	request("POST", "/api/clips/1/protect", `{"protected":false}`)
	decode(request("GET", "/api/clips", ""), &clips)
	if len(clips) != 1 || clips[0].Protected {
		t.Fatalf("unprotect was not reflected in clips: %+v", clips)
	}
	decode(request("GET", "/api/library", ""), &library)
	if len(library.Events) != 1 || library.Events[0].Protected {
		t.Fatalf("unprotect was not reflected in library: %+v", library)
	}
	decode(request("POST", "/api/retention/run", `{"dry_run":true}`), &report)
	if len(report.Candidates) != 1 || report.BytesFreed != 1024 || len(report.Deleted) != 0 {
		t.Fatalf("unprotected clip was not restored as a dry-run candidate: %+v", report)
	}
	if _, err := os.Stat(path); err != nil {
		t.Fatal(err)
	}
	cfg, err := services.LoadRetentionConfig(svc.ConfigPath)
	if err != nil || cfg.Enabled || !cfg.DryRun {
		t.Fatalf("workflow enabled deletes: %+v %v", cfg, err)
	}
	t.Log("auth-off HTTP workflow: PUT settings -> dry-run (1 candidate, 1024 bytes) -> restart -> GET status/storage -> protect -> clips/library protected -> dry-run (0 candidates) -> unprotect -> clips/library unprotected -> dry-run (1 candidate); fixture intact, enabled=false dry_run=true")
}

func TestRetentionRejectedDestructiveWritesLeaveStateIntact(t *testing.T) {
	for _, tc := range []struct {
		name, remote, origin string
		csrf                 bool
	}{
		{"cross-origin", "127.0.0.1:1234", "https://attacker.invalid", true},
		{"off-LAN", "8.8.8.8:1234", "http://localhost", true},
		{"no-CSRF", "127.0.0.1:1234", "http://localhost", false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			r, svc, path, _ := setupRetentionAPI(t, false)
			cfg := svc.Config()
			cfg.Enabled = true
			cfg.DryRun = false
			if err := svc.SetConfig(cfg); err != nil {
				t.Fatal(err)
			}
			before, err := os.ReadFile(svc.ConfigPath + "/retention.json")
			if err != nil {
				t.Fatal(err)
			}
			for _, op := range retentionWrites {
				body := op.body
				if op.path == "/api/retention/run" {
					body = `{"dry_run":false}`
				}
				w := retentionRequest(r, op.method, op.path, body, tc.remote, tc.origin, tc.csrf, "")
				if w.Code != 403 {
					t.Fatalf("%s: %d %s", op.path, w.Code, w.Body.String())
				}
			}
			after, err := os.ReadFile(svc.ConfigPath + "/retention.json")
			if err != nil {
				t.Fatal(err)
			}
			if !bytes.Equal(before, after) || svc.Config() != cfg {
				t.Fatal("rejected write changed settings")
			}
			var clip models.Clip
			if err := database.DB.First(&clip, 1).Error; err != nil || clip.Protected {
				t.Fatalf("rejected write changed clip: %v", err)
			}
			if _, err := os.Stat(path); err != nil {
				t.Fatal(err)
			}
		})
	}
}
