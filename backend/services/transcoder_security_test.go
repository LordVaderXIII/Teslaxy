package services

import (
	"testing"
)

func TestTranscodeConcurrencyLimit(t *testing.T) {
	// The semaphore buffer size is 4
	const limit = 4

	// Acquire all slots
	for i := 0; i < limit; i++ {
		if err := AcquireTranscodeSlot(); err != nil {
			t.Fatalf("Failed to acquire slot %d: %v", i+1, err)
		}
	}

	// Try to acquire the 5th slot - should fail
	if err := AcquireTranscodeSlot(); err != ErrServerBusy {
		t.Errorf("Expected ErrServerBusy when limit reached, got %v", err)
	}

	// Release one slot
	ReleaseTranscodeSlot()

	// Try to acquire again - should succeed
	if err := AcquireTranscodeSlot(); err != nil {
		t.Errorf("Failed to re-acquire slot after release: %v", err)
	}

	// Cleanup: Release all slots
	for i := 0; i < limit; i++ {
		ReleaseTranscodeSlot()
	}
}
