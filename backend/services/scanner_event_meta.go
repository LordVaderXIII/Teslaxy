package services

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

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
	if clip == nil || s.Geocoder == nil {
		return
	}
	need := clip.City == "" || IsCoordinateCity(clip.City)
	if !need {
		return
	}
	if !ValidGeographicPoint(lat, lon) {
		// Try parsing the coordinate-string city itself.
		if IsCoordinateCity(clip.City) {
			var parseLat, parseLon float64
			if _, err := fmt.Sscanf(clip.City, "%f, %f", &parseLat, &parseLon); err == nil {
				lat, lon = parseLat, parseLon
			}
		}
	}
	if !ValidGeographicPoint(lat, lon) {
		return
	}
	name, err := s.Geocoder.Reverse(lat, lon)
	if err != nil || name == "" {
		return
	}
	s.DB.Model(clip).Update("city", name)
	clip.City = name
}

// backfillIncompleteMetadata re-reads event.json for Sentry/Saved clips with
// empty city or reason, and geocodes any coordinate-looking cities. Called
// once at the end of ScanAll (startup / full rescan).
func (s *ScannerService) backfillIncompleteMetadata() {
	var eventClips []models.Clip
	if err := s.DB.Where(
		"(event = ? OR event = ?) AND source_dir != '' AND source_dir IS NOT NULL AND (city = '' OR city IS NULL OR reason = '' OR reason IS NULL)",
		"Sentry", "Saved",
	).Find(&eventClips).Error; err != nil {
		fmt.Printf("backfill event metadata query error: %v\n", err)
	} else {
		for i := range eventClips {
			s.backfillOneEventClip(&eventClips[i])
		}
	}

	var coordClips []models.Clip
	// SQLite: pull candidates with a comma in city, then regex-filter in Go.
	if err := s.DB.Where("city LIKE '%,%'").Find(&coordClips).Error; err != nil {
		fmt.Printf("backfill coordinate city query error: %v\n", err)
		return
	}
	for i := range coordClips {
		clip := &coordClips[i]
		if !IsCoordinateCity(clip.City) {
			continue
		}
		var lat, lon float64
		if _, err := fmt.Sscanf(clip.City, "%f, %f", &lat, &lon); err != nil {
			continue
		}
		s.geocodeClipCity(clip, lat, lon)
	}
}

func (s *ScannerService) backfillOneEventClip(clip *models.Clip) {
	if clip == nil || clip.SourceDir == "" {
		return
	}
	eventPath := filepath.Join(clip.SourceDir, "event.json")
	if _, err := os.Stat(eventPath); err != nil {
		return
	}
	ev, err := readEventJSONFile(eventPath)
	if err != nil {
		fmt.Printf("backfill: still cannot parse %s: %v\n", eventPath, err)
		return
	}
	updates := map[string]interface{}{}
	if clip.City == "" && ev.City != "" {
		updates["city"] = ev.City
		clip.City = ev.City
	}
	if clip.Reason == "" && ev.Reason != "" {
		updates["reason"] = ev.Reason
		clip.Reason = ev.Reason
	}
	if clip.City == "" && ValidGeographicPoint(ev.EstLat, ev.EstLon) {
		coordCity := fmt.Sprintf("%.4f, %.4f", ev.EstLat, ev.EstLon)
		updates["city"] = coordCity
		clip.City = coordCity
	}
	if len(updates) > 0 {
		s.DB.Model(clip).Updates(updates)
	}
	s.geocodeClipCity(clip, ev.EstLat, ev.EstLon)
}
