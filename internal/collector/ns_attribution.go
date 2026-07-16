package collector

import (
	"context"
	"encoding/json"
	"fmt"
	"path/filepath"
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

// nsAttrResult is the top-level JSON output of the ns-attribution collector.
type nsAttrResult struct {
	Resources []map[string]interface{} `json:"resources"`
}

// topLevelFields are metadata fields that live at the top level of a Kubernetes object.
var topLevelFields = map[string]bool{
	"kind":       true,
	"apiVersion": true,
}

// nsAttributionCollector extracts focused metadata per resource in managed namespaces.
type nsAttributionCollector struct {
	patterns          []string
	fields            []string
	kinds             []string
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

	if k, ok := params["kinds"]; ok && k != "" {
		c.kinds = strings.Split(k, ",")
	} else {
		c.kinds = make([]string, len(defaultKinds))
		copy(c.kinds, defaultKinds)
	}

	return nil
}

// matchNsAttrNamespace returns true if the namespace matches any configured pattern.
func (c *nsAttributionCollector) matchNsAttrNamespace(namespace string) bool {
	for _, pattern := range c.patterns {
		if matched, err := filepath.Match(pattern, namespace); err == nil && matched {
			return true
		}
	}
	return false
}

// extractFields extracts the configured fields from a full Kubernetes JSON object.
// Top-level fields (kind, apiVersion) come from the root; all others come from metadata.
func (c *nsAttributionCollector) extractFields(raw json.RawMessage) (map[string]interface{}, error) {
	var obj map[string]interface{}
	if err := json.Unmarshal(raw, &obj); err != nil {
		return nil, fmt.Errorf("unmarshalling resource: %w", err)
	}

	// Extract the metadata sub-object.
	var metadata map[string]interface{}
	if md, ok := obj["metadata"]; ok {
		if m, ok := md.(map[string]interface{}); ok {
			metadata = m
		}
	}

	// Build the set of requested fields.
	wantFields := make(map[string]bool, len(c.fields))
	for _, f := range c.fields {
		wantFields[f] = true
	}

	result := make(map[string]interface{})
	for field := range wantFields {
		if topLevelFields[field] {
			if v, ok := obj[field]; ok {
				result[field] = v
			}
		} else if metadata != nil {
			if v, ok := metadata[field]; ok {
				if field == "managedFields" {
					result[field] = slimManagedFields(v)
				} else {
					result[field] = v
				}
			}
		}
	}

	return result, nil
}

// Run executes the collector against a single cluster.
func (c *nsAttributionCollector) Run(ctx context.Context, clusterID, kubeconfigPath string) (json.RawMessage, error) {
	builder := c.attrClientBuilder
	if builder == nil {
		return nil, fmt.Errorf("ns-attribution: no client builder configured")
	}

	lister, err := builder(kubeconfigPath)
	if err != nil {
		return nil, fmt.Errorf("ns-attribution: building client: %w", err)
	}

	allNamespaces, err := lister.ListNamespaces(ctx)
	if err != nil {
		return nil, fmt.Errorf("ns-attribution: listing namespaces: %w", err)
	}

	result := nsAttrResult{
		Resources: make([]map[string]interface{}, 0),
	}

	for _, ns := range allNamespaces {
		if !c.matchNsAttrNamespace(ns) {
			continue
		}

		for _, kind := range c.kinds {
			items, err := lister.ListResources(ctx, ns, kind)
			if err != nil {
				// Skip kinds that error — continue with others.
				continue
			}

			for _, item := range items {
				extracted, err := c.extractFields(item)
				if err != nil {
					continue
				}
				result.Resources = append(result.Resources, extracted)
			}
		}
	}

	return json.Marshal(result)
}

// slimManagedFields strips the bulky fieldsV1 data from managedFields entries,
// keeping only manager, operation, and time.
func slimManagedFields(v interface{}) interface{} {
	entries, ok := v.([]interface{})
	if !ok {
		return v
	}
	slim := make([]map[string]interface{}, 0, len(entries))
	for _, entry := range entries {
		m, ok := entry.(map[string]interface{})
		if !ok {
			continue
		}
		s := make(map[string]interface{})
		for _, key := range []string{"manager", "operation", "time", "apiVersion", "subresource"} {
			if val, ok := m[key]; ok {
				s[key] = val
			}
		}
		slim = append(slim, s)
	}
	return slim
}

func init() {
	Register("ns-attribution", newNsAttributionCollector)
}
