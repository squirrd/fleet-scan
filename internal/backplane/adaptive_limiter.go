package backplane

import (
	"context"
	"sync"
)

// AdaptiveLimiter implements AIMD (Additive Increase / Multiplicative Decrease)
// concurrency control. The limit starts at maxLimit and adjusts dynamically:
//   - RecordFailure halves the limit (floor division, minimum 1)
//   - RecordSuccess increments the limit by 1 (up to maxLimit)
//
// Acquire blocks until an inflight slot is available or the context is cancelled.
// Release frees a slot.
type AdaptiveLimiter struct {
	mu       sync.Mutex
	maxLimit int
	limit    int
	inflight int
	waiters  []chan struct{}
}

// NewAdaptiveLimiter creates an AdaptiveLimiter with the given maximum (and
// initial) concurrency limit.
func NewAdaptiveLimiter(maxLimit int) *AdaptiveLimiter {
	if maxLimit < 1 {
		maxLimit = 1
	}
	return &AdaptiveLimiter{
		maxLimit: maxLimit,
		limit:    maxLimit,
	}
}

// CurrentLimit returns the current concurrency limit.
func (l *AdaptiveLimiter) CurrentLimit() int {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.limit
}

// RecordFailure applies multiplicative decrease: halves the limit (minimum 1).
func (l *AdaptiveLimiter) RecordFailure() {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.limit /= 2
	if l.limit < 1 {
		l.limit = 1
	}
}

// RecordSuccess applies additive increase: increments the limit by 1 (up to max).
// If the new limit frees a slot, one blocked Acquire waiter is woken.
func (l *AdaptiveLimiter) RecordSuccess() {
	l.mu.Lock()
	defer l.mu.Unlock()
	if l.limit < l.maxLimit {
		l.limit++
	}
	l.wakeWaitersLocked()
}

// Acquire blocks until an inflight slot is available or ctx is cancelled.
// Returns ctx.Err() on cancellation.
func (l *AdaptiveLimiter) Acquire(ctx context.Context) error {
	l.mu.Lock()
	if l.inflight < l.limit {
		l.inflight++
		l.mu.Unlock()
		return nil
	}

	// Must wait for a slot.
	ch := make(chan struct{}, 1)
	l.waiters = append(l.waiters, ch)
	l.mu.Unlock()

	select {
	case <-ch:
		return nil
	case <-ctx.Done():
		// Remove ourselves from the waiter list if we were not yet signalled.
		l.mu.Lock()
		for i, w := range l.waiters {
			if w == ch {
				l.waiters = append(l.waiters[:i], l.waiters[i+1:]...)
				break
			}
		}
		l.mu.Unlock()
		return ctx.Err()
	}
}

// Release frees one inflight slot. If waiters are blocked and there is now
// capacity, one waiter is woken.
func (l *AdaptiveLimiter) Release() {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.inflight--
	l.wakeWaitersLocked()
}

// wakeWaitersLocked wakes blocked Acquire callers while there is capacity.
// Caller must hold l.mu.
func (l *AdaptiveLimiter) wakeWaitersLocked() {
	for len(l.waiters) > 0 && l.inflight < l.limit {
		ch := l.waiters[0]
		l.waiters = l.waiters[1:]
		l.inflight++
		ch <- struct{}{}
	}
}
