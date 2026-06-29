package backplane

import (
	"context"
	"fmt"
)

// LoginFunc is the signature for a backplane login function.
type LoginFunc func(ctx context.Context, clusterID string) (string, func(), error)

// RetryLogin wraps a login function with retry-with-backoff semantics through
// the adaptive limiter. This is a stub; the full implementation is in a
// separate slice.
func RetryLogin(ctx context.Context, clusterID string, login LoginFunc, limiter *AdaptiveLimiter, maxRetries int) (string, func(), error) {
	_ = limiter
	_ = maxRetries
	return "", nil, fmt.Errorf("RetryLogin not yet implemented")
}
