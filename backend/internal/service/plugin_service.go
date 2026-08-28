package service

import (
	"archive/zip"
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"

	"github.com/insmtx/Leros/backend/internal/adapter/account"
	"github.com/insmtx/Leros/backend/internal/api/contract"
	infradb "github.com/insmtx/Leros/backend/internal/infra/db"
	"github.com/insmtx/Leros/backend/internal/infra/filestore"
	skillcache "github.com/insmtx/Leros/backend/internal/skill/cache"
	skillcatalog "github.com/insmtx/Leros/backend/internal/skill/catalog"
	skillfetch "github.com/insmtx/Leros/backend/internal/skill/fetch"
	"github.com/insmtx/Leros/backend/types"
	"github.com/ygpkg/yg-go/logs"
)

const defaultPluginListLimit = 50

const maxSkillDownloadURLCodes = 100

type skillAutoInstallLockEntry struct {
	mutex sync.Mutex
	refs  int
}

var skillAutoInstallLocks = struct {
	mutex   sync.Mutex
	entries map[string]*skillAutoInstallLockEntry
}{
	entries: make(map[string]*skillAutoInstallLockEntry),
}

type pluginService struct {
	db                 *gorm.DB
	apiKeyIssuer       account.APIKeyIssuer
	userRepo           account.UserRepository
	oauth              *connectorOAuthManager
	displayTranslation *SkillDisplayTranslationService
}

// NewPluginService creates the independent plugin repository service.
func NewPluginService(database *gorm.DB, displayTranslation *SkillDisplayTranslationService) contract.PluginService {
	return &pluginService{
		db:                 database,
		oauth:              newConnectorOAuthManager(),
		displayTranslation: displayTranslation,
	}
}

// NewPluginServiceWithAPIKeyIssuer enables platform connectors backed by the configured IAM service.
func NewPluginServiceWithAPIKeyIssuer(
	database *gorm.DB,
	issuer account.APIKeyIssuer,
	userRepo account.UserRepository,
	displayTranslation *SkillDisplayTranslationService,
) contract.PluginService {
	return &pluginService{
		db:                 database,
		apiKeyIssuer:       issuer,
		userRepo:           userRepo,
		oauth:              newConnectorOAuthManager(),
		displayTranslation: displayTranslation,
	}
}

// NewOfficialPluginMarketplaceService creates the dedicated official catalogue service.
func NewOfficialPluginMarketplaceService(
	database *gorm.DB,
	displayTranslation *SkillDisplayTranslationService,
) contract.OfficialPluginMarketplaceService {
	return &pluginService{
		db:                 database,
		oauth:              newConnectorOAuthManager(),
		displayTranslation: displayTranslation,
	}
}

func (s *pluginService) ListPlugins(
	ctx context.Context,
	orgID, uin uint,
	req *contract.ListPluginsRequest,
) (*contract.ListPluginsResponse, error) {
	kind := strings.TrimSpace(req.Kind)
	status := strings.TrimSpace(req.Status)
	if kind == "mcp" && (status == "" || status == types.PluginStatusActive) {
		channel, err := s.getSupportedMCPChannel(ctx, coreKGPlatformCode)
		if err != nil {
			return nil, err
		}
		if channel != nil && s.apiKeyIssuer != nil {
			if _, err := s.ConnectMCPPlatform(ctx, orgID, uin, coreKGPlatformCode, nil); err != nil {
				return nil, fmt.Errorf("ensure CoreKG MCP platform: %w", err)
			}
		}
	}
	limit := normalizePluginListLimit(req.Limit)

	plugins, err := infradb.ListPlugins(ctx, s.db, orgID, infradb.PluginListFilter{
		Kind:                    req.Kind,
		Status:                  req.Status,
		Keyword:                 req.Keyword,
		Offset:                  max(req.Offset, 0),
		Limit:                   limit,
		Relation:                strings.TrimSpace(req.Relation),
		ExcludeMarketplaceBased: req.ExcludeMarketplaceBased,
		ViewerUin:               uin,
	})
	if err != nil {
		return nil, err
	}
	pluginIDs := make([]uint, 0, len(plugins))
	for _, plugin := range plugins {
		pluginIDs = append(pluginIDs, plugin.ID)
	}
	viewerRoles, err := infradb.ListPluginViewerRoles(ctx, s.db, orgID, uin, pluginIDs)
	if err != nil {
		return nil, err
	}
	result := make([]contract.PluginView, 0, len(plugins))
	for _, plugin := range plugins {
		result = append(result, pluginViewWithRole(plugin, viewerRoles[plugin.ID]))
	}
	if s.displayTranslation == nil {
		logs.WarnContextf(ctx, "Skill display translation not used: org=%d phase=metadata source_type=%s use=false reason=service_unavailable",
			orgID, types.PluginTranslationSourceOrganization)
	} else if orgID == 0 {
		logs.WarnContextf(ctx, "Skill display translation not used: phase=metadata source_type=%s use=false reason=organization_missing",
			types.PluginTranslationSourceOrganization)
	} else {
		pluginIDs := make([]uint, 0, len(plugins))
		for _, plugin := range plugins {
			if plugin.Kind == "skill" {
				pluginIDs = append(pluginIDs, plugin.ID)
			}
		}
		revisions, revisionErr := infradb.ListCurrentPluginRevisionsByPluginIDs(ctx, s.db, pluginIDs)
		if revisionErr != nil {
			logs.WarnContextf(ctx, "Skill display translation not used: org=%d phase=metadata source_type=%s use=false reason=revision_lookup_failed: %v",
				orgID, types.PluginTranslationSourceOrganization, revisionErr)
		} else {
			sources := make([]skillTranslationSource, 0, len(plugins))
			positions := make([]int, 0, len(plugins))
			for index, plugin := range plugins {
				if plugin.Kind != "skill" {
					continue
				}
				revision, exists := revisions[plugin.ID]
				if !exists {
					logs.WarnContextf(ctx, "Skill display translation not used: org=%d phase=metadata source_type=%s source_id=%d revision_id=0 use=false reason=revision_unavailable",
						orgID, types.PluginTranslationSourceOrganization, plugin.ID)
					continue
				}
				revisionCopy := revision
				sources = append(sources, skillTranslationSource{
					sourceType:  types.PluginTranslationSourceOrganization,
					sourceID:    plugin.ID,
					revision:    &revisionCopy,
					name:        plugin.Name,
					description: plugin.Description,
				})
				positions = append(positions, index)
			}
			translations := s.displayTranslation.translateMetadata(ctx, orgID, sources)
			for index, source := range sources {
				key := skillTranslationKey{
					sourceType: source.sourceType,
					sourceID:   source.sourceID,
					revisionID: source.revision.ID,
				}
				applyTranslatedMetadata(&result[positions[index]], translations[key])
			}
		}
	}
	return &contract.ListPluginsResponse{Plugins: result}, nil
}

// ListBuiltinSkills returns active builtin_worker system skills ordered by code.
func (s *pluginService) ListBuiltinSkills(ctx context.Context) (*contract.ListPluginsResponse, error) {
	plugins, err := infradb.ListActiveSystemPluginsByOrigin(ctx, s.db, "skill", builtinWorkerOrigin)
	if err != nil {
		return nil, err
	}
	result := make([]contract.PluginView, 0, len(plugins))
	for _, p := range plugins {
		result = append(result, pluginView(p))
	}
	if s.displayTranslation == nil || len(plugins) == 0 {
		return &contract.ListPluginsResponse{Plugins: result}, nil
	}

	pluginIDs := make([]uint, 0, len(plugins))
	for _, plugin := range plugins {
		pluginIDs = append(pluginIDs, plugin.ID)
	}
	revisions, revisionErr := infradb.ListCurrentPluginRevisionsByPluginIDs(ctx, s.db, pluginIDs)
	if revisionErr != nil {
		logs.WarnContextf(ctx, "Skill display translation not used: phase=metadata source_type=%s use=false reason=system_revision_lookup_failed: %v",
			types.PluginTranslationSourceSystem, revisionErr)
		return &contract.ListPluginsResponse{Plugins: result}, nil
	}

	sources := make([]skillTranslationSource, 0, len(plugins))
	positions := make([]int, 0, len(plugins))
	for index, plugin := range plugins {
		revision, exists := revisions[plugin.ID]
		if !exists {
			logs.WarnContextf(ctx, "Skill display translation not used: phase=metadata source_type=%s source_id=%d revision_id=0 use=false reason=system_revision_unavailable",
				types.PluginTranslationSourceSystem, plugin.ID)
			continue
		}
		revisionCopy := revision
		sources = append(sources, skillTranslationSource{
			sourceType:  types.PluginTranslationSourceSystem,
			sourceID:    plugin.ID,
			revision:    &revisionCopy,
			name:        plugin.Name,
			description: plugin.Description,
		})
		positions = append(positions, index)
	}
	translations := s.displayTranslation.translateSystemMetadata(ctx, sources)
	for index, source := range sources {
		key := skillTranslationKey{
			sourceType: source.sourceType,
			sourceID:   source.sourceID,
			revisionID: source.revision.ID,
		}
		applyTranslatedMetadata(&result[positions[index]], translations[key])
	}
	return &contract.ListPluginsResponse{Plugins: result}, nil
}

func (s *pluginService) GetPlugin(
	ctx context.Context,
	orgID, uin uint,
	pluginID string,
	req *contract.GetPluginRequest,
) (*contract.GetPluginResponse, error) {
	plugin, err := infradb.GetPluginByPublicID(ctx, s.db, orgID, pluginID)
	if err != nil {
		return nil, err
	}
	role, err := s.pluginAccess().ResolveRole(ctx, orgID, uin, plugin)
	if err != nil {
		return nil, err
	}
	view := pluginViewWithRole(*plugin, role)
	revision, err := infradb.GetCurrentPluginRevision(ctx, s.db, plugin)
	if err != nil {
		return nil, err
	}
	if revision == nil {
		if plugin.Kind == "skill" {
			logs.WarnContextf(ctx, "Skill display translation not used: org=%d phase=metadata source_type=%s source_id=%d revision_id=0 use=false reason=revision_unavailable",
				orgID, types.PluginTranslationSourceOrganization, plugin.ID)
		}
		return &contract.GetPluginResponse{Plugin: &view}, nil
	}
	var skillSource *skillTranslationSource
	if plugin.Kind == "skill" && s.displayTranslation == nil {
		logs.WarnContextf(ctx, "Skill display translation not used: org=%d phase=metadata source_type=%s source_id=%d revision_id=%d use=false reason=service_unavailable",
			orgID, types.PluginTranslationSourceOrganization, plugin.ID, revision.ID)
	} else if plugin.Kind == "skill" && s.displayTranslation != nil {
		source := skillTranslationSource{
			sourceType:  types.PluginTranslationSourceOrganization,
			sourceID:    plugin.ID,
			revision:    revision,
			name:        plugin.Name,
			description: plugin.Description,
		}
		key := skillTranslationKey{
			sourceType: source.sourceType,
			sourceID:   source.sourceID,
			revisionID: source.revision.ID,
		}
		applyTranslatedMetadata(&view, s.displayTranslation.translateMetadata(ctx, orgID, []skillTranslationSource{source})[key])
		skillSource = &source
	}
	if !isBundleDefinition(plugin.Kind) {
		definition, redactErr := redactConnectorSecrets(revision.Definition)
		if redactErr != nil {
			return nil, redactErr
		}
		return &contract.GetPluginResponse{
			Plugin:     &view,
			Definition: definition,
		}, nil
	}
	content, err := infradb.GetPluginRevisionContent(ctx, s.db, revision.ID)
	if err != nil {
		return nil, err
	}
	contentView, err := pluginRevisionContentView(revision, content)
	if err != nil {
		return nil, err
	}
	if skillSource != nil && content != nil {
		skillSource.content = content.EntrypointContent
		translatedBody, translateErr := s.displayTranslation.translateDocumentBody(ctx, orgID, *skillSource)
		if translateErr != nil {
			logs.WarnContextf(ctx, "translate organization Skill document: org=%d plugin=%s revision_id=%d: %v",
				orgID, plugin.PublicID, revision.ID, translateErr)
		} else if translatedBody != "" && contentView != nil {
			contentView.SkillMD = translatedBody
		}
	}
	return &contract.GetPluginResponse{Plugin: &view, Content: contentView}, nil
}

// GetPluginInstallationStatus reports one organization's installed revision and official update state.
func (s *pluginService) GetPluginInstallationStatus(
	ctx context.Context,
	orgID, uin uint,
	req *contract.GetPluginInstallationStatusRequest,
) (*contract.PluginInstallationStatusResponse, error) {
	if req == nil {
		return nil, fmt.Errorf("plugin installation status request is required")
	}
	kind, code := strings.TrimSpace(req.Kind), strings.TrimSpace(req.Code)
	if kind == "" || code == "" {
		return nil, fmt.Errorf("kind and code are required")
	}
	result := &contract.PluginInstallationStatusResponse{Kind: kind, Code: code}

	latest, err := loadPublishedMarketplaceReleaseByIdentity(ctx, s.db, kind, code)
	if err != nil {
		return nil, err
	}
	if latest != nil {
		result.MarketplaceAvailable = true
		result.LatestMarketplaceVersion = strconv.Itoa(latest.Revision.Revision)
	}

	plugin, err := infradb.GetOrganizationPluginByIdentity(ctx, s.db, orgID, kind, code)
	if err != nil {
		return nil, err
	}
	if plugin == nil {
		return result, nil
	}
	if _, err := s.pluginAccess().ResolveRole(ctx, orgID, uin, plugin); err != nil {
		if errors.Is(err, contract.ErrPluginNotFound) {
			return result, nil
		}
		return nil, err
	}
	result.Installed = true
	result.PluginID = plugin.PublicID
	result.CurrentVersion = strconv.Itoa(plugin.CurrentRevision)

	current, err := infradb.GetCurrentPluginRevision(ctx, s.db, plugin)
	if err != nil {
		return nil, err
	}
	if current == nil {
		return nil, fmt.Errorf("installed plugin current revision is unavailable")
	}
	if current.SourceMarketplaceItemID == nil && current.SourcePluginRevisionID == nil {
		return result, nil
	}
	if current.SourceMarketplaceItemID == nil || current.SourcePluginRevisionID == nil {
		return nil, fmt.Errorf("installed plugin marketplace lineage is incomplete")
	}

	sourceItem, err := infradb.GetPluginMarketplaceItemByIDIncludingDeleted(
		ctx,
		s.db,
		*current.SourceMarketplaceItemID,
	)
	if err != nil {
		return nil, err
	}
	sourceRevision, err := infradb.GetPluginRevisionByID(
		ctx,
		s.db,
		*current.SourcePluginRevisionID,
	)
	if err != nil {
		return nil, err
	}
	if sourceItem == nil || sourceRevision == nil || sourceItem.PluginID == 0 ||
		sourceRevision.PluginID != sourceItem.PluginID ||
		sourceItem.Kind != kind || sourceItem.Code != code {
		return nil, fmt.Errorf("installed plugin marketplace lineage is invalid")
	}
	sourcePlugin, err := infradb.GetPluginByID(ctx, s.db, sourceItem.PluginID)
	if err != nil {
		return nil, err
	}
	if sourcePlugin == nil || sourcePlugin.OwnerScope != types.OwnerScopeSystem ||
		sourcePlugin.OrgID != 0 || sourcePlugin.Origin != "builtin" ||
		sourcePlugin.Kind != kind || sourcePlugin.Code != code {
		return nil, fmt.Errorf("installed plugin marketplace source is invalid")
	}

	result.MarketplaceBased = true
	result.MarketplaceItemID = sourceItem.PublicID
	result.InstalledMarketplaceVersion = strconv.Itoa(sourceRevision.Revision)
	if latest != nil && latest.Item.ID == sourceItem.ID &&
		latest.Plugin.ID == sourcePlugin.ID &&
		latest.Revision.Revision > sourceRevision.Revision {
		result.UpdateAvailable = true
	}
	return result, nil
}

// ListOfficialPluginMarketplaceItems returns the organization-effective market view.
func (s *pluginService) ListOfficialPluginMarketplaceItems(
	ctx context.Context,
	orgID uint,
	req *contract.ListOfficialPluginMarketplaceItemsRequest,
) (*contract.ListOfficialPluginMarketplaceItemsResponse, error) {
	if req == nil {
		req = &contract.ListOfficialPluginMarketplaceItemsRequest{}
	}
	states, err := infradb.ListOrganizationPluginMarketplaceStates(ctx, s.db, orgID, req.Kind)
	if err != nil {
		return nil, err
	}
	stateByIdentity := make(map[string]*infradb.OrganizationPluginMarketplaceState, len(states))
	installedItemIDs := make([]uint, 0, len(states))
	for index := range states {
		state := &states[index]
		stateByIdentity[pluginIdentityKey(state.Kind, state.Code)] = state
		if state.SourceMarketplaceItemID != nil && state.SourcePluginRevisionID != nil &&
			state.SourceMarketplaceVersion > 0 && state.SourcePluginID > 0 {
			installedItemIDs = append(installedItemIDs, *state.SourceMarketplaceItemID)
		}
	}

	items, err := infradb.ListPluginMarketplaceItems(ctx, s.db, infradb.PluginMarketplaceListFilter{
		Kind:     req.Kind,
		Category: req.Category,
		Keyword:  req.Keyword,
	})
	if err != nil {
		return nil, err
	}
	itemIDs := make(map[uint]bool, len(items))
	for _, item := range items {
		itemIDs[item.ID] = true
	}
	installedItems, err := infradb.ListPluginMarketplaceItemsByIDsIncludingDeleted(
		ctx,
		s.db,
		installedItemIDs,
	)
	if err != nil {
		return nil, err
	}
	for _, item := range installedItems {
		if itemIDs[item.ID] || !marketplaceItemMatchesFilter(item, req) {
			continue
		}
		state := stateByIdentity[pluginIdentityKey(item.Kind, item.Code)]
		if !marketplaceStateMatchesItem(state, &item) {
			continue
		}
		items = append(items, item)
		itemIDs[item.ID] = true
	}
	sort.Slice(items, func(i, j int) bool {
		if items[i].PublishedAt.Equal(items[j].PublishedAt) {
			return items[i].PublicID < items[j].PublicID
		}
		return items[i].PublishedAt.After(items[j].PublishedAt)
	})
	if limit := normalizePluginListLimit(req.Limit); len(items) > limit {
		items = items[:limit]
	}

	result := make([]contract.OfficialPluginMarketplaceItemView, 0, len(items))
	targets := make([]marketplaceDisplayTarget, 0, len(items))
	for _, item := range items {
		state := stateByIdentity[pluginIdentityKey(item.Kind, item.Code)]
		view, visible, err := s.organizationMarketplaceItemView(
			ctx,
			&item,
			state,
			false,
		)
		if err != nil {
			logs.WarnContextf(ctx, "build marketplace item %q view failed: %v", item.Code, err)
			continue
		}
		if visible {
			result = append(result, view)
			targets = append(targets, marketplaceDisplayTarget{
				itemID: item.ID, viewIndex: len(result) - 1, item: item, state: state,
			})
		}
	}
	s.translateMarketplaceMetadata(ctx, orgID, targets, result)
	return &contract.ListOfficialPluginMarketplaceItemsResponse{Items: result}, nil
}

// GetOfficialPluginMarketplaceItem returns the organization-effective market detail.
func (s *pluginService) GetOfficialPluginMarketplaceItem(
	ctx context.Context,
	orgID uint,
	itemID string,
) (*contract.OfficialPluginMarketplaceItemView, error) {
	item, err := infradb.GetPluginMarketplaceItemByPublicIDIncludingDeleted(
		ctx,
		s.db,
		strings.TrimSpace(itemID),
	)
	if err != nil {
		return nil, err
	}
	if item == nil {
		return nil, contract.ErrPluginNotFound
	}
	states, err := infradb.ListOrganizationPluginMarketplaceStates(ctx, s.db, orgID, item.Kind)
	if err != nil {
		return nil, err
	}
	var state *infradb.OrganizationPluginMarketplaceState
	for index := range states {
		if states[index].Code == item.Code {
			state = &states[index]
			break
		}
	}
	view, visible, err := s.organizationMarketplaceItemView(ctx, item, state, true)
	if err != nil {
		return nil, err
	}
	if !visible {
		return nil, contract.ErrPluginNotFound
	}
	target := marketplaceDisplayTarget{itemID: item.ID, viewIndex: 0, item: *item, state: state}
	translatedViews := []contract.OfficialPluginMarketplaceItemView{view}
	s.translateMarketplaceMetadata(ctx, orgID, []marketplaceDisplayTarget{target}, translatedViews)
	view = translatedViews[0]
	if err := s.translateMarketplaceDocument(ctx, orgID, &target, &view); err != nil {
		logs.WarnContextf(ctx, "translate marketplace Skill document %q: %v", item.Code, err)
	}
	return &view, nil
}

type marketplaceDisplayTarget struct {
	itemID    uint
	viewIndex int
	item      types.PluginMarketplaceItem
	state     *infradb.OrganizationPluginMarketplaceState
	sourceRev *types.PluginRevision
}

// translateMarketplaceMetadata overlays validated Chinese display text on marketplace views.
func (s *pluginService) translateMarketplaceMetadata(
	ctx context.Context,
	orgID uint,
	targets []marketplaceDisplayTarget,
	views []contract.OfficialPluginMarketplaceItemView,
) {
	if s.displayTranslation == nil {
		logs.WarnContextf(ctx, "Skill display translation not used: org=%d phase=metadata source_type=%s use=false reason=service_unavailable_or_invalid_request",
			orgID, types.PluginTranslationSourceMarketplace)
		return
	}
	if orgID == 0 {
		logs.WarnContextf(ctx, "Skill display translation not used: phase=metadata source_type=%s use=false reason=organization_missing",
			types.PluginTranslationSourceMarketplace)
		return
	}
	if len(targets) == 0 {
		logs.InfoContextf(ctx, "Skill display translation not used: org=%d phase=metadata source_type=%s use=false reason=no_targets",
			orgID, types.PluginTranslationSourceMarketplace)
		return
	}

	sources := make([]skillTranslationSource, 0, len(targets))
	positions := make([]int, 0, len(targets))
	for index := range targets {
		target := &targets[index]
		if target.item.Kind != "skill" || target.viewIndex < 0 || target.viewIndex >= len(views) {
			continue
		}
		revision, err := s.resolveMarketplaceTranslationRevision(ctx, target)
		if err != nil || revision == nil {
			if err != nil {
				logs.WarnContextf(ctx, "Skill display translation not used: org=%d phase=metadata source_type=%s source_id=%d revision_id=0 use=false reason=revision_resolve_failed code=%s: %v",
					orgID, types.PluginTranslationSourceMarketplace, target.itemID, target.item.Code, err)
			} else {
				logs.WarnContextf(ctx, "Skill display translation not used: org=%d phase=metadata source_type=%s source_id=%d revision_id=0 use=false reason=revision_unavailable code=%s",
					orgID, types.PluginTranslationSourceMarketplace, target.itemID, target.item.Code)
			}
			continue
		}
		target.sourceRev = revision
		view := views[target.viewIndex]
		sources = append(sources, skillTranslationSource{
			sourceType:  types.PluginTranslationSourceMarketplace,
			sourceID:    target.itemID,
			revision:    revision,
			name:        view.Name,
			description: view.Description,
		})
		positions = append(positions, target.viewIndex)
	}
	translations := s.displayTranslation.translateMetadata(ctx, orgID, sources)
	for index, source := range sources {
		key := skillTranslationKey{
			sourceType: source.sourceType,
			sourceID:   source.sourceID,
			revisionID: source.revision.ID,
		}
		applyTranslatedMarketplaceMetadata(&views[positions[index]], translations[key])
	}
}

// translateMarketplaceDocument overlays a validated translated body on one detail response.
func (s *pluginService) translateMarketplaceDocument(
	ctx context.Context,
	orgID uint,
	target *marketplaceDisplayTarget,
	view *contract.OfficialPluginMarketplaceItemView,
) error {
	if s.displayTranslation == nil || target == nil || view == nil || view.Content == nil || target.item.Kind != "skill" {
		return nil
	}
	revision, err := s.resolveMarketplaceTranslationRevision(ctx, target)
	if err != nil {
		return err
	}
	if revision == nil {
		return nil
	}
	fullContent, err := s.loadMarketplaceDisplayDocument(ctx, target, revision)
	if err != nil || fullContent == "" {
		return err
	}
	body, err := s.displayTranslation.translateDocumentBody(ctx, orgID, skillTranslationSource{
		sourceType: types.PluginTranslationSourceMarketplace,
		sourceID:   target.itemID,
		revision:   revision,
		content:    fullContent,
	})
	if err != nil {
		return err
	}
	if body != "" {
		view.Content.SkillMD = body
	}
	return nil
}

func (s *pluginService) resolveMarketplaceTranslationRevision(
	ctx context.Context,
	target *marketplaceDisplayTarget,
) (*types.PluginRevision, error) {
	if target == nil {
		return nil, nil
	}
	if marketplaceStateMatchesItem(target.state, &target.item) && target.state.SourcePluginRevisionID != nil {
		return infradb.GetPluginRevisionByID(ctx, s.db, *target.state.SourcePluginRevisionID)
	}
	if target.item.Status != "published" || target.item.DeletedAt.Valid {
		return nil, nil
	}
	_, revision, _, err := loadMarketplaceSource(ctx, s.db, &target.item, false)
	return revision, err
}

func (s *pluginService) loadMarketplaceDisplayDocument(
	ctx context.Context,
	target *marketplaceDisplayTarget,
	sourceRevision *types.PluginRevision,
) (string, error) {
	revisionID := sourceRevision.ID
	if marketplaceStateMatchesItem(target.state, &target.item) && target.state.RevisionID > 0 {
		revisionID = target.state.RevisionID
	}
	content, err := infradb.GetPluginRevisionContent(ctx, s.db, revisionID)
	if err != nil {
		return "", err
	}
	if content == nil {
		return "", nil
	}
	return content.EntrypointContent, nil
}

// GetOfficialPluginLatestVersion reports the current published official revision by stable identity.
func (s *pluginService) GetOfficialPluginLatestVersion(
	ctx context.Context,
	req *contract.GetOfficialPluginLatestVersionRequest,
) (*contract.OfficialPluginLatestVersionResponse, error) {
	if req == nil {
		return nil, fmt.Errorf("official plugin latest version request is required")
	}
	kind, code := strings.TrimSpace(req.Kind), strings.TrimSpace(req.Code)
	if kind == "" || code == "" {
		return nil, fmt.Errorf("kind and code are required")
	}
	result := &contract.OfficialPluginLatestVersionResponse{Kind: kind, Code: code}
	release, err := loadPublishedMarketplaceReleaseByIdentity(ctx, s.db, kind, code)
	if err != nil {
		return nil, err
	}
	if release == nil {
		return result, nil
	}
	result.Available = true
	result.ItemID = release.Item.PublicID
	result.LatestVersion = strconv.Itoa(release.Revision.Revision)
	return result, nil
}

// InstallOfficialPlugin publishes a marketplace-backed revision into the caller organization.
func (s *pluginService) InstallOfficialPlugin(ctx context.Context, orgID, uin uint, itemID string) (*contract.InstallOfficialPluginResponse, error) {
	itemID = strings.TrimSpace(itemID)
	if itemID == "" {
		return nil, contract.ErrPluginNotFound
	}

	var result *contract.InstallOfficialPluginResponse
	err := s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		item, err := infradb.GetPublishedPluginMarketplaceItemByPublicID(ctx, tx, itemID)
		if err != nil {
			return err
		}
		if item == nil || !strings.EqualFold(item.Kind, "skill") {
			return contract.ErrPluginNotFound
		}
		_, sourceRevision, sourceContent, err := loadMarketplaceSource(ctx, tx, item, true)
		if err != nil {
			return fmt.Errorf("load official Skill source: %w", err)
		}

		plugin, op, err := s.installMarketplaceSkillIntoOrg(
			ctx,
			tx,
			orgID,
			"user",
			uin,
			item,
			sourceRevision,
			sourceContent,
			true,
		)
		if err != nil {
			return err
		}
		result = &contract.InstallOfficialPluginResponse{Operation: op, Plugin: pluginView(*plugin)}
		return nil
	})
	if err != nil {
		return nil, err
	}
	return result, nil
}

// installMarketplaceSkillIntoOrg creates or updates an organization Skill from a marketplace source
// inside an existing transaction. overwriteExisting is reserved for explicit user installation;
// Worker auto-install must preserve an active organization Skill that wins a concurrent create.
func (s *pluginService) installMarketplaceSkillIntoOrg(
	ctx context.Context,
	tx *gorm.DB,
	orgID uint,
	publishedByType string,
	publishedByID uint,
	item *types.PluginMarketplaceItem,
	sourceRevision *types.PluginRevision,
	sourceContent *types.PluginRevisionContent,
	overwriteExisting bool,
) (*types.Plugin, string, error) {
	var plugin types.Plugin
	err := tx.Unscoped().
		Clauses(clause.Locking{Strength: "UPDATE"}).
		Where("owner_scope = ? AND org_id = ? AND kind = ? AND code = ?",
			types.OwnerScopeOrganization, orgID, "skill", item.Code).
		Order("CASE WHEN deleted_at IS NULL THEN 0 ELSE 1 END, id DESC").
		First(&plugin).Error
	if err != nil && err != gorm.ErrRecordNotFound {
		return nil, "", err
	}
	created := errors.Is(err, gorm.ErrRecordNotFound)
	restored := false
	if created {
		plugin = types.Plugin{
			PublicID: "plugin_" + uuid.NewString(), OwnerScope: types.OwnerScopeOrganization,
			OrgID: orgID, Code: item.Code, Kind: "skill", Name: item.Name,
			Description: item.Description, Visibility: types.PluginVisibilityPublic,
			Status: types.PluginStatusActive,
			Origin: "marketplace", CreatedBy: publishedByID, UpdatedBy: publishedByID,
		}
		insert := tx.WithContext(ctx).
			Clauses(clause.OnConflict{DoNothing: true}).
			Create(&plugin)
		if insert.Error != nil {
			return nil, "", insert.Error
		}
		if insert.RowsAffected == 0 {
			if err := tx.Unscoped().
				Clauses(clause.Locking{Strength: "UPDATE"}).
				Where("owner_scope = ? AND org_id = ? AND kind = ? AND code = ?",
					types.OwnerScopeOrganization, orgID, "skill", item.Code).
				Order("CASE WHEN deleted_at IS NULL THEN 0 ELSE 1 END, id DESC").
				First(&plugin).Error; err != nil {
				return nil, "", fmt.Errorf("reload concurrently created marketplace Skill plugin: %w", err)
			}
			created = false
		}
	}
	if !created {
		if !strings.EqualFold(plugin.Kind, "skill") {
			return nil, "", fmt.Errorf("plugin kind cannot change")
		}
		if !plugin.DeletedAt.Valid && !overwriteExisting {
			return &plugin, "already_present", nil
		}
		if plugin.DeletedAt.Valid {
			if err := infradb.RestorePlugin(ctx, tx, plugin.ID, publishedByID); err != nil {
				return nil, "", err
			}
			plugin.DeletedAt = gorm.DeletedAt{}
			restored = true
		}
		if err := tx.Model(&types.Plugin{}).Where("id = ?", plugin.ID).
			Select("name", "description", "status", "origin", "updated_by").
			Updates(types.Plugin{Name: item.Name, Description: item.Description, Status: types.PluginStatusActive, Origin: "marketplace", UpdatedBy: publishedByID}).Error; err != nil {
			return nil, "", err
		}
		plugin.Name, plugin.Description, plugin.Status, plugin.Origin, plugin.UpdatedBy = item.Name, item.Description, types.PluginStatusActive, "marketplace", publishedByID
	}

	revisions, err := infradb.ListPluginRevisions(ctx, tx, orgID, plugin.PublicID)
	if err != nil {
		return nil, "", err
	}
	if !restored && len(revisions) > 0 &&
		sameMarketplaceRevision(revisions[0], item, sourceRevision) {
		return &plugin, "already_current", nil
	}
	nextRevision := 1
	for _, existing := range revisions {
		if existing.Revision >= nextRevision {
			nextRevision = existing.Revision + 1
		}
	}
	marketplaceID := item.ID
	sourceRevisionID := sourceRevision.ID
	revision := &types.PluginRevision{
		PluginID: plugin.ID, SourceMarketplaceItemID: &marketplaceID,
		SourcePluginRevisionID: &sourceRevisionID, Revision: nextRevision,
		Status: "published", Definition: append(json.RawMessage(nil), sourceRevision.Definition...),
		PublishedByType: publishedByType, PublishedByID: publishedByID, PublishedAt: time.Now(),
	}
	if err := infradb.CreatePluginRevision(ctx, tx, revision); err != nil {
		return nil, "", err
	}
	content := &types.PluginRevisionContent{
		PluginRevisionID: revision.ID, Schema: sourceContent.Schema,
		ArtifactSHA256: sourceContent.ArtifactSHA256, EntrypointPath: sourceContent.EntrypointPath,
		EntrypointContent: sourceContent.EntrypointContent,
		FileIndex:         append(types.PluginRevisionFileList(nil), sourceContent.FileIndex...),
	}
	if err := infradb.CreatePluginRevisionContent(ctx, tx, content); err != nil {
		return nil, "", err
	}
	if err := infradb.SetPluginCurrentRevision(ctx, tx, plugin.ID, uint(nextRevision), publishedByID); err != nil {
		return nil, "", err
	}
	plugin.CurrentRevision = nextRevision
	operation := "updated"
	if created {
		operation = "installed"
	}
	return &plugin, operation, nil
}

// tryAutoInstallSkill ensures an organization has a Skill installed from the official marketplace.
// If the org already has the Skill it returns nil (no auto-upgrade).
// If the marketplace has no published entry for the code it returns an error.
func (s *pluginService) tryAutoInstallSkill(
	ctx context.Context,
	orgID uint,
	code string,
	workerID uint,
) error {
	if workerID == 0 {
		return fmt.Errorf("worker identity is required")
	}
	unlock := lockSkillAutoInstall(orgID, code)
	defer unlock()

	existing, err := infradb.GetOrganizationPluginByIdentity(ctx, s.db, orgID, "skill", code)
	if err != nil {
		return err
	}
	if existing != nil {
		return nil
	}
	release, err := loadPublishedMarketplaceReleaseByIdentity(ctx, s.db, "skill", code)
	if err != nil {
		return err
	}
	if release == nil {
		return fmt.Errorf("skill %q not found in marketplace", code)
	}
	return s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		_, sourceRevision, sourceContent, err := loadMarketplaceSource(ctx, tx, release.Item, true)
		if err != nil {
			return fmt.Errorf("load marketplace source for %q: %w", code, err)
		}
		_, _, err = s.installMarketplaceSkillIntoOrg(
			ctx,
			tx,
			orgID,
			"worker",
			workerID,
			release.Item,
			sourceRevision,
			sourceContent,
			false,
		)
		return err
	})
}

func lockSkillAutoInstall(orgID uint, code string) func() {
	key := fmt.Sprintf("%d:%s", orgID, code)
	skillAutoInstallLocks.mutex.Lock()
	entry := skillAutoInstallLocks.entries[key]
	if entry == nil {
		entry = &skillAutoInstallLockEntry{}
		skillAutoInstallLocks.entries[key] = entry
	}
	entry.refs++
	skillAutoInstallLocks.mutex.Unlock()

	entry.mutex.Lock()
	return func() {
		entry.mutex.Unlock()
		skillAutoInstallLocks.mutex.Lock()
		entry.refs--
		if entry.refs == 0 {
			delete(skillAutoInstallLocks.entries, key)
		}
		skillAutoInstallLocks.mutex.Unlock()
	}
}

func (s *pluginService) ListPluginVersions(
	ctx context.Context,
	orgID, uin uint,
	pluginID string,
) (*contract.ListPluginVersionsResponse, error) {
	plugin, err := infradb.GetPluginByPublicID(ctx, s.db, orgID, pluginID)
	if err != nil {
		return nil, err
	}
	if err := s.pluginAccess().RequireView(ctx, orgID, uin, plugin); err != nil {
		return nil, err
	}
	revisions, err := infradb.ListPluginRevisions(ctx, s.db, orgID, pluginID)
	if err != nil {
		return nil, err
	}
	result := make([]contract.PluginRevisionView, 0, len(revisions))
	for _, revision := range revisions {
		result = append(result, pluginRevisionView(revision))
	}
	return &contract.ListPluginVersionsResponse{Versions: result}, nil
}

func (s *pluginService) DeletePlugin(ctx context.Context, orgID, uin uint, pluginID string, req *contract.DeletePluginRequest) (*contract.DeletePluginResponse, error) {
	plugin, err := infradb.GetPluginByPublicID(ctx, s.db, orgID, pluginID)
	if err != nil {
		return nil, err
	}
	if plugin == nil {
		return nil, contract.ErrPluginNotFound
	}

	if projectPublicID := strings.TrimSpace(req.ProjectID); projectPublicID != "" {
		project, err := infradb.GetProjectByPublicID(ctx, s.db, orgID, projectPublicID)
		if err != nil {
			return nil, err
		}
		if project == nil {
			return nil, contract.ErrPluginNotFound
		}
		removed, err := infradb.RemoveProjectPluginBinding(ctx, s.db, project.ID, plugin.ID)
		if err != nil {
			return nil, err
		}
		if !removed {
			return nil, contract.ErrPluginNotFound
		}
		return &contract.DeletePluginResponse{Operation: "project_unbound"}, nil
	}
	if err := s.pluginAccess().RequireDelete(ctx, orgID, uin, plugin); err != nil {
		return nil, err
	}

	err = s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		deleted, err := infradb.SoftDeletePlugin(ctx, tx, orgID, pluginID, uin)
		if err != nil {
			return err
		}
		if !deleted {
			return contract.ErrPluginNotFound
		}
		if err := softDeletePluginPermissionResource(ctx, tx, orgID, plugin.ID); err != nil {
			return err
		}
		return infradb.RemoveProjectPluginBindingsByPlugin(ctx, tx, plugin.ID)
	})
	if err != nil {
		return nil, err
	}
	return &contract.DeletePluginResponse{Operation: "deleted"}, nil
}

func (s *pluginService) AddSkillPlugin(ctx context.Context, orgID, uin uint, req *contract.AddSkillPluginRequest) (*contract.AddSkillPluginResponse, error) {
	if req == nil {
		return nil, fmt.Errorf("add skill request is required")
	}
	switch strings.TrimSpace(req.Mode) {
	case contract.SkillAddModeGitHub:
		if strings.TrimSpace(req.FileUploadID) != "" {
			return nil, fmt.Errorf("github mode requires github_url only")
		}
		return s.importSkillPluginFromGitHub(ctx, orgID, uin, strings.TrimSpace(req.GitHubURL))
	case contract.SkillAddModeFile:
		if strings.TrimSpace(req.GitHubURL) != "" {
			return nil, fmt.Errorf("file mode requires file_upload_id only")
		}
	default:
		return nil, fmt.Errorf("mode must be file or github")
	}
	if strings.TrimSpace(req.FileUploadID) == "" {
		return nil, fmt.Errorf("file_upload_id is required")
	}
	file, err := infradb.GetFileUploadByPublicID(ctx, s.db, orgID, strings.TrimSpace(req.FileUploadID))
	if err != nil || file == nil {
		if err != nil {
			return nil, err
		}
		return nil, fmt.Errorf("file not found")
	}
	reader, _, err := filestore.OpenFileByPublicID(ctx, s.db, orgID, file.PublicID)
	if err != nil {
		return nil, fmt.Errorf("open skill package: %w", err)
	}
	defer reader.Close()
	sourceArchive, err := io.ReadAll(io.LimitReader(reader, 100_000_000))
	if err != nil {
		return nil, fmt.Errorf("read skill package: %w", err)
	}
	isMarkdown := strings.HasSuffix(strings.ToLower(file.OriginalName), ".md")
	switch {
	case strings.HasSuffix(strings.ToLower(file.OriginalName), ".zip"):
	case isMarkdown:
		if err := validateSkillMDFromBytes(sourceArchive); err != nil {
			return nil, fmt.Errorf("validate SKILL.md: %w", err)
		}
		sourceArchive, err = skillcache.GenerateSkillZip(sourceArchive, nil)
		if err != nil {
			return nil, fmt.Errorf("package SKILL.md: %w", err)
		}
	default:
		return nil, fmt.Errorf("skill package must be a .zip archive or a SKILL.md file")
	}
	prepared, err := prepareSkillPackage(sourceArchive)
	if err != nil {
		return nil, fmt.Errorf("prepare Skill package: %w", err)
	}
	code, name, description := prepared.Manifest.Name, prepared.Manifest.Name, prepared.Manifest.Description
	artifactFile := file
	fileSHA, _ := normalizedPluginSHA256(file.Sha256)
	if file.Purpose != filestore.PurposeArtifact ||
		file.MimeType != "application/zip" ||
		fileSHA != prepared.SHA256 ||
		file.FileSize != int64(len(prepared.Archive)) {
		artifactFile, err = filestore.Upload(ctx, s.db, filestore.UploadParams{
			Data: prepared.Archive, Filename: "skill-" + uuid.NewString() + ".zip", OriginalName: code + ".zip",
			MimeType: "application/zip", OwnerScope: types.OwnerScopeOrganization,
			OrgID: orgID, OwnerID: uin,
			ObjectKey: fmt.Sprintf("plugins/%d/skills/%s.zip", orgID, uuid.NewString()), Purpose: filestore.PurposeArtifact,
		})
		if err != nil {
			return nil, fmt.Errorf("store normalized skill package: %w", err)
		}
	}
	definition, err := json.Marshal(skillDefinition{Schema: "skill/v1", Artifact: &ArtifactDefinition{FileUploadID: artifactFile.PublicID, SHA256: prepared.SHA256, SizeBytes: artifactFile.FileSize, ContentType: "application/zip"}})
	if err != nil {
		return nil, err
	}
	result, err := s.publishSkillRevision(
		ctx, orgID, uin, code, name, description, definition, prepared.Content,
	)
	if err != nil {
		return nil, err
	}
	return &contract.AddSkillPluginResponse{
		Operation: result.Operation,
		Plugin:    pluginViewWithRole(*result.Plugin, types.ResourceRoleOwner),
	}, nil
}

// ResolveSkillDownloadURLs returns current downloadable Skill artifacts and omits unavailable codes.
func (s *pluginService) ResolveSkillDownloadURLs(ctx context.Context, orgID uint, callerKind types.CallerKind, callerID uint, req *contract.ResolveSkillDownloadURLsRequest) (*contract.ResolveSkillDownloadURLsResponse, error) {
	if req == nil {
		return nil, fmt.Errorf("skill download URL request is required")
	}
	codes := uniqueSkillCodes(req.SkillCodes)
	if len(codes) > maxSkillDownloadURLCodes {
		return nil, fmt.Errorf("at most %d skill_codes are allowed", maxSkillDownloadURLCodes)
	}
	if len(req.ConnectorSkills) > maxSkillDownloadURLCodes {
		return nil, fmt.Errorf("at most %d connector_skills are allowed", maxSkillDownloadURLCodes)
	}
	rows, err := infradb.ListCurrentSkillArtifacts(ctx, s.db, orgID, codes)
	if err != nil {
		return nil, err
	}

	actorUin := req.ActorUin
	if callerKind == types.CallerKindUser {
		actorUin = callerID
	}
	var project *types.Project
	if callerKind == types.CallerKindWorker {
		if projectID := strings.TrimSpace(req.ProjectID); projectID != "" {
			project, err = infradb.GetProjectByPublicID(ctx, s.db, orgID, projectID)
			if err != nil {
				return nil, err
			}
		}
	}
	allowedCodes, err := s.downloadableSkillCodes(ctx, orgID, actorUin, project, codes)
	if err != nil {
		return nil, err
	}

	existingCodes := make(map[string]bool, len(rows))
	result := make([]contract.SkillDownloadURL, 0, len(rows))
	internalCodes := make(map[string]struct{})
	if callerKind == types.CallerKindWorker && strings.EqualFold(strings.TrimSpace(req.Scene), "salary_accounting") {
		internalCodes["attendance-payroll"] = struct{}{}
	}
	for _, code := range codes {
		if _, ok := internalCodes[code]; !ok {
			continue
		}
		download, err := s.resolveInternalSkillDownloadURL(ctx, code)
		if err != nil {
			logs.WarnContextf(ctx, "resolve internal Skill %q download URL failed: %v", code, err)
			continue
		}
		result = append(result, download)
	}
	for _, row := range rows {
		if _, internal := internalCodes[row.Code]; internal {
			continue
		}
		existingCodes[row.Code] = true
		if !allowedCodes[row.Code] {
			logs.WarnContextf(
				ctx,
				"skip Skill download URL: permission denied code=%s caller_kind=%s actor_uin=%d project_id=%s",
				row.Code, callerKind, actorUin, strings.TrimSpace(req.ProjectID),
			)
			continue
		}
		artifact, err := ArtifactFromDefinition("skill", row.Definition)
		if err != nil {
			logs.WarnContextf(ctx, "skip Skill download URL: invalid artifact definition code=%s error=%v", row.Code, err)
			continue
		}
		if artifact == nil {
			logs.WarnContextf(ctx, "skip Skill download URL: missing artifact definition code=%s", row.Code)
			continue
		}
		sha, err := normalizedPluginSHA256(artifact.SHA256)
		if err != nil {
			logs.WarnContextf(ctx, "skip Skill download URL: invalid artifact SHA-256 code=%s error=%v", row.Code, err)
			continue
		}
		downloadURL, err := s.resolveSkillArtifactDownloadURL(ctx, orgID, row, artifact, sha)
		if err != nil {
			logs.WarnContextf(ctx, "skip Skill download URL: artifact unavailable code=%s error=%v", row.Code, err)
			continue
		}
		result = append(result, contract.SkillDownloadURL{Code: row.Code, Revision: row.Revision, SHA256: sha, DownloadURL: downloadURL})
	}

	if callerKind == types.CallerKindWorker && callerID > 0 {
		for _, code := range codes {
			if _, internal := internalCodes[code]; internal {
				continue
			}
			if existingCodes[code] {
				continue
			}
			if err := s.tryAutoInstallSkill(ctx, orgID, code, callerID); err != nil {
				logs.WarnContextf(ctx, "auto-install skill %q failed: %v", code, err)
				continue
			}
			autoRows, autoErr := infradb.ListCurrentSkillArtifacts(ctx, s.db, orgID, []string{code})
			if autoErr != nil || len(autoRows) == 0 {
				if autoErr != nil {
					logs.WarnContextf(ctx, "resolve auto-installed skill %q artifact failed: %v", code, autoErr)
				}
				continue
			}
			row := autoRows[0]
			artifact, err := ArtifactFromDefinition("skill", row.Definition)
			if err != nil || artifact == nil {
				continue
			}
			sha, err := normalizedPluginSHA256(artifact.SHA256)
			if err != nil {
				continue
			}
			downloadURL, err := s.resolveSkillArtifactDownloadURL(ctx, orgID, row, artifact, sha)
			if err != nil {
				logs.WarnContextf(ctx, "resolve auto-installed skill %q download URL failed: %v", code, err)
				continue
			}
			result = append(result, contract.SkillDownloadURL{Code: row.Code, Revision: row.Revision, SHA256: sha, DownloadURL: downloadURL})
		}
		connectorDownloads := s.resolveConnectorSkillDownloads(ctx, orgID, req.ConnectorSkills)
		result = append(result, connectorDownloads...)
	}
	sort.Slice(result, func(i, j int) bool { return result[i].Code < result[j].Code })
	return &contract.ResolveSkillDownloadURLsResponse{Skills: result}, nil
}

func (s *pluginService) resolveInternalSkillDownloadURL(ctx context.Context, code string) (contract.SkillDownloadURL, error) {
	plugin, err := infradb.GetSystemPluginByCode(ctx, s.db, "skill", code)
	if err != nil || plugin == nil || plugin.Status != types.PluginStatusActive || plugin.Origin != builtinInternalOrigin {
		return contract.SkillDownloadURL{}, fmt.Errorf("internal Skill %q is unavailable", code)
	}
	revision, err := infradb.GetCurrentPluginRevision(ctx, s.db, plugin)
	if err != nil || revision == nil {
		return contract.SkillDownloadURL{}, fmt.Errorf("internal Skill %q revision is unavailable", code)
	}
	artifact, err := ArtifactFromDefinition("skill", revision.Definition)
	if err != nil || artifact == nil {
		return contract.SkillDownloadURL{}, fmt.Errorf("internal Skill %q artifact is invalid", code)
	}
	sha, err := normalizedPluginSHA256(artifact.SHA256)
	if err != nil {
		return contract.SkillDownloadURL{}, err
	}
	file, err := infradb.GetSystemFileUploadByPublicID(ctx, s.db, artifact.FileUploadID)
	if err != nil || file == nil || file.Status != "active" || file.Purpose != filestore.PurposeArtifact {
		return contract.SkillDownloadURL{}, fmt.Errorf("internal Skill %q artifact is unavailable", code)
	}
	fileSHA, err := normalizedPluginSHA256(file.Sha256)
	if err != nil || fileSHA != sha || (artifact.SizeBytes > 0 && file.FileSize != artifact.SizeBytes) {
		return contract.SkillDownloadURL{}, fmt.Errorf("internal Skill %q artifact identity is invalid", code)
	}
	url, err := filestore.PresignDownloadForFileUpload(ctx, file, time.Hour)
	if err != nil {
		return contract.SkillDownloadURL{}, err
	}
	return contract.SkillDownloadURL{Code: code, Revision: revision.Revision, SHA256: sha, DownloadURL: url}, nil
}

func (s *pluginService) resolveConnectorSkillDownloads(
	ctx context.Context,
	orgID uint,
	refs []contract.ConnectorSkillRef,
) []contract.SkillDownloadURL {
	result := make([]contract.SkillDownloadURL, 0, len(refs))
	seen := make(map[string]bool)
	for _, ref := range refs {
		pluginID := strings.TrimSpace(ref.PluginID)
		if pluginID == "" || ref.Revision <= 0 {
			continue
		}
		plugin, err := infradb.GetPluginByPublicID(ctx, s.db, orgID, pluginID)
		if err != nil || plugin == nil || plugin.Kind != "mcp" ||
			plugin.Status != types.PluginStatusActive || plugin.CurrentRevision != ref.Revision {
			continue
		}
		revision, err := infradb.GetPluginRevisionByNumber(ctx, s.db, plugin.ID, ref.Revision)
		if err != nil || revision == nil || revision.SourcePluginRevisionID == nil {
			continue
		}
		definition, err := ConnectorFromDefinition(revision.Definition)
		if err != nil || definition == nil || definition.Skill == nil ||
			definition.Skill.Artifact == nil || seen[definition.Skill.Code] {
			continue
		}
		sourceRevision, err := infradb.GetPluginRevisionByID(ctx, s.db, *revision.SourcePluginRevisionID)
		if err != nil || sourceRevision == nil {
			continue
		}
		sourcePlugin, err := infradb.GetPluginByID(ctx, s.db, sourceRevision.PluginID)
		if err != nil || sourcePlugin == nil || sourcePlugin.OwnerScope != types.OwnerScopeSystem ||
			sourcePlugin.OrgID != 0 || sourcePlugin.Kind != "mcp" ||
			sourcePlugin.Origin != builtinConnectorOrigin || sourcePlugin.Code != definition.Channel {
			continue
		}
		sourceDefinition, err := ConnectorFromDefinition(sourceRevision.Definition)
		if err != nil || sourceDefinition == nil || sourceDefinition.Skill == nil ||
			sourceDefinition.Skill.Artifact == nil ||
			sourceDefinition.Skill.Code != definition.Skill.Code ||
			sourceDefinition.Skill.Artifact.FileUploadID != definition.Skill.Artifact.FileUploadID {
			continue
		}
		expectedSHA, err := normalizedPluginSHA256(definition.Skill.Artifact.SHA256)
		if err != nil {
			continue
		}
		sourceSHA, err := normalizedPluginSHA256(sourceDefinition.Skill.Artifact.SHA256)
		if err != nil || sourceSHA != expectedSHA {
			continue
		}
		file, err := infradb.GetSystemFileUploadByPublicID(ctx, s.db, definition.Skill.Artifact.FileUploadID)
		if err != nil || file == nil || file.Status != "active" || file.Purpose != filestore.PurposeArtifact {
			continue
		}
		fileSHA, err := normalizedPluginSHA256(file.Sha256)
		if err != nil || fileSHA != expectedSHA ||
			(definition.Skill.Artifact.SizeBytes > 0 && file.FileSize != definition.Skill.Artifact.SizeBytes) {
			continue
		}
		downloadURL, err := filestore.PresignDownloadForFileUpload(ctx, file, time.Hour)
		if err != nil {
			continue
		}
		result = append(result, contract.SkillDownloadURL{
			Code: definition.Skill.Code, Revision: definition.Skill.Revision,
			SHA256: expectedSHA, DownloadURL: downloadURL,
		})
		seen[definition.Skill.Code] = true
	}
	return result
}

func sameMarketplaceRevision(
	revision types.PluginRevision,
	item *types.PluginMarketplaceItem,
	sourceRevision *types.PluginRevision,
) bool {
	if item == nil || revision.SourceMarketplaceItemID == nil ||
		*revision.SourceMarketplaceItemID != item.ID ||
		sourceRevision == nil || revision.SourcePluginRevisionID == nil ||
		*revision.SourcePluginRevisionID != sourceRevision.ID {
		return false
	}
	currentArtifact, currentErr := ArtifactFromDefinition("skill", revision.Definition)
	itemArtifact, itemErr := ArtifactFromDefinition("skill", sourceRevision.Definition)
	if currentErr == nil && itemErr == nil && currentArtifact != nil && itemArtifact != nil {
		currentSHA, err := normalizedPluginSHA256(currentArtifact.SHA256)
		if err != nil {
			return false
		}
		itemSHA, err := normalizedPluginSHA256(itemArtifact.SHA256)
		return err == nil && currentSHA == itemSHA
	}
	return bytes.Equal(revision.Definition, sourceRevision.Definition)
}

func (s *pluginService) resolveSkillArtifactDownloadURL(
	ctx context.Context,
	orgID uint,
	row infradb.CurrentSkillArtifact,
	artifact *ArtifactDefinition,
	expectedSHA string,
) (string, error) {
	file, err := infradb.GetFileUploadByPublicID(ctx, s.db, orgID, artifact.FileUploadID)
	if err != nil {
		return "", err
	}
	if file == nil {
		if row.SourceMarketplaceItemID == nil || row.SourcePluginRevisionID == nil {
			return "", fmt.Errorf("organization Skill artifact not found")
		}
		source, err := infradb.GetPluginMarketplaceItemByIDIncludingDeleted(ctx, s.db, *row.SourceMarketplaceItemID)
		if err != nil {
			return "", err
		}
		sourceRevision, err := infradb.GetPluginRevisionByID(ctx, s.db, *row.SourcePluginRevisionID)
		if err != nil {
			return "", err
		}
		if source == nil || sourceRevision == nil || source.PluginID == 0 ||
			sourceRevision.PluginID != source.PluginID ||
			!strings.EqualFold(source.Kind, "skill") || source.Code != row.Code {
			return "", fmt.Errorf("marketplace Skill lineage is invalid")
		}
		sourcePlugin, err := infradb.GetPluginByID(ctx, s.db, sourceRevision.PluginID)
		if err != nil {
			return "", err
		}
		if sourcePlugin == nil || sourcePlugin.OwnerScope != types.OwnerScopeSystem ||
			sourcePlugin.OrgID != 0 || sourcePlugin.Origin != "builtin" ||
			sourcePlugin.Kind != source.Kind || sourcePlugin.Code != source.Code {
			return "", fmt.Errorf("marketplace Skill source plugin is invalid")
		}
		sourceArtifact, err := ArtifactFromDefinition("skill", sourceRevision.Definition)
		if err != nil || sourceArtifact == nil {
			return "", fmt.Errorf("marketplace Skill source artifact is invalid")
		}
		sourceSHA, err := normalizedPluginSHA256(sourceArtifact.SHA256)
		if err != nil || sourceArtifact.FileUploadID != artifact.FileUploadID ||
			sourceSHA != expectedSHA {
			return "", fmt.Errorf("marketplace Skill source artifact does not match installed revision")
		}
		file, err = infradb.GetSystemFileUploadByPublicID(ctx, s.db, artifact.FileUploadID)
		if err != nil {
			return "", err
		}
		if file == nil {
			return "", fmt.Errorf("marketplace Skill artifact not found")
		}
	}
	if !strings.EqualFold(file.Status, "active") {
		return "", fmt.Errorf("Skill artifact is not active")
	}
	if file.Purpose != filestore.PurposeArtifact &&
		!(file.OwnerScope == types.OwnerScopeOrganization && file.Purpose == filestore.PurposeSkillPackage) {
		return "", fmt.Errorf("Skill artifact purpose is invalid")
	}
	fileSHA, err := normalizedPluginSHA256(file.Sha256)
	if err != nil || fileSHA != expectedSHA {
		return "", fmt.Errorf("Skill artifact SHA-256 does not match definition")
	}
	if artifact.SizeBytes > 0 && file.FileSize != artifact.SizeBytes {
		return "", fmt.Errorf("Skill artifact size does not match definition")
	}
	return filestore.PresignDownloadForFileUpload(ctx, file, time.Hour)
}

func uniqueSkillCodes(values []string) []string {
	seen := make(map[string]struct{}, len(values))
	result := make([]string, 0, len(values))
	for _, value := range values {
		code := strings.TrimSpace(value)
		if code == "" {
			continue
		}
		if _, exists := seen[code]; exists {
			continue
		}
		seen[code] = struct{}{}
		result = append(result, code)
	}
	sort.Strings(result)
	return result
}

func normalizedPluginSHA256(value string) (string, error) {
	value = strings.ToLower(strings.TrimSpace(value))
	if len(value) != sha256.Size*2 {
		return "", fmt.Errorf("sha256 must be a %d-character hexadecimal value", sha256.Size*2)
	}
	for _, char := range value {
		if !((char >= '0' && char <= '9') || (char >= 'a' && char <= 'f')) {
			return "", fmt.Errorf("sha256 must be hexadecimal")
		}
	}
	return value, nil
}

func (s *pluginService) importSkillPluginFromGitHub(ctx context.Context, orgID, uin uint, githubURL string) (*contract.AddSkillPluginResponse, error) {
	if strings.TrimSpace(githubURL) == "" {
		return nil, fmt.Errorf("github_url is required")
	}
	skillID, version, err := parseGitHubSkillImportURL(githubURL)
	if err != nil {
		return nil, err
	}
	source := skillfetch.NewGitHubSource()
	var bundle *skillfetch.SkillBundle
	if strings.TrimSpace(version) == "" {
		bundle, err = source.Fetch(ctx, skillID)
	} else {
		bundle, err = source.FetchVersion(ctx, skillID, version)
	}
	if err != nil {
		return nil, fmt.Errorf("fetch GitHub skill: %w", err)
	}
	defer os.RemoveAll(bundle.TempDir)
	archive, err := skillcache.GenerateSkillZip(bundle.Content, bundle.Files)
	if err != nil {
		return nil, fmt.Errorf("package GitHub skill: %w", err)
	}
	if err := validateZipSkill(archive); err != nil {
		return nil, fmt.Errorf("validate GitHub skill package: %w", err)
	}
	code, _, _, err := pluginIdentityFromSkillArchive(archive)
	if err != nil {
		return nil, err
	}
	file, err := filestore.Upload(ctx, s.db, filestore.UploadParams{
		Data: archive, Filename: "skill-" + uuid.NewString() + ".zip", OriginalName: code + ".zip",
		MimeType: "application/zip", OwnerScope: types.OwnerScopeOrganization,
		OrgID: orgID, OwnerID: uin,
		ObjectKey: fmt.Sprintf("plugins/%d/skills/%s.zip", orgID, uuid.NewString()), Purpose: filestore.PurposeArtifact,
	})
	if err != nil {
		return nil, fmt.Errorf("store GitHub skill package: %w", err)
	}
	return s.AddSkillPlugin(ctx, orgID, uin, &contract.AddSkillPluginRequest{Mode: contract.SkillAddModeFile, FileUploadID: file.PublicID})
}

func pluginIdentityFromSkillArchive(archive []byte) (code, name, description string, err error) {
	reader, err := zip.NewReader(bytes.NewReader(archive), int64(len(archive)))
	if err != nil {
		return "", "", "", fmt.Errorf("open skill package: %w", err)
	}
	var skillDocument []byte
	for _, file := range reader.File {
		if file.FileInfo().IsDir() || !strings.EqualFold(file.Name[strings.LastIndex(file.Name, "/")+1:], "SKILL.md") {
			continue
		}
		if skillDocument != nil {
			return "", "", "", fmt.Errorf("skill package contains multiple SKILL.md files")
		}
		body, openErr := file.Open()
		if openErr != nil {
			return "", "", "", fmt.Errorf("open SKILL.md: %w", openErr)
		}
		skillDocument, err = io.ReadAll(io.LimitReader(body, 1_048_576))
		body.Close()
		if err != nil {
			return "", "", "", fmt.Errorf("read SKILL.md: %w", err)
		}
	}
	if skillDocument == nil {
		return "", "", "", fmt.Errorf("skill package does not contain SKILL.md")
	}
	manifest, _, err := skillcatalog.ParseDocument(skillDocument)
	if err != nil {
		return "", "", "", fmt.Errorf("parse SKILL.md: %w", err)
	}
	code = strings.TrimSpace(manifest.Name)
	if code == "" {
		return "", "", "", fmt.Errorf("SKILL.md name is required")
	}
	return code, code, strings.TrimSpace(manifest.Description), nil
}

// normalizeSkillArchive removes the parent path of the package's single SKILL.md
// when one exists, so stored plugin artifacts always install with SKILL.md at
// their ZIP root. It returns false when the input is already root-relative.
func normalizeSkillArchive(archive []byte) ([]byte, bool, error) {
	reader, err := zip.NewReader(bytes.NewReader(archive), int64(len(archive)))
	if err != nil {
		return nil, false, fmt.Errorf("open skill package: %w", err)
	}
	var skillFile *zip.File
	for _, file := range reader.File {
		if file.FileInfo().IsDir() || !strings.EqualFold(path.Base(file.Name), "SKILL.md") {
			continue
		}
		if skillFile != nil {
			return nil, false, fmt.Errorf("skill package contains multiple SKILL.md files")
		}
		skillFile = file
	}
	if skillFile == nil {
		return nil, false, fmt.Errorf("skill package does not contain SKILL.md")
	}
	skillRoot := path.Dir(skillFile.Name)
	if skillRoot == "." {
		return archive, false, nil
	}
	prefix := skillRoot + "/"
	files := make(map[string][]byte)
	var skillContent []byte
	for _, file := range reader.File {
		if file.FileInfo().IsDir() || !strings.HasPrefix(file.Name, prefix) {
			continue
		}
		relativeName := strings.TrimPrefix(file.Name, prefix)
		content, err := readSkillArchiveFile(file)
		if err != nil {
			return nil, false, err
		}
		if strings.EqualFold(relativeName, "SKILL.md") {
			skillContent = content
			continue
		}
		files[relativeName] = content
	}
	if skillContent == nil {
		return nil, false, fmt.Errorf("skill package does not contain root SKILL.md")
	}
	normalized, err := skillcache.GenerateSkillZip(skillContent, files)
	if err != nil {
		return nil, false, err
	}
	if err := validateZipSkill(normalized); err != nil {
		return nil, false, fmt.Errorf("validate normalized skill package: %w", err)
	}
	return normalized, true, nil
}

func readSkillArchiveFile(file *zip.File) ([]byte, error) {
	reader, err := file.Open()
	if err != nil {
		return nil, fmt.Errorf("open skill package entry %q: %w", file.Name, err)
	}
	defer reader.Close()
	content, err := io.ReadAll(reader)
	if err != nil {
		return nil, fmt.Errorf("read skill package entry %q: %w", file.Name, err)
	}
	return content, nil
}

func (s *pluginService) publishSkillRevision(
	ctx context.Context,
	orgID, uin uint,
	rawCode, rawName, description string,
	definition json.RawMessage,
	contentDraft *skillRevisionContentDraft,
) (*skillPublishResult, error) {
	return s.publishSkillRevisionWithScope(ctx, s.db, skillPublishRequest{
		OwnerScope:  types.OwnerScopeOrganization,
		OrgID:       orgID,
		ActorID:     uin,
		Origin:      "org",
		ActorType:   "user",
		Code:        rawCode,
		Name:        rawName,
		Description: description,
		Definition:  definition,
		Content:     contentDraft,
	})
}

type skillPublishRequest struct {
	OwnerScope  types.OwnerScope
	OrgID       uint
	ActorID     uint
	Origin      string
	ActorType   string
	Code        string
	Name        string
	Description string
	Definition  json.RawMessage
	Content     *skillRevisionContentDraft
}

type skillPublishResult struct {
	Plugin    *types.Plugin
	Revision  *types.PluginRevision
	Operation string
}

func (s *pluginService) publishSkillRevisionWithScope(
	ctx context.Context,
	database *gorm.DB,
	request skillPublishRequest,
) (*skillPublishResult, error) {
	if database == nil {
		return nil, fmt.Errorf("database is required")
	}
	request.OwnerScope = types.NormalizeOwnerScope(request.OwnerScope)
	if !types.ValidateOwnerScope(request.OwnerScope, request.OrgID) {
		return nil, fmt.Errorf("invalid Skill owner scope %q for org_id %d", request.OwnerScope, request.OrgID)
	}
	if request.OwnerScope == types.OwnerScopeSystem && request.ActorID != 0 {
		return nil, fmt.Errorf("system Skill actor_id must be zero")
	}
	if request.OwnerScope == types.OwnerScopeOrganization && request.ActorID == 0 {
		return nil, fmt.Errorf("organization Skill actor_id is required")
	}
	if request.ActorType == "" {
		if request.OwnerScope == types.OwnerScopeSystem {
			request.ActorType = "system"
		} else {
			request.ActorType = "user"
		}
	}
	if request.Origin == "" {
		if request.OwnerScope == types.OwnerScopeSystem {
			request.Origin = "builtin"
		} else {
			request.Origin = "org"
		}
	}
	if err := ValidatePluginDefinition("skill", request.Definition); err != nil {
		return nil, err
	}
	if request.Content == nil {
		return nil, fmt.Errorf("skill revision content is required")
	}
	artifact, err := ArtifactFromDefinition("skill", request.Definition)
	if err != nil || artifact == nil {
		return nil, fmt.Errorf("skill artifact is required")
	}
	artifactSHA, err := normalizedPluginSHA256(artifact.SHA256)
	if err != nil {
		return nil, err
	}
	if artifactSHA != request.Content.ArtifactSHA256 {
		return nil, fmt.Errorf("skill content artifact SHA-256 does not match definition")
	}
	code, name := strings.TrimSpace(request.Code), strings.TrimSpace(request.Name)
	if code == "" || name == "" {
		return nil, fmt.Errorf("code and name are required")
	}

	var result *skillPublishResult
	err = database.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var plugin types.Plugin
		find := tx.Unscoped().
			Clauses(clause.Locking{Strength: "UPDATE"}).
			Where("owner_scope = ? AND org_id = ? AND kind = ? AND code = ?",
				request.OwnerScope, request.OrgID, "skill", code).
			Order("id DESC").
			First(&plugin)
		if find.Error != nil && !errors.Is(find.Error, gorm.ErrRecordNotFound) {
			return find.Error
		}
		created := errors.Is(find.Error, gorm.ErrRecordNotFound)
		restored := false
		if created {
			plugin = types.Plugin{
				PublicID: "plugin_" + uuid.NewString(), OwnerScope: request.OwnerScope,
				OrgID: request.OrgID, Code: code, Kind: "skill", Name: name,
				Description: request.Description, Visibility: types.PluginVisibilityPrivate,
				Status: types.PluginStatusActive,
				Origin: request.Origin, CreatedBy: request.ActorID, UpdatedBy: request.ActorID,
			}
			insert := tx.WithContext(ctx).
				Clauses(clause.OnConflict{DoNothing: true}).
				Create(&plugin)
			if insert.Error != nil {
				return insert.Error
			}
			if insert.RowsAffected == 0 {
				if err := tx.Unscoped().
					Clauses(clause.Locking{Strength: "UPDATE"}).
					Where("owner_scope = ? AND org_id = ? AND kind = ? AND code = ?",
						request.OwnerScope, request.OrgID, "skill", code).
					Order("id DESC").
					First(&plugin).Error; err != nil {
					return fmt.Errorf("reload concurrently created Skill plugin: %w", err)
				}
				created = false
			}
		}
		if !created {
			if plugin.Kind != "skill" {
				return fmt.Errorf("plugin kind cannot change")
			}
			if plugin.DeletedAt.Valid {
				if err := infradb.RestorePlugin(ctx, tx, plugin.ID, request.ActorID); err != nil {
					return err
				}
				plugin.DeletedAt = gorm.DeletedAt{}
				restored = true
			}
			if err := tx.Model(&types.Plugin{}).Where("id = ?", plugin.ID).
				Select("name", "description", "status", "origin", "updated_by").
				Updates(types.Plugin{
					Name: name, Description: request.Description, Status: types.PluginStatusActive,
					Origin: request.Origin, UpdatedBy: request.ActorID,
				}).Error; err != nil {
				return err
			}
			plugin.Name, plugin.Description = name, request.Description
			plugin.Status, plugin.Origin, plugin.UpdatedBy =
				types.PluginStatusActive, request.Origin, request.ActorID
		}

		if request.OwnerScope == types.OwnerScopeOrganization {
			if created || restored {
				// 新建或恢复时由当前操作者成为唯一 owner。
				if err := ensurePluginResourceOwner(ctx, tx, request.OrgID, plugin.ID, request.ActorID); err != nil {
					return err
				}
			} else if err := newPluginAccessManager(tx).RequireUpdatePermission(ctx, request.OrgID, request.ActorID, &plugin); err != nil {
				// 更新已有 code 时校验 update 权限并保留原 owner。
				return err
			}
		}

		var revisions []types.PluginRevision
		if err := tx.Where("plugin_id = ?", plugin.ID).
			Order("revision DESC").Find(&revisions).Error; err != nil {
			return err
		}
		current, err := infradb.GetCurrentPluginRevision(ctx, tx, &plugin)
		if err != nil {
			return err
		}
		if !restored && sameSkillArtifact(current, request.Definition) {
			result = &skillPublishResult{Plugin: &plugin, Revision: current, Operation: "unchanged"}
			return nil
		}
		nextRevision := 1
		for _, existing := range revisions {
			if existing.Revision >= nextRevision {
				nextRevision = existing.Revision + 1
			}
		}
		revision := &types.PluginRevision{
			PluginID: plugin.ID, Revision: nextRevision, Status: "published",
			Definition:      append(json.RawMessage(nil), request.Definition...),
			PublishedByType: request.ActorType, PublishedByID: request.ActorID,
			PublishedAt: time.Now(),
		}
		if err := infradb.CreatePluginRevision(ctx, tx, revision); err != nil {
			return err
		}
		if err := infradb.CreatePluginRevisionContent(ctx, tx, request.Content.model(revision.ID)); err != nil {
			return err
		}
		if err := infradb.SetPluginCurrentRevision(
			ctx, tx, plugin.ID, uint(revision.Revision), request.ActorID,
		); err != nil {
			return err
		}
		plugin.CurrentRevision = revision.Revision
		operation := "updated"
		if created {
			operation = "created"
		}
		result = &skillPublishResult{Plugin: &plugin, Revision: revision, Operation: operation}
		return nil
	})
	return result, err
}

func sameSkillArtifact(revision *types.PluginRevision, definition json.RawMessage) bool {
	if revision == nil {
		return false
	}
	current, err := ArtifactFromDefinition("skill", revision.Definition)
	if err != nil || current == nil {
		return false
	}
	next, err := ArtifactFromDefinition("skill", definition)
	if err != nil || next == nil {
		return false
	}
	currentSHA, err := normalizedPluginSHA256(current.SHA256)
	if err != nil {
		return false
	}
	nextSHA, err := normalizedPluginSHA256(next.SHA256)
	return err == nil && currentSHA == nextSHA
}

func normalizePluginListLimit(limit int) int {
	if limit <= 0 {
		return defaultPluginListLimit
	}
	if limit > types.PageMaxCount {
		return types.PageMaxCount
	}
	return limit
}

func pluginView(plugin types.Plugin) contract.PluginView {
	return pluginViewWithRole(plugin, "")
}

func pluginViewWithRole(plugin types.Plugin, role types.ResourceRole) contract.PluginView {
	view := contract.PluginView{
		PublicID:        plugin.PublicID,
		Code:            plugin.Code,
		Kind:            plugin.Kind,
		Name:            plugin.Name,
		Description:     plugin.Description,
		Visibility:      string(plugin.Visibility),
		Status:          plugin.Status,
		Origin:          plugin.Origin,
		CurrentRevision: plugin.CurrentRevision,
	}
	if role != "" {
		view.Permission = &contract.PluginPermission{Role: role}
	}
	return view
}

func officialPluginMarketplaceItemView(
	item types.PluginMarketplaceItem,
	source *types.Plugin,
	revision *types.PluginRevision,
	content *contract.PluginRevisionContentView,
) contract.OfficialPluginMarketplaceItemView {
	version := ""
	if revision != nil {
		version = strconv.Itoa(revision.Revision)
	} else if source != nil && source.CurrentRevision > 0 {
		version = strconv.Itoa(source.CurrentRevision)
	}
	return contract.OfficialPluginMarketplaceItemView{
		PublicID: item.PublicID, Code: item.Code, Kind: item.Kind, Name: item.Name, Description: item.Description,
		Author: item.Author, Version: version, Category: item.Category, Tags: []string(item.Tags),
		Icon: item.Icon, Verified: item.Verified, Content: content,
	}
}

func (s *pluginService) organizationMarketplaceItemView(
	ctx context.Context,
	item *types.PluginMarketplaceItem,
	state *infradb.OrganizationPluginMarketplaceState,
	includeContent bool,
) (contract.OfficialPluginMarketplaceItemView, bool, error) {
	if item == nil {
		return contract.OfficialPluginMarketplaceItemView{}, false, nil
	}
	installed := marketplaceStateMatchesItem(state, item)
	marketplaceAvailable := !item.DeletedAt.Valid && item.Status == "published"

	var latestSource *types.Plugin
	var latestRevision *types.PluginRevision
	var latestContent *types.PluginRevisionContent
	if marketplaceAvailable {
		var err error
		latestSource, latestRevision, latestContent, err = loadMarketplaceSource(
			ctx,
			s.db,
			item,
			includeContent && !installed,
		)
		if err != nil {
			if !installed {
				return contract.OfficialPluginMarketplaceItemView{}, false, nil
			}
			marketplaceAvailable = false
			latestSource, latestRevision, latestContent = nil, nil, nil
		}
	}
	if !marketplaceAvailable && !installed {
		return contract.OfficialPluginMarketplaceItemView{}, false, nil
	}

	var contentView *contract.PluginRevisionContentView
	if installed && includeContent {
		revision, err := infradb.GetPluginRevisionByID(ctx, s.db, state.RevisionID)
		if err != nil {
			return contract.OfficialPluginMarketplaceItemView{}, false, err
		}
		content, err := infradb.GetPluginRevisionContent(ctx, s.db, state.RevisionID)
		if err != nil {
			return contract.OfficialPluginMarketplaceItemView{}, false, err
		}
		contentView, err = pluginRevisionContentView(revision, content)
		if err != nil {
			return contract.OfficialPluginMarketplaceItemView{}, false, err
		}
		if contentView != nil {
			contentView.Version = state.SourceMarketplaceVersion
		}
	} else if includeContent {
		var err error
		contentView, err = pluginRevisionContentView(latestRevision, latestContent)
		if err != nil {
			return contract.OfficialPluginMarketplaceItemView{}, false, err
		}
	}

	view := officialPluginMarketplaceItemView(*item, latestSource, latestRevision, contentView)
	view.MarketplaceAvailable = marketplaceAvailable
	view.OrganizationOverride = state != nil && !installed
	if latestRevision != nil {
		view.LatestVersion = strconv.Itoa(latestRevision.Revision)
	}
	if installed {
		view.Installed = true
		view.InstalledPluginID = state.PluginPublicID
		view.Name = state.Name
		view.Description = state.Description
		view.Version = strconv.Itoa(state.SourceMarketplaceVersion)
		view.UpdateAvailable = marketplaceAvailable && latestRevision != nil &&
			latestRevision.Revision > state.SourceMarketplaceVersion
	}
	return view, true, nil
}

func marketplaceStateMatchesItem(
	state *infradb.OrganizationPluginMarketplaceState,
	item *types.PluginMarketplaceItem,
) bool {
	return state != nil && item != nil &&
		state.SourceMarketplaceItemID != nil &&
		*state.SourceMarketplaceItemID == item.ID &&
		state.SourcePluginRevisionID != nil &&
		state.SourceMarketplaceVersion > 0 &&
		state.SourcePluginID == item.PluginID &&
		state.Kind == item.Kind &&
		state.Code == item.Code
}

func marketplaceItemMatchesFilter(
	item types.PluginMarketplaceItem,
	req *contract.ListOfficialPluginMarketplaceItemsRequest,
) bool {
	if req == nil {
		return true
	}
	if kind := strings.TrimSpace(req.Kind); kind != "" && item.Kind != kind {
		return false
	}
	if category := strings.TrimSpace(req.Category); category != "" && item.Category != category {
		return false
	}
	keyword := strings.ToLower(strings.TrimSpace(req.Keyword))
	if keyword == "" {
		return true
	}
	return strings.Contains(strings.ToLower(item.Code), keyword) ||
		strings.Contains(strings.ToLower(item.Name), keyword) ||
		strings.Contains(strings.ToLower(item.Description), keyword)
}

func pluginIdentityKey(kind, code string) string {
	return kind + "\x00" + code
}

type marketplaceRelease struct {
	Item     *types.PluginMarketplaceItem
	Plugin   *types.Plugin
	Revision *types.PluginRevision
}

func loadPublishedMarketplaceReleaseByIdentity(
	ctx context.Context,
	database *gorm.DB,
	kind, code string,
) (*marketplaceRelease, error) {
	item, err := infradb.GetPublishedPluginMarketplaceItemByIdentity(
		ctx,
		database,
		kind,
		code,
	)
	if err != nil {
		return nil, err
	}
	if item == nil {
		return nil, nil
	}
	plugin, revision, _, err := loadMarketplaceSource(ctx, database, item, false)
	if err != nil {
		return nil, err
	}
	return &marketplaceRelease{Item: item, Plugin: plugin, Revision: revision}, nil
}

func loadMarketplaceSource(
	ctx context.Context,
	database *gorm.DB,
	item *types.PluginMarketplaceItem,
	requireContent bool,
) (*types.Plugin, *types.PluginRevision, *types.PluginRevisionContent, error) {
	if item == nil || item.PluginID == 0 {
		return nil, nil, nil, fmt.Errorf("marketplace item has no source plugin")
	}
	plugin, err := infradb.GetPluginByID(ctx, database, item.PluginID)
	if err != nil {
		return nil, nil, nil, err
	}
	if plugin == nil || plugin.OwnerScope != types.OwnerScopeSystem || plugin.OrgID != 0 ||
		plugin.Origin != "builtin" || plugin.Kind != item.Kind || plugin.Code != item.Code {
		return nil, nil, nil, fmt.Errorf("marketplace source plugin is invalid")
	}
	revision, err := infradb.GetCurrentPluginRevision(ctx, database, plugin)
	if err != nil {
		return nil, nil, nil, err
	}
	if revision == nil || revision.Status != "published" {
		return nil, nil, nil, fmt.Errorf("marketplace source revision is unavailable")
	}
	if err := ValidatePluginDefinition(plugin.Kind, revision.Definition); err != nil {
		return nil, nil, nil, fmt.Errorf("invalid marketplace source definition: %w", err)
	}
	artifact, err := ArtifactFromDefinition(plugin.Kind, revision.Definition)
	if err != nil || artifact == nil {
		return nil, nil, nil, fmt.Errorf("marketplace source artifact is required")
	}
	expectedSHA, err := normalizedPluginSHA256(artifact.SHA256)
	if err != nil {
		return nil, nil, nil, err
	}
	file, err := infradb.GetSystemFileUploadByPublicID(ctx, database, artifact.FileUploadID)
	if err != nil {
		return nil, nil, nil, err
	}
	if file == nil || file.OwnerScope != types.OwnerScopeSystem || file.OrgID != 0 ||
		file.OwnerID != 0 || file.Purpose != filestore.PurposeArtifact ||
		!strings.EqualFold(file.Status, "active") {
		return nil, nil, nil, fmt.Errorf("marketplace source artifact is unavailable")
	}
	fileSHA, err := normalizedPluginSHA256(file.Sha256)
	if err != nil || fileSHA != expectedSHA {
		return nil, nil, nil, fmt.Errorf("marketplace source artifact SHA-256 does not match definition")
	}
	if artifact.SizeBytes > 0 && file.FileSize != artifact.SizeBytes {
		return nil, nil, nil, fmt.Errorf("marketplace source artifact size does not match definition")
	}
	if !requireContent {
		return plugin, revision, nil, nil
	}
	content, err := infradb.GetPluginRevisionContent(ctx, database, revision.ID)
	if err != nil {
		return nil, nil, nil, err
	}
	if content == nil || content.ArtifactSHA256 != expectedSHA {
		return nil, nil, nil, fmt.Errorf("marketplace source content is unavailable")
	}
	return plugin, revision, content, nil
}

func pluginRevisionContentView(
	revision *types.PluginRevision,
	content *types.PluginRevisionContent,
) (*contract.PluginRevisionContentView, error) {
	if revision == nil || content == nil {
		return nil, nil
	}
	_, body, err := skillcatalog.ParseDocument([]byte(content.EntrypointContent))
	if err != nil {
		return nil, fmt.Errorf("parse stored SKILL.md: %w", err)
	}
	files := make([]contract.PluginRevisionFileView, 0, len(content.FileIndex))
	for _, file := range content.FileIndex {
		files = append(files, contract.PluginRevisionFileView{
			Path: file.Path, SizeBytes: file.SizeBytes, SHA256: file.SHA256,
		})
	}
	return &contract.PluginRevisionContentView{
		Schema: content.Schema, Version: revision.Revision,
		EntrypointPath: content.EntrypointPath, SkillMD: body, Files: files,
	}, nil
}

func pluginRevisionView(revision types.PluginRevision) contract.PluginRevisionView {
	return contract.PluginRevisionView{
		Revision:        revision.Revision,
		Status:          revision.Status,
		PublishedByType: revision.PublishedByType,
		PublishedByID:   revision.PublishedByID,
		PublishedAt:     revision.PublishedAt,
	}
}

var _ contract.PluginService = (*pluginService)(nil)
var _ contract.OfficialPluginMarketplaceService = (*pluginService)(nil)
