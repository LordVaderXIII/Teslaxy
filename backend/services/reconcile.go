package services

import (
	"log"
	"os"
	"path/filepath"
	"strings"

	"github.com/jinzhu/gorm"
	"teslaxy/models"
)

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

	var files []models.VideoFile
	if err := db.Find(&files).Error; err != nil {
		return result, err
	}

	// clipID -> number of live files that would be removed (for DryRun clip counts).
	missingByClip := map[uint]int{}

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
