package runner

import (
	"context"
	"fmt"
	"sync"
	"sync/atomic"
	"time"

	"github.com/squirrd/fleet-scan/internal/ocm"
	"github.com/squirrd/fleet-scan/internal/output"
)

// Dispatcher dispatches cluster processing to concurrent workers using a
// semaphore (buffered channel) to limit parallelism. It tracks per-cluster
// outcomes via atomic counters: succeeded (all collectors ok), failed (at
// least one collector errored), skipped (backplane login failed).
type Dispatcher struct {
	concurrency int
	opts        RunOptions

	succeeded atomic.Int64
	failed    atomic.Int64
	skipped   atomic.Int64
}

// NewDispatcher creates a Dispatcher that processes clusters with the given
// concurrency level. Use concurrency=1 for serial (backward-compatible) execution.
func NewDispatcher(concurrency int, opts RunOptions) *Dispatcher {
	return &Dispatcher{
		concurrency: concurrency,
		opts:        opts,
	}
}

// Succeeded returns the number of clusters where all collectors succeeded.
func (d *Dispatcher) Succeeded() int { return int(d.succeeded.Load()) }

// Failed returns the number of clusters where at least one collector errored.
func (d *Dispatcher) Failed() int { return int(d.failed.Load()) }

// Skipped returns the number of clusters where backplane login failed.
func (d *Dispatcher) Skipped() int { return int(d.skipped.Load()) }

// Dispatch processes all clusters concurrently (up to d.concurrency at a time),
// writing a ClusterRecord for each one via w. It respects context cancellation:
// in-flight workers finish, but no new clusters are dispatched.
func (d *Dispatcher) Dispatch(ctx context.Context, clusters []ocm.ClusterMetadata, w RecordWriter) error {
	if len(clusters) == 0 {
		return nil
	}

	sem := make(chan struct{}, d.concurrency)
	var wg sync.WaitGroup

	total := len(clusters)

	for i, cluster := range clusters {
		// Check context before dispatching a new cluster.
		if ctx.Err() != nil {
			break
		}

		// Acquire semaphore slot (blocks if all slots are in use).
		// If the context is cancelled while waiting, stop dispatching.
		select {
		case sem <- struct{}{}:
			// Got a slot.
		case <-ctx.Done():
			// Context cancelled while waiting for a slot.
			goto done
		}

		wg.Add(1)
		go func(idx int, cl ocm.ClusterMetadata) {
			defer wg.Done()
			defer func() { <-sem }()

			d.processCluster(ctx, idx, total, cl, w)
		}(i, cluster)
	}

done:
	wg.Wait()

	if ctx.Err() != nil {
		return ctx.Err()
	}
	return nil
}

// processCluster handles a single cluster: applies per-cluster timeout,
// runs backplane login if configured, runs collectors, and writes the record.
func (d *Dispatcher) processCluster(ctx context.Context, idx, total int, cluster ocm.ClusterMetadata, w RecordWriter) {
	start := time.Now()

	// Per-cluster timeout.
	clusterCtx, cancel := context.WithTimeout(ctx, d.opts.ClusterTimeout)
	defer cancel()

	rec := output.ClusterRecord{
		ClusterMetadata: cluster,
		ClusterResult:   map[string]output.CollectorResult{},
	}

	// Backplane login (if configured).
	var clusterCleanup func()
	var kubeconfigPath string
	loginFailed := false
	if d.opts.BackplaneLogin != nil {
		// Acquire login limiter slot if configured.
		if d.opts.LoginLimiter != nil {
			if err := d.opts.LoginLimiter.Acquire(clusterCtx); err != nil {
				// Context cancelled while waiting for limiter slot.
				loginFailed = true
				for _, c := range d.opts.Collectors {
					rec.ClusterResult[c.Name()] = output.CollectorResult{
						Status: "skipped",
						Error:  err.Error(),
					}
				}
			}
		}

		if !loginFailed {
			kp, cleanup, loginErr := d.opts.BackplaneLogin(clusterCtx, cluster.ID)
			clusterCleanup = cleanup

			// Release limiter slot after login completes.
			if d.opts.LoginLimiter != nil {
				d.opts.LoginLimiter.Release()
			}

			if loginErr != nil {
				loginFailed = true
				// Record login failure in the limiter.
				if d.opts.LoginLimiter != nil {
					d.opts.LoginLimiter.RecordFailure()
				}
				for _, c := range d.opts.Collectors {
					rec.ClusterResult[c.Name()] = output.CollectorResult{
						Status: "skipped",
						Error:  loginErr.Error(),
					}
				}
			} else {
				kubeconfigPath = kp
				// Record login success in the limiter.
				if d.opts.LoginLimiter != nil {
					d.opts.LoginLimiter.RecordSuccess()
				}
			}
		}
	}

	// Run collectors (unless login failed).
	hasError := false
	if !loginFailed {
		for _, c := range d.opts.Collectors {
			data, runErr := c.Run(clusterCtx, cluster.ID, kubeconfigPath)
			if runErr != nil {
				hasError = true
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

	// Determine outcome string for progress reporting.
	var outcome string
	switch {
	case loginFailed:
		outcome = "skipped"
		d.skipped.Add(1)
	case hasError:
		outcome = "error"
		d.failed.Add(1)
	default:
		outcome = "ok"
		d.succeeded.Add(1)
	}

	// Write record (RecordWriter implementations must be concurrent-safe).
	_ = w.WriteRecord(rec)

	// Print after-line with outcome and timing.
	elapsed := time.Since(start)
	if d.opts.Stderr != nil {
		fmt.Fprintf(d.opts.Stderr, "[%d/%d] %s (%s, %s)\n",
			idx+1, total, cluster.Name, outcome, formatDuration(elapsed))
	}

	// Clean up kubeconfig.
	if clusterCleanup != nil {
		clusterCleanup()
	}
}

// SignalHandler manages OS signal handling for graceful shutdown.
// First signal cancels the context (allowing in-flight workers to finish),
// second signal force-exits.
type SignalHandler struct {
	ctx    context.Context
	cancel context.CancelFunc
}

// NewSignalHandler creates a SignalHandler with a derived context from the parent.
func NewSignalHandler(parent context.Context) *SignalHandler {
	ctx, cancel := context.WithCancel(parent)
	return &SignalHandler{
		ctx:    ctx,
		cancel: cancel,
	}
}

// Context returns the derived context that will be cancelled on signal.
func (sh *SignalHandler) Context() context.Context {
	return sh.ctx
}

// Stop cancels the derived context and cleans up.
func (sh *SignalHandler) Stop() {
	sh.cancel()
}

// SummaryLine returns a compact summary string for a completed run.
// Format: "Done: N clusters (X ok, Y error, Z skipped) in <dur> -> <dir>/"
func (d *Dispatcher) SummaryLine(total int, dur time.Duration, outputDir string) string {
	return fmt.Sprintf("Done: %d clusters (%d ok, %d error, %d skipped) in %s -> %s/",
		total, d.Succeeded(), d.Failed(), d.Skipped(), formatDuration(dur), outputDir)
}

// InterruptedSummaryLine returns a compact summary string for an interrupted run.
// Format: "Interrupted: N clusters (X ok, Y error, Z skipped) in <dur> -> <dir>/"
func (d *Dispatcher) InterruptedSummaryLine(total int, dur time.Duration, outputDir string) string {
	return fmt.Sprintf("Interrupted: %d clusters (%d ok, %d error, %d skipped) in %s -> %s/",
		total, d.Succeeded(), d.Failed(), d.Skipped(), formatDuration(dur), outputDir)
}

// formatDuration formats a duration in a human-readable way.
func formatDuration(dur time.Duration) string {
	if dur < time.Minute {
		return fmt.Sprintf("%.1fs", dur.Seconds())
	}
	m := int(dur.Minutes())
	s := int(dur.Seconds()) % 60
	return fmt.Sprintf("%dm%02ds", m, s)
}

