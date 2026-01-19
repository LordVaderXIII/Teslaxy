package services

import (
	"testing"
	"time"
)

func TestTranscodingSemaphore(t *testing.T) {
	// 1. Acquire all 4 slots
	for i := 0; i < 4; i++ {
		if !AcquireTranscodingSession() {
			t.Fatalf("Failed to acquire session %d", i+1)
		}
	}

	// 2. Try to acquire 5th slot (should fail)
	if AcquireTranscodingSession() {
		t.Fatal("Expected 5th session to fail, but it succeeded")
	}

	// 3. Release one slot
	ReleaseTranscodingSession()

	// 4. Try to acquire again (should succeed)
	if !AcquireTranscodingSession() {
		t.Fatal("Failed to re-acquire session after release")
	}

	// 5. Cleanup: Release all held slots
	// We have acquired 4 initial + 1 re-acquired = 5 total successful acquisitions.
	// We released 1.
	// So we currently hold 4.
	for i := 0; i < 4; i++ {
		ReleaseTranscodingSession()
	}

	// 6. Verify we can acquire again
	if !AcquireTranscodingSession() {
		t.Fatal("Failed to acquire session after full cleanup")
	}
	ReleaseTranscodingSession()
}

func TestTranscodingSemaphoreRace(t *testing.T) {
	// Concurrent acquisition test
	successCount := 0
	done := make(chan bool)

	for i := 0; i < 10; i++ {
		go func() {
			if AcquireTranscodingSession() {
				// Simulate work
				time.Sleep(10 * time.Millisecond)
				ReleaseTranscodingSession()
				done <- true
			} else {
				done <- false
			}
		}()
	}

	for i := 0; i < 10; i++ {
		if <-done {
			successCount++
		}
	}

	// We can't deterministically say how many succeeded, but at least 1 should have if the test isn't super unlucky,
	// and definitely not more than 4 AT ONCE.
	// But since we release, all 10 MIGHT succeed sequentially.
	// This test mainly ensures no panics or deadlocks.
}
