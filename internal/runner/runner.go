package runner

import (
	"context"
	"fmt"
	"io"
	"time"

	"github.com/squirrd/fleet-scan/internal/ocm"
	"github.com/squirrd/fleet-scan/internal/output"
)

// RecordWriter is the interface the runner uses to write cluster results.
type RecordWriter interface {
	WriteRecord(rec output.ClusterRecord) error
	Finalize(status string, succeeded, failed, skipped int, dur time.Duration) error
}

// BackplaneLoginFunc is the signature for the backplane login function.
// It takes a clusterID and returns the kubeconfigPath, a cleanup function, and an error.
type BackplaneLoginFunc func(clusterID string) (kubeconfigPath string, cleanup func(), err error)

// RunOptions configures the runner loop.
type RunOptions struct {
	ClusterTimeout time.Duration
	Stderr         io.Writer
	BackplaneLogin BackplaneLoginFunc
}

// Run iterates over clusters, writing a stub ClusterRecord for each.
// It prints [N/Total] progress to opts.Stderr and respects context cancellation.
func Run(ctx context.Context, clusters []ocm.ClusterMetadata, w RecordWriter, opts RunOptions) error {
	total := len(clusters)

	for i, cluster := range clusters {
		// Check context before each cluster.
		if ctx.Err() != nil {
			return ctx.Err()
		}

		// Per-cluster timeout.
		clusterCtx, cancel := context.WithTimeout(ctx, opts.ClusterTimeout)

		// Print progress.
		if opts.Stderr != nil {
			fmt.Fprintf(opts.Stderr, "[%d/%d] %s (%s)\n", i+1, total, cluster.Name, cluster.ID)
		}

		rec := output.ClusterRecord{
			ClusterMetadata: cluster,
			ClusterResult:   map[string]output.CollectorResult{},
		}

		// Backplane login (if configured).
		loginFailed := false
		if opts.BackplaneLogin != nil {
			kubeconfigPath, cleanup, loginErr := opts.BackplaneLogin(cluster.ID)
			if cleanup != nil {
				defer cleanup()
			}
			if loginErr != nil {
				loginFailed = true
				rec.ClusterResult["login_error"] = output.CollectorResult{
					Status: "skipped",
					Error:  loginErr.Error(),
				}
			} else {
				// kubeconfigPath available for future collector use.
				_ = kubeconfigPath
			}
		}

		// Skip collector execution on login failure (future: run collectors here).
		_ = loginFailed

		if err := w.WriteRecord(rec); err != nil {
			cancel()
			return fmt.Errorf("writing record for cluster %s: %w", cluster.ID, err)
		}

		// Cancel the per-cluster context (we're done with this cluster).
		cancel()

		// Check the cluster context for timeout (for future use when collectors run).
		_ = clusterCtx
	}

	return nil
}
