package backplane

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"testing"
)

func TestBackplaneLogin_BackplaneLoginFunc_Acceptance(t *testing.T) {
	t.Run("returns kubeconfig path and working cleanup", func(t *testing.T) {
		kubeconfigDir := t.TempDir()

		origRunner := commandRunner
		t.Cleanup(func() { commandRunner = origRunner })
		commandRunner = func(ctx context.Context, name string, args []string) error {
			// Simulate backplane creating <kube-path>/<clusterID>/config.
			for i, a := range args {
				if a == "--kube-path" && i+1 < len(args) {
					clusterID := args[2] // args: backplane login <clusterID> ...
					configDir := filepath.Join(args[i+1], clusterID)
					if err := os.MkdirAll(configDir, 0o700); err != nil {
						return err
					}
					return os.WriteFile(filepath.Join(configDir, "config"), []byte("fake-kubeconfig"), 0600)
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

		wantPath := filepath.Join(kubeconfigDir, "test-cluster-id", "config")
		if kubeconfigPath != wantPath {
			t.Errorf("kubeconfigPath = %q, want %q", kubeconfigPath, wantPath)
		}

		if _, err := os.Stat(kubeconfigPath); os.IsNotExist(err) {
			t.Fatalf("kubeconfig file does not exist at %s", kubeconfigPath)
		}

		cleanup()

		if _, err := os.Stat(filepath.Join(kubeconfigDir, "test-cluster-id")); !os.IsNotExist(err) {
			t.Fatalf("cleanup should have removed kubeconfig dir for cluster")
		}
	})
}

func TestLogin_CommandExecution(t *testing.T) {
	t.Run("invokes ocm backplane login with clusterID and --kube-path dir", func(t *testing.T) {
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

		_, cleanup, err := Login(context.Background(), "my-cluster-123", kubeconfigDir)
		if err != nil {
			t.Fatalf("Login returned error: %v", err)
		}
		defer cleanup()

		if capturedName != "ocm" {
			t.Errorf("command name = %q, want %q", capturedName, "ocm")
		}

		wantArgs := []string{"backplane", "login", "my-cluster-123", "--multi", "--kube-path", kubeconfigDir}
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
	t.Run("kubeconfig path is inside kubeconfigDir/<clusterID>/config", func(t *testing.T) {
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

		wantPath := filepath.Join(kubeconfigDir, "cluster-abc", "config")
		if kubeconfigPath != wantPath {
			t.Errorf("kubeconfigPath = %q, want %q", kubeconfigPath, wantPath)
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
	})
}

func TestLogin_CleanupRemovesFile(t *testing.T) {
	t.Run("cleanup removes the kubeconfig cluster dir", func(t *testing.T) {
		kubeconfigDir := t.TempDir()

		origRunner := commandRunner
		t.Cleanup(func() { commandRunner = origRunner })
		commandRunner = func(ctx context.Context, name string, args []string) error {
			clusterID := args[2]
			configDir := filepath.Join(kubeconfigDir, clusterID)
			if err := os.MkdirAll(configDir, 0o700); err != nil {
				return err
			}
			return os.WriteFile(filepath.Join(configDir, "config"), []byte("kubeconfig-data"), 0600)
		}

		kubeconfigPath, cleanup, err := Login(context.Background(), "test-cluster", kubeconfigDir)
		if err != nil {
			t.Fatalf("Login returned error: %v", err)
		}

		if _, err := os.Stat(kubeconfigPath); os.IsNotExist(err) {
			t.Fatalf("kubeconfig file should exist at %s", kubeconfigPath)
		}

		cleanup()

		clusterDir := filepath.Join(kubeconfigDir, "test-cluster")
		if _, err := os.Stat(clusterDir); !os.IsNotExist(err) {
			t.Fatalf("cleanup should have removed cluster dir at %s", clusterDir)
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
		_, cleanup, err := loginFunc(context.Background(), "cluster-xyz")
		if err != nil {
			t.Fatalf("loginFunc returned error: %v", err)
		}
		defer cleanup()

		// --kube-path should be the kubeconfigDir itself.
		wantArgs := []string{"backplane", "login", "cluster-xyz", "--multi", "--kube-path", kubeconfigDir}
		if len(capturedArgs) != len(wantArgs) {
			t.Fatalf("args = %v, want %v", capturedArgs, wantArgs)
		}
		for i, want := range wantArgs {
			if capturedArgs[i] != want {
				t.Errorf("args[%d] = %q, want %q", i, capturedArgs[i], want)
			}
		}
	})

	t.Run("different kubeconfigDirs produce independent functions", func(t *testing.T) {
		dir1 := t.TempDir()
		dir2 := t.TempDir()

		origRunner := commandRunner
		t.Cleanup(func() { commandRunner = origRunner })

		var lastKubePath string
		commandRunner = func(ctx context.Context, name string, args []string) error {
			for i, a := range args {
				if a == "--kube-path" && i+1 < len(args) {
					lastKubePath = args[i+1]
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
		path1 := lastKubePath

		_, cleanup2, err := fn2(context.Background(), "cluster-a")
		if err != nil {
			t.Fatalf("fn2 returned error: %v", err)
		}
		defer cleanup2()
		path2 := lastKubePath

		if path1 != dir1 {
			t.Errorf("fn1 --kube-path = %q, want %q", path1, dir1)
		}
		if path2 != dir2 {
			t.Errorf("fn2 --kube-path = %q, want %q", path2, dir2)
		}
	})
}
