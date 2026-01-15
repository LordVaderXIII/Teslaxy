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
	EventTimestamp *time.Time  `json:"event_timestamp"` // Timestamp from event.json
	Event          string      `json:"event"`           // e.g., "Sentry", "Saved", "Recent"
	City           string      `json:"city"`
	Reason         string      `json:"reason"`
	VideoFiles     []VideoFile `json:"video_files"`
	TelemetryID    uint        `json:"telemetry_id"`
	Telemetry      Telemetry   `json:"telemetry"`
}

type VideoFile struct {
	ID        uint       `gorm:"primary_key" json:"ID"`
	CreatedAt time.Time  `json:"-"`
	UpdatedAt time.Time  `json:"-"`
	DeletedAt *time.Time `sql:"index" json:"-"`

	ClipID    uint      `json:"clip_id" gorm:"index:idx_vf_clip_tmst"`
	Camera    string    `json:"camera"` // "Front", "Left Repeater", etc.
	FilePath  string    `json:"file_path"`
	Timestamp time.Time `json:"timestamp" gorm:"index;index:idx_vf_clip_tmst"`
}

type Telemetry struct {
	ID        uint       `gorm:"primary_key" json:"ID"`
	CreatedAt time.Time  `json:"-"`
	UpdatedAt time.Time  `json:"-"`
	DeletedAt *time.Time `sql:"index" json:"-"`

	ClipID         uint    `json:"clip_id" gorm:"index"`
	Speed          float32 `json:"speed"`
	Gear           string  `json:"gear"`
	Latitude       float64 `json:"latitude"`
	Longitude      float64 `json:"longitude"`
	SteeringAngle  float32 `json:"steering_angle"`
	AutopilotState string  `json:"autopilot_state"`
	FullDataJson   string  `json:"full_data_json"` // Store full protobuf dump if needed
}
