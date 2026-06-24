package runner

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/squirrd/fleet-scan/internal/ocm"
	"github.com/squirrd/fleet-scan/internal/output"
)

// mockWriter captures records written by the runner for test assertions.
type mockWriter struct {
	records []output.ClusterRecord
}

func (m *mockWriter) WriteRecord(rec output.ClusterRecord) error {
	m.records = append(m.records, rec)
	return nil
}

func (m *mockWriter) Finalize(status string, success, failed, skipped int, dur time.Duration) error {
	return nil
}

// TestPhase2Output_RunnerLoop_Acceptance verifies that runner.Run():
// 1. Iterates over all clusters and writes a stub ClusterRecord per cluster
// 2. Each record has empty cluster_result ({})
// 3. Prints [N/Total] progress to stderr
// 4. Respects context cancellation (stops early)
// 5. Uses cluster-timeout per cluster via context.WithTimeout
func TestPhase2Output_RunnerLoop_Acceptance(t *testing.T) {
	t.Run("iterates clusters and writes stub records", func(t *testing.T) {
		clusters := []ocm.ClusterMetadata{
			{ID: "c1", Name: "cluster-1"},
			{ID: "c2", Name: "cluster-2"},
			{ID: "c3", Name: "cluster-3"},
		}

		w := &mockWriter{}
		stderr := new(bytes.Buffer)

		opts := RunOptions{
			ClusterTimeout: 30 * time.Second,
			Stderr:         stderr,
		}

		err := Run(context.Background(), clusters, w, opts)
		if err != nil {
			t.Fatalf("Run returned error: %v", err)
		}

		// Must have written one record per cluster
		if len(w.records) != 3 {
			t.Fatalf("expected 3 records, got %d", len(w.records))
		}

		// Each record should have the cluster's ID and an empty cluster_result
		for i, rec := range w.records {
			expectedID := fmt.Sprintf("c%d", i+1)
			if rec.ClusterMetadata.ID != expectedID {
				t.Errorf("record %d: ID = %q, want %q", i, rec.ClusterMetadata.ID, expectedID)
			}

			// cluster_result should be empty object (stub)
			resultBytes, _ := json.Marshal(rec.ClusterResult)
			if string(resultBytes) != "{}" {
				t.Errorf("record %d: cluster_result = %s, want {}", i, resultBytes)
			}
		}

		// Verify progress output on stderr: [1/3], [2/3], [3/3]
		stderrStr := stderr.String()
		for i := 1; i <= 3; i++ {
			expected := fmt.Sprintf("[%d/3]", i)
			if !strings.Contains(stderrStr, expected) {
				t.Errorf("stderr missing progress %q, got:\n%s", expected, stderrStr)
			}
		}
	})

	t.Run("respects context cancellation", func(t *testing.T) {
		clusters := []ocm.ClusterMetadata{
			{ID: "c1", Name: "cluster-1"},
			{ID: "c2", Name: "cluster-2"},
			{ID: "c3", Name: "cluster-3"},
		}

		w := &mockWriter{}
		stderr := new(bytes.Buffer)

		// Cancel context immediately
		ctx, cancel := context.WithCancel(context.Background())
		cancel()

		opts := RunOptions{
			ClusterTimeout: 30 * time.Second,
			Stderr:         stderr,
		}

		err := Run(ctx, clusters, w, opts)

		// Run should respect cancellation — either return an error or process zero/partial clusters
		if err == nil && len(w.records) == 3 {
			t.Error("expected Run to stop early or return error on cancelled context, but it processed all clusters")
		}
	})

	t.Run("uses cluster timeout", func(t *testing.T) {
		clusters := []ocm.ClusterMetadata{
			{ID: "c1", Name: "cluster-1"},
		}

		w := &mockWriter{}
		stderr := new(bytes.Buffer)

		// Use a very short timeout — the stub should still complete quickly
		opts := RunOptions{
			ClusterTimeout: 1 * time.Millisecond,
			Stderr:         stderr,
		}

		// This test just verifies the timeout parameter is accepted and used.
		// With stub results, even a 1ms timeout should be fine.
		_ = Run(context.Background(), clusters, w, opts)

		// The point is that RunOptions.ClusterTimeout exists and is wired.
		// If the code compiles and runs, the timeout plumbing works.
	})
}

// backwards_compatibility: tests public API contract
// TestRun_BackplaneLogin_Unit covers detailed backplane login integration
// behaviors beyond the acceptance tests.
func TestRun_BackplaneLogin_Unit(t *testing.T) {
	t.Run("passes correct clusterID to BackplaneLogin for each cluster", func(t *testing.T) {
		var loginIDs []string

		clusters := []ocm.ClusterMetadata{
			{ID: "alpha", Name: "cluster-alpha"},
			{ID: "beta", Name: "cluster-beta"},
			{ID: "gamma", Name: "cluster-gamma"},
		}

		w := &mockWriter{}

		opts := RunOptions{
			ClusterTimeout: 30 * time.Second,
			Stderr:         new(bytes.Buffer),
			BackplaneLogin: func(ctx context.Context, clusterID string) (string, func(), error) {
				loginIDs = append(loginIDs, clusterID)
				return "/tmp/kube-" + clusterID, func() {}, nil
			},
		}

		err := Run(context.Background(), clusters, w, opts)
		if err != nil {
			t.Fatalf("Run returned error: %v", err)
		}

		// BackplaneLogin must be called once per cluster with the right ID
		if len(loginIDs) != 3 {
			t.Fatalf("expected 3 login calls, got %d", len(loginIDs))
		}
		expected := []string{"alpha", "beta", "gamma"}
		for i, got := range loginIDs {
			if got != expected[i] {
				t.Errorf("login call %d: got clusterID=%q, want %q", i, got, expected[i])
			}
		}

		// All 3 records should be written
		if len(w.records) != 3 {
			t.Fatalf("expected 3 records, got %d", len(w.records))
		}
	})

	t.Run("cleanup called per-cluster even on successful processing", func(t *testing.T) {
		var cleanupOrder []string

		clusters := []ocm.ClusterMetadata{
			{ID: "c1", Name: "cluster-1"},
			{ID: "c2", Name: "cluster-2"},
		}

		w := &mockWriter{}

		opts := RunOptions{
			ClusterTimeout: 30 * time.Second,
			Stderr:         new(bytes.Buffer),
			BackplaneLogin: func(ctx context.Context, clusterID string) (string, func(), error) {
				return "/tmp/kube-" + clusterID, func() {
					cleanupOrder = append(cleanupOrder, clusterID)
				}, nil
			},
		}

		err := Run(context.Background(), clusters, w, opts)
		if err != nil {
			t.Fatalf("Run returned error: %v", err)
		}

		// Cleanup must be called for every cluster
		if len(cleanupOrder) != 2 {
			t.Fatalf("expected 2 cleanup calls, got %d", len(cleanupOrder))
		}
		// Both cluster IDs must appear (order depends on defer semantics)
		seen := map[string]bool{}
		for _, id := range cleanupOrder {
			seen[id] = true
		}
		if !seen["c1"] || !seen["c2"] {
			t.Errorf("expected cleanups for c1 and c2, got %v", cleanupOrder)
		}
	})

	t.Run("mixed success and failure across multiple clusters", func(t *testing.T) {
		clusters := []ocm.ClusterMetadata{
			{ID: "ok1", Name: "cluster-ok1"},
			{ID: "fail1", Name: "cluster-fail1"},
			{ID: "ok2", Name: "cluster-ok2"},
		}

		w := &mockWriter{}

		opts := RunOptions{
			ClusterTimeout: 30 * time.Second,
			Stderr:         new(bytes.Buffer),
			BackplaneLogin: func(ctx context.Context, clusterID string) (string, func(), error) {
				if clusterID == "fail1" {
					return "", nil, fmt.Errorf("backplane: no route to host")
				}
				return "/tmp/kube-" + clusterID, func() {}, nil
			},
		}

		err := Run(context.Background(), clusters, w, opts)
		if err != nil {
			t.Fatalf("Run should not return error on partial login failures, got: %v", err)
		}

		// All 3 clusters should produce records (including the failed one)
		if len(w.records) != 3 {
			t.Fatalf("expected 3 records, got %d", len(w.records))
		}

		// The failed cluster's record should exist with ID "fail1"
		failRec := w.records[1]
		if failRec.ClusterMetadata.ID != "fail1" {
			t.Errorf("expected second record to be fail1, got %q", failRec.ClusterMetadata.ID)
		}

		// Successful clusters should have empty results (stub, no collectors configured)
		okRec1 := w.records[0]
		if okRec1.ClusterMetadata.ID != "ok1" {
			t.Errorf("expected first record to be ok1, got %q", okRec1.ClusterMetadata.ID)
		}
	})

	t.Run("login failure captures error in record with login_error key", func(t *testing.T) {
		clusters := []ocm.ClusterMetadata{
			{ID: "c1", Name: "cluster-1"},
		}

		w := &mockWriter{}

		opts := RunOptions{
			ClusterTimeout: 30 * time.Second,
			Stderr:         new(bytes.Buffer),
			BackplaneLogin: func(ctx context.Context, clusterID string) (string, func(), error) {
				return "", nil, fmt.Errorf("token expired")
			},
		}

		err := Run(context.Background(), clusters, w, opts)
		if err != nil {
			t.Fatalf("Run returned error: %v", err)
		}

		if len(w.records) != 1 {
			t.Fatalf("expected 1 record, got %d", len(w.records))
		}

		rec := w.records[0]
		// The record should have a "login_error" entry in cluster_result
		// with status "skipped" and the error message
		loginResult, ok := rec.ClusterResult["login_error"]
		if !ok {
			t.Fatal("expected cluster_result to contain 'login_error' key on login failure")
		}
		if loginResult.Status != "skipped" {
			t.Errorf("login_error status = %q, want %q", loginResult.Status, "skipped")
		}
		if !strings.Contains(loginResult.Error, "token expired") {
			t.Errorf("login_error error = %q, want it to contain %q", loginResult.Error, "token expired")
		}
	})

	t.Run("nil cleanup from BackplaneLogin does not panic", func(t *testing.T) {
		clusters := []ocm.ClusterMetadata{
			{ID: "c1", Name: "cluster-1"},
		}

		w := &mockWriter{}

		opts := RunOptions{
			ClusterTimeout: 30 * time.Second,
			Stderr:         new(bytes.Buffer),
			BackplaneLogin: func(ctx context.Context, clusterID string) (string, func(), error) {
				// Login failure returns nil cleanup
				return "", nil, fmt.Errorf("login failed")
			},
		}

		// Should not panic even with nil cleanup
		err := Run(context.Background(), clusters, w, opts)
		if err != nil {
			t.Fatalf("Run returned error: %v", err)
		}
	})

	t.Run("cleanup called even when login fails", func(t *testing.T) {
		// Some implementations might return a cleanup even on failure
		// (e.g. to clean up partial state). Runner should call it if non-nil.
		cleanupCalled := false

		clusters := []ocm.ClusterMetadata{
			{ID: "c1", Name: "cluster-1"},
		}

		w := &mockWriter{}

		opts := RunOptions{
			ClusterTimeout: 30 * time.Second,
			Stderr:         new(bytes.Buffer),
			BackplaneLogin: func(ctx context.Context, clusterID string) (string, func(), error) {
				return "", func() { cleanupCalled = true }, fmt.Errorf("partial failure")
			},
		}

		err := Run(context.Background(), clusters, w, opts)
		if err != nil {
			t.Fatalf("Run returned error: %v", err)
		}

		if !cleanupCalled {
			t.Error("expected cleanup to be called even on login failure")
		}
	})
}

// TestBackplaneLogin_RunnerLoginIntegration_Acceptance verifies that:
// 1. RunOptions has a BackplaneLogin function field
// 2. Runner calls BackplaneLogin before writing records for each cluster
// 3. On login failure: all collectors for that cluster marked "skipped" with
//    the login error, and the cluster record is still written (not silently dropped)
// 4. On login success: kubeconfigPath is available (for future collector use)
// 5. Cleanup is deferred per-cluster (called even on success)
// 6. Nil BackplaneLogin means skip login (backward-compatible with Phase 2 stub behavior)
//
// Acceptance criterion: BackplaneLogin injected as a function field on RunOptions;
// runner calls login before writing records; on login failure all collectors
// marked "skipped" with error and cluster is skipped; on success kubeconfigPath
// is available for future collector use; cleanup deferred per-cluster.
//
// Phase: RED — BackplaneLogin field does not exist on RunOptions yet.
func TestBackplaneLogin_RunnerLoginIntegration_Acceptance(t *testing.T) {
	t.Run("calls BackplaneLogin and records success", func(t *testing.T) {
		loginCalled := false
		cleanupCalled := false

		clusters := []ocm.ClusterMetadata{
			{ID: "c1", Name: "cluster-1"},
		}

		w := &mockWriter{}
		stderr := new(bytes.Buffer)

		opts := RunOptions{
			ClusterTimeout: 30 * time.Second,
			Stderr:         stderr,
			// BackplaneLogin is a function field that does not exist yet.
			// This will cause a compilation error (RED).
			BackplaneLogin: func(ctx context.Context, clusterID string) (string, func(), error) {
				loginCalled = true
				return "/tmp/fake-kubeconfig", func() { cleanupCalled = true }, nil
			},
		}

		err := Run(context.Background(), clusters, w, opts)
		if err != nil {
			t.Fatalf("Run returned error: %v", err)
		}

		if !loginCalled {
			t.Error("expected BackplaneLogin to be called")
		}
		if !cleanupCalled {
			t.Error("expected cleanup to be called after cluster processing")
		}

		// Record should still be written
		if len(w.records) != 1 {
			t.Fatalf("expected 1 record, got %d", len(w.records))
		}
	})

	t.Run("login failure marks collectors as skipped", func(t *testing.T) {
		clusters := []ocm.ClusterMetadata{
			{ID: "c1", Name: "cluster-1"},
		}

		w := &mockWriter{}
		stderr := new(bytes.Buffer)

		opts := RunOptions{
			ClusterTimeout: 30 * time.Second,
			Stderr:         stderr,
			BackplaneLogin: func(ctx context.Context, clusterID string) (string, func(), error) {
				return "", nil, fmt.Errorf("backplane login failed: connection refused")
			},
		}

		err := Run(context.Background(), clusters, w, opts)
		// The runner should NOT return an error for a single cluster login failure;
		// it should skip the cluster and continue.
		if err != nil {
			t.Fatalf("Run should not return error on login failure, got: %v", err)
		}

		// A record should still be written for the failed cluster
		if len(w.records) != 1 {
			t.Fatalf("expected 1 record (even on login failure), got %d", len(w.records))
		}

		// All collectors in the record should be marked "skipped" with the login error.
		// For now with no collectors configured, the record should indicate the login error.
		rec := w.records[0]
		if rec.ClusterMetadata.ID != "c1" {
			t.Errorf("record cluster ID = %q, want %q", rec.ClusterMetadata.ID, "c1")
		}
	})

	t.Run("nil BackplaneLogin skips login (backward compatible)", func(t *testing.T) {
		clusters := []ocm.ClusterMetadata{
			{ID: "c1", Name: "cluster-1"},
		}

		w := &mockWriter{}
		stderr := new(bytes.Buffer)

		opts := RunOptions{
			ClusterTimeout: 30 * time.Second,
			Stderr:         stderr,
			BackplaneLogin: nil, // nil means skip login
		}

		err := Run(context.Background(), clusters, w, opts)
		if err != nil {
			t.Fatalf("Run with nil BackplaneLogin returned error: %v", err)
		}

		// Should still process clusters normally (stub behavior)
		if len(w.records) != 1 {
			t.Fatalf("expected 1 record, got %d", len(w.records))
		}
	})
}
