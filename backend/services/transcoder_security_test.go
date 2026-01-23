package services

import (
	"testing"
)

func TestTranscoderConcurrencyLimit(t *testing.T) {
	// 1. Consume all slots (4 slots)
	for i := 0; i < 4; i++ {
		err := AcquireTranscodeSlot()
		if err != nil {
			t.Fatalf("Failed to acquire slot %d: %v", i+1, err)
		}
	}

	// 2. Try to acquire 5th slot - should fail
	err := AcquireTranscodeSlot()
	if err != ErrServerBusy {
		t.Errorf("Expected ErrServerBusy when all slots are full, got: %v", err)
	}

	// 3. Release one slot
	ReleaseTranscodeSlot()

	// 4. Acquire again - should succeed
	err = AcquireTranscodeSlot()
	if err != nil {
		t.Errorf("Failed to acquire slot after release: %v", err)
	}

	// Cleanup: Release all slots for other tests (though this test runs in isolation usually)
	// We have 4 slots held now (3 from first loop, 1 from last acquire)
	for i := 0; i < 4; i++ {
		ReleaseTranscodeSlot()
	}
}
