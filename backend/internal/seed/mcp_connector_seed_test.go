package seed

import (
	"context"
	"testing"

	"gorm.io/driver/sqlite"
	"gorm.io/gorm"

	"github.com/insmtx/Lework/backend/config"
	"github.com/insmtx/Lework/backend/internal/infra/filestore"
	"github.com/insmtx/Lework/backend/types"
)

func setupConnectorSeedTestDB(t *testing.T) *gorm.DB {
	t.Helper()
	database, err := gorm.Open(sqlite.Open("file:"+t.Name()+"?mode=memory&cache=shared"), &gorm.Config{})
	if err != nil {
		t.Fatalf("open database: %v", err)
	}
	if err := database.AutoMigrate(
		&types.Plugin{}, &types.PluginRevision{}, &types.PluginRevisionContent{},
		&types.MCPChannel{}, &types.FileUpload{},
	); err != nil {
		t.Fatalf("migrate connector seed models: %v", err)
	}
	if err := filestore.Init(&config.StorageConfig{
		Driver: "local", LocalDir: t.TempDir(), Bucket: "connector-seed-test",
		BaseURL: "http://127.0.0.1:8080/storage", SignSecret: "connector-seed-test-secret",
	}); err != nil {
		t.Fatalf("initialize connector seed storage: %v", err)
	}
	return database
}

func TestSyncConfiguredMCPConnectorsEmptyConfigIsNoOp(t *testing.T) {
	database := setupConnectorSeedTestDB(t)
	report, err := SyncConfiguredMCPConnectors(context.Background(), database, "", nil)
	if err != nil {
		t.Fatalf("sync configured connectors: %v", err)
	}
	if report.Scanned != 0 || report.Created != 0 || report.Updated != 0 || len(report.Failures) != 0 {
		t.Fatalf("sync report = %#v", report)
	}
	var channelCount int64
	if err := database.Model(&types.MCPChannel{}).Count(&channelCount).Error; err != nil {
		t.Fatalf("count channels: %v", err)
	}
	if channelCount != 0 {
		t.Fatalf("empty config created %d channels", channelCount)
	}
}

func TestSyncConfiguredMCPConnectorsAppliesConfigAndIsolatesFailures(t *testing.T) {
	database := setupConnectorSeedTestDB(t)
	if err := database.Create(&types.MCPChannel{
		Channel: "unlisted", Name: "Unlisted", Transport: "http", URL: "https://unlisted.example/mcp",
		Status: types.MCPChannelStatusActive,
	}).Error; err != nil {
		t.Fatalf("create unlisted channel: %v", err)
	}
	configured := []types.MCPConnectorSpec{
		{
			Channel: "netease-mail", Name: "Configured Mail",
			Description: "Configured description", Status: types.MCPChannelStatusActive,
			SkillCode: "connector-netease-mail", AuthType: types.MCPChannelAuthTypeForm,
			AuthConfig: types.MCPChannelAuthConfig{
				Fields: []types.MCPChannelAuthField{{Key: "email", Label: "Email", Type: "text", Required: true},
					{Key: "authorization_code", Label: "Code", Type: "password", Required: true}},
				Bindings: types.MCPChannelAuthBindings{SkillEnv: map[string]string{
					"NETEASE_EMAIL_USER": "email", "NETEASE_EMAIL_PASS": "authorization_code",
				}},
			},
		},
		{Channel: "broken", Name: "Broken", SkillCode: "missing-skill", Status: types.MCPChannelStatusActive},
	}
	report, err := SyncConfiguredMCPConnectors(context.Background(), database, "", configured)
	if err != nil {
		t.Fatalf("sync configured connectors: %v", err)
	}
	if len(report.Failures) != 1 || report.Failures[0].Code != "broken" {
		t.Fatalf("sync failures = %#v", report.Failures)
	}
	var channel types.MCPChannel
	if err := database.Where("channel = ?", "netease-mail").First(&channel).Error; err != nil {
		t.Fatalf("load configured mail channel: %v", err)
	}
	if channel.Name != "Configured Mail" || channel.Description != "Configured description" {
		t.Fatalf("configured mail channel = %#v", channel)
	}
	var brokenCount int64
	if err := database.Model(&types.MCPChannel{}).Where("channel = ?", "broken").Count(&brokenCount).Error; err != nil {
		t.Fatalf("count broken channel: %v", err)
	}
	if brokenCount != 0 {
		t.Fatalf("broken channel must roll back, count=%d", brokenCount)
	}
	var unlisted types.MCPChannel
	if err := database.Where("channel = ?", "unlisted").First(&unlisted).Error; err != nil {
		t.Fatalf("load unlisted channel: %v", err)
	}
	if unlisted.Name != "Unlisted" || unlisted.URL != "https://unlisted.example/mcp" {
		t.Fatalf("unlisted channel changed = %#v", unlisted)
	}
	var unlistedTemplateCount int64
	if err := database.Model(&types.Plugin{}).
		Where("owner_scope = ? AND org_id = ? AND kind = ? AND code = ?", types.OwnerScopeSystem, 0, "mcp", "unlisted").
		Count(&unlistedTemplateCount).Error; err != nil {
		t.Fatalf("count unlisted template: %v", err)
	}
	if unlistedTemplateCount != 0 {
		t.Fatalf("unlisted channel template was synchronized, count=%d", unlistedTemplateCount)
	}
}
