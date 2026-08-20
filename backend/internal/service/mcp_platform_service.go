package service

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/ygpkg/yg-go/logs"
	"gorm.io/gorm"

	"github.com/insmtx/Leros/backend/internal/adapter/account"
	"github.com/insmtx/Leros/backend/internal/api/contract"
	infradb "github.com/insmtx/Leros/backend/internal/infra/db"
	"github.com/insmtx/Leros/backend/types"
)

func (s *pluginService) ListMCPPlatforms(
	ctx context.Context,
	orgID, uin uint,
) (*contract.ListMCPPlatformsResponse, error) {
	if orgID == 0 || uin == 0 {
		return nil, invalidMCPConfig("organization and user identity are required")
	}
	channels, err := infradb.ListActiveMCPChannels(ctx, s.db)
	if err != nil {
		return nil, err
	}
	platforms := make([]contract.MCPPlatformView, 0, len(channels))
	for index := range channels {
		channel, ok := normalizeSupportedMCPChannel(&channels[index])
		if !ok {
			continue
		}
		platform, viewErr := s.mcpPlatformView(ctx, orgID, uin, channel)
		if viewErr != nil {
			return nil, viewErr
		}
		platforms = append(platforms, platform)
	}
	return &contract.ListMCPPlatformsResponse{Platforms: platforms}, nil
}

func (s *pluginService) ConnectMCPPlatform(
	ctx context.Context,
	orgID, uin uint,
	platformCode string,
	req *contract.ConnectMCPPlatformRequest,
) (*contract.ConnectMCPPlatformResponse, error) {
	if orgID == 0 || uin == 0 {
		return nil, invalidMCPConfig("organization and user identity are required")
	}
	channelCode := strings.ToLower(strings.TrimSpace(platformCode))
	channel, err := s.getSupportedMCPChannel(ctx, channelCode)
	if err != nil {
		return nil, err
	}
	if channel == nil {
		return nil, invalidMCPConfig("MCP platform is not configured or active")
	}
	if channel.AuthType == types.MCPChannelAuthTypeOAuth {
		return nil, invalidMCPConfig("OAuth connector must use the OAuth authorization endpoint")
	}
	code := platformPluginCode(orgID, uin, channelCode)
	existing, err := infradb.GetOrganizationPluginByIdentity(ctx, s.db, orgID, "mcp", code)
	if err != nil {
		return nil, err
	}
	if existing != nil {
		if existing.CreatedBy != uin {
			return nil, contract.ErrPluginNotFound
		}
		platform := mcpPlatformViewFromPlugin(channel, s.channelCanConnect(channel), existing.PublicID)
		view := pluginView(*existing)
		return &contract.ConnectMCPPlatformResponse{Platform: platform, Plugin: view}, nil
	}

	var definition json.RawMessage
	var sourceRevisionID *uint
	toolCount := 0
	switch channel.AuthType {
	case types.MCPChannelAuthTypeForm:
		values, validateErr := validateChannelAuthValues(channel, req)
		if validateErr != nil {
			return nil, validateErr
		}
		definition, sourceRevisionID, err = s.instantiateSystemConnectorTemplate(ctx, channel, values)
	case types.MCPChannelAuthTypeManaged:
		handler := strings.TrimSpace(types.MCPChannelAuthConfig(channel.AuthConfig).Handler)
		if handler != coreKGPlatformCode || s.apiKeyIssuer == nil {
			return nil, invalidMCPConfig("current edition does not support this managed authorization")
		}
		var template *ConnectorDefinition
		var templateRevision *types.PluginRevision
		template, templateRevision, err = s.loadSystemConnectorTemplate(ctx, channel)
		if err != nil {
			return nil, err
		}
		var credential *account.CreatedAPIKey
		credential, err = s.apiKeyIssuer.CreateAPIKey(ctx, account.CreateAPIKeyInput{
			Name: "SingerOS " + channel.Name + " MCP", Purpose: "mcp_connector",
			ResourceType: "mcp", ResourceID: 0, ExpireHours: 0,
		})
		if err != nil {
			return nil, fmt.Errorf("create CoreKG API key: %w", err)
		}
		definition, err = instantiateSystemConnectorDefinition(template, map[string]string{"api_key": credential.APIKey})
		sourceRevisionID = &templateRevision.ID
	case types.MCPChannelAuthTypeNone:
		definition, sourceRevisionID, err = s.instantiateSystemConnectorTemplate(ctx, channel, nil)
	default:
		return nil, invalidMCPConfig("unsupported connector authorization type")
	}
	if err != nil {
		return nil, err
	}
	if mcp, decodeErr := MCPFromDefinition(definition); decodeErr != nil {
		return nil, decodeErr
	} else if mcp != nil && mcp.Transport == "http" {
		testResult, testErr := s.TestMCPPlugin(ctx, &contract.TestMCPPluginRequest{
			Transport: mcp.Transport, URL: mcp.URL, BearerToken: mcp.BearerToken, Headers: mcp.Headers,
		})
		if testErr != nil {
			return nil, testErr
		}
		toolCount = testResult.ToolCount
	}

	created, err := s.createPlatformConnector(ctx, orgID, uin, channel, code, definition, sourceRevisionID)
	if err != nil {
		if concurrent, lookupErr := infradb.GetOrganizationPluginByIdentity(
			ctx, s.db, orgID, "mcp", code,
		); lookupErr == nil && concurrent != nil && concurrent.CreatedBy == uin {
			view := pluginView(*concurrent)
			return &contract.ConnectMCPPlatformResponse{
				Platform: mcpPlatformViewFromPlugin(channel, true, concurrent.PublicID),
				Plugin:   view, ToolCount: toolCount,
			}, nil
		}
		return nil, err
	}
	return &contract.ConnectMCPPlatformResponse{
		Platform: mcpPlatformViewFromPlugin(channel, true, created.PublicID),
		Plugin:   *created, ToolCount: toolCount,
	}, nil
}

// TestMCPPlatform tests a connected platform using its server-side revision and credentials.
// Platform connector definitions are intentionally not exposed as editable custom MCP definitions.
func (s *pluginService) TestMCPPlatform(
	ctx context.Context,
	orgID, uin uint,
	platformCode string,
) (*contract.TestMCPPluginResponse, error) {
	if orgID == 0 || uin == 0 {
		return nil, invalidMCPConfig("organization and user identity are required")
	}
	channelCode := strings.ToLower(strings.TrimSpace(platformCode))
	channel, err := s.getSupportedMCPChannel(ctx, channelCode)
	if err != nil {
		return nil, err
	}
	if channel == nil {
		return nil, invalidMCPConfig("MCP platform is not configured or active")
	}

	plugin, err := infradb.GetOrganizationPluginByIdentity(
		ctx, s.db, orgID, "mcp", platformPluginCode(orgID, uin, channelCode),
	)
	if err != nil {
		return nil, err
	}
	if plugin == nil || plugin.CreatedBy != uin {
		return nil, invalidMCPConfig("MCP platform is not connected")
	}
	revision, err := infradb.GetCurrentPluginRevision(ctx, s.db, plugin)
	if err != nil {
		return nil, err
	}
	if revision == nil {
		return nil, invalidMCPConfig("MCP platform has no published revision")
	}
	mcp, err := MCPFromDefinition(revision.Definition)
	if err != nil {
		return nil, err
	}
	if mcp == nil || strings.TrimSpace(mcp.URL) == "" {
		return nil, invalidMCPConfig("MCP platform does not expose a testable MCP service")
	}
	return s.TestMCPPlugin(ctx, &contract.TestMCPPluginRequest{
		Transport:   mcp.Transport,
		URL:         mcp.URL,
		BearerToken: mcp.BearerToken,
		Headers:     mcp.Headers,
	})
}

func validateChannelAuthValues(
	channel *types.MCPChannel,
	req *contract.ConnectMCPPlatformRequest,
) (map[string]string, error) {
	values := map[string]string{}
	if req != nil {
		for key, value := range req.AuthValues {
			values[strings.TrimSpace(key)] = strings.TrimSpace(value)
		}
	}
	config := types.MCPChannelAuthConfig(channel.AuthConfig)
	allowed := make(map[string]types.MCPChannelAuthField, len(config.Fields))
	for _, field := range config.Fields {
		allowed[field.Key] = field
		if field.Required && values[field.Key] == "" {
			return nil, invalidMCPConfig(field.Label + " is required")
		}
	}
	for key, value := range values {
		if _, ok := allowed[key]; !ok {
			return nil, invalidMCPConfig("unknown authorization field")
		}
		if strings.ContainsRune(value, '\x00') {
			return nil, invalidMCPConfig("authorization value contains NUL")
		}
	}
	return values, nil
}

// instantiateSystemConnectorTemplate copies one system connector template with caller-specific credentials.
func (s *pluginService) instantiateSystemConnectorTemplate(
	ctx context.Context,
	channel *types.MCPChannel,
	values map[string]string,
) (json.RawMessage, *uint, error) {
	definition, revision, err := s.loadSystemConnectorTemplate(ctx, channel)
	if err != nil {
		return nil, nil, err
	}
	encoded, err := instantiateSystemConnectorDefinition(definition, values)
	if err != nil {
		return nil, nil, err
	}
	return encoded, &revision.ID, nil
}

func (s *pluginService) loadSystemConnectorTemplate(
	ctx context.Context,
	channel *types.MCPChannel,
) (*ConnectorDefinition, *types.PluginRevision, error) {
	template, err := infradb.GetSystemPluginByCode(ctx, s.db, "mcp", channel.Channel)
	if err != nil {
		return nil, nil, err
	}
	if template == nil || template.Origin != builtinConnectorOrigin ||
		template.Status != types.PluginStatusActive {
		return nil, nil, invalidMCPConfig("connector template is not available")
	}
	revision, err := infradb.GetCurrentPluginRevision(ctx, s.db, template)
	if err != nil {
		return nil, nil, err
	}
	if revision == nil {
		return nil, nil, invalidMCPConfig("connector template has no published revision")
	}
	definition, err := ConnectorFromDefinition(revision.Definition)
	if err != nil || definition == nil {
		return nil, nil, invalidMCPConfig("connector template definition is invalid")
	}
	return definition, revision, nil
}

func instantiateSystemConnectorDefinition(
	template *ConnectorDefinition,
	values map[string]string,
) (json.RawMessage, error) {
	if template == nil {
		return nil, invalidMCPConfig("connector template definition is invalid")
	}
	definition := *template
	definition.Auth.Values = cloneStringMap(values)
	encoded, err := json.Marshal(definition)
	if err != nil {
		return nil, err
	}
	if err := ValidatePluginDefinition("mcp", encoded); err != nil {
		return nil, err
	}
	return encoded, nil
}

func (s *pluginService) createPlatformConnector(
	ctx context.Context,
	orgID, uin uint,
	channel *types.MCPChannel,
	code string,
	definition json.RawMessage,
	sourceRevisionID *uint,
) (*contract.PluginView, error) {
	var created types.Plugin
	err := s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		created = types.Plugin{
			PublicID: "plugin_" + uuid.NewString(), OwnerScope: types.OwnerScopeOrganization,
			OrgID: orgID, Code: code, Kind: "mcp", Name: channel.Name,
			Description: channel.Description, Visibility: types.PluginVisibilityPrivate,
			Status: types.PluginStatusActive,
			Origin: "org", CreatedBy: uin, UpdatedBy: uin,
		}
		if err := infradb.CreatePlugin(ctx, tx, &created); err != nil {
			return err
		}
		revision := &types.PluginRevision{
			PluginID: created.ID, SourcePluginRevisionID: sourceRevisionID,
			Revision: 1, Status: "published", Definition: definition,
			PublishedByType: "user", PublishedByID: uin, PublishedAt: time.Now(),
		}
		if err := infradb.CreatePluginRevision(ctx, tx, revision); err != nil {
			return err
		}
		if err := infradb.SetPluginCurrentRevision(ctx, tx, created.ID, 1, uin); err != nil {
			return err
		}
		created.CurrentRevision = 1
		if err := ensurePluginResourceOwner(ctx, tx, orgID, created.ID, uin); err != nil {
			return err
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	view := pluginView(created)
	return &view, nil
}

func (s *pluginService) getSupportedMCPChannel(
	ctx context.Context,
	channelCode string,
) (*types.MCPChannel, error) {
	channel, err := infradb.GetActiveMCPChannelByChannel(ctx, s.db, channelCode)
	if err != nil || channel == nil {
		return channel, err
	}
	normalized, ok := normalizeSupportedMCPChannel(channel)
	if !ok {
		return nil, nil
	}
	return normalized, nil
}

func normalizeSupportedMCPChannel(channel *types.MCPChannel) (*types.MCPChannel, bool) {
	return normalizeMCPChannel(channel, true)
}

func normalizeMCPChannel(channel *types.MCPChannel, requireOAuthAppConfig bool) (*types.MCPChannel, bool) {
	if channel == nil {
		return nil, false
	}
	normalized := *channel
	normalized.Channel = strings.TrimSpace(channel.Channel)
	normalized.Name = strings.TrimSpace(channel.Name)
	normalized.Description = strings.TrimSpace(channel.Description)
	normalized.SkillCode = strings.TrimSpace(channel.SkillCode)
	normalized.Transport = strings.ToLower(strings.TrimSpace(channel.Transport))
	normalized.URL = strings.TrimSpace(channel.URL)
	normalized.AuthType = strings.ToLower(strings.TrimSpace(channel.AuthType))
	normalized.Headers = cloneMCPChannelHeaders(channel.Headers)
	if normalized.Channel == coreKGPlatformCode && normalized.AuthType == types.MCPChannelAuthTypeNone {
		normalized.AuthType = types.MCPChannelAuthTypeManaged
		config := types.MCPChannelAuthConfig(normalized.AuthConfig)
		config.Handler = coreKGPlatformCode
		normalized.AuthConfig = types.MCPChannelAuthConfigJSON(config)
	}

	reason := ""
	switch {
	case channel.Channel != normalized.Channel || normalized.Channel != strings.ToLower(normalized.Channel) ||
		!mcpCodePattern.MatchString(normalized.Channel):
		reason = "channel must be a lowercase slug without surrounding whitespace"
	case normalized.Name == "":
		reason = "name is required"
	case normalized.SkillCode == "" && normalized.Transport == "":
		reason = "skill_code or transport is required"
	case normalized.Transport != "" && normalized.Transport != "http" && normalized.Transport != "sse":
		reason = "built-in channel transport must be http or sse"
	case hasMCPHeader(map[string]string(normalized.Headers), "authorization"):
		reason = "Authorization header is not allowed"
	case !supportedChannelAuthType(normalized.AuthType):
		reason = "unsupported auth_type"
	default:
		if normalized.Transport != "" {
			if err := validateMCPConnection(normalized.URL, map[string]string(normalized.Headers)); err != nil {
				reason = err.Error()
			}
		}
		if reason == "" {
			if err := validateChannelAuthConfig(&normalized, requireOAuthAppConfig); err != nil {
				reason = err.Error()
			}
		}
	}
	if reason != "" {
		logs.Warnf("Skipping invalid MCP channel: id=%d channel=%q reason=%s", channel.ID, channel.Channel, reason)
		return nil, false
	}
	return &normalized, true
}

func validateChannelAuthConfig(channel *types.MCPChannel, requireOAuthAppConfig bool) error {
	config := types.MCPChannelAuthConfig(channel.AuthConfig)
	fields := make(map[string]struct{}, len(config.Fields))
	for _, field := range config.Fields {
		key := strings.TrimSpace(field.Key)
		if key == "" || key != field.Key || !envNamePattern.MatchString(key) {
			return fmt.Errorf("authorization field key is invalid")
		}
		if strings.TrimSpace(field.Label) == "" {
			return fmt.Errorf("authorization field label is required")
		}
		if field.Type != "text" && field.Type != "password" {
			return fmt.Errorf("authorization field type is invalid")
		}
		if _, exists := fields[key]; exists {
			return fmt.Errorf("authorization field key is duplicated")
		}
		fields[key] = struct{}{}
	}
	if channel.AuthType == types.MCPChannelAuthTypeForm && len(fields) == 0 {
		return fmt.Errorf("form authorization requires fields")
	}
	if channel.AuthType == types.MCPChannelAuthTypeManaged && strings.TrimSpace(config.Handler) == "" {
		return fmt.Errorf("managed authorization requires a handler")
	}
	knownValues := fields
	if channel.AuthType == types.MCPChannelAuthTypeManaged {
		knownValues = map[string]struct{}{"api_key": {}}
	}
	if channel.AuthType == types.MCPChannelAuthTypeOAuth {
		if strings.TrimSpace(config.Handler) != baiduNetdiskPlatformCode || config.OAuth == nil {
			return fmt.Errorf("oauth authorization requires a supported handler")
		}
		if requireOAuthAppConfig {
			if _, err := baiduOAuthAppConfig(channel); err != nil {
				return err
			}
		}
		knownValues = map[string]struct{}{
			baiduNetdiskOAuthValueKey: {}, baiduNetdiskRefreshValueKey: {},
		}
	}
	bindings := config.Bindings
	for envName, expression := range bindings.SkillEnv {
		if !envNamePattern.MatchString(envName) {
			return fmt.Errorf("skill environment binding is invalid")
		}
		if err := validateConnectorBinding(expression, knownValues); err != nil {
			return fmt.Errorf("skill environment binding is invalid: %w", err)
		}
	}
	for header, expression := range bindings.MCPHeaders {
		if !headerNamePattern.MatchString(header) {
			return fmt.Errorf("MCP header binding is invalid")
		}
		if err := validateConnectorBinding(expression, knownValues); err != nil {
			return fmt.Errorf("MCP header binding is invalid: %w", err)
		}
	}
	for envName, expression := range bindings.MCPEnv {
		if !envNamePattern.MatchString(envName) {
			return fmt.Errorf("MCP environment binding is invalid")
		}
		if err := validateConnectorBinding(expression, knownValues); err != nil {
			return fmt.Errorf("MCP environment binding is invalid: %w", err)
		}
	}
	for queryName, expression := range bindings.MCPQuery {
		if strings.TrimSpace(queryName) == "" || strings.ContainsAny(queryName, "&=?#") {
			return fmt.Errorf("MCP query binding is invalid")
		}
		if err := validateConnectorBinding(expression, knownValues); err != nil {
			return fmt.Errorf("MCP query binding is invalid: %w", err)
		}
	}
	return nil
}

func supportedChannelAuthType(value string) bool {
	switch value {
	case types.MCPChannelAuthTypeNone, types.MCPChannelAuthTypeForm,
		types.MCPChannelAuthTypeOAuth, types.MCPChannelAuthTypeManaged:
		return true
	default:
		return false
	}
}

func cloneMCPChannelHeaders(headers types.MCPChannelHeaders) types.MCPChannelHeaders {
	if len(headers) == 0 {
		return types.MCPChannelHeaders{}
	}
	result := make(types.MCPChannelHeaders, len(headers))
	for key, value := range headers {
		result[key] = value
	}
	return result
}

func (s *pluginService) mcpPlatformView(
	ctx context.Context,
	orgID, uin uint,
	channel *types.MCPChannel,
) (contract.MCPPlatformView, error) {
	plugin, err := infradb.GetOrganizationPluginByIdentity(
		ctx, s.db, orgID, "mcp", platformPluginCode(orgID, uin, channel.Channel),
	)
	if err != nil {
		return contract.MCPPlatformView{}, err
	}
	if plugin == nil || plugin.CreatedBy != uin {
		view := mcpPlatformViewFromPlugin(channel, s.channelCanConnect(channel), "")
		if channel.AuthType == types.MCPChannelAuthTypeOAuth {
			view.AuthorizationStatus = "disconnected"
		}
		return view, nil
	}
	view := mcpPlatformViewFromPlugin(channel, s.channelCanConnect(channel), plugin.PublicID)
	if channel.AuthType != types.MCPChannelAuthTypeOAuth {
		return view, nil
	}
	definition, err := s.currentConnectorDefinition(ctx, s.db, plugin)
	if err != nil {
		return contract.MCPPlatformView{}, err
	}
	view.Connected = false
	view.AuthorizationStatus = "disconnected"
	if definition != nil && definition.Auth.OAuth != nil {
		view.AuthorizationStatus = definition.Auth.OAuth.Status
		view.Connected = definition.Auth.OAuth.Status == ConnectorOAuthActive
	}
	return view, nil
}

func (s *pluginService) channelCanConnect(channel *types.MCPChannel) bool {
	if channel == nil {
		return false
	}
	if channel.AuthType == types.MCPChannelAuthTypeManaged {
		return strings.TrimSpace(types.MCPChannelAuthConfig(channel.AuthConfig).Handler) == coreKGPlatformCode &&
			s.apiKeyIssuer != nil
	}
	if channel.AuthType == types.MCPChannelAuthTypeOAuth {
		return strings.TrimSpace(types.MCPChannelAuthConfig(channel.AuthConfig).Handler) == baiduNetdiskPlatformCode
	}
	return true
}

func mcpPlatformViewFromPlugin(
	channel *types.MCPChannel,
	autoConnectSupported bool,
	pluginID string,
) contract.MCPPlatformView {
	config := types.MCPChannelAuthConfig(channel.AuthConfig)
	mode := ConnectorModeMCPOnly
	if channel.SkillCode != "" && channel.Transport == "" {
		mode = ConnectorModeSkillOnly
	} else if channel.SkillCode != "" {
		mode = ConnectorModeHybrid
	}
	return contract.MCPPlatformView{
		Code: channel.Channel, Name: channel.Name, Description: channel.Description,
		Mode: mode, AuthType: channel.AuthType, AuthDescription: config.Description,
		AuthFields:           append([]types.MCPChannelAuthField(nil), config.Fields...),
		AutoConnectSupported: autoConnectSupported,
		Connected:            connectedPluginID(pluginID), PluginID: pluginID,
	}
}

func connectedPluginID(pluginID string) bool {
	return strings.TrimSpace(pluginID) != ""
}

func platformPluginCode(orgID, uin uint, channel string) string {
	sum := sha256.Sum256([]byte(fmt.Sprintf("%d:%d:%s", orgID, uin, channel)))
	return channel + "-" + hex.EncodeToString(sum[:16])
}

func coreKGPluginCode(orgID, uin uint) string {
	return platformPluginCode(orgID, uin, coreKGPlatformCode)
}
