package services

import (
	"testing"
)

func TestTranscodeConcurrencyLimit(t *testing.T) {
	// 1. Reset State: Drain any lingering slots from other tests/runs
	// Consume until full
	acquired := 0
	for {
		if err := AcquireTranscodeSlot(); err != nil {
			break
		}
		acquired++
	}
	// Release all to ensure clean slate (empty channel, 4 slots available)
	for i := 0; i < acquired; i++ {
		ReleaseTranscodeSlot()
	}

	t.Run("EnforceMaxConcurrency", func(t *testing.T) {
		// Acquire 4 slots (Max)
		for i := 0; i < 4; i++ {
			if err := AcquireTranscodeSlot(); err != nil {
				t.Fatalf("Failed to acquire slot %d: %v", i+1, err)
			}
		}

		// 5th attempt should fail immediately
		if err := AcquireTranscodeSlot(); err != ErrServerBusy {
			t.Errorf("Expected ErrServerBusy, got %v", err)
		}

		// Release one
		ReleaseTranscodeSlot()

		// Should be able to acquire again
		if err := AcquireTranscodeSlot(); err != nil {
			t.Errorf("Failed to acquire after release: %v", err)
		}

		// Cleanup: Release all held slots
		// We held 4 initially, failed 5th, released 1, acquired 1. Total held: 4.
		for i := 0; i < 4; i++ {
			ReleaseTranscodeSlot()
		}
	})

	t.Run("ReleaseOnEmpty", func(t *testing.T) {
		// Should not block or panic
		ReleaseTranscodeSlot()

		// Verify capacity is still intact
		for i := 0; i < 4; i++ {
			if err := AcquireTranscodeSlot(); err != nil {
				t.Fatalf("Failed to acquire slot %d: %v", i+1, err)
			}
		}
		// Cleanup
		for i := 0; i < 4; i++ {
			ReleaseTranscodeSlot()
		}
	})
}
