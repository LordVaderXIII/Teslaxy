package services

import (
	"context"
	"sync"
	"sync/atomic"
	"time"
)

// Default concurrency and timeouts for ffmpeg fan-out (transcode + thumbnails).
const (
	DefaultMaxConcurrentFFmpeg = 4
	DefaultTranscodeTimeout    = 2 * time.Minute
	DefaultThumbnailTimeout    = 30 * time.Second
)

var (
	ffmpegLimitMu sync.Mutex
	ffmpegSem     chan struct{}
	ffmpegActive  int64 // atomic peak tracking for tests
	ffmpegPeak    int64
)

func init() {
	resetFFmpegLimiter(DefaultMaxConcurrentFFmpeg)
}

func resetFFmpegLimiter(n int) {
	if n < 1 {
		n = 1
	}
	ffmpegLimitMu.Lock()
	defer ffmpegLimitMu.Unlock()
	ffmpegSem = make(chan struct{}, n)
	atomic.StoreInt64(&ffmpegActive, 0)
	atomic.StoreInt64(&ffmpegPeak, 0)
}

// SetMaxConcurrentFFmpegForTest replaces the semaphore capacity (tests only).
func SetMaxConcurrentFFmpegForTest(n int) {
	resetFFmpegLimiter(n)
}

// FFmpegPeakConcurrentForTest returns the high-water mark of in-flight ffmpeg slots.
func FFmpegPeakConcurrentForTest() int64 {
	return atomic.LoadInt64(&ffmpegPeak)
}

// AcquireFFmpeg blocks until a slot is available or ctx is done.
func AcquireFFmpeg(ctx context.Context) error {
	ffmpegLimitMu.Lock()
	sem := ffmpegSem
	ffmpegLimitMu.Unlock()

	select {
	case sem <- struct{}{}:
		cur := atomic.AddInt64(&ffmpegActive, 1)
		for {
			peak := atomic.LoadInt64(&ffmpegPeak)
			if cur <= peak || atomic.CompareAndSwapInt64(&ffmpegPeak, peak, cur) {
				break
			}
		}
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

// ReleaseFFmpeg frees one concurrency slot.
func ReleaseFFmpeg() {
	ffmpegLimitMu.Lock()
	sem := ffmpegSem
	ffmpegLimitMu.Unlock()
	atomic.AddInt64(&ffmpegActive, -1)
	<-sem
}

// WithFFmpegTimeout returns a child context with the given timeout.
func WithFFmpegTimeout(parent context.Context, d time.Duration) (context.Context, context.CancelFunc) {
	return context.WithTimeout(parent, d)
}
