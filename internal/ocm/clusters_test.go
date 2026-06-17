package ocm

import (
	"context"
	"encoding/json"
	"testing"
	"time"
)

// mockOCMClient implements OCMClient for testing pagination.
type mockOCMClient struct {
	clusters []ClusterMetadata
	total    int
	pageSize int
	calls    int // tracks how many times ListClusters was called
}

func (m *mockOCMClient) ListClusters(_ context.Context, _ string, page, size int) ([]ClusterMetadata, int, error) {
	m.calls++
	start := (page - 1) * size
	if start >= len(m.clusters) {
		return nil, m.total, nil
	}
	end := start + size
	if end > len(m.clusters) {
		end = len(m.clusters)
	}
	return m.clusters[start:end], m.total, nil
}

// TestListClusters_Pagination verifies that ListAllClusters correctly paginates
// through all clusters. Uses a mock that returns 250 total clusters across
// multiple pages (page size 100 = 3 pages).
func TestListClusters_Pagination(t *testing.T) {
	// Generate 250 mock clusters.
	totalClusters := 250
	allClusters := make([]ClusterMetadata, totalClusters)
	for i := range allClusters {
		allClusters[i] = ClusterMetadata{
			ID:   idForIndex(i),
			Name: nameForIndex(i),
		}
	}

	mock := &mockOCMClient{
		clusters: allClusters,
		total:    totalClusters,
		pageSize: 100,
	}

	ctx := context.Background()
	got, err := ListAllClusters(ctx, mock, "product.id = 'rosa'")
	if err != nil {
		t.Fatalf("ListAllClusters returned error: %v", err)
	}

	if len(got) != totalClusters {
		t.Errorf("got %d clusters, want %d", len(got), totalClusters)
	}

	// Verify pagination happened — should be at least 3 calls for 250 clusters at page size 100.
	if mock.calls < 3 {
		t.Errorf("expected at least 3 API calls for %d clusters, got %d calls", totalClusters, mock.calls)
	}

	// Verify first and last cluster IDs to ensure order is preserved.
	if len(got) > 0 {
		if got[0].ID != idForIndex(0) {
			t.Errorf("first cluster ID = %q, want %q", got[0].ID, idForIndex(0))
		}
		if got[len(got)-1].ID != idForIndex(totalClusters-1) {
			t.Errorf("last cluster ID = %q, want %q", got[len(got)-1].ID, idForIndex(totalClusters-1))
		}
	}
}

// TestClusterMetadata_Fields verifies that ExtractClusterMetadata correctly
// extracts all 12 fields from a raw cluster data map and that the result
// serializes to JSON with exactly the expected keys.
func TestClusterMetadata_Fields(t *testing.T) {
	// Simulate raw cluster data as it would come from the OCM API response.
	rawCluster := map[string]interface{}{
		"id":                "1q2w3e4r5t",
		"name":              "my-prod-cluster",
		"external_id":       "abc-123-def",
		"product":           map[string]interface{}{"id": "rosa"},
		"cloud_provider":    map[string]interface{}{"id": "aws"},
		"region":            map[string]interface{}{"id": "us-east-1"},
		"state":             "ready",
		"health_state":      "healthy",
		"openshift_version": "4.16.3",
		"multi_az":          true,
		"managed":           true,
		"creation_timestamp": "2025-01-15T10:30:00Z",
	}

	meta, err := ExtractClusterMetadata(rawCluster)
	if err != nil {
		t.Fatalf("ExtractClusterMetadata returned error: %v", err)
	}
	if meta == nil {
		t.Fatal("ExtractClusterMetadata returned nil metadata — extraction not implemented")
	}

	// Verify all 12 fields are populated.
	if meta.ID != "1q2w3e4r5t" {
		t.Errorf("ID = %q, want %q", meta.ID, "1q2w3e4r5t")
	}
	if meta.Name != "my-prod-cluster" {
		t.Errorf("Name = %q, want %q", meta.Name, "my-prod-cluster")
	}
	if meta.ExternalID != "abc-123-def" {
		t.Errorf("ExternalID = %q, want %q", meta.ExternalID, "abc-123-def")
	}
	if meta.Product != "rosa" {
		t.Errorf("Product = %q, want %q", meta.Product, "rosa")
	}
	if meta.CloudProvider != "aws" {
		t.Errorf("CloudProvider = %q, want %q", meta.CloudProvider, "aws")
	}
	if meta.Region != "us-east-1" {
		t.Errorf("Region = %q, want %q", meta.Region, "us-east-1")
	}
	if meta.State != "ready" {
		t.Errorf("State = %q, want %q", meta.State, "ready")
	}
	if meta.HealthState != "healthy" {
		t.Errorf("HealthState = %q, want %q", meta.HealthState, "healthy")
	}
	if meta.OpenshiftVersion != "4.16.3" {
		t.Errorf("OpenshiftVersion = %q, want %q", meta.OpenshiftVersion, "4.16.3")
	}
	if !meta.MultiAZ {
		t.Error("MultiAZ should be true")
	}
	if !meta.Managed {
		t.Error("Managed should be true")
	}
	expectedTS := time.Date(2025, 1, 15, 10, 30, 0, 0, time.UTC)
	if !meta.CreationTimestamp.Equal(expectedTS) {
		t.Errorf("CreationTimestamp = %v, want %v", meta.CreationTimestamp, expectedTS)
	}

	// Verify JSON serialization produces exactly 12 keys.
	data, err := json.Marshal(meta)
	if err != nil {
		t.Fatalf("failed to marshal ClusterMetadata: %v", err)
	}

	var raw map[string]interface{}
	if err := json.Unmarshal(data, &raw); err != nil {
		t.Fatalf("failed to unmarshal JSON: %v", err)
	}

	expectedKeys := []string{
		"id", "name", "external_id", "product", "cloud_provider",
		"region", "state", "health_state", "openshift_version",
		"multi_az", "managed", "creation_timestamp",
	}

	for _, key := range expectedKeys {
		if _, ok := raw[key]; !ok {
			t.Errorf("JSON missing expected key %q. Got keys: %v", key, keysOf(raw))
		}
	}

	if len(raw) != 12 {
		t.Errorf("expected 12 JSON keys, got %d: %v", len(raw), keysOf(raw))
	}
}

// --- helpers ---

func idForIndex(i int) string {
	return "cluster-" + itoa(i)
}

func nameForIndex(i int) string {
	return "cluster-name-" + itoa(i)
}

func itoa(i int) string {
	if i == 0 {
		return "0"
	}
	s := ""
	neg := false
	if i < 0 {
		neg = true
		i = -i
	}
	for i > 0 {
		s = string(rune('0'+i%10)) + s
		i /= 10
	}
	if neg {
		s = "-" + s
	}
	return s
}

func keysOf(m map[string]interface{}) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	return keys
}
