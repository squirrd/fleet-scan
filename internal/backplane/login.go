package backplane

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
)

// commandRunner is the function that executes external commands.
// It can be replaced in tests to avoid shelling out to real binaries.
var commandRunner = defaultCommandRunner

func defaultCommandRunner(name string, args []string, env []string) error {
	cmd := exec.Command(name, args...)
	cmd.Env = append(os.Environ(), env...)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	return cmd.Run()
}

// MakeBackplaneLoginFunc returns a closure that binds kubeconfigDir to Login,
// producing a function matching the runner.BackplaneLoginFunc signature:
//
//	func(clusterID string) (kubeconfigPath string, cleanup func(), err error)
func MakeBackplaneLoginFunc(kubeconfigDir string) func(clusterID string) (string, func(), error) {
	return func(clusterID string) (string, func(), error) {
		return Login(clusterID, kubeconfigDir)
	}
}

// Login shell-execs `ocm backplane login <clusterID>` with an isolated
// kubeconfig file in kubeconfigDir. It returns the path to the kubeconfig,
// a cleanup function that removes it, and any error.
//
// The cleanup function is safe to call even if the kubeconfig file does not
// exist (e.g., if the command did not create one).
func Login(clusterID, kubeconfigDir string) (string, func(), error) {
	if clusterID == "" {
		return "", nil, fmt.Errorf("cluster ID must not be empty")
	}

	kubeconfigPath := filepath.Join(kubeconfigDir, fmt.Sprintf("kubeconfig-%s", clusterID))

	env := []string{
		fmt.Sprintf("KUBECONFIG=%s", kubeconfigPath),
	}

	err := commandRunner("ocm", []string{"backplane", "login", clusterID}, env)
	if err != nil {
		return "", nil, fmt.Errorf("backplane login for cluster %s: %w", clusterID, err)
	}

	cleanup := func() {
		os.Remove(kubeconfigPath) //nolint:errcheck
	}

	return kubeconfigPath, cleanup, nil
}
