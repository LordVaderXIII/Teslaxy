package models

import (
	"time"

	"github.com/jinzhu/gorm"
)

type Clip struct {
	gorm.Model
	Timestamp      time.Time   `json:"timestamp" gorm:"index"`
	EventTimestamp *time.Time  `json:"event_timestamp"` // Timestamp from event.json
	Event          string      `json:"event"`           // e.g., "Sentry", "Saved", "Recent"
	City           string      `json:"city"`
	VideoFiles     []VideoFile `json:"video_files"`
	TelemetryID    uint        `json:"telemetry_id"`
	Telemetry      Telemetry   `json:"telemetry"`
}

type VideoFile struct {
	gorm.Model
	ClipID   uint   `json:"clip_id" gorm:"index"`
	Camera   string `json:"camera"` // "Front", "Left Repeater", etc.
	FilePath string `json:"file_path"`
}

type Telemetry struct {
	gorm.Model
	ClipID         uint    `json:"clip_id" gorm:"index"`
	Speed          float32 `json:"speed"`
	Gear           string  `json:"gear"`
	Latitude       float64 `json:"latitude"`
	Longitude      float64 `json:"longitude"`
	SteeringAngle  float32 `json:"steering_angle"`
	AutopilotState string  `json:"autopilot_state"`
	FullDataJson   string  `json:"full_data_json"` // Store full protobuf dump if needed
}
