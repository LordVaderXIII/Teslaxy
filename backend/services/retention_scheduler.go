package services

import (
	"log"
	"time"
)

// Start launches the retention scheduler. It runs once shortly after boot,
// then on IntervalHours. Mirrors the export/auth cleanup ticker pattern.
func (s *RetentionService) Start() {
	go s.loop()
}

// Stop signals the scheduler to exit. Safe to call once.
func (s *RetentionService) Stop() {
	select {
	case <-s.stopCh:
		return
	default:
		close(s.stopCh)
	}
}

func (s *RetentionService) loop() {
	// Short initial delay so scanner can begin indexing before the first pass.
	initial := time.NewTimer(2 * time.Minute)
	defer initial.Stop()

	select {
	case <-s.stopCh:
		return
	case <-initial.C:
		s.runScheduled("startup")
	}

	for {
		s.mu.Lock()
		interval := s.cfg.Interval()
		s.mu.Unlock()
		if interval <= 0 {
			interval = time.Duration(DefaultRetentionIntervalHours) * time.Hour
		}

		timer := time.NewTimer(interval)
		select {
		case <-s.stopCh:
			timer.Stop()
			return
		case <-timer.C:
			s.runScheduled("interval")
		}
	}
}

func (s *RetentionService) runScheduled(trigger string) {
	if err := s.ReloadConfig(); err != nil {
		log.Printf("retention: reload config before %s run: %v", trigger, err)
	}
	cfg := s.Config()
	if !cfg.Enabled && !cfg.DryRun {
		log.Printf("retention: skipping %s run (disabled)", trigger)
		return
	}
	log.Printf("retention: starting %s run (enabled=%v dry_run=%v)", trigger, cfg.Enabled, cfg.DryRun)
	if _, err := s.Run(); err != nil {
		log.Printf("retention: %s run error: %v", trigger, err)
	}
}
