package ocm

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
)

func ResolveToken() (string, error) {
	home := os.Getenv("HOME")
	return ResolveTokenWithConfigDirs([]string{
		filepath.Join(home, ".config", "ocm"),
		filepath.Join(home, "Library", "Application Support", "ocm"),
	})
}

// ResolveTokenWithConfigDir checks a single config directory for an OCM token.
// Kept for backwards compatibility; prefer ResolveTokenWithConfigDirs.
func ResolveTokenWithConfigDir(configDir string) (string, error) {
	return ResolveTokenWithConfigDirs([]string{configDir})
}

// ResolveTokenWithConfigDirs resolves an OCM token by checking, in order:
//  1. OCM_TOKEN environment variable
//  2. Each config directory for a valid ocm.json containing a refresh_token
//
// Returns an error only if none of the sources yield a token.
func ResolveTokenWithConfigDirs(configDirs []string) (string, error) {
	if token := os.Getenv("OCM_TOKEN"); token != "" {
		return token, nil
	}

	var lastErr error
	for _, dir := range configDirs {
		configPath := filepath.Join(dir, "ocm.json")
		cfg, err := ParseOCMConfig(configPath)
		if err != nil {
			lastErr = err
			continue
		}
		return cfg.RefreshToken, nil
	}

	if lastErr != nil {
		return "", fmt.Errorf("no OCM token found: set OCM_TOKEN or run `ocm login`: %w", lastErr)
	}
	return "", fmt.Errorf("no OCM token found: set OCM_TOKEN or run `ocm login`: no config directories to search")
}

type OCMConfig struct {
	RefreshToken string `json:"refresh_token"`
}

func ParseOCMConfig(path string) (*OCMConfig, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("failed to read OCM config: %w", err)
	}

	var cfg OCMConfig
	if err := json.Unmarshal(data, &cfg); err != nil {
		return nil, fmt.Errorf("failed to parse OCM config: %w", err)
	}

	if cfg.RefreshToken == "" {
		return nil, fmt.Errorf("OCM config missing refresh_token")
	}

	return &cfg, nil
}
