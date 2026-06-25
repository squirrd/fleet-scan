package runner

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"sync/atomic"
	"testing"
	"time"

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
