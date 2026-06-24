package cli

import (
	"context"
	"encoding/json"
	"fmt"
	"testing"

	"github.com/squirrd/fleet-scan/internal/collector"
)

// TestParseCollectorSpecs uses table-driven tests to verify parsing of
// --collector flag values in the format name:key=val,key2=val2.
func TestParseCollectorSpecs(t *testing.T) {
	tests := []struct {
		name      string
		input     []string
		want      []CollectorSpec
		wantErr   bool
		errSubstr string
	}{
		{
			name:  "simple name only",
			input: []string{"managed-namespaces"},
			want: []CollectorSpec{
				{Name: "managed-namespaces", Params: map[string]string{}},
			},
		},
		{
			name:  "name with one param",
			input: []string{"high-restarts:threshold=10"},
			want: []CollectorSpec{
				{Name: "high-restarts", Params: map[string]string{"threshold": "10"}},
			},
		},
		{
			name:  "name with multiple params",
			input: []string{"managed-namespaces:patterns=openshift-*,kinds=Pods"},
			want: []CollectorSpec{
				{Name: "managed-namespaces", Params: map[string]string{
					"patterns": "openshift-*",
					"kinds":    "Pods",
				}},
			},
		},
		{
			name:      "malformed - missing name (starts with colon)",
			input:     []string{":key=val"},
			wantErr:   true,
			errSubstr: "name",
		},
		{
			name:      "malformed - bad key=val (missing equals)",
			input:     []string{"collector:badparam"},
			wantErr:   true,
			errSubstr: "=",
		},
		{
			name:  "empty string",
			input: []string{""},
			want:  nil, // or empty slice — no specs parsed from empty input
		},
		{
			name:  "multiple specs",
			input: []string{"managed-namespaces", "high-restarts:threshold=10"},
			want: []CollectorSpec{
				{Name: "managed-namespaces", Params: map[string]string{}},
				{Name: "high-restarts", Params: map[string]string{"threshold": "10"}},
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := ParseCollectorSpecs(tt.input)

			if tt.wantErr {
				if err == nil {
					t.Fatalf("expected error containing %q, got nil", tt.errSubstr)
				}
				if tt.errSubstr != "" && !containsStr(err.Error(), tt.errSubstr) {
					t.Errorf("expected error containing %q, got: %v", tt.errSubstr, err)
				}
				return
			}

			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}

			if len(got) != len(tt.want) {
				t.Fatalf("got %d specs, want %d", len(got), len(tt.want))
			}

			for i, wantSpec := range tt.want {
				gotSpec := got[i]
				if gotSpec.Name != wantSpec.Name {
					t.Errorf("spec[%d].Name = %q, want %q", i, gotSpec.Name, wantSpec.Name)
				}
				if len(gotSpec.Params) != len(wantSpec.Params) {
					t.Errorf("spec[%d].Params has %d entries, want %d", i, len(gotSpec.Params), len(wantSpec.Params))
					continue
				}
				for k, v := range wantSpec.Params {
					if gotSpec.Params[k] != v {
						t.Errorf("spec[%d].Params[%q] = %q, want %q", i, k, gotSpec.Params[k], v)
					}
				}
			}
		})
	}
}

// TestCollectorRequiredUnlessDryRun verifies that validation requires at least
// one collector unless dry-run mode is enabled.
func TestCollectorRequiredUnlessDryRun(t *testing.T) {
	tests := []struct {
		name       string
		collectors []CollectorSpec
		dryRun     bool
		wantErr    bool
	}{
		{
			name:       "no collectors and not dry-run returns error",
			collectors: nil,
			dryRun:     false,
			wantErr:    true,
		},
		{
			name:       "no collectors and dry-run returns nil",
			collectors: nil,
			dryRun:     true,
			wantErr:    false,
		},
		{
			name: "with collectors and not dry-run returns nil",
			collectors: []CollectorSpec{
				{Name: "managed-namespaces", Params: map[string]string{}},
			},
			dryRun:  false,
			wantErr: false,
		},
		{
			name:       "empty slice and not dry-run returns error",
			collectors: []CollectorSpec{},
			dryRun:     false,
			wantErr:    true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := ValidateCollectors(tt.collectors, tt.dryRun)
			if tt.wantErr && err == nil {
				t.Error("expected error, got nil")
			}
			if !tt.wantErr && err != nil {
				t.Errorf("expected no error, got: %v", err)
			}
		})
	}
}

// TestCollectorFramework_CliValidation_Acceptance verifies that:
// 1. ValidateCollectors checks each collector name against the registry
// 2. Unknown collector names cause ValidateCollectors to return an error naming the unknown collector
// 3. Known collectors pass validation and have Configure called with their parsed params
// 4. Configure errors are surfaced by ValidateCollectors
//
// Acceptance criterion: ValidateCollectors rejects unknown collector names by
// checking the registry; Configure is called with parsed params before the run starts.
//
// Phase: RED — ValidateCollectors does not yet check the registry.
func TestCollectorFramework_CliValidation_Acceptance(t *testing.T) {
	t.Cleanup(func() { collector.ResetRegistry() })

	t.Run("rejects unknown collector name", func(t *testing.T) {
		collector.ResetRegistry()

		specs := []CollectorSpec{
			{Name: "nonexistent-collector", Params: map[string]string{}},
		}

		err := ValidateCollectors(specs, false)
		if err == nil {
			t.Fatal("expected error for unknown collector, got nil")
		}
		if !containsStr(err.Error(), "nonexistent-collector") {
			t.Errorf("error should mention the unknown collector name, got: %v", err)
		}
	})

	t.Run("accepts known collector and calls Configure", func(t *testing.T) {
		collector.ResetRegistry()

		var configuredParams map[string]string
		collector.Register("known-collector", func() collector.Collector {
			return &configTracker{
				name:     "known-collector",
				onConfig: func(p map[string]string) { configuredParams = p },
			}
		})

		specs := []CollectorSpec{
			{Name: "known-collector", Params: map[string]string{"key": "val"}},
		}

		err := ValidateCollectors(specs, false)
		if err != nil {
			t.Fatalf("ValidateCollectors returned error for known collector: %v", err)
		}

		// Configure must have been called with the parsed params.
		if configuredParams == nil {
			t.Fatal("Configure was not called on the collector")
		}
		if configuredParams["key"] != "val" {
			t.Errorf("Configure params[key] = %q, want %q", configuredParams["key"], "val")
		}
	})

	t.Run("surfaces Configure error", func(t *testing.T) {
		collector.ResetRegistry()

		collector.Register("bad-config", func() collector.Collector {
			return &configTracker{
				name:      "bad-config",
				configErr: "invalid param: threshold must be numeric",
			}
		})

		specs := []CollectorSpec{
			{Name: "bad-config", Params: map[string]string{"threshold": "abc"}},
		}

		err := ValidateCollectors(specs, false)
		if err == nil {
			t.Fatal("expected error from Configure failure, got nil")
		}
		if !containsStr(err.Error(), "threshold must be numeric") {
			t.Errorf("error should contain Configure error message, got: %v", err)
		}
	})

	t.Run("dry-run with no collectors still passes", func(t *testing.T) {
		collector.ResetRegistry()

		err := ValidateCollectors(nil, true)
		if err != nil {
			t.Errorf("dry-run with no collectors should pass, got: %v", err)
		}
	})
}

// configTracker is a test helper that records Configure calls.
type configTracker struct {
	name      string
	configErr string
	onConfig  func(map[string]string)
}

func (c *configTracker) Name() string { return c.name }

func (c *configTracker) Configure(params map[string]string) error {
	if c.onConfig != nil {
		c.onConfig(params)
	}
	if c.configErr != "" {
		return fmt.Errorf(c.configErr)
	}
	return nil
}

func (c *configTracker) Run(ctx context.Context, clusterID, kubeconfigPath string) (json.RawMessage, error) {
	return json.RawMessage(`{}`), nil
}

// containsStr is a simple helper to avoid importing strings in tests.
func containsStr(s, substr string) bool {
	for i := 0; i <= len(s)-len(substr); i++ {
		if s[i:i+len(substr)] == substr {
			return true
		}
	}
	return false
}
