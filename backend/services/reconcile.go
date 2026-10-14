package services

import (
	"errors"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"strings"

	"github.com/jinzhu/gorm"
	"teslaxy/models"
)

// ErrReconcileRefused is returned when the footage root is missing or empty
// while the database still has clips under that root. Deleting in that state
// wipes the library, and putting the files back does not restore the rows
// until a later full scan.
var ErrReconcileRefused = errors.New("refuse to reconcile: footage root is missing or empty while the database has clips")

// ReconcileOpts controls ReconcileMissing behaviour.
// Empty opts use defaults (soft-delete missing VideoFiles and empty Clips under root).
type ReconcileOpts struct {
	// DryRun, when true, counts candidates without writing deleted_at.
	DryRun bool
}

// ReconcileResult summarizes soft-deletes performed (or counted in DryRun).
type ReconcileResult struct {
	SoftDeletedFiles int `json:"soft_deleted_files"`
	SoftDeletedClips int `json:"soft_deleted_clips"`
}

// ReconcileMissing soft-deletes VideoFiles whose paths are gone from disk and
// Clips that then have zero live VideoFiles. It holds mediaOpMu's write lock so
// retention (#238) and the scanner share one coordinator.
//
// root is the footage root; only file_path values under root are considered.
// Paths outside root are left alone (safety).
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

	// A missing mount, or an empty directory swapped in for the root, makes
	// every file look deleted. Refuse instead of wiping clips that a restore
	// would not bring back until a full scan.
	missing, empty, err := rootMissingOrEmpty(absRoot)
	if err != nil {
		return result, err
	}
	if missing || empty {
		has, err := liveFootageUnderRoot(db, absRoot)
		if err != nil {
			return result, err
		}
		if has {
			state := "empty"
			if missing {
				state = "missing"
			}
			return result, fmt.Errorf("%w (%s is %s)", ErrReconcileRefused, absRoot, state)
		}
	}

	var files []models.VideoFile
	if err := db.Find(&files).Error; err != nil {
		return result, err
	}

	// clipID -> number of live files that would be removed (for DryRun clip counts).
	missingByClip := map[uint]int{}
	// Clips that have at least one live VideoFile under this root.
	clipsWithFiles := map[uint]struct{}{}

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

		result.SoftDeletedFiles++
		missingByClip[vf.ClipID]++
		if opts.DryRun {
			continue
		}
		if err := db.Delete(vf).Error; err != nil {
			log.Printf("ReconcileMissing: soft-delete VideoFile %d: %v", vf.ID, err)
			result.SoftDeletedFiles--
			missingByClip[vf.ClipID]--
		}
	}

	// Soft-delete clips with no remaining live video files.
	var clips []models.Clip
	if err := db.Find(&clips).Error; err != nil {
		return result, err
	}
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
		if opts.DryRun {
			live -= missingByClip[clip.ID]
		}
		if live > 0 {
			continue
		}
		result.SoftDeletedClips++
		if opts.DryRun {
			continue
		}
		if err := db.Delete(clip).Error; err != nil {
			log.Printf("ReconcileMissing: soft-delete Clip %d: %v", clip.ID, err)
			result.SoftDeletedClips--
		}
	}

	if result.SoftDeletedFiles > 0 || result.SoftDeletedClips > 0 {
		log.Printf("ReconcileMissing: soft-deleted %d video file(s), %d clip(s) (dry_run=%v)",
			result.SoftDeletedFiles, result.SoftDeletedClips, opts.DryRun)
	}
	return result, nil
}

// rootMissingOrEmpty reports whether absRoot does not exist, or exists as a
// directory with no entries. Other stat/read errors are returned.
func rootMissingOrEmpty(absRoot string) (missing bool, empty bool, err error) {
	info, statErr := os.Stat(absRoot)
	if statErr != nil {
		if os.IsNotExist(statErr) {
			return true, false, nil
		}
		return false, false, statErr
	}
	if !info.IsDir() {
		return false, false, fmt.Errorf("footage root %s is not a directory", absRoot)
	}
	entries, readErr := os.ReadDir(absRoot)
	if readErr != nil {
		return false, false, readErr
	}
	return false, len(entries) == 0, nil
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
