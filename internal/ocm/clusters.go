package ocm

import "context"

// OCMClient defines the interface for OCM API operations.
// This enables testing with mocks per ADR-0001.
type OCMClient interface {
	// ListClusters returns clusters matching the search query.
	// page is 1-based, size is the number of results per page.
	// Returns clusters, total count, and any error.
	ListClusters(ctx context.Context, search string, page, size int) ([]ClusterMetadata, int, error)
}

// ListAllClusters paginates through all clusters matching the search query.
// Uses response.Total() to determine when all pages have been fetched.
// TODO: implement pagination logic.
func ListAllClusters(ctx context.Context, client OCMClient, search string) ([]ClusterMetadata, error) {
	return nil, nil
}

// ExtractClusterMetadata extracts the 12 hardcoded metadata fields from a raw
// cluster data map (as returned by the OCM API).
// TODO: implement extraction logic.
func ExtractClusterMetadata(raw map[string]interface{}) (*ClusterMetadata, error) {
	return nil, nil
}
