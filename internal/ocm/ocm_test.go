package ocm

import (
	"testing"
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
