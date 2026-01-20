package services

import (
	"context"
	"testing"
)

// Sentinel: Verify concurrency limits to prevent DoS
func TestTranscoderConcurrencyLimit(t *testing.T) {
	// 1. Consume all slots
	for i := 0; i < 4; i++ {
		if !AcquireTranscodeSlot() {
			t.Fatalf("Failed to acquire slot %d, expected success", i)
		}
	}

	// 2. Try to acquire 5th slot (should fail)
	if AcquireTranscodeSlot() {
		t.Fatal("Acquired 5th slot, expected failure (limit is 4)")
	}

	// 3. Release one slot
	ReleaseTranscodeSlot()

	// 4. Try to acquire again (should succeed)
	if !AcquireTranscodeSlot() {
		t.Fatal("Failed to re-acquire slot after release")
	}

	// Cleanup: Release all held slots
	// We held 4 initially, released 1, acquired 1. Total held: 4.
	for i := 0; i < 4; i++ {
		ReleaseTranscodeSlot()
	}

	// Ensure we can acquire again (system is clean)
	if !AcquireTranscodeSlot() {
		t.Fatal("Failed to acquire slot after cleanup")
	}
	ReleaseTranscodeSlot()
}

func TestTranscoderConcurrencyRelease(t *testing.T) {
	// Verify that release is non-blocking even if empty (idempotency-ish check)
	// Our implementation blocks on release if empty?
	// The implementation: case <-transcodeSemaphore: ... default: ...
	// So it is non-blocking if empty.

	// Ensure empty
	select {
	case <-transcodeSemaphore:
		t.Fatal("Semaphore should be empty")
	default:
	}

	// Should not panic or block
	ReleaseTranscodeSlot()
}

func TestGetTranscodeStreamConcurrency(t *testing.T) {
	// This test mocks the behavior by consuming slots manually and checking if GetTranscodeStream fails.
	// We don't want to actually run ffmpeg here, so we'll just check the semaphore acquisition logic
	// by filling the semaphore before calling GetTranscodeStream.

	// 1. Fill slots
	for i := 0; i < 4; i++ {
		AcquireTranscodeSlot()
	}

	// 2. Call GetTranscodeStream
	// It should return ErrServerBusy immediately without running ffmpeg
	ctx := context.TODO()
	cmd, _, release, err := GetTranscodeStream(ctx, "dummy", "480p")

	if err != ErrServerBusy {
		t.Errorf("Expected ErrServerBusy, got %v", err)
	}
	if cmd != nil {
		t.Error("Expected cmd to be nil")
	}
	if release != nil {
		t.Error("Expected release function to be nil")
	}

	// Cleanup
	for i := 0; i < 4; i++ {
		ReleaseTranscodeSlot()
	}
}
