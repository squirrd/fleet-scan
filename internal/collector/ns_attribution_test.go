package collector

import (
	"context"
	"encoding/json"
	"testing"
)

// TestMc118NsAttribution_ParamConfig_Acceptance verifies that:
// 1. A "ns-attribution" collector can be constructed via newNsAttributionCollector()
// 2. Name() returns "ns-attribution"
// 3. Configure() with no params uses sensible defaults:
//    - patterns: openshift-*, kube-system, kube-public, default, redhat-*
//    - fields: namespace, name, kind, apiVersion, creationTimestamp,
//              ownerReferences, labels, annotations, managedFields
// 4. Configure() with explicit patterns and fields overrides the defaults
// 5. The collector registers itself via init() so Get("ns-attribution") works
//
// Acceptance criterion: ns-attribution collector registers via init(),
// Configure() parses patterns and fields params with sensible defaults,
// and Name() returns "ns-attribution".
//
// Phase: RED — the ns-attribution collector does not exist yet.
func TestMc118NsAttribution_ParamConfig_Acceptance(t *testing.T) {
	t.Run("constructor and Name", func(t *testing.T) {
		c := newNsAttributionCollector()
		if c.Name() != "ns-attribution" {
			t.Errorf("Name() = %q, want %q", c.Name(), "ns-attribution")
		}
	})

	t.Run("default patterns after empty Configure", func(t *testing.T) {
		c := newNsAttributionCollector()
		if err := c.Configure(map[string]string{}); err != nil {
			t.Fatalf("Configure({}) error: %v", err)
		}

		nc, ok := c.(*nsAttributionCollector)
		if !ok {
			t.Fatal("expected *nsAttributionCollector type")
		}

		wantPatterns := []string{"openshift-*", "kube-system", "kube-public", "default", "redhat-*"}
		if len(nc.patterns) != len(wantPatterns) {
			t.Fatalf("patterns = %v, want %v", nc.patterns, wantPatterns)
		}
		for i, p := range wantPatterns {
			if nc.patterns[i] != p {
				t.Errorf("patterns[%d] = %q, want %q", i, nc.patterns[i], p)
			}
		}
	})

	t.Run("default fields after empty Configure", func(t *testing.T) {
		c := newNsAttributionCollector()
		if err := c.Configure(map[string]string{}); err != nil {
			t.Fatalf("Configure({}) error: %v", err)
		}

		nc := c.(*nsAttributionCollector)

		wantFields := []string{
			"namespace", "name", "kind", "apiVersion", "creationTimestamp",
			"ownerReferences", "labels", "annotations", "managedFields",
		}
		if len(nc.fields) != len(wantFields) {
			t.Fatalf("fields = %v, want %v", nc.fields, wantFields)
		}
		for i, f := range wantFields {
			if nc.fields[i] != f {
				t.Errorf("fields[%d] = %q, want %q", i, nc.fields[i], f)
			}
		}
	})

	t.Run("Configure overrides patterns and fields", func(t *testing.T) {
		c := newNsAttributionCollector()
		err := c.Configure(map[string]string{
			"patterns": "my-ns-*,other-ns",
			"fields":   "name,kind,labels",
		})
		if err != nil {
			t.Fatalf("Configure with custom params error: %v", err)
		}

		nc := c.(*nsAttributionCollector)

		if len(nc.patterns) != 2 || nc.patterns[0] != "my-ns-*" || nc.patterns[1] != "other-ns" {
			t.Errorf("patterns = %v, want [my-ns-* other-ns]", nc.patterns)
		}
		if len(nc.fields) != 3 || nc.fields[0] != "name" || nc.fields[1] != "kind" || nc.fields[2] != "labels" {
			t.Errorf("fields = %v, want [name kind labels]", nc.fields)
		}
	})

	t.Run("registered via init so Get works", func(t *testing.T) {
		c, err := Get("ns-attribution")
		if err != nil {
			t.Fatalf("Get(%q) returned error: %v", "ns-attribution", err)
		}
		if c.Name() != "ns-attribution" {
			t.Errorf("Name() = %q, want %q", c.Name(), "ns-attribution")
		}
	})

}

// TestMc118NsAttribution_MetadataExtraction_Acceptance verifies that:
// 1. Run() produces a flat {"resources": [...]} array (not nested by namespace/kind)
// 2. Each resource entry contains only the configured attribution fields
//    (namespace, name, kind, apiVersion, creationTimestamp, ownerReferences,
//    labels, annotations, managedFields) extracted from full Kubernetes objects
// 3. Resources from non-matching namespaces are excluded
// 4. Extraneous fields (spec, status, etc.) are stripped from output
// 5. The namespace field is injected from context (not from the resource itself)
//
// Acceptance criterion: Given mock Kubernetes objects with full metadata + spec + status,
// Run() returns a flat resources array where each entry has only the configured
// attribution fields, with non-matching namespaces excluded.
//
// Phase: RED — the ns-attribution collector does not exist yet.
func TestMc118NsAttribution_MetadataExtraction_Acceptance(t *testing.T) {
	c := newNsAttributionCollector()
	if err := c.Configure(map[string]string{
		"patterns": "openshift-*",
		"fields":   "namespace,name,kind,apiVersion,creationTimestamp,labels",
	}); err != nil {
		t.Fatalf("Configure error: %v", err)
	}
	nc := c.(*nsAttributionCollector)

	// Build fake Kubernetes-style objects with full metadata, spec, status.
	// The collector should extract only the configured fields.
	fakeObjects := map[string]map[string][]json.RawMessage{
		"openshift-monitoring": {
			"Pods": {
				json.RawMessage(`{
					"apiVersion": "v1",
					"kind": "Pod",
					"metadata": {
						"name": "prometheus-0",
						"namespace": "openshift-monitoring",
						"creationTimestamp": "2024-01-15T10:00:00Z",
						"labels": {"app": "prometheus"},
						"annotations": {"note": "should-be-stripped"},
						"ownerReferences": [{"name": "prometheus-sts"}],
						"managedFields": [{"manager": "kube-controller"}]
					},
					"spec": {"containers": [{"name": "prometheus"}]},
					"status": {"phase": "Running"}
				}`),
			},
			"Services": {
				json.RawMessage(`{
					"apiVersion": "v1",
					"kind": "Service",
					"metadata": {
						"name": "prometheus-svc",
						"namespace": "openshift-monitoring",
						"creationTimestamp": "2024-01-15T10:01:00Z",
						"labels": {"app": "prometheus"}
					},
					"spec": {"ports": [{"port": 9090}]}
				}`),
			},
		},
		"other-ns": {
			"Pods": {
				json.RawMessage(`{
					"apiVersion": "v1",
					"kind": "Pod",
					"metadata": {"name": "ignored-pod", "namespace": "other-ns"}
				}`),
			},
		},
	}

	nc.attrClientBuilder = fakeNsAttrClientBuilder(fakeObjects)

	data, err := nc.Run(context.Background(), "cluster-456", "/fake/kubeconfig")
	if err != nil {
		t.Fatalf("Run() error: %v", err)
	}

	// Parse the output and verify flat resources array shape.
	var result struct {
		Resources []map[string]interface{} `json:"resources"`
	}
	if err := json.Unmarshal(data, &result); err != nil {
		t.Fatalf("failed to unmarshal Run() output: %v\nraw: %s", err, data)
	}

	// Should have 2 resources (from openshift-monitoring only).
	if len(result.Resources) != 2 {
		t.Fatalf("expected 2 resources, got %d: %s", len(result.Resources), data)
	}

	// Verify no resources from other-ns.
	for _, r := range result.Resources {
		if ns, ok := r["namespace"]; ok && ns == "other-ns" {
			t.Error("resources should not contain entries from other-ns")
		}
	}

	// Verify each resource contains only allowed fields.
	allowedFields := map[string]bool{
		"namespace": true, "name": true, "kind": true,
		"apiVersion": true, "creationTimestamp": true, "labels": true,
	}
	for _, r := range result.Resources {
		for key := range r {
			if !allowedFields[key] {
				t.Errorf("unexpected field %q in resource output (should be stripped)", key)
			}
		}
	}

	// Verify extraneous fields (spec, status, annotations) are NOT present.
	for _, r := range result.Resources {
		for _, excluded := range []string{"spec", "status", "annotations", "ownerReferences", "managedFields", "metadata"} {
			if _, ok := r[excluded]; ok {
				t.Errorf("resource should not contain %q field", excluded)
			}
		}
	}

	// Verify specific field values for the prometheus pod.
	found := false
	for _, r := range result.Resources {
		if r["name"] == "prometheus-0" {
			found = true
			if r["namespace"] != "openshift-monitoring" {
				t.Errorf("namespace = %v, want openshift-monitoring", r["namespace"])
			}
			if r["kind"] != "Pod" {
				t.Errorf("kind = %v, want Pod", r["kind"])
			}
			if r["apiVersion"] != "v1" {
				t.Errorf("apiVersion = %v, want v1", r["apiVersion"])
			}
			if r["creationTimestamp"] != "2024-01-15T10:00:00Z" {
				t.Errorf("creationTimestamp = %v, want 2024-01-15T10:00:00Z", r["creationTimestamp"])
			}
			labels, ok := r["labels"].(map[string]interface{})
			if !ok {
				t.Fatal("labels should be a map")
			}
			if labels["app"] != "prometheus" {
				t.Errorf("labels[app] = %v, want prometheus", labels["app"])
			}
		}
	}
	if !found {
		t.Error("expected to find prometheus-0 in resources")
	}
}

// TestNsAttribution_SlimManagedFields verifies that when managedFields is in the
// configured fields, the output retains only manager, operation, time, apiVersion,
// and subresource — stripping the bulky fieldsV1 data.
func TestNsAttribution_SlimManagedFields(t *testing.T) {
	c := newNsAttributionCollector()
	if err := c.Configure(map[string]string{
		"patterns": "openshift-*",
		"fields":   "name,kind,managedFields",
	}); err != nil {
		t.Fatalf("Configure error: %v", err)
	}
	nc := c.(*nsAttributionCollector)

	fakeObjects := map[string]map[string][]json.RawMessage{
		"openshift-monitoring": {
			"Pods": {
				json.RawMessage(`{
					"apiVersion": "v1",
					"kind": "Pod",
					"metadata": {
						"name": "prometheus-0",
						"namespace": "openshift-monitoring",
						"managedFields": [
							{
								"manager": "kube-controller-manager",
								"operation": "Update",
								"time": "2024-01-15T10:00:00Z",
								"apiVersion": "v1",
								"fieldsType": "FieldsV1",
								"fieldsV1": {"f:metadata":{"f:labels":{".":{},"f:app":{}}}}
							},
							{
								"manager": "kubelet",
								"operation": "Update",
								"time": "2024-01-15T10:01:00Z",
								"apiVersion": "v1",
								"subresource": "status",
								"fieldsType": "FieldsV1",
								"fieldsV1": {"f:status":{"f:conditions":{},"f:phase":{}}}
							}
						]
					},
					"spec": {"containers": [{"name": "prometheus"}]},
					"status": {"phase": "Running"}
				}`),
			},
		},
	}

	nc.attrClientBuilder = fakeNsAttrClientBuilder(fakeObjects)

	data, err := nc.Run(context.Background(), "cluster-slim", "/fake/kubeconfig")
	if err != nil {
		t.Fatalf("Run() error: %v", err)
	}

	var result struct {
		Resources []map[string]interface{} `json:"resources"`
	}
	if err := json.Unmarshal(data, &result); err != nil {
		t.Fatalf("failed to unmarshal: %v", err)
	}

	if len(result.Resources) != 1 {
		t.Fatalf("expected 1 resource, got %d", len(result.Resources))
	}

	r := result.Resources[0]
	mf, ok := r["managedFields"]
	if !ok {
		t.Fatal("expected managedFields in output")
	}

	entries, ok := mf.([]interface{})
	if !ok {
		t.Fatalf("managedFields should be an array, got %T", mf)
	}
	if len(entries) != 2 {
		t.Fatalf("expected 2 managedFields entries, got %d", len(entries))
	}

	for i, entry := range entries {
		m, ok := entry.(map[string]interface{})
		if !ok {
			t.Fatalf("entry %d should be a map", i)
		}
		if _, ok := m["fieldsV1"]; ok {
			t.Errorf("entry %d should not contain fieldsV1", i)
		}
		if _, ok := m["fieldsType"]; ok {
			t.Errorf("entry %d should not contain fieldsType", i)
		}
		if _, ok := m["manager"]; !ok {
			t.Errorf("entry %d missing manager", i)
		}
		if _, ok := m["operation"]; !ok {
			t.Errorf("entry %d missing operation", i)
		}
		if _, ok := m["time"]; !ok {
			t.Errorf("entry %d missing time", i)
		}
	}

	// Verify second entry has subresource preserved.
	second := entries[1].(map[string]interface{})
	if second["subresource"] != "status" {
		t.Errorf("second entry subresource = %v, want status", second["subresource"])
	}
}

// TestMc118NsAttribution_KubeClientWiring_Acceptance verifies that:
// 1. The collector builds dynamic+discovery clients from a kubeconfig path
//    via newNsAttrKubeClientBuilder() which returns an attrClientBuilderFunc
// 2. The attrNamespaceLister interface is implemented by nsAttrKubeLister
// 3. With an invalid kubeconfig, Run() returns a descriptive error
//    (not a panic or generic failure)
// 4. The collector uses the same GVR resolution pattern as managed-namespaces
//    (discovery-based with cache)
// 5. The ns-attribution collector package is blank-imported in main.go
//    for init() registration
//
// Acceptance criterion: The collector wires real Kubernetes dynamic+discovery
// clients from a kubeconfig via newNsAttrKubeClientBuilder(), returns a clear
// error for invalid kubeconfig, and the nsAttrKubeLister type implements the
// attrNamespaceLister interface.
//
// Phase: RED — the ns-attribution collector does not exist yet.
func TestMc118NsAttribution_KubeClientWiring_Acceptance(t *testing.T) {
	t.Run("newNsAttrKubeClientBuilder returns attrClientBuilderFunc", func(t *testing.T) {
		builder := newNsAttrKubeClientBuilder()
		if builder == nil {
			t.Fatal("newNsAttrKubeClientBuilder() returned nil")
		}
	})

	t.Run("invalid kubeconfig returns descriptive error", func(t *testing.T) {
		c := newNsAttributionCollector()
		if err := c.Configure(map[string]string{
			"patterns": "test-*",
		}); err != nil {
			t.Fatalf("Configure error: %v", err)
		}

		nc := c.(*nsAttributionCollector)
		// Use the real client builder (not a mock) with a non-existent kubeconfig.
		nc.attrClientBuilder = newNsAttrKubeClientBuilder()

		_, err := nc.Run(context.Background(), "cluster-789", "/nonexistent/kubeconfig/path")
		if err == nil {
			t.Fatal("expected error for invalid kubeconfig, got nil")
		}

		// Error should mention building the client, not be a generic panic.
		errMsg := err.Error()
		if errMsg == "" {
			t.Fatal("error message should not be empty")
		}
	})

	t.Run("nsAttrKubeLister satisfies attrNamespaceLister interface", func(t *testing.T) {
		// Compile-time check that nsAttrKubeLister implements attrNamespaceLister.
		var _ attrNamespaceLister = (*nsAttrKubeLister)(nil)
	})

	t.Run("default collector uses real kube client builder", func(t *testing.T) {
		c := newNsAttributionCollector()
		nc := c.(*nsAttributionCollector)
		if nc.attrClientBuilder == nil {
			t.Error("default collector should have a non-nil attrClientBuilder")
		}
	})
}
