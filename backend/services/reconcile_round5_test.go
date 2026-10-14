package services

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"

	"teslaxy/models"
)

// TestReconcileMissing_RejectsTamperedFirstSeen: missing/null/backdated JSON
// first_seen must not authorize immediate deletion (Codex High #1).
func TestReconcileMissing_RejectsTamperedFirstSeen(t *testing.T) {
	db := setupFableADB(t)
	defer db.Close()
	stateDir := testStateDir(t)

	tmpDir, err := os.MkdirTemp("", "reconcile_tamper_fs")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(tmpDir)

	videoPath := filepath.Join(tmpDir, "gone-front.mp4")
	clip := models.Clip{Event: "Sentry", SourceDir: tmpDir}
	db.Create(&clip)
	vf := models.VideoFile{ClipID: clip.ID, Camera: "Front", FilePath: videoPath}
	db.Create(&vf)

	if _, err := ReconcileMissing(db, tmpDir, reconcileOpts(stateDir)); !errors.Is(err, ErrReconcilePendingConfirm) {
		t.Fatalf("first pass: got %v, want pending", err)
	}

	path := filepath.Join(stateDir, pendingPurgeFileName)
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var st pendingPurgeState
	if err := json.Unmarshal(raw, &st); err != nil {
		t.Fatal(err)
	}

	cases := []struct {
		name string
		mut  func(*pendingPurgeState)
	}{
		{"missing-first-seen", func(p *pendingPurgeState) { p.FirstSeen = time.Time{} }},
		{"null-first-seen", func(p *pendingPurgeState) { p.FirstSeen = time.Time{} }},
		{"backdated", func(p *pendingPurgeState) { p.FirstSeen = time.Now().UTC().Add(-2 * time.Hour) }},
		{"changed-ids", func(p *pendingPurgeState) {
			p.FileIDs = []uint{p.FileIDs[0] + 999}
			// leave fingerprint so integrity check fails
		}},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			// Restore a valid pending baseline each case.
			forgetPendingObservation(filepath.Clean(mustAbs(t, tmpDir)), "")
			delete(pendingClearBlocked, filepath.Clean(mustAbs(t, tmpDir)))
			base := st
			base.FirstSeen = time.Now().UTC()
			if err := savePendingPurge(stateDir, &base); err != nil {
				t.Fatal(err)
			}
			// Age observation so a healthy confirm would delete — then tamper.
			if err := agePendingPurgeForTest(stateDir, tmpDir, reconcileConfirmMinInterval+time.Second); err != nil {
				t.Fatal(err)
			}

			raw, err := os.ReadFile(path)
			if err != nil {
				t.Fatal(err)
			}
			var cur pendingPurgeState
			if err := json.Unmarshal(raw, &cur); err != nil {
				t.Fatal(err)
			}
			tc.mut(&cur)
			tampered, err := json.MarshalIndent(&cur, "", "  ")
			if err != nil {
				t.Fatal(err)
			}
			// Bypass symlink-safe writer to simulate an on-disk tamper of JSON only.
			if err := os.WriteFile(path, tampered, 0644); err != nil {
				t.Fatal(err)
			}
			// Keep file mtime mature for backdated case; authorization must still fail
			// for missing/null/changed-ids. For backdated JSON alone with mature mtime
			// + memory, confirmElapsed uses memory/mtime — clear memory to isolate JSON.
			if tc.name == "backdated" {
				forgetPendingObservation(filepath.Clean(mustAbs(t, tmpDir)), "")
				// Also set mtime to now so JSON-only backdate cannot authorize via mtime.
				now := time.Now()
				if err := os.Chtimes(path, now, now); err != nil {
					t.Fatal(err)
				}
			}

			res, err := ReconcileMissing(db, tmpDir, reconcileOpts(stateDir))
			var liveFiles, liveClips int
			db.Model(&models.VideoFile{}).Count(&liveFiles)
			db.Model(&models.Clip{}).Count(&liveClips)
			if liveFiles != 1 || liveClips != 1 {
				t.Fatalf("%s deleted rows: files=%d clips=%d res=%+v err=%v", tc.name, liveFiles, liveClips, res, err)
			}
			if err == nil {
				t.Fatalf("%s: expected error (pending reset or invalid), got nil", tc.name)
			}
		})
	}
}

func mustAbs(t *testing.T, p string) string {
	t.Helper()
	abs, err := filepath.Abs(p)
	if err != nil {
		t.Fatal(err)
	}
	return abs
}

// TestReconcileMissing_RejectsPendingSymlinks: file/dir/ancestor/temp symlinks
// must not authorize deletes or overwrite sentinels (Codex High #2).
func TestReconcileMissing_RejectsPendingSymlinks(t *testing.T) {
	db := setupFableADB(t)
	defer db.Close()

	tmpDir, err := os.MkdirTemp("", "reconcile_symlink_root")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(tmpDir)

	videoPath := filepath.Join(tmpDir, "gone-front.mp4")
	clip := models.Clip{Event: "Sentry", SourceDir: tmpDir}
	db.Create(&clip)
	vf := models.VideoFile{ClipID: clip.ID, Camera: "Front", FilePath: videoPath}
	db.Create(&vf)

	t.Run("file-symlink", func(t *testing.T) {
		stateDir := testStateDir(t)
		if _, err := ReconcileMissing(db, tmpDir, reconcileOpts(stateDir)); !errors.Is(err, ErrReconcilePendingConfirm) {
			t.Fatalf("first: %v", err)
		}
		if err := agePendingPurgeForTest(stateDir, tmpDir, reconcileConfirmMinInterval+time.Second); err != nil {
			t.Fatal(err)
		}
		path := filepath.Join(stateDir, pendingPurgeFileName)
		payload, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		outside := filepath.Join(stateDir, "evil.json")
		if err := os.WriteFile(outside, payload, 0644); err != nil {
			t.Fatal(err)
		}
		if err := os.Remove(path); err != nil {
			t.Fatal(err)
		}
		if err := os.Symlink(outside, path); err != nil {
			t.Fatal(err)
		}
		res, err := ReconcileMissing(db, tmpDir, reconcileOpts(stateDir))
		var live int
		db.Model(&models.VideoFile{}).Count(&live)
		if live != 1 {
			t.Fatalf("file-symlink authorized delete: live=%d res=%+v err=%v", live, res, err)
		}
		if err == nil {
			t.Fatal("file-symlink: expected error")
		}
	})

	t.Run("directory-symlink", func(t *testing.T) {
		realState := testStateDir(t)
		if _, err := ReconcileMissing(db, tmpDir, reconcileOpts(realState)); !errors.Is(err, ErrReconcilePendingConfirm) {
			t.Fatalf("first: %v", err)
		}
		if err := agePendingPurgeForTest(realState, tmpDir, reconcileConfirmMinInterval+time.Second); err != nil {
			t.Fatal(err)
		}
		parent, err := os.MkdirTemp("", "reconcile_state_parent")
		if err != nil {
			t.Fatal(err)
		}
		defer os.RemoveAll(parent)
		link := filepath.Join(parent, "link-state")
		if err := os.Symlink(realState, link); err != nil {
			t.Fatal(err)
		}
		res, err := ReconcileMissing(db, tmpDir, reconcileOpts(link))
		var live int
		db.Model(&models.VideoFile{}).Count(&live)
		if live != 1 {
			t.Fatalf("directory-symlink authorized delete: live=%d res=%+v err=%v", live, res, err)
		}
		if err == nil {
			t.Fatal("directory-symlink: expected error")
		}
	})

	t.Run("ancestor-symlink", func(t *testing.T) {
		base, err := os.MkdirTemp("", "reconcile_anc")
		if err != nil {
			t.Fatal(err)
		}
		defer os.RemoveAll(base)
		real := filepath.Join(base, "real")
		if err := os.MkdirAll(real, 0755); err != nil {
			t.Fatal(err)
		}
		if _, err := ReconcileMissing(db, tmpDir, reconcileOpts(real)); !errors.Is(err, ErrReconcilePendingConfirm) {
			t.Fatalf("first: %v", err)
		}
		if err := agePendingPurgeForTest(real, tmpDir, reconcileConfirmMinInterval+time.Second); err != nil {
			t.Fatal(err)
		}
		// Replace parent with a symlink so openStateDirNoFollow fails mid-walk.
		moved := filepath.Join(base, "moved-real")
		if err := os.Rename(real, moved); err != nil {
			t.Fatal(err)
		}
		if err := os.Symlink(moved, real); err != nil {
			t.Fatal(err)
		}
		nested := filepath.Join(real, "nested")
		_ = os.MkdirAll(nested, 0755)
		// Point reconcile at a path whose ancestor is the symlink.
		res, err := ReconcileMissing(db, tmpDir, reconcileOpts(nested))
		var live int
		db.Model(&models.VideoFile{}).Count(&live)
		if live != 1 {
			t.Fatalf("ancestor-symlink authorized delete: live=%d res=%+v err=%v", live, res, err)
		}
		// Nested under symlink ancestor: save or load should fail closed.
		if err == nil && res.SoftDeletedFiles > 0 {
			t.Fatalf("ancestor-symlink deleted: %+v", res)
		}
	})

	t.Run("temporary-symlink", func(t *testing.T) {
		stateDir := testStateDir(t)
		sentinel := filepath.Join(stateDir, "sentinel.txt")
		if err := os.WriteFile(sentinel, []byte("keep-me"), 0644); err != nil {
			t.Fatal(err)
		}
		tmp := filepath.Join(stateDir, pendingPurgeFileName+".tmp")
		if err := os.Symlink(sentinel, tmp); err != nil {
			t.Fatal(err)
		}
		_, err := ReconcileMissing(db, tmpDir, reconcileOpts(stateDir))
		body, readErr := os.ReadFile(sentinel)
		if readErr != nil {
			t.Fatal(readErr)
		}
		if string(body) != "keep-me" {
			t.Fatalf("temporary-symlink overwrote sentinel: %q err=%v", body, err)
		}
		if err == nil {
			t.Fatal("temporary-symlink: expected error refusing write")
		}
		var live int
		db.Model(&models.VideoFile{}).Count(&live)
		if live != 1 {
			t.Fatalf("temporary-symlink side-effect deleted rows: live=%d", live)
		}
	})
}

// TestReconcileMissing_FailedClearDoesNotReuseConfirmation (Codex Medium #3).
func TestReconcileMissing_FailedClearDoesNotReuseConfirmation(t *testing.T) {
	db := setupFableADB(t)
	defer db.Close()

	tmpDir, err := os.MkdirTemp("", "reconcile_failclear")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(tmpDir)

	stateParent, err := os.MkdirTemp("", "reconcile_failclear_state")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(stateParent)
	stateDir := filepath.Join(stateParent, "cfg")
	if err := os.MkdirAll(stateDir, 0755); err != nil {
		t.Fatal(err)
	}

	saved := filepath.Join(tmpDir, "SavedClips", "evt")
	videoPath := filepath.Join(saved, "2024-09-02_12-00-00-front.mp4")
	writeDummyMP4(t, videoPath)
	clip := models.Clip{Event: "Saved", SourceDir: saved}
	db.Create(&clip)
	db.Create(&models.VideoFile{ClipID: clip.ID, Camera: "Front", FilePath: videoPath})

	aside := filepath.Join(tmpDir, "SavedClips.aside")
	if err := os.Rename(filepath.Join(tmpDir, "SavedClips"), aside); err != nil {
		t.Fatal(err)
	}

	if _, err := ReconcileMissing(db, tmpDir, reconcileOpts(stateDir)); !errors.Is(err, ErrReconcilePendingConfirm) {
		t.Fatalf("first: %v", err)
	}
	if err := agePendingPurgeForTest(stateDir, tmpDir, reconcileConfirmMinInterval+time.Second); err != nil {
		t.Fatal(err)
	}

	// Restore footage, then make state dir unwritable so clear fails.
	if err := os.Rename(aside, filepath.Join(tmpDir, "SavedClips")); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(stateDir, 0500); err != nil {
		t.Fatal(err)
	}
	defer os.Chmod(stateDir, 0755)

	_, err = ReconcileMissing(db, tmpDir, reconcileOpts(stateDir))
	if err == nil {
		t.Fatal("healthy pass with unwritable state must surface clear failure")
	}

	// New outage: must not immediately delete using the mature pending file.
	if err := os.Rename(filepath.Join(tmpDir, "SavedClips"), aside); err != nil {
		t.Fatal(err)
	}
	res, err := ReconcileMissing(db, tmpDir, reconcileOpts(stateDir))
	var liveFiles, liveClips int
	db.Model(&models.VideoFile{}).Count(&liveFiles)
	db.Model(&models.Clip{}).Count(&liveClips)
	if liveFiles != 1 || liveClips != 1 {
		t.Fatalf("reused confirmation deleted rows: files=%d clips=%d res=%+v err=%v", liveFiles, liveClips, res, err)
	}
	if err == nil {
		t.Fatal("expected error blocking reuse after failed clear")
	}
}

// TestReconcileMissing_RestoreDuringCollectionPreservesRows (Codex Medium #4).
func TestReconcileMissing_RestoreDuringCollectionPreservesRows(t *testing.T) {
	db := setupFableADB(t)
	defer db.Close()
	stateDir := testStateDir(t)

	tmpDir, err := os.MkdirTemp("", "reconcile_restore")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(tmpDir)

	saved := filepath.Join(tmpDir, "SavedClips", "evt")
	videoPath := filepath.Join(saved, "2024-09-03_12-00-00-front.mp4")
	writeDummyMP4(t, videoPath)
	clip := models.Clip{Event: "Saved", SourceDir: saved}
	db.Create(&clip)
	db.Create(&models.VideoFile{ClipID: clip.ID, Camera: "Front", FilePath: videoPath})

	aside := filepath.Join(tmpDir, "SavedClips.aside")
	if err := os.Rename(filepath.Join(tmpDir, "SavedClips"), aside); err != nil {
		t.Fatal(err)
	}

	if _, err := ReconcileMissing(db, tmpDir, reconcileOpts(stateDir)); !errors.Is(err, ErrReconcilePendingConfirm) {
		t.Fatalf("first: %v", err)
	}
	if err := agePendingPurgeForTest(stateDir, tmpDir, reconcileConfirmMinInterval+time.Second); err != nil {
		t.Fatal(err)
	}

	reconcileAfterCollectCandidates = func() {
		_ = os.Rename(aside, filepath.Join(tmpDir, "SavedClips"))
	}
	defer func() { reconcileAfterCollectCandidates = nil }()

	res, err := ReconcileMissing(db, tmpDir, reconcileOpts(stateDir))
	if err != nil {
		t.Fatalf("restore during collection: %v", err)
	}
	if res.SoftDeletedFiles != 0 || res.SoftDeletedClips != 0 {
		t.Fatalf("restored footage must not be deleted: %+v", res)
	}
	var liveFiles, liveClips int
	db.Model(&models.VideoFile{}).Count(&liveFiles)
	db.Model(&models.Clip{}).Count(&liveClips)
	if liveFiles != 1 || liveClips != 1 {
		t.Fatalf("want live rows after restore, got files=%d clips=%d", liveFiles, liveClips)
	}
	if _, err := os.Stat(videoPath); err != nil {
		t.Fatalf("physical file missing: %v", err)
	}
}

// TestReconcileMissing_ClockAnomaliesDoNotAuthorize (Codex Medium #5).
func TestReconcileMissing_ClockAnomaliesDoNotAuthorize(t *testing.T) {
	db := setupFableADB(t)
	defer db.Close()
	stateDir := testStateDir(t)

	tmpDir, err := os.MkdirTemp("", "reconcile_clock")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(tmpDir)

	videoPath := filepath.Join(tmpDir, "gone-front.mp4")
	clip := models.Clip{Event: "Sentry", SourceDir: tmpDir}
	db.Create(&clip)
	db.Create(&models.VideoFile{ClipID: clip.ID, Camera: "Front", FilePath: videoPath})

	if _, err := ReconcileMissing(db, tmpDir, reconcileOpts(stateDir)); !errors.Is(err, ErrReconcilePendingConfirm) {
		t.Fatalf("first: %v", err)
	}

	path := filepath.Join(stateDir, pendingPurgeFileName)
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var st pendingPurgeState
	if err := json.Unmarshal(raw, &st); err != nil {
		t.Fatal(err)
	}

	t.Run("plus-one-hour", func(t *testing.T) {
		cur := st
		cur.FirstSeen = time.Now().UTC().Add(-time.Hour)
		body, _ := json.MarshalIndent(&cur, "", "  ")
		if err := os.WriteFile(path, body, 0644); err != nil {
			t.Fatal(err)
		}
		now := time.Now()
		_ = os.Chtimes(path, now, now)
		forgetPendingObservation(filepath.Clean(mustAbs(t, tmpDir)), "")

		res, err := ReconcileMissing(db, tmpDir, reconcileOpts(stateDir))
		var live int
		db.Model(&models.VideoFile{}).Count(&live)
		if live != 1 {
			t.Fatalf("+1h clock step deleted: live=%d res=%+v err=%v", live, res, err)
		}
		if !errors.Is(err, ErrReconcilePendingConfirm) && err == nil {
			t.Fatalf("want pending/reset, got %v", err)
		}
	})

	t.Run("future", func(t *testing.T) {
		// Re-seed pending.
		forgetPendingObservation(filepath.Clean(mustAbs(t, tmpDir)), "")
		delete(pendingClearBlocked, filepath.Clean(mustAbs(t, tmpDir)))
		_ = os.Remove(path)
		if _, err := ReconcileMissing(db, tmpDir, reconcileOpts(stateDir)); !errors.Is(err, ErrReconcilePendingConfirm) {
			t.Fatalf("reseed: %v", err)
		}
		raw, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		var cur pendingPurgeState
		if err := json.Unmarshal(raw, &cur); err != nil {
			t.Fatal(err)
		}
		cur.FirstSeen = time.Now().UTC().Add(24 * time.Hour)
		body, _ := json.MarshalIndent(&cur, "", "  ")
		if err := os.WriteFile(path, body, 0644); err != nil {
			t.Fatal(err)
		}
		forgetPendingObservation(filepath.Clean(mustAbs(t, tmpDir)), "")

		res, err := ReconcileMissing(db, tmpDir, reconcileOpts(stateDir))
		var live int
		db.Model(&models.VideoFile{}).Count(&live)
		if live != 1 {
			t.Fatalf("future first_seen deleted: live=%d res=%+v err=%v", live, res, err)
		}
		if err == nil {
			t.Fatal("future first_seen must error or reset pending")
		}
	})
}
