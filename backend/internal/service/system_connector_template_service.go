package service

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"path/filepath"
	"sync"
	"time"

	"github.com/google/uuid"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"

	infradb "github.com/insmtx/Lework/backend/internal/infra/db"
	"github.com/insmtx/Lework/backend/types"
)

const builtinConnectorOrigin = "builtin_connector"

var systemConnectorTemplateSyncMu sync.Mutex

// SyncSystemConnectorTemplate validates and publishes one system connector template atomically.
// The channel row, optional Skill artifact, Plugin, and immutable revision are updated together.
func SyncSystemConnectorTemplate(
	ctx context.Context,
	database *gorm.DB,
	connectorDir string,
	spec types.MCPConnectorSpec,
) (string, error) {
	systemConnectorTemplateSyncMu.Lock()
	defer systemConnectorTemplateSyncMu.Unlock()

	if database == nil {
		return "", fmt.Errorf("database is required")
	}
	channel, err := normalizeSystemConnectorSpec(spec)
	if err != nil {
		return "", err
	}
	prepared, err := prepareBuiltinConnectorSkill(connectorDir, channel)
	if err != nil {
		return "", err
	}
	var operation string
	err = database.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := infradb.UpsertMCPChannel(ctx, tx, channel); err != nil {
			return fmt.Errorf("upsert system MCP channel: %w", err)
		}
		var syncErr error
		operation, syncErr = syncSystemConnectorTemplateTx(ctx, tx, channel, prepared)
		return syncErr
	})
	return operation, err
}

func normalizeSystemConnectorSpec(spec types.MCPConnectorSpec) (*types.MCPChannel, error) {
	if spec.Status != types.MCPChannelStatusActive && spec.Status != types.MCPChannelStatusInactive {
		return nil, fmt.Errorf("configured MCP channel %q status must be active or inactive", spec.Channel)
	}
	channel := mcpChannelFromSpec(spec)
	normalized, ok := normalizeMCPChannel(channel, channel.Status == types.MCPChannelStatusActive)
	if !ok {
		return nil, fmt.Errorf("configured MCP channel %q is invalid", spec.Channel)
	}
	auth := types.MCPChannelAuthConfig(normalized.AuthConfig)
	if normalized.AuthType == types.MCPChannelAuthTypeOAuth && auth.Handler != baiduNetdiskPlatformCode {
		return nil, fmt.Errorf("oauth authorization only supports handler %q", baiduNetdiskPlatformCode)
	}
	return normalized, nil
}

func mcpChannelFromSpec(spec types.MCPConnectorSpec) *types.MCPChannel {
	return &types.MCPChannel{
		Channel:     spec.Channel,
		Name:        spec.Name,
		Description: spec.Description,
		Status:      spec.Status,
		SkillCode:   spec.SkillCode,
		Transport:   spec.Transport,
		URL:         spec.URL,
		Headers:     cloneMCPChannelHeaders(spec.Headers),
		AuthType:    spec.AuthType,
		AuthConfig:  types.MCPChannelAuthConfigJSON(cloneMCPAuthConfig(spec.AuthConfig)),
	}
}

func prepareBuiltinConnectorSkill(connectorDir string, channel *types.MCPChannel) (*preparedSkillPackage, error) {
	if channel.SkillCode == "" {
		return nil, nil
	}
	prepared, err := packageBuiltinSkillDirectory(filepath.Join(connectorDir, channel.SkillCode))
	if err != nil {
		return nil, err
	}
	if prepared.Manifest.Name != channel.SkillCode {
		return nil, fmt.Errorf("SKILL.md name %q must match directory %q", prepared.Manifest.Name, channel.SkillCode)
	}
	return prepared, nil
}

func syncSystemConnectorTemplateTx(
	ctx context.Context,
	tx *gorm.DB,
	channel *types.MCPChannel,
	prepared *preparedSkillPackage,
) (string, error) {
	operation := "unchanged"
	var skill *ConnectorSkillDefinition
	if prepared != nil {
		file, err := storeSystemSkillArtifact(ctx, tx, channel.SkillCode, prepared.Archive, prepared.SHA256)
		if err != nil {
			return "", err
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
		return "", find.Error
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
			return "", insert.Error
		}
		if insert.RowsAffected == 0 {
			if err := tx.Unscoped().Clauses(clause.Locking{Strength: "UPDATE"}).
				Where("owner_scope = ? AND org_id = ? AND kind = ? AND code = ?",
					types.OwnerScopeSystem, 0, "mcp", channel.Channel).
				Order("id DESC").First(&plugin).Error; err != nil {
				return "", fmt.Errorf("reload concurrently created connector template: %w", err)
			}
			created = false
		}
	}
	if !created {
		if plugin.Origin != builtinConnectorOrigin {
			return "", fmt.Errorf("connector channel %q conflicts with system plugin origin %q", channel.Channel, plugin.Origin)
		}
		if plugin.DeletedAt.Valid || plugin.Status == types.PluginStatusArchived {
			if err := infradb.RestorePlugin(ctx, tx, plugin.ID, 0); err != nil {
				return "", err
			}
			restored = true
		}
		if err := tx.Model(&types.Plugin{}).Where("id = ?", plugin.ID).
			Select("name", "description", "status").
			Updates(types.Plugin{
				Name: channel.Name, Description: channel.Description, Status: types.PluginStatusActive,
			}).Error; err != nil {
			return "", err
		}
	}

	current, err := infradb.GetCurrentPluginRevision(ctx, tx, &plugin)
	if err != nil {
		return "", err
	}
	if skill != nil && current != nil {
		skill.Revision = current.Revision
		definition = connectorTemplateDefinition(channel, skill)
	}
	if !restored && current != nil && bytes.Equal(current.Definition, definition) {
		return operation, nil
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
		return "", err
	}
	revision := &types.PluginRevision{
		PluginID: plugin.ID, Revision: nextRevision, Status: "published",
		Definition: definition, PublishedByType: "system", PublishedAt: time.Now(),
	}
	if err := infradb.CreatePluginRevision(ctx, tx, revision); err != nil {
		return "", err
	}
	if prepared != nil {
		if err := infradb.CreatePluginRevisionContent(ctx, tx, prepared.Content.model(revision.ID)); err != nil {
			return "", err
		}
	}
	if err := infradb.SetPluginCurrentRevision(ctx, tx, plugin.ID, uint(nextRevision), 0); err != nil {
		return "", err
	}
	switch {
	case created:
		operation = "created"
	case restored:
		operation = "restored"
	default:
		operation = "updated"
	}
	return operation, nil
}

func cloneMCPAuthConfig(config types.MCPChannelAuthConfig) types.MCPChannelAuthConfig {
	result := types.MCPChannelAuthConfig{
		Description: config.Description,
		Fields:      append([]types.MCPChannelAuthField(nil), config.Fields...),
		Bindings: types.MCPChannelAuthBindings{
			SkillEnv:   cloneStringMap(config.Bindings.SkillEnv),
			MCPHeaders: cloneStringMap(config.Bindings.MCPHeaders),
			MCPEnv:     cloneStringMap(config.Bindings.MCPEnv),
			MCPQuery:   cloneStringMap(config.Bindings.MCPQuery),
		},
		Handler: config.Handler,
	}
	if config.OAuth != nil {
		oauth := *config.OAuth
		oauth.Scopes = append([]string(nil), config.OAuth.Scopes...)
		result.OAuth = &oauth
	}
	return result
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
		Auth: auth, Skill: skill, MCP: mcp,
	})
	return encoded
}
