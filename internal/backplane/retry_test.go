package backplane

import (
	"context"
	"fmt"
	"sync/atomic"
	"testing"
	"time"
)

func TestRetryLogin_SucceedsOnFirstAttempt(t *testing.T) {
	t.Run("returns result immediately when login succeeds", func(t *testing.T) {
		limiter := NewAdaptiveLimiter(10)
		var attempts int32

		loginFn := func(ctx context.Context, clusterID string) (string, func(), error) {
			atomic.AddInt32(&attempts, 1)
			return "/kube/config", func() {}, nil
		}

		path, cleanup, err := RetryLogin(context.Background(), "cluster-1", loginFn, limiter, 3)
		if err != nil {
			t.Fatalf("RetryLogin returned error: %v", err)
		}
		if path != "/kube/config" {
			t.Errorf("path = %q, want /kube/config", path)
		}
		cleanup()

		if got := atomic.LoadInt32(&attempts); got != 1 {
			t.Errorf("attempts = %d, want 1 (no retries needed)", got)
		}
	})

	t.Run("records success on the limiter when first attempt succeeds", func(t *testing.T) {
		limiter := NewAdaptiveLimiter(4)
		// Drive limit down so we can observe the RecordSuccess bump.
		limiter.RecordFailure() // 4 -> 2
		limiter.RecordFailure() // 2 -> 1

		loginFn := func(ctx context.Context, clusterID string) (string, func(), error) {
			return "/kube/config", func() {}, nil
		}

		_, cleanup, err := RetryLogin(context.Background(), "cluster-1", loginFn, limiter, 3)
		if err != nil {
			t.Fatalf("RetryLogin returned error: %v", err)
		}
		cleanup()

		// After success: 1 -> 2
		if got := limiter.CurrentLimit(); got != 2 {
			t.Errorf("limit = %d, want 2 (1 + RecordSuccess)", got)
		}
	})
}

func TestRetryLogin_RetriesOnFailure(t *testing.T) {
	t.Run("succeeds on second attempt after one transient failure", func(t *testing.T) {
		limiter := NewAdaptiveLimiter(10)
		var attempts int32

		loginFn := func(ctx context.Context, clusterID string) (string, func(), error) {
			n := atomic.AddInt32(&attempts, 1)
			if n == 1 {
				return "", nil, fmt.Errorf("transient error")
			}
			return "/kube/config", func() {}, nil
		}

		path, cleanup, err := RetryLogin(context.Background(), "cluster-1", loginFn, limiter, 3)
		if err != nil {
			t.Fatalf("RetryLogin returned error: %v", err)
		}
		if path != "/kube/config" {
			t.Errorf("path = %q, want /kube/config", path)
		}
		cleanup()

		if got := atomic.LoadInt32(&attempts); got != 2 {
			t.Errorf("attempts = %d, want 2", got)
		}
	})

	t.Run("succeeds on third attempt after two transient failures", func(t *testing.T) {
		limiter := NewAdaptiveLimiter(10)
		var attempts int32

		loginFn := func(ctx context.Context, clusterID string) (string, func(), error) {
			n := atomic.AddInt32(&attempts, 1)
			if n < 3 {
				return "", nil, fmt.Errorf("transient error attempt %d", n)
			}
			return "/kube/config", func() {}, nil
		}

		path, cleanup, err := RetryLogin(context.Background(), "cluster-1", loginFn, limiter, 5)
		if err != nil {
			t.Fatalf("RetryLogin returned error: %v", err)
		}
		if path != "/kube/config" {
			t.Errorf("path = %q, want /kube/config", path)
		}
		cleanup()

		if got := atomic.LoadInt32(&attempts); got != 3 {
			t.Errorf("attempts = %d, want 3", got)
		}
	})
}

func TestRetryLogin_ExhaustsRetries(t *testing.T) {
	t.Run("returns final error after 1 initial + maxRetries attempts", func(t *testing.T) {
		limiter := NewAdaptiveLimiter(10)
		var attempts int32

		loginFn := func(ctx context.Context, clusterID string) (string, func(), error) {
			atomic.AddInt32(&attempts, 1)
			return "", nil, fmt.Errorf("persistent error")
		}

		_, _, err := RetryLogin(context.Background(), "cluster-1", loginFn, limiter, 3)
		if err == nil {
			t.Fatal("expected error when all retries exhausted")
		}

		// 1 initial + 3 retries = 4 total attempts.
		if got := atomic.LoadInt32(&attempts); got != 4 {
			t.Errorf("attempts = %d, want 4 (1 initial + 3 retries)", got)
		}
	})

	t.Run("maxRetries=0 means only one attempt with no retries", func(t *testing.T) {
		limiter := NewAdaptiveLimiter(10)
		var attempts int32

		loginFn := func(ctx context.Context, clusterID string) (string, func(), error) {
			atomic.AddInt32(&attempts, 1)
			return "", nil, fmt.Errorf("error")
		}

		_, _, err := RetryLogin(context.Background(), "cluster-1", loginFn, limiter, 0)
		if err == nil {
			t.Fatal("expected error")
		}

		if got := atomic.LoadInt32(&attempts); got != 1 {
			t.Errorf("attempts = %d, want 1 (maxRetries=0 means no retries)", got)
		}
	})
}

func TestRetryLogin_ExponentialBackoff(t *testing.T) {
	t.Run("backoff duration increases between retries", func(t *testing.T) {
		limiter := NewAdaptiveLimiter(10)
		var timestamps []time.Time

		loginFn := func(ctx context.Context, clusterID string) (string, func(), error) {
			timestamps = append(timestamps, time.Now())
			if len(timestamps) < 4 {
				return "", nil, fmt.Errorf("fail")
			}
			return "/kube/config", func() {}, nil
		}

		start := time.Now()
		_, cleanup, err := RetryLogin(context.Background(), "cluster-1", loginFn, limiter, 5)
		if err != nil {
			t.Fatalf("RetryLogin returned error: %v", err)
		}
		cleanup()
		elapsed := time.Since(start)

		// Backoff should produce measurable delays.
		if elapsed < 10*time.Millisecond {
			t.Errorf("elapsed = %v, expected measurable backoff delay", elapsed)
		}

		// With 4 timestamps (3 intervals), each interval should generally not
		// be drastically shorter than the previous (accounting for jitter).
		if len(timestamps) < 3 {
			t.Fatalf("expected at least 3 timestamps, got %d", len(timestamps))
		}

		for i := 2; i < len(timestamps); i++ {
			prev := timestamps[i-1].Sub(timestamps[i-2])
			curr := timestamps[i].Sub(timestamps[i-1])
			// With jitter the current interval should be at least ~25% of previous.
			if curr < prev/4 {
				t.Errorf("interval %d (%v) is much shorter than interval %d (%v); backoff should increase",
					i, curr, i-1, prev)
			}
		}
	})
}

func TestRetryLogin_LimiterInteraction(t *testing.T) {
	t.Run("records failures on the limiter for each failed attempt", func(t *testing.T) {
		limiter := NewAdaptiveLimiter(8)
		var attempts int32

		loginFn := func(ctx context.Context, clusterID string) (string, func(), error) {
			n := atomic.AddInt32(&attempts, 1)
			if n <= 2 {
				return "", nil, fmt.Errorf("fail")
			}
			return "/kube/config", func() {}, nil
		}

		_, cleanup, err := RetryLogin(context.Background(), "cluster-1", loginFn, limiter, 5)
		if err != nil {
			t.Fatalf("RetryLogin returned error: %v", err)
		}
		cleanup()

		// After 2 failures and 1 success:
		// 8 -> 4 (RecordFailure) -> 2 (RecordFailure) -> 3 (RecordSuccess)
		if got := limiter.CurrentLimit(); got != 3 {
			t.Errorf("limit = %d, want 3 (8 -> 4 -> 2 -> 3)", got)
		}
	})

	t.Run("records only failures when all retries exhausted", func(t *testing.T) {
		limiter := NewAdaptiveLimiter(16)

		loginFn := func(ctx context.Context, clusterID string) (string, func(), error) {
			return "", nil, fmt.Errorf("fail")
		}

		_, _, err := RetryLogin(context.Background(), "cluster-1", loginFn, limiter, 3)
		if err == nil {
			t.Fatal("expected error")
		}

		// 4 failures total: 16 -> 8 -> 4 -> 2 -> 1
		if got := limiter.CurrentLimit(); got != 1 {
			t.Errorf("limit = %d, want 1 (16 -> 8 -> 4 -> 2 -> 1)", got)
		}
	})

	t.Run("acquires and releases limiter slot for each attempt", func(t *testing.T) {
		// Use a limiter with limit=1 to verify Acquire/Release pairing.
		// If Release is not called, subsequent Acquires would deadlock.
		limiter := NewAdaptiveLimiter(1)
		var attempts int32

		loginFn := func(ctx context.Context, clusterID string) (string, func(), error) {
			n := atomic.AddInt32(&attempts, 1)
			if n < 3 {
				return "", nil, fmt.Errorf("fail")
			}
			return "/kube/config", func() {}, nil
		}

		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()

		_, cleanup, err := RetryLogin(ctx, "cluster-1", loginFn, limiter, 5)
		if err != nil {
			t.Fatalf("RetryLogin returned error: %v", err)
		}
		cleanup()

		if got := atomic.LoadInt32(&attempts); got != 3 {
			t.Errorf("attempts = %d, want 3", got)
		}
	})
}

func TestRetryLogin_ContextCancellation(t *testing.T) {
	t.Run("stops retrying when context is cancelled", func(t *testing.T) {
		limiter := NewAdaptiveLimiter(10)
		var attempts int32
		ctx, cancel := context.WithCancel(context.Background())

		loginFn := func(ctx context.Context, clusterID string) (string, func(), error) {
			n := atomic.AddInt32(&attempts, 1)
			if n == 2 {
				cancel()
			}
			return "", nil, fmt.Errorf("fail")
		}

		_, _, err := RetryLogin(ctx, "cluster-1", loginFn, limiter, 10)
		if err == nil {
			t.Fatal("expected error on context cancellation")
		}

		// Must have called the login function at least once (stub never calls it).
		got := atomic.LoadInt32(&attempts)
		if got < 1 {
			t.Errorf("attempts = %d, want >= 1 (login function must be called)", got)
		}
		// Should stop soon after cancel (allow at most 3 attempts).
		if got > 3 {
			t.Errorf("attempts = %d, want <= 3 (should stop after context cancel)", got)
		}
	})

	t.Run("returns error when context is already cancelled before first attempt", func(t *testing.T) {
		limiter := NewAdaptiveLimiter(10)
		var attempts int32
		ctx, cancel := context.WithCancel(context.Background())
		cancel()

		loginFn := func(ctx context.Context, clusterID string) (string, func(), error) {
			atomic.AddInt32(&attempts, 1)
			return "/kube/config", func() {}, nil
		}

		_, _, err := RetryLogin(ctx, "cluster-1", loginFn, limiter, 3)
		if err == nil {
			t.Fatal("expected error when context is already cancelled")
		}

		// With an already-cancelled context, the function should either not
		// call login at all (early exit from Acquire) or call it at most once.
		if got := atomic.LoadInt32(&attempts); got > 1 {
			t.Errorf("attempts = %d, want <= 1 with pre-cancelled context", got)
		}
	})
}

func TestRetryLogin_ClusterIDPassedThrough(t *testing.T) {
	t.Run("passes cluster ID to the login function on every attempt", func(t *testing.T) {
		limiter := NewAdaptiveLimiter(10)
		var capturedIDs []string
		var attempts int32

		loginFn := func(ctx context.Context, clusterID string) (string, func(), error) {
			capturedIDs = append(capturedIDs, clusterID)
			n := atomic.AddInt32(&attempts, 1)
			if n < 2 {
				return "", nil, fmt.Errorf("fail")
			}
			return "/kube/config", func() {}, nil
		}

		_, cleanup, err := RetryLogin(context.Background(), "my-cluster-xyz", loginFn, limiter, 3)
		if err != nil {
			t.Fatalf("RetryLogin returned error: %v", err)
		}
		cleanup()

		for i, id := range capturedIDs {
			if id != "my-cluster-xyz" {
				t.Errorf("attempt %d: clusterID = %q, want %q", i+1, id, "my-cluster-xyz")
			}
		}
	})
}

func TestRetryLogin_ReturnsCleanupFromSuccessfulAttempt(t *testing.T) {
	t.Run("cleanup function comes from the successful login attempt", func(t *testing.T) {
		limiter := NewAdaptiveLimiter(10)
		var cleanupCalled bool
		var attempts int32

		loginFn := func(ctx context.Context, clusterID string) (string, func(), error) {
			n := atomic.AddInt32(&attempts, 1)
			if n == 1 {
				return "", nil, fmt.Errorf("fail")
			}
			return "/kube/config", func() { cleanupCalled = true }, nil
		}

		_, cleanup, err := RetryLogin(context.Background(), "cluster-1", loginFn, limiter, 3)
		if err != nil {
			t.Fatalf("RetryLogin returned error: %v", err)
		}

		cleanup()
		if !cleanupCalled {
			t.Error("cleanup from successful attempt was not called")
		}
	})
}
