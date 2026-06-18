package output

import (
	"encoding/json"
	"testing"
	"time"

	"github.com/squirrd/fleet-scan/internal/ocm"
)

// TestPhase2Output_OutputTypes_Acceptance verifies that ClusterRecord, RunMeta,
// and CollectorResult structs serialize to JSON with the expected schema:
// - ClusterRecord has nested ClusterMetadata and ClusterResult map
// - CollectorResult is an envelope with Status, Data, and Error
// - RunMeta includes all required fields (status, search, collectors, counts, timestamps)
func TestPhase2Output_OutputTypes_Acceptance(t *testing.T) {
	// --- CollectorResult envelope ---
	cr := CollectorResult{
		Status: "success",
		Data:   json.RawMessage(`{"namespaces":["openshift-monitoring"]}`),
		Error:  "",
	}

	crJSON, err := json.Marshal(cr)
	if err != nil {
		t.Fatalf("failed to marshal CollectorResult: %v", err)
	}

	var crMap map[string]interface{}
	if err := json.Unmarshal(crJSON, &crMap); err != nil {
		t.Fatalf("failed to unmarshal CollectorResult JSON: %v", err)
	}

	// CollectorResult must have status, data, and error fields
	for _, field := range []string{"status", "data", "error"} {
		if _, ok := crMap[field]; !ok {
			t.Errorf("CollectorResult JSON missing field %q", field)
		}
	}

	// --- ClusterRecord with nested ClusterMetadata ---
	meta := ocm.ClusterMetadata{
		ID:                "cluster-123",
		Name:              "test-cluster",
		ExternalID:        "ext-456",
		Product:           "osd",
		CloudProvider:     "aws",
		Region:            "us-east-1",
		State:             "ready",
		HealthState:       "healthy",
		OpenshiftVersion:  "4.14.0",
		MultiAZ:           true,
		Managed:           true,
		CreationTimestamp: time.Date(2024, 1, 15, 10, 30, 0, 0, time.UTC),
	}

	rec := ClusterRecord{
		ClusterMetadata: meta,
		ClusterResult:   map[string]CollectorResult{
			"managed-namespaces": cr,
		},
	}

	recJSON, err := json.Marshal(rec)
	if err != nil {
		t.Fatalf("failed to marshal ClusterRecord: %v", err)
	}

	var recMap map[string]interface{}
	if err := json.Unmarshal(recJSON, &recMap); err != nil {
		t.Fatalf("failed to unmarshal ClusterRecord JSON: %v", err)
	}

	// ClusterRecord must have cluster_metadata and cluster_result as nested objects
	if _, ok := recMap["cluster_metadata"]; !ok {
		t.Error("ClusterRecord JSON missing 'cluster_metadata' field")
	}
	if _, ok := recMap["cluster_result"]; !ok {
		t.Error("ClusterRecord JSON missing 'cluster_result' field")
	}

	// Verify cluster_metadata is a nested object with expected fields
	cmRaw, ok := recMap["cluster_metadata"].(map[string]interface{})
	if !ok {
		t.Fatal("cluster_metadata is not a nested object")
	}
	if cmRaw["id"] != "cluster-123" {
		t.Errorf("cluster_metadata.id = %v, want %q", cmRaw["id"], "cluster-123")
	}
	if cmRaw["name"] != "test-cluster" {
		t.Errorf("cluster_metadata.name = %v, want %q", cmRaw["name"], "test-cluster")
	}

	// Verify cluster_result has the collector name as key
	crRaw, ok := recMap["cluster_result"].(map[string]interface{})
	if !ok {
		t.Fatal("cluster_result is not a nested object")
	}
	if _, ok := crRaw["managed-namespaces"]; !ok {
		t.Error("cluster_result missing 'managed-namespaces' key")
	}

	// --- RunMeta with all required fields ---
	rm := RunMeta{
		Status:          "running",
		Search:          "managed='true'",
		Collectors:      []string{"managed-namespaces:filter=openshift-*"},
		ClustersTotal:   42,
		ClustersSuccess: 0,
		ClustersFailed:  0,
		ClustersSkipped: 0,
		StartedAt:       time.Date(2024, 6, 15, 14, 30, 0, 0, time.UTC),
		DurationSeconds: 0.0,
	}

	rmJSON, err := json.Marshal(rm)
	if err != nil {
		t.Fatalf("failed to marshal RunMeta: %v", err)
	}

	var rmMap map[string]interface{}
	if err := json.Unmarshal(rmJSON, &rmMap); err != nil {
		t.Fatalf("failed to unmarshal RunMeta JSON: %v", err)
	}

	requiredFields := []string{
		"status", "search", "collectors",
		"clusters_total", "clusters_success", "clusters_failed", "clusters_skipped",
		"started_at", "duration_seconds",
	}
	for _, field := range requiredFields {
		if _, ok := rmMap[field]; !ok {
			t.Errorf("RunMeta JSON missing required field %q", field)
		}
	}

	if rmMap["status"] != "running" {
		t.Errorf("RunMeta.status = %v, want %q", rmMap["status"], "running")
	}
	if rmMap["search"] != "managed='true'" {
		t.Errorf("RunMeta.search = %v, want %q", rmMap["search"], "managed='true'")
	}
}
