// Package collector: cco-tight-loop detects OCPBUGS-132683 — the GCP Cloud
// Credential Operator tight reconcile loop that occurs on clusters running
// credentialsMode: Manual with Workload Identity Federation enabled
// (serviceAccountIssuer set). Affected clusters never reach a stable/idle
// state: CCO continuously re-syncs every GCP CredentialsRequest, including
// ones that are already correctly provisioned.
//
// Checks are read-only and require no elevated (backplane-cluster-admin)
// access — see workspaces/cre/fleet-scan/work/cco-tight-loop-scan/fleet-scan-note.md
// in the oraculum repo for the diagnostic writeup this collector is based on,
// developed and validated against case 07791204 (sei-rio).
package collector

import (
	"context"
	"encoding/json"
	"fmt"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"
)

const (
	ccoDefaultWindow    = 30 * time.Minute
	ccoDefaultThreshold = 5
	ccoNamespace        = "openshift-cloud-credential-operator"
	ccoLoopLogPattern   = "status has changed, updating"
)

// ccoCRRegexp extracts the CredentialsRequest identity from a CCO log line,
// e.g. `...controller=credreq cr=openshift-cloud-credential-operator/openshift-gcp-ccm secret=...`
var ccoCRRegexp = regexp.MustCompile(`cr=(\S+)`)

// ccoClientBuilderFunc builds a ccoLister from a kubeconfig path.
type ccoClientBuilderFunc func(kubeconfigPath string) (ccoLister, error)

// ccoLister provides the operations needed by the cco-tight-loop collector.
type ccoLister interface {
	// GetCredentialsMode returns spec.credentialsMode of CloudCredential/cluster.
	GetCredentialsMode(ctx context.Context) (string, error)
	// GetServiceAccountIssuer returns spec.serviceAccountIssuer of Authentication/cluster.
	GetServiceAccountIssuer(ctx context.Context) (string, error)
	// GetCCOLogs returns the Cloud Credential Operator pod logs emitted within
	// the given window (e.g. logs --since=<window>).
	GetCCOLogs(ctx context.Context, window time.Duration) (string, error)
}

// ccoTightLoopResult is the top-level JSON output of the cco-tight-loop collector.
type ccoTightLoopResult struct {
	CredentialsMode      string         `json:"credentials_mode"`
	ServiceAccountIssuer string         `json:"service_account_issuer"`
	RiskProfile          bool           `json:"risk_profile"`
	WindowSeconds        int64          `json:"window_seconds"`
	Threshold            int            `json:"threshold"`
	LoopDetected         bool           `json:"loop_detected"`
	MaxResyncCount       int            `json:"max_resync_count"`
	ResyncCounts         map[string]int `json:"resync_counts,omitempty"`
}

// ccoTightLoopCollector detects OCPBUGS-132683 on a single cluster.
type ccoTightLoopCollector struct {
	window        time.Duration
	threshold     int
	clientBuilder ccoClientBuilderFunc
}

// newCCOTightLoopCollector creates a new cco-tight-loop collector.
func newCCOTightLoopCollector() Collector {
	return &ccoTightLoopCollector{
		window:        ccoDefaultWindow,
		threshold:     ccoDefaultThreshold,
		clientBuilder: newCCOKubeClientBuilder(),
	}
}

// Name returns the collector's registered name.
func (c *ccoTightLoopCollector) Name() string { return "cco-tight-loop" }

// Configure parses the "window" (Go duration string, e.g. "30m") and
// "threshold" (int, min resyncs-per-CR within the window to call it a loop)
// params. Defaults: window=30m, threshold=5.
func (c *ccoTightLoopCollector) Configure(params map[string]string) error {
	c.window = ccoDefaultWindow
	c.threshold = ccoDefaultThreshold

	if w, ok := params["window"]; ok && w != "" {
		d, err := time.ParseDuration(w)
		if err != nil {
			return fmt.Errorf("cco-tight-loop: invalid window %q: %w", w, err)
		}
		c.window = d
	}

	if th, ok := params["threshold"]; ok && th != "" {
		n, err := strconv.Atoi(th)
		if err != nil {
			return fmt.Errorf("cco-tight-loop: invalid threshold %q: %w", th, err)
		}
		c.threshold = n
	}

	return nil
}

// Run executes the collector against a single cluster.
func (c *ccoTightLoopCollector) Run(ctx context.Context, _, kubeconfigPath string) (json.RawMessage, error) {
	lister, err := c.clientBuilder(kubeconfigPath)
	if err != nil {
		return nil, fmt.Errorf("cco-tight-loop: building client: %w", err)
	}

	mode, err := lister.GetCredentialsMode(ctx)
	if err != nil {
		return nil, fmt.Errorf("cco-tight-loop: getting credentialsMode: %w", err)
	}

	issuer, err := lister.GetServiceAccountIssuer(ctx)
	if err != nil {
		return nil, fmt.Errorf("cco-tight-loop: getting serviceAccountIssuer: %w", err)
	}

	result := ccoTightLoopResult{
		CredentialsMode:      mode,
		ServiceAccountIssuer: issuer,
		RiskProfile:          isCCORiskProfile(mode, issuer),
		WindowSeconds:        int64(c.window.Seconds()),
		Threshold:            c.threshold,
	}

	// Cheap filter: only clusters in the risk profile (Manual + WIF) can hit
	// this bug, so skip the more expensive log fetch/parse otherwise.
	if !result.RiskProfile {
		return json.Marshal(result)
	}

	logs, err := lister.GetCCOLogs(ctx, c.window)
	if err != nil {
		return nil, fmt.Errorf("cco-tight-loop: getting CCO logs: %w", err)
	}

	counts := countCCOResyncs(logs)
	result.ResyncCounts = counts
	result.MaxResyncCount = maxCount(counts)
	result.LoopDetected = result.MaxResyncCount >= c.threshold

	return json.Marshal(result)
}

// isCCORiskProfile reports whether the cluster configuration matches the
// precondition for OCPBUGS-132683: GCP WIF (serviceAccountIssuer set) with
// credentialsMode: Manual.
func isCCORiskProfile(credentialsMode, serviceAccountIssuer string) bool {
	return credentialsMode == "Manual" && strings.TrimSpace(serviceAccountIssuer) != ""
}

// countCCOResyncs counts "status has changed, updating" log lines per
// CredentialsRequest (the cr=<namespace>/<name> field in each log line).
func countCCOResyncs(logs string) map[string]int {
	counts := make(map[string]int)
	for _, line := range strings.Split(logs, "\n") {
		if !strings.Contains(line, ccoLoopLogPattern) {
			continue
		}
		match := ccoCRRegexp.FindStringSubmatch(line)
		if match == nil {
			continue
		}
		counts[match[1]]++
	}
	return counts
}

// maxCount returns the largest value in counts, or 0 if counts is empty.
func maxCount(counts map[string]int) int {
	max := 0
	keys := make([]string, 0, len(counts))
	for k := range counts {
		keys = append(keys, k)
	}
	sort.Strings(keys) // deterministic iteration for equal-max ties
	for _, k := range keys {
		if counts[k] > max {
			max = counts[k]
		}
	}
	return max
}

func init() {
	Register("cco-tight-loop", newCCOTightLoopCollector)
}
