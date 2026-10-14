package services

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"
)

func TestAutoDetectEncoder_FallsBackWhenNVENCRuntimeFails(t *testing.T) {
	// H3 regression: ffmpeg may list h264_nvenc while CUDA is absent.
	if _, err := exec.LookPath("ffmpeg"); err != nil {
		t.Skip("ffmpeg not installed")
	}

	ResetEncoderDetectionForTest()
	SetNVENCProbeForTest(nil) // real probe
	defer ResetEncoderDetectionForTest()

	listed := ffmpegHasNVENCEncoder()
	enc := AutoDetectEncoder()

	if listed && !HasWorkingNVENC() {
		if enc != "libx264" {
			t.Fatalf("expected libx264 when nvenc listed but runtime fails, got %s", enc)
		}
	}
	if !listed && enc != "libx264" {
		t.Fatalf("expected libx264 when nvenc not listed, got %s", enc)
	}
	if HasWorkingNVENC() && enc != "h264_nvenc" {
		t.Fatalf("expected h264_nvenc when probe succeeds, got %s", enc)
	}
}

func TestAutoDetectEncoder_OldListOnlyWouldEnableNVENC(t *testing.T) {
	// Documents the H3 bug: list-only detection disagrees with runtime probe on this host.
	if _, err := exec.LookPath("ffmpeg"); err != nil {
		t.Skip("ffmpeg not installed")
	}
	if !ffmpegHasNVENCEncoder() {
		t.Skip("ffmpeg does not list h264_nvenc; cannot demonstrate list-vs-runtime gap")
	}

	ResetEncoderDetectionForTest()
	SetNVENCProbeForTest(nil)
	defer func() {
		SetNVENCProbeForTest(nil)
		ResetEncoderDetectionForTest()
	}()

	_ = AutoDetectEncoder()
	if HasWorkingNVENC() {
		t.Skip("NVENC runtime works here; H3 empty-200 repro needs a GPU-less host")
	}
	// Reaching here means list says yes, probe says no — the fix path.
}

func TestCheckForNvidiaGPU_IgnoresNvidiaSMIAlone(t *testing.T) {
	// H5 regression: nvidia-smi success must not enable NVENC when encoder probe fails.
	tmp := t.TempDir()
	fakeSMI := filepath.Join(tmp, "nvidia-smi")
	if err := os.WriteFile(fakeSMI, []byte("#!/bin/sh\nexit 0\n"), 0755); err != nil {
		t.Fatal(err)
	}
	oldPath := os.Getenv("PATH")
	os.Setenv("PATH", tmp+string(os.PathListSeparator)+oldPath)
	defer os.Setenv("PATH", oldPath)

	ResetEncoderDetectionForTest()
	ResetGPUCheckForTest()
	SetNVENCProbeForTest(func() bool { return false })
	defer func() {
		SetNVENCProbeForTest(nil)
		ResetEncoderDetectionForTest()
		ResetGPUCheckForTest()
	}()

	if CheckForNvidiaGPU() {
		t.Fatal("CheckForNvidiaGPU must be false when NVENC probe fails, even if nvidia-smi exits 0")
	}
	if AutoDetectEncoder() != "libx264" {
		t.Fatalf("expected libx264, got %s", AutoDetectEncoder())
	}
}

func TestCheckForNvidiaGPU_TrueWhenProbeSucceeds(t *testing.T) {
	ResetEncoderDetectionForTest()
	ResetGPUCheckForTest()
	SetNVENCProbeForTest(func() bool { return true })
	defer func() {
		SetNVENCProbeForTest(nil)
		ResetEncoderDetectionForTest()
		ResetGPUCheckForTest()
	}()

	if !CheckForNvidiaGPU() {
		t.Fatal("expected true when probe succeeds")
	}
	if AutoDetectEncoder() != "h264_nvenc" {
		t.Fatalf("expected h264_nvenc, got %s", AutoDetectEncoder())
	}
}

func TestFFmpegConcurrencyCap(t *testing.T) {
	// M8 regression: parallel acquires must not exceed the cap.
	SetMaxConcurrentFFmpegForTest(2)
	defer SetMaxConcurrentFFmpegForTest(DefaultMaxConcurrentFFmpeg)

	ctx := context.Background()
	var wg sync.WaitGroup
	started := make(chan struct{}, 8)
	releaseGate := make(chan struct{})

	for i := 0; i < 8; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if err := AcquireFFmpeg(ctx); err != nil {
				t.Errorf("acquire: %v", err)
				return
			}
			started <- struct{}{}
			<-releaseGate
			ReleaseFFmpeg()
		}()
	}

	// Wait until 2 are holding slots
	deadline := time.After(2 * time.Second)
	got := 0
	for got < 2 {
		select {
		case <-started:
			got++
		case <-deadline:
			t.Fatalf("timed out waiting for 2 acquires; got %d", got)
		}
	}

	time.Sleep(50 * time.Millisecond)
	if peak := FFmpegPeakConcurrentForTest(); peak > 2 {
		t.Fatalf("peak concurrent ffmpeg %d exceeds cap 2", peak)
	}

	// Remaining 6 should still be blocked
	select {
	case <-started:
		t.Fatal("third acquire should still be blocked")
	case <-time.After(50 * time.Millisecond):
	}

	close(releaseGate)
	wg.Wait()

	if peak := FFmpegPeakConcurrentForTest(); peak > 2 {
		t.Fatalf("final peak %d exceeds cap 2", peak)
	}
}

func TestFFmpegAcquireTimeout(t *testing.T) {
	SetMaxConcurrentFFmpegForTest(1)
	defer SetMaxConcurrentFFmpegForTest(DefaultMaxConcurrentFFmpeg)

	ctx := context.Background()
	if err := AcquireFFmpeg(ctx); err != nil {
		t.Fatal(err)
	}
	defer ReleaseFFmpeg()

	tctx, cancel := context.WithTimeout(ctx, 50*time.Millisecond)
	defer cancel()
	err := AcquireFFmpeg(tctx)
	if err == nil {
		ReleaseFFmpeg()
		t.Fatal("expected timeout while slot held")
	}
	if !strings.Contains(err.Error(), "context") && err != context.DeadlineExceeded {
		t.Fatalf("expected context deadline, got %v", err)
	}
}

func makeSyntheticMP4(t *testing.T, path string, seconds float64) {
	t.Helper()
	if _, err := exec.LookPath("ffmpeg"); err != nil {
		t.Skip("ffmpeg not installed")
	}
	dur := strconv.FormatFloat(seconds, 'f', 3, 64)
	cmd := exec.Command("ffmpeg", "-y", "-hide_banner", "-loglevel", "error",
		"-f", "lavfi", "-i", "color=c=blue:s=320x240:d="+dur,
		"-c:v", "libx264", "-pix_fmt", "yuv420p", "-t", dur, path)
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("ffmpeg fixture: %v (%s)", err, out)
	}
}
