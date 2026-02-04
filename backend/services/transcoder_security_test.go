package services

import (
	"testing"
)

func TestTranscoderConcurrency(t *testing.T) {
	// Reset semaphore state for test if needed, but since it's global and this is the only test using it, we assume it's clean or we should drain it.
	// Drain any existing slots to ensure predictable state
	for {
		select {
		case <-transcodeSemaphore:
		default:
			goto Drained
		}
	}
Drained:

	// Attempt to acquire 4 slots
	for i := 0; i < 4; i++ {
		if err := AcquireTranscodeSlot(); err != nil {
			t.Fatalf("Failed to acquire slot %d: %v", i+1, err)
		}
	}

	// 5th attempt should fail
	if err := AcquireTranscodeSlot(); err != ErrServerBusy {
		t.Fatalf("Expected ErrServerBusy, got %v", err)
	}

	// Release one slot
	ReleaseTranscodeSlot()

	// Acquire again, should succeed
	if err := AcquireTranscodeSlot(); err != nil {
		t.Fatalf("Failed to acquire slot after release: %v", err)
	}

	// Clean up (release all 4 slots held)
	for i := 0; i < 4; i++ {
		ReleaseTranscodeSlot()
	}
}
