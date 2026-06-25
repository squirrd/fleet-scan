package collector

import (
	"context"
	"encoding/json"
	"path/filepath"
	"strings"
	"testing"
)

// TestManagedNamespacesCollector_ParamConfig_Acceptance verifies that:
// 1. A "managed-namespaces" collector can be constructed via newManagedNamespacesCollector()
// 2. Name() returns "managed-namespaces"
// 3. Configure() with no params uses sensible defaults:
//    - patterns: openshift-*, kube-system, kube-public, default, redhat-*
//    - kinds: Pods, Deployments, StatefulSets, DaemonSets, Jobs, CronJobs,
//             Services, ConfigMaps, Secrets, NetworkPolicies, Routes,
//             ServiceAccounts, Roles, RoleBindings
// 4. Configure() with explicit patterns and kinds overrides the defaults
// 5. The collector registers itself via init() so Get("managed-namespaces") works
//
// Acceptance criterion: managed-namespaces collector registers via init(),
// Configure() parses patterns and kinds params with sensible defaults
// (patterns=openshift-*,kube-system,kube-public,default,redhat-*;
// kinds=Pods,Deployments,StatefulSets,DaemonSets,Jobs,CronJobs,Services,
// ConfigMaps,Secrets,NetworkPolicies,Routes,ServiceAccounts,Roles,RoleBindings),
// and Name() returns "managed-namespaces".
//
// Phase: RED — the managed-namespaces collector does not exist yet.
func TestManagedNamespacesCollector_ParamConfig_Acceptance(t *testing.T) {
	t.Run("constructor and Name", func(t *testing.T) {
		c := newManagedNamespacesCollector()
		if c.Name() != "managed-namespaces" {
			t.Errorf("Name() = %q, want %q", c.Name(), "managed-namespaces")
		}
	})

	t.Run("default patterns after empty Configure", func(t *testing.T) {
		c := newManagedNamespacesCollector()
		if err := c.Configure(map[string]string{}); err != nil {
			t.Fatalf("Configure({}) error: %v", err)
		}

		mc, ok := c.(*managedNamespacesCollector)
		if !ok {
			t.Fatal("expected *managedNamespacesCollector type")
		}

		wantPatterns := []string{"openshift-*", "kube-system", "kube-public", "default", "redhat-*"}
		if len(mc.patterns) != len(wantPatterns) {
			t.Fatalf("patterns = %v, want %v", mc.patterns, wantPatterns)
		}
		for i, p := range wantPatterns {
			if mc.patterns[i] != p {
				t.Errorf("patterns[%d] = %q, want %q", i, mc.patterns[i], p)
			}
		}
	})

	t.Run("default kinds after empty Configure", func(t *testing.T) {
		c := newManagedNamespacesCollector()
		if err := c.Configure(map[string]string{}); err != nil {
			t.Fatalf("Configure({}) error: %v", err)
		}

		mc := c.(*managedNamespacesCollector)

		wantKinds := []string{
			"Pods", "Deployments", "StatefulSets", "DaemonSets",
			"Jobs", "CronJobs", "Services", "ConfigMaps", "Secrets",
			"NetworkPolicies", "Routes", "ServiceAccounts", "Roles", "RoleBindings",
		}
		if len(mc.kinds) != len(wantKinds) {
			t.Fatalf("kinds = %v, want %v", mc.kinds, wantKinds)
		}
		for i, k := range wantKinds {
			if mc.kinds[i] != k {
				t.Errorf("kinds[%d] = %q, want %q", i, mc.kinds[i], k)
			}
		}
	})

	t.Run("Configure overrides patterns and kinds", func(t *testing.T) {
		c := newManagedNamespacesCollector()
		err := c.Configure(map[string]string{
			"patterns": "my-ns-*,other-ns",
			"kinds":    "Pods,Services",
		})
		if err != nil {
			t.Fatalf("Configure with custom params error: %v", err)
		}

		mc := c.(*managedNamespacesCollector)

		if len(mc.patterns) != 2 || mc.patterns[0] != "my-ns-*" || mc.patterns[1] != "other-ns" {
			t.Errorf("patterns = %v, want [my-ns-* other-ns]", mc.patterns)
		}
		if len(mc.kinds) != 2 || mc.kinds[0] != "Pods" || mc.kinds[1] != "Services" {
			t.Errorf("kinds = %v, want [Pods Services]", mc.kinds)
		}
	})

	t.Run("registered via init so Get works", func(t *testing.T) {
		// The init() function should have registered "managed-namespaces".
		// We need to ensure it's in the registry. Since registry_test.go
		// resets the registry, we check if the factory was registered by
		// looking at the Name() output from a fresh instance.
		c, err := Get("managed-namespaces")
		if err != nil {
			t.Fatalf("Get(%q) returned error: %v", "managed-namespaces", err)
		}
		if c.Name() != "managed-namespaces" {
			t.Errorf("Name() = %q, want %q", c.Name(), "managed-namespaces")
		}
	})
}

// TestManagedNamespacesCollector_NamespaceMatching_Acceptance verifies that:
// 1. matchNamespace() uses filepath.Match (glob) semantics
// 2. "openshift-*" matches "openshift-monitoring" but not "kube-system"
// 3. "kube-system" matches "kube-system" exactly but not "kube-system-extra"
// 4. Multiple patterns are OR'd — a namespace matching any pattern is included
// 5. Non-matching namespaces are excluded
//
// Acceptance criterion: Given a list of namespace names, pattern matcher
// correctly filters using glob matching against configured patterns including
// wildcards (openshift-* matches openshift-monitoring but not kube-system) and
// exact matches (kube-system matches kube-system).
//
// Phase: RED — the managed-namespaces collector does not exist yet.
func TestManagedNamespacesCollector_NamespaceMatching_Acceptance(t *testing.T) {
	c := newManagedNamespacesCollector()
	// Configure with specific patterns for controlled testing.
	if err := c.Configure(map[string]string{
		"patterns": "openshift-*,kube-system",
		"kinds":    "Pods",
	}); err != nil {
		t.Fatalf("Configure error: %v", err)
	}
	mc := c.(*managedNamespacesCollector)

	tests := []struct {
		namespace string
		want      bool
	}{
		{"openshift-monitoring", true},
		{"openshift-logging", true},
		{"openshift-authentication", true},
		{"kube-system", true},
		{"kube-public", false},       // not in patterns
		{"default", false},           // not in patterns
		{"my-app", false},            // not in patterns
		{"openshift", false},         // "openshift-*" requires dash + suffix
	}

	for _, tt := range tests {
		t.Run(tt.namespace, func(t *testing.T) {
			got := mc.matchNamespace(tt.namespace)
			if got != tt.want {
				t.Errorf("matchNamespace(%q) = %v, want %v", tt.namespace, got, tt.want)
			}
		})
	}

	// Suppress unused import warnings.
	_ = filepath.Match
	_ = strings.Split
}

// TestManagedNamespacesCollector_ResourceEnumeration_Acceptance verifies that:
// 1. Run() accepts a kubeconfigPath and uses it to build dynamic+discovery clients
// 2. The returned JSON has shape {"namespaces": [{"name": "...", "resources": {"Kind": {"count": N, "items": [...]}}}]}
// 3. Only namespaces matching configured patterns are included
// 4. Resource counts match actual items length
// 5. main.go blank-imports the collector package for init() registration
//
// Acceptance criterion: Run() builds dynamic+discovery clients from kubeconfigPath,
// resolves GVRs for configured kinds with hardcoded fallback for core resources,
// lists resources per matched namespace, and returns JSON in the expected shape.
//
// Phase: RED — the managed-namespaces collector does not exist yet.
func TestManagedNamespacesCollector_ResourceEnumeration_Acceptance(t *testing.T) {
	c := newManagedNamespacesCollector()
	if err := c.Configure(map[string]string{
		"patterns": "test-ns-*",
		"kinds":    "Pods,Services",
	}); err != nil {
		t.Fatalf("Configure error: %v", err)
	}

	// Run with a non-existent kubeconfig should return an error (no cluster to connect to),
	// but the important thing is that the method exists and accepts the right arguments.
	// For the acceptance test, we verify the function signature compiles and the output
	// JSON shape is correct by testing with a mock/fake client.

	// Use the clientBuilder override for testing (function variable on the collector).
	mc := c.(*managedNamespacesCollector)

	// Set up a fake client builder that returns predictable results.
	mc.clientBuilder = fakeManagedNSClientBuilder(map[string]map[string][]string{
		"test-ns-alpha": {
			"Pods":     {"pod-1", "pod-2"},
			"Services": {"svc-1"},
		},
		"test-ns-beta": {
			"Pods":     {"pod-3"},
			"Services": {},
		},
		"other-ns": { // should be filtered out by pattern matching
			"Pods": {"pod-ignored"},
		},
	})

	data, err := mc.Run(context.Background(), "cluster-123", "/fake/kubeconfig")
	if err != nil {
		t.Fatalf("Run() error: %v", err)
	}

	// Parse the output JSON.
	var result struct {
		Namespaces []struct {
			Name      string `json:"name"`
			Resources map[string]struct {
				Count int               `json:"count"`
				Items []json.RawMessage `json:"items"`
			} `json:"resources"`
		} `json:"namespaces"`
	}
	if err := json.Unmarshal(data, &result); err != nil {
		t.Fatalf("failed to unmarshal Run() output: %v\nraw: %s", err, data)
	}

	// Verify only matching namespaces are included.
	nsNames := make(map[string]bool)
	for _, ns := range result.Namespaces {
		nsNames[ns.Name] = true
	}
	if nsNames["other-ns"] {
		t.Error("other-ns should be excluded by pattern matching")
	}
	if !nsNames["test-ns-alpha"] {
		t.Error("test-ns-alpha should be included")
	}
	if !nsNames["test-ns-beta"] {
		t.Error("test-ns-beta should be included")
	}

	// Verify resource counts match items.
	for _, ns := range result.Namespaces {
		for kind, res := range ns.Resources {
			if res.Count != len(res.Items) {
				t.Errorf("ns=%s kind=%s: count=%d but len(items)=%d",
					ns.Name, kind, res.Count, len(res.Items))
			}
		}
	}

	// Verify specific counts.
	for _, ns := range result.Namespaces {
		if ns.Name == "test-ns-alpha" {
			if pods, ok := ns.Resources["Pods"]; ok {
				if pods.Count != 2 {
					t.Errorf("test-ns-alpha Pods count = %d, want 2", pods.Count)
				}
			} else {
				t.Error("test-ns-alpha missing Pods resource")
			}
			if svcs, ok := ns.Resources["Services"]; ok {
				if svcs.Count != 1 {
					t.Errorf("test-ns-alpha Services count = %d, want 1", svcs.Count)
				}
			} else {
				t.Error("test-ns-alpha missing Services resource")
			}
		}
	}
}
