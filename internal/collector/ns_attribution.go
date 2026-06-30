package collector

import (
	"context"
	"encoding/json"
	"strings"
)

// Default metadata fields extracted per resource.
var defaultFields = []string{
	"namespace", "name", "kind", "apiVersion", "creationTimestamp",
	"ownerReferences", "labels", "annotations", "managedFields",
}

// attrClientBuilderFunc builds an attrNamespaceLister from a kubeconfig path.
type attrClientBuilderFunc func(kubeconfigPath string) (attrNamespaceLister, error)

// attrNamespaceLister provides the operations needed by the ns-attribution collector.
type attrNamespaceLister interface {
	// ListNamespaces returns all namespace names in the cluster.
	ListNamespaces(ctx context.Context) ([]string, error)
	// ListResources returns full JSON objects of the given kind in the given namespace.
	ListResources(ctx context.Context, namespace, kind string) ([]json.RawMessage, error)
}

// nsAttributionCollector extracts focused metadata per resource in managed namespaces.
type nsAttributionCollector struct {
	patterns          []string
	fields            []string
	attrClientBuilder attrClientBuilderFunc
}

// newNsAttributionCollector creates a new ns-attribution collector.
func newNsAttributionCollector() Collector {
	return &nsAttributionCollector{
		attrClientBuilder: newNsAttrKubeClientBuilder(),
	}
}

// Name returns the collector's registered name.
func (c *nsAttributionCollector) Name() string {
	return "ns-attribution"
}

// Configure parses patterns and fields params. If not provided, sensible
// defaults are used.
func (c *nsAttributionCollector) Configure(params map[string]string) error {
	if p, ok := params["patterns"]; ok && p != "" {
		c.patterns = strings.Split(p, ",")
	} else {
		c.patterns = make([]string, len(defaultPatterns))
		copy(c.patterns, defaultPatterns)
	}

	if f, ok := params["fields"]; ok && f != "" {
		c.fields = strings.Split(f, ",")
	} else {
		c.fields = make([]string, len(defaultFields))
		copy(c.fields, defaultFields)
	}

	return nil
}

// Run executes the collector against a single cluster.
func (c *nsAttributionCollector) Run(ctx context.Context, clusterID, kubeconfigPath string) (json.RawMessage, error) {
	// Stub — will be implemented in the metadata-extraction slice.
	return nil, nil
}

func init() {
	Register("ns-attribution", newNsAttributionCollector)
}
