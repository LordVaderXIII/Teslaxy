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

// RetentionService prunes old / oversized footage under FOOTAGE_PATH.
type RetentionService struct {
	DB          *gorm.DB
	ConfigPath  string
	FootagePath string

	mu          sync.Mutex
	cfg         RetentionConfig
	stopCh      chan struct{}
	nowFn       func() time.Time
	lstatFn     func(string) (os.FileInfo, error)
	removeFn    func(string) error
	readDirFn   func(string) ([]os.DirEntry, error)
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
		lstatFn:     os.Lstat,
		removeFn:    os.Remove,
		readDirFn:   os.ReadDir,
	}
}

func (s *RetentionService) Config() RetentionConfig {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.cfg
}

func (s *RetentionService) SetConfig(cfg RetentionConfig) error {
	normalizeRetentionConfig(&cfg)
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

// IsClipProtected returns true for user-flagged clips and Tesla SavedClips.
func IsClipProtected(clip models.Clip) bool {
	if clip.Protected {
		return true
	}
	return strings.EqualFold(clip.Event, "Saved")
}

type clipEval struct {
	clip      models.Clip
	paths     []string
	bytes     int64
	protected bool
}

// Run executes one retention pass using the current config.
func (s *RetentionService) Run() (*RetentionReport, error) {
	s.mu.Lock()
	cfg := s.cfg
	s.mu.Unlock()
	return s.RunWithConfig(cfg)
}

// RunWithConfig executes one pass with an explicit config (does not persist it).
// Dry-run never deletes files or DB rows. When Enabled is false and DryRun is
// false, the report is empty. DryRun alone still evaluates candidates.
func (s *RetentionService) RunWithConfig(cfg RetentionConfig) (*RetentionReport, error) {
	normalizeRetentionConfig(&cfg)
	footagePath := cfg.FootagePath
	if strings.TrimSpace(footagePath) == "" {
		s.mu.Lock()
		footagePath = s.FootagePath
		s.mu.Unlock()
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

	root, err := resolveFootageRoot(footagePath)
	if err != nil {
		report.Errors = append(report.Errors, fmt.Sprintf("resolve footage root: %v", err))
		report.FinishedAt = s.nowFn().UTC()
		logRetentionReport(report)
		return report, err
	}
	report.FootageRoot = root

	var clips []models.Clip
	if err := s.DB.Preload("VideoFiles").Preload("Telemetry").Find(&clips).Error; err != nil {
		report.Errors = append(report.Errors, fmt.Sprintf("load clips: %v", err))
		report.FinishedAt = s.nowFn().UTC()
		logRetentionReport(report)
		return report, err
	}

	minAge := cfg.MinFileAge()
	now := s.nowFn()
	evals := make([]clipEval, 0, len(clips))
	var totalBytes int64

	for _, clip := range clips {
		ev := clipEval{clip: clip, protected: IsClipProtected(clip)}
		for _, vf := range clip.VideoFiles {
			safePath, skip, ok := s.safeDeletablePath(root, vf.FilePath, minAge, now)
			if !ok {
				if skip.Reason != "" {
					skip.ClipID = clip.ID
					report.Skipped = append(report.Skipped, skip)
				}
				continue
			}
			info, err := s.lstatFn(safePath)
			if err != nil {
				report.Skipped = append(report.Skipped, RetentionSkip{
					ClipID: clip.ID,
					Path:   safePath,
					Reason: "stat_failed",
				})
				continue
			}
			ev.paths = append(ev.paths, safePath)
			ev.bytes += info.Size()
		}
		totalBytes += ev.bytes
		evals = append(evals, ev)
	}
	report.TotalBytesBefore = totalBytes

	sort.SliceStable(evals, func(i, j int) bool {
		ti, tj := evals[i].clip.Timestamp, evals[j].clip.Timestamp
		if ti.Equal(tj) {
			return evals[i].clip.ID < evals[j].clip.ID
		}
		return ti.Before(tj)
	})

	selected := selectRetentionCandidates(evals, cfg, now, totalBytes)
	report.Candidates = selected

	doDelete := cfg.Enabled && !cfg.DryRun
	var bytesFreed int64
	for _, cand := range selected {
		if !doDelete {
			bytesFreed += cand.Bytes
			continue
		}
		freed, err := s.pruneClip(root, cand)
		if err != nil {
			report.Errors = append(report.Errors, fmt.Sprintf("clip %d: %v", cand.ClipID, err))
			// Still count successfully removed bytes from a partial pass.
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

func selectRetentionCandidates(evals []clipEval, cfg RetentionConfig, now time.Time, totalBytes int64) []RetentionCandidate {
	maxAgeCutoff := time.Time{}
	if cfg.MaxAgeDays > 0 {
		maxAgeCutoff = now.AddDate(0, 0, -cfg.MaxAgeDays)
	}
	maxBytes := cfg.MaxSizeBytes()

	ageIDs := map[uint]bool{}
	sizeIDs := map[uint]bool{}
	selectedOrder := make([]clipEval, 0)
	selectedSet := map[uint]bool{}
	projected := totalBytes

	for _, ev := range evals {
		if ev.protected || len(ev.paths) == 0 {
			continue
		}
		if cfg.MaxAgeDays > 0 && ev.clip.Timestamp.Before(maxAgeCutoff) {
			ageIDs[ev.clip.ID] = true
			selectedOrder = append(selectedOrder, ev)
			selectedSet[ev.clip.ID] = true
			projected -= ev.bytes
		}
	}

	if maxBytes > 0 && projected > maxBytes {
		for _, ev := range evals {
			if projected <= maxBytes {
				break
			}
			if ev.protected || len(ev.paths) == 0 || selectedSet[ev.clip.ID] {
				continue
			}
			sizeIDs[ev.clip.ID] = true
			selectedOrder = append(selectedOrder, ev)
			selectedSet[ev.clip.ID] = true
			projected -= ev.bytes
		}
		// Clips already selected for age also count as size when the post-age
		// projected total was still over the cap before they were removed.
		// Label age-selected clips as age+size when size pressure remains after
		// removing only non-age candidates would be insufficient — simpler rule:
		// if we entered this block, every selected clip contributed to relieving
		// the cap. Mark age clips as age+size; pure size stay size.
		for id := range ageIDs {
			if selectedSet[id] {
				sizeIDs[id] = true
			}
		}
	}

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
		out = append(out, RetentionCandidate{
			ClipID:    ev.clip.ID,
			Event:     ev.clip.Event,
			SourceDir: ev.clip.SourceDir,
			Timestamp: ev.clip.Timestamp,
			Reason:    reason,
			Bytes:     ev.bytes,
			FilePaths: append([]string(nil), ev.paths...),
		})
	}
	return out
}

func (s *RetentionService) pruneClip(root string, cand RetentionCandidate) (int64, error) {
	var clip models.Clip
	if err := s.DB.Preload("VideoFiles").Preload("Telemetry").First(&clip, cand.ClipID).Error; err != nil {
		return 0, err
	}
	if IsClipProtected(clip) {
		return 0, fmt.Errorf("refusing to prune protected clip")
	}

	// Soft-delete DB rows first so the library stays consistent if file IO fails mid-way.
	tx := s.DB.Begin()
	if tx.Error != nil {
		return 0, tx.Error
	}
	for i := range clip.VideoFiles {
		if err := tx.Delete(&clip.VideoFiles[i]).Error; err != nil {
			tx.Rollback()
			return 0, err
		}
	}
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
	if err := tx.Commit().Error; err != nil {
		return 0, err
	}

	var freed int64
	var fileErrs []string
	for _, p := range cand.FilePaths {
		// minAge=0 on delete: candidate already passed the freshness check at selection.
		safePath, skip, ok := s.safeDeletablePath(root, p, 0, s.nowFn())
		if !ok {
			fileErrs = append(fileErrs, fmt.Sprintf("%s: %s", p, skip.Reason))
			continue
		}
		info, err := s.lstatFn(safePath)
		if err != nil {
			if os.IsNotExist(err) {
				continue
			}
			fileErrs = append(fileErrs, err.Error())
			continue
		}
		if info.Mode()&os.ModeSymlink != 0 {
			fileErrs = append(fileErrs, fmt.Sprintf("%s: symlink", safePath))
			continue
		}
		size := info.Size()
		if err := s.removeFn(safePath); err != nil && !os.IsNotExist(err) {
			fileErrs = append(fileErrs, err.Error())
			continue
		}
		freed += size
	}

	if clip.SourceDir != "" && (strings.EqualFold(clip.Event, "Sentry") || strings.EqualFold(clip.Event, "Saved")) {
		if err := s.removeEmptyEventDir(root, clip.SourceDir); err != nil {
			fileErrs = append(fileErrs, err.Error())
		}
	}

	if len(fileErrs) == 0 {
		s.DB.Unscoped().Where("clip_id = ?", clip.ID).Delete(&models.VideoFile{})
		s.DB.Unscoped().Where("clip_id = ?", clip.ID).Delete(&models.Telemetry{})
		s.DB.Unscoped().Delete(&models.Clip{}, clip.ID)
	}

	if len(fileErrs) > 0 {
		return freed, fmt.Errorf("partial file delete: %s", strings.Join(fileErrs, "; "))
	}
	return freed, nil
}

func (s *RetentionService) removeEmptyEventDir(root, sourceDir string) error {
	candidate := sourceDir
	if !filepath.IsAbs(candidate) {
		candidate = filepath.Join(root, sourceDir)
	}
	abs, err := filepath.Abs(filepath.Clean(candidate))
	if err != nil {
		return err
	}
	if !pathInsideRoot(abs, root) {
		return fmt.Errorf("event dir outside root: %s", abs)
	}
	info, err := s.lstatFn(abs)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return err
	}
	if info.Mode()&os.ModeSymlink != 0 {
		return fmt.Errorf("skip symlink event dir: %s", abs)
	}
	if !info.IsDir() {
		return nil
	}
	entries, err := s.readDirFn(abs)
	if err != nil {
		return err
	}
	for _, e := range entries {
		p := filepath.Join(abs, e.Name())
		fi, err := s.lstatFn(p)
		if err != nil {
			continue
		}
		if fi.Mode()&os.ModeSymlink != 0 || fi.IsDir() {
			continue
		}
		_ = s.removeFn(p)
	}
	entries, err = s.readDirFn(abs)
	if err != nil {
		return err
	}
	if len(entries) == 0 {
		return s.removeFn(abs)
	}
	return nil
}

// safeDeletablePath ensures the path is under root, is not a symlink, and is not freshly written.
// It never follows symlinks (Lstat only).
func (s *RetentionService) safeDeletablePath(root, filePath string, minAge time.Duration, now time.Time) (string, RetentionSkip, bool) {
	if strings.TrimSpace(filePath) == "" {
		return "", RetentionSkip{Reason: "empty_path"}, false
	}
	cleaned := filepath.Clean(filePath)
	if !filepath.IsAbs(cleaned) {
		cleaned = filepath.Join(root, cleaned)
	}
	abs, err := filepath.Abs(cleaned)
	if err != nil {
		return "", RetentionSkip{Path: filePath, Reason: "abs_failed"}, false
	}
	if !pathInsideRoot(abs, root) {
		return "", RetentionSkip{Path: abs, Reason: "outside_root"}, false
	}

	info, err := s.lstatFn(abs)
	if err != nil {
		if os.IsNotExist(err) {
			return "", RetentionSkip{Path: abs, Reason: "missing"}, false
		}
		return "", RetentionSkip{Path: abs, Reason: "lstat_failed"}, false
	}
	if info.Mode()&os.ModeSymlink != 0 {
		return "", RetentionSkip{Path: abs, Reason: "symlink"}, false
	}
	if info.IsDir() {
		return "", RetentionSkip{Path: abs, Reason: "is_directory"}, false
	}
	if minAge > 0 && now.Sub(info.ModTime()) < minAge {
		return "", RetentionSkip{Path: abs, Reason: "recent_mtime"}, false
	}
	return abs, RetentionSkip{}, true
}

func logRetentionReport(report *RetentionReport) {
	payload, err := json.Marshal(report)
	if err != nil {
		log.Printf("retention: report marshal error: %v", err)
		return
	}
	log.Printf("retention: report %s", string(payload))
}
