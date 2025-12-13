package services

import (
	"encoding/json"
	"fmt"
	"os"
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

// ScannerService handles directory scanning and file watching.
type ScannerService struct {
	FootagePath string
	DB          *gorm.DB
	Watcher     *fsnotify.Watcher
}

var (
	// Tesla file format: 2019-01-21_14-15-20-front.mp4
	fileRegex = regexp.MustCompile(`(\d{4}-\d{2}-\d{2}_\d{2}-\d{2}-\d{2})-([a-zA-Z0-9_-]+)\.mp4$`)
)

func NewScannerService(footagePath string, db *gorm.DB) *ScannerService {
	return &ScannerService{
		FootagePath: footagePath,
		DB:          db,
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
					// Handle new file
					// Debounce or process immediately?
					// For now, minimal processing, maybe trigger a rescan of that folder or parsing.
					// Ideally we wait for the set of files (front, back, left, right) to arrive.
					// But they might not arrive at exact same millisecond.
					// Simpler approach: Just re-scan the folder or specific timestamp?
					fmt.Println("New file detected:", event.Name)
					// TODO: incremental update
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
		eventJsonPath := filepath.Join(dir, "event.json")
		if _, err := os.Stat(eventJsonPath); err == nil {
			// Found event.json
			content, err := os.ReadFile(eventJsonPath)
			if err == nil {
				var eventData struct {
					Timestamp string `json:"timestamp"`
					City      string `json:"city"`
					Reason    string `json:"reason"`
				}
				if err := json.Unmarshal(content, &eventData); err == nil {
					city = eventData.City
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
				meta, err := ExtractSEI(path)
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
