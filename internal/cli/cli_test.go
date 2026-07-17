package cli

import (
	"bytes"
	"strings"
	"testing"

	"github.com/spf13/cobra"
)

// TestScanCommandHelp verifies that the root command has a "scan" subcommand
// and that scan --help output contains all expected flags.
func TestScanCommandHelp(t *testing.T) {
	root := NewRootCommand("test")

	// The root command must have a "scan" subcommand.
	var scanCmd = findSubcommand(root, "scan")
	if scanCmd == nil {
		t.Fatal("expected root command to have a 'scan' subcommand, but it was not found")
	}

	// Execute scan --help and capture output.
	buf := new(bytes.Buffer)
	root.SetOut(buf)
	root.SetErr(buf)
	root.SetArgs([]string{"scan", "--help"})
	err := root.Execute()
	if err != nil {
		t.Fatalf("scan --help returned error: %v", err)
	}

	helpOutput := buf.String()

	// Verify all expected flags appear in the help output.
	expectedFlags := []string{
		"--search",
		"--collector",
		"--dry-run",
		"--cluster-timeout",
		"--output-dir",
		"--verbose",
		"--debug",
		"--ocm-url",
	}

	for _, flag := range expectedFlags {
		if !strings.Contains(helpOutput, flag) {
			t.Errorf("expected help output to contain %q, but it did not.\nHelp output:\n%s", flag, helpOutput)
		}
	}
}

// TestScanCommand_OCMURLFlag_RegisteredWithEmptyDefault verifies that the scan
// subcommand registers --ocm-url as a string flag with a default value of "".
// This ensures users can target staging/integration OCM endpoints via the flag.
func TestScanCommand_OCMURLFlag_RegisteredWithEmptyDefault(t *testing.T) {
	root := NewRootCommand("test")

	scanCmd := findSubcommand(root, "scan")
	if scanCmd == nil {
		t.Fatal("expected root command to have a 'scan' subcommand, but it was not found")
	}

	flag := scanCmd.Flags().Lookup("ocm-url")
	if flag == nil {
		t.Fatal("expected scan command to have --ocm-url flag, but it was not registered")
	}

	if flag.DefValue != "" {
		t.Errorf("expected --ocm-url default value to be %q, got %q", "", flag.DefValue)
	}
}

// findSubcommand searches a cobra command's subcommands by name.
func findSubcommand(parent *cobra.Command, name string) *cobra.Command {
	for _, cmd := range parent.Commands() {
		if cmd.Name() == name {
			return cmd
		}
	}
	return nil
}
