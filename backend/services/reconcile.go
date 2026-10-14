package services

import (
	"errors"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"syscall"

	"github.com/jinzhu/gorm"
	"teslaxy/models"
)

// ErrReconcileRefused is returned when the footage root is missing, or is empty
// while the database still has clips under that root and a second consecutive
// confirm (or ForceEmptyPurge) has not been granted. Deleting in that state
// would wipe the library, and putting the files back would not restore the rows
// until a later full scan.
var ErrReconcileRefused = errors.New("refuse to reconcile: footage root is missing or empty while the database has clips")

// ErrReconcileRootChanged is returned when the footage root vanishes or its
// device/inode identity changes after candidates were collected but before (or
// between) soft-delete batches. Already-applied batches are not rolled back.
var ErrReconcileRootChanged = errors.New("refuse to reconcile: footage root vanished or changed identity mid-cleanup")

// ReconcileOpts controls ReconcileMissing behaviour.
// Empty opts use defaults (soft-delete missing VideoFiles and empty Clips under root).
type ReconcileOpts struct {
	// DryRun, when true, counts candidates without writing deleted_at.
	DryRun bool
	// ForceEmptyPurge allows soft-deleting under an empty-but-present root in a
	// single pass. The root must still exist; a missing mount is always refused.
	// Prefer the automatic two consecutive empty scans when the root identity is
	// stable (same device/inode as when it last held content, or two matching
	// empty sightings on cold start).
	ForceEmptyPurge bool
}

// ReconcileResult summarizes soft-deletes performed (or counted in DryRun).
type ReconcileResult struct {
	SoftDeletedFiles int `json:"soft_deleted_files"`
	SoftDeletedClips int `json:"soft_deleted_clips"`
}

// rootIdentity is the stable on-disk identity of a footage root (device + inode).
type rootIdentity struct {
	dev uint64
	ino uint64
}

var (
	reconcileRootMu sync.Mutex
	// lastNonEmptyRoot maps abs root -> identity when the root was last seen
	// with at least one entry. Used to refuse empty directories that were swapped
	// in for a previously non-empty mount (different inode).
	lastNonEmptyRoot = map[string]rootIdentity{}
	// emptyRootPending maps abs root -> identity awaiting a second consecutive
	// empty confirm before purging dead library rows.
	emptyRootPending = map[string]rootIdentity{}

	// reconcileDeleteBatchSize is how many soft-deletes run between root
	// identity re-checks. Tests may lower it.
	reconcileDeleteBatchSize = 64

	// reconcileBeforeDeleteBatch is an optional test hook invoked immediately
	// before each delete-batch identity re-check.
	reconcileBeforeDeleteBatch func()
)

// resetReconcileRootState clears empty-root confirmation state. Tests only.
func resetReconcileRootState() {
	reconcileRootMu.Lock()
	defer reconcileRootMu.Unlock()
	lastNonEmptyRoot = map[string]rootIdentity{}
	emptyRootPending = map[string]rootIdentity{}
}

// ReconcileMissing soft-deletes VideoFiles whose paths are gone from disk and
// Clips that then have zero live VideoFiles. It holds mediaOpMu's write lock so
// retention (#238) and the scanner share one coordinator.
//
// root is the footage root; only file_path values under root are considered.
// Paths outside root are left alone (safety).
//
// Safety:
//   - A missing root never deletes (drive drop-out).
//   - An empty root deletes only after two consecutive scans with a stable
//     device/inode identity, or when ForceEmptyPurge is set. An empty directory
//     whose identity differs from the last non-empty sighting is refused
//     (swapped mount / placeholder).
//   - Before each soft-delete batch the root must still be present with the
//     same device and inode captured at the start of the pass.
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

	lockMediaWrite()
	defer unlockMediaWrite()

	id, missing, empty, err := inspectFootageRoot(absRoot)
	if err != nil {
		return result, err
	}

	has, err := liveFootageUnderRoot(db, absRoot)
	if err != nil {
		return result, err
	}

	if missing {
		clearEmptyRootPending(absRoot)
		if has {
			return result, fmt.Errorf("%w (%s is missing)", ErrReconcileRefused, absRoot)
		}
		return result, nil
	}

	if empty {
		if !has {
			// Empty root, nothing in the DB under it: no-op.
			return result, nil
		}
		if err := gateEmptyRootPurge(absRoot, id, opts.ForceEmptyPurge); err != nil {
			return result, err
		}
		// Fall through: second consecutive empty confirm (or ForceEmptyPurge).
	} else {
		rememberNonEmptyRoot(absRoot, id)
	}

	var files []models.VideoFile
	if err := db.Find(&files).Error; err != nil {
		return result, err
	}

	// clipID -> number of live files that would be removed (for DryRun clip counts).
	missingByClip := map[uint]int{}
	// Clips that have at least one live VideoFile under this root.
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
		// Only reconcile footage under the configured root.
		if absPath != absRoot && !isUnderRoot(absRoot, absPath) {
			continue
		}
		clipsWithFiles[vf.ClipID] = struct{}{}
		if _, err := os.Stat(absPath); err == nil {
			continue
		} else if !os.IsNotExist(err) {
			// Permission or transient errors: do not soft-delete.
			log.Printf("ReconcileMissing: skip file %s: %v", absPath, err)
			continue
		}

		missingByClip[vf.ClipID]++
		missingFiles = append(missingFiles, *vf)
	}

	// Soft-delete clips with no remaining live video files (computed after
	// collecting missing files so DryRun can adjust live counts).
	var clips []models.Clip
	if err := db.Find(&clips).Error; err != nil {
		return result, err
	}
	var emptyClips []models.Clip
	for i := range clips {
		clip := &clips[i]
		// Clip cleanup is scoped to the requested root. An empty clip that
		// belongs to a different footage root must survive this pass.
		if !clipScopedToRoot(clip, absRoot, clipsWithFiles) {
			continue
		}
		var live int
		if err := db.Model(&models.VideoFile{}).Where("clip_id = ?", clip.ID).Count(&live).Error; err != nil {
			return result, err
		}
		live -= missingByClip[clip.ID]
		if live > 0 {
			continue
		}
		emptyClips = append(emptyClips, *clip)
	}

	if opts.DryRun {
		result.SoftDeletedFiles = len(missingFiles)
		result.SoftDeletedClips = len(emptyClips)
		if result.SoftDeletedFiles > 0 || result.SoftDeletedClips > 0 {
			log.Printf("ReconcileMissing: soft-deleted %d video file(s), %d clip(s) (dry_run=%v)",
				result.SoftDeletedFiles, result.SoftDeletedClips, opts.DryRun)
		}
		return result, nil
	}

	if len(missingFiles) == 0 && len(emptyClips) == 0 {
		return result, nil
	}

	// Re-check identity before the first delete batch, then again before each
	// subsequent batch, so a mid-pass mount drop or empty-folder swap aborts.
	if err := deleteVideoFilesInBatches(db, absRoot, id, missingFiles, &result); err != nil {
		return result, err
	}
	if err := deleteClipsInBatches(db, absRoot, id, emptyClips, &result); err != nil {
		return result, err
	}

	if result.SoftDeletedFiles > 0 || result.SoftDeletedClips > 0 {
		log.Printf("ReconcileMissing: soft-deleted %d video file(s), %d clip(s) (dry_run=%v)",
			result.SoftDeletedFiles, result.SoftDeletedClips, opts.DryRun)
	}
	return result, nil
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
			// Re-count live files in case a prior batch aborted partway; only
			// soft-delete when nothing remains.
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

// inspectFootageRoot reports the root's identity and whether it is missing or
// an empty directory. Other stat/read errors are returned.
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

// assertRootUnchanged fails when absRoot is gone or no longer the same device/inode.
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

func rememberNonEmptyRoot(absRoot string, id rootIdentity) {
	reconcileRootMu.Lock()
	defer reconcileRootMu.Unlock()
	lastNonEmptyRoot[absRoot] = id
	delete(emptyRootPending, absRoot)
}

func clearEmptyRootPending(absRoot string) {
	reconcileRootMu.Lock()
	defer reconcileRootMu.Unlock()
	delete(emptyRootPending, absRoot)
}

// gateEmptyRootPurge returns nil when an empty-but-present root may soft-delete.
// A first empty sighting (or a swapped inode) returns ErrReconcileRefused.
func gateEmptyRootPurge(absRoot string, id rootIdentity, force bool) error {
	reconcileRootMu.Lock()
	defer reconcileRootMu.Unlock()

	if force {
		delete(emptyRootPending, absRoot)
		return nil
	}

	if known, ok := lastNonEmptyRoot[absRoot]; ok && known != id {
		// Empty placeholder swapped in for a different directory (new inode).
		delete(emptyRootPending, absRoot)
		return fmt.Errorf("%w (%s is empty with a different identity than the last non-empty root)", ErrReconcileRefused, absRoot)
	}

	if pending, ok := emptyRootPending[absRoot]; ok && pending == id {
		// Second consecutive empty sighting with stable identity.
		delete(emptyRootPending, absRoot)
		return nil
	}

	emptyRootPending[absRoot] = id
	return fmt.Errorf("%w (%s is empty; confirm on next scan with the same root identity)", ErrReconcileRefused, absRoot)
}

// liveFootageUnderRoot is true when a live clip or video file belongs to absRoot.
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

// clipScopedToRoot is true when the clip's source directory is under absRoot
// or it still has (or just lost) a video file under absRoot.
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
