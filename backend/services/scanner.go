package services

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/fsnotify/fsnotify"
	"github.com/jinzhu/gorm"
	"teslaxy/models"
	pb "teslaxy/proto"
)

// SEIExtractor is a function type for extracting SEI metadata.
type SEIExtractor func(path string) ([]*pb.SeiMetadata, error)

// ScannerService handles directory scanning and file watching.
type ScannerService struct {
	FootagePath string
	DB          *gorm.DB
	Watcher     *fsnotify.Watcher
	SEIExtractor SEIExtractor

	// Incremental update state
	mu           sync.Mutex
	pendingFiles map[string][]string // Key is Directory Path
	timers       map[string]*time.Timer // Key is Directory Path
}

var (
	// Tesla file format: 2019-01-21_14-15-20-front.mp4
	fileRegex = regexp.MustCompile(`(\d{4}-\d{2}-\d{2}_\d{2}-\d{2}-\d{2})-([a-zA-Z0-9_-]+)\.mp4$`)
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
	if matches := fileRegex.FindStringSubmatch(filename); len(matches) == 3 {
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
	// We should probably re-scan the whole directory to be safe and ensure we have all files
	// creating the complete event structure.
	s.scanDir(dirPath)
}

func (s *ScannerService) scanDir(dirPath string) {
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
		s.processClipGroup(dirPath, filePaths)
	}
}

func (s *ScannerService) ScanAll() {
	fmt.Println("Starting full scan of", s.FootagePath)
	start := time.Now()

	// Map to group files by Directory
	clipMap := make(map[string][]string)

	err := filepath.Walk(s.FootagePath, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		if !info.IsDir() && strings.HasSuffix(info.Name(), ".mp4") {
			if fileRegex.MatchString(info.Name()) {
				dir := filepath.Dir(path)
				clipMap[dir] = append(clipMap[dir], path)
			}
		}
		return nil
	})

	if err != nil {
		fmt.Println("Error walking path:", err)
		return
	}

	// Process groups
	var wg sync.WaitGroup
	semaphore := make(chan struct{}, 5)

	for dir, files := range clipMap {
		wg.Add(1)
		semaphore <- struct{}{}
		go func(d string, f []string) {
			defer wg.Done()
			defer func() { <-semaphore }()
			s.processClipGroup(d, f)
		}(dir, files)
	}

	wg.Wait()
	fmt.Printf("Scan complete in %v. Processed %d events.\n", time.Since(start), len(clipMap))
}

func (s *ScannerService) processClipGroup(dirPath string, filePaths []string) {
	if len(filePaths) == 0 {
		return
	}

	// 1. Determine Clip Timestamp (earliest file timestamp)
	var minTime time.Time
	var minSet = false

	// Also gather individual file timestamps
	fileTimestamps := make(map[string]time.Time)

	for _, path := range filePaths {
		matches := fileRegex.FindStringSubmatch(filepath.Base(path))
		if len(matches) == 3 {
			// Parse timestamp
			t, err := time.Parse("2006-01-02_15-04-05", matches[1])
			if err == nil {
				fileTimestamps[path] = t
				if !minSet || t.Before(minTime) {
					minTime = t
					minSet = true
				}
			}
		}
	}

	if !minSet {
		fmt.Println("Could not determine timestamp for clip in:", dirPath)
		return
	}

	// 2. Determine Event Type
	eventType := "Recent"
	if strings.Contains(dirPath, "SentryClips") {
		eventType = "Sentry"
	} else if strings.Contains(dirPath, "SavedClips") {
		eventType = "Saved"
	}

	// 3. Check for event.json
	var eventTimestamp *time.Time
	var city string
	var estLat, estLon float64

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

				if parsed, err := time.Parse("2006-01-02T15:04:05", eventData.Timestamp); err == nil {
					eventTimestamp = &parsed
				} else if parsed, err := time.Parse(time.RFC3339, eventData.Timestamp); err == nil {
					eventTimestamp = &parsed
				}
			}
		}
	}

	// 4. Create or Update Clip
	var clip models.Clip
	if err := s.DB.Where("timestamp = ?", minTime).First(&clip).Error; err != nil {
		if gorm.IsRecordNotFoundError(err) {
			clip = models.Clip{
				Timestamp:      minTime,
				Event:          eventType,
				EventTimestamp: eventTimestamp,
				City:           city,
			}
			s.DB.Create(&clip)
		} else {
			fmt.Println("DB Error:", err)
			return
		}
	} else {
		// Update existing
		updates := map[string]interface{}{}
		if clip.EventTimestamp == nil && eventTimestamp != nil {
			updates["event_timestamp"] = eventTimestamp
		}
		if city != "" {
			updates["city"] = city
		}
		if len(updates) > 0 {
			s.DB.Model(&clip).Updates(updates)
		}
	}

	// 5. Create Fallback Telemetry
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

	// 6. Process Video Files
	for _, path := range filePaths {
		matches := fileRegex.FindStringSubmatch(filepath.Base(path))
		cameraName := "Unknown"
		if len(matches) == 3 {
			cameraName = matches[2]
		}
		cameraName = normalizeCameraName(cameraName)
		fileTs, hasTs := fileTimestamps[path]

		var vf models.VideoFile
		// Check if file exists in DB
		if err := s.DB.Where("clip_id = ? AND camera = ? AND file_path = ?", clip.ID, cameraName, path).First(&vf).Error; gorm.IsRecordNotFoundError(err) {
			vf = models.VideoFile{
				ClipID:   clip.ID,
				Camera:   cameraName,
				FilePath: path,
			}
			if hasTs {
				vf.Timestamp = fileTs
			}
			s.DB.Create(&vf)

			// Telemetry extraction (only for Front camera, and maybe just the first one?)
			if cameraName == "Front" && clip.TelemetryID == 0 {
				meta, err := s.SEIExtractor(path)
				if err == nil && len(meta) > 0 {
					jsonData, _ := json.Marshal(meta)
					telemetry := models.Telemetry{
						ClipID:       clip.ID,
						FullDataJson: string(jsonData),
					}
					mid := len(meta) / 2
					if mid < len(meta) {
						m := meta[mid]
						telemetry.Speed = m.VehicleSpeedMps * 2.23694
						telemetry.Gear = m.GearState.String()
						telemetry.Latitude = m.LatitudeDeg
						telemetry.Longitude = m.LongitudeDeg
						telemetry.SteeringAngle = m.SteeringWheelAngle
						telemetry.AutopilotState = m.AutopilotState.String()
					}
					s.DB.Create(&telemetry)
					s.DB.Model(&clip).Update("telemetry_id", telemetry.ID)
					if clip.City == "" && (telemetry.Latitude != 0 || telemetry.Longitude != 0) {
						newCity := fmt.Sprintf("%.4f, %.4f", telemetry.Latitude, telemetry.Longitude)
						s.DB.Model(&clip).Update("city", newCity)
					}
				}
			}
		}
	}
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
