package services

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/jinzhu/gorm"
	"teslaxy/models"
)

// eventJSON holds the fields we care about from Tesla's event.json.
type eventJSON struct {
	Timestamp string
	City      string
	Reason    string
	EstLat    float64
	EstLon    float64
}

func toFloat64(v interface{}) float64 {
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

// readEventJSONFile reads and parses event.json, retrying briefly so a
// half-written create does not permanently leave empty city/reason.
func readEventJSONFile(path string) (eventJSON, error) {
	var lastErr error
	var empty eventJSON
	for attempt := 0; attempt < 3; attempt++ {
		if attempt > 0 {
			time.Sleep(500 * time.Millisecond)
		}
		content, err := os.ReadFile(path)
		if err != nil {
			lastErr = err
			fmt.Printf("event.json read error (%s attempt %d): %v\n", path, attempt+1, err)
			continue
		}
		if len(strings.TrimSpace(string(content))) == 0 {
			lastErr = fmt.Errorf("empty file")
			fmt.Printf("event.json empty (%s attempt %d)\n", path, attempt+1)
			continue
		}
		var raw struct {
			Timestamp string      `json:"timestamp"`
			City      string      `json:"city"`
			Reason    string      `json:"reason"`
			EstLat    interface{} `json:"est_lat"`
			EstLon    interface{} `json:"est_lon"`
		}
		if err := json.Unmarshal(content, &raw); err != nil {
			lastErr = err
			fmt.Printf("event.json parse error (%s attempt %d): %v\n", path, attempt+1, err)
			continue
		}
		return eventJSON{
			Timestamp: raw.Timestamp,
			City:      strings.TrimSpace(raw.City),
			Reason:    strings.TrimSpace(raw.Reason),
			EstLat:    toFloat64(raw.EstLat),
			EstLon:    toFloat64(raw.EstLon),
		}, nil
	}
	if lastErr == nil {
		lastErr = fmt.Errorf("unreadable event.json")
	}
	return empty, lastErr
}

// geocodeClipCity replaces empty or coordinate-string cities via the offline
// suburb lookup. Failures leave the coordinate string (or empty) unchanged.
func (s *ScannerService) geocodeClipCity(clip *models.Clip, lat, lon float64) {
	if clip == nil || clip.ID == 0 || s.Geocoder == nil {
		return
	}
	need := clip.City == "" || IsCoordinateCity(clip.City)
	if !need {
		return
	}
	if !ValidGeographicPoint(lat, lon) {
		return
	}
	name, err := s.Geocoder.Reverse(lat, lon)
	if err != nil || name == "" {
		return
	}
	if err := s.DB.Model(&models.Clip{}).Where("id = ?", clip.ID).Update("city", name).Error; err != nil {
		fmt.Printf("Clip %d geocode update: %v\n", clip.ID, err)
		return
	}
	clip.City = name
}

// backfillIncompleteMetadata repairs every active clip, including cities produced
// by earlier backfills. Only event.json and this clip's telemetry supply location;
// the stored city may contain stale coordinates or a stale geocoded name.
func (s *ScannerService) backfillIncompleteMetadata() {
	var after uint
	for {
		var clips []models.Clip
		if err := s.DB.Where("id > ?", after).Order("id").Limit(256).
			Preload("Telemetry", func(db *gorm.DB) *gorm.DB {
				// Do not load full playback tracks during this startup sweep.
				return db.Select("id, clip_id, latitude, longitude")
			}).Find(&clips).Error; err != nil {
			fmt.Printf("backfill metadata query error: %v\n", err)
			return
		}
		if len(clips) == 0 {
			return
		}
		for i := range clips {
			s.backfillClipMetadata(&clips[i])
		}
		after = clips[len(clips)-1].ID
	}
}

func (s *ScannerService) backfillClipMetadata(clip *models.Clip) {
	var ev eventJSON
	if clip.SourceDir != "" {
		eventPath := filepath.Join(clip.SourceDir, "event.json")
		if _, err := os.Stat(eventPath); err == nil {
			var readErr error
			ev, readErr = readEventJSONFile(eventPath)
			if readErr != nil {
				fmt.Printf("backfill: still cannot parse %s: %v\n", eventPath, readErr)
				return // Do not overwrite an authoritative city while its source is unreadable.
			}
		} else if !os.IsNotExist(err) {
			fmt.Printf("backfill: cannot stat %s: %v\n", eventPath, err)
			return
		}
	}
	updates := map[string]interface{}{}
	if clip.Reason == "" && ev.Reason != "" {
		updates["reason"] = ev.Reason
	}
	city := ev.City
	if city == "" || IsCoordinateCity(city) {
		lat, lon := ev.EstLat, ev.EstLon
		if !ValidGeographicPoint(lat, lon) {
			lat, lon = clip.Telemetry.Latitude, clip.Telemetry.Longitude
		}
		if ValidGeographicPoint(lat, lon) {
			city = fmt.Sprintf("%.4f, %.4f", lat, lon)
			if s.Geocoder != nil {
				if name, err := s.Geocoder.Reverse(lat, lon); err == nil && name != "" {
					city = name
				}
			}
		} else {
			city = "" // No trustworthy location: never interpret a stored coordinate string.
		}
	}
	if city != "" && city != clip.City {
		updates["city"] = city
	}
	if len(updates) > 0 {
		// Use a bare model: GORM v1 would otherwise auto-save the partially loaded
		// Telemetry association and erase fields not selected by this sweep.
		if err := s.DB.Model(&models.Clip{}).Where("id = ?", clip.ID).Updates(updates).Error; err != nil {
			fmt.Printf("backfill clip %d metadata update error: %v\n", clip.ID, err)
		}
	}
}
