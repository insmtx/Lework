package baidunetdisk

import (
	"context"
	"errors"
	"io"
	"net/http"
	"net/url"
	"strings"
	"testing"
)

type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(request *http.Request) (*http.Response, error) {
	return f(request)
}

func TestTokenTransportErrorDoesNotExposeRequestSecrets(t *testing.T) {
	client := NewClient(&http.Client{Transport: roundTripFunc(func(request *http.Request) (*http.Response, error) {
		return nil, errors.New(request.URL.String())
	})})
	_, err := client.Refresh(context.Background(), AppConfig{
		AppKey: "app-key", SecretKey: "secret-key", RedirectURI: "https://leros.example.com/callback",
		Scopes: []string{"basic", "netdisk"},
	}, "refresh-secret")
	if err == nil || strings.Contains(err.Error(), "secret-key") || strings.Contains(err.Error(), "refresh-secret") {
		t.Fatalf("refresh error leaked request secrets: %v", err)
	}
}

func TestAuthorizationURLUsesConfiguredApplicationAndState(t *testing.T) {
	client := NewClient(nil)
	value, err := client.AuthorizationURL(AppConfig{
		AppKey: "app-key", SecretKey: "secret-key", RedirectURI: "https://leros.example.com/callback",
		Scopes: []string{"basic", "netdisk"},
	}, "opaque-state")
	if err != nil {
		t.Fatalf("AuthorizationURL() error = %v", err)
	}
	parsed, err := url.Parse(value)
	if err != nil {
		t.Fatalf("parse authorization URL: %v", err)
	}
	query := parsed.Query()
	if parsed.Scheme != "https" || parsed.Host != "openapi.baidu.com" ||
		query.Get("client_id") != "app-key" || query.Get("state") != "opaque-state" ||
		query.Get("scope") != "basic,netdisk" || query.Get("redirect_uri") != "https://leros.example.com/callback" {
		t.Fatalf("authorization URL = %q", value)
	}
}

func TestExchangeCodeNormalizesTokenResponseWithoutExposingDescription(t *testing.T) {
	client := NewClient(&http.Client{Transport: roundTripFunc(func(request *http.Request) (*http.Response, error) {
		query := request.URL.Query()
		if query.Get("grant_type") != "authorization_code" || query.Get("code") != "authorization-code" ||
			query.Get("client_secret") != "secret-key" {
			t.Fatalf("token request query = %#v", query)
		}
		return &http.Response{
			StatusCode: http.StatusOK,
			Header:     make(http.Header),
			Body:       io.NopCloser(strings.NewReader(`{"access_token":"access","refresh_token":"refresh","expires_in":2592000,"scope":"basic netdisk"}`)),
		}, nil
	})})
	tokens, err := client.ExchangeCode(context.Background(), AppConfig{
		AppKey: "app-key", SecretKey: "secret-key", RedirectURI: "https://leros.example.com/callback",
		Scopes: []string{"basic", "netdisk"},
	}, "authorization-code")
	if err != nil {
		t.Fatalf("ExchangeCode() error = %v", err)
	}
	if tokens.AccessToken != "access" || tokens.RefreshToken != "refresh" || len(tokens.Scopes) != 2 {
		t.Fatalf("tokens = %#v", tokens)
	}
}
