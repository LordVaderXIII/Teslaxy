package services

import (
	"encoding/json"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"time"
)

const retentionStatusFileName = "retention-status.json"

// RetentionRunSummary deliberately excludes paths and unbounded candidate lists.
// CandidateBytes is the hardlink-aware estimate, not bytes actually deleted.
type RetentionRunSummary struct {
	StartedAt      time.Time `json:"started_at"`
	FinishedAt     time.Time `json:"finished_at"`
	Enabled        bool      `json:"enabled"`
	DryRun         bool      `json:"dry_run"`
	CandidateCount int       `json:"candidate_count"`
	CandidateBytes int64     `json:"candidate_bytes"`
	CandidateGiB   float64   `json:"candidate_gib"`
	DeletedCount   int       `json:"deleted_count"`
	ErrorCount     int       `json:"error_count"`
}

type RetentionStatus struct {
	LastRun *RetentionRunSummary `json:"last_run"`
	// NextRun is an estimate: last finished run + current interval, or boot + 2m.
	// It is null when retention is disabled, regardless of dry-run mode.
	NextRun    *time.Time           `json:"next_run"`
	LastDryRun *RetentionRunSummary `json:"last_dry_run"`
}

func (s *RetentionService) Status() RetentionStatus {
	s.mu.Lock()
	defer s.mu.Unlock()
	status := s.status
	// Return copies so callers cannot mutate service history outside the lock.
	if status.LastRun != nil {
		v := *status.LastRun
		status.LastRun = &v
	}
	if status.LastDryRun != nil {
		v := *status.LastDryRun
		status.LastDryRun = &v
	}
	status.NextRun = nil
	if s.cfg.Enabled {
		next := s.firstRunAt
		if status.LastRun != nil {
			next = status.LastRun.FinishedAt.Add(s.cfg.Interval())
		}
		status.NextRun = &next
	}
	return status
}

func (s *RetentionService) statusPath() string {
	configPath := s.ConfigPath
	if configPath == "" {
		configPath = os.Getenv("CONFIG_PATH")
	}
	if configPath == "" {
		configPath = "/config"
	}
	return filepath.Join(configPath, retentionStatusFileName)
}

// loadStatus is called only during construction, before the service is shared.
func (s *RetentionService) loadStatus() {
	data, err := os.ReadFile(s.statusPath())
	if os.IsNotExist(err) {
		return
	}
	if err != nil {
		log.Printf("retention: read status: %v", err)
		return
	}
	var status RetentionStatus
	if err := json.Unmarshal(data, &status); err != nil {
		log.Printf("retention: parse status: %v", err)
		return
	}
	status.NextRun = nil // Derived from the current config, never stale persisted data.
	s.status = status
}

// recordReport runs with s.mu held by RunWithConfig. History is a separate file:
// it cannot change or enable retention.json, including after a dry-run override.
func (s *RetentionService) recordReport(report *RetentionReport) error {
	summary := &RetentionRunSummary{
		StartedAt: report.StartedAt, FinishedAt: report.FinishedAt, Enabled: report.Enabled, DryRun: report.DryRun,
		CandidateCount: len(report.Candidates), DeletedCount: len(report.Deleted), ErrorCount: len(report.Errors),
	}
	for _, candidate := range report.Candidates {
		summary.CandidateBytes += candidate.Bytes
	}
	summary.CandidateGiB = float64(summary.CandidateBytes) / (1 << 30)
	s.status.LastRun = summary
	if report.DryRun {
		s.status.LastDryRun = summary
	}
	data, err := json.Marshal(s.status)
	if err != nil {
		return err
	}
	path := s.statusPath()
	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		return err
	}
	file, err := os.CreateTemp(filepath.Dir(path), ".retention-status-*")
	if err != nil {
		return err
	}
	defer os.Remove(file.Name())
	if _, err := file.Write(data); err != nil {
		file.Close()
		return err
	}
	if err := file.Close(); err != nil {
		return err
	}
	if err := os.Rename(file.Name(), path); err != nil {
		return fmt.Errorf("replace status: %w", err)
	}
	return nil
}
