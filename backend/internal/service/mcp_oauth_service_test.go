package service

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/url"
	"strings"
	"testing"
	"time"

	infradb "github.com/insmtx/Leros/backend/internal/infra/db"
	baidunetdisk "github.com/insmtx/Leros/backend/internal/infra/providers/baidunetdisk"
	"github.com/insmtx/Leros/backend/types"
)

type oauthRoundTripFunc func(*http.Request) (*http.Response, error)

func (f oauthRoundTripFunc) RoundTrip(request *http.Request) (*http.Response, error) {
	return f(request)
}

func TestBaiduNetdiskOAuthCreatesImmutableActiveRevisionAndRedactsSecrets(t *testing.T) {
	database := setupPluginServiceTestDB(t)
	setupPluginServiceTestStorage(t)
	sourceDir := t.TempDir()
	writeBuiltinSkillTestFiles(
		t, sourceDir, "connector-baidu-netdisk", "Baidu Netdisk connector", "Use Baidu Netdisk MCP.",
	)
	writeBuiltinSkillTestFiles(
		t, sourceDir, "connector-netease-mail", "Mail connector", "Use the mail connector.",
	)
	channel := &types.MCPChannel{
		Channel: baiduNetdiskPlatformCode, Name: "百度网盘", SkillCode: "connector-baidu-netdisk",
		Transport: "sse", URL: "https://mcp-pan.baidu.com/sse", AuthType: types.MCPChannelAuthTypeOAuth,
		AuthConfig: types.MCPChannelAuthConfigJSON{
			Handler: baiduNetdiskPlatformCode,
			OAuth: &types.MCPChannelOAuthConfig{
				AppKey: "app-key", SecretKey: "secret-key",
				RedirectURI: "https://leros.example.com/v1/plugins/mcp/oauth/baidu-netdisk/callback",
				Scopes:      []string{"basic", "netdisk"},
			},
			Bindings: types.MCPChannelAuthBindings{
				MCPQuery: map[string]string{"access_token": baiduNetdiskOAuthValueKey},
			},
		},
		Status: types.MCPChannelStatusActive,
	}
	if err := database.Create(channel).Error; err != nil {
		t.Fatalf("create Baidu Netdisk channel: %v", err)
	}
	report, err := SyncBuiltinConnectorTemplates(context.Background(), database, sourceDir)
	if err != nil || report.Created != 2 || len(report.Failures) != 0 {
		t.Fatalf("sync connector templates = %#v, %v", report, err)
	}

	tokenCalls := 0
	client := baidunetdisk.NewClient(&http.Client{Transport: oauthRoundTripFunc(func(request *http.Request) (*http.Response, error) {
		tokenCalls++
		if request.URL.Query().Get("code") != "authorization-code" {
			t.Fatalf("token request = %s", request.URL.String())
		}
		return &http.Response{
			StatusCode: http.StatusOK,
			Header:     make(http.Header),
			Body: io.NopCloser(strings.NewReader(
				`{"access_token":"access-secret","refresh_token":"refresh-secret","expires_in":2592000,"scope":"basic netdisk"}`,
			)),
		}, nil
	})})
	service := &pluginService{
		db: database,
		oauth: &connectorOAuthManager{baidu: client, now: func() time.Time {
			return time.Date(2026, 8, 3, 2, 0, 0, 0, time.UTC)
		}},
	}
	started, err := service.StartMCPPlatformOAuth(context.Background(), 7, 9, baiduNetdiskPlatformCode)
	if err != nil {
		t.Fatalf("StartMCPPlatformOAuth() error = %v", err)
	}
	authorizationURL, err := url.Parse(started.AuthorizationURL)
	if err != nil || authorizationURL.Query().Get("state") == "" {
		t.Fatalf("authorization URL = %q, %v", started.AuthorizationURL, err)
	}
	state := authorizationURL.Query().Get("state")
	pending, err := service.GetMCPPlatformOAuthStatus(
		context.Background(), 7, 9, baiduNetdiskPlatformCode, started.AttemptID,
	)
	if err != nil || pending.Status != ConnectorOAuthPending || pending.Connected {
		t.Fatalf("pending status = %#v, %v", pending, err)
	}

	active, err := service.CompleteMCPPlatformOAuth(
		context.Background(), baiduNetdiskPlatformCode, state, "authorization-code", "",
	)
	if err != nil || !active.Connected || active.Status != ConnectorOAuthActive {
		t.Fatalf("active status = %#v, %v", active, err)
	}
	if tokenCalls != 1 {
		t.Fatalf("token calls = %d, want 1", tokenCalls)
	}
	if _, err := service.CompleteMCPPlatformOAuth(
		context.Background(), baiduNetdiskPlatformCode, state, "authorization-code", "",
	); err == nil {
		t.Fatal("replayed callback was accepted")
	}

	plugin, err := infradb.GetPluginByPublicID(context.Background(), database, 7, active.PluginID)
	if err != nil || plugin == nil || plugin.CurrentRevision != 3 {
		t.Fatalf("plugin = %#v, %v", plugin, err)
	}
	revision, err := infradb.GetCurrentPluginRevision(context.Background(), database, plugin)
	if err != nil || revision == nil {
		t.Fatalf("current revision = %#v, %v", revision, err)
	}
	mcp, err := MCPFromDefinition(revision.Definition)
	if err != nil || mcp == nil || mcp.Transport != "sse" {
		t.Fatalf("MCP definition = %#v, %v", mcp, err)
	}
	parsedMCPURL, _ := url.Parse(mcp.URL)
	if parsedMCPURL.Query().Get("access_token") != "access-secret" {
		t.Fatalf("MCP URL = %q", mcp.URL)
	}
	detail, err := service.GetPlugin(context.Background(), 7, 9, plugin.PublicID, nil)
	if err != nil {
		t.Fatalf("GetPlugin() error = %v", err)
	}
	var safeDefinition ConnectorDefinition
	if err := json.Unmarshal(detail.Definition, &safeDefinition); err != nil {
		t.Fatalf("decode safe definition: %v", err)
	}
	if len(safeDefinition.Auth.Values) != 0 || safeDefinition.Auth.OAuth.StateHash != "" ||
		strings.Contains(string(detail.Definition), "access-secret") ||
		strings.Contains(string(detail.Definition), "refresh-secret") {
		t.Fatalf("detail leaked OAuth secrets: %s", detail.Definition)
	}
}

func TestBaiduNetdiskOAuthRefreshPublishesSuccessorRevision(t *testing.T) {
	database := setupPluginServiceTestDB(t)
	now := time.Date(2026, 8, 3, 2, 0, 0, 0, time.UTC)
	channel := &types.MCPChannel{
		Channel: baiduNetdiskPlatformCode, Name: "百度网盘", Transport: "sse",
		URL: "https://mcp-pan.baidu.com/sse", AuthType: types.MCPChannelAuthTypeOAuth,
		AuthConfig: types.MCPChannelAuthConfigJSON{
			Handler: baiduNetdiskPlatformCode,
			OAuth: &types.MCPChannelOAuthConfig{
				AppKey: "app-key", SecretKey: "secret-key", RedirectURI: "https://leros.example.com/callback",
				Scopes: []string{"basic", "netdisk"},
			},
			Bindings: types.MCPChannelAuthBindings{
				MCPQuery: map[string]string{"access_token": baiduNetdiskOAuthValueKey},
			},
		},
		Status: types.MCPChannelStatusActive,
	}
	if err := database.Create(channel).Error; err != nil {
		t.Fatalf("create channel: %v", err)
	}
	definition, err := json.Marshal(ConnectorDefinition{
		Schema: "connector/v1", Channel: baiduNetdiskPlatformCode, Mode: ConnectorModeMCPOnly,
		MCP: &MCPDefinition{
			Schema: "mcp/v1", Transport: "sse", Name: baiduNetdiskPlatformCode,
			URL: "https://mcp-pan.baidu.com/sse",
		},
		Auth: ConnectorAuthDefinition{
			Type: types.MCPChannelAuthTypeOAuth,
			Bindings: types.MCPChannelAuthBindings{
				MCPQuery: map[string]string{"access_token": baiduNetdiskOAuthValueKey},
			},
			Values: map[string]string{
				baiduNetdiskOAuthValueKey:   "old-access",
				baiduNetdiskRefreshValueKey: "old-refresh",
			},
			OAuth: &ConnectorOAuthDefinition{
				Status: ConnectorOAuthActive, AttemptID: "oauth_attempt",
				TokenExpiresAt: now.Add(5 * time.Minute), Scopes: []string{"basic", "netdisk"},
			},
		},
	})
	if err != nil {
		t.Fatalf("encode connector: %v", err)
	}
	plugin := &types.Plugin{
		PublicID: "plugin_baidu_refresh", OwnerScope: types.OwnerScopeOrganization, OrgID: 7,
		Code: "baidu-refresh", Kind: "mcp", Name: "百度网盘", Status: types.PluginStatusActive,
		Origin: "org", CurrentRevision: 1, CreatedBy: 9, UpdatedBy: 9,
	}
	if err := database.Create(plugin).Error; err != nil {
		t.Fatalf("create plugin: %v", err)
	}
	if err := database.Create(&types.PluginRevision{
		PluginID: plugin.ID, Revision: 1, Status: "published", Definition: definition,
		PublishedByType: "user", PublishedByID: 9, PublishedAt: now,
	}).Error; err != nil {
		t.Fatalf("create revision: %v", err)
	}
	client := baidunetdisk.NewClient(&http.Client{Transport: oauthRoundTripFunc(func(request *http.Request) (*http.Response, error) {
		query := request.URL.Query()
		if query.Get("grant_type") != "refresh_token" || query.Get("refresh_token") != "old-refresh" {
			t.Fatalf("refresh request = %s", request.URL.String())
		}
		return &http.Response{
			StatusCode: http.StatusOK,
			Header:     make(http.Header),
			Body: io.NopCloser(strings.NewReader(
				`{"access_token":"new-access","refresh_token":"new-refresh","expires_in":2592000,"scope":"basic netdisk"}`,
			)),
		}, nil
	})})
	service := &pluginService{
		db: database,
		oauth: &connectorOAuthManager{baidu: client, now: func() time.Time {
			return now
		}},
	}
	usable, changed, err := service.refreshMCPPlatformOAuth(context.Background(), 7, plugin.PublicID)
	if err != nil || !usable || !changed {
		t.Fatalf("refresh result = usable=%t changed=%t error=%v", usable, changed, err)
	}
	if err := database.First(plugin, plugin.ID).Error; err != nil || plugin.CurrentRevision != 2 {
		t.Fatalf("plugin revision = %d, %v", plugin.CurrentRevision, err)
	}
	revision, err := infradb.GetCurrentPluginRevision(context.Background(), database, plugin)
	if err != nil || revision == nil {
		t.Fatalf("current revision = %#v, %v", revision, err)
	}
	mcp, err := MCPFromDefinition(revision.Definition)
	if err != nil || mcp == nil {
		t.Fatalf("MCPFromDefinition() = %#v, %v", mcp, err)
	}
	parsed, _ := url.Parse(mcp.URL)
	if parsed.Query().Get("access_token") != "new-access" {
		t.Fatalf("refreshed MCP URL = %q", mcp.URL)
	}
}
