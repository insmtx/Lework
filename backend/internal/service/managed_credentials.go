package service

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/insmtx/Lework/backend/internal/adapter/account"
	localauth "github.com/insmtx/Lework/backend/internal/api/auth"
)

// ManagedCredentialRequest describes the trusted purpose for resolving connector credentials.
type ManagedCredentialRequest struct {
	ConnectorCode string `json:"connector_code"`
	ConnectorName string `json:"connector_name,omitempty"`
	Purpose       string `json:"purpose"`
}

// ManagedCredentialProvider resolves credentials without exposing them to the browser.
type ManagedCredentialProvider interface {
	Resolve(context.Context, ManagedCredentialRequest) (map[string]string, error)
}

// ManagedCredentialService dispatches managed credential requests by configured handler.
type ManagedCredentialService struct {
	providers map[string]ManagedCredentialProvider
}

// NewManagedCredentialService creates a handler registry for managed connectors.
func NewManagedCredentialService(providers map[string]ManagedCredentialProvider) *ManagedCredentialService {
	copy := make(map[string]ManagedCredentialProvider, len(providers))
	for name, provider := range providers {
		if strings.TrimSpace(name) != "" && provider != nil {
			copy[strings.ToLower(strings.TrimSpace(name))] = provider
		}
	}
	return &ManagedCredentialService{providers: copy}
}

// CanResolve reports whether a managed handler is configured.
func (s *ManagedCredentialService) CanResolve(handler string) bool {
	return s != nil && s.providers[strings.ToLower(strings.TrimSpace(handler))] != nil
}

// Resolve returns a credential set for one configured managed handler.
func (s *ManagedCredentialService) Resolve(ctx context.Context, handler string, req ManagedCredentialRequest) (map[string]string, error) {
	if s == nil {
		return nil, fmt.Errorf("managed credential handler is unavailable")
	}
	provider := s.providers[strings.ToLower(strings.TrimSpace(handler))]
	if provider == nil {
		return nil, fmt.Errorf("managed credential handler is unavailable")
	}
	values, err := provider.Resolve(ctx, req)
	if err != nil {
		return nil, err
	}
	if len(values) == 0 {
		return nil, fmt.Errorf("managed credential provider returned no values")
	}
	for key, value := range values {
		if strings.TrimSpace(key) == "" || value == "" {
			return nil, fmt.Errorf("managed credential provider returned an invalid value")
		}
	}
	return values, nil
}

type apiKeyIssuerCredentialProvider struct{ issuer account.APIKeyIssuer }

func (p apiKeyIssuerCredentialProvider) Resolve(ctx context.Context, req ManagedCredentialRequest) (map[string]string, error) {
	name := req.ConnectorName
	if name == "" {
		name = req.ConnectorCode
	}
	issued, err := p.issuer.CreateAPIKey(ctx, account.CreateAPIKeyInput{
		Name: "SingerOS " + name + " MCP", Purpose: req.Purpose,
		ResourceType: "mcp", ResourceID: 0, ExpireHours: 0,
	})
	if err != nil {
		return nil, fmt.Errorf("issue managed connector credential: %w", err)
	}
	if issued == nil || issued.APIKey == "" {
		return nil, fmt.Errorf("managed credential issuer returned no credential")
	}
	return map[string]string{"api_key": issued.APIKey}, nil
}

// NewAPIKeyIssuerCredentialProvider adapts the existing IAM API key issuer to managed credentials.
func NewAPIKeyIssuerCredentialProvider(issuer account.APIKeyIssuer) ManagedCredentialProvider {
	if issuer == nil {
		return nil
	}
	return apiKeyIssuerCredentialProvider{issuer: issuer}
}

type httpManagedCredentialProvider struct {
	endpoint     string
	serviceToken string
	client       *http.Client
}

// NewHTTPManagedCredentialProvider resolves credentials from a trusted internal endpoint.
func NewHTTPManagedCredentialProvider(endpoint, serviceToken string) ManagedCredentialProvider {
	endpoint = strings.TrimSpace(endpoint)
	serviceToken = strings.TrimSpace(serviceToken)
	if endpoint == "" || serviceToken == "" {
		return nil
	}
	return &httpManagedCredentialProvider{
		endpoint: endpoint, serviceToken: serviceToken,
		client: &http.Client{
			Timeout:       10 * time.Second,
			CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse },
		},
	}
}

func (p *httpManagedCredentialProvider) Resolve(ctx context.Context, input ManagedCredentialRequest) (map[string]string, error) {
	if p.endpoint == "" {
		return nil, fmt.Errorf("managed credential endpoint is not configured")
	}
	token := localauth.BearerTokenFromContext(ctx)
	if token == "" {
		return nil, fmt.Errorf("authenticated session is required")
	}
	body, err := json.Marshal(input)
	if err != nil {
		return nil, fmt.Errorf("encode managed credential request: %w", err)
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, p.endpoint, bytes.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("create managed credential request: %w", err)
	}
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("X-Lework-Credential-Token", p.serviceToken)
	req.Header.Set("Content-Type", "application/json")
	resp, err := p.client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("managed credential endpoint request failed")
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("managed credential endpoint returned status %d", resp.StatusCode)
	}
	var result struct {
		Code     int `json:"code"`
		Response struct {
			Schema string            `json:"schema"`
			Values map[string]string `json:"values"`
		} `json:"response"`
	}
	if err := json.NewDecoder(io.LimitReader(resp.Body, 64*1024)).Decode(&result); err != nil {
		return nil, fmt.Errorf("decode managed credential response: %w", err)
	}
	if result.Code != 0 || result.Response.Schema != "managed-credentials/v1" {
		return nil, fmt.Errorf("managed credential endpoint returned an invalid response")
	}
	return result.Response.Values, nil
}
