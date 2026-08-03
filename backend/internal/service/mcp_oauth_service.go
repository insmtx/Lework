package service

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"

	"github.com/insmtx/Leros/backend/internal/api/contract"
	baidunetdisk "github.com/insmtx/Leros/backend/internal/infra/providers/baidunetdisk"
	"github.com/insmtx/Leros/backend/types"
)

const (
	oauthAttemptTTL      = 10 * time.Minute
	oauthRefreshLeadTime = 10 * time.Minute
)

type connectorOAuthManager struct {
	baidu *baidunetdisk.Client
	now   func() time.Time
}

func newConnectorOAuthManager() *connectorOAuthManager {
	return &connectorOAuthManager{baidu: baidunetdisk.NewClient(nil), now: time.Now}
}

func (s *pluginService) oauthManager() *connectorOAuthManager {
	if s.oauth != nil {
		return s.oauth
	}
	return newConnectorOAuthManager()
}

func (s *pluginService) StartMCPPlatformOAuth(
	ctx context.Context,
	orgID, uin uint,
	platformCode string,
) (*contract.StartMCPPlatformOAuthResponse, error) {
	oauth := s.oauthManager()
	if orgID == 0 || uin == 0 {
		return nil, invalidMCPConfig("organization and user identity are required")
	}
	channel, err := s.getSupportedMCPChannel(ctx, strings.ToLower(strings.TrimSpace(platformCode)))
	if err != nil {
		return nil, err
	}
	if channel == nil || channel.AuthType != types.MCPChannelAuthTypeOAuth {
		return nil, invalidMCPConfig("OAuth MCP platform is not configured or active")
	}
	appConfig, err := baiduOAuthAppConfig(channel)
	if err != nil {
		return nil, invalidMCPConfig(err.Error())
	}

	stateSecret, err := randomOAuthSecret()
	if err != nil {
		return nil, err
	}
	attemptID := "oauth_" + uuid.NewString()
	expiresAt := oauth.now().UTC().Add(oauthAttemptTTL)
	code := platformPluginCode(orgID, uin, channel.Channel)
	existing, err := s.getOAuthPluginByIdentity(ctx, orgID, uin, code)
	if err != nil {
		return nil, err
	}
	pluginPublicID := "plugin_" + uuid.NewString()
	if existing != nil {
		pluginPublicID = existing.PublicID
	}
	state := pluginPublicID + "." + stateSecret
	authorizationURL, err := oauth.baidu.AuthorizationURL(appConfig, state)
	if err != nil {
		return nil, invalidMCPConfig(err.Error())
	}

	definition, sourceRevisionID, err := s.personalConnectorDefinition(ctx, channel, nil)
	if err != nil {
		return nil, err
	}
	connector, err := ConnectorFromDefinition(definition)
	if err != nil || connector == nil {
		return nil, invalidMCPConfig("connector template definition is invalid")
	}
	connector.Auth.Values = nil
	connector.Auth.OAuth = &ConnectorOAuthDefinition{
		Status: ConnectorOAuthPending, AttemptID: attemptID,
		StateHash: oauthStateHash(state), StateExpiresAt: expiresAt,
		Scopes: append([]string(nil), appConfig.Scopes...),
	}
	definition, err = json.Marshal(connector)
	if err != nil {
		return nil, err
	}

	if existing == nil {
		if err := s.createPendingOAuthConnector(
			ctx, orgID, uin, channel, code, pluginPublicID, definition, sourceRevisionID,
		); err != nil {
			return nil, err
		}
	} else {
		if _, err := s.appendOAuthRevision(ctx, existing, uin, definition, sourceRevisionID, "user"); err != nil {
			return nil, err
		}
	}
	return &contract.StartMCPPlatformOAuthResponse{
		AttemptID: attemptID, AuthorizationURL: authorizationURL, ExpiresAt: expiresAt,
	}, nil
}

func (s *pluginService) GetMCPPlatformOAuthStatus(
	ctx context.Context,
	orgID, uin uint,
	platformCode, attemptID string,
) (*contract.MCPPlatformOAuthStatusResponse, error) {
	channel := strings.ToLower(strings.TrimSpace(platformCode))
	plugin, err := s.getOAuthPluginByIdentity(ctx, orgID, uin, platformPluginCode(orgID, uin, channel))
	if err != nil {
		return nil, err
	}
	if plugin == nil {
		return nil, contract.ErrPluginNotFound
	}
	definition, err := s.currentConnectorDefinition(ctx, s.db, plugin)
	if err != nil {
		return nil, err
	}
	if definition == nil || definition.Channel != channel || definition.Auth.OAuth == nil ||
		definition.Auth.OAuth.AttemptID != strings.TrimSpace(attemptID) {
		return nil, contract.ErrPluginNotFound
	}
	return oauthStatusResponse(plugin, definition.Auth.OAuth), nil
}

func (s *pluginService) CompleteMCPPlatformOAuth(
	ctx context.Context,
	platformCode, state, code, providerError string,
) (*contract.MCPPlatformOAuthStatusResponse, error) {
	oauth := s.oauthManager()
	platformCode = strings.ToLower(strings.TrimSpace(platformCode))
	separator := strings.LastIndex(state, ".")
	if separator <= 0 || separator == len(state)-1 {
		return nil, invalidMCPConfig("OAuth state is invalid")
	}
	pluginPublicID := state[:separator]
	var plugin types.Plugin
	err := s.db.WithContext(ctx).
		Where("public_id = ? AND kind = ? AND owner_scope = ?", pluginPublicID, "mcp", types.OwnerScopeOrganization).
		First(&plugin).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, contract.ErrPluginNotFound
	}
	if err != nil {
		return nil, err
	}
	channel, err := s.getSupportedMCPChannel(ctx, platformCode)
	if err != nil {
		return nil, err
	}
	if channel == nil || channel.AuthType != types.MCPChannelAuthTypeOAuth {
		return nil, invalidMCPConfig("OAuth MCP platform is not configured or active")
	}
	appConfig, err := baiduOAuthAppConfig(channel)
	if err != nil {
		return nil, invalidMCPConfig(err.Error())
	}

	current, err := s.currentConnectorDefinition(ctx, s.db, &plugin)
	if err != nil {
		return nil, err
	}
	if current == nil || current.Channel != platformCode || current.Auth.OAuth == nil ||
		current.Auth.OAuth.Status != ConnectorOAuthPending || current.Auth.OAuth.StateExpiresAt.Before(oauth.now().UTC()) ||
		subtle.ConstantTimeCompare([]byte(current.Auth.OAuth.StateHash), []byte(oauthStateHash(state))) != 1 {
		return nil, invalidMCPConfig("OAuth state is invalid or expired")
	}
	attemptID := current.Auth.OAuth.AttemptID
	exchanging := cloneConnectorDefinition(current)
	exchanging.Auth.OAuth.Status = ConnectorOAuthExchanging
	exchanging.Auth.OAuth.StateHash = ""
	exchanging.Auth.OAuth.StateExpiresAt = time.Time{}
	exchangingRaw, err := json.Marshal(exchanging)
	if err != nil {
		return nil, err
	}
	updated, err := s.appendOAuthRevisionIfCurrent(
		ctx, &plugin, exchangingRaw, attemptID, ConnectorOAuthPending,
	)
	if err != nil {
		return nil, err
	}
	plugin = *updated

	if providerError = strings.TrimSpace(providerError); providerError != "" {
		return s.finishOAuthFailure(ctx, &plugin, exchanging, sanitizeOAuthErrorCode(providerError))
	}
	tokens, err := oauth.baidu.ExchangeCode(ctx, appConfig, strings.TrimSpace(code))
	if err != nil {
		return s.finishOAuthFailure(ctx, &plugin, exchanging, oauthProviderErrorCode(err))
	}
	if strings.TrimSpace(tokens.RefreshToken) == "" {
		return s.finishOAuthFailure(ctx, &plugin, exchanging, "token_response_incomplete")
	}
	active := cloneConnectorDefinition(exchanging)
	active.Auth.Values = map[string]string{
		baiduNetdiskOAuthValueKey:   tokens.AccessToken,
		baiduNetdiskRefreshValueKey: tokens.RefreshToken,
	}
	active.Auth.OAuth = &ConnectorOAuthDefinition{
		Status: ConnectorOAuthActive, AttemptID: attemptID,
		TokenExpiresAt: oauth.now().UTC().Add(tokens.ExpiresIn), Scopes: append([]string(nil), tokens.Scopes...),
	}
	activeRaw, err := json.Marshal(active)
	if err != nil {
		return nil, err
	}
	updated, err = s.appendOAuthRevisionIfCurrent(
		ctx, &plugin, activeRaw, attemptID, ConnectorOAuthExchanging,
	)
	if err != nil {
		return nil, err
	}
	return oauthStatusResponse(updated, active.Auth.OAuth), nil
}

func (s *pluginService) appendOAuthRevisionIfCurrent(
	ctx context.Context,
	plugin *types.Plugin,
	definition json.RawMessage,
	attemptID, expectedStatus string,
) (*types.Plugin, error) {
	if err := ValidatePluginDefinition("mcp", definition); err != nil {
		return nil, err
	}
	var updated types.Plugin
	err := s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).Where("id = ?", plugin.ID).First(&updated).Error; err != nil {
			return err
		}
		current, err := s.currentConnectorDefinition(ctx, tx, &updated)
		if err != nil {
			return err
		}
		if current == nil || current.Auth.OAuth == nil || current.Auth.OAuth.AttemptID != attemptID ||
			current.Auth.OAuth.Status != expectedStatus {
			return invalidMCPConfig("OAuth attempt is no longer pending")
		}
		revision, err := s.currentPluginRevision(ctx, tx, &updated)
		if err != nil {
			return err
		}
		if revision == nil {
			return invalidMCPConfig("OAuth connector revision is missing")
		}
		return s.createOAuthRevision(ctx, tx, &updated, definition, revision.SourcePluginRevisionID, "system", 0)
	})
	return &updated, err
}

// refreshMCPPlatformOAuth refreshes a bound connector before its immutable Run snapshot is created.
func (s *pluginService) refreshMCPPlatformOAuth(
	ctx context.Context,
	orgID uint,
	pluginPublicID string,
) (usable, changed bool, resultErr error) {
	oauth := s.oauthManager()
	resultErr = s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var plugin types.Plugin
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).Where(
			"owner_scope = ? AND org_id = ? AND public_id = ? AND kind = ? AND status = ?",
			types.OwnerScopeOrganization, orgID, pluginPublicID, "mcp", types.PluginStatusActive,
		).First(&plugin).Error; err != nil {
			return err
		}
		currentRevision, err := s.currentPluginRevision(ctx, tx, &plugin)
		if err != nil {
			return err
		}
		if currentRevision == nil {
			return invalidMCPConfig("OAuth connector revision is missing")
		}
		definition, err := ConnectorFromDefinition(currentRevision.Definition)
		if err != nil || definition == nil || definition.Auth.OAuth == nil {
			usable = err == nil
			return err
		}
		if definition.Auth.OAuth.Status != ConnectorOAuthActive {
			usable = false
			return nil
		}
		now := oauth.now().UTC()
		if definition.Auth.OAuth.TokenExpiresAt.After(now.Add(oauthRefreshLeadTime)) {
			usable = true
			return nil
		}
		var channel types.MCPChannel
		if err := tx.Where("channel = ? AND status = ?", definition.Channel, types.MCPChannelStatusActive).
			First(&channel).Error; err != nil {
			return err
		}
		normalized, ok := normalizeSupportedMCPChannel(&channel)
		if !ok || normalized.AuthType != types.MCPChannelAuthTypeOAuth {
			return invalidMCPConfig("OAuth MCP platform is not configured or active")
		}
		appConfig, err := baiduOAuthAppConfig(normalized)
		if err != nil {
			return err
		}
		refreshToken := strings.TrimSpace(definition.Auth.Values[baiduNetdiskRefreshValueKey])
		tokens, err := oauth.baidu.Refresh(ctx, appConfig, refreshToken)
		if err != nil {
			if oauthProviderRequiresReauthorization(err) {
				reauthorization := cloneConnectorDefinition(definition)
				reauthorization.Auth.Values = nil
				reauthorization.Auth.OAuth.Status = ConnectorOAuthReauthorizationRequired
				reauthorization.Auth.OAuth.ErrorCode = oauthProviderErrorCode(err)
				raw, marshalErr := json.Marshal(reauthorization)
				if marshalErr != nil {
					return marshalErr
				}
				changed = true
				usable = false
				return s.createOAuthRevision(
					ctx, tx, &plugin, raw, currentRevision.SourcePluginRevisionID, "system", 0,
				)
			}
			usable = definition.Auth.OAuth.TokenExpiresAt.After(now)
			return err
		}
		if strings.TrimSpace(tokens.RefreshToken) == "" {
			tokens.RefreshToken = refreshToken
		}
		refreshed := cloneConnectorDefinition(definition)
		refreshed.Auth.Values = map[string]string{
			baiduNetdiskOAuthValueKey:   tokens.AccessToken,
			baiduNetdiskRefreshValueKey: tokens.RefreshToken,
		}
		refreshed.Auth.OAuth.TokenExpiresAt = now.Add(tokens.ExpiresIn)
		refreshed.Auth.OAuth.Scopes = append([]string(nil), tokens.Scopes...)
		refreshed.Auth.OAuth.ErrorCode = ""
		raw, err := json.Marshal(refreshed)
		if err != nil {
			return err
		}
		changed = true
		usable = true
		return s.createOAuthRevision(ctx, tx, &plugin, raw, currentRevision.SourcePluginRevisionID, "system", 0)
	})
	return usable, changed, resultErr
}

func (s *pluginService) createOAuthRevision(
	ctx context.Context,
	tx *gorm.DB,
	plugin *types.Plugin,
	definition json.RawMessage,
	sourceRevisionID *uint,
	publishedByType string,
	publishedBy uint,
) error {
	if err := ValidatePluginDefinition("mcp", definition); err != nil {
		return err
	}
	next := plugin.CurrentRevision + 1
	if next <= 0 {
		next = 1
	}
	revision := &types.PluginRevision{
		PluginID: plugin.ID, SourcePluginRevisionID: sourceRevisionID, Revision: next,
		Status: "published", Definition: definition, PublishedByType: publishedByType,
		PublishedByID: publishedBy, PublishedAt: s.oauthManager().now(),
	}
	if err := tx.Create(revision).Error; err != nil {
		return err
	}
	if err := infradbSetCurrentRevision(ctx, tx, plugin.ID, next, publishedBy); err != nil {
		return err
	}
	plugin.CurrentRevision = next
	return nil
}

func (s *pluginService) finishOAuthFailure(
	ctx context.Context,
	plugin *types.Plugin,
	definition *ConnectorDefinition,
	code string,
) (*contract.MCPPlatformOAuthStatusResponse, error) {
	failed := cloneConnectorDefinition(definition)
	failed.Auth.Values = nil
	failed.Auth.OAuth.Status = ConnectorOAuthFailed
	failed.Auth.OAuth.ErrorCode = code
	failedRaw, err := json.Marshal(failed)
	if err != nil {
		return nil, err
	}
	updated, err := s.appendOAuthRevisionIfCurrent(
		ctx, plugin, failedRaw, failed.Auth.OAuth.AttemptID, ConnectorOAuthExchanging,
	)
	if err != nil {
		return nil, err
	}
	return oauthStatusResponse(updated, failed.Auth.OAuth), nil
}

func (s *pluginService) createPendingOAuthConnector(
	ctx context.Context,
	orgID, uin uint,
	channel *types.MCPChannel,
	code, publicID string,
	definition json.RawMessage,
	sourceRevisionID *uint,
) error {
	if err := ValidatePluginDefinition("mcp", definition); err != nil {
		return err
	}
	return s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		plugin := &types.Plugin{
			PublicID: publicID, OwnerScope: types.OwnerScopeOrganization, OrgID: orgID,
			Code: code, Kind: "mcp", Name: channel.Name, Description: channel.Description,
			Status: types.PluginStatusActive, Origin: "org", CreatedBy: uin, UpdatedBy: uin,
		}
		if err := tx.Create(plugin).Error; err != nil {
			return err
		}
		revision := &types.PluginRevision{
			PluginID: plugin.ID, SourcePluginRevisionID: sourceRevisionID, Revision: 1,
			Status: "published", Definition: definition, PublishedByType: "user",
			PublishedByID: uin, PublishedAt: s.oauthManager().now(),
		}
		if err := tx.Create(revision).Error; err != nil {
			return err
		}
		return infradbSetCurrentRevision(ctx, tx, plugin.ID, 1, uin)
	})
}

func (s *pluginService) appendOAuthRevision(
	ctx context.Context,
	plugin *types.Plugin,
	publishedBy uint,
	definition json.RawMessage,
	sourceRevisionID *uint,
	publishedByType string,
) (*types.Plugin, error) {
	if err := ValidatePluginDefinition("mcp", definition); err != nil {
		return nil, err
	}
	var updated types.Plugin
	err := s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).Where("id = ?", plugin.ID).First(&updated).Error; err != nil {
			return err
		}
		current, err := s.currentPluginRevision(ctx, tx, &updated)
		if err != nil {
			return err
		}
		if sourceRevisionID == nil && current != nil {
			sourceRevisionID = current.SourcePluginRevisionID
		}
		next := updated.CurrentRevision + 1
		if next <= 0 {
			next = 1
		}
		revision := &types.PluginRevision{
			PluginID: updated.ID, SourcePluginRevisionID: sourceRevisionID, Revision: next,
			Status: "published", Definition: definition, PublishedByType: publishedByType,
			PublishedByID: publishedBy, PublishedAt: s.oauthManager().now(),
		}
		if err := tx.Create(revision).Error; err != nil {
			return err
		}
		if err := infradbSetCurrentRevision(ctx, tx, updated.ID, next, publishedBy); err != nil {
			return err
		}
		updated.CurrentRevision = next
		return nil
	})
	return &updated, err
}

func (s *pluginService) getOAuthPluginByIdentity(ctx context.Context, orgID, uin uint, code string) (*types.Plugin, error) {
	var plugin types.Plugin
	err := s.db.WithContext(ctx).Where(
		"owner_scope = ? AND org_id = ? AND kind = ? AND code = ? AND created_by = ?",
		types.OwnerScopeOrganization, orgID, "mcp", code, uin,
	).First(&plugin).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, nil
	}
	return &plugin, err
}

func (s *pluginService) currentPluginRevision(ctx context.Context, database *gorm.DB, plugin *types.Plugin) (*types.PluginRevision, error) {
	var revision types.PluginRevision
	err := database.WithContext(ctx).Where(
		"plugin_id = ? AND revision = ?", plugin.ID, plugin.CurrentRevision,
	).First(&revision).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, nil
	}
	return &revision, err
}

func (s *pluginService) currentConnectorDefinition(ctx context.Context, database *gorm.DB, plugin *types.Plugin) (*ConnectorDefinition, error) {
	revision, err := s.currentPluginRevision(ctx, database, plugin)
	if err != nil || revision == nil {
		return nil, err
	}
	return ConnectorFromDefinition(revision.Definition)
}

func baiduOAuthAppConfig(channel *types.MCPChannel) (baidunetdisk.AppConfig, error) {
	config := types.MCPChannelAuthConfig(channel.AuthConfig)
	if config.OAuth == nil {
		return baidunetdisk.AppConfig{}, fmt.Errorf("baidu OAuth application configuration is required")
	}
	result := baidunetdisk.AppConfig{
		AppKey: strings.TrimSpace(config.OAuth.AppKey), SecretKey: strings.TrimSpace(config.OAuth.SecretKey),
		RedirectURI: strings.TrimSpace(config.OAuth.RedirectURI), Scopes: append([]string(nil), config.OAuth.Scopes...),
	}
	client := baidunetdisk.NewClient(nil)
	if _, err := client.AuthorizationURL(result, "validation"); err != nil {
		return baidunetdisk.AppConfig{}, err
	}
	return result, nil
}

func randomOAuthSecret() (string, error) {
	value := make([]byte, 32)
	if _, err := rand.Read(value); err != nil {
		return "", fmt.Errorf("generate OAuth state: %w", err)
	}
	return base64.RawURLEncoding.EncodeToString(value), nil
}

func oauthStateHash(state string) string {
	sum := sha256.Sum256([]byte(state))
	return hex.EncodeToString(sum[:])
}

func oauthStatusResponse(plugin *types.Plugin, oauth *ConnectorOAuthDefinition) *contract.MCPPlatformOAuthStatusResponse {
	return &contract.MCPPlatformOAuthStatusResponse{
		AttemptID: oauth.AttemptID, Status: oauth.Status, PluginID: plugin.PublicID,
		DisplayName: oauth.DisplayName, ErrorCode: oauth.ErrorCode, Connected: oauth.Status == ConnectorOAuthActive,
	}
}

func cloneConnectorDefinition(source *ConnectorDefinition) *ConnectorDefinition {
	encoded, _ := json.Marshal(source)
	var result ConnectorDefinition
	_ = json.Unmarshal(encoded, &result)
	return &result
}

func redactConnectorSecrets(raw json.RawMessage) (json.RawMessage, error) {
	connector, err := ConnectorFromDefinition(raw)
	if err != nil || connector == nil {
		return append(json.RawMessage(nil), raw...), err
	}
	connector.Auth.Values = nil
	if connector.Auth.OAuth != nil {
		connector.Auth.OAuth.StateHash = ""
	}
	return json.Marshal(connector)
}

func sanitizeOAuthErrorCode(value string) string {
	value = strings.ToLower(strings.TrimSpace(value))
	if value == "" {
		return "authorization_failed"
	}
	for _, r := range value {
		if (r < 'a' || r > 'z') && (r < '0' || r > '9') && r != '_' && r != '-' {
			return "authorization_failed"
		}
	}
	if len(value) > 64 {
		return "authorization_failed"
	}
	return value
}

func oauthProviderErrorCode(err error) string {
	var providerErr *baidunetdisk.ProviderError
	if errors.As(err, &providerErr) {
		return sanitizeOAuthErrorCode(providerErr.Code)
	}
	return "token_exchange_failed"
}

func oauthProviderRequiresReauthorization(err error) bool {
	var providerErr *baidunetdisk.ProviderError
	if !errors.As(err, &providerErr) {
		return false
	}
	switch strings.ToLower(strings.TrimSpace(providerErr.Code)) {
	case "invalid_grant", "invalid_token", "expired_token":
		return true
	default:
		return false
	}
}

func infradbSetCurrentRevision(ctx context.Context, database *gorm.DB, pluginID uint, revision int, updatedBy uint) error {
	return database.WithContext(ctx).Model(&types.Plugin{}).Where("id = ?", pluginID).
		Select("current_revision", "updated_by").
		Updates(types.Plugin{CurrentRevision: revision, UpdatedBy: updatedBy}).Error
}
