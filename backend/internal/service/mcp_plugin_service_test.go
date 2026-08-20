package service

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"regexp"
	"testing"

	"github.com/insmtx/Leros/backend/internal/api/contract"
	infradb "github.com/insmtx/Leros/backend/internal/infra/db"
	"github.com/insmtx/Leros/backend/types"
	mcpsdk "github.com/mark3labs/mcp-go/mcp"
	mcpserver "github.com/mark3labs/mcp-go/server"
)

func TestMCPPluginCreateUpdateAndDetail(t *testing.T) {
	database := setupPluginServiceTestDB(t)
	service := NewPluginService(database, NewSkillDisplayTranslationService(database))
	ctx := context.Background()

	created, err := service.AddMCPPlugin(ctx, 10, 20, &contract.AddMCPPluginRequest{
		MCPPluginConfig: contract.MCPPluginConfig{
			Code:        "docs",
			Name:        "Docs",
			Description: "Organization docs",
			URL:         "https://example.com/mcp",
			BearerToken: "docs-secret",
			Headers:     map[string]string{"Authorization": "Bearer secret"},
		},
	})
	if err != nil {
		t.Fatalf("AddMCPPlugin() error = %v", err)
	}
	if created.Kind != "mcp" || created.CurrentRevision != 1 || created.Status != "active" {
		t.Fatalf("created plugin = %#v", created)
	}

	detail, err := service.GetPlugin(ctx, 10, 20, created.PublicID, &contract.GetPluginRequest{})
	if err != nil {
		t.Fatalf("GetPlugin() error = %v", err)
	}
	var definition MCPDefinition
	if err := json.Unmarshal(detail.Definition, &definition); err != nil {
		t.Fatalf("decode definition: %v", err)
	}
	if definition.Name != "docs" || definition.BearerToken != "docs-secret" ||
		definition.Headers["Authorization"] != "Bearer secret" {
		t.Fatalf("definition = %#v", definition)
	}

	updated, err := service.UpdateMCPPlugin(ctx, 10, 20, created.PublicID, &contract.UpdateMCPPluginRequest{
		MCPPluginConfig: contract.MCPPluginConfig{
			Name:    "Docs v2",
			URL:     "https://example.com/v2/mcp",
			Headers: map[string]string{"X-Tenant": "two"},
		},
	})
	if err != nil {
		t.Fatalf("UpdateMCPPlugin() error = %v", err)
	}
	if updated.CurrentRevision != 2 || updated.Name != "Docs v2" ||
		updated.Code != "docs" || updated.Description != "Organization docs" {
		t.Fatalf("updated plugin = %#v", updated)
	}
	if _, err := service.UpdateMCPPlugin(ctx, 10, 20, created.PublicID, &contract.UpdateMCPPluginRequest{
		MCPPluginConfig: contract.MCPPluginConfig{
			Code: "other", Name: "Docs v3", URL: "https://example.com/v3/mcp",
		},
	}); !errors.Is(err, contract.ErrInvalidPluginConfig) {
		t.Fatalf("change-code UpdateMCPPlugin() error = %v", err)
	}
	revisions, err := infradb.ListPluginRevisions(ctx, database, 10, created.PublicID)
	if err != nil {
		t.Fatalf("ListPluginRevisions() error = %v", err)
	}
	if len(revisions) != 2 || revisions[0].PublishedByID != 20 ||
		revisions[0].PublishedByType != "user" {
		t.Fatalf("revisions = %#v", revisions)
	}

	if _, err := service.GetPlugin(ctx, 11, 20, created.PublicID, &contract.GetPluginRequest{}); !errors.Is(
		err,
		contract.ErrPluginNotFound,
	) {
		t.Fatalf("cross-org GetPlugin() error = %v", err)
	}
	if _, err := service.GetPlugin(ctx, 10, 21, created.PublicID, &contract.GetPluginRequest{}); !errors.Is(
		err,
		contract.ErrPluginNotFound,
	) {
		t.Fatalf("cross-user GetPlugin() error = %v", err)
	}
	if _, err := service.UpdateMCPPlugin(ctx, 10, 21, created.PublicID, &contract.UpdateMCPPluginRequest{
		MCPPluginConfig: contract.MCPPluginConfig{
			Name: "Other user", URL: "https://example.com/other-user/mcp",
		},
	}); !errors.Is(err, contract.ErrPluginNotFound) {
		t.Fatalf("cross-user UpdateMCPPlugin() error = %v", err)
	}
}

func TestMCPPluginCreateGeneratesStableCode(t *testing.T) {
	database := setupPluginServiceTestDB(t)
	service := NewPluginService(database, NewSkillDisplayTranslationService(database))
	ctx := context.Background()

	first, err := service.AddMCPPlugin(ctx, 10, 20, &contract.AddMCPPluginRequest{
		MCPPluginConfig: contract.MCPPluginConfig{
			Name: "知识库", URL: "https://example.com/knowledge/mcp",
		},
	})
	if err != nil {
		t.Fatalf("first AddMCPPlugin() error = %v", err)
	}
	second, err := service.AddMCPPlugin(ctx, 10, 20, &contract.AddMCPPluginRequest{
		MCPPluginConfig: contract.MCPPluginConfig{
			Name: "知识库", URL: "https://example.com/other/mcp",
		},
	})
	if err != nil {
		t.Fatalf("second AddMCPPlugin() error = %v", err)
	}
	codePattern := regexp.MustCompile(`^mcp-[0-9a-f]{32}$`)
	if !codePattern.MatchString(first.Code) || !codePattern.MatchString(second.Code) {
		t.Fatalf("generated codes = %q, %q", first.Code, second.Code)
	}
	if first.Code == second.Code || first.Code == "leros" {
		t.Fatalf("generated codes must be unique and non-reserved: %q, %q", first.Code, second.Code)
	}
	detail, err := service.GetPlugin(ctx, 10, 20, first.PublicID, &contract.GetPluginRequest{})
	if err != nil {
		t.Fatalf("GetPlugin() error = %v", err)
	}
	var definition MCPDefinition
	if err := json.Unmarshal(detail.Definition, &definition); err != nil {
		t.Fatalf("decode generated-code definition: %v", err)
	}
	if definition.Name != first.Code {
		t.Fatalf("definition name = %q, want %q", definition.Name, first.Code)
	}

	updated, err := service.UpdateMCPPlugin(ctx, 10, 20, first.PublicID, &contract.UpdateMCPPluginRequest{
		MCPPluginConfig: contract.MCPPluginConfig{
			Name: "新名称", URL: "https://example.com/knowledge/v2/mcp",
		},
	})
	if err != nil {
		t.Fatalf("UpdateMCPPlugin() error = %v", err)
	}
	if updated.Code != first.Code {
		t.Fatalf("updated code = %q, want %q", updated.Code, first.Code)
	}
}

func TestMCPPluginCreateAndUpdateStdioDefinition(t *testing.T) {
	database := setupPluginServiceTestDB(t)
	service := NewPluginService(database, NewSkillDisplayTranslationService(database))
	ctx := context.Background()

	created, err := service.AddMCPPlugin(ctx, 10, 20, &contract.AddMCPPluginRequest{
		MCPPluginConfig: contract.MCPPluginConfig{
			Code:      "sqlite",
			Name:      "SQLite",
			Transport: "stdio",
			Command:   "openai-dev-mcp",
			Args:      []string{"serve-sqlite", "--readonly"},
			Env:       map[string]string{"DATABASE_URL": "sqlite:///workspace/data.db"},
		},
	})
	if err != nil {
		t.Fatalf("AddMCPPlugin() error = %v", err)
	}
	detail, err := service.GetPlugin(ctx, 10, 20, created.PublicID, &contract.GetPluginRequest{})
	if err != nil {
		t.Fatalf("GetPlugin() error = %v", err)
	}
	var definition MCPDefinition
	if err := json.Unmarshal(detail.Definition, &definition); err != nil {
		t.Fatalf("decode definition: %v", err)
	}
	if definition.Transport != "stdio" || definition.Command != "openai-dev-mcp" ||
		len(definition.Args) != 2 || definition.Env["DATABASE_URL"] == "" ||
		definition.URL != "" || len(definition.Headers) != 0 {
		t.Fatalf("definition = %#v", definition)
	}

	updated, err := service.UpdateMCPPlugin(ctx, 10, 20, created.PublicID, &contract.UpdateMCPPluginRequest{
		MCPPluginConfig: contract.MCPPluginConfig{
			Name:      "SQLite HTTP",
			Transport: "http",
			URL:       "https://example.com/sqlite/mcp",
		},
	})
	if err != nil {
		t.Fatalf("UpdateMCPPlugin() error = %v", err)
	}
	if updated.CurrentRevision != 2 {
		t.Fatalf("updated plugin = %#v", updated)
	}
}

func TestMCPPluginsAreVisibleAndGloballyManagedOnlyByCreator(t *testing.T) {
	database := setupPluginServiceTestDB(t)
	service := NewPluginService(database, NewSkillDisplayTranslationService(database))
	ctx := context.Background()

	mine, err := service.AddMCPPlugin(ctx, 10, 20, &contract.AddMCPPluginRequest{
		MCPPluginConfig: contract.MCPPluginConfig{
			Name: "Mine", URL: "https://example.com/mine/mcp",
		},
	})
	if err != nil {
		t.Fatalf("create mine: %v", err)
	}
	other, err := service.AddMCPPlugin(ctx, 10, 21, &contract.AddMCPPluginRequest{
		MCPPluginConfig: contract.MCPPluginConfig{
			Name: "Other", URL: "https://example.com/other/mcp",
		},
	})
	if err != nil {
		t.Fatalf("create other: %v", err)
	}
	if err := database.Create(&types.Plugin{
		PublicID: "plugin_shared_skill", OwnerScope: types.OwnerScopeOrganization,
		OrgID: 10, Code: "shared-skill", Kind: "skill", Name: "Shared Skill",
		Visibility: types.PluginVisibilityPublic, Status: types.PluginStatusActive, Origin: "org", CreatedBy: 21, UpdatedBy: 21,
	}).Error; err != nil {
		t.Fatalf("create shared skill: %v", err)
	}

	list, err := service.ListPlugins(ctx, 10, 20, &contract.ListPluginsRequest{
		Status: types.PluginStatusActive,
	})
	if err != nil {
		t.Fatalf("ListPlugins() error = %v", err)
	}
	visible := make(map[string]bool, len(list.Plugins))
	for _, plugin := range list.Plugins {
		visible[plugin.PublicID] = true
	}
	if !visible[mine.PublicID] || !visible["plugin_shared_skill"] || visible[other.PublicID] {
		t.Fatalf("visible plugins = %#v", list.Plugins)
	}

	mcps, err := service.ListPlugins(ctx, 10, 20, &contract.ListPluginsRequest{
		Kind: "mcp", Status: types.PluginStatusActive,
	})
	if err != nil || len(mcps.Plugins) != 1 || mcps.Plugins[0].PublicID != mine.PublicID {
		t.Fatalf("personal MCP list = %#v, %v", mcps, err)
	}
	if _, err := service.ListPluginVersions(ctx, 10, 20, other.PublicID); !errors.Is(
		err,
		contract.ErrPluginNotFound,
	) {
		t.Fatalf("cross-user ListPluginVersions() error = %v", err)
	}
	statusRequest := &contract.GetPluginInstallationStatusRequest{Kind: "mcp", Code: other.Code}
	hiddenStatus, err := service.GetPluginInstallationStatus(ctx, 10, 20, statusRequest)
	if err != nil || hiddenStatus.Installed {
		t.Fatalf("cross-user installation status = %#v, %v", hiddenStatus, err)
	}
	ownerStatus, err := service.GetPluginInstallationStatus(ctx, 10, 21, statusRequest)
	if err != nil || !ownerStatus.Installed || ownerStatus.PluginID != other.PublicID {
		t.Fatalf("owner installation status = %#v, %v", ownerStatus, err)
	}
	if _, err := service.DeletePlugin(
		ctx,
		10,
		20,
		other.PublicID,
		&contract.DeletePluginRequest{},
	); !errors.Is(err, contract.ErrPluginNotFound) {
		t.Fatalf("cross-user DeletePlugin() error = %v", err)
	}
	if _, err := service.GetPlugin(
		ctx,
		10,
		21,
		other.PublicID,
		&contract.GetPluginRequest{},
	); err != nil {
		t.Fatalf("owner GetPlugin() after rejected delete error = %v", err)
	}
}

func TestMCPPluginConnectionTestReturnsToolCount(t *testing.T) {
	server := mcpserver.NewMCPServer("test", "1.0.0")
	server.AddTool(
		mcpsdk.NewTool("search", mcpsdk.WithDescription("Search documents")),
		func(context.Context, mcpsdk.CallToolRequest) (*mcpsdk.CallToolResult, error) {
			return mcpsdk.NewToolResultText("ok"), nil
		},
	)
	streamableServer := mcpserver.NewStreamableHTTPServer(server)
	httpServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		if req.Header.Get("Authorization") != "Bearer test-token" {
			http.Error(w, "unauthorized", http.StatusUnauthorized)
			return
		}
		streamableServer.ServeHTTP(w, req)
	}))
	defer httpServer.Close()

	database := setupPluginServiceTestDB(t)
	service := NewPluginService(database, NewSkillDisplayTranslationService(database))
	result, err := service.TestMCPPlugin(context.Background(), &contract.TestMCPPluginRequest{
		URL:         httpServer.URL,
		BearerToken: "test-token",
	})
	if err != nil {
		t.Fatalf("TestMCPPlugin() error = %v", err)
	}
	if !result.OK || result.ToolCount != 1 {
		t.Fatalf("TestMCPPlugin() result = %#v", result)
	}
}

func TestMCPPluginValidationAndDuplicateCode(t *testing.T) {
	database := setupPluginServiceTestDB(t)
	service := NewPluginService(database, NewSkillDisplayTranslationService(database))
	ctx := context.Background()

	for name, config := range map[string]contract.MCPPluginConfig{
		"reserved": {
			Code: "leros", Name: "Reserved", URL: "https://example.com/mcp",
		},
		"invalid url": {
			Code: "docs", Name: "Docs", URL: "file:///tmp/mcp",
		},
		"invalid header": {
			Code: "docs", Name: "Docs", URL: "https://example.com/mcp",
			Headers: map[string]string{"Bad Header": "value"},
		},
		"invalid bearer token": {
			Code: "docs", Name: "Docs", URL: "https://example.com/mcp",
			BearerToken: "bad\ntoken",
		},
		"missing stdio command": {
			Code: "docs", Name: "Docs", Transport: "stdio",
		},
		"invalid stdio env name": {
			Code: "docs", Name: "Docs", Transport: "stdio", Command: "mcp-server",
			Env: map[string]string{"BAD-NAME": "value"},
		},
	} {
		t.Run(name, func(t *testing.T) {
			_, err := service.AddMCPPlugin(ctx, 10, 20, &contract.AddMCPPluginRequest{
				MCPPluginConfig: config,
			})
			if !errors.Is(err, contract.ErrInvalidPluginConfig) {
				t.Fatalf("AddMCPPlugin() error = %v", err)
			}
		})
	}

	request := &contract.AddMCPPluginRequest{MCPPluginConfig: contract.MCPPluginConfig{
		Code: "docs", Name: "Docs", URL: "https://example.com/mcp",
	}}
	if _, err := service.AddMCPPlugin(ctx, 10, 20, request); err != nil {
		t.Fatalf("first AddMCPPlugin() error = %v", err)
	}
	if _, err := service.AddMCPPlugin(ctx, 10, 20, request); !errors.Is(
		err,
		contract.ErrInvalidPluginConfig,
	) {
		t.Fatalf("duplicate AddMCPPlugin() error = %v", err)
	}
	if _, err := service.AddMCPPlugin(ctx, 11, 20, request); err != nil {
		t.Fatalf("cross-org AddMCPPlugin() error = %v", err)
	}
}
