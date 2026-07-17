package ocm

import (
	"testing"

	sdk "github.com/openshift-online/ocm-sdk-go"
)

// TestMc111SdkClientIdMismatch_Regression verifies that NewSDKClient forwards
// the OAuth client ID to the SDK connection builder so that refresh token
// exchange uses the correct client identity.
//
// Bug: MC-111
// Reproduced: NewSDKClient() omits .Client(clientID, "") when building the SDK
// connection. The OCM CLI issues refresh tokens bound to client_id "ocm-cli",
// but the SDK defaults to "cloud-services" when no client ID is set.
// Expected: When a client ID is provided (e.g. "ocm-cli" from ocm.json),
// NewSDKClient passes it to the connection builder via .Client(clientID, ""),
// so the refresh token exchange succeeds.
// Actual: NewSDKClient ignores the client ID, causing the SDK to default to
// "cloud-services". The OAuth server rejects the refresh with:
// "can't get access token: invalid_grant: Invalid refresh token. Token client
// and authorized client don't match"
func TestMc111SdkClientIdMismatch_Regression(t *testing.T) {
	// The OCM config written by `ocm login` typically has client_id = "ocm-cli".
	// NewSDKClient must accept and forward this to the SDK connection builder.
	wantClientID := "ocm-cli"

	// Create an SDK client with a dummy token and the expected client ID.
	// No API calls are made — we only inspect the connection configuration.
	client, err := NewSDKClient("offline-token-dummy", wantClientID)
	if err != nil {
		t.Fatalf("NewSDKClient() error: %v", err)
	}

	// Verify the connection was built with the correct client ID.
	sc, ok := client.(*sdkClient)
	if !ok {
		t.Fatal("NewSDKClient() did not return *sdkClient")
	}

	gotClientID, _ := sc.conn.Client()
	if gotClientID != wantClientID {
		t.Errorf("connection client ID = %q, want %q; "+
			"refresh token exchange will fail when the token was issued for a "+
			"different OAuth client",
			gotClientID, wantClientID)
	}
}

// TestNewSDKClient_EmptyClientID verifies that when no client ID is provided
// (empty string), the SDK falls back to its default client ID ("cloud-services").
// This preserves backwards compatibility for callers using access tokens or
// the default SSO client.
func TestNewSDKClient_EmptyClientID(t *testing.T) {
	client, err := NewSDKClient("some-token", "")
	if err != nil {
		t.Fatalf("NewSDKClient() error: %v", err)
	}

	sc, ok := client.(*sdkClient)
	if !ok {
		t.Fatal("NewSDKClient() did not return *sdkClient")
	}

	gotClientID, _ := sc.conn.Client()
	// When no client ID is specified, the SDK defaults to "cloud-services".
	if gotClientID != "cloud-services" {
		t.Errorf("connection client ID = %q, want %q (SDK default)",
			gotClientID, "cloud-services")
	}
}

// TestNewSDKClient_URLOverride verifies that NewSDKClient forwards a non-empty
// URL to the SDK connection builder so the client connects to the specified
// endpoint instead of silently defaulting to the production URL.
//
// Bug: MC-127
// Reproduced: NewSDKClient() never calls builder.URL(), so the OCM SDK always
// connects to sdk.DefaultURL ("https://api.openshift.com") regardless of any
// --ocm-url flag or config file value passed by the caller.
// Expected: When a non-empty URL is provided, NewSDKClient passes it to the
// connection builder via builder.URL(url), so the OCM client connects to the
// correct endpoint (e.g. staging, integration, or a custom gateway).
// Actual: The URL parameter is ignored; the SDK always uses the production
// default, making it impossible to target non-production clusters.
//
// backwards_compatibility: tests public API contract
func TestNewSDKClient_URLOverride(t *testing.T) {
	tests := []struct {
		name    string
		url     string
		wantURL string
	}{
		{
			name:    "custom URL is forwarded to connection",
			url:     "https://api.stage.openshift.com",
			wantURL: "https://api.stage.openshift.com",
		},
		{
			name:    "empty URL uses SDK production default",
			url:     "",
			wantURL: sdk.DefaultURL,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			client, err := NewSDKClient("offline-token-dummy", "", tt.url)
			if err != nil {
				t.Fatalf("NewSDKClient() error: %v", err)
			}

			sc, ok := client.(*sdkClient)
			if !ok {
				t.Fatal("NewSDKClient() did not return *sdkClient")
			}

			gotURL := sc.conn.URL()
			if gotURL != tt.wantURL {
				t.Errorf("connection URL = %q, want %q; "+
					"the OCM client will silently connect to the wrong endpoint",
					gotURL, tt.wantURL)
			}
		})
	}
}
