package ocm

import (
	"context"
	"fmt"
	"time"
)

const pageSize = 100

type OCMClient interface {
	ListClusters(ctx context.Context, search string, page, size int) ([]ClusterMetadata, int, error)
}

func ListAllClusters(ctx context.Context, client OCMClient, search string) ([]ClusterMetadata, error) {
	var all []ClusterMetadata
	page := 1

	for {
		clusters, total, err := client.ListClusters(ctx, search, page, pageSize)
		if err != nil {
			return nil, fmt.Errorf("listing clusters (page %d): %w", page, err)
		}

		all = append(all, clusters...)

		if len(all) >= total {
			break
		}
		page++
	}

	return all, nil
}

func ExtractClusterMetadata(raw map[string]interface{}) (*ClusterMetadata, error) {
	m := &ClusterMetadata{
		ID:               getString(raw, "id"),
		Name:             getString(raw, "name"),
		ExternalID:       getString(raw, "external_id"),
		State:            getString(raw, "state"),
		HealthState:      getString(raw, "health_state"),
		OpenshiftVersion: getString(raw, "openshift_version"),
		MultiAZ:          getBool(raw, "multi_az"),
		Managed:          getBool(raw, "managed"),
	}

	if nested, ok := raw["product"].(map[string]interface{}); ok {
		m.Product = getString(nested, "id")
	}
	if nested, ok := raw["cloud_provider"].(map[string]interface{}); ok {
		m.CloudProvider = getString(nested, "id")
	}
	if nested, ok := raw["region"].(map[string]interface{}); ok {
		m.Region = getString(nested, "id")
	}

	if ts, ok := raw["creation_timestamp"].(string); ok {
		parsed, err := time.Parse(time.RFC3339, ts)
		if err != nil {
			return nil, fmt.Errorf("parsing creation_timestamp: %w", err)
		}
		m.CreationTimestamp = parsed
	}

	return m, nil
}

func GetClusterCount(ctx context.Context, client OCMClient, search string) (int, error) {
	_, total, err := client.ListClusters(ctx, search, 1, 1)
	if err != nil {
		return 0, fmt.Errorf("getting cluster count: %w", err)
	}
	return total, nil
}

func getString(m map[string]interface{}, key string) string {
	if v, ok := m[key].(string); ok {
		return v
	}
	return ""
}

func getBool(m map[string]interface{}, key string) bool {
	if v, ok := m[key].(bool); ok {
		return v
	}
	return false
}
