package services

import (
	"encoding/json"
	"fmt"
	"os"
	"strconv"
	"path/filepath"
	"regexp"
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
	pendingFiles map[string][]string
	timers       map[string]*time.Timer
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

	// Also watch subdirectories (RecentClips, etc) - fsnotify is not recursive by default on some platforms,
	// but we can walk and add.
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
		ts := matches[1]

		s.mu.Lock()
		// Add to pending files if not already present
		exists := false
		for _, f := range s.pendingFiles[ts] {
			if f == path {
				exists = true
				break
			}
		}
		if !exists {
			s.pendingFiles[ts] = append(s.pendingFiles[ts], path)
		}

		// Debounce: reset timer
		if t, ok := s.timers[ts]; ok {
			t.Stop()
		}
		s.timers[ts] = time.AfterFunc(2*time.Second, func() {
			s.processPending(ts)
		})
		s.mu.Unlock()

		fmt.Println("New file detected, queued for processing:", filename)
		return
	}

	// Case 2: event.json
	// If event.json appears, we should ensure the clip is updated with event info
	if strings.ToLower(filename) == "event.json" {
		fmt.Println("event.json detected:", path)
		// Scan the directory for any mp4s to update their metadata
		dir := filepath.Dir(path)
		go s.scanDir(dir)
	}
}

func (s *ScannerService) processPending(timestamp string) {
	s.mu.Lock()
	files, ok := s.pendingFiles[timestamp]
	if !ok {
		s.mu.Unlock()
		return
	}
	// Cleanup
	delete(s.pendingFiles, timestamp)
	delete(s.timers, timestamp)
	s.mu.Unlock()

	fmt.Printf("Processing incremental update for timestamp %s with %d files\n", timestamp, len(files))
	s.processClipGroup(timestamp, files)
}

func (s *ScannerService) scanDir(dirPath string) {
	clipMap := make(map[string][]string)

	files, err := os.ReadDir(dirPath)
	if err != nil {
		fmt.Println("Error reading dir:", err)
		return
	}

	for _, f := range files {
		if !f.IsDir() && strings.HasSuffix(f.Name(), ".mp4") {
			matches := fileRegex.FindStringSubmatch(f.Name())
			if len(matches) == 3 {
				ts := matches[1]
				clipMap[ts] = append(clipMap[ts], filepath.Join(dirPath, f.Name()))
			}
		}
	}

	for ts, filePaths := range clipMap {
		s.processClipGroup(ts, filePaths)
	}
}

func (s *ScannerService) ScanAll() {
	fmt.Println("Starting full scan of", s.FootagePath)
	start := time.Now()

	// Map to group files by timestamp
	// Key: Timestamp string (e.g., "2019-01-21_14-15-20")
	// Value: List of file paths
	clipMap := make(map[string][]string)

	err := filepath.Walk(s.FootagePath, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		if !info.IsDir() && strings.HasSuffix(info.Name(), ".mp4") {
			matches := fileRegex.FindStringSubmatch(info.Name())
			if len(matches) == 3 {
				ts := matches[1]
				clipMap[ts] = append(clipMap[ts], path)
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
	// Limit concurrency
	semaphore := make(chan struct{}, 5)

	for ts, files := range clipMap {
		wg.Add(1)
		semaphore <- struct{}{}
		go func(timestamp string, filePaths []string) {
			defer wg.Done()
			defer func() { <-semaphore }()
			s.processClipGroup(timestamp, filePaths)
		}(ts, files)
	}

	wg.Wait()
	fmt.Printf("Scan complete in %v. Processed %d clips.\n", time.Since(start), len(clipMap))
}

func (s *ScannerService) processClipGroup(timestampStr string, filePaths []string) {
	// Parse timestamp
	// Tesla format: YYYY-MM-DD_HH-MM-SS
	t, err := time.Parse("2006-01-02_15-04-05", timestampStr)
	if err != nil {
		fmt.Println("Error parsing timestamp:", timestampStr, err)
		return
	}

	// Determine event type based on folder path of first file
	// e.g. /footage/SentryClips/..., /footage/SavedClips/...
	eventType := "Recent"
	if len(filePaths) > 0 {
		if strings.Contains(filePaths[0], "SentryClips") {
			eventType = "Sentry"
		} else if strings.Contains(filePaths[0], "SavedClips") {
			eventType = "Saved"
		}
	}

	// Check for event.json in the same directory (assuming filePaths[0] is valid)
	var eventTimestamp *time.Time
	var city string
	if len(filePaths) > 0 {
		dir := filepath.Dir(filePaths[0])
		// Check case-insensitive existence or just check event.json
		// Standard Tesla is lowercase event.json
		eventJsonPath := filepath.Join(dir, "event.json")
		if _, err := os.Stat(eventJsonPath); err == nil {
			// Found event.json
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

					// Helper to safely convert interface{} to float64
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

					lat := toFloat(eventData.EstLat)
					lon := toFloat(eventData.EstLon)

					// Fallback: If City is empty, use coordinates
					if city == "" && (lat != 0 || lon != 0) {
						city = fmt.Sprintf("%.4f, %.4f", lat, lon)
					}

					// Parse event timestamp
					// Tesla JSON timestamp format: 2023-10-27T10:00:30 (sometimes with milliseconds)
					// Try ISO formats
					if parsed, err := time.Parse("2006-01-02T15:04:05", eventData.Timestamp); err == nil {
						eventTimestamp = &parsed
					} else if parsed, err := time.Parse(time.RFC3339, eventData.Timestamp); err == nil {
						eventTimestamp = &parsed
					}
				}
			}
		}
	}

	// Create Clip record if not exists
	var clip models.Clip
	if err := s.DB.Where("timestamp = ?", t).First(&clip).Error; err != nil {
		if gorm.IsRecordNotFoundError(err) {
			clip = models.Clip{
				Timestamp:      t,
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
		// Update existing clip if event info was missing
		if clip.EventTimestamp == nil && eventTimestamp != nil {
			s.DB.Model(&clip).Update("event_timestamp", eventTimestamp)
		}
		if clip.City == "" && city != "" {
			s.DB.Model(&clip).Update("city", city)
		}
	}

	// Process video files
	for _, path := range filePaths {
		// Determine camera
		matches := fileRegex.FindStringSubmatch(filepath.Base(path))
		cameraName := "Unknown"
		if len(matches) == 3 {
			cameraName = matches[2]
		}

		// Normalize camera name
		// front, back, left_repeater, right_repeater, etc.
		cameraName = normalizeCameraName(cameraName)

		// Check if video file exists in DB
		var vf models.VideoFile
		if err := s.DB.Where("clip_id = ? AND camera = ?", clip.ID, cameraName).First(&vf).Error; gorm.IsRecordNotFoundError(err) {
			vf = models.VideoFile{
				ClipID:   clip.ID,
				Camera:   cameraName,
				FilePath: path,
			}
			s.DB.Create(&vf)

			// If it's the front camera, try to extract telemetry
			// Only valid for new inserts to avoid re-processing
			if cameraName == "Front" {
				// Process telemetry in background to not block
				// But we are already in a goroutine
				meta, err := s.SEIExtractor(path)
				if err == nil && len(meta) > 0 {
					// Store first valid metadata frame or aggregate?
					// Storing everything might be too heavy for SQLite.
					// Let's store a representative sample or the full JSON string.
					// The requirement says "extract... and save to DB".
					// Ideally we want per-second data for the overlay.
					// For now, let's store the first frame to prove it works,
					// or maybe store a simplified JSON array.

					// Let's just store the full thing as JSON for now.
					// It might be large. A 1 min clip at 30fps = 1800 entries.
					// Maybe just store it.

					jsonData, _ := json.Marshal(meta) // This might be huge

					// Optimization: only store 1 sample per second?
					// Or just store it. SQLite can handle blobs.

					// Create Telemetry record
					telemetry := models.Telemetry{
						ClipID:       clip.ID,
						FullDataJson: string(jsonData),
						// Populate summary fields from the middle of the clip?
					}

					// Set summary fields from a frame in the middle
					mid := len(meta) / 2
					if mid < len(meta) {
						m := meta[mid]
						telemetry.Speed = m.VehicleSpeedMps * 2.23694 // mps to mph approx
						telemetry.Gear = m.GearState.String()
						telemetry.Latitude = m.LatitudeDeg
						telemetry.Longitude = m.LongitudeDeg
						telemetry.SteeringAngle = m.SteeringWheelAngle
						telemetry.AutopilotState = m.AutopilotState.String()
					}

					s.DB.Create(&telemetry)

					// Update clip reference
					s.DB.Model(&clip).Update("telemetry_id", telemetry.ID)

					// Fallback: If Clip City is empty, use telemetry coordinates
					if clip.City == "" && (telemetry.Latitude != 0 || telemetry.Longitude != 0) {
						newCity := fmt.Sprintf("%.4f, %.4f", telemetry.Latitude, telemetry.Longitude)
						s.DB.Model(&clip).Update("city", newCity)
						clip.City = newCity
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

// Convert SEI extraction to work with protobuf
func ConvertSEIToModel(pbMeta *pb.SeiMetadata) models.Telemetry {
	// helper if needed
	return models.Telemetry{}
}
