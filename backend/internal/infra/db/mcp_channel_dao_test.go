package db

import (
	"context"
	"testing"

	"gorm.io/driver/sqlite"
	"gorm.io/gorm"

	"github.com/insmtx/Leros/backend/types"
)

func setupMCPChannelDAOTestDB(t *testing.T) *gorm.DB {
	t.Helper()
	database, err := gorm.Open(sqlite.Open("file:"+t.Name()+"?mode=memory&cache=shared"), &gorm.Config{})
	if err != nil {
		t.Fatalf("open database: %v", err)
	}
	if err := database.AutoMigrate(&types.MCPChannel{}); err != nil {
		t.Fatalf("migrate MCP channel: %v", err)
	}
	return database
}

func TestMCPChannelSchemaAndReadQueries(t *testing.T) {
	database := setupMCPChannelDAOTestDB(t)
	if !database.Migrator().HasTable(types.TableNameMCPChannel) {
		t.Fatalf("table %s was not created", types.TableNameMCPChannel)
	}
	if !database.Migrator().HasIndex(&types.MCPChannel{}, "ux_mcp_channel_channel") {
		t.Fatal("MCP channel unique index was not created")
	}

	records := []types.MCPChannel{
		{
			Channel: "zeta", Name: "Zeta", Transport: "http", URL: "https://zeta.example.com/mcp",
			Headers: types.MCPChannelHeaders{"X-Channel": "zeta"}, Status: types.MCPChannelStatusActive,
		},
		{
			Channel: "alpha", Name: "Alpha", Transport: "http", URL: "https://alpha.example.com/mcp",
			Headers: types.MCPChannelHeaders{}, Status: types.MCPChannelStatusActive,
		},
		{
			Channel: "inactive", Name: "Inactive", Transport: "http", URL: "https://inactive.example.com/mcp",
			Headers: types.MCPChannelHeaders{}, Status: types.MCPChannelStatusInactive,
		},
		{
			Channel: "deleted", Name: "Deleted", Transport: "http", URL: "https://deleted.example.com/mcp",
			Headers: types.MCPChannelHeaders{}, Status: types.MCPChannelStatusActive,
		},
	}
	for index := range records {
		if err := database.Create(&records[index]).Error; err != nil {
			t.Fatalf("create channel %d: %v", index, err)
		}
	}
	if err := database.Delete(&records[3]).Error; err != nil {
		t.Fatalf("soft delete channel: %v", err)
	}
	if err := database.Create(&types.MCPChannel{
		Channel: "alpha", Name: "Duplicate", Transport: "http", URL: "https://example.com/mcp",
		Status: types.MCPChannelStatusActive,
	}).Error; err == nil {
		t.Fatal("duplicate channel should violate the unique index")
	}

	channels, err := ListActiveMCPChannels(context.Background(), database)
	if err != nil {
		t.Fatalf("ListActiveMCPChannels() error = %v", err)
	}
	if len(channels) != 2 || channels[0].Channel != "alpha" || channels[1].Channel != "zeta" {
		t.Fatalf("active channels = %#v", channels)
	}
	if channels[1].Headers["X-Channel"] != "zeta" {
		t.Fatalf("channel headers = %#v", channels[1].Headers)
	}

	channel, err := GetActiveMCPChannelByChannel(context.Background(), database, "zeta")
	if err != nil || channel == nil || channel.URL != "https://zeta.example.com/mcp" {
		t.Fatalf("GetActiveMCPChannelByChannel() channel/error = %#v/%v", channel, err)
	}
	inactive, err := GetActiveMCPChannelByChannel(context.Background(), database, "inactive")
	if err != nil || inactive != nil {
		t.Fatalf("inactive channel/error = %#v/%v", inactive, err)
	}
}
