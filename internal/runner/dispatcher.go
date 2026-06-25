package runner

import (
	"context"
	"fmt"
	"sync"

	"github.com/squirrd/fleet-scan/internal/ocm"
	"github.com/squirrd/fleet-scan/internal/output"
)

// Dispatcher dispatches cluster processing to concurrent workers using a
// semaphore (buffered channel) to limit parallelism.
type Dispatcher struct {
	concurrency int
	opts        RunOptions
}

// NewDispatcher creates a Dispatcher that processes clusters with the given
// concurrency level. Use concurrency=1 for serial (backward-compatible) execution.
func NewDispatcher(concurrency int, opts RunOptions) *Dispatcher {
	return &Dispatcher{
		concurrency: concurrency,
		opts:        opts,
	}
}

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
	// Per-cluster timeout.
	clusterCtx, cancel := context.WithTimeout(ctx, d.opts.ClusterTimeout)
	defer cancel()

	// Print progress.
	if d.opts.Stderr != nil {
		fmt.Fprintf(d.opts.Stderr, "[%d/%d] %s (%s)\n", idx+1, total, cluster.Name, cluster.ID)
	}

	rec := output.ClusterRecord{
		ClusterMetadata: cluster,
		ClusterResult:   map[string]output.CollectorResult{},
	}

	// Backplane login (if configured).
	var clusterCleanup func()
	var kubeconfigPath string
	loginFailed := false
	if d.opts.BackplaneLogin != nil {
		kp, cleanup, loginErr := d.opts.BackplaneLogin(clusterCtx, cluster.ID)
		clusterCleanup = cleanup
		if loginErr != nil {
			loginFailed = true
			for _, c := range d.opts.Collectors {
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
		for _, c := range d.opts.Collectors {
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

	// Write record (RecordWriter implementations must be concurrent-safe).
	_ = w.WriteRecord(rec)

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

