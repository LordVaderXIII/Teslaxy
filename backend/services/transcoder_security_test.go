package services

import (
	"testing"
)

func TestTranscodeConcurrency(t *testing.T) {
	// The semaphore size is 4 (hardcoded in transcoder.go)
	limit := 4

	// Acquire all slots
	for i := 0; i < limit; i++ {
		err := AcquireTranscodeSlot()
		if err != nil {
			t.Fatalf("Failed to acquire slot %d: %v", i, err)
		}
	}

	// Try to acquire one more
	err := AcquireTranscodeSlot()
	if err != ErrServerBusy {
		t.Errorf("Expected ErrServerBusy when acquiring 5th slot, got %v", err)
	}

	// Release one slot
	ReleaseTranscodeSlot()

	// Try to acquire again (should succeed)
	err = AcquireTranscodeSlot()
	if err != nil {
		t.Errorf("Failed to acquire slot %v", err)
	}

	// Clean up: Release all slots we acquired
	// We acquired 'limit' slots initially, then 1 failed, then released 1, then acquired 1.
	// So we currently hold 'limit' slots.
	for i := 0; i < limit; i++ {
		ReleaseTranscodeSlot()
	}
}
