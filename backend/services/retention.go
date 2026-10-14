package services

import (
	"encoding/json"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/jinzhu/gorm"
	"teslaxy/models"
)

// RetentionCandidate describes one logical clip selected for pruning.
type RetentionCandidate struct {
	ClipID    uint      `json:"clip_id"`
	Event     string    `json:"event"`
	SourceDir string    `json:"source_dir,omitempty"`
	Timestamp time.Time `json:"timestamp"`
	Reason    string    `json:"reason"` // "age", "size", or "age+size"
	Bytes     int64     `json:"bytes"`
	FilePaths []string  `json:"file_paths"`
	Protected bool      `json:"protected"`
}

// RetentionSkip records why a path or clip was not pruned.
type RetentionSkip struct {
	ClipID uint   `json:"clip_id,omitempty"`
	Path   string `json:"path,omitempty"`
	Reason string `json:"reason"`
}

// RetentionReport is structured output for a prune pass (including dry-run).
type RetentionReport struct {
	StartedAt        time.Time            `json:"started_at"`
	FinishedAt       time.Time            `json:"finished_at"`
	DryRun           bool                 `json:"dry_run"`
	Enabled          bool                 `json:"enabled"`
	FootageRoot      string               `json:"footage_root"`
	TotalBytesBefore int64                `json:"total_bytes_before"`
	TotalBytesAfter  int64                `json:"total_bytes_after"`
	BytesFreed       int64                `json:"bytes_freed"`
	Candidates       []RetentionCandidate `json:"candidates"`
	Deleted          []RetentionCandidate `json:"deleted"`
	Skipped          []RetentionSkip      `json:"skipped"`
	Errors           []string             `json:"errors"`
}

type fileRef struct {
	vfID     uint
	path     string
	size     int64
	dev      uint64
	ino      uint64
	fresh    bool // recent mtime: counts toward size, not deletable
	identity bool
}

type clipEval struct {
	clip      models.Clip
	files     []fileRef
	deletable []fileRef
	bytes     int64 // unique-inode deletable bytes for this clip
	protected bool
	tombstone bool // soft-deleted pending recovery
}

// RetentionService prunes old / oversized footage under FOOTAGE_PATH.
type RetentionService struct {
	DB          *gorm.DB
	ConfigPath  string
	FootagePath string

	mu     sync.Mutex // config + serializes Run
	clipMu sync.Mutex // coordinates protect vs prune
	cfg    RetentionConfig
	stopCh chan struct{}
	nowFn  func() time.Time
	// Optional test hooks; nil uses footageRoot methods.
	resolveFn func(root *footageRoot, path string) (string, os.FileInfo, error)
	unlinkFn  func(root *footageRoot, path string) (freed int64, removed bool, err error)
	// beforeHardDeleteTelemetry, when set, runs before Unscoped telemetry delete.
	// A non-nil error leaves the clip soft-deleted so recovery can retry.
	beforeHardDeleteTelemetry func(clipID uint) error
	// beforeOrphanTelemetryBatch, when set, runs before each orphan delete batch.
	// A non-nil error stops the sweep; earlier batches remain deleted.
	beforeOrphanTelemetryBatch func(ids []uint) error
}

func NewRetentionService(db *gorm.DB, footagePath, configPath string) *RetentionService {
	cfg, err := LoadRetentionConfig(configPath)
	if err != nil {
		log.Printf("retention: using defaults after config load error: %v", err)
		cfg = DefaultRetentionConfig()
	}
	if strings.TrimSpace(cfg.FootagePath) == "" {
		cfg.FootagePath = footagePath
	}
	return &RetentionService{
		DB:          db,
		ConfigPath:  configPath,
		FootagePath: cfg.FootagePath,
		cfg:         cfg,
		stopCh:      make(chan struct{}),
		nowFn:       time.Now,
	}
}

func (s *RetentionService) Config() RetentionConfig {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.cfg
}

func (s *RetentionService) SetConfig(cfg RetentionConfig) error {
	normalizeRetentionConfig(&cfg)
	if err := ValidateRetentionConfig(cfg); err != nil {
		return err
	}
	if strings.TrimSpace(cfg.FootagePath) == "" {
		cfg.FootagePath = s.FootagePath
	}
	if err := SaveRetentionConfig(s.ConfigPath, cfg); err != nil {
		return err
	}
	s.mu.Lock()
	s.cfg = cfg
	s.FootagePath = cfg.FootagePath
	s.mu.Unlock()
	return nil
}

func (s *RetentionService) ReloadConfig() error {
	cfg, err := LoadRetentionConfig(s.ConfigPath)
	if err != nil {
		return err
	}
	if strings.TrimSpace(cfg.FootagePath) == "" {
		cfg.FootagePath = s.FootagePath
	}
	s.mu.Lock()
	s.cfg = cfg
	s.FootagePath = cfg.FootagePath
	s.mu.Unlock()
	return nil
}

// WithClipLock runs fn while holding the protect/prune coordination lock.
func (s *RetentionService) WithClipLock(fn func()) {
	s.clipMu.Lock()
	defer s.clipMu.Unlock()
	fn()
}

// IsClipProtected returns true for user-flagged clips and Tesla SavedClips.
func IsClipProtected(clip models.Clip) bool {
	if clip.Protected {
		return true
	}
	return strings.EqualFold(clip.Event, "Saved")
}

// Run executes one retention pass using the current config.
func (s *RetentionService) Run() (*RetentionReport, error) {
	s.mu.Lock()
	cfg := s.cfg
	s.mu.Unlock()
	return s.RunWithConfig(cfg)
}

// RunWithConfig executes one pass with an explicit config (does not persist it).
func (s *RetentionService) RunWithConfig(cfg RetentionConfig) (*RetentionReport, error) {
	normalizeRetentionConfig(&cfg)
	s.mu.Lock()
	defer s.mu.Unlock()

	footagePath := cfg.FootagePath
	if strings.TrimSpace(footagePath) == "" {
		footagePath = s.FootagePath
	}

	report := &RetentionReport{
		StartedAt:  s.nowFn().UTC(),
		DryRun:     cfg.DryRun,
		Enabled:    cfg.Enabled,
		Candidates: []RetentionCandidate{},
		Deleted:    []RetentionCandidate{},
		Skipped:    []RetentionSkip{},
		Errors:     []string{},
	}

	if !cfg.Enabled && !cfg.DryRun {
		report.FinishedAt = s.nowFn().UTC()
		logRetentionReport(report)
		return report, nil
	}

	// Lexical canonicalize for reporting, then pin a no-follow directory fd for
	// the whole pass so a mid-prune path swap cannot redirect unlinks.
	lexRoot, err := resolveFootageRoot(footagePath)
	if err != nil {
		report.Errors = append(report.Errors, fmt.Sprintf("resolve footage root: %v", err))
		report.FinishedAt = s.nowFn().UTC()
		logRetentionReport(report)
		return report, err
	}
	fr, err := openFootageRoot(lexRoot)
	if err != nil {
		report.Errors = append(report.Errors, fmt.Sprintf("open footage root fd: %v", err))
		report.FinishedAt = s.nowFn().UTC()
		logRetentionReport(report)
		return report, err
	}
	defer fr.Close()
	report.FootageRoot = fr.Path()

	doDelete := cfg.Enabled && !cfg.DryRun
	if doDelete {
		if err := s.completePendingDeletes(fr, cfg, report); err != nil {
			report.Errors = append(report.Errors, fmt.Sprintf("pending recovery: %v", err))
		}
	}

	evals, totalBytes, skips := s.evaluateLiveClips(fr, cfg)
	report.Skipped = append(report.Skipped, skips...)
	report.TotalBytesBefore = totalBytes

	selected := selectRetentionCandidates(evals, cfg, s.nowFn(), totalBytes)
	report.Candidates = selected

	var bytesFreed int64
	for _, cand := range selected {
		if !doDelete {
			bytesFreed += cand.Bytes
			continue
		}
		freed, err := s.pruneClip(fr, cand, cfg)
		if err != nil {
			report.Errors = append(report.Errors, fmt.Sprintf("clip %d: %v", cand.ClipID, err))
			bytesFreed += freed
			continue
		}
		bytesFreed += freed
		report.Deleted = append(report.Deleted, cand)
	}

	report.BytesFreed = bytesFreed
	if doDelete {
		report.TotalBytesAfter = totalBytes - bytesFreed
		if report.TotalBytesAfter < 0 {
			report.TotalBytesAfter = 0
		}
	} else {
		report.TotalBytesAfter = totalBytes
		report.Deleted = []RetentionCandidate{}
	}

	report.FinishedAt = s.nowFn().UTC()
	logRetentionReport(report)
	return report, nil
}

func (s *RetentionService) evaluateLiveClips(root *footageRoot, cfg RetentionConfig) ([]clipEval, int64, []RetentionSkip) {
	var clips []models.Clip
	if err := s.DB.Preload("VideoFiles").Find(&clips).Error; err != nil {
		return nil, 0, []RetentionSkip{{Reason: "load_clips_failed"}}
	}

	minAge := cfg.MinFileAge()
	now := s.nowFn()
	evals := make([]clipEval, 0, len(clips))
	skips := []RetentionSkip{}
	seenInodes := map[string]bool{}
	var totalBytes int64

	for _, clip := range clips {
		ev := clipEval{clip: clip, protected: IsClipProtected(clip)}
		seenPaths := map[string]bool{}
		for _, vf := range clip.VideoFiles {
			abs, info, reason, ok := s.classifyPath(root, vf.FilePath, minAge, now)
			if !ok {
				if reason != "" {
					skips = append(skips, RetentionSkip{ClipID: clip.ID, Path: vf.FilePath, Reason: reason})
				}
				continue
			}
			if seenPaths[abs] {
				continue
			}
			seenPaths[abs] = true

			ref := fileRef{vfID: vf.ID, path: abs, size: info.Size()}
			if dev, ino, idOK := fileIdentity(info); idOK {
				ref.dev, ref.ino, ref.identity = dev, ino, true
				key := fmt.Sprintf("%d:%d", dev, ino)
				if !seenInodes[key] {
					seenInodes[key] = true
					totalBytes += ref.size
				}
			} else {
				totalBytes += ref.size
			}
			if reason == "recent_mtime" {
				ref.fresh = true
				skips = append(skips, RetentionSkip{ClipID: clip.ID, Path: abs, Reason: "recent_mtime"})
			}
			ev.files = append(ev.files, ref)
			if !ref.fresh && !ev.protected {
				ev.deletable = append(ev.deletable, ref)
			}
		}
		ev.bytes = uniqueInodeBytes(ev.deletable)
		evals = append(evals, ev)
	}
	return evals, totalBytes, skips
}

func uniqueInodeBytes(files []fileRef) int64 {
	seen := map[string]bool{}
	var sum int64
	for _, f := range files {
		if f.identity {
			key := fmt.Sprintf("%d:%d", f.dev, f.ino)
			if seen[key] {
				continue
			}
			seen[key] = true
		} else {
			if seen[f.path] {
				continue
			}
			seen[f.path] = true
		}
		sum += f.size
	}
	return sum
}

func inodeKey(f fileRef) string {
	if f.identity {
		return fmt.Sprintf("%d:%d", f.dev, f.ino)
	}
	return "path:" + f.path
}

// inodeLinkIndex counts every known DB path per inode (including protected/fresh).
// Space is only estimated as freed when every known link is selected for deletion.
type inodeLinkIndex struct {
	size   int64
	total  int
	byClip map[uint]int // deletable links belonging to clip
}

func buildInodeLinkIndex(evals []clipEval) map[string]*inodeLinkIndex {
	out := map[string]*inodeLinkIndex{}
	for _, ev := range evals {
		for _, f := range ev.files {
			key := inodeKey(f)
			meta, ok := out[key]
			if !ok {
				meta = &inodeLinkIndex{size: f.size, byClip: map[uint]int{}}
				out[key] = meta
			}
			meta.total++
			if !f.fresh && !ev.protected {
				meta.byClip[ev.clip.ID]++
			}
		}
	}
	return out
}

// estimateFreedBytes returns unique-inode bytes that would actually be freed if
// every deletable path on selectedClipIDs is unlinked (0 for inodes that still
// have a non-selected or non-deletable link).
func estimateFreedBytes(index map[string]*inodeLinkIndex, selectedClipIDs map[uint]bool) int64 {
	var freed int64
	for _, meta := range index {
		removed := 0
		for clipID, n := range meta.byClip {
			if selectedClipIDs[clipID] {
				removed += n
			}
		}
		if removed >= meta.total && meta.total > 0 {
			freed += meta.size
		}
	}
	return freed
}

// clipInodeContrib is one clip's deletable link count for a single inode.
type clipInodeContrib struct {
	key string
	n   int
}

// freedLedger counts an inode's bytes once, when the last known DB link is
// covered by the selected set. Each clip is applied in O(its files).
type freedLedger struct {
	index     map[string]*inodeLinkIndex
	remaining map[string]int
	contribs  map[uint][]clipInodeContrib
	freed     int64
}

func newFreedLedger(evals []clipEval) *freedLedger {
	index := buildInodeLinkIndex(evals)
	led := &freedLedger{
		index:     index,
		remaining: make(map[string]int, len(index)),
		contribs:  make(map[uint][]clipInodeContrib, len(evals)),
	}
	for key, meta := range index {
		led.remaining[key] = meta.total
		for clipID, n := range meta.byClip {
			if n <= 0 {
				continue
			}
			led.contribs[clipID] = append(led.contribs[clipID], clipInodeContrib{key: key, n: n})
		}
	}
	return led
}

// add marks clipID selected and returns the unique bytes that become free
// because this clip covered the inode's last remaining link.
func (l *freedLedger) add(clipID uint) int64 {
	var incr int64
	for _, c := range l.contribs[clipID] {
		prev := l.remaining[c.key]
		left := prev - c.n
		l.remaining[c.key] = left
		if prev > 0 && left <= 0 {
			incr += l.index[c.key].size
		}
	}
	l.freed += incr
	return incr
}

func (s *RetentionService) classifyPath(root *footageRoot, filePath string, minAge time.Duration, now time.Time) (abs string, info os.FileInfo, reason string, ok bool) {
	var err error
	if s.resolveFn != nil {
		abs, info, err = s.resolveFn(root, filePath)
	} else {
		abs, info, err = root.resolve(filePath)
	}
	if err != nil {
		msg := err.Error()
		switch {
		case strings.Contains(msg, "outside_root"):
			return "", nil, "outside_root", false
		case strings.Contains(msg, "symlink"):
			return "", nil, "symlink", false
		case strings.Contains(msg, "missing"):
			return "", nil, "missing", false
		case strings.Contains(msg, "is_directory"):
			return "", nil, "is_directory", false
		case strings.Contains(msg, "empty_path"):
			return "", nil, "empty_path", false
		default:
			return "", nil, "resolve_failed", false
		}
	}
	if minAge > 0 && now.Sub(info.ModTime()) < minAge {
		return abs, info, "recent_mtime", true // still counts toward size
	}
	return abs, info, "", true
}

func selectRetentionCandidates(evals []clipEval, cfg RetentionConfig, now time.Time, totalBytes int64) []RetentionCandidate {
	maxAgeCutoff := time.Time{}
	if cfg.MaxAgeDays > 0 {
		maxAgeCutoff = now.AddDate(0, 0, -cfg.MaxAgeDays)
	}
	maxBytes := cfg.MaxSizeBytes()

	sort.SliceStable(evals, func(i, j int) bool {
		ti, tj := evals[i].clip.Timestamp, evals[j].clip.Timestamp
		if ti.Equal(tj) {
			return evals[i].clip.ID < evals[j].clip.ID
		}
		return ti.Before(tj)
	})

	// Precomputed hardlink groups. Applying a clip updates only its inodes, so
	// choosing thousands of candidates stays near-linear (no full rescan).
	ledger := newFreedLedger(evals)
	ageIDs := map[uint]bool{}
	sizeIDs := map[uint]bool{}
	selectedOrder := make([]clipEval, 0)
	selectedSet := map[uint]bool{}
	incrByID := map[uint]int64{}

	selectable := func(ev clipEval) bool {
		return !ev.protected && len(ev.deletable) > 0
	}
	selectClip := func(ev clipEval) {
		selectedOrder = append(selectedOrder, ev)
		selectedSet[ev.clip.ID] = true
		incrByID[ev.clip.ID] = ledger.add(ev.clip.ID)
	}

	for _, ev := range evals {
		if !selectable(ev) {
			continue
		}
		if cfg.MaxAgeDays > 0 && ev.clip.Timestamp.Before(maxAgeCutoff) {
			ageIDs[ev.clip.ID] = true
			selectClip(ev)
		}
	}

	// Size cap uses hardlink-aware freed estimates: an inode only counts once
	// all known DB links are in the selected set. Keep selecting until the
	// projected freed bytes actually bring the unique total under the cap.
	if maxBytes > 0 {
		ageFreed := ledger.freed
		// Age-selected clips that were needed to relieve size pressure get age+size.
		if totalBytes > maxBytes && totalBytes-ageFreed > maxBytes {
			for id := range ageIDs {
				sizeIDs[id] = true
			}
		}
		for _, ev := range evals {
			if totalBytes-ledger.freed <= maxBytes {
				break
			}
			if !selectable(ev) || selectedSet[ev.clip.ID] {
				continue
			}
			sizeIDs[ev.clip.ID] = true
			selectClip(ev)
		}
	}

	// Per-candidate Bytes = incremental hardlink-aware freed when this clip was added.
	out := make([]RetentionCandidate, 0, len(selectedOrder))
	for _, ev := range selectedOrder {
		reason := "age"
		hasAge := ageIDs[ev.clip.ID]
		hasSize := sizeIDs[ev.clip.ID]
		switch {
		case hasAge && hasSize:
			reason = "age+size"
		case hasSize:
			reason = "size"
		case hasAge:
			reason = "age"
		}
		paths := make([]string, 0, len(ev.deletable))
		for _, f := range ev.deletable {
			paths = append(paths, f.path)
		}
		out = append(out, RetentionCandidate{
			ClipID:    ev.clip.ID,
			Event:     ev.clip.Event,
			SourceDir: ev.clip.SourceDir,
			Timestamp: ev.clip.Timestamp,
			Reason:    reason,
			Bytes:     incrByID[ev.clip.ID],
			FilePaths: paths,
		})
	}
	return out
}

func (s *RetentionService) pruneClip(root *footageRoot, cand RetentionCandidate, cfg RetentionConfig) (int64, error) {
	// media write lock blocks the scanner from recreating rows mid-prune.
	lockMediaWrite()
	defer unlockMediaWrite()
	s.clipMu.Lock()
	defer s.clipMu.Unlock()

	minAge := cfg.MinFileAge()
	now := s.nowFn()

	tx := s.DB.Begin()
	if tx.Error != nil {
		return 0, tx.Error
	}

	var clip models.Clip
	if err := tx.Preload("VideoFiles").Preload("Telemetry").First(&clip, cand.ClipID).Error; err != nil {
		tx.Rollback()
		return 0, err
	}
	if IsClipProtected(clip) {
		tx.Rollback()
		return 0, fmt.Errorf("refusing to prune protected clip")
	}

	// Soft-delete only the selected video files that still pass safety + freshness.
	selected := map[string]bool{}
	for _, p := range cand.FilePaths {
		selected[filepath.Clean(p)] = true
	}
	var toDelete []models.VideoFile
	for _, vf := range clip.VideoFiles {
		abs, _, reason, ok := s.classifyPath(root, vf.FilePath, minAge, now)
		if !ok || reason == "recent_mtime" {
			continue
		}
		if !selected[filepath.Clean(abs)] && !selected[filepath.Clean(vf.FilePath)] {
			continue
		}
		toDelete = append(toDelete, vf)
	}
	if len(toDelete) == 0 {
		tx.Rollback()
		return 0, fmt.Errorf("no deletable files remaining")
	}

	for i := range toDelete {
		if err := tx.Delete(&toDelete[i]).Error; err != nil {
			tx.Rollback()
			return 0, err
		}
	}

	// Soft-delete clip+telemetry only when no live video files remain.
	var liveVF int
	if err := tx.Model(&models.VideoFile{}).Where("clip_id = ?", clip.ID).Count(&liveVF).Error; err != nil {
		tx.Rollback()
		return 0, err
	}
	clipSoftDeleted := false
	if liveVF == 0 {
		if clip.Telemetry.ID != 0 {
			if err := tx.Delete(&clip.Telemetry).Error; err != nil {
				tx.Rollback()
				return 0, err
			}
		}
		if err := tx.Delete(&clip).Error; err != nil {
			tx.Rollback()
			return 0, err
		}
		clipSoftDeleted = true
	}
	if err := tx.Commit().Error; err != nil {
		return 0, err
	}

	var freed int64
	var fileErrs []string
	for _, vf := range toDelete {
		n, removed, err := s.unlinkSelected(root, vf, minAge, now)
		if err != nil {
			fileErrs = append(fileErrs, fmt.Sprintf("%s: %v", vf.FilePath, err))
			_ = s.DB.Unscoped().Model(&models.VideoFile{}).Where("id = ?", vf.ID).Update("deleted_at", nil).Error
			continue
		}
		if !removed {
			// Path still present (e.g. unlink refused); keep the DB row.
			_ = s.DB.Unscoped().Model(&models.VideoFile{}).Where("id = ?", vf.ID).Update("deleted_at", nil).Error
			fileErrs = append(fileErrs, fmt.Sprintf("%s: not_removed", vf.FilePath))
			continue
		}
		freed += n
		if err := s.DB.Unscoped().Delete(&models.VideoFile{}, vf.ID).Error; err != nil {
			fileErrs = append(fileErrs, fmt.Sprintf("hard-delete videofile %d: %v", vf.ID, err))
		}
	}

	if clipSoftDeleted {
		var stillLive int
		_ = s.DB.Unscoped().Model(&models.VideoFile{}).Where("clip_id = ? AND deleted_at IS NULL", clip.ID).Count(&stillLive)
		var stillPending int
		_ = s.DB.Unscoped().Model(&models.VideoFile{}).Where("clip_id = ? AND deleted_at IS NOT NULL", clip.ID).Count(&stillPending)
		if stillLive > 0 {
			_ = s.DB.Unscoped().Model(&models.Clip{}).Where("id = ?", clip.ID).Update("deleted_at", nil).Error
			_ = s.DB.Unscoped().Model(&models.Telemetry{}).Where("clip_id = ?", clip.ID).Update("deleted_at", nil).Error
		} else if stillPending == 0 {
			if err := s.hardDeleteClipOrphans(clip.ID); err != nil {
				fileErrs = append(fileErrs, err.Error())
			}
		}
	}

	if len(fileErrs) > 0 {
		return freed, fmt.Errorf("partial file delete: %s", strings.Join(fileErrs, "; "))
	}
	return freed, nil
}

// unlinkSelected unlinks one video path via the pinned root fd.
// freed is nonzero only when the last hard link was removed.
// removed is true when the directory entry is gone (unlink ok or already missing).
func (s *RetentionService) unlinkSelected(root *footageRoot, vf models.VideoFile, minAge time.Duration, now time.Time) (freed int64, removed bool, err error) {
	abs, info, reason, ok := s.classifyPath(root, vf.FilePath, minAge, now)
	if !ok {
		if reason == "missing" {
			return 0, true, nil
		}
		return 0, false, fmt.Errorf("%s", reason)
	}
	if reason == "recent_mtime" {
		return 0, false, fmt.Errorf("recent_mtime")
	}
	_ = info
	if s.unlinkFn != nil {
		return s.unlinkFn(root, abs)
	}
	return root.unlink(abs)
}

func (s *RetentionService) hardDeleteClipOrphans(clipID uint) error {
	// Telemetry must succeed before the clip row is removed; otherwise recovery
	// loses the soft-delete marker it needs to finish the orphan.
	if s.beforeHardDeleteTelemetry != nil {
		if err := s.beforeHardDeleteTelemetry(clipID); err != nil {
			return fmt.Errorf("hard-delete telemetry: %w", err)
		}
	}
	if err := s.DB.Unscoped().Where("clip_id = ?", clipID).Delete(&models.Telemetry{}).Error; err != nil {
		return fmt.Errorf("hard-delete telemetry: %w", err)
	}
	if err := s.DB.Unscoped().Delete(&models.Clip{}, clipID).Error; err != nil {
		return fmt.Errorf("hard-delete clip: %w", err)
	}
	return nil
}

// completePendingDeletes finishes soft-deleted video files and orphaned
// soft-deleted clip/telemetry rows left by a prior crash.
func (s *RetentionService) completePendingDeletes(root *footageRoot, cfg RetentionConfig, report *RetentionReport) error {
	if err := s.completePendingFileDeletes(root, cfg, report); err != nil {
		return err
	}
	// The sweep is its own short critical section. It must not run inside the
	// file-recovery lock, and it must not load telemetry payloads.
	s.recoverOrphanTelemetry(report)
	return nil
}

// completePendingFileDeletes holds the media and clip locks while finishing
// soft-deleted video files and clip rows. Orphan telemetry is handled after
// those locks are released.
func (s *RetentionService) completePendingFileDeletes(root *footageRoot, cfg RetentionConfig, report *RetentionReport) error {
	lockMediaWrite()
	defer unlockMediaWrite()
	s.clipMu.Lock()
	defer s.clipMu.Unlock()

	var pending []models.VideoFile
	if err := s.DB.Unscoped().Where("deleted_at IS NOT NULL").Find(&pending).Error; err != nil {
		return err
	}
	minAge := cfg.MinFileAge()
	now := s.nowFn()
	clipIDs := map[uint]bool{}

	for _, vf := range pending {
		clipIDs[vf.ClipID] = true
		var clip models.Clip
		if err := s.DB.Unscoped().First(&clip, vf.ClipID).Error; err == nil && IsClipProtected(clip) {
			_ = s.DB.Unscoped().Model(&vf).Update("deleted_at", nil).Error
			_ = s.DB.Unscoped().Model(&models.Clip{}).Where("id = ?", clip.ID).Update("deleted_at", nil).Error
			_ = s.DB.Unscoped().Model(&models.Telemetry{}).Where("clip_id = ?", clip.ID).Update("deleted_at", nil).Error
			continue
		}
		abs, _, reason, ok := s.classifyPath(root, vf.FilePath, minAge, now)
		if !ok {
			if reason == "missing" {
				if err := s.DB.Unscoped().Delete(&models.VideoFile{}, vf.ID).Error; err != nil {
					report.Errors = append(report.Errors, fmt.Sprintf("hard-delete missing vf %d: %v", vf.ID, err))
				}
				continue
			}
			report.Skipped = append(report.Skipped, RetentionSkip{ClipID: vf.ClipID, Path: vf.FilePath, Reason: reason})
			continue
		}
		if reason == "recent_mtime" {
			report.Skipped = append(report.Skipped, RetentionSkip{ClipID: vf.ClipID, Path: abs, Reason: "recent_mtime"})
			continue
		}
		freed, removed, err := s.unlinkSelected(root, vf, minAge, now)
		if err != nil {
			report.Errors = append(report.Errors, fmt.Sprintf("pending vf %d: %v", vf.ID, err))
			continue
		}
		if !removed {
			_ = s.DB.Unscoped().Model(&models.VideoFile{}).Where("id = ?", vf.ID).Update("deleted_at", nil).Error
			continue
		}
		report.BytesFreed += freed
		if err := s.DB.Unscoped().Delete(&models.VideoFile{}, vf.ID).Error; err != nil {
			report.Errors = append(report.Errors, fmt.Sprintf("hard-delete pending vf %d: %v", vf.ID, err))
		}
	}

	for id := range clipIDs {
		s.recoverOrphanClip(id, report)
	}
	// Soft-deleted clips with no remaining video rows (crash after last VF hard-delete).
	var orphanClips []models.Clip
	if err := s.DB.Unscoped().Where("deleted_at IS NOT NULL").Find(&orphanClips).Error; err != nil {
		return err
	}
	for _, c := range orphanClips {
		s.recoverOrphanClip(c.ID, report)
	}
	return nil
}

func (s *RetentionService) recoverOrphanClip(clipID uint, report *RetentionReport) {
	var live int
	_ = s.DB.Unscoped().Model(&models.VideoFile{}).Where("clip_id = ? AND deleted_at IS NULL", clipID).Count(&live)
	var pend int
	_ = s.DB.Unscoped().Model(&models.VideoFile{}).Where("clip_id = ? AND deleted_at IS NOT NULL", clipID).Count(&pend)
	if live != 0 || pend != 0 {
		return
	}
	if err := s.hardDeleteClipOrphans(clipID); err != nil {
		report.Errors = append(report.Errors, fmt.Sprintf("clip %d: %v", clipID, err))
	}
}

// orphanTelemetryDeleteBatchSize caps each DELETE ... IN (?) bind list.
// SQLite rejects statements with too many SQL variables (~32,767); a fixed
// batch stays well under that so a large orphan backlog still progresses.
const orphanTelemetryDeleteBatchSize = 500

// recoverOrphanTelemetry hard-deletes telemetry rows whose clip is already gone
// (stranded when an older build deleted the clip before telemetry succeeded).
//
// One set-based query reads primary keys only (not full_data_json) for rows
// whose clip id is absent. Clip existence is not checked one row at a time.
// That scan does not hold the media or clip locks. Deletes run in fixed-size
// batches under the locks, each re-checking that the clip is still gone.
// A failed batch leaves earlier batches committed and remaining ids for the
// next run (resumable across large backlogs).
func (s *RetentionService) recoverOrphanTelemetry(report *RetentionReport) {
	var ids []uint
	err := s.DB.Unscoped().
		Model(&models.Telemetry{}).
		Where("NOT EXISTS (SELECT 1 FROM clips WHERE clips.id = telemetries.clip_id)").
		Pluck("id", &ids).Error
	if err != nil {
		report.Errors = append(report.Errors, fmt.Sprintf("orphan telemetry scan: %v", err))
		return
	}
	if len(ids) == 0 {
		return
	}

	for start := 0; start < len(ids); start += orphanTelemetryDeleteBatchSize {
		end := start + orphanTelemetryDeleteBatchSize
		if end > len(ids) {
			end = len(ids)
		}
		if err := s.deleteOrphanTelemetryBatch(ids[start:end]); err != nil {
			report.Errors = append(report.Errors, fmt.Sprintf("orphan telemetry: %v", err))
			// Prior batches already committed; remaining ids stay for the next pass.
			return
		}
	}
}

// deleteOrphanTelemetryBatch hard-deletes one orphan id batch. The NOT EXISTS
// guard is the final check that each row's clip is still gone.
func (s *RetentionService) deleteOrphanTelemetryBatch(ids []uint) error {
	if len(ids) == 0 {
		return nil
	}
	if s.beforeOrphanTelemetryBatch != nil {
		if err := s.beforeOrphanTelemetryBatch(ids); err != nil {
			return err
		}
	}
	lockMediaWrite()
	defer unlockMediaWrite()
	s.clipMu.Lock()
	defer s.clipMu.Unlock()
	return s.DB.Unscoped().
		Where("id IN (?) AND NOT EXISTS (SELECT 1 FROM clips WHERE clips.id = telemetries.clip_id)", ids).
		Delete(&models.Telemetry{}).Error
}

func logRetentionReport(report *RetentionReport) {
	payload, err := json.Marshal(report)
	if err != nil {
		log.Printf("retention: report marshal error: %v", err)
		return
	}
	log.Printf("retention: report %s", string(payload))
}
