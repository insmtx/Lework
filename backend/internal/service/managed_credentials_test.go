package service

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	localauth "github.com/insmtx/Leros/backend/internal/api/auth"
)

func TestHTTPManagedCredentialProviderResolvesValuesWithLoginToken(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost || r.Header.Get("Authorization") != "Bearer test-login-token" || r.Header.Get("X-Lework-Credential-Token") != "test-service-token" {
			t.Errorf("request method/auth = %s/%q", r.Method, r.Header.Get("Authorization"))
		}
		var input ManagedCredentialRequest
		if err := json.NewDecoder(r.Body).Decode(&input); err != nil {
			t.Errorf("decode request: %v", err)
		}
		if input.ConnectorCode != "demo" || input.Purpose != "mcp_connector" {
			t.Errorf("request = %#v", input)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"code":0,"response":{"schema":"managed-credentials/v1","values":{"api_key":"server-only-test-key"}}}`))
	}))
	defer server.Close()

	provider := NewHTTPManagedCredentialProvider(server.URL, "test-service-token")
	ctx := localauth.WithBearerToken(context.Background(), "test-login-token")
	values, err := provider.Resolve(ctx, ManagedCredentialRequest{ConnectorCode: "demo", Purpose: "mcp_connector"})
	if err != nil {
		t.Fatal(err)
	}
	if values["api_key"] != "server-only-test-key" {
		t.Fatalf("resolved credential = %#v", values)
	}
}
