//go:build integration

package cli

import (
	"bytes"
	"strings"
	"testing"
)

// TestDryRunPrintsClusterCount runs the scan command with --dry-run and --search,
// and verifies that the output contains a cluster count.
// This is an integration test that requires OCM credentials to be available.
func TestDryRunPrintsClusterCount(t *testing.T) {
	root := NewRootCommand()

	buf := new(bytes.Buffer)
	root.SetOut(buf)
	root.SetErr(buf)
	root.SetArgs([]string{
		"scan",
		"--search", "managed='true'",
		"--dry-run",
	})

	err := root.Execute()
	if err != nil {
		t.Fatalf("scan --dry-run returned error: %v", err)
	}

	output := buf.String()

	// The dry-run output should contain a cluster count.
	// We look for common patterns like "N clusters" or "clusters: N" or "Found N clusters".
	if !strings.Contains(strings.ToLower(output), "cluster") {
		t.Errorf("expected dry-run output to mention clusters, got:\n%s", output)
	}

	// Verify the output contains at least one digit (the count).
	hasDigit := false
	for _, r := range output {
		if r >= '0' && r <= '9' {
			hasDigit = true
			break
		}
	}
	if !hasDigit {
		t.Errorf("expected dry-run output to contain a cluster count (number), got:\n%s", output)
	}
}
