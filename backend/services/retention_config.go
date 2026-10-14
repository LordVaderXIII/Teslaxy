package services

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"
)

const (
	DefaultRetentionMaxAgeDays     = 90
	DefaultRetentionMaxSizeGB      = 300
	DefaultRetentionIntervalHours  = 24
	DefaultRetentionMinFileAgeMins = 15
	RetentionSettingsFileName      = "retention.json"
)

// RetentionConfig controls footage pruning.
// Defaults: 90 days and ~300 GB. Env vars bootstrap; settings file overrides per-field.
type RetentionConfig struct {
	Enabled           bool    `json:"enabled"`
	DryRun            bool    `json:"dry_run"`
	MaxAgeDays        int     `json:"max_age_days"`
	MaxSizeGB         float64 `json:"max_size_gb"`
	IntervalHours     int     `json:"interval_hours"`
	MinFileAgeMinutes int     `json:"min_file_age_minutes"`
	FootagePath       string  `json:"footage_path,omitempty"`
}

// retentionConfigPatch supports partial JSON overlays from retention.json.
type retentionConfigPatch struct {
	Enabled           *bool    `json:"enabled"`
	DryRun            *bool    `json:"dry_run"`
	MaxAgeDays        *int     `json:"max_age_days"`
	MaxSizeGB         *float64 `json:"max_size_gb"`
	IntervalHours     *int     `json:"interval_hours"`
	MinFileAgeMinutes *int     `json:"min_file_age_minutes"`
	FootagePath       *string  `json:"footage_path"`
}

// DefaultRetentionConfig returns safe defaults.
// Retention is disabled and dry-run is on until the operator opts in.
func DefaultRetentionConfig() RetentionConfig {
	return RetentionConfig{
		Enabled:           false,
		DryRun:            true,
		MaxAgeDays:        DefaultRetentionMaxAgeDays,
		MaxSizeGB:         DefaultRetentionMaxSizeGB,
		IntervalHours:     DefaultRetentionIntervalHours,
		MinFileAgeMinutes: DefaultRetentionMinFileAgeMins,
	}
}

// LoadRetentionConfig merges defaults ← env ← settings file (when present).
func LoadRetentionConfig(configPath string) (RetentionConfig, error) {
	cfg := DefaultRetentionConfig()
	applyRetentionEnv(&cfg)

	if configPath == "" {
		configPath = os.Getenv("CONFIG_PATH")
		if configPath == "" {
			configPath = "/config"
		}
	}

	path := retentionSettingsPath(configPath)
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			normalizeRetentionConfig(&cfg)
			return cfg, nil
		}
		return cfg, fmt.Errorf("read retention settings: %w", err)
	}

	var patch retentionConfigPatch
	if err := json.Unmarshal(data, &patch); err != nil {
		return cfg, fmt.Errorf("parse retention settings: %w", err)
	}
	applyRetentionPatch(&cfg, patch)
	normalizeRetentionConfig(&cfg)
	return cfg, nil
}

// SaveRetentionConfig writes settings under CONFIG_PATH.
func SaveRetentionConfig(configPath string, cfg RetentionConfig) error {
	normalizeRetentionConfig(&cfg)
	if configPath == "" {
		configPath = os.Getenv("CONFIG_PATH")
		if configPath == "" {
			configPath = "/config"
		}
	}
	if err := os.MkdirAll(configPath, 0755); err != nil {
		return err
	}
	path := retentionSettingsPath(configPath)
	data, err := json.MarshalIndent(cfg, "", "  ")
	if err != nil {
		return err
	}
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, data, 0644); err != nil {
		return err
	}
	return os.Rename(tmp, path)
}

func retentionSettingsPath(configPath string) string {
	return filepath.Join(configPath, RetentionSettingsFileName)
}

func applyRetentionEnv(cfg *RetentionConfig) {
	if v, ok := lookupBoolEnv("RETENTION_ENABLED"); ok {
		cfg.Enabled = v
	}
	if v, ok := lookupBoolEnv("RETENTION_DRY_RUN"); ok {
		cfg.DryRun = v
	}
	if v, ok := lookupIntEnv("RETENTION_MAX_AGE_DAYS"); ok {
		cfg.MaxAgeDays = v
	}
	if v, ok := lookupFloatEnv("RETENTION_MAX_SIZE_GB"); ok {
		cfg.MaxSizeGB = v
	}
	if v, ok := lookupIntEnv("RETENTION_INTERVAL_HOURS"); ok {
		cfg.IntervalHours = v
	}
	if v, ok := lookupIntEnv("RETENTION_MIN_FILE_AGE_MINUTES"); ok {
		cfg.MinFileAgeMinutes = v
	}
	if fp := strings.TrimSpace(os.Getenv("FOOTAGE_PATH")); fp != "" {
		cfg.FootagePath = fp
	}
}

func applyRetentionPatch(dst *RetentionConfig, src retentionConfigPatch) {
	if src.Enabled != nil {
		dst.Enabled = *src.Enabled
	}
	if src.DryRun != nil {
		dst.DryRun = *src.DryRun
	}
	if src.MaxAgeDays != nil {
		dst.MaxAgeDays = *src.MaxAgeDays
	}
	if src.MaxSizeGB != nil {
		dst.MaxSizeGB = *src.MaxSizeGB
	}
	if src.IntervalHours != nil {
		dst.IntervalHours = *src.IntervalHours
	}
	if src.MinFileAgeMinutes != nil {
		dst.MinFileAgeMinutes = *src.MinFileAgeMinutes
	}
	if src.FootagePath != nil && strings.TrimSpace(*src.FootagePath) != "" {
		dst.FootagePath = *src.FootagePath
	}
}

func normalizeRetentionConfig(cfg *RetentionConfig) {
	if cfg.MaxAgeDays < 0 {
		cfg.MaxAgeDays = 0
	}
	if cfg.MaxSizeGB < 0 {
		cfg.MaxSizeGB = 0
	}
	if cfg.IntervalHours <= 0 {
		cfg.IntervalHours = DefaultRetentionIntervalHours
	}
	if cfg.MinFileAgeMinutes < 0 {
		cfg.MinFileAgeMinutes = 0
	}
}

func (c RetentionConfig) MaxSizeBytes() int64 {
	return int64(c.MaxSizeGB * 1024 * 1024 * 1024)
}

func (c RetentionConfig) MinFileAge() time.Duration {
	return time.Duration(c.MinFileAgeMinutes) * time.Minute
}

func (c RetentionConfig) Interval() time.Duration {
	return time.Duration(c.IntervalHours) * time.Hour
}

func lookupBoolEnv(key string) (bool, bool) {
	raw, ok := os.LookupEnv(key)
	if !ok {
		return false, false
	}
	switch strings.ToLower(strings.TrimSpace(raw)) {
	case "1", "true", "yes", "on":
		return true, true
	case "0", "false", "no", "off":
		return false, true
	default:
		return false, true
	}
}

func lookupIntEnv(key string) (int, bool) {
	raw, ok := os.LookupEnv(key)
	if !ok || strings.TrimSpace(raw) == "" {
		return 0, false
	}
	v, err := strconv.Atoi(strings.TrimSpace(raw))
	if err != nil {
		return 0, false
	}
	return v, true
}

func lookupFloatEnv(key string) (float64, bool) {
	raw, ok := os.LookupEnv(key)
	if !ok || strings.TrimSpace(raw) == "" {
		return 0, false
	}
	v, err := strconv.ParseFloat(strings.TrimSpace(raw), 64)
	if err != nil {
		return 0, false
	}
	return v, true
}
