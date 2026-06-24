package backplane

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestBackplaneLogin_BackplaneLoginFunc_Acceptance verifies that Login():
// 1. Takes a clusterID and a kubeconfig directory path
// 2. Returns (kubeconfigPath, cleanup, error)
// 3. The kubeconfigPath is an isolated file inside the provided directory
// 4. The cleanup function removes the kubeconfig file
//
// Acceptance criterion: Login() shell-execs `ocm backplane login` with an
// isolated kubeconfig path and returns (kubeconfigPath, cleanup, error);
// cleanup removes the temp file.
//
// Phase: RED — the Login function does not exist yet.
func TestBackplaneLogin_BackplaneLoginFunc_Acceptance(t *testing.T) {
	t.Run("returns kubeconfig path and working cleanup", func(t *testing.T) {
		kubeconfigDir := t.TempDir()

		// Inject a fake command runner to avoid needing a real backplane.
		// The fake simulates `ocm backplane login` by creating the kubeconfig
		// file at the path specified by the KUBECONFIG env var.
		origRunner := commandRunner
		t.Cleanup(func() { commandRunner = origRunner })
		commandRunner = func(name string, args []string, env []string) error {
			for _, e := range env {
				if strings.HasPrefix(e, "KUBECONFIG=") {
					path := strings.TrimPrefix(e, "KUBECONFIG=")
					return os.WriteFile(path, []byte("fake-kubeconfig"), 0600)
				}
			}
			return nil
		}

		kubeconfigPath, cleanup, err := Login("test-cluster-id", kubeconfigDir)
		if err != nil {
			t.Fatalf("Login returned error: %v", err)
		}

		// The kubeconfig path should be inside the provided directory.
		if kubeconfigPath == "" {
			t.Fatal("expected non-empty kubeconfig path")
		}

		// The kubeconfig file should exist after Login returns.
		if _, err := os.Stat(kubeconfigPath); os.IsNotExist(err) {
			t.Fatalf("kubeconfig file does not exist at %s", kubeconfigPath)
		}

		// Call cleanup — it should remove the kubeconfig file.
		cleanup()

		if _, err := os.Stat(kubeconfigPath); !os.IsNotExist(err) {
			t.Fatalf("cleanup should have removed kubeconfig at %s, but it still exists", kubeconfigPath)
		}
	})
}

// backwards_compatibility: tests public API contract
//
// TestLogin_CommandExecution tests that Login invokes the command runner with
// the correct arguments and KUBECONFIG environment variable.
func TestLogin_CommandExecution(t *testing.T) {
	t.Run("invokes ocm backplane login with clusterID and KUBECONFIG env", func(t *testing.T) {
		kubeconfigDir := t.TempDir()

		var capturedName string
		var capturedArgs []string
		var capturedEnv []string

		origRunner := commandRunner
		t.Cleanup(func() { commandRunner = origRunner })

		commandRunner = func(name string, args []string, env []string) error {
			capturedName = name
			capturedArgs = args
			capturedEnv = env
			return nil
		}

		kubeconfigPath, cleanup, err := Login("my-cluster-123", kubeconfigDir)
		if err != nil {
			t.Fatalf("Login returned error: %v", err)
		}
		defer cleanup()

		// Should invoke "ocm" with "backplane", "login", "<clusterID>"
		if capturedName != "ocm" {
			t.Errorf("command name = %q, want %q", capturedName, "ocm")
		}

		wantArgs := []string{"backplane", "login", "my-cluster-123"}
		if len(capturedArgs) != len(wantArgs) {
			t.Fatalf("args = %v, want %v", capturedArgs, wantArgs)
		}
		for i, want := range wantArgs {
			if capturedArgs[i] != want {
				t.Errorf("args[%d] = %q, want %q", i, capturedArgs[i], want)
			}
		}

		// KUBECONFIG env var should be set to a path inside kubeconfigDir
		foundKubeconfig := false
		for _, e := range capturedEnv {
			if strings.HasPrefix(e, "KUBECONFIG=") {
				foundKubeconfig = true
				val := strings.TrimPrefix(e, "KUBECONFIG=")
				if val != kubeconfigPath {
					t.Errorf("KUBECONFIG env = %q, want %q", val, kubeconfigPath)
				}
			}
		}
		if !foundKubeconfig {
			t.Error("KUBECONFIG env var not found in command environment")
		}
	})
}

// backwards_compatibility: tests public API contract
//
// TestLogin_KubeconfigPathIsolation tests that the kubeconfig path returned by
// Login is inside the provided kubeconfigDir and contains the cluster ID.
func TestLogin_KubeconfigPathIsolation(t *testing.T) {
	t.Run("kubeconfig path is inside kubeconfigDir and contains cluster ID", func(t *testing.T) {
		kubeconfigDir := t.TempDir()

		origRunner := commandRunner
		t.Cleanup(func() { commandRunner = origRunner })
		commandRunner = func(name string, args []string, env []string) error {
			return nil
		}

		kubeconfigPath, cleanup, err := Login("cluster-abc", kubeconfigDir)
		if err != nil {
			t.Fatalf("Login returned error: %v", err)
		}
		defer cleanup()

		// Path must be inside kubeconfigDir.
		dir := filepath.Dir(kubeconfigPath)
		if dir != kubeconfigDir {
			t.Errorf("kubeconfig dir = %q, want %q", dir, kubeconfigDir)
		}

		// Path should contain the cluster ID for debuggability.
		base := filepath.Base(kubeconfigPath)
		if !strings.Contains(base, "cluster-abc") {
			t.Errorf("kubeconfig filename %q should contain cluster ID %q", base, "cluster-abc")
		}
	})
}

// backwards_compatibility: tests public API contract
//
// TestLogin_CommandFailure tests that Login returns an error when the command fails.
func TestLogin_CommandFailure(t *testing.T) {
	t.Run("returns error when command fails", func(t *testing.T) {
		kubeconfigDir := t.TempDir()

		origRunner := commandRunner
		t.Cleanup(func() { commandRunner = origRunner })
		commandRunner = func(name string, args []string, env []string) error {
			return fmt.Errorf("exit status 1: unable to login")
		}

		kubeconfigPath, cleanup, err := Login("bad-cluster", kubeconfigDir)
		if err == nil {
			t.Fatal("expected error when command fails, got nil")
		}

		// Should mention the cluster ID in the error.
		if !strings.Contains(err.Error(), "bad-cluster") {
			t.Errorf("error %q should contain cluster ID %q", err.Error(), "bad-cluster")
		}

		// kubeconfigPath should be empty on failure.
		if kubeconfigPath != "" {
			t.Errorf("kubeconfigPath = %q, want empty on failure", kubeconfigPath)
		}

		// cleanup should be nil on failure.
		if cleanup != nil {
			t.Error("cleanup should be nil on failure")
		}
	})
}

// backwards_compatibility: tests public API contract
//
// TestLogin_EmptyClusterID tests that Login returns an error for empty cluster ID.
func TestLogin_EmptyClusterID(t *testing.T) {
	t.Run("returns error for empty cluster ID", func(t *testing.T) {
		kubeconfigDir := t.TempDir()

		_, _, err := Login("", kubeconfigDir)
		if err == nil {
			t.Fatal("expected error for empty cluster ID, got nil")
		}

		if !strings.Contains(err.Error(), "cluster") {
			t.Errorf("error %q should mention cluster", err.Error())
		}
	})
}

// backwards_compatibility: tests public API contract
//
// TestLogin_CleanupRemovesFile tests that the cleanup function removes the
// kubeconfig file even if the file was created by the command runner.
func TestLogin_CleanupRemovesFile(t *testing.T) {
	t.Run("cleanup removes the kubeconfig file", func(t *testing.T) {
		kubeconfigDir := t.TempDir()

		origRunner := commandRunner
		t.Cleanup(func() { commandRunner = origRunner })
		commandRunner = func(name string, args []string, env []string) error {
			// Simulate backplane creating the kubeconfig file.
			for _, e := range env {
				if strings.HasPrefix(e, "KUBECONFIG=") {
					path := strings.TrimPrefix(e, "KUBECONFIG=")
					return os.WriteFile(path, []byte("kubeconfig-data"), 0600)
				}
			}
			return nil
		}

		kubeconfigPath, cleanup, err := Login("test-cluster", kubeconfigDir)
		if err != nil {
			t.Fatalf("Login returned error: %v", err)
		}

		// Verify file exists.
		if _, err := os.Stat(kubeconfigPath); os.IsNotExist(err) {
			t.Fatalf("kubeconfig file should exist at %s", kubeconfigPath)
		}

		// Run cleanup.
		cleanup()

		// File should be gone.
		if _, err := os.Stat(kubeconfigPath); !os.IsNotExist(err) {
			t.Fatalf("kubeconfig file should be removed after cleanup, path: %s", kubeconfigPath)
		}
	})

	t.Run("cleanup is safe to call when file does not exist", func(t *testing.T) {
		kubeconfigDir := t.TempDir()

		origRunner := commandRunner
		t.Cleanup(func() { commandRunner = origRunner })
		commandRunner = func(name string, args []string, env []string) error {
			return nil
		}

		_, cleanup, err := Login("test-cluster", kubeconfigDir)
		if err != nil {
			t.Fatalf("Login returned error: %v", err)
		}

		// cleanup should not panic even if file doesn't exist.
		cleanup()
	})
}
