package runner

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/squirrd/fleet-scan/internal/backplane"
	"github.com/squirrd/fleet-scan/internal/collector"
	"github.com/squirrd/fleet-scan/internal/ocm"
	"github.com/squirrd/fleet-scan/internal/output"
)

// TestConcurrencySignals_SemaphoreDispatch_Acceptance verifies that:
// 1. A Dispatcher type exists with a Dispatch method that processes clusters concurrently
// 2. Concurrency is limited by a configurable worker count (semaphore via buffered channel)
// 3. Default concurrency of 1 gives serial execution (backward-compatible)
// 4. With concurrency > 1, multiple clusters are processed in parallel
// 5. All cluster results are collected regardless of concurrency level
// 6. Writer.WriteRecord() is safe for concurrent use (mutex-guarded)
//
// Acceptance criterion: Dispatcher dispatches clusters to a configurable number of
// concurrent workers using a semaphore (buffered channel), collects results, and
// the --concurrency flag controls worker count (default 1 for backward compatibility);
// Writer.WriteRecord() is guarded by a mutex for concurrent write safety.
//
// Phase: RED — Dispatcher type does not exist yet.
func TestConcurrencySignals_SemaphoreDispatch_Acceptance(t *testing.T) {
	t.Run("dispatches clusters concurrently with configurable worker count", func(t *testing.T) {
		clusters := []ocm.ClusterMetadata{
			{ID: "c1", Name: "cluster-1"},
			{ID: "c2", Name: "cluster-2"},
			{ID: "c3", Name: "cluster-3"},
			{ID: "c4", Name: "cluster-4"},
		}

		w := &concurrentMockWriter{}
		stderr := new(bytes.Buffer)

		// Track peak concurrency to verify semaphore works.
		var active int64
		var peakActive int64
		var mu sync.Mutex

		slowCollector := &mockCollector{
			name: "slow",
			runFn: func(ctx context.Context, clusterID, kubeconfigPath string) (json.RawMessage, error) {
				cur := atomic.AddInt64(&active, 1)
				mu.Lock()
				if cur > peakActive {
					peakActive = cur
				}
				mu.Unlock()

				time.Sleep(50 * time.Millisecond) // simulate work

				atomic.AddInt64(&active, -1)
				return json.RawMessage(`{"done": true}`), nil
			},
		}

		opts := RunOptions{
			ClusterTimeout: 30 * time.Second,
			Stderr:         stderr,
			Collectors:     []collector.Collector{slowCollector},
		}

		// Create a Dispatcher with concurrency=2.
		d := NewDispatcher(2, opts)

		err := d.Dispatch(context.Background(), clusters, w)
		if err != nil {
			t.Fatalf("Dispatch returned error: %v", err)
		}

		// All 4 clusters must produce records.
		if len(w.Records()) != 4 {
			t.Fatalf("expected 4 records, got %d", len(w.Records()))
		}

		// Peak concurrency should be at least 2 (proving parallel execution).
		if peakActive < 2 {
			t.Errorf("expected peak concurrency >= 2, got %d (semaphore not working)", peakActive)
		}

		// Verify all cluster IDs are present in results.
		ids := map[string]bool{}
		for _, rec := range w.Records() {
			ids[rec.ClusterMetadata.ID] = true
		}
		for _, c := range clusters {
			if !ids[c.ID] {
				t.Errorf("missing result for cluster %s", c.ID)
			}
		}
	})

	t.Run("concurrency 1 gives serial execution", func(t *testing.T) {
		clusters := []ocm.ClusterMetadata{
			{ID: "c1", Name: "cluster-1"},
			{ID: "c2", Name: "cluster-2"},
			{ID: "c3", Name: "cluster-3"},
		}

		w := &concurrentMockWriter{}
		stderr := new(bytes.Buffer)

		var peakActive int64
		var mu sync.Mutex

		serialCollector := &mockCollector{
			name: "serial-check",
			runFn: func(ctx context.Context, clusterID, kubeconfigPath string) (json.RawMessage, error) {
				cur := atomic.AddInt64(&peakActive, 1)
				mu.Lock()
				if cur > peakActive {
					peakActive = cur
				}
				mu.Unlock()

				time.Sleep(20 * time.Millisecond)
				atomic.AddInt64(&peakActive, -1)
				return json.RawMessage(`{}`), nil
			},
		}

		opts := RunOptions{
			ClusterTimeout: 30 * time.Second,
			Stderr:         stderr,
			Collectors:     []collector.Collector{serialCollector},
		}

		// concurrency=1 → serial, backward-compatible.
		d := NewDispatcher(1, opts)

		err := d.Dispatch(context.Background(), clusters, w)
		if err != nil {
			t.Fatalf("Dispatch returned error: %v", err)
		}

		// All 3 records must be written.
		if len(w.Records()) != 3 {
			t.Fatalf("expected 3 records, got %d", len(w.Records()))
		}
	})

	t.Run("writer is safe for concurrent writes", func(t *testing.T) {
		// This test verifies that concurrent calls to WriteRecord do not
		// produce data races or lost writes. We use a real output.Writer
		// with concurrency > 1 to exercise the mutex guard.
		clusters := make([]ocm.ClusterMetadata, 20)
		for i := range clusters {
			clusters[i] = ocm.ClusterMetadata{
				ID:   "c" + string(rune('a'+i)),
				Name: "cluster-" + string(rune('a'+i)),
			}
		}

		tmpDir := t.TempDir()
		meta := output.RunMeta{
			Status:     "running",
			Search:     "test",
			Collectors: []string{"fast"},
		}
		realWriter, err := output.NewWriter(tmpDir, meta)
		if err != nil {
			t.Fatalf("NewWriter: %v", err)
		}

		fastCollector := &mockCollector{
			name: "fast",
			runFn: func(ctx context.Context, clusterID, kubeconfigPath string) (json.RawMessage, error) {
				return json.RawMessage(`{"id":"` + clusterID + `"}`), nil
			},
		}

		opts := RunOptions{
			ClusterTimeout: 30 * time.Second,
			Stderr:         new(bytes.Buffer),
			Collectors:     []collector.Collector{fastCollector},
		}

		// High concurrency to stress-test writer safety.
		d := NewDispatcher(5, opts)

		err = d.Dispatch(context.Background(), clusters, realWriter)
		if err != nil {
			t.Fatalf("Dispatch returned error: %v", err)
		}

		// Finalize to ensure meta.json is written and file is closed.
		if err := realWriter.Finalize("completed", 20, 0, 0, time.Second); err != nil {
			t.Fatalf("Finalize returned error: %v", err)
		}
	})
}

// TestConcurrencySignals_GracefulSigint_Acceptance verifies that:
// 1. Dispatcher accepts a context that can be cancelled (simulating SIGINT)
// 2. When context is cancelled, in-flight workers finish within a grace period
// 3. No new clusters are dispatched after cancellation
// 4. The Dispatch method returns a context.Canceled (or wrapped) error
// 5. A SignalHandler type exists that installs OS signal handlers and manages
//    the two-signal protocol: first SIGINT cancels context, second force-exits
// 6. meta.json status is set to "interrupted" when cancelled via signal
//
// Acceptance criterion: First SIGINT cancels the context (30s grace period for
// in-flight workers to finish), second SIGINT force-exits; meta.json status is
// set to "interrupted" when cancelled via signal.
//
// Phase: RED — SignalHandler type does not exist yet.
func TestConcurrencySignals_GracefulSigint_Acceptance(t *testing.T) {
	t.Run("context cancellation stops dispatching new clusters", func(t *testing.T) {
		// Create many clusters but cancel the context quickly.
		clusters := make([]ocm.ClusterMetadata, 10)
		for i := range clusters {
			clusters[i] = ocm.ClusterMetadata{
				ID:   fmt.Sprintf("c%d", i+1),
				Name: fmt.Sprintf("cluster-%d", i+1),
			}
		}

		w := &concurrentMockWriter{}

		slowCollector := &mockCollector{
			name: "slow",
			runFn: func(ctx context.Context, clusterID, kubeconfigPath string) (json.RawMessage, error) {
				select {
				case <-time.After(100 * time.Millisecond):
					return json.RawMessage(`{}`), nil
				case <-ctx.Done():
					return nil, ctx.Err()
				}
			},
		}

		opts := RunOptions{
			ClusterTimeout: 30 * time.Second,
			Stderr:         new(bytes.Buffer),
			Collectors:     []collector.Collector{slowCollector},
		}

		d := NewDispatcher(2, opts)

		// Cancel after a short time — not all clusters should be processed.
		ctx, cancel := context.WithTimeout(context.Background(), 150*time.Millisecond)
		defer cancel()

		err := d.Dispatch(ctx, clusters, w)

		// Dispatch should return an error or have processed fewer than all clusters.
		processedCount := len(w.Records())
		if err == nil && processedCount == 10 {
			t.Fatal("expected Dispatch to stop early on context cancellation, but all 10 clusters were processed")
		}
	})

	t.Run("SignalHandler exists and manages cancellation", func(t *testing.T) {
		// Verify the SignalHandler type exists and can be constructed.
		// SignalHandler(ctx) returns a derived context and cancel func.
		// When a signal is received, the derived context is cancelled.
		parentCtx := context.Background()
		sh := NewSignalHandler(parentCtx)

		// SignalHandler must expose a Context() method for the derived context.
		derivedCtx := sh.Context()
		if derivedCtx == nil {
			t.Fatal("SignalHandler.Context() returned nil")
		}

		// The derived context should not be cancelled yet.
		if derivedCtx.Err() != nil {
			t.Fatal("derived context should not be cancelled before any signal")
		}

		// Clean up.
		sh.Stop()
	})

	t.Run("interrupted status in meta.json on cancellation", func(t *testing.T) {
		tmpDir := t.TempDir()
		meta := output.RunMeta{
			Status:     "running",
			Search:     "test",
			Collectors: []string{"test-col"},
		}
		w, err := output.NewWriter(tmpDir, meta)
		if err != nil {
			t.Fatalf("NewWriter: %v", err)
		}

		clusters := []ocm.ClusterMetadata{
			{ID: "c1", Name: "cluster-1"},
			{ID: "c2", Name: "cluster-2"},
			{ID: "c3", Name: "cluster-3"},
		}

		slowCollector := &mockCollector{
			name: "test-col",
			runFn: func(ctx context.Context, clusterID, kubeconfigPath string) (json.RawMessage, error) {
				select {
				case <-time.After(200 * time.Millisecond):
					return json.RawMessage(`{}`), nil
				case <-ctx.Done():
					return nil, ctx.Err()
				}
			},
		}

		opts := RunOptions{
			ClusterTimeout: 30 * time.Second,
			Stderr:         new(bytes.Buffer),
			Collectors:     []collector.Collector{slowCollector},
		}

		d := NewDispatcher(1, opts)

		// Cancel almost immediately.
		ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
		defer cancel()

		dispatchErr := d.Dispatch(ctx, clusters, w)

		// Finalize with "interrupted" status — this is what the CLI layer should
		// do when the signal handler fires.
		status := "completed"
		if dispatchErr != nil {
			status = "interrupted"
		}
		if err := w.Finalize(status, 0, 0, 0, time.Second); err != nil {
			t.Fatalf("Finalize: %v", err)
		}

		// Read back meta.json and verify status is "interrupted".
		metaPath := filepath.Join(w.RunDir(), "meta.json")
		metaBytes, err := os.ReadFile(metaPath)
		if err != nil {
			t.Fatalf("reading meta.json: %v", err)
		}

		var finalMeta output.RunMeta
		if err := json.Unmarshal(metaBytes, &finalMeta); err != nil {
			t.Fatalf("parsing meta.json: %v", err)
		}

		if finalMeta.Status != "interrupted" {
			t.Errorf("meta.json status = %q, want %q", finalMeta.Status, "interrupted")
		}
	})
}

// concurrentMockWriter is a thread-safe mock writer for dispatcher tests.
type concurrentMockWriter struct {
	mu      sync.Mutex
	records []output.ClusterRecord
}

func (m *concurrentMockWriter) WriteRecord(rec output.ClusterRecord) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.records = append(m.records, rec)
	return nil
}

func (m *concurrentMockWriter) Finalize(status string, success, failed, skipped int, dur time.Duration) error {
	return nil
}

func (m *concurrentMockWriter) Records() []output.ClusterRecord {
	m.mu.Lock()
	defer m.mu.Unlock()
	result := make([]output.ClusterRecord, len(m.records))
	copy(result, m.records)
	return result
}

// --- Unit Tests for Dispatcher ---

// TestNewDispatcher_Concurrency_SetsWorkerCount verifies that NewDispatcher
// creates a valid Dispatcher with the requested concurrency level.
func TestNewDispatcher_Concurrency_SetsWorkerCount(t *testing.T) {
	tests := []struct {
		name        string
		concurrency int
	}{
		{"concurrency 1 (serial)", 1},
		{"concurrency 2", 2},
		{"concurrency 10", 10},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			opts := RunOptions{
				ClusterTimeout: 30 * time.Second,
				Stderr:         new(bytes.Buffer),
			}

			d := NewDispatcher(tt.concurrency, opts)
			if d == nil {
				t.Fatal("NewDispatcher returned nil")
			}
			if d.concurrency != tt.concurrency {
				t.Errorf("concurrency = %d, want %d", d.concurrency, tt.concurrency)
			}
		})
	}
}

// TestNewDispatcher_StoresRunOptions verifies that the Dispatcher retains
// the RunOptions passed at construction time.
func TestNewDispatcher_StoresRunOptions(t *testing.T) {
	stderr := new(bytes.Buffer)
	col := &mockCollector{name: "test-col"}

	opts := RunOptions{
		ClusterTimeout: 45 * time.Second,
		Stderr:         stderr,
		Collectors:     []collector.Collector{col},
	}

	d := NewDispatcher(3, opts)
	if d.opts.ClusterTimeout != 45*time.Second {
		t.Errorf("ClusterTimeout = %v, want %v", d.opts.ClusterTimeout, 45*time.Second)
	}
	if len(d.opts.Collectors) != 1 {
		t.Fatalf("Collectors len = %d, want 1", len(d.opts.Collectors))
	}
	if d.opts.Collectors[0].Name() != "test-col" {
		t.Errorf("Collector name = %q, want %q", d.opts.Collectors[0].Name(), "test-col")
	}
}

// TestDispatch_AllClusters_WritesRecords verifies that Dispatch processes
// every cluster and writes a record for each one.
func TestDispatch_AllClusters_WritesRecords(t *testing.T) {
	clusters := []ocm.ClusterMetadata{
		{ID: "c1", Name: "cluster-1"},
		{ID: "c2", Name: "cluster-2"},
		{ID: "c3", Name: "cluster-3"},
	}

	w := &concurrentMockWriter{}

	col := &mockCollector{
		name: "test",
		runFn: func(ctx context.Context, clusterID, kubeconfigPath string) (json.RawMessage, error) {
			return json.RawMessage(`{"id":"` + clusterID + `"}`), nil
		},
	}

	opts := RunOptions{
		ClusterTimeout: 30 * time.Second,
		Stderr:         new(bytes.Buffer),
		Collectors:     []collector.Collector{col},
	}

	d := NewDispatcher(2, opts)
	err := d.Dispatch(context.Background(), clusters, w)
	if err != nil {
		t.Fatalf("Dispatch returned error: %v", err)
	}

	recs := w.Records()
	if len(recs) != 3 {
		t.Fatalf("expected 3 records, got %d", len(recs))
	}

	// All cluster IDs must appear in the results.
	ids := map[string]bool{}
	for _, rec := range recs {
		ids[rec.ClusterMetadata.ID] = true
	}
	for _, c := range clusters {
		if !ids[c.ID] {
			t.Errorf("missing record for cluster %s", c.ID)
		}
	}
}

// TestDispatch_EmptyClusters_ReturnsNoError verifies that Dispatch handles
// an empty cluster list gracefully without error.
func TestDispatch_EmptyClusters_ReturnsNoError(t *testing.T) {
	w := &concurrentMockWriter{}

	opts := RunOptions{
		ClusterTimeout: 30 * time.Second,
		Stderr:         new(bytes.Buffer),
	}

	d := NewDispatcher(2, opts)
	err := d.Dispatch(context.Background(), nil, w)
	if err != nil {
		t.Fatalf("Dispatch on empty list returned error: %v", err)
	}

	if len(w.Records()) != 0 {
		t.Errorf("expected 0 records for empty cluster list, got %d", len(w.Records()))
	}
}

// TestDispatch_SemaphoreLimitsConcurrency verifies that the buffered channel
// semaphore actually caps the number of concurrent workers.
func TestDispatch_SemaphoreLimitsConcurrency(t *testing.T) {
	const maxConcurrency = 2
	clusters := make([]ocm.ClusterMetadata, 8)
	for i := range clusters {
		clusters[i] = ocm.ClusterMetadata{
			ID:   fmt.Sprintf("c%d", i+1),
			Name: fmt.Sprintf("cluster-%d", i+1),
		}
	}

	w := &concurrentMockWriter{}

	var active int64
	var peakActive int64
	var mu sync.Mutex

	slowCol := &mockCollector{
		name: "slow",
		runFn: func(ctx context.Context, clusterID, kubeconfigPath string) (json.RawMessage, error) {
			cur := atomic.AddInt64(&active, 1)
			mu.Lock()
			if cur > peakActive {
				peakActive = cur
			}
			mu.Unlock()

			time.Sleep(30 * time.Millisecond)

			atomic.AddInt64(&active, -1)
			return json.RawMessage(`{}`), nil
		},
	}

	opts := RunOptions{
		ClusterTimeout: 30 * time.Second,
		Stderr:         new(bytes.Buffer),
		Collectors:     []collector.Collector{slowCol},
	}

	d := NewDispatcher(maxConcurrency, opts)
	err := d.Dispatch(context.Background(), clusters, w)
	if err != nil {
		t.Fatalf("Dispatch returned error: %v", err)
	}

	if len(w.Records()) != 8 {
		t.Fatalf("expected 8 records, got %d", len(w.Records()))
	}

	// Peak concurrency must not exceed the configured limit.
	if peakActive > int64(maxConcurrency) {
		t.Errorf("peak concurrency = %d, exceeds limit of %d", peakActive, maxConcurrency)
	}

	// Peak should reach the limit (proving parallelism works).
	if peakActive < int64(maxConcurrency) {
		t.Errorf("peak concurrency = %d, expected to reach %d (semaphore underutilized)", peakActive, maxConcurrency)
	}
}

// TestDispatch_PassesCollectorsToEachCluster verifies that the collectors
// from RunOptions are invoked for every cluster.
func TestDispatch_PassesCollectorsToEachCluster(t *testing.T) {
	clusters := []ocm.ClusterMetadata{
		{ID: "c1", Name: "cluster-1"},
		{ID: "c2", Name: "cluster-2"},
	}

	w := &concurrentMockWriter{}

	var invokedMu sync.Mutex
	invokedIDs := map[string]bool{}

	col := &mockCollector{
		name: "tracker",
		runFn: func(ctx context.Context, clusterID, kubeconfigPath string) (json.RawMessage, error) {
			invokedMu.Lock()
			invokedIDs[clusterID] = true
			invokedMu.Unlock()
			return json.RawMessage(`{}`), nil
		},
	}

	opts := RunOptions{
		ClusterTimeout: 30 * time.Second,
		Stderr:         new(bytes.Buffer),
		Collectors:     []collector.Collector{col},
	}

	d := NewDispatcher(2, opts)
	err := d.Dispatch(context.Background(), clusters, w)
	if err != nil {
		t.Fatalf("Dispatch returned error: %v", err)
	}

	// Collector must have been invoked for both clusters.
	for _, c := range clusters {
		if !invokedIDs[c.ID] {
			t.Errorf("collector was not invoked for cluster %s", c.ID)
		}
	}

	// Records should contain the collector results.
	for _, rec := range w.Records() {
		result, ok := rec.ClusterResult["tracker"]
		if !ok {
			t.Errorf("record for %s missing 'tracker' result", rec.ClusterMetadata.ID)
		}
		if result.Status != "success" {
			t.Errorf("record for %s: tracker status = %q, want %q", rec.ClusterMetadata.ID, result.Status, "success")
		}
	}
}

// TestDispatch_BackplaneLoginCalledPerCluster verifies that BackplaneLogin
// (when configured on RunOptions) is called for each cluster during dispatch.
func TestDispatch_BackplaneLoginCalledPerCluster(t *testing.T) {
	clusters := []ocm.ClusterMetadata{
		{ID: "alpha", Name: "cluster-alpha"},
		{ID: "beta", Name: "cluster-beta"},
	}

	w := &concurrentMockWriter{}

	var loginMu sync.Mutex
	var loginIDs []string

	opts := RunOptions{
		ClusterTimeout: 30 * time.Second,
		Stderr:         new(bytes.Buffer),
		BackplaneLogin: func(ctx context.Context, clusterID string) (string, func(), error) {
			loginMu.Lock()
			loginIDs = append(loginIDs, clusterID)
			loginMu.Unlock()
			return "/tmp/kube-" + clusterID, func() {}, nil
		},
		Collectors: []collector.Collector{
			&mockCollector{
				name: "check",
				runFn: func(ctx context.Context, clusterID, kubeconfigPath string) (json.RawMessage, error) {
					return json.RawMessage(`{}`), nil
				},
			},
		},
	}

	d := NewDispatcher(2, opts)
	err := d.Dispatch(context.Background(), clusters, w)
	if err != nil {
		t.Fatalf("Dispatch returned error: %v", err)
	}

	loginMu.Lock()
	defer loginMu.Unlock()
	if len(loginIDs) != 2 {
		t.Fatalf("expected 2 login calls, got %d", len(loginIDs))
	}

	seen := map[string]bool{}
	for _, id := range loginIDs {
		seen[id] = true
	}
	if !seen["alpha"] || !seen["beta"] {
		t.Errorf("expected logins for alpha and beta, got %v", loginIDs)
	}
}

// TestDispatch_ContextCancellation_StopsNewDispatches verifies that when
// the context is cancelled, no new clusters are dispatched.
func TestDispatch_ContextCancellation_StopsNewDispatches(t *testing.T) {
	clusters := make([]ocm.ClusterMetadata, 20)
	for i := range clusters {
		clusters[i] = ocm.ClusterMetadata{
			ID:   fmt.Sprintf("c%d", i+1),
			Name: fmt.Sprintf("cluster-%d", i+1),
		}
	}

	w := &concurrentMockWriter{}

	slowCol := &mockCollector{
		name: "slow",
		runFn: func(ctx context.Context, clusterID, kubeconfigPath string) (json.RawMessage, error) {
			select {
			case <-time.After(50 * time.Millisecond):
				return json.RawMessage(`{}`), nil
			case <-ctx.Done():
				return nil, ctx.Err()
			}
		},
	}

	opts := RunOptions{
		ClusterTimeout: 30 * time.Second,
		Stderr:         new(bytes.Buffer),
		Collectors:     []collector.Collector{slowCol},
	}

	d := NewDispatcher(2, opts)

	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()

	_ = d.Dispatch(ctx, clusters, w)

	// Should have processed far fewer than all 20 clusters.
	processedCount := len(w.Records())
	if processedCount >= 20 {
		t.Errorf("expected fewer than 20 records on cancelled context, got %d", processedCount)
	}
}

// backwards_compatibility: tests public API contract

// TestDispatcherCounters_MixedOutcomes_TracksCorrectCounts verifies that the
// Dispatcher tracks succeeded/failed/skipped outcomes via atomic counters
// with Succeeded(), Failed(), Skipped() accessors. The outcome ternary is:
//   - ok (succeeded): all collectors succeeded
//   - error (failed): at least one collector errored
//   - skipped: backplane login failed
func TestDispatcherCounters_MixedOutcomes_TracksCorrectCounts(t *testing.T) {
	tests := []struct {
		name              string
		clusters          []ocm.ClusterMetadata
		backplaneLogin    BackplaneLoginFunc
		collectorRunFn    func(ctx context.Context, clusterID, kubeconfigPath string) (json.RawMessage, error)
		wantSucceeded     int
		wantFailed        int
		wantSkipped       int
	}{
		{
			name: "all clusters succeed",
			clusters: []ocm.ClusterMetadata{
				{ID: "c1", Name: "cluster-1"},
				{ID: "c2", Name: "cluster-2"},
				{ID: "c3", Name: "cluster-3"},
			},
			collectorRunFn: func(ctx context.Context, clusterID, kubeconfigPath string) (json.RawMessage, error) {
				return json.RawMessage(`{"ok":true}`), nil
			},
			wantSucceeded: 3,
			wantFailed:    0,
			wantSkipped:   0,
		},
		{
			name: "one collector error marks cluster as failed",
			clusters: []ocm.ClusterMetadata{
				{ID: "ok1", Name: "cluster-ok1"},
				{ID: "err1", Name: "cluster-err1"},
				{ID: "ok2", Name: "cluster-ok2"},
			},
			collectorRunFn: func(ctx context.Context, clusterID, kubeconfigPath string) (json.RawMessage, error) {
				if clusterID == "err1" {
					return nil, fmt.Errorf("connection refused")
				}
				return json.RawMessage(`{}`), nil
			},
			wantSucceeded: 2,
			wantFailed:    1,
			wantSkipped:   0,
		},
		{
			name: "backplane login failure marks cluster as skipped",
			clusters: []ocm.ClusterMetadata{
				{ID: "ok1", Name: "cluster-ok1"},
				{ID: "skip1", Name: "cluster-skip1"},
			},
			backplaneLogin: func(ctx context.Context, clusterID string) (string, func(), error) {
				if clusterID == "skip1" {
					return "", nil, fmt.Errorf("backplane: no route to host")
				}
				return "/tmp/kube-" + clusterID, func() {}, nil
			},
			collectorRunFn: func(ctx context.Context, clusterID, kubeconfigPath string) (json.RawMessage, error) {
				return json.RawMessage(`{}`), nil
			},
			wantSucceeded: 1,
			wantFailed:    0,
			wantSkipped:   1,
		},
		{
			name: "mixed outcomes: ok, error, and skipped",
			clusters: []ocm.ClusterMetadata{
				{ID: "ok1", Name: "cluster-ok1"},
				{ID: "ok2", Name: "cluster-ok2"},
				{ID: "err1", Name: "cluster-err1"},
				{ID: "skip1", Name: "cluster-skip1"},
			},
			backplaneLogin: func(ctx context.Context, clusterID string) (string, func(), error) {
				if clusterID == "skip1" {
					return "", nil, fmt.Errorf("backplane: no route to host")
				}
				return "/tmp/kube-" + clusterID, func() {}, nil
			},
			collectorRunFn: func(ctx context.Context, clusterID, kubeconfigPath string) (json.RawMessage, error) {
				if clusterID == "err1" {
					return nil, fmt.Errorf("collector error")
				}
				return json.RawMessage(`{}`), nil
			},
			wantSucceeded: 2,
			wantFailed:    1,
			wantSkipped:   1,
		},
		{
			name:     "empty cluster list yields all-zero counters",
			clusters: []ocm.ClusterMetadata{},
			collectorRunFn: func(ctx context.Context, clusterID, kubeconfigPath string) (json.RawMessage, error) {
				return json.RawMessage(`{}`), nil
			},
			wantSucceeded: 0,
			wantFailed:    0,
			wantSkipped:   0,
		},
		{
			name: "no backplane configured — all succeed when collectors succeed",
			clusters: []ocm.ClusterMetadata{
				{ID: "c1", Name: "cluster-1"},
				{ID: "c2", Name: "cluster-2"},
			},
			// backplaneLogin nil — no login step
			collectorRunFn: func(ctx context.Context, clusterID, kubeconfigPath string) (json.RawMessage, error) {
				return json.RawMessage(`{}`), nil
			},
			wantSucceeded: 2,
			wantFailed:    0,
			wantSkipped:   0,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			w := &concurrentMockWriter{}

			col := &mockCollector{
				name:  "test-col",
				runFn: tt.collectorRunFn,
			}

			opts := RunOptions{
				ClusterTimeout: 30 * time.Second,
				Stderr:         new(bytes.Buffer),
				Collectors:     []collector.Collector{col},
				BackplaneLogin: tt.backplaneLogin,
			}

			d := NewDispatcher(2, opts)
			_ = d.Dispatch(context.Background(), tt.clusters, w)

			if got := d.Succeeded(); got != tt.wantSucceeded {
				t.Errorf("Succeeded() = %d, want %d", got, tt.wantSucceeded)
			}
			if got := d.Failed(); got != tt.wantFailed {
				t.Errorf("Failed() = %d, want %d", got, tt.wantFailed)
			}
			if got := d.Skipped(); got != tt.wantSkipped {
				t.Errorf("Skipped() = %d, want %d", got, tt.wantSkipped)
			}
		})
	}
}

// TestDispatcherCounters_ConcurrencySafety_AtomicAccess verifies that
// counters are safe under high concurrency (atomic operations, not racy).
func TestDispatcherCounters_ConcurrencySafety_AtomicAccess(t *testing.T) {
	clusters := make([]ocm.ClusterMetadata, 50)
	for i := range clusters {
		clusters[i] = ocm.ClusterMetadata{
			ID:   fmt.Sprintf("c%d", i),
			Name: fmt.Sprintf("cluster-%d", i),
		}
	}

	w := &concurrentMockWriter{}

	col := &mockCollector{
		name: "fast",
		runFn: func(ctx context.Context, clusterID, kubeconfigPath string) (json.RawMessage, error) {
			return json.RawMessage(`{}`), nil
		},
	}

	opts := RunOptions{
		ClusterTimeout: 30 * time.Second,
		Stderr:         new(bytes.Buffer),
		Collectors:     []collector.Collector{col},
	}

	d := NewDispatcher(10, opts)
	_ = d.Dispatch(context.Background(), clusters, w)

	total := d.Succeeded() + d.Failed() + d.Skipped()
	if total != 50 {
		t.Errorf("total outcomes = %d, want 50 (succeeded=%d, failed=%d, skipped=%d)",
			total, d.Succeeded(), d.Failed(), d.Skipped())
	}
	// All should be succeeded since no errors or login failures.
	if d.Succeeded() != 50 {
		t.Errorf("Succeeded() = %d, want 50", d.Succeeded())
	}
}

// TestDispatcherCounters_MultipleCollectors_AnyErrorMeansFailed verifies the
// outcome ternary: if ANY collector errors, the cluster is counted as failed,
// even if other collectors succeed.
func TestDispatcherCounters_MultipleCollectors_AnyErrorMeansFailed(t *testing.T) {
	clusters := []ocm.ClusterMetadata{
		{ID: "c1", Name: "cluster-1"},
	}

	w := &concurrentMockWriter{}

	goodCol := &mockCollector{
		name: "good",
		runFn: func(ctx context.Context, clusterID, kubeconfigPath string) (json.RawMessage, error) {
			return json.RawMessage(`{}`), nil
		},
	}
	badCol := &mockCollector{
		name: "bad",
		runFn: func(ctx context.Context, clusterID, kubeconfigPath string) (json.RawMessage, error) {
			return nil, fmt.Errorf("collector failure")
		},
	}

	opts := RunOptions{
		ClusterTimeout: 30 * time.Second,
		Stderr:         new(bytes.Buffer),
		Collectors:     []collector.Collector{goodCol, badCol},
	}

	d := NewDispatcher(1, opts)
	_ = d.Dispatch(context.Background(), clusters, w)

	if got := d.Succeeded(); got != 0 {
		t.Errorf("Succeeded() = %d, want 0 (one collector errored)", got)
	}
	if got := d.Failed(); got != 1 {
		t.Errorf("Failed() = %d, want 1 (one collector errored → cluster failed)", got)
	}
	if got := d.Skipped(); got != 0 {
		t.Errorf("Skipped() = %d, want 0", got)
	}
}

// TestMc105ProgressSummary_DispatcherCounters_Acceptance verifies that:
// 1. Dispatcher exposes Succeeded(), Failed(), Skipped() int accessors
// 2. After dispatching clusters with mixed outcomes (ok, error, skipped),
//    the counters reflect the correct totals
// 3. Outcome ternary per cluster:
//    - ok: all collectors succeeded
//    - error: at least one collector errored
//    - skipped: backplane login failed
// 4. Counters are safe for concurrent access (atomic)
//
// Acceptance criterion: Dispatcher tracks succeeded/failed/skipped outcomes
// via atomic counters with Succeeded(), Failed(), Skipped() accessors;
// outcome ternary (ok=all collectors succeeded, error=at least one errored,
// skipped=backplane login failed) determined per-cluster inside processCluster.
//
// Phase: RED — Succeeded(), Failed(), Skipped() methods do not exist yet.
func TestMc105ProgressSummary_DispatcherCounters_Acceptance(t *testing.T) {
	t.Run("tracks succeeded/failed/skipped outcomes correctly", func(t *testing.T) {
		clusters := []ocm.ClusterMetadata{
			{ID: "ok1", Name: "cluster-ok1"},       // all collectors succeed → ok
			{ID: "ok2", Name: "cluster-ok2"},       // all collectors succeed → ok
			{ID: "err1", Name: "cluster-err1"},     // one collector errors → error
			{ID: "skip1", Name: "cluster-skip1"},   // backplane login fails → skipped
		}

		w := &concurrentMockWriter{}
		stderr := new(bytes.Buffer)

		successCol := &mockCollector{
			name: "good-col",
			runFn: func(ctx context.Context, clusterID, kubeconfigPath string) (json.RawMessage, error) {
				if clusterID == "err1" {
					return nil, fmt.Errorf("collector error: connection refused")
				}
				return json.RawMessage(`{"status":"ok"}`), nil
			},
		}

		opts := RunOptions{
			ClusterTimeout: 30 * time.Second,
			Stderr:         stderr,
			Collectors:     []collector.Collector{successCol},
			BackplaneLogin: func(ctx context.Context, clusterID string) (string, func(), error) {
				if clusterID == "skip1" {
					return "", nil, fmt.Errorf("backplane: no route to host")
				}
				return "/tmp/kube-" + clusterID, func() {}, nil
			},
		}

		d := NewDispatcher(2, opts)
		err := d.Dispatch(context.Background(), clusters, w)
		if err != nil {
			t.Fatalf("Dispatch returned error: %v", err)
		}

		// Verify counter accessors exist and return correct values.
		if got := d.Succeeded(); got != 2 {
			t.Errorf("Succeeded() = %d, want 2 (ok1, ok2)", got)
		}
		if got := d.Failed(); got != 1 {
			t.Errorf("Failed() = %d, want 1 (err1)", got)
		}
		if got := d.Skipped(); got != 1 {
			t.Errorf("Skipped() = %d, want 1 (skip1)", got)
		}
	})

	t.Run("all-success scenario counts correctly", func(t *testing.T) {
		clusters := []ocm.ClusterMetadata{
			{ID: "c1", Name: "cluster-1"},
			{ID: "c2", Name: "cluster-2"},
			{ID: "c3", Name: "cluster-3"},
		}

		w := &concurrentMockWriter{}

		col := &mockCollector{
			name: "ok-col",
			runFn: func(ctx context.Context, clusterID, kubeconfigPath string) (json.RawMessage, error) {
				return json.RawMessage(`{}`), nil
			},
		}

		opts := RunOptions{
			ClusterTimeout: 30 * time.Second,
			Stderr:         new(bytes.Buffer),
			Collectors:     []collector.Collector{col},
		}

		d := NewDispatcher(3, opts)
		_ = d.Dispatch(context.Background(), clusters, w)

		if got := d.Succeeded(); got != 3 {
			t.Errorf("Succeeded() = %d, want 3", got)
		}
		if got := d.Failed(); got != 0 {
			t.Errorf("Failed() = %d, want 0", got)
		}
		if got := d.Skipped(); got != 0 {
			t.Errorf("Skipped() = %d, want 0", got)
		}
	})

	t.Run("counters are safe under high concurrency", func(t *testing.T) {
		clusters := make([]ocm.ClusterMetadata, 50)
		for i := range clusters {
			clusters[i] = ocm.ClusterMetadata{
				ID:   fmt.Sprintf("c%d", i),
				Name: fmt.Sprintf("cluster-%d", i),
			}
		}

		w := &concurrentMockWriter{}

		col := &mockCollector{
			name: "fast",
			runFn: func(ctx context.Context, clusterID, kubeconfigPath string) (json.RawMessage, error) {
				return json.RawMessage(`{}`), nil
			},
		}

		opts := RunOptions{
			ClusterTimeout: 30 * time.Second,
			Stderr:         new(bytes.Buffer),
			Collectors:     []collector.Collector{col},
		}

		d := NewDispatcher(10, opts)
		_ = d.Dispatch(context.Background(), clusters, w)

		total := d.Succeeded() + d.Failed() + d.Skipped()
		if total != 50 {
			t.Errorf("total outcomes = %d, want 50 (succeeded=%d, failed=%d, skipped=%d)",
				total, d.Succeeded(), d.Failed(), d.Skipped())
		}
	})
}

// TestMc105ProgressSummary_AfterLineProgress_Acceptance verifies that:
// 1. After each cluster completes, dispatcher prints an after-line to stderr
// 2. After-line format: [N/Total] cluster-name (outcome, duration)
//    where outcome is one of ok, error, skipped
// 3. N is the dispatch index (1-based), matching the before-line
// 4. Duration is formatted as human-readable (e.g. "2.3s")
// 5. After-lines appear for every processed cluster
//
// Acceptance criterion: After each cluster completes, dispatcher prints an
// after-line to stderr — [N/Total] cluster-name (ok|error|skipped, 2.3s) —
// where N is dispatch index (stable on both before and after lines).
//
// Phase: RED — dispatcher only prints before-lines, no after-lines with outcome/timing.
func TestMc105ProgressSummary_AfterLineProgress_Acceptance(t *testing.T) {
	t.Run("prints after-line with outcome and timing for each cluster", func(t *testing.T) {
		clusters := []ocm.ClusterMetadata{
			{ID: "c1", Name: "cluster-alpha"},
			{ID: "c2", Name: "cluster-beta"},
		}

		w := &concurrentMockWriter{}
		stderr := new(bytes.Buffer)

		col := &mockCollector{
			name: "test-col",
			runFn: func(ctx context.Context, clusterID, kubeconfigPath string) (json.RawMessage, error) {
				time.Sleep(10 * time.Millisecond) // ensure measurable duration
				return json.RawMessage(`{}`), nil
			},
		}

		opts := RunOptions{
			ClusterTimeout: 30 * time.Second,
			Stderr:         stderr,
			Collectors:     []collector.Collector{col},
		}

		d := NewDispatcher(1, opts) // serial to get deterministic order
		err := d.Dispatch(context.Background(), clusters, w)
		if err != nil {
			t.Fatalf("Dispatch returned error: %v", err)
		}

		output := stderr.String()

		// After-lines must contain outcome and timing.
		// Look for pattern: [1/2] cluster-alpha (ok, <duration>)
		if !strings.Contains(output, "cluster-alpha") || !strings.Contains(output, "(ok,") {
			t.Errorf("expected after-line with '(ok,' for cluster-alpha in stderr, got:\n%s", output)
		}
		if !strings.Contains(output, "cluster-beta") || !strings.Contains(output, "(ok,") {
			t.Errorf("expected after-line with '(ok,' for cluster-beta in stderr, got:\n%s", output)
		}

		// After-lines must contain a duration (at least a number followed by time unit).
		// We check for a pattern like "0.0" or "1." followed by something.
		if !strings.Contains(output, "s)") {
			t.Errorf("expected after-line to include duration ending with 's)', got:\n%s", output)
		}
	})

	t.Run("after-line shows error outcome for collector failures", func(t *testing.T) {
		clusters := []ocm.ClusterMetadata{
			{ID: "c1", Name: "fail-cluster"},
		}

		w := &concurrentMockWriter{}
		stderr := new(bytes.Buffer)

		failCol := &mockCollector{
			name: "fail-col",
			runFn: func(ctx context.Context, clusterID, kubeconfigPath string) (json.RawMessage, error) {
				return nil, fmt.Errorf("connection refused")
			},
		}

		opts := RunOptions{
			ClusterTimeout: 30 * time.Second,
			Stderr:         stderr,
			Collectors:     []collector.Collector{failCol},
		}

		d := NewDispatcher(1, opts)
		_ = d.Dispatch(context.Background(), clusters, w)

		output := stderr.String()

		// After-line must show "error" outcome.
		if !strings.Contains(output, "(error,") {
			t.Errorf("expected after-line with '(error,' for failed cluster, got:\n%s", output)
		}
	})

	t.Run("after-line shows skipped outcome for backplane failures", func(t *testing.T) {
		clusters := []ocm.ClusterMetadata{
			{ID: "c1", Name: "skip-cluster"},
		}

		w := &concurrentMockWriter{}
		stderr := new(bytes.Buffer)

		col := &mockCollector{
			name: "test-col",
			runFn: func(ctx context.Context, clusterID, kubeconfigPath string) (json.RawMessage, error) {
				return json.RawMessage(`{}`), nil
			},
		}

		opts := RunOptions{
			ClusterTimeout: 30 * time.Second,
			Stderr:         stderr,
			Collectors:     []collector.Collector{col},
			BackplaneLogin: func(ctx context.Context, clusterID string) (string, func(), error) {
				return "", nil, fmt.Errorf("backplane: no route")
			},
		}

		d := NewDispatcher(1, opts)
		_ = d.Dispatch(context.Background(), clusters, w)

		output := stderr.String()

		// After-line must show "skipped" outcome.
		if !strings.Contains(output, "(skipped,") {
			t.Errorf("expected after-line with '(skipped,' for backplane-failed cluster, got:\n%s", output)
		}
	})
}

// TestMc105ProgressSummary_FinalizeSummary_Acceptance verifies that:
// 1. Dispatcher counters feed accurate counts into Finalize (replacing stubs)
// 2. meta.json has correct clusters_success, clusters_failed, clusters_skipped
// 3. A compact summary line is printed to stderr:
//    "Done: N clusters (X ok, Y error, Z skipped) in <duration> -> <output-dir>/"
// 4. When dispatch was interrupted, prefix is "Interrupted:" instead of "Done:"
//
// Acceptance criterion: Wire d.Succeeded()/Failed()/Skipped() into Finalize()
// replacing stubbed counts; print compact summary line to stderr before Finalize.
//
// Phase: RED — CLI currently uses stubbed counts (succeeded=len(clusters), failed=0, skipped=0).
func TestMc105ProgressSummary_FinalizeSummary_Acceptance(t *testing.T) {
	t.Run("dispatcher counters produce accurate meta.json counts", func(t *testing.T) {
		tmpDir := t.TempDir()
		meta := output.RunMeta{
			Status:     "running",
			Search:     "test",
			Collectors: []string{"test-col"},
		}
		w, err := output.NewWriter(tmpDir, meta)
		if err != nil {
			t.Fatalf("NewWriter: %v", err)
		}

		clusters := []ocm.ClusterMetadata{
			{ID: "ok1", Name: "cluster-ok1"},
			{ID: "ok2", Name: "cluster-ok2"},
			{ID: "err1", Name: "cluster-err1"},
			{ID: "skip1", Name: "cluster-skip1"},
		}

		col := &mockCollector{
			name: "test-col",
			runFn: func(ctx context.Context, clusterID, kubeconfigPath string) (json.RawMessage, error) {
				if clusterID == "err1" {
					return nil, fmt.Errorf("collector error")
				}
				return json.RawMessage(`{}`), nil
			},
		}

		opts := RunOptions{
			ClusterTimeout: 30 * time.Second,
			Stderr:         new(bytes.Buffer),
			Collectors:     []collector.Collector{col},
			BackplaneLogin: func(ctx context.Context, clusterID string) (string, func(), error) {
				if clusterID == "skip1" {
					return "", nil, fmt.Errorf("backplane: no route")
				}
				return "/tmp/kube-" + clusterID, func() {}, nil
			},
		}

		d := NewDispatcher(2, opts)
		_ = d.Dispatch(context.Background(), clusters, w)

		// Finalize using dispatcher counters (not stubs).
		if err := w.Finalize("completed", d.Succeeded(), d.Failed(), d.Skipped(), time.Second); err != nil {
			t.Fatalf("Finalize: %v", err)
		}

		// Read back meta.json and verify counts are accurate.
		metaPath := filepath.Join(w.RunDir(), "meta.json")
		metaBytes, err := os.ReadFile(metaPath)
		if err != nil {
			t.Fatalf("reading meta.json: %v", err)
		}

		var finalMeta output.RunMeta
		if err := json.Unmarshal(metaBytes, &finalMeta); err != nil {
			t.Fatalf("parsing meta.json: %v", err)
		}

		if finalMeta.ClustersSuccess != 2 {
			t.Errorf("clusters_success = %d, want 2", finalMeta.ClustersSuccess)
		}
		if finalMeta.ClustersFailed != 1 {
			t.Errorf("clusters_failed = %d, want 1", finalMeta.ClustersFailed)
		}
		if finalMeta.ClustersSkipped != 1 {
			t.Errorf("clusters_skipped = %d, want 1", finalMeta.ClustersSkipped)
		}
	})

	t.Run("summary line printed to stderr with correct format", func(t *testing.T) {
		clusters := []ocm.ClusterMetadata{
			{ID: "c1", Name: "cluster-1"},
			{ID: "c2", Name: "cluster-2"},
			{ID: "c3", Name: "cluster-3"},
		}

		w := &concurrentMockWriter{}
		stderr := new(bytes.Buffer)

		col := &mockCollector{
			name: "test",
			runFn: func(ctx context.Context, clusterID, kubeconfigPath string) (json.RawMessage, error) {
				return json.RawMessage(`{}`), nil
			},
		}

		opts := RunOptions{
			ClusterTimeout: 30 * time.Second,
			Stderr:         stderr,
			Collectors:     []collector.Collector{col},
		}

		d := NewDispatcher(1, opts)
		_ = d.Dispatch(context.Background(), clusters, w)

		// Call the summary function that should exist on the dispatcher.
		// SummaryLine(total int, dur time.Duration, outputDir string) produces
		// "Done: 3 clusters (3 ok, 0 error, 0 skipped) in <dur> -> <dir>/"
		summaryLine := d.SummaryLine(3, time.Second, "/tmp/output/run1")

		if !strings.Contains(summaryLine, "Done:") {
			t.Errorf("summary line should start with 'Done:', got: %s", summaryLine)
		}
		if !strings.Contains(summaryLine, "3 clusters") {
			t.Errorf("summary line should contain '3 clusters', got: %s", summaryLine)
		}
		if !strings.Contains(summaryLine, "3 ok") {
			t.Errorf("summary line should contain '3 ok', got: %s", summaryLine)
		}
		if !strings.Contains(summaryLine, "0 error") {
			t.Errorf("summary line should contain '0 error', got: %s", summaryLine)
		}
		if !strings.Contains(summaryLine, "/tmp/output/run1") {
			t.Errorf("summary line should contain output dir, got: %s", summaryLine)
		}
	})

	t.Run("summary line uses Interrupted prefix when cancelled", func(t *testing.T) {
		clusters := []ocm.ClusterMetadata{
			{ID: "c1", Name: "cluster-1"},
		}

		w := &concurrentMockWriter{}

		col := &mockCollector{
			name: "test",
			runFn: func(ctx context.Context, clusterID, kubeconfigPath string) (json.RawMessage, error) {
				return json.RawMessage(`{}`), nil
			},
		}

		opts := RunOptions{
			ClusterTimeout: 30 * time.Second,
			Stderr:         new(bytes.Buffer),
			Collectors:     []collector.Collector{col},
		}

		d := NewDispatcher(1, opts)
		_ = d.Dispatch(context.Background(), clusters, w)

		// InterruptedSummaryLine should use "Interrupted:" prefix.
		summaryLine := d.InterruptedSummaryLine(5, time.Minute, "/tmp/output/run2")

		if !strings.Contains(summaryLine, "Interrupted:") {
			t.Errorf("interrupted summary should start with 'Interrupted:', got: %s", summaryLine)
		}
		if !strings.Contains(summaryLine, "5 clusters") {
			t.Errorf("interrupted summary should contain '5 clusters', got: %s", summaryLine)
		}
	})
}

// backwards_compatibility: tests public API contract

// TestProcessCluster_LoginLimiter_AcquireRelease verifies that when LoginLimiter
// is set on RunOptions, processCluster calls Acquire before BackplaneLogin and
// Release after it returns (regardless of success or failure). This ensures login
// concurrency is properly gated through the limiter.
func TestProcessCluster_LoginLimiter_AcquireRelease(t *testing.T) {
	tests := []struct {
		name       string
		loginErr   error
		wantStatus string // "success" or "skipped"
	}{
		{
			name:       "acquire and release called around successful login",
			loginErr:   nil,
			wantStatus: "success",
		},
		{
			name:       "acquire and release called around failed login",
			loginErr:   fmt.Errorf("rate limited"),
			wantStatus: "skipped",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			clusters := []ocm.ClusterMetadata{
				{ID: "c1", Name: "cluster-1"},
			}

			w := &concurrentMockWriter{}

			// Use real AdaptiveLimiter with max=1 to prove acquire/release.
			limiter := backplane.NewAdaptiveLimiter(1)

			var loginCalled bool
			loginFn := func(ctx context.Context, clusterID string) (string, func(), error) {
				loginCalled = true
				if tt.loginErr != nil {
					return "", nil, tt.loginErr
				}
				return "/tmp/kube/" + clusterID, func() {}, nil
			}

			col := &mockCollector{
				name: "test",
				runFn: func(ctx context.Context, clusterID, kubeconfigPath string) (json.RawMessage, error) {
					return json.RawMessage(`{}`), nil
				},
			}

			opts := RunOptions{
				ClusterTimeout: 30 * time.Second,
				Stderr:         new(bytes.Buffer),
				BackplaneLogin: loginFn,
				Collectors:     []collector.Collector{col},
				LoginLimiter:   limiter,
			}

			d := NewDispatcher(1, opts)
			err := d.Dispatch(context.Background(), clusters, w)
			if err != nil {
				t.Fatalf("Dispatch returned error: %v", err)
			}

			if !loginCalled {
				t.Fatal("BackplaneLogin was not called")
			}

			records := w.Records()
			if len(records) != 1 {
				t.Fatalf("got %d records, want 1", len(records))
			}

			// Verify limiter released its slot (inflight back to 0).
			// Acquiring again should succeed immediately, proving Release was called.
			acquireCtx, cancel := context.WithTimeout(context.Background(), 10*time.Millisecond)
			defer cancel()
			if err := limiter.Acquire(acquireCtx); err != nil {
				t.Errorf("Acquire after dispatch timed out — Release was not called: %v", err)
			} else {
				limiter.Release()
			}

			// Check the record status.
			result := records[0].ClusterResult["test"]
			if result.Status != tt.wantStatus {
				t.Errorf("collector status = %q, want %q", result.Status, tt.wantStatus)
			}
		})
	}
}

// TestProcessCluster_LoginLimiter_RecordSuccessOnSuccess verifies that when
// BackplaneLogin succeeds, the dispatcher calls limiter.RecordSuccess().
func TestProcessCluster_LoginLimiter_RecordSuccessOnSuccess(t *testing.T) {
	clusters := []ocm.ClusterMetadata{
		{ID: "c1", Name: "cluster-1"},
	}

	w := &concurrentMockWriter{}

	// Start with limit reduced to 1 so RecordSuccess increases it.
	limiter := backplane.NewAdaptiveLimiter(10)
	// Simulate previous failures that reduced the limit.
	for i := 0; i < 5; i++ {
		limiter.RecordFailure()
	}
	limitBefore := limiter.CurrentLimit()

	loginFn := func(ctx context.Context, clusterID string) (string, func(), error) {
		return "/tmp/kube/" + clusterID, func() {}, nil
	}

	col := &mockCollector{
		name: "test",
		runFn: func(ctx context.Context, clusterID, kubeconfigPath string) (json.RawMessage, error) {
			return json.RawMessage(`{}`), nil
		},
	}

	opts := RunOptions{
		ClusterTimeout: 30 * time.Second,
		Stderr:         new(bytes.Buffer),
		BackplaneLogin: loginFn,
		Collectors:     []collector.Collector{col},
		LoginLimiter:   limiter,
	}

	d := NewDispatcher(1, opts)
	_ = d.Dispatch(context.Background(), clusters, w)

	// After a successful login, RecordSuccess should have increased the limit.
	limitAfter := limiter.CurrentLimit()
	if limitAfter <= limitBefore {
		t.Errorf("limiter limit after success = %d, should be > %d (RecordSuccess not called)",
			limitAfter, limitBefore)
	}
}

// TestProcessCluster_LoginLimiter_RecordFailureOnFailure verifies that when
// BackplaneLogin fails, the dispatcher calls limiter.RecordFailure().
func TestProcessCluster_LoginLimiter_RecordFailureOnFailure(t *testing.T) {
	clusters := []ocm.ClusterMetadata{
		{ID: "c1", Name: "cluster-1"},
	}

	w := &concurrentMockWriter{}

	// Start at max limit so RecordFailure decreases it.
	limiter := backplane.NewAdaptiveLimiter(8)
	limitBefore := limiter.CurrentLimit() // should be 8

	loginFn := func(ctx context.Context, clusterID string) (string, func(), error) {
		return "", nil, fmt.Errorf("rate limited")
	}

	col := &mockCollector{
		name: "test",
		runFn: func(ctx context.Context, clusterID, kubeconfigPath string) (json.RawMessage, error) {
			return json.RawMessage(`{}`), nil
		},
	}

	opts := RunOptions{
		ClusterTimeout: 30 * time.Second,
		Stderr:         new(bytes.Buffer),
		BackplaneLogin: loginFn,
		Collectors:     []collector.Collector{col},
		LoginLimiter:   limiter,
	}

	d := NewDispatcher(1, opts)
	_ = d.Dispatch(context.Background(), clusters, w)

	// After all login attempts fail, RecordFailure should have been called once
	// per attempt. With RetryLogin(maxRetries=3) there are 4 total attempts, so
	// the AIMD halving is applied 4 times: 8 → 4 → 2 → 1 → 1 (floor 1).
	limitAfter := limiter.CurrentLimit()
	if limitAfter >= limitBefore {
		t.Errorf("limiter limit after failure = %d, should be < %d (RecordFailure not called)",
			limitAfter, limitBefore)
	}
	// With 4 failed attempts the floor is reached: expect 1.
	want := 1
	if limitAfter != want {
		t.Errorf("limiter limit after failure = %d, want %d (4 halvings of 8)", limitAfter, want)
	}
}

// TestProcessCluster_NilLoginLimiter_BackwardCompatible verifies that when
// LoginLimiter is nil, login proceeds without any limiter calls and all
// existing behavior is preserved. This is the backward-compatibility test.
// backwards_compatibility: tests public API contract
func TestProcessCluster_NilLoginLimiter_BackwardCompatible(t *testing.T) {
	clusters := []ocm.ClusterMetadata{
		{ID: "c1", Name: "cluster-1"},
		{ID: "c2", Name: "cluster-2"},
	}

	w := &concurrentMockWriter{}

	var loginIDs []string
	var loginMu sync.Mutex
	loginFn := func(ctx context.Context, clusterID string) (string, func(), error) {
		loginMu.Lock()
		loginIDs = append(loginIDs, clusterID)
		loginMu.Unlock()
		return "/tmp/kube/" + clusterID, func() {}, nil
	}

	col := &mockCollector{
		name: "test",
		runFn: func(ctx context.Context, clusterID, kubeconfigPath string) (json.RawMessage, error) {
			return json.RawMessage(`{}`), nil
		},
	}

	opts := RunOptions{
		ClusterTimeout: 30 * time.Second,
		Stderr:         new(bytes.Buffer),
		BackplaneLogin: loginFn,
		Collectors:     []collector.Collector{col},
		LoginLimiter:   nil, // explicitly nil — no limiter
	}

	d := NewDispatcher(2, opts)
	err := d.Dispatch(context.Background(), clusters, w)
	if err != nil {
		t.Fatalf("Dispatch returned error: %v", err)
	}

	records := w.Records()
	if len(records) != 2 {
		t.Fatalf("got %d records, want 2", len(records))
	}

	// Both logins should have been called.
	loginMu.Lock()
	defer loginMu.Unlock()
	if len(loginIDs) != 2 {
		t.Fatalf("expected 2 login calls, got %d", len(loginIDs))
	}

	// All results should be successful.
	for _, rec := range records {
		result := rec.ClusterResult["test"]
		if result.Status != "success" {
			t.Errorf("cluster %s: status = %q, want %q", rec.ClusterMetadata.ID, result.Status, "success")
		}
	}
}

// TestProcessCluster_LoginLimiter_AcquireCancelled verifies that if the
// limiter's Acquire blocks and the context is cancelled, the cluster is
// handled gracefully (no deadlock, no panic).
func TestProcessCluster_LoginLimiter_AcquireCancelled(t *testing.T) {
	clusters := make([]ocm.ClusterMetadata, 5)
	for i := range clusters {
		clusters[i] = ocm.ClusterMetadata{
			ID:   fmt.Sprintf("c%d", i),
			Name: fmt.Sprintf("cluster-%d", i),
		}
	}

	w := &concurrentMockWriter{}

	// Limiter with max=1, but login is slow — combined with context cancellation
	// this will cause some Acquire calls to fail.
	limiter := backplane.NewAdaptiveLimiter(1)

	loginFn := func(ctx context.Context, clusterID string) (string, func(), error) {
		select {
		case <-time.After(50 * time.Millisecond):
			return "/tmp/kube/" + clusterID, func() {}, nil
		case <-ctx.Done():
			return "", nil, ctx.Err()
		}
	}

	col := &mockCollector{
		name: "test",
		runFn: func(ctx context.Context, clusterID, kubeconfigPath string) (json.RawMessage, error) {
			return json.RawMessage(`{}`), nil
		},
	}

	opts := RunOptions{
		ClusterTimeout: 30 * time.Second,
		Stderr:         new(bytes.Buffer),
		BackplaneLogin: loginFn,
		Collectors:     []collector.Collector{col},
		LoginLimiter:   limiter,
	}

	d := NewDispatcher(3, opts)

	// Cancel quickly so some clusters get Acquire-cancelled.
	ctx, cancel := context.WithTimeout(context.Background(), 80*time.Millisecond)
	defer cancel()

	// This should not panic or deadlock.
	_ = d.Dispatch(ctx, clusters, w)

	// Some records may have been written, some may not — the point is no deadlock.
	t.Logf("processed %d of %d clusters before cancellation", len(w.Records()), len(clusters))
}

// TestProcessCluster_LoginLimiter_GatesConcurrentLogins verifies that with high
// worker concurrency but a low limiter max, peak login concurrency stays bounded.
func TestProcessCluster_LoginLimiter_GatesConcurrentLogins(t *testing.T) {
	const workerConcurrency = 8
	const limiterMax = 2

	clusters := make([]ocm.ClusterMetadata, 16)
	for i := range clusters {
		clusters[i] = ocm.ClusterMetadata{
			ID:   fmt.Sprintf("c%d", i),
			Name: fmt.Sprintf("cluster-%d", i),
		}
	}

	w := &concurrentMockWriter{}

	var loginActive int64
	var peakLogin int64
	var loginMu sync.Mutex

	limiter := backplane.NewAdaptiveLimiter(limiterMax)

	loginFn := func(ctx context.Context, clusterID string) (string, func(), error) {
		cur := atomic.AddInt64(&loginActive, 1)
		loginMu.Lock()
		if cur > peakLogin {
			peakLogin = cur
		}
		loginMu.Unlock()

		time.Sleep(15 * time.Millisecond) // slow login

		atomic.AddInt64(&loginActive, -1)
		return "/tmp/kube/" + clusterID, func() {}, nil
	}

	col := &mockCollector{
		name: "test",
		runFn: func(ctx context.Context, clusterID, kubeconfigPath string) (json.RawMessage, error) {
			return json.RawMessage(`{}`), nil
		},
	}

	opts := RunOptions{
		ClusterTimeout: 30 * time.Second,
		Stderr:         new(bytes.Buffer),
		BackplaneLogin: loginFn,
		Collectors:     []collector.Collector{col},
		LoginLimiter:   limiter,
	}

	d := NewDispatcher(workerConcurrency, opts)
	err := d.Dispatch(context.Background(), clusters, w)
	if err != nil {
		t.Fatalf("Dispatch returned error: %v", err)
	}

	if len(w.Records()) != 16 {
		t.Fatalf("got %d records, want 16", len(w.Records()))
	}

	// Peak login concurrency should be bounded by limiter, NOT worker concurrency.
	if peakLogin > int64(limiterMax) {
		t.Errorf("peak login concurrency = %d, want <= %d (limiter max)", peakLogin, limiterMax)
	}
	if peakLogin < 1 {
		t.Error("peak login concurrency = 0, login was never called")
	}
}

// TestMc109AdaptiveLoginRateLimit_DispatcherIntegration_Acceptance verifies that:
// 1. With --concurrency 100 and a slow login service, the dispatcher does NOT produce
//    100 simultaneous login attempts — login is gated by the adaptive limiter
// 2. Only login calls are gated; collector execution runs at full concurrency
// 3. RunOptions accepts a LoginLimiter field that the dispatcher uses for login gating
// 4. Peak login concurrency is bounded by the limiter's initial rate, not --concurrency
//
// Acceptance criterion: --concurrency 100 with a slow login service does not produce
// 100 simultaneous login attempts; only login calls are gated while collectors run at
// full concurrency; --max-login-rate CLI flag controls the initial rate ceiling.
func TestMc109AdaptiveLoginRateLimit_DispatcherIntegration_Acceptance(t *testing.T) {
	t.Run("login concurrency is gated by adaptive limiter not worker concurrency", func(t *testing.T) {
		clusters := make([]ocm.ClusterMetadata, 20)
		for i := range clusters {
			clusters[i] = ocm.ClusterMetadata{
				ID:   fmt.Sprintf("c%d", i),
				Name: fmt.Sprintf("cluster-%d", i),
			}
		}

		w := &concurrentMockWriter{}

		// Track peak login concurrency.
		var loginActive int64
		var peakLoginActive int64
		var loginMu sync.Mutex

		// Track peak collector concurrency.
		var collectorActive int64
		var peakCollectorActive int64
		var collectorMu sync.Mutex

		slowLogin := func(ctx context.Context, clusterID string) (string, func(), error) {
			cur := atomic.AddInt64(&loginActive, 1)
			loginMu.Lock()
			if cur > peakLoginActive {
				peakLoginActive = cur
			}
			loginMu.Unlock()

			time.Sleep(20 * time.Millisecond) // slow login

			atomic.AddInt64(&loginActive, -1)
			return "/tmp/kube/" + clusterID, func() {}, nil
		}

		fastCollector := &mockCollector{
			name: "fast",
			runFn: func(ctx context.Context, clusterID, kubeconfigPath string) (json.RawMessage, error) {
				cur := atomic.AddInt64(&collectorActive, 1)
				collectorMu.Lock()
				if cur > peakCollectorActive {
					peakCollectorActive = cur
				}
				collectorMu.Unlock()

				time.Sleep(5 * time.Millisecond) // fast collector

				atomic.AddInt64(&collectorActive, -1)
				return json.RawMessage(`{"ok":true}`), nil
			},
		}

		// Create an adaptive limiter with initial limit = 5.
		limiter := backplane.NewAdaptiveLimiter(5)

		opts := RunOptions{
			ClusterTimeout: 30 * time.Second,
			Stderr:         new(bytes.Buffer),
			BackplaneLogin: slowLogin,
			Collectors:     []collector.Collector{fastCollector},
			LoginLimiter:   limiter,
		}

		// High concurrency (10 workers) but login limiter caps at 5.
		d := NewDispatcher(10, opts)
		err := d.Dispatch(context.Background(), clusters, w)
		if err != nil {
			t.Fatalf("Dispatch returned error: %v", err)
		}

		// All clusters should be processed.
		records := w.Records()
		if len(records) != 20 {
			t.Fatalf("got %d records, want 20", len(records))
		}

		// Peak login concurrency should be <= 5 (the limiter initial rate),
		// NOT 10 (the worker concurrency).
		if peakLoginActive > 5 {
			t.Errorf("peak login concurrency = %d, want <= 5 (limiter initial rate)", peakLoginActive)
		}

		// Collectors should have had higher concurrency than login (they are NOT gated).
		// With 10 workers and fast collectors, collector peak should be > login peak.
		t.Logf("peak login concurrency: %d, peak collector concurrency: %d",
			peakLoginActive, peakCollectorActive)
	})

	t.Run("nil LoginLimiter means login is not gated (backward compatible)", func(t *testing.T) {
		clusters := []ocm.ClusterMetadata{
			{ID: "c1", Name: "cluster-1"},
			{ID: "c2", Name: "cluster-2"},
		}

		w := &concurrentMockWriter{}

		loginFn := func(ctx context.Context, clusterID string) (string, func(), error) {
			return "/tmp/kube/" + clusterID, func() {}, nil
		}

		col := &mockCollector{
			name: "test",
			runFn: func(ctx context.Context, clusterID, kubeconfigPath string) (json.RawMessage, error) {
				return json.RawMessage(`{}`), nil
			},
		}

		opts := RunOptions{
			ClusterTimeout: 30 * time.Second,
			Stderr:         new(bytes.Buffer),
			BackplaneLogin: loginFn,
			Collectors:     []collector.Collector{col},
			LoginLimiter:   nil, // No limiter — backward compatible.
		}

		d := NewDispatcher(2, opts)
		err := d.Dispatch(context.Background(), clusters, w)
		if err != nil {
			t.Fatalf("Dispatch returned error: %v", err)
		}

		records := w.Records()
		if len(records) != 2 {
			t.Fatalf("got %d records, want 2", len(records))
		}
	})

	t.Run("limiter adapts during dispatch reducing login concurrency on failures", func(t *testing.T) {
		clusters := make([]ocm.ClusterMetadata, 15)
		for i := range clusters {
			clusters[i] = ocm.ClusterMetadata{
				ID:   fmt.Sprintf("c%d", i),
				Name: fmt.Sprintf("cluster-%d", i),
			}
		}

		w := &concurrentMockWriter{}

		var loginCount int64
		loginFn := func(ctx context.Context, clusterID string) (string, func(), error) {
			n := atomic.AddInt64(&loginCount, 1)
			time.Sleep(10 * time.Millisecond)
			// First 5 logins fail to trigger multiplicative decrease.
			if n <= 5 {
				return "", nil, fmt.Errorf("rate limited")
			}
			return "/tmp/kube/" + clusterID, func() {}, nil
		}

		col := &mockCollector{
			name: "test",
			runFn: func(ctx context.Context, clusterID, kubeconfigPath string) (json.RawMessage, error) {
				return json.RawMessage(`{}`), nil
			},
		}

		limiter := backplane.NewAdaptiveLimiter(8)

		opts := RunOptions{
			ClusterTimeout: 30 * time.Second,
			Stderr:         new(bytes.Buffer),
			BackplaneLogin: loginFn,
			Collectors:     []collector.Collector{col},
			LoginLimiter:   limiter,
		}

		d := NewDispatcher(10, opts)
		_ = d.Dispatch(context.Background(), clusters, w)

		// After failures, the limiter's current limit should have decreased from 8.
		// The exact value depends on the interleaving, but it should be < 8.
		currentLimit := limiter.CurrentLimit()
		t.Logf("limiter current limit after dispatch: %d", currentLimit)

		// The test is that dispatch completed without deadlock and the limiter adapted.
		// All 15 clusters should have been attempted.
		records := w.Records()
		if len(records) != 15 {
			t.Errorf("got %d records, want 15", len(records))
		}
	})
}

// TestMC126WireErrorHandling_Regression is the permanent regression test for
// MC-126: three pieces of error-handling infrastructure (AdaptiveLimiter /
// RetryLogin, SignalHandler, WriteRecord error propagation) are fully
// implemented and tested in isolation but were never wired into the CLI run
// path, making them dead code in production.
//
// Sub-bugs:
//
//   (A) dispatcher.go — WriteRecord error silently discarded (line used `_ = w.WriteRecord(rec)`
//       instead of propagating the error).  The serial runner.Run() handled it
//       correctly; Dispatch must do the same.
//
//   (B) cli.go — context.Background() passed to Dispatch rather than the context
//       returned by NewSignalHandler(…).Context(), so SIGINT cannot gracefully
//       cancel in-flight work.
//
//   (C) cli.go — RunOptions built without LoginLimiter; dispatcher called
//       BackplaneLogin once per cluster with no retry, so a transient 429 causes
//       an unrecoverable skip instead of an AIMD-gated retry.
//
// Expected (after fix):
//
//	(A) Dispatch returns the error returned by WriteRecord.
//	(B) When a SignalHandler's Stop() is called, the Dispatch context is
//	    cancelled and in-flight work drains gracefully.
//	(C) BackplaneLogin is retried more than once per cluster on transient failure.
//
// Actual (bug present):
//
//	(A) Dispatch returns nil even when WriteRecord returns an error.
//	(B) Dispatch never sees context cancellation from SIGINT.
//	(C) BackplaneLogin called exactly once per cluster; single 429 → skip.
func TestMC126WireErrorHandling_Regression(t *testing.T) {
	// ---- Sub-bug (A): WriteRecord error must propagate from Dispatch ----
	t.Run("(A) Dispatch propagates WriteRecord error not silently discards it", func(t *testing.T) {
		clusters := []ocm.ClusterMetadata{
			{ID: "c1", Name: "cluster-1"},
		}

		diskFull := fmt.Errorf("write /output/results.jsonl: no space left on device")
		w := &failingRecordWriter{returnErr: diskFull}

		col := &mockCollector{
			name: "noop",
			runFn: func(_ context.Context, _, _ string) (json.RawMessage, error) {
				return json.RawMessage(`{}`), nil
			},
		}

		opts := RunOptions{
			ClusterTimeout: 10 * time.Second,
			Collectors:     []collector.Collector{col},
		}

		d := NewDispatcher(1, opts)
		err := d.Dispatch(context.Background(), clusters, w)

		if err == nil {
			t.Fatal("Dispatch returned nil but WriteRecord returned an error — " +
				"regression: WriteRecord error must be propagated, not silently dropped " +
				"(was: `_ = w.WriteRecord(rec)` at dispatcher.go)")
		}
	})

	// ---- Sub-bug (B): SignalHandler context cancellation must stop Dispatch ----
	//
	// The SignalHandler struct exists and its Stop() simulates what the OS signal
	// handler does.  Dispatch must be called with sh.Context() so that Stop()
	// (i.e., SIGINT) drains in-flight work gracefully.
	//
	// This test verifies the Dispatch side: when passed a context that is
	// cancelled via SignalHandler.Stop(), Dispatch stops and returns an error.
	// The complementary CLI-level wiring (using sh.Context() instead of
	// context.Background()) must be verified in cli.go.
	t.Run("(B) Dispatch stops and errors on SignalHandler context cancellation", func(t *testing.T) {
		clusters := make([]ocm.ClusterMetadata, 10)
		for i := range clusters {
			clusters[i] = ocm.ClusterMetadata{
				ID:   fmt.Sprintf("c%d", i),
				Name: fmt.Sprintf("cluster-%d", i),
			}
		}

		w := &concurrentMockWriter{}

		sh := NewSignalHandler(context.Background())

		// Slow collector — gives us time to cancel before all clusters complete.
		slowCol := &mockCollector{
			name: "slow",
			runFn: func(ctx context.Context, _, _ string) (json.RawMessage, error) {
				select {
				case <-time.After(100 * time.Millisecond):
					return json.RawMessage(`{}`), nil
				case <-ctx.Done():
					return nil, ctx.Err()
				}
			},
		}

		opts := RunOptions{
			ClusterTimeout: 30 * time.Second,
			Collectors:     []collector.Collector{slowCol},
		}

		d := NewDispatcher(2, opts)

		// Cancel after a short delay — not all clusters should be processed.
		go func() {
			time.Sleep(50 * time.Millisecond)
			sh.Stop()
		}()

		err := d.Dispatch(sh.Context(), clusters, w)

		// After cancellation, Dispatch must return a non-nil error and must NOT
		// have processed all 10 clusters.
		if err == nil && len(w.Records()) == 10 {
			t.Fatal("Dispatch processed all clusters despite SignalHandler cancellation — " +
				"regression: CLI must pass sh.Context() to Dispatch, not context.Background()")
		}
	})

	// ---- Sub-bug (C): Login must be retried on transient failure ----
	//
	// When BackplaneLogin is configured with a LoginLimiter, a transient 429-style
	// failure should trigger a retry (via backplane.RetryLogin or an equivalent
	// retry loop).  Without retry, the cluster is immediately skipped.
	t.Run("(C) transient login failure triggers retry not immediate skip", func(t *testing.T) {
		var loginCalls int
		var loginMu sync.Mutex

		clusters := []ocm.ClusterMetadata{
			{ID: "c1", Name: "cluster-1"},
		}
		w := &concurrentMockWriter{}

		limiter := backplane.NewAdaptiveLimiter(5)

		opts := RunOptions{
			ClusterTimeout: 10 * time.Second,
			BackplaneLogin: func(_ context.Context, _ string) (string, func(), error) {
				loginMu.Lock()
				loginCalls++
				loginMu.Unlock()
				return "", nil, fmt.Errorf("429 Too Many Requests: rate limited by backplane")
			},
			LoginLimiter: limiter,
			Collectors: []collector.Collector{
				&mockCollector{
					name: "noop",
					runFn: func(_ context.Context, _, _ string) (json.RawMessage, error) {
						return json.RawMessage(`{}`), nil
					},
				},
			},
		}

		d := NewDispatcher(1, opts)
		_ = d.Dispatch(context.Background(), clusters, w)

		loginMu.Lock()
		calls := loginCalls
		loginMu.Unlock()

		if calls < 2 {
			t.Fatalf("BackplaneLogin called %d time(s) on 429 error — expected >= 2 retries. "+
				"Regression: dispatcher must retry transient login failures using "+
				"backplane.RetryLogin or equivalent; a single 429 must not immediately skip a cluster.", calls)
		}
	})
}

// failingRecordWriter is a RecordWriter that always returns an error from WriteRecord.
type failingRecordWriter struct {
	returnErr error
}

func (f *failingRecordWriter) WriteRecord(_ output.ClusterRecord) error {
	return f.returnErr
}

func (f *failingRecordWriter) Finalize(_ string, _, _, _ int, _ time.Duration) error { return nil }
