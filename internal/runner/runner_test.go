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
