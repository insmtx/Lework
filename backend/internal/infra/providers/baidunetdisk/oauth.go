// Package baidunetdisk implements the server-side Baidu Netdisk OAuth client.
package baidunetdisk

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"
)

const (
	authorizeURL = "https://openapi.baidu.com/oauth/2.0/authorize"
	tokenURL     = "https://openapi.baidu.com/oauth/2.0/token"
	maxBodyBytes = 1 << 20
)

// AppConfig is the operations-managed OAuth application projection read from an MCP channel.
type AppConfig struct {
	AppKey      string
	SecretKey   string
	RedirectURI string
	Scopes      []string
}

// TokenSet is the normalized result of an authorization-code exchange or refresh.
type TokenSet struct {
	AccessToken  string
	RefreshToken string
	ExpiresIn    time.Duration
	Scopes       []string
}

// ProviderError exposes only the stable provider error code.
type ProviderError struct {
	Code string
}

func (e *ProviderError) Error() string { return "baidu oauth request failed: " + e.Code }

// Client calls the fixed Baidu OAuth endpoints without accepting database-controlled destinations.
type Client struct {
	httpClient   *http.Client
	authorizeURL string
	tokenURL     string
}

// NewClient creates a Baidu OAuth client.
func NewClient(httpClient *http.Client) *Client {
	if httpClient == nil {
		httpClient = &http.Client{Timeout: 10 * time.Second}
	}
	return &Client{httpClient: httpClient, authorizeURL: authorizeURL, tokenURL: tokenURL}
}

// AuthorizationURL builds the user-facing authorization-code URL.
func (c *Client) AuthorizationURL(config AppConfig, state string) (string, error) {
	if err := validateConfig(config); err != nil {
		return "", err
	}
	query := url.Values{
		"response_type": {"code"},
		"client_id":     {config.AppKey},
		"redirect_uri":  {config.RedirectURI},
		"scope":         {strings.Join(config.Scopes, ",")},
		"state":         {state},
		"display":       {"popup"},
	}
	return c.authorizeURL + "?" + query.Encode(), nil
}

// ExchangeCode exchanges one authorization code for a token set.
func (c *Client) ExchangeCode(ctx context.Context, config AppConfig, code string) (*TokenSet, error) {
	if strings.TrimSpace(code) == "" {
		return nil, fmt.Errorf("authorization code is required")
	}
	return c.requestToken(ctx, config, url.Values{
		"grant_type":   {"authorization_code"},
		"code":         {code},
		"redirect_uri": {config.RedirectURI},
	})
}

// Refresh exchanges the latest refresh token for a successor token set.
func (c *Client) Refresh(ctx context.Context, config AppConfig, refreshToken string) (*TokenSet, error) {
	if strings.TrimSpace(refreshToken) == "" {
		return nil, fmt.Errorf("refresh token is required")
	}
	return c.requestToken(ctx, config, url.Values{
		"grant_type":    {"refresh_token"},
		"refresh_token": {refreshToken},
	})
}

func (c *Client) requestToken(ctx context.Context, config AppConfig, query url.Values) (*TokenSet, error) {
	if err := validateConfig(config); err != nil {
		return nil, err
	}
	query.Set("client_id", config.AppKey)
	query.Set("client_secret", config.SecretKey)
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.tokenURL+"?"+query.Encode(), nil)
	if err != nil {
		return nil, errors.New("create baidu oauth request")
	}
	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, errors.New("execute baidu oauth request")
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(io.LimitReader(resp.Body, maxBodyBytes))
	if err != nil {
		return nil, fmt.Errorf("read baidu oauth response: %w", err)
	}
	var value struct {
		AccessToken      string      `json:"access_token"`
		RefreshToken     string      `json:"refresh_token"`
		ExpiresIn        json.Number `json:"expires_in"`
		Scope            string      `json:"scope"`
		Error            string      `json:"error"`
		ErrorDescription string      `json:"error_description"`
	}
	if err := json.Unmarshal(body, &value); err != nil {
		return nil, fmt.Errorf("decode baidu oauth response")
	}
	if resp.StatusCode < http.StatusOK || resp.StatusCode >= http.StatusMultipleChoices || value.Error != "" {
		code := strings.TrimSpace(value.Error)
		if code == "" {
			code = "http_" + strconv.Itoa(resp.StatusCode)
		}
		return nil, &ProviderError{Code: code}
	}
	seconds, err := strconv.ParseInt(string(value.ExpiresIn), 10, 64)
	if err != nil || seconds <= 0 || strings.TrimSpace(value.AccessToken) == "" {
		return nil, fmt.Errorf("baidu oauth response is incomplete")
	}
	return &TokenSet{
		AccessToken: strings.TrimSpace(value.AccessToken), RefreshToken: strings.TrimSpace(value.RefreshToken),
		ExpiresIn: time.Duration(seconds) * time.Second, Scopes: splitScopes(value.Scope, config.Scopes),
	}, nil
}

func validateConfig(config AppConfig) error {
	if strings.TrimSpace(config.AppKey) == "" || strings.TrimSpace(config.SecretKey) == "" {
		return fmt.Errorf("baidu oauth application credentials are incomplete")
	}
	redirect, err := url.Parse(strings.TrimSpace(config.RedirectURI))
	if err != nil || redirect == nil || redirect.Host == "" ||
		(redirect.Scheme != "https" && redirect.Scheme != "http") || redirect.User != nil ||
		redirect.RawQuery != "" || redirect.Fragment != "" {
		return fmt.Errorf("baidu oauth redirect uri is invalid")
	}
	if len(config.Scopes) == 0 {
		return fmt.Errorf("baidu oauth scopes are required")
	}
	return nil
}

func splitScopes(value string, fallback []string) []string {
	parts := strings.FieldsFunc(value, func(r rune) bool { return r == ',' || r == ' ' })
	if len(parts) == 0 {
		return append([]string(nil), fallback...)
	}
	return parts
}
