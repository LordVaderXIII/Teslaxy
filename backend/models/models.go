package models

import (
	"time"
)

type Clip struct {
	ID        uint       `gorm:"primary_key" json:"ID"`
	CreatedAt time.Time  `json:"-"`
	UpdatedAt time.Time  `json:"-"`
	DeletedAt *time.Time `sql:"index" json:"-"`

	Timestamp      time.Time   `json:"timestamp" gorm:"index"`
	EventTimestamp *time.Time  `json:"event_timestamp"` // Timestamp from event.json (primary source of truth for event start)
	Event          string      `json:"event"`           // e.g., "Sentry", "Saved", "Recent"
	City           string      `json:"city"`
	Reason         string      `json:"reason"`

	// SourceDir is the stable identifier for a logical Tesla event.
	// For SentryClips/SavedClips: the full path to the event directory (contains event.json)
	// For Recent: a synthetic key based on the first segment's directory + time bucket.
	// This field makes the backend the source of truth for "which 1-min files belong together".
	SourceDir  string      `json:"source_dir,omitempty" gorm:"index"`
	VideoFiles []VideoFile `json:"video_files"`
	TelemetryID    uint        `json:"-"`
	Telemetry      Telemetry   `json:"telemetry"`
}

type VideoFile struct {
	ID        uint       `gorm:"primary_key" json:"ID"`
	CreatedAt time.Time  `json:"-"`
	UpdatedAt time.Time  `json:"-"`
	DeletedAt *time.Time `sql:"index" json:"-"`

	ClipID    uint      `json:"-" gorm:"index"`
	Camera    string    `json:"camera"` // "Front", "Left Repeater", etc.
	FilePath  string    `json:"file_path"`
	Timestamp time.Time `json:"timestamp" gorm:"index"`
}

type Telemetry struct {
	ID        uint       `gorm:"primary_key" json:"ID"`
	CreatedAt time.Time  `json:"-"`
	UpdatedAt time.Time  `json:"-"`
	DeletedAt *time.Time `sql:"index" json:"-"`

	ClipID         uint    `json:"-" gorm:"index"`
	Speed          float32 `json:"speed"`
	Gear           string  `json:"gear"`
	Latitude       float64 `json:"latitude"`
	Longitude      float64 `json:"longitude"`
	SteeringAngle  float32 `json:"steering_angle"`
	AutopilotState string  `json:"autopilot_state"`
	FullDataJson   string  `json:"full_data_json"` // Store full protobuf dump if needed
}
