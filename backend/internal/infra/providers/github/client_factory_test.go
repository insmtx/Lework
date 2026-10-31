package githubprovider

import (
	"context"
	"crypto/rand"
	"crypto/rsa"
	"crypto/x509"
	"encoding/pem"
	"io"
	"net/http"
	"strings"
	"testing"

	"github.com/insmtx/Lework/backend/config"
	"github.com/insmtx/Lework/backend/internal/api/auth"
)

func TestClientFactoryResolveClientUsesInstallationSelector(t *testing.T) {
	privateKeyPEM := generateRSAPrivateKeyPEM(t)
	var installationTokenCalls int

	factory := NewClientFactoryWithHTTPClient(config.GithubAppConfig{
		AppID:      12345,
		PrivateKey: privateKeyPEM,
		BaseURL:    "https://api.github.test/",
	}, nil, &http.Client{
		Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
			switch {
			case req.Method == http.MethodPost && req.URL.String() == "https://api.github.test/app/installations/99/access_tokens":
				installationTokenCalls++
				if !strings.HasPrefix(req.Header.Get("Authorization"), "Bearer ") {
					t.Fatalf("expected bearer app JWT, got %q", req.Header.Get("Authorization"))
				}
				return jsonHTTPResponse(req, `{
					"token":"installation-token",
					"expires_at":"2026-04-15T12:00:00Z",
					"permissions":{"contents":"read","pull_requests":"write"}
				}`), nil
			case req.Method == http.MethodGet && req.URL.String() == "https://api.github.test/repos/insmtx/Lework":
				if req.Header.Get("Authorization") != "Bearer installation-token" {
					t.Fatalf("expected installation token auth header, got %q", req.Header.Get("Authorization"))
				}
				return jsonHTTPResponse(req, `{"id":1,"full_name":"insmtx/Lework"}`), nil
			default:
				return &http.Response{
					StatusCode: http.StatusNotFound,
					Header:     make(http.Header),
					Body:       io.NopCloser(strings.NewReader(`{"message":"not found"}`)),
					Request:    req,
				}, nil
			}
		}),
	})

	resolved, err := factory.ResolveClient(context.Background(), &ResolveClientRequest{
		Selector: &auth.AuthSelector{
			Provider: auth.ProviderGitHub,
			ExternalRefs: map[string]string{
				"github.installation_id": "99",
			},
		},
	})
	if err != nil {
		t.Fatalf("resolve client: %v", err)
	}
	if resolved.ResolvedBy != "github_installation" {
		t.Fatalf("expected github_installation, got %s", resolved.ResolvedBy)
	}
	if resolved.Account == nil || resolved.Account.AccountType != auth.AccountTypeAppInstallation {
		t.Fatalf("unexpected resolved account: %+v", resolved.Account)
	}
	if installationTokenCalls != 1 {
		t.Fatalf("expected one installation token exchange, got %d", installationTokenCalls)
	}

	_, _, err = resolved.Client.Repositories.Get(context.Background(), "insmtx", "Lework")
	if err != nil {
		t.Fatalf("call repositories get with installation client: %v", err)
	}
}

type roundTripFunc func(req *http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(req *http.Request) (*http.Response, error) {
	return f(req)
}

func jsonHTTPResponse(req *http.Request, body string) *http.Response {
	return &http.Response{
		StatusCode: http.StatusOK,
		Header: http.Header{
			"Content-Type": []string{"application/json"},
		},
		Body:    io.NopCloser(strings.NewReader(body)),
		Request: req,
	}
}

func generateRSAPrivateKeyPEM(t *testing.T) string {
	t.Helper()

	privateKey, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatalf("generate RSA key: %v", err)
	}

	block := &pem.Block{
		Type:  "RSA PRIVATE KEY",
		Bytes: x509.MarshalPKCS1PrivateKey(privateKey),
	}

	return string(pem.EncodeToMemory(block))
}
