package service

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"

	infradb "github.com/insmtx/Leros/backend/internal/infra/db"
	skilllinks "github.com/insmtx/Leros/backend/internal/skill/links"
	"github.com/insmtx/Leros/backend/types"
)

const builtinConnectorOrigin = "builtin_connector"

var builtinConnectorSyncMu sync.Mutex

// SyncBuiltinConnectorTemplates seeds built-in channels and publishes their immutable templates.
// Bundled templates are published before activation so operations can enable a channel without restarting the server.
// One invalid connector is isolated from the remaining synchronization pass.
func SyncBuiltinConnectorTemplates(
	ctx context.Context,
	database *gorm.DB,
	sourceDir string,
) (*BuiltinSkillSyncReport, error) {
	builtinConnectorSyncMu.Lock()
	defer builtinConnectorSyncMu.Unlock()

	if database == nil {
		return nil, fmt.Errorf("database is required")
	}
	emailChannel, err := seedBuiltinEmailChannel(ctx, database)
	if err != nil {
		return nil, err
	}
	baiduNetdiskChannel, err := seedBuiltinBaiduNetdiskChannel(ctx, database)
	if err != nil {
		return nil, err
	}
	channels, err := infradb.ListActiveMCPChannels(ctx, database)
	if err != nil {
		return nil, err
	}
	channelsByCode := make(map[string]types.MCPChannel, len(channels)+2)
	for index := range channels {
		channelsByCode[channels[index].Channel] = channels[index]
	}
	channelsByCode[emailChannel.Channel] = *emailChannel
	channelsByCode[baiduNetdiskChannel.Channel] = *baiduNetdiskChannel
	codes := make([]string, 0, len(channelsByCode))
	for code := range channelsByCode {
		codes = append(codes, code)
	}
	sort.Strings(codes)
	var connectorDir string
	if strings.TrimSpace(sourceDir) != "" {
		connectorDir = sourceDir
	} else {
		connectorDir, err = skilllinks.ResolveBuiltinSkillsSource("", "connectors")
		if err != nil {
			return nil, err
		}
	}
	report := &BuiltinSkillSyncReport{}
	service := &pluginService{db: database}
	for _, code := range codes {
		configured := channelsByCode[code]
		channel, ok := normalizeBuiltinConnectorTemplateChannel(&configured)
		if !ok {
			continue
		}
		report.Scanned++
		operation, syncErr := service.syncBuiltinConnectorTemplate(ctx, connectorDir, channel)
		if syncErr != nil {
			report.Failures = append(report.Failures, BuiltinSkillSyncFailure{Code: channel.Channel, Err: syncErr})
			continue
		}
		switch operation {
		case "created":
			report.Created++
		case "updated":
			report.Updated++
		case "restored":
			report.Restored++
		default:
			report.Unchanged++
		}
	}
	return report, nil
}

func seedBuiltinBaiduNetdiskChannel(ctx context.Context, database *gorm.DB) (*types.MCPChannel, error) {
	const channelCode = baiduNetdiskPlatformCode
	var existing types.MCPChannel
	err := database.WithContext(ctx).Where("channel = ?", channelCode).First(&existing).Error
	if err == nil {
		return &existing, nil
	}
	if !errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, err
	}
	channel := &types.MCPChannel{
		Channel: channelCode, Name: "百度网盘",
		Description: "通过百度网盘 MCP 浏览、检索、整理和分享云端文件",
		SkillCode:   "connector-baidu-netdisk", Transport: "sse",
		URL: "https://mcp-pan.baidu.com/sse", AuthType: types.MCPChannelAuthTypeOAuth,
		AuthConfig: types.MCPChannelAuthConfigJSON{
			Handler: channelCode,
			OAuth:   &types.MCPChannelOAuthConfig{Scopes: []string{"basic", "netdisk"}},
			Bindings: types.MCPChannelAuthBindings{
				MCPQuery: map[string]string{"access_token": baiduNetdiskOAuthValueKey},
			},
		},
		Status: types.MCPChannelStatusInactive,
	}
	if err := database.WithContext(ctx).Create(channel).Error; err != nil {
		var concurrent types.MCPChannel
		if reloadErr := database.WithContext(ctx).Where("channel = ?", channelCode).First(&concurrent).Error; reloadErr == nil {
			return &concurrent, nil
		}
		return nil, fmt.Errorf("seed baidu netdisk connector channel: %w", err)
	}
	return channel, nil
}

func seedBuiltinEmailChannel(ctx context.Context, database *gorm.DB) (*types.MCPChannel, error) {
	const channelCode = "netease-mail"
	var existing types.MCPChannel
	err := database.WithContext(ctx).Where("channel = ?", channelCode).First(&existing).Error
	if err == nil {
		return &existing, nil
	}
	if !errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, err
	}
	channel := &types.MCPChannel{
		Channel:     channelCode,
		Name:        "邮箱",
		Description: "通过标准 IMAP/SMTP 收发、搜索和管理邮箱邮件及附件",
		SkillCode:   "connector-netease-mail",
		AuthType:    types.MCPChannelAuthTypeForm,
		AuthConfig: types.MCPChannelAuthConfigJSON{
			Fields: []types.MCPChannelAuthField{
				{
					Key: "email", Label: "邮箱地址", Type: "text", Required: true,
					Placeholder: "yourname@163.com",
					Description: "完整邮箱地址，例如 xxx@163.com、xxx@126.com 或 xxx@yeah.net",
				},
				{
					Key: "authorization_code", Label: "IMAP/SMTP 授权码", Type: "password", Required: true,
					Placeholder: "在邮箱设置中生成的客户端授权码",
					Description: "开启 IMAP/SMTP 后生成的授权码，不是网页登录密码",
				},
			},
			Bindings: types.MCPChannelAuthBindings{
				SkillEnv: map[string]string{
					"NETEASE_EMAIL_USER": "email",
					"NETEASE_EMAIL_PASS": "authorization_code",
				},
			},
		},
		Status: types.MCPChannelStatusActive,
	}
	if err := database.WithContext(ctx).Create(channel).Error; err != nil {
		var concurrent types.MCPChannel
		if reloadErr := database.WithContext(ctx).Where("channel = ?", channelCode).First(&concurrent).Error; reloadErr == nil {
			return &concurrent, nil
		}
		return nil, fmt.Errorf("seed email connector channel: %w", err)
	}
	return channel, nil
}

func (s *pluginService) syncBuiltinConnectorTemplate(
	ctx context.Context,
	connectorDir string,
	channel *types.MCPChannel,
) (string, error) {
	var prepared *preparedSkillPackage
	if channel.SkillCode != "" {
		var err error
		prepared, err = packageBuiltinSkillDirectory(filepath.Join(connectorDir, channel.SkillCode))
		if err != nil {
			return "", err
		}
		if prepared.Manifest.Name != channel.SkillCode {
			return "", fmt.Errorf("SKILL.md name %q must match directory %q", prepared.Manifest.Name, channel.SkillCode)
		}
	}

	operation := "unchanged"
	err := s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var skill *ConnectorSkillDefinition
		if prepared != nil {
			file, err := storeSystemSkillArtifact(ctx, tx, channel.SkillCode, prepared.Archive, prepared.SHA256)
			if err != nil {
				return err
			}
			skill = &ConnectorSkillDefinition{
				Code: channel.SkillCode,
				Artifact: &ArtifactDefinition{
					FileUploadID: file.PublicID,
					SHA256:       prepared.SHA256,
					SizeBytes:    file.FileSize,
					ContentType:  "application/zip",
				},
			}
		}
		definition := connectorTemplateDefinition(channel, skill)

		var plugin types.Plugin
		find := tx.Unscoped().Clauses(clause.Locking{Strength: "UPDATE"}).
			Where("owner_scope = ? AND org_id = ? AND kind = ? AND code = ?",
				types.OwnerScopeSystem, 0, "mcp", channel.Channel).
			Order("id DESC").First(&plugin)
		if find.Error != nil && !errors.Is(find.Error, gorm.ErrRecordNotFound) {
			return find.Error
		}
		created := errors.Is(find.Error, gorm.ErrRecordNotFound)
		restored := false
		if created {
			plugin = types.Plugin{
				PublicID: "plugin_" + uuid.NewString(), OwnerScope: types.OwnerScopeSystem,
				Code: channel.Channel, Kind: "mcp", Name: channel.Name,
				Description: channel.Description, Status: types.PluginStatusActive,
				Origin: builtinConnectorOrigin,
			}
			insert := tx.WithContext(ctx).Clauses(clause.OnConflict{DoNothing: true}).Create(&plugin)
			if insert.Error != nil {
				return insert.Error
			}
			if insert.RowsAffected == 0 {
				if err := tx.Unscoped().Clauses(clause.Locking{Strength: "UPDATE"}).
					Where("owner_scope = ? AND org_id = ? AND kind = ? AND code = ?",
						types.OwnerScopeSystem, 0, "mcp", channel.Channel).
					Order("id DESC").First(&plugin).Error; err != nil {
					return fmt.Errorf("reload concurrently created connector template: %w", err)
				}
				created = false
			}
		}
		if !created {
			if plugin.Origin != builtinConnectorOrigin {
				return fmt.Errorf("connector channel %q conflicts with system plugin origin %q", channel.Channel, plugin.Origin)
			}
			if plugin.DeletedAt.Valid || plugin.Status == types.PluginStatusArchived {
				if err := infradb.RestorePlugin(ctx, tx, plugin.ID, 0); err != nil {
					return err
				}
				restored = true
			}
			if err := tx.Model(&types.Plugin{}).Where("id = ?", plugin.ID).
				Select("name", "description", "status").
				Updates(types.Plugin{
					Name: channel.Name, Description: channel.Description, Status: types.PluginStatusActive,
				}).Error; err != nil {
				return err
			}
		}

		current, err := infradb.GetCurrentPluginRevision(ctx, tx, &plugin)
		if err != nil {
			return err
		}
		if skill != nil && current != nil {
			skill.Revision = current.Revision
			definition = connectorTemplateDefinition(channel, skill)
		}
		if !restored && current != nil && bytes.Equal(current.Definition, definition) {
			operation = "unchanged"
			return nil
		}
		nextRevision := 1
		if current != nil && current.Revision >= nextRevision {
			nextRevision = current.Revision + 1
		}
		if skill != nil {
			skill.Revision = nextRevision
			definition = connectorTemplateDefinition(channel, skill)
		}
		if err := ValidatePluginDefinition("mcp", definition); err != nil {
			return err
		}
		revision := &types.PluginRevision{
			PluginID: plugin.ID, Revision: nextRevision, Status: "published",
			Definition: definition, PublishedByType: "system", PublishedAt: time.Now(),
		}
		if err := infradb.CreatePluginRevision(ctx, tx, revision); err != nil {
			return err
		}
		if prepared != nil {
			if err := infradb.CreatePluginRevisionContent(ctx, tx, prepared.Content.model(revision.ID)); err != nil {
				return err
			}
		}
		if err := infradb.SetPluginCurrentRevision(ctx, tx, plugin.ID, uint(nextRevision), 0); err != nil {
			return err
		}
		switch {
		case created:
			operation = "created"
		case restored:
			operation = "restored"
		default:
			operation = "updated"
		}
		return nil
	})
	return operation, err
}

func connectorTemplateDefinition(
	channel *types.MCPChannel,
	skill *ConnectorSkillDefinition,
) json.RawMessage {
	var mcp *MCPDefinition
	if channel.Transport != "" {
		mcp = &MCPDefinition{
			Schema: "mcp/v1", Transport: channel.Transport, Name: channel.Channel,
			Provider: channel.Channel, URL: channel.URL, Headers: cloneStringMap(map[string]string(channel.Headers)),
		}
	}
	mode := ConnectorModeMCPOnly
	if skill != nil && mcp == nil {
		mode = ConnectorModeSkillOnly
	} else if skill != nil {
		mode = ConnectorModeHybrid
	}
	auth := ConnectorAuthDefinition{
		Type: channel.AuthType, Bindings: types.MCPChannelAuthConfig(channel.AuthConfig).Bindings,
	}
	if channel.AuthType == types.MCPChannelAuthTypeOAuth {
		auth.OAuth = &ConnectorOAuthDefinition{Status: ConnectorOAuthPending}
	}
	encoded, _ := json.Marshal(ConnectorDefinition{
		Schema: "connector/v1", Channel: channel.Channel, Mode: mode,
		Auth:  auth,
		Skill: skill, MCP: mcp,
	})
	return encoded
}
