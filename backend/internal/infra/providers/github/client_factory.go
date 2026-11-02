// githubprovider 包提供 GitHub provider client 工厂。
package githubprovider

import (
	"context"
	"crypto"
	"crypto/rand"
	"crypto/rsa"
	"crypto/sha256"
	"crypto/x509"
	"encoding/base64"
	"encoding/json"
	"encoding/pem"
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"time"

	gogithub "github.com/google/go-github/v78/github"
	"github.com/insmtx/Lework/backend/config"
	"github.com/insmtx/Lework/backend/internal/api/auth"
)

const installationAccessTokenTTL = 9 * time.Minute

var (
	errAuthServiceRequired = fmt.Errorf("github auth service is required")
	errEmptyAccessToken    = fmt.Errorf("github access token is empty")
)

// ResolveClientRequest 表示一次 GitHub client 解析请求。
type ResolveClientRequest struct {
	Selector  *auth.AuthSelector
	UserID    string
	AccountID string
}

// ResolvedClient 表示解析后的 GitHub client 与账户信息。
type ResolvedClient struct {
	Client     *gogithub.Client
	Account    *auth.AuthorizedAccount
	Credential *auth.AccountCredential
	ResolvedBy string
}

// ClientFactory 负责把已授权账户转换成 GitHub client。
type ClientFactory struct {
	cfg         config.GithubAppConfig
	authService *auth.ThirdPartyAuthService
	httpClient  *http.Client
	now         func() time.Time
	resolvers   []clientResolver
}

// NewClientFactory 创建一个新的 GitHub client 工厂。
func NewClientFactory(cfg config.GithubAppConfig, authService *auth.ThirdPartyAuthService) *ClientFactory {
	return NewClientFactoryWithHTTPClient(cfg, authService, nil)
}

// NewClientFactoryWithHTTPClient 创建一个可注入自定义 HTTP client 的 GitHub client 工厂。
func NewClientFactoryWithHTTPClient(cfg config.GithubAppConfig, authService *auth.ThirdPartyAuthService, httpClient *http.Client) *ClientFactory {
	if httpClient == nil {
		httpClient = &http.Client{Timeout: 15 * time.Second}
	}

	return &ClientFactory{
		cfg:         cfg,
		authService: authService,
		httpClient:  httpClient,
		now:         func() time.Time { return time.Now().UTC() },
		resolvers:   defaultResolvers(),
	}
}

// ResolveClient 解析用户账户并创建 GitHub client。
func (f *ClientFactory) ResolveClient(ctx context.Context, req *ResolveClientRequest) (*ResolvedClient, error) {
	if req == nil {
		return nil, fmt.Errorf("resolve client request is required")
	}

	for _, resolver := range f.resolvers {
		resolved, handled, err := resolver.Resolve(ctx, f, req)
		if err != nil {
			return nil, err
		}
		if handled {
			return resolved, nil
		}
	}

	return nil, fmt.Errorf("no github auth resolver matched the request")
}

func (f *ClientFactory) resolveInstallationClient(ctx context.Context, installationID string) (*ResolvedClient, error) {
	if !isInstallationConfigured(f.cfg) {
		return nil, fmt.Errorf("github app installation auth is not configured")
	}

	appJWT, err := f.buildAppJWT()
	if err != nil {
		return nil, err
	}

	endpoint, err := f.resolveEndpoint("app/installations/" + installationID + "/access_tokens")
	if err != nil {
		return nil, err
	}

	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, nil)
	if err != nil {
		return nil, fmt.Errorf("create installation token request: %w", err)
	}
	httpReq.Header.Set("Accept", "application/vnd.github+json")
	httpReq.Header.Set("Authorization", "Bearer "+appJWT)

	resp, err := f.httpClient.Do(httpReq)
	if err != nil {
		return nil, fmt.Errorf("execute installation token request: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, fmt.Errorf("installation token request failed with status %d", resp.StatusCode)
	}

	var tokenResp installationTokenResponse
	if err := json.NewDecoder(resp.Body).Decode(&tokenResp); err != nil {
		return nil, fmt.Errorf("decode installation token response: %w", err)
	}
	if tokenResp.Token == "" {
		return nil, fmt.Errorf("github installation access token is empty")
	}

	account := &auth.AuthorizedAccount{
		ID:                buildInstallationAccountID(installationID),
		Provider:          auth.ProviderGitHub,
		OwnerType:         auth.AccountOwnerTypeSystem,
		AccountType:       auth.AccountTypeAppInstallation,
		ExternalAccountID: installationID,
		DisplayName:       resolveInstallationDisplayName(installationID),
		Scopes:            append([]string(nil), tokenResp.Permissions.Keys()...),
		Status:            auth.AccountStatusActive,
		Metadata: map[string]string{
			"installation_id": installationID,
		},
		CreatedAt: f.now(),
		UpdatedAt: f.now(),
	}
	credential := &auth.AccountCredential{
		AccountID:   account.ID,
		GrantType:   "app_installation",
		AccessToken: tokenResp.Token,
		ExpiresAt:   tokenResp.ExpiresAt,
		Metadata: map[string]string{
			"installation_id": installationID,
		},
	}

	return &ResolvedClient{
		Client:     f.newGitHubClient(tokenResp.Token),
		Account:    account,
		Credential: credential,
		ResolvedBy: "github_installation",
	}, nil
}

func (f *ClientFactory) buildAppJWT() (string, error) {
	privateKey, err := parsePrivateKey(f.cfg.PrivateKey)
	if err != nil {
		return "", err
	}

	now := f.now()
	header := `{"alg":"RS256","typ":"JWT"}`
	payload := fmt.Sprintf(`{"iat":%d,"exp":%d,"iss":%d}`, now.Add(-60*time.Second).Unix(), now.Add(installationAccessTokenTTL).Unix(), f.cfg.AppID)
	signingInput := base64.RawURLEncoding.EncodeToString([]byte(header)) + "." + base64.RawURLEncoding.EncodeToString([]byte(payload))

	hashed := sha256.Sum256([]byte(signingInput))
	signature, err := rsa.SignPKCS1v15(rand.Reader, privateKey, crypto.SHA256, hashed[:])
	if err != nil {
		return "", fmt.Errorf("sign github app jwt: %w", err)
	}

	return signingInput + "." + base64.RawURLEncoding.EncodeToString(signature), nil
}

func (f *ClientFactory) newGitHubClient(token string) *gogithub.Client {
	client := gogithub.NewClient(f.httpClient).WithAuthToken(token)
	if f.cfg.BaseURL != "" {
		baseURL, err := url.Parse(f.cfg.BaseURL)
		if err == nil {
			client.BaseURL = baseURL
		}
	}
	return client
}

func (f *ClientFactory) resolveEndpoint(path string) (string, error) {
	baseURL := f.cfg.BaseURL
	if baseURL == "" {
		baseURL = "https://api.github.com/"
	}
	parsed, err := url.Parse(baseURL)
	if err != nil {
		return "", fmt.Errorf("parse github base_url: %w", err)
	}
	ref, err := url.Parse(path)
	if err != nil {
		return "", fmt.Errorf("parse github endpoint path: %w", err)
	}
	return parsed.ResolveReference(ref).String(), nil
}

func explicitProfileID(req *ResolveClientRequest) string {
	if req == nil {
		return ""
	}
	if req.Selector != nil && req.Selector.ExplicitProfileID != "" {
		return req.Selector.ExplicitProfileID
	}
	return req.AccountID
}

func selectorExternalRef(selector *auth.AuthSelector, key string) string {
	if selector == nil || selector.ExternalRefs == nil {
		return ""
	}
	return selector.ExternalRefs[key]
}

func (f *ClientFactory) buildResolveAuthorizationRequest(req *ResolveClientRequest) *auth.ResolveAuthorizationRequest {
	return &auth.ResolveAuthorizationRequest{
		Selector:  req.Selector,
		UserID:    req.UserID,
		Provider:  auth.ProviderGitHub,
		AccountID: req.AccountID,
	}
}

func isInstallationConfigured(cfg config.GithubAppConfig) bool {
	return cfg.AppID > 0 && strings.TrimSpace(cfg.PrivateKey) != ""
}

func parsePrivateKey(value string) (*rsa.PrivateKey, error) {
	block, _ := pem.Decode([]byte(value))
	if block == nil {
		return nil, fmt.Errorf("decode github app private key: invalid PEM")
	}

	if key, err := x509.ParsePKCS1PrivateKey(block.Bytes); err == nil {
		return key, nil
	}

	parsed, err := x509.ParsePKCS8PrivateKey(block.Bytes)
	if err != nil {
		return nil, fmt.Errorf("parse github app private key: %w", err)
	}
	key, ok := parsed.(*rsa.PrivateKey)
	if !ok {
		return nil, fmt.Errorf("parse github app private key: not an RSA private key")
	}
	return key, nil
}

type installationTokenResponse struct {
	Token       string                  `json:"token"`
	ExpiresAt   *time.Time              `json:"expires_at,omitempty"`
	Permissions installationPermissions `json:"permissions,omitempty"`
}

type installationPermissions map[string]string

func (p installationPermissions) Keys() []string {
	if len(p) == 0 {
		return nil
	}
	keys := make([]string, 0, len(p))
	for key, value := range p {
		keys = append(keys, key+":"+value)
	}
	return keys
}

func buildInstallationAccountID(installationID string) string {
	return auth.ProviderGitHub + ":installation:" + installationID
}

func resolveInstallationDisplayName(installationID string) string {
	if installationID == "" {
		return "github-installation"
	}
	return "github-installation-" + installationID
}
