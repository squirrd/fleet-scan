package output

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/squirrd/fleet-scan/internal/ocm"
)

// TestPhase2Output_ResumeLoader_Acceptance verifies that LoadCompletedSet():
// 1. Reads an existing results.jsonl file
// 2. Extracts cluster IDs of succeeded records into a set
// 3. Handles corrupt lines gracefully (skips them)
// 4. Handles empty files gracefully (returns empty set)
func TestPhase2Output_ResumeLoader_Acceptance(t *testing.T) {
	t.Run("extracts succeeded cluster IDs", func(t *testing.T) {
		dir := t.TempDir()
		jsonlPath := filepath.Join(dir, "results.jsonl")

		// Write test JSONL: two succeeded records, one failed
		records := []ClusterRecord{
			{
				ClusterMetadata: ocm.ClusterMetadata{ID: "cluster-001"},
				ClusterResult: map[string]CollectorResult{
					"ns": {Status: "success"},
				},
			},
			{
				ClusterMetadata: ocm.ClusterMetadata{ID: "cluster-002"},
				ClusterResult: map[string]CollectorResult{
					"ns": {Status: "error", Error: "timeout"},
				},
			},
			{
				ClusterMetadata: ocm.ClusterMetadata{ID: "cluster-003"},
				ClusterResult: map[string]CollectorResult{
					"ns": {Status: "success"},
				},
			},
		}

		f, err := os.Create(jsonlPath)
		if err != nil {
			t.Fatalf("failed to create test JSONL: %v", err)
		}
		for _, rec := range records {
			line, _ := json.Marshal(rec)
			f.Write(line)
			f.Write([]byte("\n"))
		}
		f.Close()

		completed, err := LoadCompletedSet(jsonlPath)
		if err != nil {
			t.Fatalf("LoadCompletedSet failed: %v", err)
		}

		// Only cluster-001 and cluster-003 succeeded; cluster-002 had an error
		if !completed["cluster-001"] {
			t.Error("expected cluster-001 in completed set")
		}
		if completed["cluster-002"] {
			t.Error("cluster-002 should not be in completed set (it had an error)")
		}
		if !completed["cluster-003"] {
			t.Error("expected cluster-003 in completed set")
		}
	})

	t.Run("handles corrupt lines gracefully", func(t *testing.T) {
		dir := t.TempDir()
		jsonlPath := filepath.Join(dir, "results.jsonl")

		content := ""
		rec, _ := json.Marshal(ClusterRecord{
			ClusterMetadata: ocm.ClusterMetadata{ID: "cluster-good"},
			ClusterResult: map[string]CollectorResult{
				"ns": {Status: "success"},
			},
		})
		content += string(rec) + "\n"
		content += "this is not valid json\n"
		content += "{\"cluster_metadata\":{\"id\":\"cluster-also-good\"},\"cluster_result\":{\"ns\":{\"status\":\"success\"}}}\n"

		os.WriteFile(jsonlPath, []byte(content), 0644)

		completed, err := LoadCompletedSet(jsonlPath)
		if err != nil {
			t.Fatalf("LoadCompletedSet should not error on corrupt lines: %v", err)
		}

		if !completed["cluster-good"] {
			t.Error("expected cluster-good in completed set")
		}
		if !completed["cluster-also-good"] {
			t.Error("expected cluster-also-good in completed set")
		}
	})

	t.Run("handles empty file", func(t *testing.T) {
		dir := t.TempDir()
		jsonlPath := filepath.Join(dir, "results.jsonl")
		os.WriteFile(jsonlPath, []byte(""), 0644)

		completed, err := LoadCompletedSet(jsonlPath)
		if err != nil {
			t.Fatalf("LoadCompletedSet should not error on empty file: %v", err)
		}

		if len(completed) != 0 {
			t.Errorf("expected empty set, got %d entries", len(completed))
		}
	})
}
