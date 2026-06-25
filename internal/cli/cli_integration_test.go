package cli

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/squirrd/fleet-scan/internal/output"
)

// TestPhase2Output_CliWiring_Acceptance verifies that the scan subcommand:
// 1. Has a --resume flag
// 2. --resume and --search are mutually exclusive (error if combined)
// 3. --resume and --collector are mutually exclusive (error if combined)
// 4. Resume reads search from meta.json when --resume is used alone
func TestPhase2Output_CliWiring_Acceptance(t *testing.T) {
	t.Run("scan has resume flag", func(t *testing.T) {
		root := NewRootCommand()
		scanCmd := findSubcommand(root, "scan")
		if scanCmd == nil {
			t.Fatal("expected scan subcommand to exist")
		}

		resumeFlag := scanCmd.Flags().Lookup("resume")
		if resumeFlag == nil {
			t.Fatal("expected scan command to have --resume flag")
		}
	})

	t.Run("resume and search are mutually exclusive", func(t *testing.T) {
		root := NewRootCommand()
		buf := new(bytes.Buffer)
		root.SetOut(buf)
		root.SetErr(buf)
		root.SetArgs([]string{
			"scan",
			"--resume", "/some/path",
			"--search", "managed='true'",
			"--collector", "ns",
		})

		err := root.Execute()
		if err == nil {
			t.Fatal("expected error when --resume and --search are both specified")
		}

		errMsg := err.Error()
		if !strings.Contains(strings.ToLower(errMsg), "resume") {
			t.Errorf("error message should mention 'resume', got: %s", errMsg)
		}
	})

	t.Run("resume and collector are mutually exclusive", func(t *testing.T) {
		root := NewRootCommand()
		buf := new(bytes.Buffer)
		root.SetOut(buf)
		root.SetErr(buf)
		root.SetArgs([]string{
			"scan",
			"--resume", "/some/path",
			"--collector", "ns",
		})

		err := root.Execute()
		if err == nil {
			t.Fatal("expected error when --resume and --collector are both specified")
		}

		errMsg := err.Error()
		if !strings.Contains(strings.ToLower(errMsg), "resume") {
			t.Errorf("error message should mention 'resume', got: %s", errMsg)
		}
	})

	t.Run("resume reads search from meta.json", func(t *testing.T) {
		// Create a fake run directory with meta.json
		tmpDir := t.TempDir()
		runDir := filepath.Join(tmpDir, "2024-06-15T143000")
		if err := os.MkdirAll(runDir, 0755); err != nil {
			t.Fatalf("failed to create run dir: %v", err)
		}

		meta := output.RunMeta{
			Status:     "interrupted",
			Search:     "managed='true'",
			Collectors: []string{"managed-namespaces"},
		}
		metaBytes, _ := json.Marshal(meta)
		if err := os.WriteFile(filepath.Join(runDir, "meta.json"), metaBytes, 0644); err != nil {
			t.Fatalf("failed to write meta.json: %v", err)
		}

		// Also write an empty results.jsonl so resume can read it
		if err := os.WriteFile(filepath.Join(runDir, "results.jsonl"), []byte(""), 0644); err != nil {
			t.Fatalf("failed to write results.jsonl: %v", err)
		}

		// When only --resume is given (no --search, no --collector), the command
		// should read search and collectors from meta.json.
		// We can't fully test the OCM call here (no token), but we can verify
		// the flag is accepted and meta.json is parsed by checking that the error
		// is NOT about missing search/collector but rather about OCM auth.
		root := NewRootCommand()
		buf := new(bytes.Buffer)
		root.SetOut(buf)
		root.SetErr(buf)
		root.SetArgs([]string{
			"scan",
			"--resume", runDir,
		})

		err := root.Execute()
		// We expect it to fail eventually (OCM auth), but NOT because of
		// "missing --search" or "missing --collector" — those should come from meta.json.
		if err != nil {
			errMsg := err.Error()
			if strings.Contains(errMsg, "at least one --collector is required") {
				t.Errorf("resume should read collectors from meta.json, but got: %s", errMsg)
			}
			if strings.Contains(errMsg, "--search") && strings.Contains(errMsg, "required") {
				t.Errorf("resume should read search from meta.json, but got: %s", errMsg)
			}
		}
	})
}

// TestConcurrencySignals_ConcurrencyWiring_Acceptance verifies that:
// 1. The scan command has a --concurrency flag
// 2. The --concurrency flag defaults to 1 (backward-compatible serial execution)
// 3. The --concurrency flag value is accepted and wired to the dispatcher
// 4. Invalid --concurrency values (0, negative) are rejected with an error
// 5. The scan command uses dispatcher.Dispatch() instead of direct runner.Run()
//
// Acceptance criterion: CLI wires --concurrency flag through to the dispatcher,
// replacing the direct runner.Run() call with dispatcher.Dispatch(), and
// progress reporting works correctly under concurrency.
//
// Phase: RED — --concurrency flag does not exist yet.
func TestConcurrencySignals_ConcurrencyWiring_Acceptance(t *testing.T) {
	t.Run("scan has concurrency flag with default 1", func(t *testing.T) {
		root := NewRootCommand()
		scanCmd := findSubcommand(root, "scan")
		if scanCmd == nil {
			t.Fatal("expected scan subcommand to exist")
		}

		concFlag := scanCmd.Flags().Lookup("concurrency")
		if concFlag == nil {
			t.Fatal("expected scan command to have --concurrency flag")
		}

		// Default value should be "1" (serial execution).
		if concFlag.DefValue != "1" {
			t.Errorf("--concurrency default = %q, want %q", concFlag.DefValue, "1")
		}
	})

	t.Run("invalid concurrency value is rejected with validation error", func(t *testing.T) {
		root := NewRootCommand()
		scanCmd := findSubcommand(root, "scan")
		if scanCmd == nil {
			t.Fatal("expected scan subcommand to exist")
		}

		concFlag := scanCmd.Flags().Lookup("concurrency")
		if concFlag == nil {
			t.Fatal("--concurrency flag must exist before testing validation")
		}

		buf := new(bytes.Buffer)
		root.SetOut(buf)
		root.SetErr(buf)
		root.SetArgs([]string{
			"scan",
			"--concurrency", "0",
			"--search", "managed='true'",
			"--collector", "test",
		})

		err := root.Execute()
		if err == nil {
			t.Fatal("expected error for --concurrency=0")
		}

		// The error must be a validation error about concurrency being too low,
		// not an unrelated error (e.g. unknown collector, OCM auth).
		errMsg := err.Error()
		if !strings.Contains(errMsg, "concurrency") || !strings.Contains(errMsg, "must be") {
			t.Errorf("error should be a validation error about concurrency value, got: %s", errMsg)
		}
	})

	t.Run("negative concurrency value is rejected with validation error", func(t *testing.T) {
		root := NewRootCommand()
		scanCmd := findSubcommand(root, "scan")
		if scanCmd == nil {
			t.Fatal("expected scan subcommand to exist")
		}

		concFlag := scanCmd.Flags().Lookup("concurrency")
		if concFlag == nil {
			t.Fatal("--concurrency flag must exist before testing validation")
		}

		buf := new(bytes.Buffer)
		root.SetOut(buf)
		root.SetErr(buf)
		root.SetArgs([]string{
			"scan",
			"--concurrency", "-1",
			"--search", "managed='true'",
			"--collector", "test",
		})

		err := root.Execute()
		if err == nil {
			t.Fatal("expected error for --concurrency=-1")
		}

		errMsg := err.Error()
		if !strings.Contains(errMsg, "concurrency") || !strings.Contains(errMsg, "must be") {
			t.Errorf("error should be a validation error about concurrency value, got: %s", errMsg)
		}
	})
}
