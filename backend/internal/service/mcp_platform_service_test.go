package service

import (
	"context"
	"net/http"
	"net/http/httptest"
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
		Headers:     types.MCPChannelHeaders{"X-CoreKG-Channel": "database"},
		Status:      types.MCPChannelStatusActive,
	}
	if err := database.Create(channel).Error; err != nil {
		t.Fatalf("create MCP channel: %v", err)
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
		if req.Header.Get("X-CoreKG-Channel") != "database" {
			http.Error(w, "missing configured header", http.StatusBadRequest)
			return
		}
		streamableServer.ServeHTTP(w, req)
	}))
	defer httpServer.Close()

	database := setupPluginServiceTestDB(t)
	channel := createTestMCPChannel(t, database, httpServer.URL)
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
	definition, err := MCPFromDefinition(revision.Definition)
	if err != nil {
		t.Fatalf("MCPFromDefinition() error = %v", err)
	}
	if definition.Provider != "corekg" ||
		definition.URL != httpServer.URL ||
		definition.BearerToken != "yg-corekg-test" ||
		definition.Headers["X-CoreKG-Channel"] != "database" {
		t.Fatalf("CoreKG definition = %#v", definition)
	}

	otherUser, err := service.ListMCPPlatforms(context.Background(), 10, 21)
	if err != nil {
		t.Fatalf("other user ListMCPPlatforms() error = %v", err)
	}
	if otherUser.Platforms[0].Connected {
		t.Fatalf("other user platform = %#v", otherUser.Platforms[0])
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
	createTestMCPChannel(t, database, httpServer.URL)
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
