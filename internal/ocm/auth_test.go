package ocm

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestTokenResolution verifies the OCM token resolution priority:
// 1. OCM_TOKEN env var (if set)
// 2. refresh_token from ~/.config/ocm/ocm.json (if available)
// 3. Clear error if neither available
func TestTokenResolution(t *testing.T) {
	tests := []struct {
		name        string
		envToken    string   // value for OCM_TOKEN (empty string = unset)
		configJSON  string   // content for fake ocm.json (empty = no file)
		wantToken   string
		wantErr     bool
		errContains string
	}{
		{
			name:      "OCM_TOKEN env var set returns it",
			envToken:  "env-token-abc123",
			wantToken: "env-token-abc123",
			wantErr:   false,
		},
		{
			name:     "OCM_TOKEN empty reads from config file",
			envToken: "",
			configJSON: `{
				"refresh_token": "config-refresh-token-xyz"
			}`,
			wantToken: "config-refresh-token-xyz",
			wantErr:   false,
		},
		{
			name:        "neither available returns clear error",
			envToken:    "",
			configJSON:  "",
			wantErr:     true,
			errContains: "token",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Save and restore OCM_TOKEN.
			origToken := os.Getenv("OCM_TOKEN")
			defer os.Setenv("OCM_TOKEN", origToken)

			if tt.envToken != "" {
				os.Setenv("OCM_TOKEN", tt.envToken)
			} else {
				os.Unsetenv("OCM_TOKEN")
			}

			// Set up a fake config directory if config content provided.
			var configDir string
			if tt.configJSON != "" {
				tmpDir := t.TempDir()
				configDir = tmpDir
				configPath := filepath.Join(tmpDir, "ocm.json")
				if err := os.WriteFile(configPath, []byte(tt.configJSON), 0644); err != nil {
					t.Fatalf("failed to write test config: %v", err)
				}
			} else {
				// Point to a non-existent directory so no config is found.
				configDir = filepath.Join(t.TempDir(), "nonexistent")
			}

			got, err := ResolveTokenWithConfigDir(configDir)

			if tt.wantErr {
				if err == nil {
					t.Fatal("expected error, got nil")
				}
				if tt.errContains != "" && !strings.Contains(err.Error(), tt.errContains) {
					t.Errorf("expected error containing %q, got: %v", tt.errContains, err)
				}
				return
			}

			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}

			if got != tt.wantToken {
				t.Errorf("got token %q, want %q", got, tt.wantToken)
			}
		})
	}
}

// TestParseOCMConfig verifies parsing of OCM config files.
func TestParseOCMConfig(t *testing.T) {
	tests := []struct {
		name        string
		content     string
		wantToken   string
		wantErr     bool
		errContains string
	}{
		{
			name:      "valid config with refresh_token",
			content:   `{"refresh_token": "my-refresh-token", "access_token": "ignored"}`,
			wantToken: "my-refresh-token",
		},
		{
			name:        "missing refresh_token field",
			content:     `{"access_token": "only-access"}`,
			wantErr:     true,
			errContains: "refresh_token",
		},
		{
			name:        "malformed JSON",
			content:     `{not valid json`,
			wantErr:     true,
			errContains: "parse",
		},
		{
			name:        "empty refresh_token",
			content:     `{"refresh_token": ""}`,
			wantErr:     true,
			errContains: "refresh_token",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tmpDir := t.TempDir()
			path := filepath.Join(tmpDir, "ocm.json")
			if err := os.WriteFile(path, []byte(tt.content), 0644); err != nil {
				t.Fatalf("failed to write test config: %v", err)
			}

			config, err := ParseOCMConfig(path)

			if tt.wantErr {
				if err == nil {
					t.Fatal("expected error, got nil")
				}
				if tt.errContains != "" && !strings.Contains(err.Error(), tt.errContains) {
					t.Errorf("expected error containing %q, got: %v", tt.errContains, err)
				}
				return
			}

			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}

			if config == nil {
				t.Fatal("expected non-nil config, got nil")
			}

			if config.RefreshToken != tt.wantToken {
				t.Errorf("RefreshToken = %q, want %q", config.RefreshToken, tt.wantToken)
			}
		})
	}
}
