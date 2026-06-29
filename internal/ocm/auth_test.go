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

// TestResolveTokenMultiDirFallback verifies that ResolveTokenWithConfigDirs
// tries multiple config directories in order and returns the token from the
// first directory that contains a valid ocm.json.
func TestResolveTokenMultiDirFallback(t *testing.T) {
	// Save and restore OCM_TOKEN.
	origToken := os.Getenv("OCM_TOKEN")
	defer os.Setenv("OCM_TOKEN", origToken)
	os.Unsetenv("OCM_TOKEN")

	tests := []struct {
		name      string
		// setupDirs returns config directories; each entry is (dirPath, configJSON).
		// If configJSON is empty, no file is created in that dir.
		setupDirs func(t *testing.T) []string
		wantToken string
		wantErr   bool
	}{
		{
			name: "first dir has config — returns its token",
			setupDirs: func(t *testing.T) []string {
				dir1 := t.TempDir()
				os.WriteFile(filepath.Join(dir1, "ocm.json"), []byte(`{"refresh_token":"first-token"}`), 0644)
				dir2 := t.TempDir() // second dir exists but has no config
				return []string{dir1, dir2}
			},
			wantToken: "first-token",
		},
		{
			name: "first dir empty, second has config — returns second",
			setupDirs: func(t *testing.T) []string {
				dir1 := filepath.Join(t.TempDir(), "nonexistent")
				dir2 := t.TempDir()
				os.WriteFile(filepath.Join(dir2, "ocm.json"), []byte(`{"refresh_token":"second-token"}`), 0644)
				return []string{dir1, dir2}
			},
			wantToken: "second-token",
		},
		{
			name: "no dirs have config — returns error",
			setupDirs: func(t *testing.T) []string {
				return []string{
					filepath.Join(t.TempDir(), "nonexistent1"),
					filepath.Join(t.TempDir(), "nonexistent2"),
				}
			},
			wantErr: true,
		},
		{
			name: "single dir with config — works like single-dir case",
			setupDirs: func(t *testing.T) []string {
				dir := t.TempDir()
				os.WriteFile(filepath.Join(dir, "ocm.json"), []byte(`{"refresh_token":"only-token"}`), 0644)
				return []string{dir}
			},
			wantToken: "only-token",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			dirs := tt.setupDirs(t)
			got, err := ResolveTokenWithConfigDirs(dirs)

			if tt.wantErr {
				if err == nil {
					t.Fatal("expected error, got nil")
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

// backwards_compatibility: tests public API contract

// TestMC111ResolveAuthWithConfigDirs_ClientID verifies that ResolveAuthWithConfigDirs
// returns both the refresh token AND the client_id from the OCM config file.
//
// Bug: MC-111
// Reproduced: NewSDKClient() omits .Client(clientID, "") when building the SDK
// connection because the auth layer only returns a token, not a client ID.
// The OCM CLI issues refresh tokens bound to client_id "ocm-cli", but the SDK
// defaults to "cloud-services" when no client ID is set.
// Expected: ResolveAuthWithConfigDirs returns (token, clientID, error) so that
// callers can forward the client ID to NewSDKClient and the SDK connection
// builder sets .Client(clientID, "").
// Actual: No ResolveAuth function exists; only ResolveToken which discards
// the client_id field from ocm.json.
func TestMC111ResolveAuthWithConfigDirs_ClientID(t *testing.T) {
	// Save and restore OCM_TOKEN.
	origToken := os.Getenv("OCM_TOKEN")
	defer os.Setenv("OCM_TOKEN", origToken)
	os.Unsetenv("OCM_TOKEN")

	tests := []struct {
		name         string
		configJSON   string
		wantToken    string
		wantClientID string
		wantErr      bool
		errContains  string
	}{
		{
			name:         "config with client_id returns both token and client ID",
			configJSON:   `{"refresh_token": "my-refresh-token", "client_id": "ocm-cli"}`,
			wantToken:    "my-refresh-token",
			wantClientID: "ocm-cli",
		},
		{
			name:         "config without client_id returns empty client ID",
			configJSON:   `{"refresh_token": "my-refresh-token"}`,
			wantToken:    "my-refresh-token",
			wantClientID: "",
		},
		{
			name:         "config with empty client_id returns empty string",
			configJSON:   `{"refresh_token": "my-refresh-token", "client_id": ""}`,
			wantToken:    "my-refresh-token",
			wantClientID: "",
		},
		{
			name:         "config with custom client_id preserves it",
			configJSON:   `{"refresh_token": "tok-123", "client_id": "my-custom-client"}`,
			wantToken:    "tok-123",
			wantClientID: "my-custom-client",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			dir := t.TempDir()
			if err := os.WriteFile(filepath.Join(dir, "ocm.json"), []byte(tt.configJSON), 0644); err != nil {
				t.Fatalf("failed to write test config: %v", err)
			}

			gotToken, gotClientID, err := ResolveAuthWithConfigDirs([]string{dir})

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
			if gotToken != tt.wantToken {
				t.Errorf("token = %q, want %q", gotToken, tt.wantToken)
			}
			if gotClientID != tt.wantClientID {
				t.Errorf("clientID = %q, want %q", gotClientID, tt.wantClientID)
			}
		})
	}
}

// TestMC111ResolveAuth_EnvVarReturnsEmptyClientID verifies that when OCM_TOKEN
// env var is set, ResolveAuth returns the token with an empty client ID
// (environment tokens have no associated client identity).
func TestMC111ResolveAuth_EnvVarReturnsEmptyClientID(t *testing.T) {
	origToken := os.Getenv("OCM_TOKEN")
	defer os.Setenv("OCM_TOKEN", origToken)
	os.Setenv("OCM_TOKEN", "env-token-xyz")

	token, clientID, err := ResolveAuthWithConfigDirs([]string{filepath.Join(t.TempDir(), "nonexistent")})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if token != "env-token-xyz" {
		t.Errorf("token = %q, want %q", token, "env-token-xyz")
	}
	if clientID != "" {
		t.Errorf("clientID = %q, want empty string for env var token", clientID)
	}
}

// TestMC111ResolveAuth_MultiDirFallbackPreservesClientID verifies that when
// multiple config dirs are provided and the first is invalid, the client_id
// from the second valid config is returned.
func TestMC111ResolveAuth_MultiDirFallbackPreservesClientID(t *testing.T) {
	origToken := os.Getenv("OCM_TOKEN")
	defer os.Setenv("OCM_TOKEN", origToken)
	os.Unsetenv("OCM_TOKEN")

	// First dir: no config file
	dir1 := filepath.Join(t.TempDir(), "nonexistent")

	// Second dir: valid config with client_id
	dir2 := t.TempDir()
	configJSON := `{"refresh_token": "fallback-token", "client_id": "ocm-cli"}`
	if err := os.WriteFile(filepath.Join(dir2, "ocm.json"), []byte(configJSON), 0644); err != nil {
		t.Fatalf("failed to write test config: %v", err)
	}

	token, clientID, err := ResolveAuthWithConfigDirs([]string{dir1, dir2})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if token != "fallback-token" {
		t.Errorf("token = %q, want %q", token, "fallback-token")
	}
	if clientID != "ocm-cli" {
		t.Errorf("clientID = %q, want %q", clientID, "ocm-cli")
	}
}

// TestMC111ParseOCMConfig_ClientIDField verifies that ParseOCMConfig correctly
// extracts the client_id field from ocm.json into OCMConfig.ClientID.
func TestMC111ParseOCMConfig_ClientIDField(t *testing.T) {
	tests := []struct {
		name         string
		content      string
		wantClientID string
		wantToken    string
	}{
		{
			name:         "config with client_id",
			content:      `{"refresh_token": "tok", "client_id": "ocm-cli"}`,
			wantClientID: "ocm-cli",
			wantToken:    "tok",
		},
		{
			name:         "config without client_id",
			content:      `{"refresh_token": "tok"}`,
			wantClientID: "",
			wantToken:    "tok",
		},
		{
			name:         "config with empty client_id",
			content:      `{"refresh_token": "tok", "client_id": ""}`,
			wantClientID: "",
			wantToken:    "tok",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "ocm.json")
			if err := os.WriteFile(path, []byte(tt.content), 0644); err != nil {
				t.Fatalf("failed to write test config: %v", err)
			}

			config, err := ParseOCMConfig(path)
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}

			if config.ClientID != tt.wantClientID {
				t.Errorf("ClientID = %q, want %q", config.ClientID, tt.wantClientID)
			}
			if config.RefreshToken != tt.wantToken {
				t.Errorf("RefreshToken = %q, want %q", config.RefreshToken, tt.wantToken)
			}
		})
	}
}

// TestMC111ResolveAuth_TopLevelFunction verifies that ResolveAuth() (the
// convenience wrapper using default config paths) exists and returns
// (token, clientID, error).
func TestMC111ResolveAuth_TopLevelFunction(t *testing.T) {
	// Use OCM_TOKEN env to avoid needing real config files.
	origToken := os.Getenv("OCM_TOKEN")
	defer os.Setenv("OCM_TOKEN", origToken)
	os.Setenv("OCM_TOKEN", "env-token-for-resolve-auth")

	token, clientID, err := ResolveAuth()
	if err != nil {
		t.Fatalf("ResolveAuth() error: %v", err)
	}
	if token != "env-token-for-resolve-auth" {
		t.Errorf("token = %q, want %q", token, "env-token-for-resolve-auth")
	}
	// Env var tokens have no client ID.
	if clientID != "" {
		t.Errorf("clientID = %q, want empty string", clientID)
	}
}

// TestMC110OcmConfigMacosPath_Regression verifies that ResolveToken() can find
// the OCM config file at the macOS-standard path
// ~/Library/Application Support/ocm/ocm.json, not just ~/.config/ocm/ocm.json.
//
// Bug: MC-110
// Reproduced: ResolveToken() hardcodes ~/.config/ocm; on macOS the ocm CLI
// writes to ~/Library/Application Support/ocm, so the fallback path never
// finds the file.
// Expected: ResolveToken() checks both XDG (~/.config/ocm) and macOS
// (~/Library/Application Support/ocm) config paths, returning the token
// from whichever location has a valid config file.
// Actual: ResolveToken() only checks ~/.config/ocm and returns an error
// when the config exists only at the macOS Application Support path.
func TestMC110OcmConfigMacosPath_Regression(t *testing.T) {
	// Ensure OCM_TOKEN is unset so we fall through to config file lookup.
	origToken := os.Getenv("OCM_TOKEN")
	defer os.Setenv("OCM_TOKEN", origToken)
	os.Unsetenv("OCM_TOKEN")

	// Create a fake HOME with the config ONLY at the macOS path.
	fakeHome := t.TempDir()
	macOSConfigDir := filepath.Join(fakeHome, "Library", "Application Support", "ocm")
	if err := os.MkdirAll(macOSConfigDir, 0755); err != nil {
		t.Fatalf("failed to create macOS config dir: %v", err)
	}
	configContent := `{"refresh_token": "macos-token-abc123"}`
	if err := os.WriteFile(filepath.Join(macOSConfigDir, "ocm.json"), []byte(configContent), 0644); err != nil {
		t.Fatalf("failed to write config: %v", err)
	}

	// Verify the Linux/XDG path does NOT exist.
	linuxConfigPath := filepath.Join(fakeHome, ".config", "ocm", "ocm.json")
	if _, err := os.Stat(linuxConfigPath); err == nil {
		t.Fatal("linux config path should not exist in this test setup")
	}

	// Point HOME to our fake home so ResolveToken() uses it.
	origHome := os.Getenv("HOME")
	defer os.Setenv("HOME", origHome)
	os.Setenv("HOME", fakeHome)

	// Call the real ResolveToken() -- it should find the macOS config.
	token, err := ResolveToken()
	if err != nil {
		t.Fatalf("ResolveToken() should find config at macOS path, but got error: %v", err)
	}

	if token != "macos-token-abc123" {
		t.Errorf("expected token %q, got %q", "macos-token-abc123", token)
	}
}
