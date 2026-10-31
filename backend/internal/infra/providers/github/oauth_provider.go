package githubprovider

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	gogithub "github.com/google/go-github/v78/github"
	"github.com/insmtx/Lework/backend/config"
	"github.com/insmtx/Lework/backend/internal/api/auth"
)

const (
	githubAuthorizeURL = "https://github.com/login/oauth/authorize"
	githubTokenURL     = "https://github.com/login/oauth/access_token"
)

var defaultOAuthScopes = []string{"read:user", "user:email", "repo"}

// 确保 OAuthProvider 实现了 auth.AuthorizationProvider 接口
var _ auth.AuthorizationProvider = (*OAuthProvider)(nil)

// OAuthProvider 实现 GitHub 用户 OAuth 授权接入。
type OAuthProvider struct {
	cfg        config.GithubAppConfig
	httpClient *http.Client
}

// NewOAuthProvider 创建一个新的 GitHub OAuth provider。
func NewOAuthProvider(cfg config.GithubAppConfig) *OAuthProvider {
	return &OAuthProvider{
		cfg:        cfg,
		httpClient: &http.Client{Timeout: 15 * time.Second},
	}
}

// ProviderCode 返回 provider 标识。
func (p *OAuthProvider) ProviderCode() string {
	return auth.ProviderGitHub
}

// BuildAuthorizationURL 构造 GitHub 授权 URL。
func (p *OAuthProvider) BuildAuthorizationURL(req *auth.StartAuthorizationRequest, state *auth.OAuthState) (string, error) {
	if p.cfg.ClientID == "" {
		return "", fmt.Errorf("github oauth client_id is not configured")
	}

	query := url.Values{}
	query.Set("client_id", p.cfg.ClientID)
	query.Set("state", state.State)
	query.Set("scope", strings.Join(resolveScopes(p.cfg.OAuthScopes), " "))

	redirectURI := state.RedirectURI
	if redirectURI == "" {
		redirectURI = p.cfg.RedirectURL
	}
	if redirectURI != "" {
		query.Set("redirect_uri", redirectURI)
	}

	return githubAuthorizeURL + "?" + query.Encode(), nil
}

// CompleteAuthorization 用 code 完成 GitHub 授权。
func (p *OAuthProvider) CompleteAuthorization(req *auth.CompleteAuthorizationRequest) (*auth.AuthorizationResult, error) {
	tokenResp, err := p.exchangeCodeForToken(req)
	if err != nil {
		return nil, err
	}

	client := gogithub.NewTokenClient(context.Background(), tokenResp.AccessToken)
	user, _, err := client.Users.Get(context.Background(), "")
	if err != nil {
		return nil, fmt.Errorf("fetch github user profile: %w", err)
	}

	externalID := strconv.FormatInt(user.GetID(), 10)
	now := time.Now().UTC()
	account := &auth.AuthorizedAccount{
		ID:                buildAccountID(req.State.UserID, externalID),
		UserID:            req.State.UserID,
		Provider:          auth.ProviderGitHub,
		OwnerType:         auth.AccountOwnerTypeUser,
		AccountType:       auth.AccountTypeUserOAuth,
		ExternalAccountID: externalID,
		DisplayName:       resolveDisplayName(user),
		Scopes:            splitScopes(tokenResp.Scope),
		Status:            auth.AccountStatusActive,
		Metadata: map[string]string{
			"github_login": user.GetLogin(),
			"name":         user.GetName(),
			"email":        user.GetEmail(),
			"avatar_url":   user.GetAvatarURL(),
		},
		CreatedAt: now,
		UpdatedAt: now,
	}

	credential := &auth.AccountCredential{
		AccountID:   account.ID,
		GrantType:   auth.GrantTypeOAuth2,
		AccessToken: tokenResp.AccessToken,
		Metadata: map[string]string{
			"token_type": tokenResp.TokenType,
			"scope":      tokenResp.Scope,
		},
	}

	return &auth.AuthorizationResult{
		Account:    account,
		Credential: credential,
	}, nil
}

type tokenResponse struct {
	AccessToken string `json:"access_token"`
	TokenType   string `json:"token_type"`
	Scope       string `json:"scope"`
}

func (p *OAuthProvider) exchangeCodeForToken(req *auth.CompleteAuthorizationRequest) (*tokenResponse, error) {
	form := url.Values{}
	form.Set("code", req.Code)
	form.Set("client_id", p.cfg.ClientID)
	form.Set("client_secret", p.cfg.ClientSecret)

	redirectURI := req.State.RedirectURI
	if redirectURI == "" {
		redirectURI = p.cfg.RedirectURL
	}
	if redirectURI != "" {
		form.Set("redirect_uri", redirectURI)
	}

	httpReq, err := http.NewRequest(http.MethodPost, githubTokenURL, strings.NewReader(form.Encode()))
	if err != nil {
		return nil, fmt.Errorf("create github token request: %w", err)
	}

	httpReq.Header.Set("Accept", "application/json")
	httpReq.Header.Set("Content-Type", "application/x-www-form-urlencoded")

	resp, err := p.httpClient.Do(httpReq)
	if err != nil {
		return nil, fmt.Errorf("execute github token request: %w", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("read github token response: %w", err)
	}

	var tokenResp tokenResponse
	if err := json.Unmarshal(body, &tokenResp); err != nil {
		return nil, fmt.Errorf("parse github token response: %w", err)
	}

	if tokenResp.AccessToken == "" {
		return nil, fmt.Errorf("github access token missing in response")
	}

	return &tokenResp, nil
}

func buildAccountID(userID, externalID string) string {
	return auth.ProviderGitHub + ":" + userID + ":" + externalID
}

func resolveDisplayName(user *gogithub.User) string {
	if user.GetLogin() != "" {
		return user.GetLogin()
	}
	if user.GetName() != "" {
		return user.GetName()
	}
	return "github-user"
}

func splitScopes(scope string) []string {
	if scope == "" {
		return append([]string(nil), defaultOAuthScopes...)
	}

	parts := strings.Split(scope, ",")
	scopes := make([]string, 0, len(parts))
	for _, part := range parts {
		trimmed := strings.TrimSpace(part)
		if trimmed == "" {
			continue
		}
		scopes = append(scopes, trimmed)
	}

	if len(scopes) == 0 {
		return append([]string(nil), defaultOAuthScopes...)
	}

	return scopes
}

func resolveScopes(configured []string) []string {
	if len(configured) == 0 {
		return append([]string(nil), defaultOAuthScopes...)
	}

	scopes := make([]string, 0, len(configured))
	for _, scope := range configured {
		trimmed := strings.TrimSpace(scope)
		if trimmed == "" {
			continue
		}
		scopes = append(scopes, trimmed)
	}
	if len(scopes) == 0 {
		return append([]string(nil), defaultOAuthScopes...)
	}
	return scopes
}
