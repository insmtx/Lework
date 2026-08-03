package service

import (
	"context"
	"path/filepath"
	"testing"

	"github.com/insmtx/Leros/backend/internal/api/contract"
	infradb "github.com/insmtx/Leros/backend/internal/infra/db"
	skilllinks "github.com/insmtx/Leros/backend/internal/skill/links"
	"github.com/insmtx/Leros/backend/types"
)

func TestBuiltinEmailConnectorPackage(t *testing.T) {
	sourceDir, err := skilllinks.ResolveBuiltinSkillsSource("", "connectors")
	if err != nil {
		t.Fatalf("resolve connector source: %v", err)
	}
	prepared, err := packageBuiltinSkillDirectory(
		filepath.Join(sourceDir, "connector-netease-mail"),
	)
	if err != nil {
		t.Fatalf("package email connector: %v", err)
	}
	if prepared.Manifest.Name != "connector-netease-mail" || prepared.SHA256 == "" ||
		prepared.Content == nil || len(prepared.Content.FileIndex) != 3 {
		t.Fatalf("email connector package = %#v", prepared)
	}
}

func TestBuiltinBaiduNetdiskConnectorPackage(t *testing.T) {
	sourceDir, err := skilllinks.ResolveBuiltinSkillsSource("", "connectors")
	if err != nil {
		t.Fatalf("resolve connector source: %v", err)
	}
	prepared, err := packageBuiltinSkillDirectory(
		filepath.Join(sourceDir, "connector-baidu-netdisk"),
	)
	if err != nil {
		t.Fatalf("package Baidu Netdisk connector: %v", err)
	}
	if prepared.Manifest.Name != "connector-baidu-netdisk" || prepared.SHA256 == "" ||
		prepared.Content == nil || len(prepared.Content.FileIndex) != 1 {
		t.Fatalf("Baidu Netdisk connector package = %#v", prepared)
	}
}

func TestEmailConnectorTemplateConnectAndResolveSkill(t *testing.T) {
	database := setupPluginServiceTestDB(t)
	setupPluginServiceTestStorage(t)
	sourceDir := t.TempDir()
	writeBuiltinSkillTestFiles(
		t,
		sourceDir,
		"connector-netease-mail",
		"Mail connector",
		"Use the injected mail credentials.",
	)
	writeBuiltinSkillTestFiles(
		t,
		sourceDir,
		"connector-baidu-netdisk",
		"Baidu Netdisk connector",
		"Use the Baidu Netdisk connector.",
	)

	report, err := SyncBuiltinConnectorTemplates(context.Background(), database, sourceDir)
	if err != nil {
		t.Fatalf("sync connector templates: %v", err)
	}
	if report.Scanned != 2 || report.Created != 2 || len(report.Failures) != 0 {
		t.Fatalf("sync report = %#v", report)
	}

	var channel types.MCPChannel
	if err := database.Where("channel = ?", "netease-mail").First(&channel).Error; err != nil {
		t.Fatalf("load email channel: %v", err)
	}
	if channel.SkillCode != "connector-netease-mail" ||
		channel.AuthType != types.MCPChannelAuthTypeForm ||
		len(types.MCPChannelAuthConfig(channel.AuthConfig).Fields) != 2 {
		t.Fatalf("email channel = %#v", channel)
	}
	systemPlugin, err := infradb.GetSystemPluginByCode(
		context.Background(),
		database,
		"mcp",
		"netease-mail",
	)
	if err != nil || systemPlugin == nil || systemPlugin.Origin != builtinConnectorOrigin {
		t.Fatalf("system connector = %#v, %v", systemPlugin, err)
	}

	pluginService := &pluginService{db: database}
	connected, err := pluginService.ConnectMCPPlatform(
		context.Background(),
		7,
		9,
		"netease-mail",
		&contract.ConnectMCPPlatformRequest{AuthValues: map[string]string{
			"email":              "user@example.com",
			"authorization_code": "client-authorization-code",
		}},
	)
	if err != nil {
		t.Fatalf("connect email platform: %v", err)
	}
	if connected.ToolCount != 0 || connected.Platform.Mode != ConnectorModeSkillOnly ||
		connected.Plugin.PublicID == "" {
		t.Fatalf("connected response = %#v", connected)
	}
	orgPlugin, err := infradb.GetPluginByPublicID(
		context.Background(),
		database,
		7,
		connected.Plugin.PublicID,
	)
	if err != nil || orgPlugin == nil {
		t.Fatalf("organization connector = %#v, %v", orgPlugin, err)
	}
	revision, err := infradb.GetCurrentPluginRevision(context.Background(), database, orgPlugin)
	if err != nil || revision == nil || revision.SourcePluginRevisionID == nil {
		t.Fatalf("organization revision = %#v, %v", revision, err)
	}
	definition, err := ConnectorFromDefinition(revision.Definition)
	if err != nil || definition == nil || definition.Skill == nil ||
		definition.Auth.Values["email"] != "user@example.com" ||
		definition.Auth.Values["authorization_code"] != "client-authorization-code" {
		t.Fatalf("connector definition = %#v, %v", definition, err)
	}

	downloads, err := pluginService.ResolveSkillDownloadURLs(
		context.Background(),
		7,
		types.CallerKindWorker,
		99,
		&contract.ResolveSkillDownloadURLsRequest{
			ConnectorSkills: []contract.ConnectorSkillRef{
				{PluginID: orgPlugin.PublicID, Revision: revision.Revision},
			},
		},
	)
	if err != nil {
		t.Fatalf("resolve connector Skill: %v", err)
	}
	if len(downloads.Skills) != 1 ||
		downloads.Skills[0].Code != "connector-netease-mail" ||
		downloads.Skills[0].Revision != definition.Skill.Revision ||
		downloads.Skills[0].DownloadURL == "" {
		t.Fatalf("connector downloads = %#v", downloads.Skills)
	}
}

func TestEmailConnectorRequiresEveryAuthorizationField(t *testing.T) {
	database := setupPluginServiceTestDB(t)
	setupPluginServiceTestStorage(t)
	sourceDir := t.TempDir()
	writeBuiltinSkillTestFiles(t, sourceDir, "connector-netease-mail", "Mail connector", "Mail.")
	writeBuiltinSkillTestFiles(
		t,
		sourceDir,
		"connector-baidu-netdisk",
		"Baidu Netdisk connector",
		"Use the Baidu Netdisk connector.",
	)
	if _, err := SyncBuiltinConnectorTemplates(context.Background(), database, sourceDir); err != nil {
		t.Fatalf("sync connector templates: %v", err)
	}

	_, err := (&pluginService{db: database}).ConnectMCPPlatform(
		context.Background(),
		7,
		9,
		"netease-mail",
		&contract.ConnectMCPPlatformRequest{
			AuthValues: map[string]string{"email": "user@example.com"},
		},
	)
	if err == nil {
		t.Fatal("connect email platform expected missing authorization code error")
	}
}

func TestInactiveBaiduNetdiskTemplateIsPublishedBeforeActivation(t *testing.T) {
	database := setupPluginServiceTestDB(t)
	setupPluginServiceTestStorage(t)
	sourceDir := t.TempDir()
	writeBuiltinSkillTestFiles(t, sourceDir, "connector-netease-mail", "Mail connector", "Mail.")
	writeBuiltinSkillTestFiles(
		t,
		sourceDir,
		"connector-baidu-netdisk",
		"Baidu Netdisk connector",
		"Use the Baidu Netdisk connector.",
	)

	report, err := SyncBuiltinConnectorTemplates(context.Background(), database, sourceDir)
	if err != nil || report.Scanned != 2 || report.Created != 2 || len(report.Failures) != 0 {
		t.Fatalf("sync report = %#v, %v", report, err)
	}
	var channel types.MCPChannel
	if err := database.Where("channel = ?", baiduNetdiskPlatformCode).First(&channel).Error; err != nil {
		t.Fatalf("load Baidu Netdisk channel: %v", err)
	}
	if channel.Status != types.MCPChannelStatusInactive {
		t.Fatalf("Baidu Netdisk channel status = %q", channel.Status)
	}
	template, err := infradb.GetSystemPluginByCode(
		context.Background(), database, "mcp", baiduNetdiskPlatformCode,
	)
	if err != nil || template == nil || template.CurrentRevision != 1 {
		t.Fatalf("Baidu Netdisk template = %#v, %v", template, err)
	}
	secondReport, err := SyncBuiltinConnectorTemplates(context.Background(), database, sourceDir)
	if err != nil || secondReport.Scanned != 2 || secondReport.Unchanged != 2 ||
		secondReport.Created != 0 || len(secondReport.Failures) != 0 {
		t.Fatalf("second sync report = %#v, %v", secondReport, err)
	}
	if err := database.First(template, template.ID).Error; err != nil || template.CurrentRevision != 1 {
		t.Fatalf("Baidu Netdisk template revision after second sync = %d, %v", template.CurrentRevision, err)
	}

	pluginService := &pluginService{db: database}
	platforms, err := pluginService.ListMCPPlatforms(context.Background(), 7, 9)
	if err != nil {
		t.Fatalf("list MCP platforms: %v", err)
	}
	for _, platform := range platforms.Platforms {
		if platform.Code == baiduNetdiskPlatformCode {
			t.Fatal("inactive Baidu Netdisk channel was exposed")
		}
	}

	config := types.MCPChannelAuthConfig(channel.AuthConfig)
	config.OAuth = &types.MCPChannelOAuthConfig{
		AppKey: "app-key", SecretKey: "secret-key",
		RedirectURI: "https://leros.example.com/v1/plugins/mcp/oauth/baidu-netdisk/callback",
		Scopes:      []string{"basic", "netdisk"},
	}
	if err := database.Model(&types.MCPChannel{}).
		Where("channel = ?", baiduNetdiskPlatformCode).
		Updates(map[string]any{
			"auth_config": types.MCPChannelAuthConfigJSON(config),
			"status":      types.MCPChannelStatusActive,
		}).Error; err != nil {
		t.Fatalf("activate Baidu Netdisk channel: %v", err)
	}
	started, err := pluginService.StartMCPPlatformOAuth(
		context.Background(), 7, 9, baiduNetdiskPlatformCode,
	)
	if err != nil || started == nil || started.AuthorizationURL == "" {
		t.Fatalf("start Baidu Netdisk OAuth = %#v, %v", started, err)
	}
}
