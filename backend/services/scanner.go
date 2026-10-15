package services

import (
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/bradfitz/latlong"
	"github.com/fsnotify/fsnotify"
	"github.com/jinzhu/gorm"
	"teslaxy/models"
	pb "teslaxy/proto"
)

// SEIExtractor is a function type for extracting SEI metadata.
type SEIExtractor func(path string) ([]*pb.SeiMetadata, error)

// ScannerService handles directory scanning and file watching.
type ScannerService struct {
	FootagePath  string
	DB           *gorm.DB
	Watcher      *fsnotify.Watcher
	SEIExtractor SEIExtractor

	// Incremental update state
	mu           sync.Mutex
	pendingFiles map[string][]string    // Key is Directory Path
	timers       map[string]*time.Timer // Key is Directory Path

	// Per-directory scan locks prevent duplicate Clip rows when debounce and
	// event.json both trigger scanDir on the same path (L3).
	dirLocks sync.Map // map[string]*sync.Mutex

	reconcileTimer *time.Timer
}

var (
	// Tesla file format:
	// Standard: 2019-01-21_14-15-20-front.mp4
	// With MS:  2019-01-21_14-15-20_123456-front.mp4 (or _front.mp4)
	fileRegex = regexp.MustCompile(`(\d{4}-\d{2}-\d{2}_\d{2}-\d{2}-\d{2})(?:[_-]\d+)?[_-]([a-zA-Z0-9_]+)\.mp4$`)

	// recentIndexMu serializes Recent find-or-create. The media lock is a
	// read lock, so concurrent ScanAll calls would otherwise all pass the
	// "does this clip exist?" check and insert duplicate rows.
	recentIndexMu sync.Mutex

	// reconcileDebounce is the fsnotify Remove/Rename debounce before the
	// first ReconcileMissing pass. Tests may lower it.
	reconcileDebounce = 2 * time.Second
)

func NewScannerService(footagePath string, db *gorm.DB) *ScannerService {
	return &ScannerService{
		FootagePath:  footagePath,
		DB:           db,
		SEIExtractor: ExtractSEI,
		pendingFiles: make(map[string][]string),
		timers:       make(map[string]*time.Timer),
	}
}

func (s *ScannerService) Start() {
	// Initial scan
	go s.ScanAll()

	// Watch for changes
	var err error
	s.Watcher, err = fsnotify.NewWatcher()
	if err != nil {
		fmt.Println("Error creating watcher:", err)
		return
	}

	go func() {
		for {
			select {
			case event, ok := <-s.Watcher.Events:
				if !ok {
					return
				}
				if event.Op&fsnotify.Create == fsnotify.Create {
					s.handleFileCreate(event.Name)
				}
				if event.Op&(fsnotify.Remove|fsnotify.Rename) != 0 {
					s.scheduleReconcile()
				}
			case err, ok := <-s.Watcher.Errors:
				if !ok {
					return
				}
				fmt.Println("Watcher error:", err)
			}
		}
	}()

	err = s.Watcher.Add(s.FootagePath)
	if err != nil {
		fmt.Println("Error adding watcher path:", err)
	}

	// Also watch subdirectories (skip unreadable paths; do not abort the walk)
	filepath.Walk(s.FootagePath, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			log.Printf("Skipping unreadable path while watching %s: %v", path, err)
			return nil
		}
		if info != nil && info.IsDir() {
			s.Watcher.Add(path)
		}
		return nil
	})
}

func (s *ScannerService) handleFileCreate(path string) {
	filename := filepath.Base(path)

	// Check if it's a new directory (need to watch it and scan existing files — M2)
	if info, err := os.Stat(path); err == nil && info.IsDir() {
		fmt.Println("New directory detected, watching:", path)
		if s.Watcher != nil {
			_ = s.Watcher.Add(path)
			// Nested dirs inside a moved/copied tree must be watched too.
			filepath.Walk(path, func(p string, fi os.FileInfo, e error) error {
				if e != nil {
					log.Printf("Skipping unreadable path while watching %s: %v", p, e)
					return nil
				}
				if fi != nil && fi.IsDir() {
					_ = s.Watcher.Add(p)
				}
				return nil
			})
		}
		// Pre-populated trees never emit per-file Create events. Walk nested
		// event directories; scanning only the outer folder drops them.
		go s.scanTree(path)
		return
	}

	// Case 1: Video file
	if fileRegex.MatchString(filename) {
		dir := filepath.Dir(path)

		s.mu.Lock()
		// Add to pending files if not already present
		exists := false
		for _, f := range s.pendingFiles[dir] {
			if f == path {
				exists = true
				break
			}
		}
		if !exists {
			s.pendingFiles[dir] = append(s.pendingFiles[dir], path)
		}

		// Debounce: reset timer for this directory
		if t, ok := s.timers[dir]; ok {
			t.Stop()
		}
		s.timers[dir] = time.AfterFunc(2*time.Second, func() {
			s.processPending(dir)
		})
		s.mu.Unlock()

		fmt.Println("New file detected, queued for processing:", filename)
		return
	}

	// Case 2: event.json
	if strings.ToLower(filename) == "event.json" {
		fmt.Println("event.json detected:", path)
		dir := filepath.Dir(path)
		// Trigger re-scan of the directory
		go s.scanDir(dir)
	}
}

func (s *ScannerService) processPending(dirPath string) {
	s.mu.Lock()
	files, ok := s.pendingFiles[dirPath]
	if !ok {
		s.mu.Unlock()
		return
	}
	// Cleanup
	delete(s.pendingFiles, dirPath)
	delete(s.timers, dirPath)
	s.mu.Unlock()

	fmt.Printf("Processing update for directory %s with %d files\n", dirPath, len(files))
	// Re-scan directory to ensure completeness
	s.scanDir(dirPath)
}

func (s *ScannerService) scanDir(dirPath string) {
	lockMediaRead()
	defer unlockMediaRead()
	s.scanDirLocked(dirPath)
}

// scanTree indexes root and every nested directory. A moved TeslaCam tree
// contains event folders below the directory the watcher reports.
func (s *ScannerService) scanTree(root string) {
	lockMediaRead()
	defer unlockMediaRead()
	s.scanTreeLocked(root)
}

func (s *ScannerService) scanTreeLocked(root string) {
	var dirs []string
	_ = filepath.Walk(root, func(p string, fi os.FileInfo, err error) error {
		if err != nil {
			log.Printf("Skipping unreadable path while scanning %s: %v", p, err)
			return nil
		}
		if fi != nil && fi.IsDir() {
			dirs = append(dirs, p)
		}
		return nil
	})
	for _, d := range dirs {
		s.scanDirLocked(d)
	}
}

func (s *ScannerService) scanDirLocked(dirPath string) {
	unlock := s.lockDir(dirPath)
	defer unlock()

	// Check for event.json to determine type
	isEvent := false
	eventJsonPath := filepath.Join(dirPath, "event.json")
	if _, err := os.Stat(eventJsonPath); err == nil {
		isEvent = true
	} else if strings.Contains(dirPath, "SentryClips") || strings.Contains(dirPath, "SavedClips") {
		isEvent = true
	}

	// Read all files in the directory
	files, err := os.ReadDir(dirPath)
	if err != nil {
		fmt.Println("Error reading dir:", err)
		return
	}

	var filePaths []string
	for _, f := range files {
		if !f.IsDir() && strings.HasSuffix(f.Name(), ".mp4") {
			// Validate regex
			if fileRegex.MatchString(f.Name()) {
				filePaths = append(filePaths, filepath.Join(dirPath, f.Name()))
			}
		}
	}

	if len(filePaths) > 0 {
		if isEvent {
			// Caller already holds the media read lock; do not re-RLock
			// (queued writers + nested RLock deadlocks under Go's RWMutex).
			s.processEventGroupLocked(dirPath, filePaths)
		} else {
			s.processRecentGroupLocked(filePaths)
		}
	}
}

// lockDir serializes scanDir/processEventGroup work for one directory (L3).
func (s *ScannerService) lockDir(dirPath string) func() {
	v, _ := s.dirLocks.LoadOrStore(dirPath, &sync.Mutex{})
	m := v.(*sync.Mutex)
	m.Lock()
	return m.Unlock
}

// reconcileStateDir is where pending delete-all confirmation is persisted.
func (s *ScannerService) reconcileStateDir() string {
	if p := os.Getenv("CONFIG_PATH"); p != "" {
		return p
	}
	return "."
}

// scheduleReconcile debounces filesystem removals into ReconcileMissing (M1).
func (s *ScannerService) scheduleReconcile() {
	s.scheduleReconcileAfter(reconcileDebounce)
}

// scheduleReconcileAfter runs ReconcileMissing once after d. Used for the
// initial debounce and for pending-confirm retries after the min interval.
func (s *ScannerService) scheduleReconcileAfter(d time.Duration) {
	if d < 0 {
		d = 0
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.reconcileTimer != nil {
		s.reconcileTimer.Stop()
	}
	s.reconcileTimer = time.AfterFunc(d, s.runReconcile)
}

// runReconcile is the watcher/ScanAll shared entry for ReconcileMissing.
// When confirmation is pending it schedules another attempt after the remaining wait.
func (s *ScannerService) runReconcile() {
	_, err := ReconcileMissing(s.DB, s.FootagePath, ReconcileOpts{
		StateDir: s.reconcileStateDir(),
	})
	if errors.Is(err, ErrReconcilePendingConfirm) {
		remain := PendingConfirmRemaining(s.reconcileStateDir(), s.FootagePath)
		if remain <= 0 {
			remain = time.Second
		}
		log.Printf("ReconcileMissing pending confirm; retry in %s", remain.Round(time.Millisecond))
		s.scheduleReconcileAfter(remain)
		return
	}
	if err != nil {
		log.Printf("ReconcileMissing after remove: %v", err)
	}
}

// PurgeMissing is the explicit operator purge path: soft-delete missing
// VideoFiles and empty Clips under the footage root in one pass. The root
// must still exist (a missing mount is refused).
func (s *ScannerService) PurgeMissing() (ReconcileResult, error) {
	return PurgeMissingFootage(s.DB, s.FootagePath, s.reconcileStateDir())
}

func (s *ScannerService) ScanAll() {
	fmt.Println("Starting full scan of", s.FootagePath)
	start := time.Now()

	// 1. Map files
	eventDirs := make(map[string]struct{})
	var recentFiles []string

	// H1: never abort the whole library scan because one subdirectory is unreadable.
	_ = filepath.Walk(s.FootagePath, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			log.Printf("Skipping unreadable path %s: %v", path, err)
			return nil
		}
		if info == nil {
			return nil
		}
		if !info.IsDir() && strings.HasSuffix(info.Name(), ".mp4") {
			if fileRegex.MatchString(info.Name()) {
				dir := filepath.Dir(path)

				// Check if event dir
				isEvent := false
				if strings.Contains(dir, "SentryClips") || strings.Contains(dir, "SavedClips") {
					isEvent = true
				} else {
					if _, err := os.Stat(filepath.Join(dir, "event.json")); err == nil {
						isEvent = true
					}
				}

				if isEvent {
					eventDirs[dir] = struct{}{}
				} else {
					recentFiles = append(recentFiles, path)
				}
			}
		}
		return nil
	})

	// 2-3. Index while holding the media read lock so ReconcileMissing (write
	// lock) cannot delete rows this scan is inserting. Unlock before reconcile.
	func() {
		lockMediaRead()
		defer unlockMediaRead()

		var wg sync.WaitGroup
		semaphore := make(chan struct{}, 5)

		for dir := range eventDirs {
			wg.Add(1)
			semaphore <- struct{}{}
			go func(d string) {
				defer wg.Done()
				defer func() { <-semaphore }()
				// scanDirLocked: ScanAll already holds the read lock.
				s.scanDirLocked(d)
			}(dir)
		}
		wg.Wait()

		if len(recentFiles) > 0 {
			fmt.Printf("Processing %d recent files...\n", len(recentFiles))
			s.processRecentGroupLocked(recentFiles)
		}
	}()

	// 4. Reconcile missing footage (M1). Soft-delete only after a persisted,
	// time-separated confirmation (or explicit PurgeMissing). Shared with retention (#238).
	if _, err := ReconcileMissing(s.DB, s.FootagePath, ReconcileOpts{
		StateDir: s.reconcileStateDir(),
	}); errors.Is(err, ErrReconcilePendingConfirm) {
		remain := PendingConfirmRemaining(s.reconcileStateDir(), s.FootagePath)
		if remain <= 0 {
			remain = time.Second
		}
		log.Printf("ReconcileMissing after scan pending confirm; retry in %s", remain.Round(time.Millisecond))
		s.scheduleReconcileAfter(remain)
	} else if err != nil {
		log.Printf("ReconcileMissing after scan: %v", err)
	}

	fmt.Printf("Scan complete in %v.\n", time.Since(start))
}

// Struct to hold file info for sorting
type fileInfo struct {
	path      string
	timestamp time.Time
}

// groupFilesByTimestamp creates groups of files (the 6 cameras) synchronized by timestamp.
func groupFilesByTimestamp(files []fileInfo) [][]fileInfo {
	// Bucket by timestamp
	buckets := make(map[time.Time][]fileInfo)
	var times []time.Time

	for _, f := range files {
		if _, exists := buckets[f.timestamp]; !exists {
			times = append(times, f.timestamp)
		}
		buckets[f.timestamp] = append(buckets[f.timestamp], f)
	}

	sort.Slice(times, func(i, j int) bool {
		return times[i].Before(times[j])
	})

	var result [][]fileInfo
	for _, t := range times {
		result = append(result, buckets[t])
	}
	return result
}

// processEventGroup is the primary place where "one logical Tesla event" becomes one Clip record.
//
// SOURCE OF TRUTH RULE (addresses review 1.1 + 1.2):
//   - The physical directory on disk + event.json is the source of truth for a Sentry/Saved event.
//   - We store SourceDir = dirPath so the Clip can be reliably found and updated on re-scans.
//   - event.json provides city, reason, event_timestamp, and initial coordinates.
//   - SEI data from all Front camera files in the directory provides rich telemetry (via aggregateTelemetry).
//   - The frontend must treat any Clip that has SourceDir set (or >1 VideoFile) as already-grouped.
func (s *ScannerService) processEventGroup(dirPath string, filePaths []string) {
	if len(filePaths) == 0 {
		return
	}
	lockMediaRead()
	defer unlockMediaRead()
	s.processEventGroupLocked(dirPath, filePaths)
}

// processEventGroupLocked indexes one event directory. Caller must already hold
// the media read lock (ScanAll / scanDirLocked) or call processEventGroup.
func (s *ScannerService) processEventGroupLocked(dirPath string, filePaths []string) {
	if len(filePaths) == 0 {
		return
	}

	// Parse Metadata
	var city string
	var estLat, estLon float64
	var timezone *time.Location
	var eventJsonTimestamp *time.Time // Timestamp from event.json

	eventJsonPath := filepath.Join(dirPath, "event.json")
	if _, err := os.Stat(eventJsonPath); err == nil {
		content, err := os.ReadFile(eventJsonPath)
		if err == nil {
			var eventData struct {
				Timestamp string      `json:"timestamp"`
				City      string      `json:"city"`
				Reason    string      `json:"reason"`
				EstLat    interface{} `json:"est_lat"`
				EstLon    interface{} `json:"est_lon"`
			}
			if err := json.Unmarshal(content, &eventData); err == nil {
				city = eventData.City

				// Update Reason in Clip
				if eventData.Reason != "" {
					// We'll update the clip with this reason later
				}

				toFloat := func(v interface{}) float64 {
					switch val := v.(type) {
					case float64:
						return val
					case string:
						f, _ := strconv.ParseFloat(val, 64)
						return f
					default:
						return 0
					}
				}

				estLat = toFloat(eventData.EstLat)
				estLon = toFloat(eventData.EstLon)

				if city == "" && ValidGeographicPoint(estLat, estLon) {
					city = fmt.Sprintf("%.4f, %.4f", estLat, estLon)
				}
				timezone = determineTimezone(estLat, estLon)

				if t, err := time.ParseInLocation("2006-01-02T15:04:05", eventData.Timestamp, timezone); err == nil {
					eventJsonTimestamp = &t
				} else if t, err := time.Parse(time.RFC3339, eventData.Timestamp); err == nil {
					eventJsonTimestamp = &t
				}
			}
		}
	}

	if timezone == nil {
		timezone = determineTimezone(0, 0)
	}

	var files []fileInfo
	for _, path := range filePaths {
		if s.isPendingDeletePath(path) {
			continue
		}
		if _, err := os.Stat(path); err != nil {
			continue
		}
		matches := fileRegex.FindStringSubmatch(filepath.Base(path))
		if len(matches) == 3 {
			// Using matches[1] (date-time)
			t, err := time.ParseInLocation("2006-01-02_15-04-05", matches[1], timezone)
			if err == nil {
				files = append(files, fileInfo{path: path, timestamp: t})
			}
		}
	}

	// Sort files by timestamp
	sort.Slice(files, func(i, j int) bool {
		return files[i].timestamp.Before(files[j].timestamp)
	})

	if len(files) == 0 {
		return
	}

	// Use the timestamp of the FIRST file as the Clip timestamp
	minTime := files[0].timestamp

	eventType := "Recent"
	if strings.Contains(dirPath, "SentryClips") {
		eventType = "Sentry"
	} else if strings.Contains(dirPath, "SavedClips") {
		eventType = "Saved"
	}

	// Helper to read reason
	var eventReason string
	if _, err := os.Stat(eventJsonPath); err == nil {
		content, _ := os.ReadFile(eventJsonPath)
		var ed struct {
			Reason string `json:"reason"`
		}
		json.Unmarshal(content, &ed)
		eventReason = ed.Reason
	}

	// === SOURCE OF TRUTH: event directory + event.json ===
	// We now use SourceDir (the physical Tesla folder) as the stable primary key for a logical event.
	// This prevents duplicate clips when rescanning and makes event.json the authoritative metadata source.
	var clip models.Clip
	if err := s.DB.Where("source_dir = ?", dirPath).First(&clip).Error; err != nil {
		if gorm.IsRecordNotFoundError(err) {
			// Do not recreate a retention-pending prune tombstone.
			if s.isPendingDeleteSourceDir(dirPath) {
				return
			}
			// Clear a reconcile-only soft-delete so restored footage is visible again.
			if restored, ok := s.clearReconcileClipTombstone(dirPath); ok {
				clip = restored
			} else {
				clip = models.Clip{
					Timestamp:      minTime,
					Event:          eventType,
					City:           city,
					Reason:         eventReason,
					EventTimestamp: eventJsonTimestamp,
					SourceDir:      dirPath, // <-- Critical: stable identity from Tesla's folder structure
				}
				s.DB.Create(&clip)
			}
		}
	} else {
		// Update existing using event.json as the source of truth
		updates := map[string]interface{}{}
		if clip.EventTimestamp == nil && eventJsonTimestamp != nil {
			updates["event_timestamp"] = eventJsonTimestamp
		}
		if city != "" && clip.City == "" {
			updates["city"] = city
		}
		if eventReason != "" && clip.Reason == "" {
			updates["reason"] = eventReason
		}
		if clip.SourceDir == "" {
			updates["source_dir"] = dirPath
		}
		if len(updates) > 0 {
			s.DB.Model(&clip).Updates(updates)
		}
	}

	// Seed incident coordinates from event.json onto a single telemetry row.
	// Reload TelemetryID in memory so aggregateTelemetry updates this row instead of creating another.
	if clip.TelemetryID == 0 && ValidGeographicPoint(estLat, estLon) {
		telemetry := models.Telemetry{
			ClipID:    clip.ID,
			Latitude:  estLat,
			Longitude: estLon,
		}
		if err := s.DB.Create(&telemetry).Error; err == nil {
			s.DB.Model(&clip).Update("telemetry_id", telemetry.ID)
			clip.TelemetryID = telemetry.ID
		}
	}

	// Add ALL files in the directory to this single clip
	s.addFilesToClip(clip, files)

	// Aggregate Telemetry (will process all front files sorted by time)
	s.aggregateTelemetry(&clip, files, estLat, estLon)
}

func pathsInClipGroup(clipGroup [][]fileInfo) []string {
	var paths []string
	for _, segment := range clipGroup {
		for _, f := range segment {
			paths = append(paths, f.path)
		}
	}
	return paths
}

// findRecentClipByFilePaths returns the live Recent clip that already owns one
// of paths. A Sentry/Saved clip with the same timestamp is not a match (H2).
func (s *ScannerService) findRecentClipByFilePaths(paths []string) (models.Clip, bool) {
	if len(paths) == 0 {
		return models.Clip{}, false
	}
	var vf models.VideoFile
	if err := s.DB.Where("file_path IN (?)", paths).First(&vf).Error; err != nil {
		return models.Clip{}, false
	}
	var clip models.Clip
	if err := s.DB.First(&clip, vf.ClipID).Error; err != nil {
		return models.Clip{}, false
	}
	if clip.Event != "Recent" {
		return models.Clip{}, false
	}
	return clip, true
}

// processRecentGroup groups flat RecentClips into logical multi-minute drives using time heuristics.
//
// This is the secondary (less reliable) grouping path. RecentClips often lack event.json,
// so we fall back to 65-second gap detection. We still try to set SourceDir for traceability.
// Long-term goal: Tesla may start providing better metadata even for Recent clips.
func (s *ScannerService) processRecentGroup(filePaths []string) {
	if len(filePaths) == 0 {
		return
	}
	lockMediaRead()
	defer unlockMediaRead()
	s.processRecentGroupLocked(filePaths)
}

func (s *ScannerService) processRecentGroupLocked(filePaths []string) {
	if len(filePaths) == 0 {
		return
	}
	recentIndexMu.Lock()
	defer recentIndexMu.Unlock()

	timezone := determineTimezone(0, 0)

	var files []fileInfo
	for _, path := range filePaths {
		if s.isPendingDeletePath(path) {
			continue
		}
		if _, err := os.Stat(path); err != nil {
			continue
		}
		matches := fileRegex.FindStringSubmatch(filepath.Base(path))
		if len(matches) == 3 {
			t, err := time.ParseInLocation("2006-01-02_15-04-05", matches[1], timezone)
			if err == nil {
				files = append(files, fileInfo{path: path, timestamp: t})
			}
		}
	}

	// 1. Group into "Time Buckets" (Camera Sets)
	timeGroups := groupFilesByTimestamp(files)
	if len(timeGroups) == 0 {
		return
	}

	// 2. Merge Time Buckets into "Continuous Clips"
	var clipGroups [][][]fileInfo // List of Clips, each Clip is a list of Camera Sets (Time Buckets)
	currentClipGroup := [][]fileInfo{timeGroups[0]}

	for i := 1; i < len(timeGroups); i++ {
		prevTime := timeGroups[i-1][0].timestamp
		currTime := timeGroups[i][0].timestamp

		// If gap is > 5 seconds (assuming ~60s duration for prev clip), split
		// StartDiff > 65s implies Gap > 5s
		if currTime.Sub(prevTime) > 65*time.Second {
			clipGroups = append(clipGroups, currentClipGroup)
			currentClipGroup = [][]fileInfo{timeGroups[i]}
		} else {
			currentClipGroup = append(currentClipGroup, timeGroups[i])
		}
	}
	clipGroups = append(clipGroups, currentClipGroup)

	// 3. Process each Clip Group
	for _, clipGroup := range clipGroups { // clipGroup is [][]fileInfo (a list of minute-segments)
		if len(clipGroup) == 0 {
			continue
		}

		// Start time of the FIRST segment in this continuous block
		minTime := clipGroup[0][0].timestamp

		var clip models.Clip
		found := false

		// Idempotency: a live Recent clip that already owns one of these files
		// wins, even when its timestamp is an earlier minute that has since
		// been deleted. Matching only `event = Recent AND timestamp = minTime`
		// misses that clip and inserts a second copy of the remaining footage.
		// H2: never reuse a non-Recent clip that happens to share a timestamp.
		if existing, ok := s.findRecentClipByFilePaths(pathsInClipGroup(clipGroup)); ok {
			clip = existing
			found = true
		}

		// Merge Strategy:
		// We look for any Recent clip that ends just before `minTime`.
		// Lookback window matches the 5s split logic (Start-to-Start ~ 65s).
		startTime := minTime.Add(-65 * time.Second)

		var lastVf models.VideoFile
		if !found {
			if err := s.DB.Table("video_files").
				Joins("JOIN clips ON video_files.clip_id = clips.id AND clips.deleted_at IS NULL").
				Where("clips.event = ? AND video_files.deleted_at IS NULL AND video_files.timestamp >= ? AND video_files.timestamp < ?", "Recent", startTime, minTime).
				Order("video_files.timestamp desc").
				First(&lastVf).Error; err == nil {

				if err := s.DB.First(&clip, lastVf.ClipID).Error; err == nil && clip.Event == "Recent" {
					found = true
				}
			}
		}
		if !found {
			if err := s.DB.Where("event = ? AND timestamp = ?", "Recent", minTime).First(&clip).Error; err == nil {
				// Idempotency: only match a live Recent clip (H2). GORM soft-delete
				// adds deleted_at IS NULL so soft-deleted rows are not reused.
				found = true
			}
		}

		if !found {
			firstDir := ""
			if len(clipGroup) > 0 && len(clipGroup[0]) > 0 {
				firstDir = filepath.Dir(clipGroup[0][0].path)
			}
			clip = models.Clip{
				Timestamp: minTime,
				Event:     "Recent",
				SourceDir: firstDir,
			}
			s.DB.Create(&clip)
		} else if clip.SourceDir == "" {
			firstDir := ""
			if len(clipGroup) > 0 && len(clipGroup[0]) > 0 {
				firstDir = filepath.Dir(clipGroup[0][0].path)
			}
			s.DB.Model(&clip).Update("source_dir", firstDir)
		}

		// Flatten the clip group to get all files for telemetry aggregation
		var allFiles []fileInfo

		// Add ALL segments in this continuous block to the clip
		for _, segment := range clipGroup {
			s.addFilesToClip(clip, segment)
			allFiles = append(allFiles, segment...)
		}

		// Aggregate Telemetry. Recent clips have no event.json point (0,0) → valid SEI is used.
		s.aggregateTelemetry(&clip, allFiles, 0, 0)
	}
}

func (s *ScannerService) addFilesToClip(clip models.Clip, files []fileInfo) {
	for _, f := range files {
		if s.isPendingDeletePath(f.path) {
			continue
		}
		// Re-stat before insert so a prune that already removed the file cannot
		// leave a live DB row pointing at nothing.
		if _, err := os.Stat(f.path); err != nil {
			continue
		}
		matches := fileRegex.FindStringSubmatch(filepath.Base(f.path))
		cameraName := "Unknown"
		if len(matches) == 3 {
			cameraName = matches[2] // This is group 2 now in the new regex
		}
		cameraName = normalizeCameraName(cameraName)

		var vf models.VideoFile
		// One live row per path. Keying only on clip_id lets a second clip
		// insert the same file again after the first Recent minute is gone.
		err := s.DB.Where("file_path = ?", f.path).First(&vf).Error
		if err == nil {
			continue
		}
		if !gorm.IsRecordNotFoundError(err) {
			log.Printf("addFilesToClip: lookup %s: %v", f.path, err)
			continue
		}
		// Reconcile tombstone for a path that exists again: clear and reuse.
		if s.clearReconcileVideoTombstone(f.path, clip.ID, cameraName, f.timestamp) {
			continue
		}
		// Re-check retention tombstone + existence under the same conditions after the lookup.
		if s.isPendingDeletePath(f.path) {
			continue
		}
		if _, err := os.Stat(f.path); err != nil {
			continue
		}
		vf = models.VideoFile{
			ClipID:    clip.ID,
			Camera:    cameraName,
			FilePath:  f.path,
			Timestamp: f.timestamp,
		}
		if err := s.DB.Create(&vf).Error; err != nil {
			log.Printf("addFilesToClip: create %s: %v", f.path, err)
		}
	}
}

// isPendingDeletePath is true when retention soft-deleted a VideoFile for this path.
// The scanner must not recreate rows for files still awaiting unlink / hard-delete.
// Reconcile-only tombstones (retention_pending=false) are not pending deletes.
func (s *ScannerService) isPendingDeletePath(path string) bool {
	var n int
	s.DB.Unscoped().Model(&models.VideoFile{}).
		Where("file_path = ? AND deleted_at IS NOT NULL AND retention_pending = ?", path, true).
		Count(&n)
	return n > 0
}

// isPendingDeleteSourceDir is true when retention still owns a soft-deleted clip
// for this event directory (awaiting unlink / hard-delete).
func (s *ScannerService) isPendingDeleteSourceDir(dir string) bool {
	var n int
	s.DB.Unscoped().Model(&models.Clip{}).
		Where("source_dir = ? AND deleted_at IS NOT NULL AND retention_pending = ?", dir, true).
		Count(&n)
	return n > 0
}

// clearReconcileClipTombstone undeletes a non-retention soft-deleted clip for
// sourceDir so a rescan can restore visibility after footage returns.
func (s *ScannerService) clearReconcileClipTombstone(sourceDir string) (models.Clip, bool) {
	var clip models.Clip
	err := s.DB.Unscoped().
		Where("source_dir = ? AND deleted_at IS NOT NULL AND retention_pending = ?", sourceDir, false).
		First(&clip).Error
	if err != nil {
		return models.Clip{}, false
	}
	if err := s.DB.Unscoped().Model(&clip).
		Updates(map[string]interface{}{"deleted_at": nil, "retention_pending": false}).Error; err != nil {
		log.Printf("clearReconcileClipTombstone: clip %d: %v", clip.ID, err)
		return models.Clip{}, false
	}
	_ = s.DB.Unscoped().Model(&models.Telemetry{}).Where("clip_id = ?", clip.ID).
		Update("deleted_at", nil).Error
	clip.DeletedAt = nil
	clip.RetentionPending = false
	return clip, true
}

// clearReconcileVideoTombstone undeletes a non-retention soft-deleted VideoFile
// when the path exists again on disk. Returns true when a tombstone was cleared.
func (s *ScannerService) clearReconcileVideoTombstone(path string, clipID uint, camera string, ts time.Time) bool {
	var vf models.VideoFile
	err := s.DB.Unscoped().
		Where("file_path = ? AND deleted_at IS NOT NULL AND retention_pending = ?", path, false).
		First(&vf).Error
	if err != nil {
		return false
	}
	updates := map[string]interface{}{
		"deleted_at":        nil,
		"retention_pending": false,
		"clip_id":           clipID,
		"camera":            camera,
		"timestamp":         ts,
	}
	if err := s.DB.Unscoped().Model(&vf).Updates(updates).Error; err != nil {
		log.Printf("clearReconcileVideoTombstone: vf %d: %v", vf.ID, err)
		return false
	}
	return true
}

// aggregateTelemetry iterates through all 'Front' files in the clip, extracts SEI, and updates the Telemetry record.
// Speed/Gear/Steering/Autopilot/FullDataJson always come from the SEI midpoint.
// Latitude/Longitude are the incident point: valid event.json (or an already-stored valid
// incident point) wins over SEI, including SEI (0,0). This is not a playback track sample.
func (s *ScannerService) aggregateTelemetry(clip *models.Clip, files []fileInfo, eventLat, eventLon float64) {
	var frontFiles []fileInfo

	// 1. Filter for Front camera and Sort
	for _, f := range files {
		matches := fileRegex.FindStringSubmatch(filepath.Base(f.path))
		cameraName := ""
		if len(matches) == 3 {
			cameraName = matches[2]
		}
		if normalizeCameraName(cameraName) == "Front" {
			frontFiles = append(frontFiles, f)
		}
	}

	sort.Slice(frontFiles, func(i, j int) bool {
		return frontFiles[i].timestamp.Before(frontFiles[j].timestamp)
	})

	if len(frontFiles) == 0 {
		return
	}

	// 2. Extract and Aggregate SEI
	var aggregatedMeta []*pb.SeiMetadata

	for _, f := range frontFiles {
		meta, err := s.SEIExtractor(f.path)
		if err == nil && len(meta) > 0 {
			aggregatedMeta = append(aggregatedMeta, meta...)
		}
	}

	if len(aggregatedMeta) == 0 {
		return
	}

	// 3. Marshal to JSON
	jsonData, _ := json.Marshal(aggregatedMeta)

	// 4. Create or Update Telemetry
	var telemetry models.Telemetry

	if clip.TelemetryID != 0 {
		// Fetch existing
		s.DB.First(&telemetry, clip.TelemetryID)
	}

	existingLat, existingLon := telemetry.Latitude, telemetry.Longitude

	// Update fields
	telemetry.ClipID = clip.ID
	telemetry.FullDataJson = string(jsonData)

	var seiLat, seiLon float64
	// Update summary fields from the middle of the *entire* clip (approx)
	mid := len(aggregatedMeta) / 2
	if mid < len(aggregatedMeta) {
		m := aggregatedMeta[mid]
		telemetry.Speed = m.VehicleSpeedMps * 2.23694
		telemetry.Gear = m.GearState.String()
		seiLat = m.LatitudeDeg
		seiLon = m.LongitudeDeg
		telemetry.SteeringAngle = m.SteeringWheelAngle
		telemetry.AutopilotState = m.AutopilotState.String()
	}

	incidentLat, incidentLon := eventLat, eventLon
	if !ValidGeographicPoint(incidentLat, incidentLon) && ValidGeographicPoint(existingLat, existingLon) {
		incidentLat, incidentLon = existingLat, existingLon
	}
	lat, lon := SelectIncidentPosition(incidentLat, incidentLon, seiLat, seiLon)
	telemetry.Latitude = lat
	telemetry.Longitude = lon

	if telemetry.ID != 0 {
		s.DB.Save(&telemetry)
	} else {
		s.DB.Create(&telemetry)
		s.DB.Model(clip).Update("telemetry_id", telemetry.ID)
		clip.TelemetryID = telemetry.ID
	}

	// 5. Update City if missing — use the selected incident point, never (0,0).
	if clip.City == "" && ValidGeographicPoint(lat, lon) {
		newCity := fmt.Sprintf("%.4f, %.4f", lat, lon)
		s.DB.Model(clip).Update("city", newCity)
		clip.City = newCity
	}
}

func determineTimezone(lat, lon float64) *time.Location {
	// 1. Try Lat/Lon
	if lat != 0 || lon != 0 {
		zoneName := latlong.LookupZoneName(lat, lon)
		if zoneName != "" {
			if loc, err := time.LoadLocation(zoneName); err == nil {
				return loc
			}
		}
	}

	// 2. Fallback to Env or Default
	def := os.Getenv("DEFAULT_TIMEZONE")
	if def == "" {
		def = "Australia/Adelaide"
	}

	if loc, err := time.LoadLocation(def); err == nil {
		return loc
	}

	// 3. UTC if all else fails
	return time.UTC
}

func normalizeCameraName(raw string) string {
	raw = strings.ToLower(raw)
	switch raw {
	case "front":
		return "Front"
	case "back":
		return "Back"
	case "left_repeater":
		return "Left Repeater"
	case "right_repeater":
		return "Right Repeater"
	case "left_pillar":
		return "Left Pillar"
	case "right_pillar":
		return "Right Pillar"
	case "cabin":
		return "Cabin"
	}
	return strings.Title(raw)
}
