package backplane

import (
	"context"
	"math"
	"math/rand"
	"time"
)

// LoginFunc is the signature for a backplane login function.
type LoginFunc func(ctx context.Context, clusterID string) (string, func(), error)

// baseDelay is the initial backoff delay before the first retry.
const baseDelay = 50 * time.Millisecond

// maxDelay caps the backoff to prevent excessively long waits.
const maxDelay = 30 * time.Second

// RetryLogin wraps a login function with retry-with-backoff semantics through
// the adaptive limiter. It makes up to 1 + maxRetries attempts. Each attempt
// acquires a slot from the limiter before calling login and releases it
// afterward. On failure, RecordFailure is called on the limiter and the next
// attempt waits with jittered exponential backoff. On success, RecordSuccess
// is called and the result is returned.
func RetryLogin(ctx context.Context, clusterID string, login LoginFunc, limiter *AdaptiveLimiter, maxRetries int) (string, func(), error) {
	var lastErr error

	for attempt := 0; attempt <= maxRetries; attempt++ {
		// Check context before each attempt.
		if ctx.Err() != nil {
			return "", nil, ctx.Err()
		}

		// Wait with backoff before retries (not before the first attempt).
		if attempt > 0 {
			delay := backoffDelay(attempt)
			select {
			case <-ctx.Done():
				return "", nil, ctx.Err()
			case <-time.After(delay):
			}
		}

		// Acquire a slot from the limiter.
		if err := limiter.Acquire(ctx); err != nil {
			return "", nil, err
		}

		path, cleanup, err := login(ctx, clusterID)
		limiter.Release()

		if err == nil {
			limiter.RecordSuccess()
			return path, cleanup, nil
		}

		limiter.RecordFailure()
		lastErr = err
	}

	return "", nil, lastErr
}

// backoffDelay returns a jittered exponential backoff duration for the given
// retry attempt (1-indexed). The delay is baseDelay * 2^(attempt-1) with
// uniform jitter in [0.5*delay, 1.5*delay], capped at maxDelay.
func backoffDelay(attempt int) time.Duration {
	exp := math.Pow(2, float64(attempt-1))
	delay := time.Duration(float64(baseDelay) * exp)
	if delay > maxDelay {
		delay = maxDelay
	}
	// Apply jitter: [0.5*delay, 1.5*delay].
	jitter := 0.5 + rand.Float64() // [0.5, 1.5)
	return time.Duration(float64(delay) * jitter)
}
