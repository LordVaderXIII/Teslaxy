package services

import (
	"context"
	"testing"
)

func TestTranscoderConcurrencyLimit(t *testing.T) {
	// 1. Artificially fill the semaphore
	// The limit is 4 (hardcoded in transcoder.go)

	// Reset semaphore first (just in case other tests ran)
loop:
	for {
		select {
		case <-transcodeSemaphore:
		default:
			break loop
		}
	}

	// Fill it up
	for i := 0; i < 4; i++ {
		select {
		case transcodeSemaphore <- struct{}{}:
		default:
			t.Fatal("Semaphore full before we finished filling it?")
		}
	}

	// 2. Attempt to get a stream
	ctx := context.Background()
	// Use a dummy file path; we expect it to fail anyway due to semaphore,
	// or if we release one, fail due to ffmpeg/file, but NOT server busy.
	_, _, _, err := GetTranscodeStream(ctx, "dummy.mp4", "480p")

	// 3. Verify it failed with "server busy"
	if err == nil {
		t.Fatal("Expected error due to full semaphore, got nil")
	}

	if err != ErrServerBusy {
		t.Fatalf("Expected ErrServerBusy, got: %v", err)
	}

	// 4. Release one slot
	<-transcodeSemaphore

	// 5. Attempt again
	// We expect it to TRY to start ffmpeg.
	// It might fail if ffmpeg is missing or file missing, but NOT "server busy".
	_, _, release, err := GetTranscodeStream(ctx, "dummy.mp4", "480p")

	if err != nil && err == ErrServerBusy {
		t.Fatalf("Should not get 'server busy' after releasing semaphore. Got: %v", err)
	}

	// If it succeeded (mock environment?), release it.
	if release != nil {
		release()
	}
    // If it failed (ffmpeg missing), release was already called internally.
}
