package services

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/jinzhu/gorm"
	"teslaxy/models"
)

// IncidentRepairCandidate is a dry-run row for clips whose SourceDir is set.
// Tests should assert flags, not log stored lat/lon beyond synthetic fixtures.
type IncidentRepairCandidate struct {
	ClipID                 uint
	HasValidEventPoint     bool
	StoredIsZero           bool
	StoredDiffersFromEvent bool
}

type eventJSONStatus int

const (
	eventJSONOK eventJSONStatus = iota
	eventJSONMissing
	eventJSONInvalid
)

func eventJSONPoint(path string) (lat, lon float64, status eventJSONStatus) {
	content, err := os.ReadFile(path)
	if err != nil {
		return 0, 0, eventJSONMissing
	}
	var eventData struct {
		EstLat interface{} `json:"est_lat"`
		EstLon interface{} `json:"est_lon"`
	}
	if err := json.Unmarshal(content, &eventData); err != nil {
		return 0, 0, eventJSONInvalid
	}
	toFloat := func(v interface{}) float64 {
		switch val := v.(type) {
		case float64:
			return val
		case json.Number:
			f, _ := val.Float64()
			return f
		case string:
			f, _ := strconv.ParseFloat(val, 64)
			return f
		default:
			return 0
		}
	}
	lat = toFloat(eventData.EstLat)
	lon = toFloat(eventData.EstLon)
	if !ValidGeographicPoint(lat, lon) {
		return lat, lon, eventJSONInvalid
	}
	return lat, lon, eventJSONOK
}

func resolveFootageRoot(footageRoot string) (string, error) {
	cleaned := filepath.Clean(footageRoot)
	abs, err := filepath.Abs(cleaned)
	if err != nil {
		return "", err
	}
	if _, err := os.Stat(abs); err == nil {
		real, err := filepath.EvalSymlinks(abs)
		if err != nil {
			return "", err
		}
		return real, nil
	}
	return abs, nil
}

func pathInsideRoot(p, root string) bool {
	p = filepath.Clean(p)
	root = filepath.Clean(root)
	if p == root {
		return true
	}
	return strings.HasPrefix(p, root+string(os.PathSeparator))
}

// containedEventJSONPath resolves SourceDir against footageRoot and returns
// the lexical event.json path only when the real location stays inside the root.
// Callers must not os.ReadFile when ok is false.
func containedEventJSONPath(footageRoot, sourceDir string) (eventPath string, ok bool) {
	if sourceDir == "" {
		return "", false
	}

	var candidate string
	if filepath.IsAbs(sourceDir) {
		candidate = filepath.Clean(sourceDir)
	} else {
		candidate = filepath.Join(footageRoot, sourceDir)
	}
	absCandidate, err := filepath.Abs(candidate)
	if err != nil {
		return "", false
	}
	if !pathInsideRoot(absCandidate, footageRoot) {
		return "", false
	}

	eventPath = filepath.Join(absCandidate, "event.json")
	probe := filepath.Dir(eventPath)
	if _, err := os.Lstat(eventPath); err == nil {
		probe = eventPath
	}

	realPath := probe
	if _, err := os.Lstat(probe); err == nil {
		evaled, err := filepath.EvalSymlinks(probe)
		if err != nil {
			return "", false
		}
		realPath = evaled
	}
	if !pathInsideRoot(realPath, footageRoot) {
		return "", false
	}
	return eventPath, true
}

// CountIncidentCoordinateIssues is a read-only inventory of Clip rows with SourceDir set.
// It never Update/Save/Creates telemetry or clips.
//
//   - nEventBacked: event.json has a valid geographic point
//   - nWouldChange: event-backed and stored telemetry is missing, zero, or a different point
//   - nUnrepairable: footageRoot is empty, path escapes the root, event.json is missing,
//     or event.json is present but invalid / non-JSON (do not guess, do not silent-skip)
func CountIncidentCoordinateIssues(db *gorm.DB, footageRoot string) (nEventBacked, nWouldChange, nUnrepairable int, err error) {
	var clips []models.Clip
	if err = db.Where("source_dir != ?", "").Find(&clips).Error; err != nil {
		return 0, 0, 0, err
	}

	if footageRoot == "" {
		return 0, 0, len(clips), nil
	}

	root, resolveErr := resolveFootageRoot(footageRoot)
	if resolveErr != nil {
		return 0, 0, len(clips), nil
	}

	for _, clip := range clips {
		eventPath, contained := containedEventJSONPath(root, clip.SourceDir)
		if !contained {
			nUnrepairable++
			continue
		}

		eventLat, eventLon, status := eventJSONPoint(eventPath)
		if status != eventJSONOK {
			nUnrepairable++
			continue
		}

		storedLat, storedLon := 0.0, 0.0
		hasTelemetry := false
		if clip.TelemetryID != 0 {
			var tel models.Telemetry
			if db.First(&tel, clip.TelemetryID).Error == nil {
				hasTelemetry = true
				storedLat, storedLon = tel.Latitude, tel.Longitude
			}
		}

		cand := IncidentRepairCandidate{
			ClipID:             clip.ID,
			HasValidEventPoint: true,
			StoredIsZero:       !hasTelemetry || (storedLat == 0 && storedLon == 0),
			StoredDiffersFromEvent: hasTelemetry &&
				ValidGeographicPoint(storedLat, storedLon) &&
				(storedLat != eventLat || storedLon != eventLon),
		}
		if cand.HasValidEventPoint {
			nEventBacked++
		}
		if cand.StoredIsZero || cand.StoredDiffersFromEvent || (hasTelemetry && (storedLat != eventLat || storedLon != eventLon)) {
			nWouldChange++
		}
	}

	return nEventBacked, nWouldChange, nUnrepairable, nil
}
