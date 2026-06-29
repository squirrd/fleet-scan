package backplane

import (
	"context"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

func TestAdaptiveLimiter_NewAdaptiveLimiter_InitialState(t *testing.T) {
	tests := []struct {
		name        string
		maxLimit    int
		wantLimit   int
	}{
		{"limit of 1", 1, 1},
		{"limit of 4", 4, 4},
		{"limit of 10", 10, 10},
		{"limit of 100", 100, 100},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			limiter := NewAdaptiveLimiter(tc.maxLimit)
			if got := limiter.CurrentLimit(); got != tc.wantLimit {
				t.Errorf("CurrentLimit() = %d, want %d", got, tc.wantLimit)
			}
		})
	}
}

func TestAdaptiveLimiter_RecordFailure_MultiplicativeDecrease(t *testing.T) {
	tests := []struct {
		name       string
		initial    int
		failures   int
		wantLimit  int
	}{
		{"8 halved once to 4", 8, 1, 4},
		{"8 halved twice to 2", 8, 2, 2},
		{"8 halved three times to 1", 8, 3, 1},
		{"8 halved four times floors at 1", 8, 4, 1},
		{"10 halved once to 5", 10, 1, 5},
		{"10 halved twice to 2 (floor of 2.5)", 10, 2, 2},
		{"3 halved once to 1 (floor of 1.5)", 3, 1, 1},
		{"1 halved stays at 1", 1, 1, 1},
		{"2 halved once to 1", 2, 1, 1},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			limiter := NewAdaptiveLimiter(tc.initial)
			for i := 0; i < tc.failures; i++ {
				limiter.RecordFailure()
			}
			if got := limiter.CurrentLimit(); got != tc.wantLimit {
				t.Errorf("after %d failures: CurrentLimit() = %d, want %d",
					tc.failures, got, tc.wantLimit)
			}
		})
	}
}

func TestAdaptiveLimiter_RecordSuccess_AdditiveIncrease(t *testing.T) {
	t.Run("increases by 1 per success up to max", func(t *testing.T) {
		limiter := NewAdaptiveLimiter(8)

		// Drive limit down to 1.
		for i := 0; i < 10; i++ {
			limiter.RecordFailure()
		}
		if got := limiter.CurrentLimit(); got != 1 {
			t.Fatalf("expected limit to floor at 1, got %d", got)
		}

		// Each success adds 1.
		for want := 2; want <= 8; want++ {
			limiter.RecordSuccess()
			if got := limiter.CurrentLimit(); got != want {
				t.Errorf("after %d successes: CurrentLimit() = %d, want %d",
					want-1, got, want)
			}
		}
	})

	t.Run("does not exceed max limit", func(t *testing.T) {
		limiter := NewAdaptiveLimiter(4)

		// Already at max. Extra successes should be a no-op.
		for i := 0; i < 5; i++ {
			limiter.RecordSuccess()
		}
		if got := limiter.CurrentLimit(); got != 4 {
			t.Errorf("CurrentLimit() = %d, want 4 (max)", got)
		}
	})
}

func TestAdaptiveLimiter_AIMD_Cycle(t *testing.T) {
	t.Run("failure then recovery cycle", func(t *testing.T) {
		limiter := NewAdaptiveLimiter(8)

		// 8 -> 4 (failure)
		limiter.RecordFailure()
		if got := limiter.CurrentLimit(); got != 4 {
			t.Fatalf("after failure: limit = %d, want 4", got)
		}

		// 4 -> 5 (success)
		limiter.RecordSuccess()
		if got := limiter.CurrentLimit(); got != 5 {
			t.Fatalf("after success: limit = %d, want 5", got)
		}

		// 5 -> 2 (failure halves 5 → floor(5/2) = 2)
		limiter.RecordFailure()
		if got := limiter.CurrentLimit(); got != 2 {
			t.Fatalf("after second failure: limit = %d, want 2", got)
		}

		// 2 -> 3 -> 4 -> 5 -> 6 -> 7 -> 8 (successes)
		for want := 3; want <= 8; want++ {
			limiter.RecordSuccess()
			if got := limiter.CurrentLimit(); got != want {
				t.Errorf("recovery: limit = %d, want %d", got, want)
			}
		}
	})
}

func TestAdaptiveLimiter_Acquire_BasicFlow(t *testing.T) {
	t.Run("acquire succeeds when under limit", func(t *testing.T) {
		limiter := NewAdaptiveLimiter(3)

		for i := 0; i < 3; i++ {
			if err := limiter.Acquire(context.Background()); err != nil {
				t.Fatalf("Acquire %d failed: %v", i+1, err)
			}
		}

		// Clean up.
		for i := 0; i < 3; i++ {
			limiter.Release()
		}
	})

	t.Run("acquire blocks when at limit then unblocks on release", func(t *testing.T) {
		limiter := NewAdaptiveLimiter(1)

		if err := limiter.Acquire(context.Background()); err != nil {
			t.Fatalf("first Acquire failed: %v", err)
		}

		unblocked := make(chan struct{})
		go func() {
			if err := limiter.Acquire(context.Background()); err != nil {
				t.Errorf("second Acquire failed: %v", err)
			}
			close(unblocked)
		}()

		// Verify it's blocked.
		time.Sleep(50 * time.Millisecond)
		select {
		case <-unblocked:
			t.Fatal("Acquire should have blocked at limit")
		default:
		}

		limiter.Release()

		select {
		case <-unblocked:
			// Good.
		case <-time.After(2 * time.Second):
			t.Fatal("Acquire did not unblock after Release")
		}

		limiter.Release()
	})
}

func TestAdaptiveLimiter_Acquire_ContextCancellation(t *testing.T) {
	t.Run("returns error when context is already cancelled", func(t *testing.T) {
		limiter := NewAdaptiveLimiter(1)

		if err := limiter.Acquire(context.Background()); err != nil {
			t.Fatalf("first Acquire failed: %v", err)
		}

		ctx, cancel := context.WithCancel(context.Background())
		cancel()

		err := limiter.Acquire(ctx)
		if err == nil {
			t.Fatal("expected error for cancelled context")
		}

		limiter.Release()
	})

	t.Run("returns error when context is cancelled while waiting", func(t *testing.T) {
		limiter := NewAdaptiveLimiter(1)

		if err := limiter.Acquire(context.Background()); err != nil {
			t.Fatalf("first Acquire failed: %v", err)
		}

		ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
		defer cancel()

		err := limiter.Acquire(ctx)
		if err == nil {
			t.Fatal("expected error when context times out while waiting")
		}

		limiter.Release()
	})
}

func TestAdaptiveLimiter_ConcurrencyEnforcement(t *testing.T) {
	t.Run("enforces limit under concurrent access", func(t *testing.T) {
		limiter := NewAdaptiveLimiter(3)

		var peak int64
		var current int64
		var mu sync.Mutex
		var wg sync.WaitGroup

		for i := 0; i < 20; i++ {
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
				time.Sleep(5 * time.Millisecond)
				atomic.AddInt64(&current, -1)
				limiter.Release()
			}()
		}
		wg.Wait()

		if peak > 3 {
			t.Errorf("peak concurrency = %d, want <= 3", peak)
		}
	})

	t.Run("adapts concurrency when limit decreases mid-flight", func(t *testing.T) {
		limiter := NewAdaptiveLimiter(4)

		// Acquire 4 slots.
		for i := 0; i < 4; i++ {
			if err := limiter.Acquire(context.Background()); err != nil {
				t.Fatalf("Acquire %d failed: %v", i, err)
			}
		}

		// Decrease limit to 2 via failures.
		limiter.RecordFailure() // 4 -> 2

		// Release all 4 slots.
		for i := 0; i < 4; i++ {
			limiter.Release()
		}

		// Now only 2 should be acquirable before blocking.
		if err := limiter.Acquire(context.Background()); err != nil {
			t.Fatalf("Acquire after decrease 1 failed: %v", err)
		}
		if err := limiter.Acquire(context.Background()); err != nil {
			t.Fatalf("Acquire after decrease 2 failed: %v", err)
		}

		// Third should block.
		blocked := make(chan struct{})
		go func() {
			if err := limiter.Acquire(context.Background()); err != nil {
				t.Errorf("third Acquire failed: %v", err)
			}
			close(blocked)
		}()

		time.Sleep(50 * time.Millisecond)
		select {
		case <-blocked:
			t.Fatal("third Acquire should have blocked at reduced limit 2")
		default:
		}

		limiter.Release()

		select {
		case <-blocked:
		case <-time.After(2 * time.Second):
			t.Fatal("third Acquire did not unblock after Release")
		}

		limiter.Release()
		limiter.Release()
	})
}

func TestAdaptiveLimiter_ThreadSafety(t *testing.T) {
	t.Run("concurrent RecordFailure and RecordSuccess are safe", func(t *testing.T) {
		limiter := NewAdaptiveLimiter(100)

		var wg sync.WaitGroup
		for i := 0; i < 50; i++ {
			wg.Add(2)
			go func() {
				defer wg.Done()
				limiter.RecordFailure()
			}()
			go func() {
				defer wg.Done()
				limiter.RecordSuccess()
			}()
		}
		wg.Wait()

		// The limit should be between 1 and 100 (exact value depends on
		// execution order, but must not panic or go out of bounds).
		got := limiter.CurrentLimit()
		if got < 1 || got > 100 {
			t.Errorf("CurrentLimit() = %d, want between 1 and 100", got)
		}
	})
}
