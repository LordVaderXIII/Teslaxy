package services

import (
	"testing"
)

func TestTranscoderConcurrencyLimit(t *testing.T) {
	// 1. Fill all 4 slots
	// We assume the semaphore starts empty or we might need to drain it.
	// Since tests might run in random order, we should be careful.
	// However, usually tests are isolated or we assume they clean up.
	// Let's just try to fill it.

	acquiredCount := 0

	// Helper to cleanup
	defer func() {
		for i := 0; i < acquiredCount; i++ {
			ReleaseTranscodeSlot()
		}
	}()

	for i := 0; i < 4; i++ {
		if err := AcquireTranscodeSlot(); err != nil {
			t.Fatalf("Failed to acquire slot %d: %v. Semaphore might not be empty.", i+1, err)
		}
		acquiredCount++
	}

	// 2. Try to acquire 5th slot - should fail
	if err := AcquireTranscodeSlot(); err != ErrServerBusy {
		t.Errorf("Expected ErrServerBusy when acquiring 5th slot, got %v", err)
	}

	// 3. Release one slot
	ReleaseTranscodeSlot()
	acquiredCount--

	// 4. Try to acquire again - should succeed
	if err := AcquireTranscodeSlot(); err != nil {
		t.Errorf("Expected success after releasing slot, got %v", err)
	} else {
		acquiredCount++
	}
}
