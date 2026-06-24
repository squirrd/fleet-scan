package collector

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
)

// TestCollectorFramework_InterfaceAndRegistry_Acceptance verifies that:
// 1. A Collector interface exists with Name(), Configure(map[string]string), and
//    Run(ctx, clusterID, kubeconfigPath) returning (json.RawMessage, error)
// 2. Register(name, factory) adds a collector factory to the global registry
// 3. Get(name) returns a new Collector instance from the registered factory
// 4. List() returns all registered collector names
// 5. Register panics on duplicate name registration
// 6. Get returns an error for unknown collector names
//
// Acceptance criterion: Collector interface (Name/Configure/Run) and registry
// (Register/Get/List) exist with proper error handling for duplicate and unknown names.
//
// Phase: RED — the collector package does not exist yet.
func TestCollectorFramework_InterfaceAndRegistry_Acceptance(t *testing.T) {
	// Reset global registry state between subtests.
	t.Cleanup(func() { resetRegistry() })

	t.Run("register and get a collector", func(t *testing.T) {
		resetRegistry()

		// Register a factory that produces a mock collector.
		Register("test-collector", func() Collector {
			return &stubCollector{name: "test-collector"}
		})

		// Get should return a new instance.
		c, err := Get("test-collector")
		if err != nil {
			t.Fatalf("Get returned error: %v", err)
		}

		// The returned Collector must satisfy the interface.
		if c.Name() != "test-collector" {
			t.Errorf("Name() = %q, want %q", c.Name(), "test-collector")
		}

		// Configure must accept params without error.
		if err := c.Configure(map[string]string{"key": "val"}); err != nil {
			t.Errorf("Configure returned error: %v", err)
		}

		// Run must return valid JSON data.
		data, err := c.Run(context.Background(), "cluster-1", "/tmp/kubeconfig")
		if err != nil {
			t.Fatalf("Run returned error: %v", err)
		}
		if !json.Valid(data) {
			t.Errorf("Run returned invalid JSON: %s", data)
		}
	})

	t.Run("list returns registered names", func(t *testing.T) {
		resetRegistry()

		Register("alpha", func() Collector { return &stubCollector{name: "alpha"} })
		Register("beta", func() Collector { return &stubCollector{name: "beta"} })

		names := List()
		if len(names) != 2 {
			t.Fatalf("List() returned %d names, want 2", len(names))
		}

		found := map[string]bool{}
		for _, n := range names {
			found[n] = true
		}
		if !found["alpha"] || !found["beta"] {
			t.Errorf("List() = %v, want [alpha, beta]", names)
		}
	})

	t.Run("get unknown name returns error", func(t *testing.T) {
		resetRegistry()

		_, err := Get("nonexistent")
		if err == nil {
			t.Fatal("expected error for unknown collector name, got nil")
		}
		if !strings.Contains(err.Error(), "nonexistent") {
			t.Errorf("error should mention the unknown name, got: %v", err)
		}
	})

	t.Run("duplicate registration panics", func(t *testing.T) {
		resetRegistry()

		Register("dup", func() Collector { return &stubCollector{name: "dup"} })

		defer func() {
			r := recover()
			if r == nil {
				t.Fatal("expected panic on duplicate registration, got none")
			}
			msg, ok := r.(string)
			if !ok {
				// Accept any panic value.
				return
			}
			if !strings.Contains(msg, "dup") {
				t.Errorf("panic message should mention the duplicate name, got: %s", msg)
			}
		}()

		Register("dup", func() Collector { return &stubCollector{name: "dup"} })
	})
}

// stubCollector is a minimal Collector implementation for testing.
type stubCollector struct {
	name string
}

func (s *stubCollector) Name() string { return s.name }

func (s *stubCollector) Configure(params map[string]string) error { return nil }

func (s *stubCollector) Run(ctx context.Context, clusterID, kubeconfigPath string) (json.RawMessage, error) {
	return json.RawMessage(`{"stub": true}`), nil
}
