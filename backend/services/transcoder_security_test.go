package services

import (
	"testing"
)

func TestTranscoderConcurrencyLimit(t *testing.T) {
	// Drain any existing slots from previous tests/runs
	// Since the semaphore is a package-level variable, we need to be careful.
	// We can't easily drain it if we don't know the state, but we can assume it starts empty or we can try to drain it.
	// Actually, running `go test` starts a fresh process, so it should be empty.

	// 1. Acquire all 4 slots
	for i := 0; i < 4; i++ {
		if err := AcquireTranscodeSlot(); err != nil {
			t.Fatalf("Failed to acquire slot %d: %v", i, err)
		}
	}

	// 2. Try to acquire a 5th slot - should fail
	if err := AcquireTranscodeSlot(); err != ErrServerBusy {
		t.Errorf("Expected ErrServerBusy when acquiring 5th slot, got %v", err)
	}

	// 3. Release one slot
	ReleaseTranscodeSlot()

	// 4. Try to acquire again - should succeed
	if err := AcquireTranscodeSlot(); err != nil {
		t.Errorf("Failed to acquire slot after release: %v", err)
	}

	// 5. Try to acquire again - should fail
	if err := AcquireTranscodeSlot(); err != ErrServerBusy {
		t.Errorf("Expected ErrServerBusy when acquiring slot after re-filling, got %v", err)
	}

	// Cleanup: Release all slots for future tests (though this test run ends here)
	for i := 0; i < 4; i++ {
		ReleaseTranscodeSlot()
	}
}

func TestTranscoderReleaseWithoutAcquire(t *testing.T) {
	// This test ensures that calling Release without Acquire doesn't panic/block indefinitely
	// (Our implementation uses a non-blocking read in Release, or just reads. Wait, let's check the implementation.)
	// The implementation:
	// func ReleaseTranscodeSlot() {
	// 	select {
	// 	case <-transcodeSemaphore:
	// 	default:
	// 		log.Println("Warning: specific semaphore release without acquisition")
	// 	}
	// }
	// So it is non-blocking safe.

	// Ensure semaphore is empty first (it should be empty after previous test cleanup)
	// But tests might run in parallel? `go test` runs packages in parallel but tests within a package strictly unless t.Parallel() is called.
	// We didn't call t.Parallel().

	// However, to be safe, we can just call Release and expect no panic.

	// Let's force it to be empty first?
	// The previous test cleaned up.

	// Call release on empty semaphore
	ReleaseTranscodeSlot()

	// If we are here, it didn't panic.
	// We could capture logs to verify the warning, but that's overkill.
}
