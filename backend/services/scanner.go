package services

import (
	"encoding/json"
	"fmt"
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

func (s *ScannerService) processEventGroup(dirPath string, filePaths []string) {
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

				if city == "" && (estLat != 0 || estLon != 0) {
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

	// Create or Find ONE clip for this folder (using minTime as key for now,
	// though strictly we might want a unique key per folder.
	// But usually a folder starts at minTime so it's consistent).
	var clip models.Clip
	if err := s.DB.Where("timestamp = ? AND event = ?", minTime, eventType).First(&clip).Error; err != nil {
		if gorm.IsRecordNotFoundError(err) {
			clip = models.Clip{
				Timestamp:      minTime,
				Event:          eventType,
				City:           city,
				Reason:         eventReason,
				EventTimestamp: eventJsonTimestamp, // Set directly for the folder-based clip
			}
			s.DB.Create(&clip)
		}
	} else {
		// Update existing
		updates := map[string]interface{}{}
		if clip.EventTimestamp == nil && eventJsonTimestamp != nil {
			updates["event_timestamp"] = eventJsonTimestamp
		}
		if city != "" {
			updates["city"] = city
		}
		if eventReason != "" {
			updates["reason"] = eventReason
		}
		if len(updates) > 0 {
			s.DB.Model(&clip).Updates(updates)
		}
	}

	// Fallback Telemetry setup
	if clip.TelemetryID == 0 && (estLat != 0 || estLon != 0) {
		telemetry := models.Telemetry{
			ClipID:    clip.ID,
			Latitude:  estLat,
			Longitude: estLon,
		}
		if err := s.DB.Create(&telemetry).Error; err == nil {
			s.DB.Model(&clip).Update("telemetry_id", telemetry.ID)
		}
	}

	// Add ALL files in the directory to this single clip
	s.addFilesToClip(clip, files)

	// Aggregate Telemetry (will process all front files sorted by time)
	s.aggregateTelemetry(&clip, files)
}

func (s *ScannerService) processRecentGroup(filePaths []string) {
	if len(filePaths) == 0 {
		return
	}

	timezone := determineTimezone(0, 0)

	var files []fileInfo
	for _, path := range filePaths {
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
		if err := s.DB.Table("video_files").
			Joins("JOIN clips ON video_files.clip_id = clips.id").
			Where("clips.event = ? AND video_files.timestamp >= ? AND video_files.timestamp < ?", "Recent", startTime, minTime).
			Order("video_files.timestamp desc").
			First(&lastVf).Error; err == nil {

			s.DB.First(&clip, lastVf.ClipID)
			found = true
		} else if err := s.DB.Where("timestamp = ?", minTime).First(&clip).Error; err == nil {
			// Idempotency: found the clip itself (maybe we are reprocessing)
			found = true
		}

		if !found {
			clip = models.Clip{
				Timestamp: minTime,
				Event:     "Recent",
			}
			s.DB.Create(&clip)
		}

		// Flatten the clip group to get all files for telemetry aggregation
		var allFiles []fileInfo

		// Add ALL segments in this continuous block to the clip
		for _, segment := range clipGroup {
			s.addFilesToClip(clip, segment)
			allFiles = append(allFiles, segment...)
		}

		// Aggregate Telemetry
		s.aggregateTelemetry(&clip, allFiles)
	}
}

func (s *ScannerService) addFilesToClip(clip models.Clip, files []fileInfo) {
	// Optimization: Bulk fetch existing files for this clip to avoid N+1 queries
	var existingPaths []string
	s.DB.Model(&models.VideoFile{}).Where("clip_id = ?", clip.ID).Pluck("file_path", &existingPaths)

	existingMap := make(map[string]bool)
	for _, p := range existingPaths {
		existingMap[p] = true
	}

	for _, f := range files {
		if existingMap[f.path] {
			continue
		}

		matches := fileRegex.FindStringSubmatch(filepath.Base(f.path))
		cameraName := "Unknown"
		if len(matches) == 3 {
			cameraName = matches[2] // This is group 2 now in the new regex
		}
		cameraName = normalizeCameraName(cameraName)

		vf := models.VideoFile{
			ClipID:    clip.ID,
			Camera:    cameraName,
			FilePath:  f.path,
			Timestamp: f.timestamp,
		}
		if err := s.DB.Create(&vf).Error; err == nil {
			existingMap[f.path] = true
		}
	}
}

// aggregateTelemetry iterates through all 'Front' files in the clip, extracts SEI, and updates the Telemetry record.
func (s *ScannerService) aggregateTelemetry(clip *models.Clip, files []fileInfo) {
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

	// Update fields
	telemetry.ClipID = clip.ID
	telemetry.FullDataJson = string(jsonData)

	// Update summary fields from the middle of the *entire* clip (approx)
	mid := len(aggregatedMeta) / 2
	if mid < len(aggregatedMeta) {
		m := aggregatedMeta[mid]
		telemetry.Speed = m.VehicleSpeedMps * 2.23694
		telemetry.Gear = m.GearState.String()
		telemetry.Latitude = m.LatitudeDeg
		telemetry.Longitude = m.LongitudeDeg
		telemetry.SteeringAngle = m.SteeringWheelAngle
		telemetry.AutopilotState = m.AutopilotState.String()
	}

	if telemetry.ID != 0 {
		s.DB.Save(&telemetry)
	} else {
		s.DB.Create(&telemetry)
		s.DB.Model(clip).Update("telemetry_id", telemetry.ID)
	}

	// 5. Update City if missing
	if clip.City == "" && (telemetry.Latitude != 0 || telemetry.Longitude != 0) {
		newCity := fmt.Sprintf("%.4f, %.4f", telemetry.Latitude, telemetry.Longitude)
		s.DB.Model(clip).Update("city", newCity)
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
