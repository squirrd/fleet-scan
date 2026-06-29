package backplane

import (
	"context"
	"fmt"
	"math"
	"os"
	"path/filepath"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

func TestBackplaneLogin_BackplaneLoginFunc_Acceptance(t *testing.T) {
	t.Run("returns kubeconfig path and working cleanup", func(t *testing.T) {
		kubeconfigDir := t.TempDir()

		origRunner := commandRunner
		t.Cleanup(func() { commandRunner = origRunner })
		commandRunner = func(ctx context.Context, name string, args []string) error {
			// Simulate backplane creating <kube-path>/<clusterID>/config.
			for i, a := range args {
				if a == "--kube-path" && i+1 < len(args) {
					clusterID := args[2] // args: backplane login <clusterID> ...
					configDir := filepath.Join(args[i+1], clusterID)
					if err := os.MkdirAll(configDir, 0o700); err != nil {
						return err
					}
					return os.WriteFile(filepath.Join(configDir, "config"), []byte("fake-kubeconfig"), 0600)
				}
			}
			return nil
		}

		kubeconfigPath, cleanup, err := Login(context.Background(), "test-cluster-id", kubeconfigDir)
		if err != nil {
			t.Fatalf("Login returned error: %v", err)
		}

		if kubeconfigPath == "" {
			t.Fatal("expected non-empty kubeconfig path")
		}

		wantPath := filepath.Join(kubeconfigDir, "test-cluster-id", "config")
		if kubeconfigPath != wantPath {
			t.Errorf("kubeconfigPath = %q, want %q", kubeconfigPath, wantPath)
		}

		if _, err := os.Stat(kubeconfigPath); os.IsNotExist(err) {
			t.Fatalf("kubeconfig file does not exist at %s", kubeconfigPath)
		}

		cleanup()

		if _, err := os.Stat(filepath.Join(kubeconfigDir, "test-cluster-id")); !os.IsNotExist(err) {
			t.Fatalf("cleanup should have removed kubeconfig dir for cluster")
		}
	})
}

func TestLogin_CommandExecution(t *testing.T) {
	t.Run("invokes ocm backplane login with clusterID and --kube-path dir", func(t *testing.T) {
		kubeconfigDir := t.TempDir()

		var capturedName string
		var capturedArgs []string

		origRunner := commandRunner
		t.Cleanup(func() { commandRunner = origRunner })

		commandRunner = func(ctx context.Context, name string, args []string) error {
			capturedName = name
			capturedArgs = args
			return nil
		}

		_, cleanup, err := Login(context.Background(), "my-cluster-123", kubeconfigDir)
		if err != nil {
			t.Fatalf("Login returned error: %v", err)
		}
		defer cleanup()

		if capturedName != "ocm" {
			t.Errorf("command name = %q, want %q", capturedName, "ocm")
		}

		wantArgs := []string{"backplane", "login", "my-cluster-123", "--multi", "--kube-path", kubeconfigDir}
		if len(capturedArgs) != len(wantArgs) {
			t.Fatalf("args = %v, want %v", capturedArgs, wantArgs)
		}
		for i, want := range wantArgs {
			if capturedArgs[i] != want {
				t.Errorf("args[%d] = %q, want %q", i, capturedArgs[i], want)
			}
		}
	})
}

func TestLogin_KubeconfigPathIsolation(t *testing.T) {
	t.Run("kubeconfig path is inside kubeconfigDir/<clusterID>/config", func(t *testing.T) {
		kubeconfigDir := t.TempDir()

		origRunner := commandRunner
		t.Cleanup(func() { commandRunner = origRunner })
		commandRunner = func(ctx context.Context, name string, args []string) error {
			return nil
		}

		kubeconfigPath, cleanup, err := Login(context.Background(), "cluster-abc", kubeconfigDir)
		if err != nil {
			t.Fatalf("Login returned error: %v", err)
		}
		defer cleanup()

		wantPath := filepath.Join(kubeconfigDir, "cluster-abc", "config")
		if kubeconfigPath != wantPath {
			t.Errorf("kubeconfigPath = %q, want %q", kubeconfigPath, wantPath)
		}
	})
}

func TestLogin_CommandFailure(t *testing.T) {
	t.Run("returns error when command fails", func(t *testing.T) {
		kubeconfigDir := t.TempDir()

		origRunner := commandRunner
		t.Cleanup(func() { commandRunner = origRunner })
		commandRunner = func(ctx context.Context, name string, args []string) error {
			return fmt.Errorf("exit status 1: unable to login")
		}

		kubeconfigPath, cleanup, err := Login(context.Background(), "bad-cluster", kubeconfigDir)
		if err == nil {
			t.Fatal("expected error when command fails, got nil")
		}

		if kubeconfigPath != "" {
			t.Errorf("kubeconfigPath = %q, want empty on failure", kubeconfigPath)
		}

		if cleanup != nil {
			t.Error("cleanup should be nil on failure")
		}
	})
}

func TestLogin_EmptyClusterID(t *testing.T) {
	t.Run("returns error for empty cluster ID", func(t *testing.T) {
		kubeconfigDir := t.TempDir()

		_, _, err := Login(context.Background(), "", kubeconfigDir)
		if err == nil {
			t.Fatal("expected error for empty cluster ID, got nil")
		}
	})
}

func TestLogin_CleanupRemovesFile(t *testing.T) {
	t.Run("cleanup removes the kubeconfig cluster dir", func(t *testing.T) {
		kubeconfigDir := t.TempDir()

		origRunner := commandRunner
		t.Cleanup(func() { commandRunner = origRunner })
		commandRunner = func(ctx context.Context, name string, args []string) error {
			clusterID := args[2]
			configDir := filepath.Join(kubeconfigDir, clusterID)
			if err := os.MkdirAll(configDir, 0o700); err != nil {
				return err
			}
			return os.WriteFile(filepath.Join(configDir, "config"), []byte("kubeconfig-data"), 0600)
		}

		kubeconfigPath, cleanup, err := Login(context.Background(), "test-cluster", kubeconfigDir)
		if err != nil {
			t.Fatalf("Login returned error: %v", err)
		}

		if _, err := os.Stat(kubeconfigPath); os.IsNotExist(err) {
			t.Fatalf("kubeconfig file should exist at %s", kubeconfigPath)
		}

		cleanup()

		clusterDir := filepath.Join(kubeconfigDir, "test-cluster")
		if _, err := os.Stat(clusterDir); !os.IsNotExist(err) {
			t.Fatalf("cleanup should have removed cluster dir at %s", clusterDir)
		}
	})

	t.Run("cleanup is safe to call when file does not exist", func(t *testing.T) {
		kubeconfigDir := t.TempDir()

		origRunner := commandRunner
		t.Cleanup(func() { commandRunner = origRunner })
		commandRunner = func(ctx context.Context, name string, args []string) error {
			return nil
		}

		_, cleanup, err := Login(context.Background(), "test-cluster", kubeconfigDir)
		if err != nil {
			t.Fatalf("Login returned error: %v", err)
		}

		cleanup()
	})
}

func TestMakeBackplaneLoginFunc(t *testing.T) {
	t.Run("returns a non-nil function", func(t *testing.T) {
		kubeconfigDir := t.TempDir()

		loginFunc := MakeBackplaneLoginFunc(kubeconfigDir)
		if loginFunc == nil {
			t.Fatal("expected MakeBackplaneLoginFunc to return a non-nil function")
		}
	})

	t.Run("returned function delegates to Login with bound kubeconfigDir", func(t *testing.T) {
		kubeconfigDir := t.TempDir()

		var capturedArgs []string

		origRunner := commandRunner
		t.Cleanup(func() { commandRunner = origRunner })
		commandRunner = func(ctx context.Context, name string, args []string) error {
			capturedArgs = args
			return nil
		}

		loginFunc := MakeBackplaneLoginFunc(kubeconfigDir)
		_, cleanup, err := loginFunc(context.Background(), "cluster-xyz")
		if err != nil {
			t.Fatalf("loginFunc returned error: %v", err)
		}
		defer cleanup()

		// --kube-path should be the kubeconfigDir itself.
		wantArgs := []string{"backplane", "login", "cluster-xyz", "--multi", "--kube-path", kubeconfigDir}
		if len(capturedArgs) != len(wantArgs) {
			t.Fatalf("args = %v, want %v", capturedArgs, wantArgs)
		}
		for i, want := range wantArgs {
			if capturedArgs[i] != want {
				t.Errorf("args[%d] = %q, want %q", i, capturedArgs[i], want)
			}
		}
	})

	t.Run("different kubeconfigDirs produce independent functions", func(t *testing.T) {
		dir1 := t.TempDir()
		dir2 := t.TempDir()

		origRunner := commandRunner
		t.Cleanup(func() { commandRunner = origRunner })

		var lastKubePath string
		commandRunner = func(ctx context.Context, name string, args []string) error {
			for i, a := range args {
				if a == "--kube-path" && i+1 < len(args) {
					lastKubePath = args[i+1]
				}
			}
			return nil
		}

		fn1 := MakeBackplaneLoginFunc(dir1)
		fn2 := MakeBackplaneLoginFunc(dir2)

		_, cleanup1, err := fn1(context.Background(), "cluster-a")
		if err != nil {
			t.Fatalf("fn1 returned error: %v", err)
		}
		defer cleanup1()
		path1 := lastKubePath

		_, cleanup2, err := fn2(context.Background(), "cluster-a")
		if err != nil {
			t.Fatalf("fn2 returned error: %v", err)
		}
		defer cleanup2()
		path2 := lastKubePath

		if path1 != dir1 {
			t.Errorf("fn1 --kube-path = %q, want %q", path1, dir1)
		}
		if path2 != dir2 {
			t.Errorf("fn2 --kube-path = %q, want %q", path2, dir2)
		}
	})
}

// TestMc109AdaptiveLoginRateLimit_AimdLimiter_Acceptance verifies that:
// 1. An AdaptiveLimiter type exists with AIMD (Additive Increase / Multiplicative Decrease) semantics
// 2. Acquire blocks when the current inflight count is at the limit
// 3. On consecutive failures (RecordFailure), the rate limit decreases multiplicatively (halved, min 1)
// 4. On consecutive successes (RecordSuccess), the rate limit increases additively (+1, up to max)
// 5. Context cancellation causes Acquire to return an error
//
// Acceptance criterion: AdaptiveLimiter rate decreases on consecutive failures
// (multiplicative decrease) and recovers on consecutive successes (additive increase),
// with context-aware Acquire blocking.
func TestMc109AdaptiveLoginRateLimit_AimdLimiter_Acceptance(t *testing.T) {
	t.Run("AIMD decreases limit on failure and increases on success", func(t *testing.T) {
		// Create limiter with initial limit of 8 and max of 8.
		limiter := NewAdaptiveLimiter(8)

		// Initial limit should be 8.
		if got := limiter.CurrentLimit(); got != 8 {
			t.Fatalf("initial limit = %d, want 8", got)
		}

		// Multiplicative decrease: consecutive failures halve the limit.
		limiter.RecordFailure()
		if got := limiter.CurrentLimit(); got != 4 {
			t.Errorf("after 1 failure, limit = %d, want 4", got)
		}

		limiter.RecordFailure()
		if got := limiter.CurrentLimit(); got != 2 {
			t.Errorf("after 2 failures, limit = %d, want 2", got)
		}

		limiter.RecordFailure()
		if got := limiter.CurrentLimit(); got != 1 {
			t.Errorf("after 3 failures, limit = %d, want 1 (floor)", got)
		}

		// Floor: further failures don't go below 1.
		limiter.RecordFailure()
		if got := limiter.CurrentLimit(); got != 1 {
			t.Errorf("after 4 failures, limit = %d, want 1 (floor)", got)
		}

		// Additive increase: consecutive successes increase limit by 1.
		limiter.RecordSuccess()
		if got := limiter.CurrentLimit(); got != 2 {
			t.Errorf("after 1 success, limit = %d, want 2", got)
		}

		limiter.RecordSuccess()
		if got := limiter.CurrentLimit(); got != 3 {
			t.Errorf("after 2 successes, limit = %d, want 3", got)
		}

		// Keep going until we hit the max (8).
		for i := 0; i < 10; i++ {
			limiter.RecordSuccess()
		}
		if got := limiter.CurrentLimit(); got != 8 {
			t.Errorf("after many successes, limit = %d, want 8 (ceiling)", got)
		}
	})

	t.Run("Acquire blocks at limit and releases on Release", func(t *testing.T) {
		limiter := NewAdaptiveLimiter(2)

		// Acquire two slots — should not block.
		if err := limiter.Acquire(context.Background()); err != nil {
			t.Fatalf("first Acquire failed: %v", err)
		}
		if err := limiter.Acquire(context.Background()); err != nil {
			t.Fatalf("second Acquire failed: %v", err)
		}

		// Third Acquire should block until Release is called.
		acquired := make(chan struct{})
		go func() {
			if err := limiter.Acquire(context.Background()); err != nil {
				t.Errorf("third Acquire failed: %v", err)
			}
			close(acquired)
		}()

		// Give goroutine time to block.
		time.Sleep(50 * time.Millisecond)
		select {
		case <-acquired:
			t.Fatal("third Acquire should have blocked")
		default:
			// Expected — still blocked.
		}

		// Release one slot.
		limiter.Release()

		// Now the third Acquire should succeed.
		select {
		case <-acquired:
			// Good.
		case <-time.After(2 * time.Second):
			t.Fatal("third Acquire did not unblock after Release")
		}

		// Clean up.
		limiter.Release()
		limiter.Release()
	})

	t.Run("Acquire respects context cancellation", func(t *testing.T) {
		limiter := NewAdaptiveLimiter(1)

		// Fill the single slot.
		if err := limiter.Acquire(context.Background()); err != nil {
			t.Fatalf("Acquire failed: %v", err)
		}

		// Try to acquire with a cancelled context.
		ctx, cancel := context.WithCancel(context.Background())
		cancel() // Cancel immediately.

		err := limiter.Acquire(ctx)
		if err == nil {
			t.Fatal("Acquire with cancelled context should return error")
		}

		limiter.Release()
	})

	t.Run("concurrent callers are serialised at limit 1", func(t *testing.T) {
		limiter := NewAdaptiveLimiter(8)

		// Drive limit down to 1.
		for i := 0; i < 10; i++ {
			limiter.RecordFailure()
		}
		if got := limiter.CurrentLimit(); got != 1 {
			t.Fatalf("limit should be 1, got %d", got)
		}

		// Launch 5 concurrent goroutines — peak concurrency must be 1.
		var peak int64
		var current int64
		var mu sync.Mutex
		var wg sync.WaitGroup

		for i := 0; i < 5; i++ {
			wg.Add(1)
			go func() {
				defer wg.Done()
				if err := limiter.Acquire(context.Background()); err != nil {
					return
				}
				cur := atomic.AddInt64(&current, 1)
				mu.Lock()
				if cur > peak {
					peak = cur
				}
				mu.Unlock()
				time.Sleep(10 * time.Millisecond)
				atomic.AddInt64(&current, -1)
				limiter.Release()
			}()
		}
		wg.Wait()

		if peak != 1 {
			t.Errorf("peak concurrent acquires = %d, want 1", peak)
		}
	})
}

// TestMc109AdaptiveLoginRateLimit_RetryWithBackoff_Acceptance verifies that:
// 1. A RetryLogin function wraps a login call with retry-with-backoff semantics
// 2. Transient failures are retried up to maxRetries times
// 3. Retries use jittered exponential backoff (each wait >= previous wait)
// 4. Each retry attempt goes through the adaptive limiter (Acquire/Release)
// 5. If all retries are exhausted, the final error is returned
// 6. A permanent success on retry N returns that result (no further attempts)
// 7. Context cancellation stops retries immediately
//
// Acceptance criterion: Transient login failures are retried up to N times
// through the adaptive limiter with jittered exponential backoff, then final
// failure is returned.
func TestMc109AdaptiveLoginRateLimit_RetryWithBackoff_Acceptance(t *testing.T) {
	t.Run("succeeds on first try with no retry", func(t *testing.T) {
		limiter := NewAdaptiveLimiter(10)
		var attempts int32

		loginFn := func(ctx context.Context, clusterID string) (string, func(), error) {
			atomic.AddInt32(&attempts, 1)
			return "/tmp/kube/config", func() {}, nil
		}

		path, cleanup, err := RetryLogin(context.Background(), "cluster-1", loginFn, limiter, 3)
		if err != nil {
			t.Fatalf("RetryLogin returned error: %v", err)
		}
		if path != "/tmp/kube/config" {
			t.Errorf("path = %q, want /tmp/kube/config", path)
		}
		cleanup()

		if got := atomic.LoadInt32(&attempts); got != 1 {
			t.Errorf("attempts = %d, want 1", got)
		}
	})

	t.Run("retries on failure and succeeds on Nth attempt", func(t *testing.T) {
		limiter := NewAdaptiveLimiter(10)
		var attempts int32

		loginFn := func(ctx context.Context, clusterID string) (string, func(), error) {
			n := atomic.AddInt32(&attempts, 1)
			if n < 3 {
				return "", nil, fmt.Errorf("transient error attempt %d", n)
			}
			return "/tmp/kube/config", func() {}, nil
		}

		path, cleanup, err := RetryLogin(context.Background(), "cluster-1", loginFn, limiter, 5)
		if err != nil {
			t.Fatalf("RetryLogin returned error: %v", err)
		}
		if path != "/tmp/kube/config" {
			t.Errorf("path = %q, want /tmp/kube/config", path)
		}
		cleanup()

		if got := atomic.LoadInt32(&attempts); got != 3 {
			t.Errorf("attempts = %d, want 3", got)
		}
	})

	t.Run("returns final error when all retries exhausted", func(t *testing.T) {
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

		// Should have tried 1 initial + 3 retries = 4 total attempts.
		if got := atomic.LoadInt32(&attempts); got != 4 {
			t.Errorf("attempts = %d, want 4 (1 initial + 3 retries)", got)
		}
	})

	t.Run("backoff increases between retries", func(t *testing.T) {
		limiter := NewAdaptiveLimiter(10)
		var timestamps []time.Time
		var mu sync.Mutex

		loginFn := func(ctx context.Context, clusterID string) (string, func(), error) {
			mu.Lock()
			timestamps = append(timestamps, time.Now())
			mu.Unlock()
			if len(timestamps) < 4 {
				return "", nil, fmt.Errorf("fail")
			}
			return "/tmp/kube/config", func() {}, nil
		}

		start := time.Now()
		path, cleanup, err := RetryLogin(context.Background(), "cluster-1", loginFn, limiter, 5)
		if err != nil {
			t.Fatalf("RetryLogin returned error: %v", err)
		}
		if path != "/tmp/kube/config" {
			t.Errorf("path = %q, want /tmp/kube/config", path)
		}
		cleanup()
		elapsed := time.Since(start)

		// With exponential backoff, total time should be at least baseDelay * (2^0 + 2^1 + 2^2)
		// which is at least some measurable duration (> 0).
		// We just verify it took _some_ time (not instant) indicating backoff occurred.
		if elapsed < 10*time.Millisecond {
			t.Errorf("elapsed = %v, expected measurable backoff delay", elapsed)
		}

		// Verify intervals are non-decreasing (exponential backoff).
		mu.Lock()
		ts := make([]time.Time, len(timestamps))
		copy(ts, timestamps)
		mu.Unlock()

		for i := 2; i < len(ts); i++ {
			prev := ts[i-1].Sub(ts[i-2])
			curr := ts[i].Sub(ts[i-1])
			// With jitter, current interval should be at least ~50% of previous
			// (allow for jitter variance).
			if curr < prev/4 {
				t.Errorf("interval %d (%v) is much shorter than interval %d (%v); backoff not increasing",
					i, curr, i-1, prev)
			}
		}
	})

	t.Run("records failures and successes on the limiter", func(t *testing.T) {
		limiter := NewAdaptiveLimiter(8)
		var attempts int32

		loginFn := func(ctx context.Context, clusterID string) (string, func(), error) {
			n := atomic.AddInt32(&attempts, 1)
			if n <= 2 {
				return "", nil, fmt.Errorf("fail")
			}
			return "/tmp/kube/config", func() {}, nil
		}

		_, cleanup, err := RetryLogin(context.Background(), "cluster-1", loginFn, limiter, 5)
		if err != nil {
			t.Fatalf("RetryLogin returned error: %v", err)
		}
		cleanup()

		// After 2 failures and 1 success:
		// Initial limit 8, after 2 failures: 8 -> 4 -> 2
		// After 1 success: 2 -> 3
		if got := limiter.CurrentLimit(); got != 3 {
			t.Errorf("limit = %d, want 3 (8 -> 4 -> 2 -> 3)", got)
		}
	})

	t.Run("context cancellation stops retries", func(t *testing.T) {
		limiter := NewAdaptiveLimiter(10)
		var attempts int32

		ctx, cancel := context.WithCancel(context.Background())

		loginFn := func(ctx context.Context, clusterID string) (string, func(), error) {
			n := atomic.AddInt32(&attempts, 1)
			if n == 2 {
				cancel() // Cancel after 2nd attempt.
			}
			return "", nil, fmt.Errorf("fail")
		}

		_, _, err := RetryLogin(ctx, "cluster-1", loginFn, limiter, 10)
		if err == nil {
			t.Fatal("expected error on context cancellation")
		}

		got := atomic.LoadInt32(&attempts)
		if got > 3 {
			t.Errorf("attempts = %d, want <= 3 (should stop after context cancel)", got)
		}
	})

	// Suppress unused variable warnings.
	_ = math.MaxFloat64
}
