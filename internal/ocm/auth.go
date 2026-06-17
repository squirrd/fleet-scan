package ocm

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
)

func ResolveToken() (string, error) {
	return ResolveTokenWithConfigDir(filepath.Join(os.Getenv("HOME"), ".config", "ocm"))
}

func ResolveTokenWithConfigDir(configDir string) (string, error) {
	if token := os.Getenv("OCM_TOKEN"); token != "" {
		return token, nil
	}

	configPath := filepath.Join(configDir, "ocm.json")
	cfg, err := ParseOCMConfig(configPath)
	if err != nil {
		return "", fmt.Errorf("no OCM token found: set OCM_TOKEN or run `ocm login`: %w", err)
	}
	return cfg.RefreshToken, nil
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
