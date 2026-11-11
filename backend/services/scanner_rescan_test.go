package services

import (
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"

	pb "teslaxy/proto"
)

func TestScannerSingleFullScan(t *testing.T) {
	_, s := telemetryIDFixture(t)
	path := filepath.Join(s.FootagePath, "2026-11-01_12-00-00-front.mp4")
	mustTelemetryID(t, os.WriteFile(path, []byte("fixture"), 0600))
	entered := make(chan struct{}, 2)
	release := make(chan struct{})
	s.SEIExtractor = func(string) ([]*pb.SeiMetadata, error) {
		entered <- struct{}{}
		<-release
		return []*pb.SeiMetadata{{VehicleSpeedMps: 12}}, nil
	}
	var wg sync.WaitGroup
	defer func() { close(release); wg.Wait() }()
	wg.Add(1)
	go func() { defer wg.Done(); s.ScanAll() }()
	select {
	case <-entered:
	case <-time.After(5 * time.Second):
		t.Fatal("first scan did not reach extractor")
	}
	wg.Add(1)
	go func() { defer wg.Done(); s.ScanAll() }()
	select {
	case <-entered:
		t.Error("second full scan ran concurrently")
	case <-time.After(200 * time.Millisecond):
	}
}
