// Package collector defines the Collector interface and a global registry
// for collector factories. Collectors auto-register via init() functions.
package collector

import (
	"context"
	"encoding/json"
)

// Collector is the interface that every fleet-scan collector must implement.
type Collector interface {
	// Name returns the collector's registered name.
	Name() string

	// Configure applies key=value parameters parsed from --collector flags.
	Configure(params map[string]string) error

	// Run executes the collector against a single cluster and returns
	// structured JSON data. The kubeconfigPath points to an isolated
	// kubeconfig file for the cluster.
	Run(ctx context.Context, clusterID, kubeconfigPath string) (json.RawMessage, error)
}
