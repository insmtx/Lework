package service

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"gorm.io/gorm"

	"github.com/insmtx/Leros/backend/internal/adapter/account"
	"github.com/insmtx/Leros/backend/internal/api/contract"
	infradb "github.com/insmtx/Leros/backend/internal/infra/db"
	"github.com/insmtx/Leros/backend/types"
	mcpsdk "github.com/mark3labs/mcp-go/mcp"
	mcpserver "github.com/mark3labs/mcp-go/server"
)

type mcpPlatformAPIKeyIssuer struct {
	input account.CreateAPIKeyInput
	calls int
}

type staticManagedCredentialProvider struct {
	values  map[string]string
	request ManagedCredentialRequest
	calls   int
}

func (p *staticManagedCredentialProvider) Resolve(_ context.Context, request ManagedCredentialRequest) (map[string]string, error) {
	p.request = request
	p.calls++
	return p.values, nil
}

func TestManagedMCPConnectorUsesConfiguredCredentialProvider(t *testing.T) {
	const apiKey = "test-demo-managed-key"
	server := mcpserver.NewMCPServer("demo-managed-test", "1.0.0")
	server.AddTool(mcpsdk.NewTool("page_roster"), func(context.Context, mcpsdk.CallToolRequest) (*mcpsdk.CallToolResult, error) {
		return mcpsdk.NewToolResultText("ok"), nil
	})
	transport := mcpserver.NewStreamableHTTPServer(server)
	httpServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		if req.Header.Get("Authorization") != "Bearer "+apiKey {
			http.Error(w, "unauthorized", http.StatusUnauthorized)
			return
		}
		transport.ServeHTTP(w, req)
	}))
	defer httpServer.Close()

	database := setupPluginServiceTestDB(t)
	_, err := SyncSystemConnectorTemplate(context.Background(), database, "", types.MCPConnectorSpec{
		Channel: "demo", Name: "Demo", Status: types.MCPChannelStatusActive,
		Transport: "http", URL: httpServer.URL, AuthType: types.MCPChannelAuthTypeManaged,
		AuthConfig: types.MCPChannelAuthConfig{
			Handler:  "demo",
			Bindings: types.MCPChannelAuthBindings{MCPHeaders: map[string]string{"Authorization": "Bearer {{api_key}}"}},
		},
	})
	if err != nil {
		t.Fatalf("SyncSystemConnectorTemplate() error = %v", err)
	}
	provider := &staticManagedCredentialProvider{values: map[string]string{"api_key": apiKey}}
	credentials := NewManagedCredentialService(map[string]ManagedCredentialProvider{"demo": provider})
	pluginSvc := NewPluginServiceWithManagedCredentials(database, credentials, nil, nil)
	connected, err := pluginSvc.ConnectMCPPlatform(context.Background(), 10, 20, "demo", nil)
	if err != nil {
		t.Fatalf("ConnectMCPPlatform() error = %v", err)
	}
	if connected.ToolCount != 1 || provider.request.ConnectorCode != "demo" ||
		provider.request.ConnectorName != "Demo" || provider.request.Purpose != "mcp_connector" || provider.calls != 1 {
		t.Fatalf("connected=%#v provider_request=%#v", connected, provider.request)
	}

	plugin, err := infradb.GetPluginByPublicID(context.Background(), database, 10, connected.Plugin.PublicID)
	if err != nil || plugin == nil {
		t.Fatalf("GetPluginByPublicID() plugin/error = %#v/%v", plugin, err)
	}
	revision, err := infradb.GetCurrentPluginRevision(context.Background(), database, plugin)
	if err != nil || revision == nil {
		t.Fatalf("GetCurrentPluginRevision() revision/error = %#v/%v", revision, err)
	}
	var legacy ConnectorDefinition
	if err := json.Unmarshal(revision.Definition, &legacy); err != nil {
		t.Fatal(err)
	}
	legacy.Auth.Type = types.MCPChannelAuthTypeNone
	legacy.Auth.Values = nil
	legacyDefinition, err := json.Marshal(legacy)
	if err != nil {
		t.Fatal(err)
	}
	if err := database.Model(&types.PluginRevision{}).Where("id = ?", revision.ID).Update("definition", legacyDefinition).Error; err != nil {
		t.Fatalf("downgrade connector revision fixture: %v", err)
	}
	migrated, err := pluginSvc.ConnectMCPPlatform(context.Background(), 10, 20, "demo", nil)
	if err != nil {
		t.Fatalf("ConnectMCPPlatform() migrate old revision error = %v", err)
	}
	if migrated.Plugin.CurrentRevision != 2 || migrated.ToolCount != 1 || provider.calls != 2 {
		t.Fatalf("migrated connector=%#v provider calls=%d", migrated, provider.calls)
	}
	repeated, err := pluginSvc.ConnectMCPPlatform(context.Background(), 10, 20, "demo", nil)
	if err != nil {
		t.Fatalf("ConnectMCPPlatform() idempotent call error = %v", err)
	}
	if repeated.Plugin.CurrentRevision != 2 || provider.calls != 2 {
		t.Fatalf("repeated connector=%#v provider calls=%d", repeated, provider.calls)
	}
}

func (i *mcpPlatformAPIKeyIssuer) CreateAPIKey(
	_ context.Context,
	input account.CreateAPIKeyInput,
) (*account.CreatedAPIKey, error) {
	i.input = input
	i.calls++
	return &account.CreatedAPIKey{ID: 9, APIKey: "yg-corekg-test"}, nil
}

func createTestMCPChannel(t *testing.T, database *gorm.DB, url string) *types.MCPChannel {
	t.Helper()
	channel := &types.MCPChannel{
		Channel:     coreKGPlatformCode,
		Name:        "CoreKG",
		Description: "CoreKG configured from database",
		Transport:   "http",
		URL:         url,
		Headers:     types.MCPChannelHeaders{"X-Connector-Scope": "database"},
		Status:      types.MCPChannelStatusActive,
		AuthType:    types.MCPChannelAuthTypeManaged,
		AuthConfig: types.MCPChannelAuthConfigJSON(types.MCPChannelAuthConfig{
			Handler: coreKGPlatformCode,
			Bindings: types.MCPChannelAuthBindings{MCPHeaders: map[string]string{
				"Authorization": "Bearer {{api_key}}",
			}},
		}),
	}
	if err := database.Create(channel).Error; err != nil {
		t.Fatalf("create MCP channel: %v", err)
	}
	return channel
}

func syncTestCoreKGConnectorTemplate(t *testing.T, database *gorm.DB, url string) *types.MCPChannel {
	t.Helper()
	operation, err := SyncSystemConnectorTemplate(context.Background(), database, "", types.MCPConnectorSpec{
		Channel: coreKGPlatformCode, Name: "CoreKG", Description: "CoreKG configured from database",
		Status: types.MCPChannelStatusActive, Transport: "http", URL: url,
		Headers:  types.MCPChannelHeaders{"X-Connector-Scope": "database"},
		AuthType: types.MCPChannelAuthTypeManaged,
		AuthConfig: types.MCPChannelAuthConfig{
			Handler: coreKGPlatformCode,
			Bindings: types.MCPChannelAuthBindings{MCPHeaders: map[string]string{
				"Authorization": "Bearer {{api_key}}",
			}},
		},
	})
	if err != nil || operation != "created" {
		t.Fatalf("sync CoreKG connector = %q, %v", operation, err)
	}
	channel, err := infradb.GetActiveMCPChannelByChannel(context.Background(), database, coreKGPlatformCode)
	if err != nil || channel == nil {
		t.Fatalf("load synced CoreKG channel = %#v, %v", channel, err)
	}
	return channel
}

func TestCoreKGMCPPlatformConnectIsUserScopedAndIdempotent(t *testing.T) {
	server := mcpserver.NewMCPServer("corekg-test", "1.0.0")
	server.AddTool(
		mcpsdk.NewTool("search", mcpsdk.WithDescription("Search knowledge")),
		func(context.Context, mcpsdk.CallToolRequest) (*mcpsdk.CallToolResult, error) {
			return mcpsdk.NewToolResultText("ok"), nil
		},
	)
	streamableServer := mcpserver.NewStreamableHTTPServer(server)
	httpServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		if req.Header.Get("Authorization") != "Bearer yg-corekg-test" {
			http.Error(w, "unauthorized", http.StatusUnauthorized)
			return
		}
		if req.Header.Get("X-Connector-Scope") != "database" {
			http.Error(w, "missing configured header", http.StatusBadRequest)
			return
		}
		streamableServer.ServeHTTP(w, req)
	}))
	defer httpServer.Close()

	database := setupPluginServiceTestDB(t)
	channel := syncTestCoreKGConnectorTemplate(t, database, httpServer.URL)
	issuer := &mcpPlatformAPIKeyIssuer{}
	service := &pluginService{db: database, apiKeyIssuer: issuer}

	before, err := service.ListMCPPlatforms(context.Background(), 10, 20)
	if err != nil {
		t.Fatalf("ListMCPPlatforms() before connect error = %v", err)
	}
	if len(before.Platforms) != 1 || before.Platforms[0].Connected || !before.Platforms[0].AutoConnectSupported {
		t.Fatalf("platform before connect = %#v", before.Platforms)
	}
	if before.Platforms[0].Name != "CoreKG" ||
		before.Platforms[0].Description != "CoreKG configured from database" {
		t.Fatalf("platform metadata = %#v", before.Platforms[0])
	}

	connected, err := service.ConnectMCPPlatform(context.Background(), 10, 20, "corekg", nil)
	if err != nil {
		t.Fatalf("ConnectMCPPlatform() error = %v", err)
	}
	if !connected.Platform.Connected || connected.ToolCount != 1 || connected.Plugin.PublicID == "" {
		t.Fatalf("connected response = %#v", connected)
	}
	if issuer.calls != 1 ||
		issuer.input.Name != "SingerOS CoreKG MCP" ||
		issuer.input.Purpose != "mcp_connector" ||
		issuer.input.ResourceType != "mcp" ||
		issuer.input.ResourceID != 0 ||
		issuer.input.ExpireHours != 0 {
		t.Fatalf("issuer calls/input = %d/%#v", issuer.calls, issuer.input)
	}

	if err := database.Model(channel).Updates(map[string]interface{}{
		"name": "CoreKG Updated",
		"url":  "https://new.example.com/mcp",
	}).Error; err != nil {
		t.Fatalf("update MCP channel: %v", err)
	}
	repeated, err := service.ConnectMCPPlatform(context.Background(), 10, 20, "COREKG", nil)
	if err != nil {
		t.Fatalf("repeated ConnectMCPPlatform() error = %v", err)
	}
	if repeated.Plugin.PublicID != connected.Plugin.PublicID || issuer.calls != 1 {
		t.Fatalf("repeated response/calls = %#v/%d", repeated, issuer.calls)
	}
	if repeated.Platform.Name != "CoreKG Updated" {
		t.Fatalf("repeated platform metadata = %#v", repeated.Platform)
	}

	plugin, err := infradb.GetPluginByPublicID(context.Background(), database, 10, connected.Plugin.PublicID)
	if err != nil || plugin == nil {
		t.Fatalf("GetPluginByPublicID() plugin/error = %#v/%v", plugin, err)
	}
	revision, err := infradb.GetCurrentPluginRevision(context.Background(), database, plugin)
	if err != nil || revision == nil {
		t.Fatalf("GetCurrentPluginRevision() revision/error = %#v/%v", revision, err)
	}
	template, err := infradb.GetSystemPluginByCode(context.Background(), database, "mcp", coreKGPlatformCode)
	if err != nil || template == nil {
		t.Fatalf("CoreKG system template = %#v, %v", template, err)
	}
	templateRevision, err := infradb.GetCurrentPluginRevision(context.Background(), database, template)
	if err != nil || templateRevision == nil {
		t.Fatalf("CoreKG template revision = %#v, %v", templateRevision, err)
	}
	if revision.SourcePluginRevisionID == nil || *revision.SourcePluginRevisionID != templateRevision.ID {
		t.Fatalf("CoreKG source revision = %#v, want %d", revision.SourcePluginRevisionID, templateRevision.ID)
	}
	definition, err := MCPFromDefinition(revision.Definition)
	if err != nil {
		t.Fatalf("MCPFromDefinition() error = %v", err)
	}
	if definition.Provider != "corekg" ||
		definition.URL != httpServer.URL ||
		definition.BearerToken != "" ||
		definition.Headers["Authorization"] != "Bearer yg-corekg-test" ||
		definition.Headers["X-Connector-Scope"] != "database" {
		t.Fatalf("CoreKG definition = %#v", definition)
	}
	if strings.Contains(string(revision.Definition), "mcp_bearer_token") {
		t.Fatalf("CoreKG revision uses removed bearer shortcut: %s", revision.Definition)
	}

	otherUser, err := service.ListMCPPlatforms(context.Background(), 10, 21)
	if err != nil {
		t.Fatalf("other user ListMCPPlatforms() error = %v", err)
	}
	if otherUser.Platforms[0].Connected {
		t.Fatalf("other user platform = %#v", otherUser.Platforms[0])
	}
}

func TestCatAPIMCPPlatformConnectUsesBearerHeaderBinding(t *testing.T) {
	server := mcpserver.NewMCPServer("catapi-test", "1.0.0")
	server.AddTool(
		mcpsdk.NewTool("search", mcpsdk.WithDescription("Search API market")),
		func(context.Context, mcpsdk.CallToolRequest) (*mcpsdk.CallToolResult, error) {
			return mcpsdk.NewToolResultText("ok"), nil
		},
	)
	streamableServer := mcpserver.NewStreamableHTTPServer(server)
	httpServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		if req.Header.Get("Authorization") != "Bearer catapi-secret" {
			http.Error(w, "unauthorized", http.StatusUnauthorized)
			return
		}
		streamableServer.ServeHTTP(w, req)
	}))
	defer httpServer.Close()

	database := setupPluginServiceTestDB(t)
	operation, err := SyncSystemConnectorTemplate(context.Background(), database, "", types.MCPConnectorSpec{
		Channel: "catapi", Name: "CatAPI", Description: "CatAPI API market",
		Status: types.MCPChannelStatusActive, Transport: "http",
		URL: httpServer.URL, AuthType: types.MCPChannelAuthTypeForm,
		AuthConfig: types.MCPChannelAuthConfig{
			Description: "输入 CatAPI API Key，连接后即可使用 API 市场中的工具和服务。",
			Fields: []types.MCPChannelAuthField{{
				Key: "api_key", Label: "API Key", Type: "password", Required: true,
			}},
			Bindings: types.MCPChannelAuthBindings{MCPHeaders: map[string]string{
				"Authorization": "Bearer {{api_key}}",
			}},
		},
	})
	if err != nil || operation != "created" {
		t.Fatalf("sync CatAPI connector = %q, %v", operation, err)
	}
	service := &pluginService{db: database}
	connected, err := service.ConnectMCPPlatform(context.Background(), 10, 20, "catapi", &contract.ConnectMCPPlatformRequest{
		AuthValues: map[string]string{"api_key": "catapi-secret"},
	})
	if err != nil {
		t.Fatalf("ConnectMCPPlatform() error = %v", err)
	}
	if connected.Platform.AuthDescription != "输入 CatAPI API Key，连接后即可使用 API 市场中的工具和服务。" {
		t.Fatalf("CatAPI platform auth description = %q", connected.Platform.AuthDescription)
	}
	tested, err := service.TestMCPPlatform(context.Background(), 10, 20, "catapi")
	if err != nil || tested == nil || !tested.OK || tested.ToolCount != 1 {
		t.Fatalf("TestMCPPlatform() = %#v, %v", tested, err)
	}
	plugin, err := infradb.GetPluginByPublicID(context.Background(), database, 10, connected.Plugin.PublicID)
	if err != nil || plugin == nil {
		t.Fatalf("CatAPI plugin = %#v, %v", plugin, err)
	}
	revision, err := infradb.GetCurrentPluginRevision(context.Background(), database, plugin)
	if err != nil || revision == nil {
		t.Fatalf("CatAPI revision = %#v, %v", revision, err)
	}
	mcp, err := MCPFromDefinition(revision.Definition)
	if err != nil || mcp == nil || mcp.BearerToken != "" ||
		mcp.Headers["Authorization"] != "Bearer catapi-secret" {
		t.Fatalf("CatAPI MCP definition = %#v, %v", mcp, err)
	}
}

func TestMCPListEnsuresCoreKGConnection(t *testing.T) {
	server := mcpserver.NewMCPServer("corekg-list-test", "1.0.0")
	server.AddTool(
		mcpsdk.NewTool("search", mcpsdk.WithDescription("Search knowledge")),
		func(context.Context, mcpsdk.CallToolRequest) (*mcpsdk.CallToolResult, error) {
			return mcpsdk.NewToolResultText("ok"), nil
		},
	)
	streamableServer := mcpserver.NewStreamableHTTPServer(server)
	httpServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		streamableServer.ServeHTTP(w, req)
	}))
	defer httpServer.Close()

	issuer := &mcpPlatformAPIKeyIssuer{}
	database := setupPluginServiceTestDB(t)
	syncTestCoreKGConnectorTemplate(t, database, httpServer.URL)
	service := &pluginService{
		db:           database,
		apiKeyIssuer: issuer,
	}
	request := &contract.ListPluginsRequest{Kind: "mcp", Status: "active"}
	first, err := service.ListPlugins(context.Background(), 10, 20, request)
	if err != nil {
		t.Fatalf("ListPlugins() first error = %v", err)
	}
	if len(first.Plugins) != 1 || first.Plugins[0].Code != coreKGPluginCode(10, 20) {
		t.Fatalf("first plugins = %#v", first.Plugins)
	}
	second, err := service.ListPlugins(context.Background(), 10, 20, request)
	if err != nil {
		t.Fatalf("ListPlugins() second error = %v", err)
	}
	if len(second.Plugins) != 1 || issuer.calls != 1 {
		t.Fatalf("second plugins/issuer calls = %#v/%d", second.Plugins, issuer.calls)
	}
}

func TestCoreKGMCPPlatformIsUnavailableWithoutIAMIssuer(t *testing.T) {
	database := setupPluginServiceTestDB(t)
	createTestMCPChannel(t, database, "https://example.com/mcp")
	service := NewPluginService(database, NewSkillDisplayTranslationService(database))
	platforms, err := service.ListMCPPlatforms(context.Background(), 10, 20)
	if err != nil {
		t.Fatalf("ListMCPPlatforms() error = %v", err)
	}
	if platforms.Platforms[0].AutoConnectSupported {
		t.Fatalf("platform = %#v", platforms.Platforms[0])
	}
	if _, err := service.ConnectMCPPlatform(context.Background(), 10, 20, "corekg", nil); err == nil {
		t.Fatal("ConnectMCPPlatform() expected unsupported error")
	}
}

func TestCoreKGMCPPlatformRequiresSystemConnectorTemplate(t *testing.T) {
	database := setupPluginServiceTestDB(t)
	createTestMCPChannel(t, database, "https://example.com/mcp")
	issuer := &mcpPlatformAPIKeyIssuer{}
	service := &pluginService{db: database, apiKeyIssuer: issuer}

	_, err := service.ConnectMCPPlatform(context.Background(), 10, 20, coreKGPlatformCode, nil)
	if !errors.Is(err, contract.ErrInvalidPluginConfig) ||
		!strings.Contains(err.Error(), "connector template is not available") {
		t.Fatalf("ConnectMCPPlatform() error = %v, want unavailable template configuration error", err)
	}
	if issuer.calls != 0 {
		t.Fatalf("API key issuer calls = %d, want 0", issuer.calls)
	}
}

func TestMCPPlatformsSkipMissingInactiveAndInvalidChannels(t *testing.T) {
	database := setupPluginServiceTestDB(t)
	service := &pluginService{db: database, apiKeyIssuer: &mcpPlatformAPIKeyIssuer{}}

	empty, err := service.ListMCPPlatforms(context.Background(), 10, 20)
	if err != nil || len(empty.Platforms) != 0 {
		t.Fatalf("empty platforms/error = %#v/%v", empty, err)
	}
	if err := database.Create(&types.MCPChannel{
		Channel: "other", Name: "Other", Transport: "http", URL: "https://example.com/mcp",
		Status: types.MCPChannelStatusActive,
	}).Error; err != nil {
		t.Fatalf("create unknown channel: %v", err)
	}
	coreKG := &types.MCPChannel{
		Channel: coreKGPlatformCode, Name: "CoreKG", Transport: "http", URL: "https://example.com/mcp",
		Headers: types.MCPChannelHeaders{"Authorization": "must-not-be-stored"},
		Status:  types.MCPChannelStatusActive,
	}
	if err := database.Create(coreKG).Error; err != nil {
		t.Fatalf("create invalid CoreKG channel: %v", err)
	}
	skipped, err := service.ListMCPPlatforms(context.Background(), 10, 20)
	if err != nil || len(skipped.Platforms) != 1 || skipped.Platforms[0].Code != "other" {
		t.Fatalf("skipped platforms/error = %#v/%v", skipped, err)
	}
	if _, err := service.ListPlugins(
		context.Background(),
		10,
		20,
		&contract.ListPluginsRequest{Kind: "mcp", Status: "active"},
	); err != nil {
		t.Fatalf("invalid channel must not break plugin list: %v", err)
	}
	if err := database.Model(coreKG).Update("status", types.MCPChannelStatusInactive).Error; err != nil {
		t.Fatalf("deactivate CoreKG channel: %v", err)
	}
	if _, err := service.ConnectMCPPlatform(context.Background(), 10, 20, coreKGPlatformCode, nil); err == nil {
		t.Fatal("ConnectMCPPlatform() expected inactive configuration error")
	}
}

func TestCoreKGProviderSurvivesMCPUpdate(t *testing.T) {
	database := setupPluginServiceTestDB(t)
	service := NewPluginService(database, NewSkillDisplayTranslationService(database))
	created, err := service.AddMCPPlugin(context.Background(), 10, 20, &contract.AddMCPPluginRequest{
		MCPPluginConfig: contract.MCPPluginConfig{
			Code:     coreKGPluginCode(10, 20),
			Name:     "CoreKG",
			URL:      "https://example.com/mcp",
			Provider: "corekg",
		},
	})
	if err != nil {
		t.Fatalf("AddMCPPlugin() error = %v", err)
	}
	if _, err := service.UpdateMCPPlugin(context.Background(), 10, 20, created.PublicID, &contract.UpdateMCPPluginRequest{
		MCPPluginConfig: contract.MCPPluginConfig{
			Name: "CoreKG renamed",
			URL:  "https://example.com/mcp-v2",
		},
	}); err != nil {
		t.Fatalf("UpdateMCPPlugin() error = %v", err)
	}
	detail, err := service.GetPlugin(
		context.Background(),
		10,
		20,
		created.PublicID,
		&contract.GetPluginRequest{},
	)
	if err != nil {
		t.Fatalf("GetPlugin() error = %v", err)
	}
	definition, err := MCPFromDefinition(detail.Definition)
	if err != nil || definition.Provider != "corekg" {
		t.Fatalf("updated definition/error = %#v/%v", definition, err)
	}
}
