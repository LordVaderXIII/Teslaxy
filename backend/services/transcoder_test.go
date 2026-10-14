package services

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
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

	listed := ffmpegHasNVENCEncoder(context.Background())
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
	if !ffmpegHasNVENCEncoder(context.Background()) {
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
	SetNVENCProbeForTest(func(context.Context) bool { return false })
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
	SetNVENCProbeForTest(func(context.Context) bool { return true })
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

func TestColdStartEncoderProbeCountsAgainstCap(t *testing.T) {
	// Codex finding #2: encoder probe must take a ffmpeg slot and respect ctx.
	// At 34fc9e4 AutoDetectEncoder ran ffmpeg outside the cap while holding the
	// mutex, so cold-start + 4 workers could show 5 concurrent ffmpeg processes.
	ResetEncoderDetectionForTest()
	SetMaxConcurrentFFmpegForTest(4)
	defer func() {
		SetNVENCProbeForTest(nil)
		ResetEncoderDetectionForTest()
		SetMaxConcurrentFFmpegForTest(DefaultMaxConcurrentFFmpeg)
	}()

	var probeActive, probePeak, probeCalls atomic.Int64
	probeHold := make(chan struct{})
	probeEntered := make(chan struct{}, 1)

	SetNVENCProbeForTest(func(ctx context.Context) bool {
		probeCalls.Add(1)
		cur := probeActive.Add(1)
		for {
			p := probePeak.Load()
			if cur <= p || probePeak.CompareAndSwap(p, cur) {
				break
			}
		}
		select {
		case probeEntered <- struct{}{}:
		default:
		}
		// While probe holds its slot, total active slots must stay <= cap.
		if FFmpegActiveForTest() > 4 {
			t.Errorf("ffmpeg active %d exceeds cap during probe", FFmpegActiveForTest())
		}
		select {
		case <-probeHold:
		case <-ctx.Done():
		}
		probeActive.Add(-1)
		return false
	})

	// Occupy 3 of 4 slots so only one remains for the probe.
	ctxBG := context.Background()
	for i := 0; i < 3; i++ {
		if err := AcquireFFmpeg(ctxBG); err != nil {
			t.Fatal(err)
		}
	}

	leaderDone := make(chan error, 1)
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer cancel()
		leaderDone <- ensureEncoderDetected(ctx)
	}()

	select {
	case <-probeEntered:
	case <-time.After(2 * time.Second):
		t.Fatal("probe did not start")
	}

	if got := FFmpegActiveForTest(); got != 4 {
		t.Fatalf("expected 4 active slots during probe (3 held + probe), got %d", got)
	}

	// A waiting request with a short deadline must cancel instead of blocking on the mutex.
	cancelCtx, cancel := context.WithTimeout(context.Background(), 40*time.Millisecond)
	defer cancel()
	waiterErr := ensureEncoderDetected(cancelCtx)
	if waiterErr == nil {
		t.Fatal("expected waiting ensureEncoderDetected to cancel, got nil")
	}

	// Fifth slot attempt must also cancel — cap is full (3 + probe).
	slotCtx, slotCancel := context.WithTimeout(context.Background(), 40*time.Millisecond)
	defer slotCancel()
	if err := AcquireFFmpeg(slotCtx); err == nil {
		ReleaseFFmpeg()
		t.Fatal("expected AcquireFFmpeg to fail while cap is full during probe")
	}

	close(probeHold)
	if err := <-leaderDone; err != nil {
		t.Fatalf("leader probe: %v", err)
	}
	for i := 0; i < 3; i++ {
		ReleaseFFmpeg()
	}

	if probeCalls.Load() != 1 {
		t.Fatalf("expected exactly one probe call, got %d", probeCalls.Load())
	}
	if probePeak.Load() != 1 {
		t.Fatalf("expected single probe goroutine, peak=%d", probePeak.Load())
	}
	if FFmpegPeakConcurrentForTest() > 4 {
		t.Fatalf("peak slots %d exceeds cap 4", FFmpegPeakConcurrentForTest())
	}
}

func TestColdStartFirstTranscodeUsesProbedEncoder(t *testing.T) {
	// First GetTranscodeStream after reset must finish probe (under cap) before encoding.
	if _, err := exec.LookPath("ffmpeg"); err != nil {
		t.Skip("ffmpeg not installed")
	}
	ResetEncoderDetectionForTest()
	SetMaxConcurrentFFmpegForTest(4)
	defer func() {
		SetNVENCProbeForTest(nil)
		ResetEncoderDetectionForTest()
		SetMaxConcurrentFFmpegForTest(DefaultMaxConcurrentFFmpeg)
	}()

	var probes atomic.Int64
	SetNVENCProbeForTest(func(ctx context.Context) bool {
		probes.Add(1)
		select {
		case <-ctx.Done():
			return false
		case <-time.After(20 * time.Millisecond):
			return false
		}
	})

	tmp := t.TempDir()
	in := filepath.Join(tmp, "in.mp4")
	makeSyntheticMP4(t, in, 0.5)

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	_, stdout, cleanup, err := GetTranscodeStream(ctx, in, "480p")
	if err != nil {
		t.Fatal(err)
	}
	defer cleanup()
	buf := make([]byte, 4096)
	n, _ := stdout.Read(buf)
	if n == 0 {
		t.Fatal("expected transcode bytes after cold-start probe")
	}
	if probes.Load() != 1 {
		t.Fatalf("expected 1 probe on first request, got %d", probes.Load())
	}
	if FFmpegPeakConcurrentForTest() > 4 {
		t.Fatalf("peak %d exceeds cap", FFmpegPeakConcurrentForTest())
	}
}

func TestEncoderProbe_CancelledCheckIsNotCached(t *testing.T) {
	// A cancelled first GPU check used to be stored as "no GPU" for good.
	ResetEncoderDetectionForTest()
	defer func() {
		SetNVENCProbeForTest(nil)
		ResetEncoderDetectionForTest()
	}()

	entered := make(chan struct{})
	var calls atomic.Int64
	SetNVENCProbeForTest(func(ctx context.Context) bool {
		n := calls.Add(1)
		if n == 1 {
			close(entered)
			<-ctx.Done()
			return false
		}
		return true
	})

	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() {
		done <- ensureEncoderDetected(ctx)
	}()

	select {
	case <-entered:
	case <-time.After(2 * time.Second):
		cancel()
		t.Fatal("encoder probe did not start")
	}
	cancel()

	select {
	case err := <-done:
		if err == nil {
			t.Fatal("cancelled probe returned success and would be cached as no GPU")
		}
		if !errors.Is(err, context.Canceled) {
			t.Fatalf("expected context.Canceled, got %v", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("cancelled probe did not return")
	}

	if err := ensureEncoderDetected(context.Background()); err != nil {
		t.Fatal(err)
	}
	if calls.Load() != 2 {
		t.Fatalf("expected a second probe after cancellation, got %d", calls.Load())
	}
	if got := AutoDetectEncoder(); got != "h264_nvenc" {
		t.Fatalf("later request encoder = %s, want h264_nvenc", got)
	}
	if !HasWorkingNVENC() {
		t.Fatal("later request still reports no GPU")
	}
	if calls.Load() != 2 {
		t.Fatalf("cached success was probed again, calls=%d", calls.Load())
	}
}

func TestEncoderProbe_CleanMissIsCached(t *testing.T) {
	// A real NVENC miss (context still live) stays cached. Only cancellation is retried.
	ResetEncoderDetectionForTest()
	defer func() {
		SetNVENCProbeForTest(nil)
		ResetEncoderDetectionForTest()
	}()

	var calls atomic.Int64
	SetNVENCProbeForTest(func(context.Context) bool {
		calls.Add(1)
		return false
	})
	if err := ensureEncoderDetected(context.Background()); err != nil {
		t.Fatal(err)
	}
	if err := ensureEncoderDetected(context.Background()); err != nil {
		t.Fatal(err)
	}
	if calls.Load() != 1 {
		t.Fatalf("clean NVENC miss must be cached, probes=%d", calls.Load())
	}
	if got := AutoDetectEncoder(); got != "libx264" {
		t.Fatalf("encoder = %s, want libx264", got)
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
