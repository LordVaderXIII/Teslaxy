package services

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"sync"
	"sync/atomic"
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
	Geocoder     Geocoder   // optional; nil skips reverse geocoding
	telemetryMu  sync.Mutex // serialize short telemetry transactions, not SEI extraction
	scanRunning  atomic.Bool

	// Incremental update state
	mu           sync.Mutex
	pendingFiles map[string][]string    // Key is Directory Path
	timers       map[string]*time.Timer // Key is Directory Path
}

var (
	// Tesla file format:
	// Standard: 2019-01-21_14-15-20-front.mp4
	// With MS:  2019-01-21_14-15-20_123456-front.mp4 (or _front.mp4)
	fileRegex = regexp.MustCompile(`(\d{4}-\d{2}-\d{2}_\d{2}-\d{2}-\d{2})(?:[_-]\d+)?[_-]([a-zA-Z0-9_]+)\.mp4$`)
)

func NewScannerService(footagePath string, db *gorm.DB) *ScannerService {
	return &ScannerService{
		FootagePath:  footagePath,
		DB:           db,
		SEIExtractor: ExtractSEI,
		Geocoder:     NewOfflineGeocoder(),
		pendingFiles: make(map[string][]string),
		timers:       make(map[string]*time.Timer),
	}
}

func (s *ScannerService) Start() {
	// Initial scan
	s.StartScan()

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
				// Create and Write: Tesla may create an empty/partial event.json
				// then write the body; we must not only watch Create.
				if event.Op&(fsnotify.Create|fsnotify.Write) != 0 {
					s.handleFileCreate(event.Name)
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

	// Also watch subdirectories
	filepath.Walk(s.FootagePath, func(path string, info os.FileInfo, err error) error {
		if info != nil && info.IsDir() {
			s.Watcher.Add(path)
		}
		return nil
	})
}

func (s *ScannerService) handleFileCreate(path string) {
	filename := filepath.Base(path)

	// Check if it's a new directory (need to watch it)
	if info, err := os.Stat(path); err == nil && info.IsDir() {
		fmt.Println("New directory detected, watching:", path)
		s.Watcher.Add(path)
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

	// Case 2: event.json — debounce like videos so a half-written create
	// is not scanned before Tesla finishes writing city/reason.
	if strings.ToLower(filename) == "event.json" {
		dir := filepath.Dir(path)
		fmt.Println("event.json detected, queued for processing:", path)

		s.mu.Lock()
		if _, ok := s.pendingFiles[dir]; !ok {
			s.pendingFiles[dir] = nil
		}
		if t, ok := s.timers[dir]; ok {
			t.Stop()
		}
		s.timers[dir] = time.AfterFunc(2*time.Second, func() {
			s.processPending(dir)
		})
		s.mu.Unlock()
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
			s.processEventGroup(dirPath, filePaths)
		} else {
			s.processRecentGroup(filePaths)
		}
	}
}

func (s *ScannerService) ScanAll() {
	if !s.scanRunning.CompareAndSwap(false, true) {
		fmt.Println("Skipping full scan: already running")
		return
	}
	s.scanAll()
}

// StartScan reserves the full-scan slot before starting background work.
// Startup, synchronous callers and HTTP rescans share the same reservation.
func (s *ScannerService) StartScan() bool {
	if !s.scanRunning.CompareAndSwap(false, true) {
		return false
	}
	go s.scanAll()
	return true
}

// ScanRunning reports whether a full scan is reserved or executing.
func (s *ScannerService) ScanRunning() bool {
	return s.scanRunning.Load()
}

func (s *ScannerService) scanAll() {
	defer s.scanRunning.Store(false)
	fmt.Println("Starting full scan of", s.FootagePath)
	start := time.Now()

	// 1. Map files
	eventDirs := make(map[string][]string)
	var recentFiles []string

	err := filepath.Walk(s.FootagePath, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
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
					eventDirs[dir] = append(eventDirs[dir], path)
				} else {
					recentFiles = append(recentFiles, path)
				}
			}
		}
		return nil
	})

	if err != nil {
		fmt.Println("Error walking path:", err)
		return
	}

	// 2. Process Event Groups (Parallel)
	var wg sync.WaitGroup
	semaphore := make(chan struct{}, 5)

	for dir, files := range eventDirs {
		wg.Add(1)
		semaphore <- struct{}{}
		go func(d string, f []string) {
			defer wg.Done()
			defer func() { <-semaphore }()
			s.processEventGroup(d, f)
		}(dir, files)
	}
	wg.Wait()

	// 3. Process Recent Files (Single Batch)
	if len(recentFiles) > 0 {
		fmt.Printf("Processing %d recent files...\n", len(recentFiles))
		s.processRecentGroup(recentFiles)
	}

	// Repair metadata from each clip's source, including previously backfilled cities.
	s.backfillIncompleteMetadata()

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

	// Parse Metadata (retry on half-written event.json)
	var city string
	var eventReason string
	var estLat, estLon float64
	var timezone *time.Location
	var eventJsonTimestamp *time.Time // Timestamp from event.json

	eventJsonPath := filepath.Join(dirPath, "event.json")
	if _, err := os.Stat(eventJsonPath); err == nil {
		ev, err := readEventJSONFile(eventJsonPath)
		if err != nil {
			fmt.Printf("event.json unreadable for %s: %v\n", dirPath, err)
		} else {
			city = ev.City
			eventReason = ev.Reason
			estLat, estLon = ev.EstLat, ev.EstLon
			if city == "" && ValidGeographicPoint(estLat, estLon) {
				city = fmt.Sprintf("%.4f, %.4f", estLat, estLon)
			}
			timezone = determineTimezone(estLat, estLon)
			if t, err := time.ParseInLocation("2006-01-02T15:04:05", ev.Timestamp, timezone); err == nil {
				eventJsonTimestamp = &t
			} else if t, err := time.Parse(time.RFC3339, ev.Timestamp); err == nil {
				eventJsonTimestamp = &t
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

	// === SOURCE OF TRUTH: event directory + event.json ===
	// We now use SourceDir (the physical Tesla folder) as the stable primary key for a logical event.
	// This prevents duplicate clips when rescanning and makes event.json the authoritative metadata source.
	var clip models.Clip
	if err := s.DB.Where("source_dir = ?", dirPath).First(&clip).Error; err != nil {
		if gorm.IsRecordNotFoundError(err) {
			// Do not recreate a soft-deleted (pending prune) event directory.
			if s.isPendingDeleteSourceDir(dirPath) {
				return
			}
			clip = models.Clip{
				Timestamp:      minTime,
				Event:          eventType,
				City:           city,
				Reason:         eventReason,
				EventTimestamp: eventJsonTimestamp,
				SourceDir:      dirPath, // <-- Critical: stable identity from Tesla's folder structure
			}
			if err := s.DB.Create(&clip).Error; err != nil {
				fmt.Printf("Skipping event %s: create clip: %v\n", dirPath, err)
				return
			}
		} else {
			fmt.Printf("Skipping event %s: lookup clip: %v\n", dirPath, err)
			return
		}
	} else {
		// Update existing using event.json as the source of truth
		if clip.ID == 0 {
			fmt.Printf("Skipping event %s: zero clip ID\n", dirPath)
			return
		}
		updates := map[string]interface{}{}
		if clip.EventTimestamp == nil && eventJsonTimestamp != nil {
			updates["event_timestamp"] = eventJsonTimestamp
		}
		// Tesla's own city always wins over empty or coordinate-string cities.
		if city != "" && !IsCoordinateCity(city) && (clip.City == "" || IsCoordinateCity(clip.City)) {
			updates["city"] = city
			clip.City = city
		} else if city != "" && clip.City == "" {
			updates["city"] = city
			clip.City = city
		}
		if eventReason != "" && clip.Reason == "" {
			updates["reason"] = eventReason
			clip.Reason = eventReason
		}
		if clip.SourceDir == "" {
			updates["source_dir"] = dirPath
		}
		if len(updates) > 0 {
			if err := s.DB.Model(&models.Clip{}).Where("id = ?", clip.ID).Updates(updates).Error; err != nil {
				fmt.Printf("Skipping event %s: update clip: %v\n", dirPath, err)
				return
			}
		}
	}
	if clip.ID == 0 {
		fmt.Printf("Skipping event %s: zero clip ID\n", dirPath)
		return
	}
	// Reverse-geocode coordinate fallbacks (and empty cities with coordinates).
	s.geocodeClipCity(&clip, estLat, estLon)

	// Seed incident coordinates from event.json onto a single telemetry row.
	// Reload TelemetryID in memory so aggregateTelemetry updates this row instead of creating another.
	if clip.TelemetryID == 0 && ValidGeographicPoint(estLat, estLon) {
		if err := s.writeClipTelemetry(&clip, func(row *models.Telemetry) {
			row.Latitude, row.Longitude = estLat, estLon
		}); err != nil {
			fmt.Printf("Skipping event %s: seed telemetry: %v\n", dirPath, err)
			return
		}
	}

	// Add ALL files in the directory to this single clip
	s.addFilesToClip(clip, files)

	// Aggregate Telemetry (will process all front files sorted by time)
	s.aggregateTelemetry(&clip, files, estLat, estLon)
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
		var found bool = false

		// Merge Strategy:
		// We look for any Recent clip that ends just before `minTime`.
		// Lookback window matches the 5s split logic (Start-to-Start ~ 65s).
		startTime := minTime.Add(-65 * time.Second)

		var lastVf models.VideoFile
		err := s.DB.Table("video_files").
			Joins("JOIN clips ON video_files.clip_id = clips.id").
			Where("clips.deleted_at IS NULL AND clips.event = ? AND video_files.timestamp >= ? AND video_files.timestamp < ?", "Recent", startTime, minTime).
			Order("video_files.timestamp desc").
			First(&lastVf).Error
		if err == nil {
			err = s.DB.First(&clip, lastVf.ClipID).Error
			found = err == nil && clip.ID != 0
		} else if gorm.IsRecordNotFoundError(err) {
			err = s.DB.Where("timestamp = ? AND event = ?", minTime, "Recent").First(&clip).Error
			found = err == nil && clip.ID != 0
		}
		if err != nil && !gorm.IsRecordNotFoundError(err) {
			fmt.Printf("Skipping recent group %s: lookup clip: %v\n", minTime, err)
			continue
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
			if err := s.DB.Create(&clip).Error; err != nil {
				fmt.Printf("Skipping recent group %s: create clip: %v\n", minTime, err)
				continue
			}
		} else if clip.SourceDir == "" {
			firstDir := ""
			if len(clipGroup) > 0 && len(clipGroup[0]) > 0 {
				firstDir = filepath.Dir(clipGroup[0][0].path)
			}
			if err := s.DB.Model(&models.Clip{}).Where("id = ?", clip.ID).Update("source_dir", firstDir).Error; err != nil {
				fmt.Printf("Skipping recent group %s: update clip: %v\n", minTime, err)
				continue
			}
		}

		if clip.ID == 0 {
			fmt.Printf("Skipping recent group %s: zero clip ID\n", minTime)
			continue
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
	if clip.ID == 0 {
		fmt.Println("Skipping files: zero clip ID")
		return
	}
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
		if err := s.DB.Where("clip_id = ? AND camera = ? AND file_path = ?", clip.ID, cameraName, f.path).First(&vf).Error; gorm.IsRecordNotFoundError(err) {
			// Re-check tombstone + existence under the same conditions after the lookup.
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
			s.DB.Create(&vf)
		}
	}
}

// isPendingDeletePath is true when retention soft-deleted a VideoFile for this path.
// The scanner must not recreate rows for files still awaiting unlink / hard-delete.
func (s *ScannerService) isPendingDeletePath(path string) bool {
	var n int
	s.DB.Unscoped().Model(&models.VideoFile{}).
		Where("file_path = ? AND deleted_at IS NOT NULL", path).
		Count(&n)
	return n > 0
}

// isPendingDeleteSourceDir is true when a soft-deleted Clip still owns this event directory.
func (s *ScannerService) isPendingDeleteSourceDir(dir string) bool {
	var n int
	s.DB.Unscoped().Model(&models.Clip{}).
		Where("source_dir = ? AND deleted_at IS NOT NULL", dir).
		Count(&n)
	return n > 0
}

// aggregateTelemetry iterates through all 'Front' files in the clip, extracts SEI, and updates the Telemetry record.
// Speed/Gear/Steering/Autopilot/FullDataJson always come from the SEI midpoint.
// Latitude/Longitude are the incident point: valid event.json (or an already-stored valid
// incident point) wins over SEI, including SEI (0,0). This is not a playback track sample.
func (s *ScannerService) aggregateTelemetry(clip *models.Clip, files []fileInfo, eventLat, eventLon float64) {
	if clip == nil || clip.ID == 0 {
		fmt.Println("Skipping telemetry: zero clip ID")
		return
	}
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
		// Missing cameras/coordinates must not leave a repaired clip pointing at
		// zero or another clip's row. Preserve owned data; do not invent SEI.
		if err := s.writeClipTelemetry(clip, func(row *models.Telemetry) {
			if ValidGeographicPoint(eventLat, eventLon) {
				row.Latitude, row.Longitude = eventLat, eventLon
			}
		}); err != nil {
			fmt.Printf("Skipping clip %d telemetry: %v\n", clip.ID, err)
		}
		return
	}

	// 2. Extract and Aggregate SEI
	var aggregatedMeta []*pb.SeiMetadata
	fileTiming := make([]TelemetryFileTiming, 0, len(frontFiles))

	for _, f := range frontFiles {
		meta, err := s.SEIExtractor(f.path)
		boundary := TelemetryFileTiming{FilePath: f.path, FirstSampleIndex: len(aggregatedMeta)}
		if err == nil && len(meta) > 0 {
			boundary.SampleCount = len(meta)
			boundary.SampleTimes, boundary.FrameDuration, _ = telemetryPacketTimes(f.path, meta)
			aggregatedMeta = append(aggregatedMeta, meta...)
		}
		fileTiming = append(fileTiming, boundary)
	}

	// 3. Marshal to JSON
	jsonData, _ := json.Marshal(aggregatedMeta)
	timingData, _ := json.Marshal(fileTiming)

	// 4. Create or Update Telemetry
	var lat, lon float64
	err := s.writeClipTelemetry(clip, func(telemetry *models.Telemetry) {
		existingLat, existingLon := telemetry.Latitude, telemetry.Longitude

		// Update fields
		telemetry.ClipID = clip.ID
		telemetry.FullDataJson = string(jsonData)
		telemetry.FileTimingJson = string(timingData)

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
		lat, lon = SelectIncidentPosition(incidentLat, incidentLon, seiLat, seiLon)
		telemetry.Latitude = lat
		telemetry.Longitude = lon

	})
	if err != nil {
		fmt.Printf("Skipping clip %d telemetry: %v\n", clip.ID, err)
		return
	}

	// 5. Update City if missing — use the selected incident point, never (0,0).
	// Tesla's own city (from event.json) always wins when already set to a real name.
	if clip.City == "" && ValidGeographicPoint(lat, lon) {
		newCity := fmt.Sprintf("%.4f, %.4f", lat, lon)
		if err := s.DB.Model(&models.Clip{}).Where("id = ?", clip.ID).Update("city", newCity).Error; err != nil {
			fmt.Printf("Clip %d city update: %v\n", clip.ID, err)
			return
		}
		clip.City = newCity
	}
	s.geocodeClipCity(clip, lat, lon)
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
