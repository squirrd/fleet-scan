package backplane

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestBackplaneLogin_BackplaneLoginFunc_Acceptance(t *testing.T) {
	t.Run("returns kubeconfig path and working cleanup", func(t *testing.T) {
		kubeconfigDir := t.TempDir()

		origRunner := commandRunner
		t.Cleanup(func() { commandRunner = origRunner })
		commandRunner = func(ctx context.Context, name string, args []string) error {
			// Simulate backplane creating the kubeconfig at the --kube-path location.
			for i, a := range args {
				if a == "--kube-path" && i+1 < len(args) {
					return os.WriteFile(args[i+1], []byte("fake-kubeconfig"), 0600)
				}
			}
			return nil
		}

		kubeconfigPath, cleanup, err := Login(context.Background(), "test-cluster-id", kubeconfigDir)
		if err != nil {
			t.Fatalf("Login returned error: %v", err)
		}

		if kubeconfigPath == "" {
			t.Fatal("expected non-empty kubeconfig path")
		}

		if _, err := os.Stat(kubeconfigPath); os.IsNotExist(err) {
			t.Fatalf("kubeconfig file does not exist at %s", kubeconfigPath)
		}

		cleanup()

		if _, err := os.Stat(kubeconfigPath); !os.IsNotExist(err) {
			t.Fatalf("cleanup should have removed kubeconfig at %s, but it still exists", kubeconfigPath)
		}
	})
}

func TestLogin_CommandExecution(t *testing.T) {
	t.Run("invokes ocm backplane login with clusterID and --kube-path", func(t *testing.T) {
		kubeconfigDir := t.TempDir()

		var capturedName string
		var capturedArgs []string

		origRunner := commandRunner
		t.Cleanup(func() { commandRunner = origRunner })

		commandRunner = func(ctx context.Context, name string, args []string) error {
			capturedName = name
			capturedArgs = args
			return nil
		}

		kubeconfigPath, cleanup, err := Login(context.Background(), "my-cluster-123", kubeconfigDir)
		if err != nil {
			t.Fatalf("Login returned error: %v", err)
		}
		defer cleanup()

		if capturedName != "ocm" {
			t.Errorf("command name = %q, want %q", capturedName, "ocm")
		}

		wantArgs := []string{"backplane", "login", "my-cluster-123", "--kube-path", kubeconfigPath}
		if len(capturedArgs) != len(wantArgs) {
			t.Fatalf("args = %v, want %v", capturedArgs, wantArgs)
		}
		for i, want := range wantArgs {
			if capturedArgs[i] != want {
				t.Errorf("args[%d] = %q, want %q", i, capturedArgs[i], want)
			}
		}
	})
}

func TestLogin_KubeconfigPathIsolation(t *testing.T) {
	t.Run("kubeconfig path is inside kubeconfigDir and contains cluster ID", func(t *testing.T) {
		kubeconfigDir := t.TempDir()

		origRunner := commandRunner
		t.Cleanup(func() { commandRunner = origRunner })
		commandRunner = func(ctx context.Context, name string, args []string) error {
			return nil
		}

		kubeconfigPath, cleanup, err := Login(context.Background(), "cluster-abc", kubeconfigDir)
		if err != nil {
			t.Fatalf("Login returned error: %v", err)
		}
		defer cleanup()

		dir := filepath.Dir(kubeconfigPath)
		if dir != kubeconfigDir {
			t.Errorf("kubeconfig dir = %q, want %q", dir, kubeconfigDir)
		}

		base := filepath.Base(kubeconfigPath)
		if !strings.Contains(base, "cluster-abc") {
			t.Errorf("kubeconfig filename %q should contain cluster ID %q", base, "cluster-abc")
		}
	})
}

func TestLogin_CommandFailure(t *testing.T) {
	t.Run("returns error when command fails", func(t *testing.T) {
		kubeconfigDir := t.TempDir()

		origRunner := commandRunner
		t.Cleanup(func() { commandRunner = origRunner })
		commandRunner = func(ctx context.Context, name string, args []string) error {
			return fmt.Errorf("exit status 1: unable to login")
		}

		kubeconfigPath, cleanup, err := Login(context.Background(), "bad-cluster", kubeconfigDir)
		if err == nil {
			t.Fatal("expected error when command fails, got nil")
		}

		if !strings.Contains(err.Error(), "bad-cluster") {
			t.Errorf("error %q should contain cluster ID %q", err.Error(), "bad-cluster")
		}

		if kubeconfigPath != "" {
			t.Errorf("kubeconfigPath = %q, want empty on failure", kubeconfigPath)
		}

		if cleanup != nil {
			t.Error("cleanup should be nil on failure")
		}
	})
}

func TestLogin_EmptyClusterID(t *testing.T) {
	t.Run("returns error for empty cluster ID", func(t *testing.T) {
		kubeconfigDir := t.TempDir()

		_, _, err := Login(context.Background(), "", kubeconfigDir)
		if err == nil {
			t.Fatal("expected error for empty cluster ID, got nil")
		}

		if !strings.Contains(err.Error(), "cluster") {
			t.Errorf("error %q should mention cluster", err.Error())
		}
	})
}

func TestLogin_CleanupRemovesFile(t *testing.T) {
	t.Run("cleanup removes the kubeconfig file", func(t *testing.T) {
		kubeconfigDir := t.TempDir()

		origRunner := commandRunner
		t.Cleanup(func() { commandRunner = origRunner })
		commandRunner = func(ctx context.Context, name string, args []string) error {
			for i, a := range args {
				if a == "--kube-path" && i+1 < len(args) {
					return os.WriteFile(args[i+1], []byte("kubeconfig-data"), 0600)
				}
			}
			return nil
		}

		kubeconfigPath, cleanup, err := Login(context.Background(), "test-cluster", kubeconfigDir)
		if err != nil {
			t.Fatalf("Login returned error: %v", err)
		}

		if _, err := os.Stat(kubeconfigPath); os.IsNotExist(err) {
			t.Fatalf("kubeconfig file should exist at %s", kubeconfigPath)
		}

		cleanup()

		if _, err := os.Stat(kubeconfigPath); !os.IsNotExist(err) {
			t.Fatalf("kubeconfig file should be removed after cleanup, path: %s", kubeconfigPath)
		}
	})

	t.Run("cleanup is safe to call when file does not exist", func(t *testing.T) {
		kubeconfigDir := t.TempDir()

		origRunner := commandRunner
		t.Cleanup(func() { commandRunner = origRunner })
		commandRunner = func(ctx context.Context, name string, args []string) error {
			return nil
		}

		_, cleanup, err := Login(context.Background(), "test-cluster", kubeconfigDir)
		if err != nil {
			t.Fatalf("Login returned error: %v", err)
		}

		cleanup()
	})
}

func TestMakeBackplaneLoginFunc(t *testing.T) {
	t.Run("returns a non-nil function", func(t *testing.T) {
		kubeconfigDir := t.TempDir()

		loginFunc := MakeBackplaneLoginFunc(kubeconfigDir)
		if loginFunc == nil {
			t.Fatal("expected MakeBackplaneLoginFunc to return a non-nil function")
		}
	})

	t.Run("returned function delegates to Login with bound kubeconfigDir", func(t *testing.T) {
		kubeconfigDir := t.TempDir()

		var capturedArgs []string

		origRunner := commandRunner
		t.Cleanup(func() { commandRunner = origRunner })
		commandRunner = func(ctx context.Context, name string, args []string) error {
			capturedArgs = args
			return nil
		}

		loginFunc := MakeBackplaneLoginFunc(kubeconfigDir)
		kubeconfigPath, cleanup, err := loginFunc(context.Background(), "cluster-xyz")
		if err != nil {
			t.Fatalf("loginFunc returned error: %v", err)
		}
		defer cleanup()

		// Verify --kube-path points into the bound kubeconfigDir.
		foundKubePath := false
		for i, a := range capturedArgs {
			if a == "--kube-path" && i+1 < len(capturedArgs) {
				foundKubePath = true
				val := capturedArgs[i+1]
				dir := filepath.Dir(val)
				if dir != kubeconfigDir {
					t.Errorf("--kube-path dir = %q, want %q", dir, kubeconfigDir)
				}
				if val != kubeconfigPath {
					t.Errorf("--kube-path = %q, want kubeconfigPath %q", val, kubeconfigPath)
				}
			}
		}
		if !foundKubePath {
			t.Error("--kube-path not found in command args")
		}

		// Verify clusterID was passed through.
		wantArgs := []string{"backplane", "login", "cluster-xyz", "--kube-path", kubeconfigPath}
		if len(capturedArgs) != len(wantArgs) {
			t.Fatalf("args = %v, want %v", capturedArgs, wantArgs)
		}
	})

	t.Run("different kubeconfigDirs produce independent functions", func(t *testing.T) {
		dir1 := t.TempDir()
		dir2 := t.TempDir()

		origRunner := commandRunner
		t.Cleanup(func() { commandRunner = origRunner })

		var lastKubeconfigPath string
		commandRunner = func(ctx context.Context, name string, args []string) error {
			for i, a := range args {
				if a == "--kube-path" && i+1 < len(args) {
					lastKubeconfigPath = args[i+1]
				}
			}
			return nil
		}

		fn1 := MakeBackplaneLoginFunc(dir1)
		fn2 := MakeBackplaneLoginFunc(dir2)

		_, cleanup1, err := fn1(context.Background(), "cluster-a")
		if err != nil {
			t.Fatalf("fn1 returned error: %v", err)
		}
		defer cleanup1()
		path1 := lastKubeconfigPath

		_, cleanup2, err := fn2(context.Background(), "cluster-a")
		if err != nil {
			t.Fatalf("fn2 returned error: %v", err)
		}
		defer cleanup2()
		path2 := lastKubeconfigPath

		if filepath.Dir(path1) != dir1 {
			t.Errorf("fn1 kubeconfig dir = %q, want %q", filepath.Dir(path1), dir1)
		}
		if filepath.Dir(path2) != dir2 {
			t.Errorf("fn2 kubeconfig dir = %q, want %q", filepath.Dir(path2), dir2)
		}
		if path1 == path2 {
			t.Errorf("expected different kubeconfig paths for different dirs, got same: %q", path1)
		}
	})
}
