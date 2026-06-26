package collector

import (
	"context"
	"encoding/json"
	"fmt"
	"path/filepath"
	"strings"
)

// Default namespace patterns matching managed OpenShift namespaces.
var defaultPatterns = []string{
	"openshift-*",
	"kube-system",
	"kube-public",
	"default",
	"redhat-*",
}

// Default resource kinds to enumerate.
var defaultKinds = []string{
	"Pods", "Deployments", "StatefulSets", "DaemonSets",
	"Jobs", "CronJobs", "Services", "ConfigMaps", "Secrets",
	"NetworkPolicies", "Routes", "ServiceAccounts", "Roles", "RoleBindings",
}

// nsResource represents the resources of a single kind in a namespace.
type nsResource struct {
	Count int               `json:"count"`
	Items []json.RawMessage `json:"items"`
}

// nsEntry represents one namespace and its collected resources.
type nsEntry struct {
	Name      string                 `json:"name"`
	Resources map[string]*nsResource `json:"resources"`
}

// nsResult is the top-level JSON output of the managed-namespaces collector.
type nsResult struct {
	Namespaces []nsEntry `json:"namespaces"`
}

// clientBuilderFunc builds a namespaceLister from a kubeconfig path.
type clientBuilderFunc func(kubeconfigPath string) (namespaceLister, error)

// namespaceLister provides the operations needed by the collector.
type namespaceLister interface {
	// ListNamespaces returns all namespace names in the cluster.
	ListNamespaces(ctx context.Context) ([]string, error)
	// ListResources returns items of the given kind in the given namespace.
	ListResources(ctx context.Context, namespace, kind string) ([]json.RawMessage, error)
}

// managedNamespacesCollector enumerates resources in managed namespaces.
type managedNamespacesCollector struct {
	patterns      []string
	kinds         []string
	clientBuilder clientBuilderFunc
}

// newManagedNamespacesCollector creates a new managed-namespaces collector.
func newManagedNamespacesCollector() Collector {
	return &managedNamespacesCollector{
		clientBuilder: newKubeClientBuilder(),
	}
}

// Name returns the collector's registered name.
func (c *managedNamespacesCollector) Name() string {
	return "managed-namespaces"
}

// Configure parses patterns and kinds params. If not provided, sensible
// defaults are used.
func (c *managedNamespacesCollector) Configure(params map[string]string) error {
	if p, ok := params["patterns"]; ok && p != "" {
		c.patterns = strings.Split(p, ",")
	} else {
		c.patterns = make([]string, len(defaultPatterns))
		copy(c.patterns, defaultPatterns)
	}

	if k, ok := params["kinds"]; ok && k != "" {
		c.kinds = strings.Split(k, ",")
	} else {
		c.kinds = make([]string, len(defaultKinds))
		copy(c.kinds, defaultKinds)
	}

	return nil
}

// matchNamespace returns true if the namespace matches any configured pattern.
func (c *managedNamespacesCollector) matchNamespace(namespace string) bool {
	for _, pattern := range c.patterns {
		if matched, err := filepath.Match(pattern, namespace); err == nil && matched {
			return true
		}
	}
	return false
}

// Run executes the collector against a single cluster.
func (c *managedNamespacesCollector) Run(ctx context.Context, clusterID, kubeconfigPath string) (json.RawMessage, error) {
	builder := c.clientBuilder
	if builder == nil {
		return nil, fmt.Errorf("managed-namespaces: no client builder configured")
	}

	lister, err := builder(kubeconfigPath)
	if err != nil {
		return nil, fmt.Errorf("managed-namespaces: building client: %w", err)
	}

	allNamespaces, err := lister.ListNamespaces(ctx)
	if err != nil {
		return nil, fmt.Errorf("managed-namespaces: listing namespaces: %w", err)
	}

	result := nsResult{}
	for _, ns := range allNamespaces {
		if !c.matchNamespace(ns) {
			continue
		}

		entry := nsEntry{
			Name:      ns,
			Resources: make(map[string]*nsResource),
		}

		for _, kind := range c.kinds {
			items, err := lister.ListResources(ctx, ns, kind)
			if err != nil {
				// Capture error but continue with other kinds.
				entry.Resources[kind] = &nsResource{Count: 0, Items: []json.RawMessage{}}
				continue
			}
			entry.Resources[kind] = &nsResource{
				Count: len(items),
				Items: items,
			}
		}

		result.Namespaces = append(result.Namespaces, entry)
	}

	return json.Marshal(result)
}

func init() {
	Register("managed-namespaces", newManagedNamespacesCollector)
}
