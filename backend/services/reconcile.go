package services

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/jinzhu/gorm"
	"golang.org/x/sys/unix"
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

// ErrReconcilePendingInvalid is returned when on-disk pending state is corrupt,
// symlink-substituted, or cannot be invalidated after a healthy pass.
var ErrReconcilePendingInvalid = errors.New("refuse to reconcile: pending confirmation state is invalid")

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
	SoftDeletedFiles int  `json:"soft_deleted_files"`
	SoftDeletedClips int  `json:"soft_deleted_clips"`
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
	Canceled    bool      `json:"canceled,omitempty"`
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

	// reconcileAfterCollectCandidates is an optional test hook invoked after
	// missing-candidate collection and before pending-state load/confirm.
	reconcileAfterCollectCandidates func()

	reconcilePendingMu sync.Mutex

	// pendingObservedAt tracks process-local (monotonic) first observation per
	// absRoot+fingerprint. Wall-clock JSON alone must not authorize deletion.
	pendingObservedAt = map[string]time.Time{}

	// pendingClearBlocked marks absRoots where a healthy pass could not
	// invalidate canceled confirmation; confirm must not reuse that state.
	pendingClearBlocked = map[string]bool{}
)

const pendingPurgeFileName = ".teslaxy-reconcile-pending.json"

// ReconcileMissing finds VideoFiles whose paths are gone from disk and Clips
// that would then have zero live VideoFiles. It holds mediaOpMu's write lock so
// retention (#238) and the scanner share one coordinator.
//
// Soft-delete policy (round-4/5 redesign):
//   - Never soft-delete while the footage root is missing (drive drop-out).
//   - Never soft-delete on first sighting of missing footage. A pending confirm
//     is persisted under StateDir; after reconcileConfirmMinInterval with the
//     same candidate fingerprint, a later pass (watcher retry or ScanAll) may
//     soft-delete. This survives process restarts.
//   - ForceEmptyPurge (explicit operator purge) soft-deletes immediately when
//     the root exists.
//   - Before each soft-delete batch the root must still be present with the
//     same device and inode captured at the start of the delete phase.
//   - Pending state is validated for integrity; reads/writes refuse symlinks.
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
		if err := clearPendingPurge(stateDir, absRoot); err != nil {
			return result, err
		}
		return result, nil
	}

	missingFiles, emptyClips, err := collectMissingCandidates(db, absRoot)
	if err != nil {
		return result, err
	}

	if reconcileAfterCollectCandidates != nil {
		reconcileAfterCollectCandidates()
	}

	if opts.DryRun {
		result.SoftDeletedFiles = len(missingFiles)
		result.SoftDeletedClips = len(emptyClips)
		return result, nil
	}

	if len(missingFiles) == 0 && len(emptyClips) == 0 {
		if err := clearPendingPurge(stateDir, absRoot); err != nil {
			return result, err
		}
		return result, nil
	}

	if opts.ForceEmptyPurge {
		if err := clearPendingPurge(stateDir, absRoot); err != nil {
			return result, err
		}
		missingFiles, emptyClips, err = recheckMissingCandidates(db, absRoot, missingFiles, emptyClips)
		if err != nil {
			return result, err
		}
		if len(missingFiles) == 0 && len(emptyClips) == 0 {
			return result, nil
		}
		return applySoftDeletes(db, absRoot, id, missingFiles, emptyClips)
	}

	fp := candidateFingerprint(missingFiles, emptyClips)
	pending, observedAt, err := loadPendingPurge(stateDir, absRoot)
	if err != nil {
		if !errors.Is(err, ErrReconcilePendingInvalid) {
			return result, err
		}
		// Corrupt / tampered confirmation cannot authorize; start a fresh pending.
		_ = invalidatePendingPurge(stateDir, absRoot)
		forgetPendingObservation(absRoot, "")
		pending = nil
		observedAt = time.Time{}
		err = nil
	}

	now := time.Now()
	if pending == nil || pending.Fingerprint != fp || !pendingIDsMatch(pending, missingFiles, emptyClips) {
		forgetPendingObservation(absRoot, "")
		if err := savePendingPurge(stateDir, &pendingPurgeState{
			AbsRoot:     absRoot,
			Fingerprint: fp,
			FirstSeen:   now.UTC(),
			FileIDs:     videoFileIDs(missingFiles),
			ClipIDs:     clipIDs(emptyClips),
		}); err != nil {
			return result, err
		}
		result.PendingConfirm = true
		return result, fmt.Errorf("%w (%s; %d file(s), %d clip(s); wait %s)",
			ErrReconcilePendingConfirm, absRoot, len(missingFiles), len(emptyClips), reconcileConfirmMinInterval)
	}

	if pendingClearBlocked[absRoot] {
		return result, fmt.Errorf("%w (%s; prior confirmation clear failed)", ErrReconcilePendingInvalid, absRoot)
	}

	elapsed, err := confirmElapsed(pending, observedAt, now)
	if err != nil {
		forgetPendingObservation(absRoot, pending.Fingerprint)
		_ = invalidatePendingPurge(stateDir, absRoot)
		if saveErr := savePendingPurge(stateDir, &pendingPurgeState{
			AbsRoot:     absRoot,
			Fingerprint: fp,
			FirstSeen:   now.UTC(),
			FileIDs:     videoFileIDs(missingFiles),
			ClipIDs:     clipIDs(emptyClips),
		}); saveErr != nil {
			return result, saveErr
		}
		result.PendingConfirm = true
		return result, fmt.Errorf("%w (%s; reset after invalid timing: %v)", ErrReconcilePendingConfirm, absRoot, err)
	}
	if elapsed < reconcileConfirmMinInterval {
		result.PendingConfirm = true
		remain := reconcileConfirmMinInterval - elapsed
		return result, fmt.Errorf("%w (%s; %s remaining)", ErrReconcilePendingConfirm, absRoot, remain.Round(time.Millisecond))
	}

	missingFiles, emptyClips, err = recheckMissingCandidates(db, absRoot, missingFiles, emptyClips)
	if err != nil {
		return result, err
	}
	if len(missingFiles) == 0 && len(emptyClips) == 0 {
		if err := clearPendingPurge(stateDir, absRoot); err != nil {
			return result, err
		}
		return result, nil
	}
	if candidateFingerprint(missingFiles, emptyClips) != fp || !pendingIDsMatch(pending, missingFiles, emptyClips) {
		forgetPendingObservation(absRoot, fp)
		if err := savePendingPurge(stateDir, &pendingPurgeState{
			AbsRoot:     absRoot,
			Fingerprint: candidateFingerprint(missingFiles, emptyClips),
			FirstSeen:   now.UTC(),
			FileIDs:     videoFileIDs(missingFiles),
			ClipIDs:     clipIDs(emptyClips),
		}); err != nil {
			return result, err
		}
		result.PendingConfirm = true
		return result, fmt.Errorf("%w (%s; candidates changed before delete)", ErrReconcilePendingConfirm, absRoot)
	}

	if err := clearPendingPurge(stateDir, absRoot); err != nil {
		return result, err
	}
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
	pending, observedAt, err := loadPendingPurge(resolveReconcileStateDir(stateDir), absRoot)
	if err != nil || pending == nil {
		return 0
	}
	elapsed, err := confirmElapsed(pending, observedAt, time.Now())
	if err != nil {
		return reconcileConfirmMinInterval
	}
	remain := reconcileConfirmMinInterval - elapsed
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

// recheckMissingCandidates drops paths that reappeared after collection and
// recomputes empty clips so a mid-pass restore cannot be soft-deleted.
func recheckMissingCandidates(db *gorm.DB, absRoot string, missingFiles []models.VideoFile, emptyClips []models.Clip) ([]models.VideoFile, []models.Clip, error) {
	stillMissing := make([]models.VideoFile, 0, len(missingFiles))
	missingByClip := map[uint]int{}
	for _, vf := range missingFiles {
		absPath, err := filepath.Abs(vf.FilePath)
		if err != nil {
			continue
		}
		absPath = filepath.Clean(absPath)
		if _, err := os.Stat(absPath); err == nil {
			continue
		} else if err != nil && !os.IsNotExist(err) {
			log.Printf("ReconcileMissing: recheck skip file %s: %v", absPath, err)
			continue
		}
		stillMissing = append(stillMissing, vf)
		missingByClip[vf.ClipID]++
	}

	clipsWithFiles := map[uint]struct{}{}
	var allFiles []models.VideoFile
	if err := db.Find(&allFiles).Error; err != nil {
		return nil, nil, err
	}
	for i := range allFiles {
		if fileUnderRoot(allFiles[i].FilePath, absRoot) {
			clipsWithFiles[allFiles[i].ClipID] = struct{}{}
		}
	}

	stillEmpty := make([]models.Clip, 0, len(emptyClips))
	for _, clip := range emptyClips {
		if !clipScopedToRoot(&clip, absRoot, clipsWithFiles) {
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
		stillEmpty = append(stillEmpty, clip)
	}
	return stillMissing, stillEmpty, nil
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
			absPath, err := filepath.Abs(vf.FilePath)
			if err == nil {
				absPath = filepath.Clean(absPath)
				if _, statErr := os.Stat(absPath); statErr == nil {
					// Restored between batches — leave the live row alone.
					continue
				}
			}
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

func pendingObservationKey(absRoot, fingerprint string) string {
	return absRoot + "\x00" + fingerprint
}

func rememberPendingObservation(absRoot, fingerprint string, at time.Time) {
	pendingObservedAt[pendingObservationKey(absRoot, fingerprint)] = at
}

func forgetPendingObservation(absRoot, fingerprint string) {
	if fingerprint != "" {
		delete(pendingObservedAt, pendingObservationKey(absRoot, fingerprint))
		return
	}
	prefix := absRoot + "\x00"
	for k := range pendingObservedAt {
		if strings.HasPrefix(k, prefix) {
			delete(pendingObservedAt, k)
		}
	}
}

func loadPendingPurge(stateDir, absRoot string) (*pendingPurgeState, time.Time, error) {
	reconcilePendingMu.Lock()
	defer reconcilePendingMu.Unlock()

	path := pendingPurgePath(stateDir)
	data, modTime, err := readPendingPurgeBytes(stateDir, path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, time.Time{}, nil
		}
		return nil, time.Time{}, err
	}
	var st pendingPurgeState
	if err := json.Unmarshal(data, &st); err != nil {
		return nil, time.Time{}, err
	}
	if st.AbsRoot != absRoot {
		return nil, time.Time{}, nil
	}
	if st.Canceled || st.Fingerprint == "" {
		return nil, time.Time{}, nil
	}
	if err := validatePendingPurge(&st, time.Now()); err != nil {
		return nil, time.Time{}, fmt.Errorf("%w: %v", ErrReconcilePendingInvalid, err)
	}
	observed := pendingObservedAt[pendingObservationKey(absRoot, st.Fingerprint)]
	if observed.IsZero() {
		// Restart path: trust the pending file's own mtime (Lstat), not JSON alone.
		observed = modTime
	}
	return &st, observed, nil
}

func validatePendingPurge(st *pendingPurgeState, now time.Time) error {
	if st == nil {
		return fmt.Errorf("nil state")
	}
	if st.FirstSeen.IsZero() {
		return fmt.Errorf("missing first_seen")
	}
	// Reject future timestamps (clock step / tamper). Small skew for filesystem jitter.
	if st.FirstSeen.After(now.UTC().Add(2 * time.Second)) {
		return fmt.Errorf("future first_seen")
	}
	if st.Fingerprint == "" {
		return fmt.Errorf("empty fingerprint")
	}
	if err := assertFingerprintMatchesIDs(st); err != nil {
		return err
	}
	return nil
}

func assertFingerprintMatchesIDs(st *pendingPurgeState) error {
	ids := make([]string, 0, len(st.FileIDs)+len(st.ClipIDs))
	for _, id := range st.FileIDs {
		ids = append(ids, fmt.Sprintf("f:%d", id))
	}
	for _, id := range st.ClipIDs {
		ids = append(ids, fmt.Sprintf("c:%d", id))
	}
	sort.Strings(ids)
	sum := sha256.Sum256([]byte(strings.Join(ids, ",")))
	got := hex.EncodeToString(sum[:])
	if got != st.Fingerprint {
		return fmt.Errorf("fingerprint does not match file_ids/clip_ids")
	}
	return nil
}

func pendingIDsMatch(st *pendingPurgeState, files []models.VideoFile, clips []models.Clip) bool {
	if st == nil {
		return false
	}
	wantFiles := videoFileIDs(files)
	wantClips := clipIDs(clips)
	if len(st.FileIDs) != len(wantFiles) || len(st.ClipIDs) != len(wantClips) {
		return false
	}
	sf := append([]uint(nil), st.FileIDs...)
	sc := append([]uint(nil), st.ClipIDs...)
	sort.Slice(sf, func(i, j int) bool { return sf[i] < sf[j] })
	sort.Slice(sc, func(i, j int) bool { return sc[i] < sc[j] })
	wf := append([]uint(nil), wantFiles...)
	wc := append([]uint(nil), wantClips...)
	sort.Slice(wf, func(i, j int) bool { return wf[i] < wf[j] })
	sort.Slice(wc, func(i, j int) bool { return wc[i] < wc[j] })
	for i := range sf {
		if sf[i] != wf[i] {
			return false
		}
	}
	for i := range sc {
		if sc[i] != wc[i] {
			return false
		}
	}
	return true
}

// confirmElapsed returns how long the pending confirmation has been observed.
// Prefer process-local monotonic observation; on restart fall back to pending
// file mtime. JSON FirstSeen alone never authorizes (tamper / clock step).
func confirmElapsed(st *pendingPurgeState, observedAt time.Time, now time.Time) (time.Duration, error) {
	if st == nil {
		return 0, fmt.Errorf("nil pending")
	}
	if err := validatePendingPurge(st, now); err != nil {
		return 0, err
	}
	if observedAt.IsZero() {
		return 0, fmt.Errorf("missing observation time")
	}
	if observedAt.After(now.Add(2 * time.Second)) {
		return 0, fmt.Errorf("observation in the future")
	}
	elapsed := now.Sub(observedAt)
	if elapsed < 0 {
		elapsed = 0
	}
	return elapsed, nil
}

func savePendingPurge(stateDir string, st *pendingPurgeState) error {
	reconcilePendingMu.Lock()
	defer reconcilePendingMu.Unlock()

	if st == nil {
		return fmt.Errorf("nil pending state")
	}
	if err := validatePendingPurge(st, time.Now()); err != nil && !st.Canceled {
		return err
	}
	if err := os.MkdirAll(stateDir, 0755); err != nil {
		return err
	}
	data, err := json.MarshalIndent(st, "", "  ")
	if err != nil {
		return err
	}
	path := pendingPurgePath(stateDir)
	if err := writePendingPurgeBytes(stateDir, path, data); err != nil {
		return err
	}
	if !st.Canceled && st.Fingerprint != "" {
		rememberPendingObservation(st.AbsRoot, st.Fingerprint, time.Now())
	}
	delete(pendingClearBlocked, st.AbsRoot)
	return nil
}

func clearPendingPurge(stateDir, absRoot string) error {
	reconcilePendingMu.Lock()
	defer reconcilePendingMu.Unlock()

	forgetPendingObservation(absRoot, "")
	path := pendingPurgePath(stateDir)

	data, _, err := readPendingPurgeBytes(stateDir, path)
	if err != nil {
		if os.IsNotExist(err) {
			delete(pendingClearBlocked, absRoot)
			return nil
		}
		// Symlink / unreadable: still try to invalidate via direct unlink of the name.
		if invErr := invalidatePendingPurgeLocked(stateDir, absRoot); invErr != nil {
			pendingClearBlocked[absRoot] = true
			return fmt.Errorf("%w (clear read: %v; invalidate: %v)", ErrReconcilePendingInvalid, err, invErr)
		}
		if rmErr := removePendingPurgeNoFollow(stateDir, path); rmErr != nil && !os.IsNotExist(rmErr) {
			pendingClearBlocked[absRoot] = true
			return fmt.Errorf("%w (remove after invalidate: %v)", ErrReconcilePendingInvalid, rmErr)
		}
		delete(pendingClearBlocked, absRoot)
		return nil
	}

	var st pendingPurgeState
	if json.Unmarshal(data, &st) != nil {
		if err := removePendingPurgeNoFollow(stateDir, path); err != nil && !os.IsNotExist(err) {
			pendingClearBlocked[absRoot] = true
			return fmt.Errorf("%w (remove corrupt: %v)", ErrReconcilePendingInvalid, err)
		}
		delete(pendingClearBlocked, absRoot)
		return nil
	}
	if st.AbsRoot != absRoot && st.AbsRoot != "" {
		return nil
	}

	if err := invalidatePendingPurgeLocked(stateDir, absRoot); err != nil {
		pendingClearBlocked[absRoot] = true
		return fmt.Errorf("%w (invalidate: %v)", ErrReconcilePendingInvalid, err)
	}
	if err := removePendingPurgeNoFollow(stateDir, path); err != nil && !os.IsNotExist(err) {
		// Invalidated on disk; removal failed (e.g. dir mode). Block reuse until gone.
		pendingClearBlocked[absRoot] = true
		return fmt.Errorf("%w (remove canceled confirm: %v)", ErrReconcilePendingInvalid, err)
	}
	delete(pendingClearBlocked, absRoot)
	return nil
}

func invalidatePendingPurge(stateDir, absRoot string) error {
	reconcilePendingMu.Lock()
	defer reconcilePendingMu.Unlock()
	return invalidatePendingPurgeLocked(stateDir, absRoot)
}

func invalidatePendingPurgeLocked(stateDir, absRoot string) error {
	tomb := &pendingPurgeState{
		AbsRoot:     absRoot,
		Fingerprint: "",
		FirstSeen:   time.Now().UTC(),
		Canceled:    true,
	}
	data, err := json.MarshalIndent(tomb, "", "  ")
	if err != nil {
		return err
	}
	if err := os.MkdirAll(stateDir, 0755); err != nil {
		return err
	}
	return writePendingPurgeBytes(stateDir, pendingPurgePath(stateDir), data)
}

// readPendingPurgeBytes opens the pending file with O_NOFOLLOW so a symlink
// cannot redirect reads to an attacker-controlled payload.
func readPendingPurgeBytes(stateDir, path string) ([]byte, time.Time, error) {
	dirFD, err := openStateDirNoFollow(stateDir)
	if err != nil {
		return nil, time.Time{}, err
	}
	defer unix.Close(dirFD)

	fi, err := os.Lstat(path)
	if err != nil {
		return nil, time.Time{}, err
	}
	if fi.Mode()&os.ModeSymlink != 0 {
		return nil, time.Time{}, fmt.Errorf("%w (pending file is a symlink)", ErrReconcilePendingInvalid)
	}

	fd, err := unix.Openat(dirFD, pendingPurgeFileName, unix.O_RDONLY|unix.O_NOFOLLOW|unix.O_CLOEXEC, 0)
	if err != nil {
		if err == unix.ELOOP || err == syscall.ELOOP {
			return nil, time.Time{}, fmt.Errorf("%w (pending file symlink)", ErrReconcilePendingInvalid)
		}
		return nil, time.Time{}, err
	}

	var st unix.Stat_t
	if err := unix.Fstat(fd, &st); err != nil {
		_ = unix.Close(fd)
		return nil, time.Time{}, err
	}
	// O_NOFOLLOW already refused a symlink at open; fstat the opened inode.
	f := os.NewFile(uintptr(fd), path)
	data, err := io.ReadAll(f)
	_ = f.Close()
	if err != nil {
		return nil, time.Time{}, err
	}
	modTime := time.Unix(st.Mtim.Sec, st.Mtim.Nsec)
	return data, modTime, nil
}

// writePendingPurgeBytes creates a private temp file with O_NOFOLLOW|O_EXCL
// under the state directory fd, then renames into place. A pre-existing temp
// symlink cannot be followed/overwritten.
func writePendingPurgeBytes(stateDir, path string, data []byte) error {
	dirFD, err := openStateDirNoFollow(stateDir)
	if err != nil {
		return err
	}
	defer unix.Close(dirFD)

	tmpName := pendingPurgeFileName + ".tmp"
	// Remove a leftover regular temp file, but refuse if it is a symlink.
	if fi, err := os.Lstat(filepath.Join(stateDir, tmpName)); err == nil {
		if fi.Mode()&os.ModeSymlink != 0 {
			return fmt.Errorf("%w (pending temp is a symlink)", ErrReconcilePendingInvalid)
		}
		_ = unix.Unlinkat(dirFD, tmpName, 0)
	} else if !os.IsNotExist(err) {
		return err
	}

	fd, err := unix.Openat(dirFD, tmpName, unix.O_WRONLY|unix.O_CREAT|unix.O_EXCL|unix.O_NOFOLLOW|unix.O_CLOEXEC, 0644)
	if err != nil {
		if err == unix.ELOOP || err == syscall.ELOOP {
			return fmt.Errorf("%w (pending temp symlink)", ErrReconcilePendingInvalid)
		}
		return err
	}
	f := os.NewFile(uintptr(fd), filepath.Join(stateDir, tmpName))
	_, writeErr := f.Write(data)
	syncErr := f.Sync()
	closeErr := f.Close()
	if writeErr != nil {
		_ = unix.Unlinkat(dirFD, tmpName, 0)
		return writeErr
	}
	if syncErr != nil {
		_ = unix.Unlinkat(dirFD, tmpName, 0)
		return syncErr
	}
	if closeErr != nil {
		_ = unix.Unlinkat(dirFD, tmpName, 0)
		return closeErr
	}
	if err := unix.Renameat(dirFD, tmpName, dirFD, pendingPurgeFileName); err != nil {
		_ = unix.Unlinkat(dirFD, tmpName, 0)
		return err
	}
	return nil
}

func removePendingPurgeNoFollow(stateDir, path string) error {
	dirFD, err := openStateDirNoFollow(stateDir)
	if err != nil {
		// Fall back to unlink of the path name; Lstat first.
		if fi, lerr := os.Lstat(path); lerr == nil && fi.Mode()&os.ModeSymlink != 0 {
			return os.Remove(path) // remove the symlink itself
		}
		return os.Remove(path)
	}
	defer unix.Close(dirFD)
	if err := unix.Unlinkat(dirFD, pendingPurgeFileName, 0); err != nil {
		if err == unix.ENOENT || err == syscall.ENOENT {
			return os.ErrNotExist
		}
		return err
	}
	return nil
}

func openStateDirNoFollow(stateDir string) (int, error) {
	abs, err := filepath.Abs(stateDir)
	if err != nil {
		return -1, err
	}
	// Walk each component with O_NOFOLLOW so an ancestor symlink is refused.
	cleaned := filepath.Clean(abs)
	if cleaned == string(os.PathSeparator) {
		fd, err := unix.Open(cleaned, unix.O_RDONLY|unix.O_DIRECTORY|unix.O_NOFOLLOW|unix.O_CLOEXEC, 0)
		return fd, err
	}
	parts := strings.Split(cleaned, string(os.PathSeparator))
	fd, err := unix.Open("/", unix.O_RDONLY|unix.O_DIRECTORY|unix.O_CLOEXEC, 0)
	if err != nil {
		return -1, err
	}
	for _, part := range parts {
		if part == "" {
			continue
		}
		next, err := unix.Openat(fd, part, unix.O_RDONLY|unix.O_DIRECTORY|unix.O_NOFOLLOW|unix.O_CLOEXEC, 0)
		unix.Close(fd)
		if err != nil {
			if err == unix.ELOOP || err == syscall.ELOOP {
				return -1, fmt.Errorf("%w (state dir ancestor is a symlink)", ErrReconcilePendingInvalid)
			}
			return -1, err
		}
		fd = next
	}
	return fd, nil
}

// agePendingPurgeForTest backdates observation so confirmation can proceed.
// Tests only. Updates file mtime and in-memory observation; JSON FirstSeen is
// not used for authorization.
func agePendingPurgeForTest(stateDir, root string, ago time.Duration) error {
	absRoot, err := filepath.Abs(root)
	if err != nil {
		return err
	}
	absRoot = filepath.Clean(absRoot)
	stateDir = resolveReconcileStateDir(stateDir)

	reconcilePendingMu.Lock()
	defer reconcilePendingMu.Unlock()

	path := pendingPurgePath(stateDir)
	data, _, err := readPendingPurgeBytes(stateDir, path)
	if err != nil {
		return err
	}
	var st pendingPurgeState
	if err := json.Unmarshal(data, &st); err != nil {
		return err
	}
	if st.AbsRoot != absRoot {
		return fmt.Errorf("no pending purge for %s", absRoot)
	}
	when := time.Now().Add(-ago)
	st.FirstSeen = when.UTC()
	payload, err := json.MarshalIndent(&st, "", "  ")
	if err != nil {
		return err
	}
	if err := writePendingPurgeBytes(stateDir, path, payload); err != nil {
		return err
	}
	if err := os.Chtimes(path, when, when); err != nil {
		return err
	}
	rememberPendingObservation(absRoot, st.Fingerprint, when)
	delete(pendingClearBlocked, absRoot)
	return nil
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
