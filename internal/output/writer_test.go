package output

import (
	"bufio"
	"encoding/json"
	"os"
	"path/filepath"
	"regexp"
	"testing"
	"time"

	"github.com/squirrd/fleet-scan/internal/ocm"
)

// TestPhase2Output_JsonlWriter_Acceptance verifies that the Writer:
// 1. Creates a timestamped run directory in YYYY-MM-DDTHHMMSS format
// 2. Writes meta.json at start with status "running"
// 3. Writes JSONL records with flush-per-line via WriteRecord()
// 4. Overwrites meta.json at finalization with final status and counts
func TestPhase2Output_JsonlWriter_Acceptance(t *testing.T) {
	baseDir := t.TempDir()

	meta := RunMeta{
		Status:     "running",
		Search:     "managed='true'",
		Collectors: []string{"managed-namespaces"},
	}

	// Create a new Writer — it should create a timestamped run directory.
	w, err := NewWriter(baseDir, meta)
	if err != nil {
		t.Fatalf("NewWriter failed: %v", err)
	}

	// --- Verify run directory was created with correct name format ---
	runDir := w.RunDir()
	dirName := filepath.Base(runDir)

	// Must match YYYY-MM-DDTHHMMSS format (no random suffix)
	pattern := `^\d{4}-\d{2}-\d{2}T\d{6}$`
	matched, _ := regexp.MatchString(pattern, dirName)
	if !matched {
		t.Errorf("run directory name %q does not match YYYY-MM-DDTHHMMSS format", dirName)
	}

	// --- Verify meta.json was written at start with status "running" ---
	metaPath := filepath.Join(runDir, "meta.json")
	metaBytes, err := os.ReadFile(metaPath)
	if err != nil {
		t.Fatalf("failed to read initial meta.json: %v", err)
	}

	var initialMeta RunMeta
	if err := json.Unmarshal(metaBytes, &initialMeta); err != nil {
		t.Fatalf("failed to unmarshal initial meta.json: %v", err)
	}
	if initialMeta.Status != "running" {
		t.Errorf("initial meta.json status = %q, want %q", initialMeta.Status, "running")
	}

	// --- Write JSONL records ---
	rec1 := ClusterRecord{
		ClusterMetadata: ocm.ClusterMetadata{
			ID:   "cluster-001",
			Name: "c1",
		},
		ClusterResult: map[string]CollectorResult{},
	}
	rec2 := ClusterRecord{
		ClusterMetadata: ocm.ClusterMetadata{
			ID:   "cluster-002",
			Name: "c2",
		},
		ClusterResult: map[string]CollectorResult{},
	}

	if err := w.WriteRecord(rec1); err != nil {
		t.Fatalf("WriteRecord(rec1) failed: %v", err)
	}
	if err := w.WriteRecord(rec2); err != nil {
		t.Fatalf("WriteRecord(rec2) failed: %v", err)
	}

	// Verify JSONL file exists and has two lines (flush-per-line)
	jsonlPath := filepath.Join(runDir, "results.jsonl")
	f, err := os.Open(jsonlPath)
	if err != nil {
		t.Fatalf("failed to open results.jsonl: %v", err)
	}
	defer f.Close()

	scanner := bufio.NewScanner(f)
	var lines []string
	for scanner.Scan() {
		lines = append(lines, scanner.Text())
	}
	if len(lines) != 2 {
		t.Fatalf("results.jsonl has %d lines, want 2", len(lines))
	}

	// Each line must be valid JSON with cluster_metadata.id
	for i, line := range lines {
		var recMap map[string]interface{}
		if err := json.Unmarshal([]byte(line), &recMap); err != nil {
			t.Errorf("line %d is not valid JSON: %v", i+1, err)
			continue
		}
		cm, ok := recMap["cluster_metadata"].(map[string]interface{})
		if !ok {
			t.Errorf("line %d missing cluster_metadata", i+1)
			continue
		}
		if cm["id"] == nil || cm["id"] == "" {
			t.Errorf("line %d cluster_metadata.id is empty", i+1)
		}
	}

	// --- Finalize and verify meta.json is overwritten ---
	if err := w.Finalize("completed", 2, 0, 0, time.Second*30); err != nil {
		t.Fatalf("Finalize failed: %v", err)
	}

	metaBytes, err = os.ReadFile(metaPath)
	if err != nil {
		t.Fatalf("failed to read finalized meta.json: %v", err)
	}

	var finalMeta RunMeta
	if err := json.Unmarshal(metaBytes, &finalMeta); err != nil {
		t.Fatalf("failed to unmarshal finalized meta.json: %v", err)
	}
	if finalMeta.Status != "completed" {
		t.Errorf("finalized meta.json status = %q, want %q", finalMeta.Status, "completed")
	}
	if finalMeta.ClustersSuccess != 2 {
		t.Errorf("finalized meta.json clusters_success = %d, want 2", finalMeta.ClustersSuccess)
	}
	if finalMeta.DurationSeconds <= 0 {
		t.Errorf("finalized meta.json duration_seconds = %v, want > 0", finalMeta.DurationSeconds)
	}
}
