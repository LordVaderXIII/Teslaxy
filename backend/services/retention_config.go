package services

import (
	"encoding/json"
	"fmt"
	"math"
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

	MaxRetentionAgeDays        = 36500 // ~100 years
	MaxRetentionSizeGB         = 1024 * 1024 // 1 PB cap on the knob
	MaxRetentionIntervalHours  = 24 * 30
	MaxRetentionMinFileAgeMins = 60 * 24 * 7
)

// RetentionConfig controls footage pruning.
type RetentionConfig struct {
	Enabled           bool    `json:"enabled"`
	DryRun            bool    `json:"dry_run"`
	MaxAgeDays        int     `json:"max_age_days"`
	MaxSizeGB         float64 `json:"max_size_gb"`
	IntervalHours     int     `json:"interval_hours"`
	MinFileAgeMinutes int     `json:"min_file_age_minutes"`
	FootagePath       string  `json:"footage_path,omitempty"`
}

type retentionConfigPatch struct {
	Enabled           *bool    `json:"enabled"`
	DryRun            *bool    `json:"dry_run"`
	MaxAgeDays        *int     `json:"max_age_days"`
	MaxSizeGB         *float64 `json:"max_size_gb"`
	IntervalHours     *int     `json:"interval_hours"`
	MinFileAgeMinutes *int     `json:"min_file_age_minutes"`
	FootagePath       *string  `json:"footage_path"`
}

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

func SaveRetentionConfig(configPath string, cfg RetentionConfig) error {
	normalizeRetentionConfig(&cfg)
	if err := ValidateRetentionConfig(cfg); err != nil {
		return err
	}
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
	// Enabled: unknown/malformed → leave default (false). Never treat typos as true.
	if v, ok, valid := lookupStrictBoolEnv("RETENTION_ENABLED"); ok && valid {
		cfg.Enabled = v
	}
	// Dry-run: unknown/malformed → stay ON (safe). Explicit false only when parsed cleanly.
	if v, ok, valid := lookupStrictBoolEnv("RETENTION_DRY_RUN"); ok {
		if valid {
			cfg.DryRun = v
		} else {
			cfg.DryRun = true
		}
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
	if cfg.MaxAgeDays > MaxRetentionAgeDays {
		cfg.MaxAgeDays = MaxRetentionAgeDays
	}
	if cfg.MaxSizeGB < 0 || math.IsNaN(cfg.MaxSizeGB) || math.IsInf(cfg.MaxSizeGB, 0) {
		cfg.MaxSizeGB = 0
	}
	if cfg.MaxSizeGB > MaxRetentionSizeGB {
		cfg.MaxSizeGB = MaxRetentionSizeGB
	}
	if cfg.IntervalHours <= 0 {
		cfg.IntervalHours = DefaultRetentionIntervalHours
	}
	if cfg.IntervalHours > MaxRetentionIntervalHours {
		cfg.IntervalHours = MaxRetentionIntervalHours
	}
	if cfg.MinFileAgeMinutes < 0 {
		cfg.MinFileAgeMinutes = 0
	}
	if cfg.MinFileAgeMinutes > MaxRetentionMinFileAgeMins {
		cfg.MinFileAgeMinutes = MaxRetentionMinFileAgeMins
	}
}

// ValidateRetentionConfig rejects values that cannot be stored safely.
func ValidateRetentionConfig(cfg RetentionConfig) error {
	if cfg.MaxAgeDays < 0 || cfg.MaxAgeDays > MaxRetentionAgeDays {
		return fmt.Errorf("max_age_days out of range [0,%d]", MaxRetentionAgeDays)
	}
	if cfg.MaxSizeGB < 0 || cfg.MaxSizeGB > MaxRetentionSizeGB || math.IsNaN(cfg.MaxSizeGB) || math.IsInf(cfg.MaxSizeGB, 0) {
		return fmt.Errorf("max_size_gb out of range [0,%d]", MaxRetentionSizeGB)
	}
	if cfg.IntervalHours <= 0 || cfg.IntervalHours > MaxRetentionIntervalHours {
		return fmt.Errorf("interval_hours out of range [1,%d]", MaxRetentionIntervalHours)
	}
	if cfg.MinFileAgeMinutes < 0 || cfg.MinFileAgeMinutes > MaxRetentionMinFileAgeMins {
		return fmt.Errorf("min_file_age_minutes out of range [0,%d]", MaxRetentionMinFileAgeMins)
	}
	return nil
}

func (c RetentionConfig) MaxSizeBytes() int64 {
	if c.MaxSizeGB <= 0 {
		return 0
	}
	const gib = 1024.0 * 1024.0 * 1024.0
	maxGB := float64(math.MaxInt64) / gib
	if c.MaxSizeGB >= maxGB {
		return math.MaxInt64
	}
	bytes := c.MaxSizeGB * gib
	if bytes > float64(math.MaxInt64) {
		return math.MaxInt64
	}
	if bytes < 0 {
		return 0
	}
	return int64(bytes)
}

func (c RetentionConfig) MinFileAge() time.Duration {
	if c.MinFileAgeMinutes <= 0 {
		return 0
	}
	return time.Duration(c.MinFileAgeMinutes) * time.Minute
}

func (c RetentionConfig) Interval() time.Duration {
	h := c.IntervalHours
	if h <= 0 {
		h = DefaultRetentionIntervalHours
	}
	return time.Duration(h) * time.Hour
}

// lookupStrictBoolEnv returns (value, present, valid).
// present=false when unset. valid=false when the value is not a recognized boolean.
func lookupStrictBoolEnv(key string) (value bool, present bool, valid bool) {
	raw, ok := os.LookupEnv(key)
	if !ok {
		return false, false, false
	}
	switch strings.ToLower(strings.TrimSpace(raw)) {
	case "1", "true", "yes", "on":
		return true, true, true
	case "0", "false", "no", "off":
		return false, true, true
	default:
		return false, true, false
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
