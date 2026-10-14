package services

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io/ioutil"
	"log"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/jinzhu/gorm"
	"teslaxy/models"
)

// ErrReconcileRefused is returned when the footage root is missing while the
// database still has clips under that root. A missing mount must never soft-delete.
var ErrReconcileRefused = errors.New("refuse to reconcile: footage root is missing while the database has clips")

// ErrReconcilePendingConfirm is returned when missing footage was observed but
// soft-delete is deferred until a persisted, time-separated confirmation.
// Rows are left intact. The scanner schedules a retry after the remaining wait.
var ErrReconcilePendingConfirm = errors.New("reconcile pending: waiting for persisted delete confirmation")

// ErrReconcileRootChanged is returned when the footage root vanishes or its
// device/inode identity changes between soft-delete batches. Already-applied
// batches are not rolled back.
var ErrReconcileRootChanged = errors.New("refuse to reconcile: footage root vanished or changed identity mid-cleanup")

// ReconcileOpts controls ReconcileMissing behaviour.
type ReconcileOpts struct {
	// DryRun, when true, counts candidates without writing deleted_at or pending state.
	DryRun bool
	// ForceEmptyPurge soft-deletes missing rows in one pass when the root exists.
	// Used by the explicit operator purge path (ScannerService.PurgeMissing).
	ForceEmptyPurge bool
	// StateDir holds the persisted pending-confirm file. Empty uses CONFIG_PATH or ".".
	StateDir string
}

// ReconcileResult summarizes soft-deletes performed (or counted in DryRun).
type ReconcileResult struct {
	SoftDeletedFiles int `json:"soft_deleted_files"`
	SoftDeletedClips int `json:"soft_deleted_clips"`
	PendingConfirm   bool `json:"pending_confirm,omitempty"`
}

// rootIdentity is the on-disk identity of a footage root (device + inode).
type rootIdentity struct {
	dev uint64
	ino uint64
}

// pendingPurgeState is written under StateDir so delete-all confirmation
// survives process restarts.
type pendingPurgeState struct {
	AbsRoot     string    `json:"abs_root"`
	Fingerprint string    `json:"fingerprint"`
	FirstSeen   time.Time `json:"first_seen"`
	FileIDs     []uint    `json:"file_ids"`
	ClipIDs     []uint    `json:"clip_ids"`
}

var (
	// reconcileConfirmMinInterval is the minimum time between first sighting of
	// a missing-candidate set and soft-delete. Tests may lower it.
	reconcileConfirmMinInterval = 30 * time.Second

	// reconcileDeleteBatchSize is how many soft-deletes run between root
	// identity re-checks. Tests may lower it.
	reconcileDeleteBatchSize = 64

	// reconcileBeforeDeleteBatch is an optional test hook invoked immediately
	// before each delete-batch identity re-check.
	reconcileBeforeDeleteBatch func()

	reconcilePendingMu sync.Mutex
)

const pendingPurgeFileName = ".teslaxy-reconcile-pending.json"

// ReconcileMissing finds VideoFiles whose paths are gone from disk and Clips
// that would then have zero live VideoFiles. It holds mediaOpMu's write lock so
// retention (#238) and the scanner share one coordinator.
//
// Soft-delete policy (round-4 redesign):
//   - Never soft-delete while the footage root is missing (drive drop-out).
//   - Never soft-delete on first sighting of missing footage. A pending confirm
//     is persisted under StateDir; after reconcileConfirmMinInterval with the
//     same candidate fingerprint, a later pass (watcher retry or ScanAll) may
//     soft-delete. This survives process restarts.
//   - ForceEmptyPurge (explicit operator purge) soft-deletes immediately when
//     the root exists.
//   - Before each soft-delete batch the root must still be present with the
//     same device and inode captured at the start of the delete phase.
func ReconcileMissing(db *gorm.DB, root string, opts ReconcileOpts) (ReconcileResult, error) {
	var result ReconcileResult
	if db == nil {
		return result, nil
	}

	absRoot, err := filepath.Abs(root)
	if err != nil {
		return result, err
	}
	absRoot = filepath.Clean(absRoot)
	stateDir := resolveReconcileStateDir(opts.StateDir)

	lockMediaWrite()
	defer unlockMediaWrite()

	id, missing, _, err := inspectFootageRoot(absRoot)
	if err != nil {
		return result, err
	}

	has, err := liveFootageUnderRoot(db, absRoot)
	if err != nil {
		return result, err
	}

	if missing {
		if has {
			// Drive drop-out: never delete, leave any pending file for when the
			// root returns (fingerprint must still match).
			return result, fmt.Errorf("%w (%s is missing)", ErrReconcileRefused, absRoot)
		}
		clearPendingPurge(stateDir, absRoot)
		return result, nil
	}

	missingFiles, emptyClips, err := collectMissingCandidates(db, absRoot)
	if err != nil {
		return result, err
	}

	if opts.DryRun {
		result.SoftDeletedFiles = len(missingFiles)
		result.SoftDeletedClips = len(emptyClips)
		return result, nil
	}

	if len(missingFiles) == 0 && len(emptyClips) == 0 {
		clearPendingPurge(stateDir, absRoot)
		return result, nil
	}

	if opts.ForceEmptyPurge {
		clearPendingPurge(stateDir, absRoot)
		return applySoftDeletes(db, absRoot, id, missingFiles, emptyClips)
	}

	fp := candidateFingerprint(missingFiles, emptyClips)
	pending, err := loadPendingPurge(stateDir, absRoot)
	if err != nil {
		return result, err
	}

	now := time.Now().UTC()
	if pending == nil || pending.Fingerprint != fp {
		if err := savePendingPurge(stateDir, &pendingPurgeState{
			AbsRoot:     absRoot,
			Fingerprint: fp,
			FirstSeen:   now,
			FileIDs:     videoFileIDs(missingFiles),
			ClipIDs:     clipIDs(emptyClips),
		}); err != nil {
			return result, err
		}
		result.PendingConfirm = true
		return result, fmt.Errorf("%w (%s; %d file(s), %d clip(s); wait %s)",
			ErrReconcilePendingConfirm, absRoot, len(missingFiles), len(emptyClips), reconcileConfirmMinInterval)
	}

	elapsed := now.Sub(pending.FirstSeen)
	if elapsed < reconcileConfirmMinInterval {
		result.PendingConfirm = true
		remain := reconcileConfirmMinInterval - elapsed
		return result, fmt.Errorf("%w (%s; %s remaining)", ErrReconcilePendingConfirm, absRoot, remain.Round(time.Millisecond))
	}

	clearPendingPurge(stateDir, absRoot)
	return applySoftDeletes(db, absRoot, id, missingFiles, emptyClips)
}

// PurgeMissingFootage is the explicit operator purge: soft-delete missing
// VideoFiles and empty Clips under root in one pass. The root must exist.
func PurgeMissingFootage(db *gorm.DB, root string, stateDir string) (ReconcileResult, error) {
	return ReconcileMissing(db, root, ReconcileOpts{
		ForceEmptyPurge: true,
		StateDir:        stateDir,
	})
}

// PendingConfirmRemaining reports how long until a persisted pending confirm
// for absRoot may soft-delete. Returns 0 when ready or when no pending exists.
func PendingConfirmRemaining(stateDir, root string) time.Duration {
	absRoot, err := filepath.Abs(root)
	if err != nil {
		return reconcileConfirmMinInterval
	}
	absRoot = filepath.Clean(absRoot)
	pending, err := loadPendingPurge(resolveReconcileStateDir(stateDir), absRoot)
	if err != nil || pending == nil {
		return 0
	}
	remain := reconcileConfirmMinInterval - time.Since(pending.FirstSeen)
	if remain < 0 {
		return 0
	}
	return remain
}

func applySoftDeletes(db *gorm.DB, absRoot string, id rootIdentity, missingFiles []models.VideoFile, emptyClips []models.Clip) (ReconcileResult, error) {
	var result ReconcileResult
	if err := deleteVideoFilesInBatches(db, absRoot, id, missingFiles, &result); err != nil {
		return result, err
	}
	if err := deleteClipsInBatches(db, absRoot, id, emptyClips, &result); err != nil {
		return result, err
	}
	if result.SoftDeletedFiles > 0 || result.SoftDeletedClips > 0 {
		log.Printf("ReconcileMissing: soft-deleted %d video file(s), %d clip(s)",
			result.SoftDeletedFiles, result.SoftDeletedClips)
	}
	return result, nil
}

func collectMissingCandidates(db *gorm.DB, absRoot string) ([]models.VideoFile, []models.Clip, error) {
	var files []models.VideoFile
	if err := db.Find(&files).Error; err != nil {
		return nil, nil, err
	}

	missingByClip := map[uint]int{}
	clipsWithFiles := map[uint]struct{}{}
	var missingFiles []models.VideoFile

	for i := range files {
		vf := &files[i]
		if vf.FilePath == "" {
			continue
		}
		absPath, err := filepath.Abs(vf.FilePath)
		if err != nil {
			continue
		}
		absPath = filepath.Clean(absPath)
		if absPath != absRoot && !isUnderRoot(absRoot, absPath) {
			continue
		}
		clipsWithFiles[vf.ClipID] = struct{}{}
		if _, err := os.Stat(absPath); err == nil {
			continue
		} else if !os.IsNotExist(err) {
			log.Printf("ReconcileMissing: skip file %s: %v", absPath, err)
			continue
		}
		missingByClip[vf.ClipID]++
		missingFiles = append(missingFiles, *vf)
	}

	var clips []models.Clip
	if err := db.Find(&clips).Error; err != nil {
		return nil, nil, err
	}
	var emptyClips []models.Clip
	for i := range clips {
		clip := &clips[i]
		if !clipScopedToRoot(clip, absRoot, clipsWithFiles) {
			continue
		}
		var live int
		if err := db.Model(&models.VideoFile{}).Where("clip_id = ?", clip.ID).Count(&live).Error; err != nil {
			return nil, nil, err
		}
		live -= missingByClip[clip.ID]
		if live > 0 {
			continue
		}
		emptyClips = append(emptyClips, *clip)
	}
	return missingFiles, emptyClips, nil
}

func deleteVideoFilesInBatches(db *gorm.DB, absRoot string, id rootIdentity, files []models.VideoFile, result *ReconcileResult) error {
	batch := reconcileDeleteBatchSize
	if batch < 1 {
		batch = 1
	}
	for i := 0; i < len(files); i += batch {
		if reconcileBeforeDeleteBatch != nil {
			reconcileBeforeDeleteBatch()
		}
		if err := assertRootUnchanged(absRoot, id); err != nil {
			return err
		}
		end := i + batch
		if end > len(files) {
			end = len(files)
		}
		for j := i; j < end; j++ {
			vf := files[j]
			if err := db.Delete(&vf).Error; err != nil {
				log.Printf("ReconcileMissing: soft-delete VideoFile %d: %v", vf.ID, err)
				continue
			}
			result.SoftDeletedFiles++
		}
	}
	return nil
}

func deleteClipsInBatches(db *gorm.DB, absRoot string, id rootIdentity, clips []models.Clip, result *ReconcileResult) error {
	batch := reconcileDeleteBatchSize
	if batch < 1 {
		batch = 1
	}
	for i := 0; i < len(clips); i += batch {
		if reconcileBeforeDeleteBatch != nil {
			reconcileBeforeDeleteBatch()
		}
		if err := assertRootUnchanged(absRoot, id); err != nil {
			return err
		}
		end := i + batch
		if end > len(clips) {
			end = len(clips)
		}
		for j := i; j < end; j++ {
			clip := clips[j]
			var live int
			if err := db.Model(&models.VideoFile{}).Where("clip_id = ?", clip.ID).Count(&live).Error; err != nil {
				return err
			}
			if live > 0 {
				continue
			}
			if err := db.Delete(&clip).Error; err != nil {
				log.Printf("ReconcileMissing: soft-delete Clip %d: %v", clip.ID, err)
				continue
			}
			result.SoftDeletedClips++
		}
	}
	return nil
}

func inspectFootageRoot(absRoot string) (id rootIdentity, missing bool, empty bool, err error) {
	info, statErr := os.Stat(absRoot)
	if statErr != nil {
		if os.IsNotExist(statErr) {
			return rootIdentity{}, true, false, nil
		}
		return rootIdentity{}, false, false, statErr
	}
	if !info.IsDir() {
		return rootIdentity{}, false, false, fmt.Errorf("footage root %s is not a directory", absRoot)
	}
	id, err = identityFromFileInfo(info)
	if err != nil {
		return rootIdentity{}, false, false, err
	}
	entries, readErr := os.ReadDir(absRoot)
	if readErr != nil {
		return rootIdentity{}, false, false, readErr
	}
	return id, false, len(entries) == 0, nil
}

func identityFromFileInfo(info os.FileInfo) (rootIdentity, error) {
	st, ok := info.Sys().(*syscall.Stat_t)
	if !ok || st == nil {
		return rootIdentity{}, fmt.Errorf("footage root stat type %T is unsupported", info.Sys())
	}
	return rootIdentity{dev: uint64(st.Dev), ino: uint64(st.Ino)}, nil
}

func assertRootUnchanged(absRoot string, want rootIdentity) error {
	info, err := os.Stat(absRoot)
	if err != nil {
		if os.IsNotExist(err) {
			return fmt.Errorf("%w (%s vanished)", ErrReconcileRootChanged, absRoot)
		}
		return err
	}
	got, err := identityFromFileInfo(info)
	if err != nil {
		return err
	}
	if got != want {
		return fmt.Errorf("%w (%s identity changed)", ErrReconcileRootChanged, absRoot)
	}
	return nil
}

func resolveReconcileStateDir(stateDir string) string {
	if stateDir != "" {
		return stateDir
	}
	if p := os.Getenv("CONFIG_PATH"); p != "" {
		return p
	}
	return "."
}

func pendingPurgePath(stateDir string) string {
	return filepath.Join(stateDir, pendingPurgeFileName)
}

func loadPendingPurge(stateDir, absRoot string) (*pendingPurgeState, error) {
	reconcilePendingMu.Lock()
	defer reconcilePendingMu.Unlock()

	path := pendingPurgePath(stateDir)
	data, err := ioutil.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}
	var st pendingPurgeState
	if err := json.Unmarshal(data, &st); err != nil {
		return nil, err
	}
	if st.AbsRoot != absRoot {
		return nil, nil
	}
	return &st, nil
}

func savePendingPurge(stateDir string, st *pendingPurgeState) error {
	reconcilePendingMu.Lock()
	defer reconcilePendingMu.Unlock()

	if err := os.MkdirAll(stateDir, 0755); err != nil {
		return err
	}
	data, err := json.MarshalIndent(st, "", "  ")
	if err != nil {
		return err
	}
	path := pendingPurgePath(stateDir)
	tmp := path + ".tmp"
	if err := ioutil.WriteFile(tmp, data, 0644); err != nil {
		return err
	}
	return os.Rename(tmp, path)
}

func clearPendingPurge(stateDir, absRoot string) {
	reconcilePendingMu.Lock()
	defer reconcilePendingMu.Unlock()

	path := pendingPurgePath(stateDir)
	data, err := ioutil.ReadFile(path)
	if err != nil {
		return
	}
	var st pendingPurgeState
	if json.Unmarshal(data, &st) != nil {
		_ = os.Remove(path)
		return
	}
	if st.AbsRoot == absRoot || st.AbsRoot == "" {
		_ = os.Remove(path)
	}
}

// agePendingPurgeForTest backdates FirstSeen so confirmation can proceed.
// Tests only.
func agePendingPurgeForTest(stateDir, root string, ago time.Duration) error {
	absRoot, err := filepath.Abs(root)
	if err != nil {
		return err
	}
	absRoot = filepath.Clean(absRoot)
	st, err := loadPendingPurge(resolveReconcileStateDir(stateDir), absRoot)
	if err != nil {
		return err
	}
	if st == nil {
		return fmt.Errorf("no pending purge for %s", absRoot)
	}
	st.FirstSeen = time.Now().UTC().Add(-ago)
	return savePendingPurge(resolveReconcileStateDir(stateDir), st)
}

func candidateFingerprint(files []models.VideoFile, clips []models.Clip) string {
	ids := make([]string, 0, len(files)+len(clips))
	for _, f := range files {
		ids = append(ids, fmt.Sprintf("f:%d", f.ID))
	}
	for _, c := range clips {
		ids = append(ids, fmt.Sprintf("c:%d", c.ID))
	}
	sort.Strings(ids)
	sum := sha256.Sum256([]byte(strings.Join(ids, ",")))
	return hex.EncodeToString(sum[:])
}

func videoFileIDs(files []models.VideoFile) []uint {
	out := make([]uint, len(files))
	for i, f := range files {
		out[i] = f.ID
	}
	return out
}

func clipIDs(clips []models.Clip) []uint {
	out := make([]uint, len(clips))
	for i, c := range clips {
		out[i] = c.ID
	}
	return out
}

func liveFootageUnderRoot(db *gorm.DB, absRoot string) (bool, error) {
	var files []models.VideoFile
	if err := db.Find(&files).Error; err != nil {
		return false, err
	}
	for i := range files {
		if fileUnderRoot(files[i].FilePath, absRoot) {
			return true, nil
		}
	}
	var clips []models.Clip
	if err := db.Find(&clips).Error; err != nil {
		return false, err
	}
	for i := range clips {
		if fileUnderRoot(clips[i].SourceDir, absRoot) {
			return true, nil
		}
	}
	return false, nil
}

func fileUnderRoot(path, absRoot string) bool {
	if path == "" || absRoot == "" {
		return false
	}
	absPath, err := filepath.Abs(path)
	if err != nil {
		return false
	}
	absPath = filepath.Clean(absPath)
	return absPath == absRoot || isUnderRoot(absRoot, absPath)
}

func clipScopedToRoot(clip *models.Clip, absRoot string, clipsWithFiles map[uint]struct{}) bool {
	if clip == nil {
		return false
	}
	if _, ok := clipsWithFiles[clip.ID]; ok {
		return true
	}
	return fileUnderRoot(clip.SourceDir, absRoot)
}

func isUnderRoot(root, path string) bool {
	rel, err := filepath.Rel(root, path)
	if err != nil {
		return false
	}
	if rel == ".." || strings.HasPrefix(rel, ".."+string(os.PathSeparator)) {
		return false
	}
	return true
}
