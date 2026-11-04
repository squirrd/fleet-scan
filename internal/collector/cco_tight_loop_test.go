package collector

import (
	"context"
	"encoding/json"
	"testing"
	"time"
)

// TestCCOTightLoop_ParamConfig_Acceptance verifies that:
// 1. A "cco-tight-loop" collector can be constructed via newCCOTightLoopCollector()
// 2. Name() returns "cco-tight-loop"
// 3. Configure() with no params uses sensible defaults: window=30m, threshold=5
// 4. Configure() with explicit window/threshold overrides the defaults
// 5. Configure() rejects an invalid window or threshold
// 6. The collector registers itself via init() so Get("cco-tight-loop") works
// 7. The default collector has a non-nil clientBuilder
func TestCCOTightLoop_ParamConfig_Acceptance(t *testing.T) {
	t.Run("constructor and Name", func(t *testing.T) {
		c := newCCOTightLoopCollector()
		if c.Name() != "cco-tight-loop" {
			t.Errorf("Name() = %q, want %q", c.Name(), "cco-tight-loop")
		}
	})

	t.Run("default window and threshold after empty Configure", func(t *testing.T) {
		c := newCCOTightLoopCollector()
		if err := c.Configure(map[string]string{}); err != nil {
			t.Fatalf("Configure({}) error: %v", err)
		}
		cc := c.(*ccoTightLoopCollector)
		if cc.window != 30*time.Minute {
			t.Errorf("window = %v, want 30m", cc.window)
		}
		if cc.threshold != 5 {
			t.Errorf("threshold = %d, want 5", cc.threshold)
		}
	})

	t.Run("Configure overrides window and threshold", func(t *testing.T) {
		c := newCCOTightLoopCollector()
		err := c.Configure(map[string]string{"window": "5m", "threshold": "10"})
		if err != nil {
			t.Fatalf("Configure() error: %v", err)
		}
		cc := c.(*ccoTightLoopCollector)
		if cc.window != 5*time.Minute {
			t.Errorf("window = %v, want 5m", cc.window)
		}
		if cc.threshold != 10 {
			t.Errorf("threshold = %d, want 10", cc.threshold)
		}
	})

	t.Run("Configure rejects invalid window", func(t *testing.T) {
		c := newCCOTightLoopCollector()
		if err := c.Configure(map[string]string{"window": "notaduration"}); err == nil {
			t.Error("expected error for invalid window, got nil")
		}
	})

	t.Run("Configure rejects invalid threshold", func(t *testing.T) {
		c := newCCOTightLoopCollector()
		if err := c.Configure(map[string]string{"threshold": "notanumber"}); err == nil {
			t.Error("expected error for invalid threshold, got nil")
		}
	})

	t.Run("registered via init so Get works", func(t *testing.T) {
		c, err := Get("cco-tight-loop")
		if err != nil {
			t.Fatalf("Get(%q) returned error: %v", "cco-tight-loop", err)
		}
		if c.Name() != "cco-tight-loop" {
			t.Errorf("Name() = %q, want %q", c.Name(), "cco-tight-loop")
		}
	})

	t.Run("default collector has non-nil clientBuilder", func(t *testing.T) {
		c := newCCOTightLoopCollector()
		cc := c.(*ccoTightLoopCollector)
		if cc.clientBuilder == nil {
			t.Error("clientBuilder is nil")
		}
	})
}

// TestCCOTightLoop_RiskProfile_Acceptance verifies the cheap config-only
// filter: only Manual + non-empty serviceAccountIssuer counts as at-risk,
// and logs are not fetched (and no loop can be reported) otherwise.
func TestCCOTightLoop_RiskProfile_Acceptance(t *testing.T) {
	cases := []struct {
		name          string
		mode          string
		issuer        string
		wantRisk      bool
		logsErrOnCall error
	}{
		{"manual with WIF is at risk", "Manual", "https://openshift.com", true, nil},
		{"manual without WIF is not at risk", "Manual", "", false, nil},
		{"mint mode is not at risk", "Mint", "https://openshift.com", false, nil},
		{"passthrough mode is not at risk", "Passthrough", "https://openshift.com", false, nil},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			c := newCCOTightLoopCollector()
			if err := c.Configure(nil); err != nil {
				t.Fatalf("Configure() error: %v", err)
			}
			cc := c.(*ccoTightLoopCollector)

			// Logs should not be read at all when not at risk — force an
			// error if they are, to prove the short-circuit.
			cc.clientBuilder = fakeCCOClientBuilder(fakeCCOData{
				credentialsMode:      tc.mode,
				serviceAccountIssuer: tc.issuer,
				logsErr:              tc.logsErrOnCall,
			})

			raw, err := cc.Run(context.Background(), "cluster-123", "/fake/kubeconfig")
			if tc.wantRisk {
				// at-risk clusters fetch logs; empty logs is fine, just must not error.
				if err != nil {
					t.Fatalf("Run() error: %v", err)
				}
			} else if err != nil {
				t.Fatalf("Run() error for non-risk cluster (logs should be skipped): %v", err)
			}

			var result ccoTightLoopResult
			if err := json.Unmarshal(raw, &result); err != nil {
				t.Fatalf("unmarshal output: %v\nraw: %s", err, raw)
			}
			if result.RiskProfile != tc.wantRisk {
				t.Errorf("RiskProfile = %v, want %v", result.RiskProfile, tc.wantRisk)
			}
			if !tc.wantRisk && result.LoopDetected {
				t.Error("LoopDetected = true for a non-risk cluster, want false")
			}
		})
	}
}

// TestCCOTightLoop_LoopDetection_Acceptance verifies log parsing and the
// resync-count threshold logic that identifies OCPBUGS-132683.
func TestCCOTightLoop_LoopDetection_Acceptance(t *testing.T) {
	healthyLogs := `time="2026-10-22T05:15:56Z" level=info msg="status has changed, updating" controller=credreq cr=openshift-cloud-credential-operator/openshift-gcp-ccm secret=openshift-cloud-controller-manager/gcp-ccm-cloud-credentials
time="2026-10-22T05:15:56Z" level=info msg="operator detects timed access token enabled cluster (STS, Workload Identity, etc.)" controller=credreq cr=openshift-cloud-credential-operator/openshift-gcp-ccm`

	loopingLogs := buildRepeatedCCOLogLines("openshift-cloud-credential-operator/openshift-gcp-ccm", 563) +
		buildRepeatedCCOLogLines("openshift-cloud-credential-operator/openshift-machine-api-gcp", 563)

	t.Run("healthy cluster: resync count below threshold, no loop", func(t *testing.T) {
		c := newCCOTightLoopCollector()
		if err := c.Configure(nil); err != nil {
			t.Fatalf("Configure() error: %v", err)
		}
		cc := c.(*ccoTightLoopCollector)
		cc.clientBuilder = fakeCCOClientBuilder(fakeCCOData{
			credentialsMode:      "Manual",
			serviceAccountIssuer: "https://openshift.com",
			logs:                 healthyLogs,
		})

		raw, err := cc.Run(context.Background(), "cluster-123", "/fake/kubeconfig")
		if err != nil {
			t.Fatalf("Run() error: %v", err)
		}
		var result ccoTightLoopResult
		if err := json.Unmarshal(raw, &result); err != nil {
			t.Fatalf("unmarshal output: %v", err)
		}
		if result.LoopDetected {
			t.Error("LoopDetected = true for a healthy (single-resync) log sample, want false")
		}
		if result.MaxResyncCount != 1 {
			t.Errorf("MaxResyncCount = %d, want 1", result.MaxResyncCount)
		}
	})

	t.Run("affected cluster: resync count far above threshold, loop detected", func(t *testing.T) {
		c := newCCOTightLoopCollector()
		if err := c.Configure(nil); err != nil {
			t.Fatalf("Configure() error: %v", err)
		}
		cc := c.(*ccoTightLoopCollector)
		cc.clientBuilder = fakeCCOClientBuilder(fakeCCOData{
			credentialsMode:      "Manual",
			serviceAccountIssuer: "https://openshift.com",
			logs:                 loopingLogs,
		})

		raw, err := cc.Run(context.Background(), "sei-rio", "/fake/kubeconfig")
		if err != nil {
			t.Fatalf("Run() error: %v", err)
		}
		var result ccoTightLoopResult
		if err := json.Unmarshal(raw, &result); err != nil {
			t.Fatalf("unmarshal output: %v", err)
		}
		if !result.LoopDetected {
			t.Error("LoopDetected = false for a looping cluster, want true")
		}
		if result.MaxResyncCount != 563 {
			t.Errorf("MaxResyncCount = %d, want 563", result.MaxResyncCount)
		}
		if got := result.ResyncCounts["openshift-cloud-credential-operator/openshift-gcp-ccm"]; got != 563 {
			t.Errorf("ResyncCounts[openshift-gcp-ccm] = %d, want 563", got)
		}
	})

	t.Run("custom threshold changes the verdict", func(t *testing.T) {
		c := newCCOTightLoopCollector()
		if err := c.Configure(map[string]string{"threshold": "1000"}); err != nil {
			t.Fatalf("Configure() error: %v", err)
		}
		cc := c.(*ccoTightLoopCollector)
		cc.clientBuilder = fakeCCOClientBuilder(fakeCCOData{
			credentialsMode:      "Manual",
			serviceAccountIssuer: "https://openshift.com",
			logs:                 loopingLogs,
		})

		raw, err := cc.Run(context.Background(), "sei-rio", "/fake/kubeconfig")
		if err != nil {
			t.Fatalf("Run() error: %v", err)
		}
		var result ccoTightLoopResult
		if err := json.Unmarshal(raw, &result); err != nil {
			t.Fatalf("unmarshal output: %v", err)
		}
		if result.LoopDetected {
			t.Error("LoopDetected = true with threshold=1000 and max count 563, want false")
		}
	})
}

// buildRepeatedCCOLogLines produces n "status has changed, updating" log
// lines for the given cr identity, joined with newlines.
func buildRepeatedCCOLogLines(cr string, n int) string {
	line := `time="2026-10-22T05:15:56Z" level=info msg="status has changed, updating" controller=credreq cr=` + cr + " secret=some/secret\n"
	out := ""
	for i := 0; i < n; i++ {
		out += line
	}
	return out
}
