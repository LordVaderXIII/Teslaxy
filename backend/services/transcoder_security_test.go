package services

import (
	"testing"
)

func TestTranscodeConcurrencyLimit(t *testing.T) {
	// 1. Acquire 4 slots
	for i := 0; i < 4; i++ {
		err := AcquireTranscodeSlot()
		if err != nil {
			t.Fatalf("Failed to acquire slot %d: %v", i, err)
		}
	}

	// 2. Try to acquire 5th slot - should fail
	err := AcquireTranscodeSlot()
	if err != ErrServerBusy {
		t.Fatalf("Expected ErrServerBusy, got %v", err)
	}

	// 3. Release one slot
	ReleaseTranscodeSlot()

	// 4. Try to acquire again - should succeed
	err = AcquireTranscodeSlot()
	if err != nil {
		t.Fatalf("Failed to acquire slot after release: %v", err)
	}

	// Cleanup
	for i := 0; i < 4; i++ {
		ReleaseTranscodeSlot()
	}
}
