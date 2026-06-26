package backplane

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
)

var commandRunner = defaultCommandRunner

func defaultCommandRunner(ctx context.Context, name string, args []string) error {
	cmd := exec.CommandContext(ctx, name, args...)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	return cmd.Run()
}

// MakeBackplaneLoginFunc returns a closure that binds kubeconfigDir to Login,
// matching the runner.BackplaneLoginFunc signature.
func MakeBackplaneLoginFunc(kubeconfigDir string) func(ctx context.Context, clusterID string) (string, func(), error) {
	return func(ctx context.Context, clusterID string) (string, func(), error) {
		return Login(ctx, clusterID, kubeconfigDir)
	}
}

// Login shell-execs `ocm backplane login <clusterID> --kube-path <path>` with
// an isolated kubeconfig file in kubeconfigDir. It returns the path to the
// kubeconfig, a cleanup function that removes it, and any error.
func Login(ctx context.Context, clusterID, kubeconfigDir string) (string, func(), error) {
	if clusterID == "" {
		return "", nil, fmt.Errorf("cluster ID must not be empty")
	}

	if err := os.MkdirAll(kubeconfigDir, 0o700); err != nil {
		return "", nil, fmt.Errorf("creating kubeconfig directory: %w", err)
	}

	// Backplane --kube-path is a directory; it writes <kube-path>/<clusterID>/config.
	kubeconfigPath := filepath.Join(kubeconfigDir, clusterID, "config")

	err := commandRunner(ctx, "ocm", []string{"backplane", "login", clusterID, "--multi", "--kube-path", kubeconfigDir})
	if err != nil {
		return "", nil, fmt.Errorf("backplane login for cluster %s: %w", clusterID, err)
	}

	cleanup := func() {
		os.RemoveAll(filepath.Join(kubeconfigDir, clusterID)) //nolint:errcheck
	}

	return kubeconfigPath, cleanup, nil
}
