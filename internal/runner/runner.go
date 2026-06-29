package runner

import (
	"context"
	"fmt"
	"io"
	"time"

	"github.com/squirrd/fleet-scan/internal/backplane"
	"github.com/squirrd/fleet-scan/internal/collector"
	"github.com/squirrd/fleet-scan/internal/ocm"
	"github.com/squirrd/fleet-scan/internal/output"
)

// RecordWriter is the interface the runner uses to write cluster results.
type RecordWriter interface {
	WriteRecord(rec output.ClusterRecord) error
	Finalize(status string, succeeded, failed, skipped int, dur time.Duration) error
}

// BackplaneLoginFunc is the signature for the backplane login function.
type BackplaneLoginFunc func(ctx context.Context, clusterID string) (kubeconfigPath string, cleanup func(), err error)

// RunOptions configures the runner loop.
type RunOptions struct {
	ClusterTimeout time.Duration
	Stderr         io.Writer
	BackplaneLogin BackplaneLoginFunc
	Collectors     []collector.Collector
	LoginLimiter   *backplane.AdaptiveLimiter // optional; gates login concurrency via AIMD
}

// Run iterates over clusters, runs configured collectors against each, and writes
// a ClusterRecord per cluster. It prints [N/Total] progress to opts.Stderr and
// respects context cancellation.
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
		var clusterCleanup func()
		var kubeconfigPath string
		loginFailed := false
		if opts.BackplaneLogin != nil {
			kp, cleanup, loginErr := opts.BackplaneLogin(clusterCtx, cluster.ID)
			clusterCleanup = cleanup
			if loginErr != nil {
				loginFailed = true
				// Mark every configured collector as skipped.
				for _, c := range opts.Collectors {
					rec.ClusterResult[c.Name()] = output.CollectorResult{
						Status: "skipped",
						Error:  loginErr.Error(),
					}
				}
			} else {
				kubeconfigPath = kp
			}
		}

		// Run collectors (unless login failed).
		if !loginFailed {
			for _, c := range opts.Collectors {
				data, runErr := c.Run(clusterCtx, cluster.ID, kubeconfigPath)
				if runErr != nil {
					rec.ClusterResult[c.Name()] = output.CollectorResult{
						Status: "error",
						Error:  runErr.Error(),
					}
				} else {
					rec.ClusterResult[c.Name()] = output.CollectorResult{
						Status: "success",
						Data:   data,
					}
				}
			}
		}

		if err := w.WriteRecord(rec); err != nil {
			if clusterCleanup != nil {
				clusterCleanup()
			}
			cancel()
			return fmt.Errorf("writing record for cluster %s: %w", cluster.ID, err)
		}

		// Clean up kubeconfig for this cluster before moving to the next one.
		if clusterCleanup != nil {
			clusterCleanup()
		}

		// Cancel the per-cluster context (we're done with this cluster).
		cancel()
	}

	return nil
}
