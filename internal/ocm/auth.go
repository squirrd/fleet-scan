package ocm

// ResolveToken determines the OCM authentication token.
// Priority: OCM_TOKEN env var > refresh_token from ~/.config/ocm/ocm.json.
// TODO: implement token resolution.
func ResolveToken() (string, error) {
	return "", nil
}

// ResolveTokenWithConfigDir is like ResolveToken but accepts a custom config
// directory path for testing. The config file is expected at configDir/ocm.json.
// TODO: implement token resolution.
func ResolveTokenWithConfigDir(configDir string) (string, error) {
	return "", nil
}

// OCMConfig represents the structure of ~/.config/ocm/ocm.json.
type OCMConfig struct {
	RefreshToken string `json:"refresh_token"`
}

// ParseOCMConfig reads and parses an OCM config file.
// TODO: implement config file parsing.
func ParseOCMConfig(path string) (*OCMConfig, error) {
	return nil, nil
}
