package db

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"

	"github.com/insmtx/Leros/backend/types"
)

// PluginExecutionSnapshot is the database projection used to freeze a run's plugins.
type PluginExecutionSnapshot struct {
	PluginPublicID string
	Code           string
	Kind           string
	Revision       int
	Definition     json.RawMessage
}

// CurrentSkillArtifact is the server-side projection needed to create a worker download URL.
type CurrentSkillArtifact struct {
	Code                    string
	Revision                int
	Definition              json.RawMessage
	SourceMarketplaceItemID *uint
	SourcePluginRevisionID  *uint
}

// ListCurrentSkillArtifacts returns current active Skill revisions visible to one organization.
func ListCurrentSkillArtifacts(ctx context.Context, database *gorm.DB, orgID uint, codes []string) ([]CurrentSkillArtifact, error) {
	if len(codes) == 0 {
		return []CurrentSkillArtifact{}, nil
	}
	var rows []CurrentSkillArtifact
	err := database.WithContext(ctx).Table(types.TableNamePlugin+" AS p").
		Select("p.code, r.revision, r.definition, r.source_marketplace_item_id, r.source_plugin_revision_id").
		Joins("JOIN "+types.TableNamePluginRevision+" AS r ON r.plugin_id = p.id AND r.revision = p.current_revision AND r.deleted_at IS NULL").
		Where("p.owner_scope = ? AND p.org_id = ? AND p.kind = ? AND p.status = ? AND p.deleted_at IS NULL AND p.code IN ?",
			types.OwnerScopeOrganization, orgID, "skill", types.PluginStatusActive, codes).
		Order("p.code ASC").Find(&rows).Error
	return rows, err
}

// ListProjectPluginSnapshots returns active bindings with their current immutable revision.
func ListProjectPluginSnapshots(ctx context.Context, database *gorm.DB, orgID, projectID uint) ([]PluginExecutionSnapshot, error) {
	var rows []PluginExecutionSnapshot
	err := database.WithContext(ctx).Table(types.TableNameProjectPluginBinding+" AS b").
		Select("p.public_id AS plugin_public_id, p.code, p.kind, r.revision, r.definition").
		Joins("JOIN "+types.TableNamePlugin+" AS p ON p.id = b.plugin_id AND p.deleted_at IS NULL").
		Joins("JOIN "+types.TableNamePluginRevision+" AS r ON r.plugin_id = p.id AND r.revision = p.current_revision AND r.deleted_at IS NULL").
		Where("b.project_id = ? AND b.enabled = ? AND b.deleted_at IS NULL AND p.owner_scope = ? AND p.org_id = ? AND p.status = ?",
			projectID, true, types.OwnerScopeOrganization, orgID, types.PluginStatusActive).
		Order("p.code ASC, p.public_id ASC").Find(&rows).Error
	return rows, err
}

// PluginListFilter constrains an organization plugin list query.
type PluginListFilter struct {
	Kind                    string
	Status                  string
	Keyword                 string
	Limit                   int
	ExcludeMarketplaceBased bool
	ViewerUin               uint
}

// PluginMarketplaceListFilter constrains a marketplace list query.
type PluginMarketplaceListFilter struct {
	Kind     string
	Category string
	Keyword  string
	Limit    int
}

// OrganizationPluginMarketplaceState projects one active organization plugin
// and the marketplace lineage of its current revision.
type OrganizationPluginMarketplaceState struct {
	PluginID                 uint
	PluginPublicID           string
	Kind                     string
	Code                     string
	Name                     string
	Description              string
	CurrentRevision          int
	RevisionID               uint
	SourceMarketplaceItemID  *uint
	SourcePluginRevisionID   *uint
	SourceMarketplaceVersion int
	SourcePluginID           uint
}

// CreatePlugin inserts one validated scope-owned plugin identity.
func CreatePlugin(ctx context.Context, database *gorm.DB, plugin *types.Plugin) error {
	plugin.OwnerScope = types.NormalizeOwnerScope(plugin.OwnerScope)
	if !types.ValidateOwnerScope(plugin.OwnerScope, plugin.OrgID) {
		return fmt.Errorf("invalid plugin owner scope %q for org_id %d", plugin.OwnerScope, plugin.OrgID)
	}
	return database.WithContext(ctx).Create(plugin).Error
}

// GetPluginByPublicID returns one non-deleted plugin in an organization.
func GetPluginByPublicID(ctx context.Context, database *gorm.DB, orgID uint, publicID string) (*types.Plugin, error) {
	var plugin types.Plugin
	err := database.WithContext(ctx).
		Where("owner_scope = ? AND org_id = ? AND public_id = ?", types.OwnerScopeOrganization, orgID, publicID).
		First(&plugin).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &plugin, nil
}

// GetOrganizationPluginByIdentity returns one active organization plugin by its stable kind and code.
func GetOrganizationPluginByIdentity(
	ctx context.Context,
	database *gorm.DB,
	orgID uint,
	kind, code string,
) (*types.Plugin, error) {
	var plugin types.Plugin
	err := database.WithContext(ctx).
		Where(
			"owner_scope = ? AND org_id = ? AND kind = ? AND code = ? AND status = ?",
			types.OwnerScopeOrganization,
			orgID,
			kind,
			code,
			types.PluginStatusActive,
		).
		First(&plugin).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &plugin, nil
}

// ListPlugins returns organization plugins ordered for stable API responses.
func ListPlugins(ctx context.Context, database *gorm.DB, orgID uint, filter PluginListFilter) ([]types.Plugin, error) {
	query := database.WithContext(ctx).
		Table(types.TableNamePlugin+" AS p").
		Select("p.*").
		Where(
			"p.owner_scope = ? AND p.org_id = ? AND p.deleted_at IS NULL",
			types.OwnerScopeOrganization,
			orgID,
		)
	if kind := strings.TrimSpace(filter.Kind); kind != "" {
		query = query.Where("p.kind = ?", kind)
	}
	if filter.ViewerUin > 0 {
		switch strings.TrimSpace(filter.Kind) {
		case "mcp":
			query = query.Where("p.created_by = ?", filter.ViewerUin)
		case "":
			query = query.Where("p.kind <> ? OR p.created_by = ?", "mcp", filter.ViewerUin)
		}
	}
	if status := strings.TrimSpace(filter.Status); status != "" {
		query = query.Where("p.status = ?", status)
	}
	if keyword := strings.TrimSpace(filter.Keyword); keyword != "" {
		like := "%" + keyword + "%"
		query = query.Where("p.code LIKE ? OR p.name LIKE ? OR p.description LIKE ?", like, like, like)
	}
	if filter.ExcludeMarketplaceBased {
		query = query.Where(
			"NOT EXISTS (SELECT 1 FROM " + types.TableNamePluginRevision + " AS current_revision " +
				"WHERE current_revision.plugin_id = p.id AND current_revision.revision = p.current_revision " +
				"AND current_revision.deleted_at IS NULL AND current_revision.source_marketplace_item_id IS NOT NULL " +
				"AND current_revision.source_plugin_revision_id IS NOT NULL)",
		)
	}
	if filter.Limit > 0 {
		query = query.Limit(filter.Limit)
	}

	var plugins []types.Plugin
	if err := query.Order("p.kind ASC, p.code ASC, p.public_id ASC").Find(&plugins).Error; err != nil {
		return nil, err
	}
	return plugins, nil
}

// ListOrganizationPluginMarketplaceStates returns active organization plugins
// with their current revision lineage in one query.
func ListOrganizationPluginMarketplaceStates(
	ctx context.Context,
	database *gorm.DB,
	orgID uint,
	kind string,
) ([]OrganizationPluginMarketplaceState, error) {
	query := database.WithContext(ctx).
		Table(types.TableNamePlugin+" AS p").
		Select(
			"p.id AS plugin_id, p.public_id AS plugin_public_id, p.kind, p.code, p.name, p.description, "+
				"p.current_revision, current_revision.id AS revision_id, "+
				"current_revision.source_marketplace_item_id, current_revision.source_plugin_revision_id, "+
				"source_revision.revision AS source_marketplace_version, source_revision.plugin_id AS source_plugin_id",
		).
		Joins(
			"JOIN "+types.TableNamePluginRevision+" AS current_revision "+
				"ON current_revision.plugin_id = p.id AND current_revision.revision = p.current_revision "+
				"AND current_revision.deleted_at IS NULL",
		).
		Joins(
			"LEFT JOIN "+types.TableNamePluginRevision+" AS source_revision "+
				"ON source_revision.id = current_revision.source_plugin_revision_id "+
				"AND source_revision.deleted_at IS NULL",
		).
		Where(
			"p.owner_scope = ? AND p.org_id = ? AND p.status = ? AND p.deleted_at IS NULL",
			types.OwnerScopeOrganization,
			orgID,
			types.PluginStatusActive,
		)
	if kind = strings.TrimSpace(kind); kind != "" {
		query = query.Where("p.kind = ?", kind)
	}
	var states []OrganizationPluginMarketplaceState
	if err := query.Order("p.kind ASC, p.code ASC, p.public_id ASC").Find(&states).Error; err != nil {
		return nil, err
	}
	return states, nil
}

// CreatePluginRevision inserts an immutable plugin revision.
func CreatePluginRevision(ctx context.Context, database *gorm.DB, revision *types.PluginRevision) error {
	return database.WithContext(ctx).Create(revision).Error
}

// GetPluginByID returns one plugin without applying organization visibility.
func GetPluginByID(ctx context.Context, database *gorm.DB, pluginID uint) (*types.Plugin, error) {
	var plugin types.Plugin
	err := database.WithContext(ctx).Where("id = ?", pluginID).First(&plugin).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &plugin, nil
}

// GetSystemPluginByCode returns one active system plugin identity.
func GetSystemPluginByCode(ctx context.Context, database *gorm.DB, kind, code string) (*types.Plugin, error) {
	var plugin types.Plugin
	err := database.WithContext(ctx).
		Where("owner_scope = ? AND org_id = 0 AND kind = ? AND code = ?", types.OwnerScopeSystem, kind, code).
		First(&plugin).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &plugin, nil
}

// ListSystemPluginsByOrigin returns all system-scope plugins matching kind and origin,
// ordered by code. Does NOT include gorm-soft-deleted records.
func ListSystemPluginsByOrigin(ctx context.Context, database *gorm.DB, kind, origin string) ([]types.Plugin, error) {
	var plugins []types.Plugin
	err := database.WithContext(ctx).
		Where("owner_scope = ? AND org_id = 0 AND kind = ? AND origin = ?", types.OwnerScopeSystem, kind, origin).
		Order("code ASC").
		Find(&plugins).Error
	if err != nil {
		return nil, err
	}
	return plugins, nil
}

// ArchivePlugin sets a plugin's status to archived by ID.
func ArchivePlugin(ctx context.Context, database *gorm.DB, pluginID uint) error {
	return database.WithContext(ctx).
		Model(&types.Plugin{}).
		Where("id = ?", pluginID).
		Select("status").
		Updates(types.Plugin{Status: types.PluginStatusArchived}).Error
}

// ListActiveSystemPluginsByOrigin returns active system-scope plugins
// matching kind and origin, ordered by code.
func ListActiveSystemPluginsByOrigin(ctx context.Context, database *gorm.DB, kind, origin string) ([]types.Plugin, error) {
	var plugins []types.Plugin
	err := database.WithContext(ctx).
		Where("owner_scope = ? AND org_id = 0 AND kind = ? AND origin = ? AND status = ? AND current_revision > 0",
			types.OwnerScopeSystem, kind, origin, types.PluginStatusActive).
		Order("code ASC").
		Find(&plugins).Error
	if err != nil {
		return nil, err
	}
	return plugins, nil
}

// GetPluginRevisionByID returns one immutable revision.
func GetPluginRevisionByID(ctx context.Context, database *gorm.DB, revisionID uint) (*types.PluginRevision, error) {
	var revision types.PluginRevision
	err := database.WithContext(ctx).Where("id = ?", revisionID).First(&revision).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &revision, nil
}

// GetPluginRevisionByNumber returns one immutable revision by plugin identity and revision number.
func GetPluginRevisionByNumber(
	ctx context.Context,
	database *gorm.DB,
	pluginID uint,
	revisionNumber int,
) (*types.PluginRevision, error) {
	var revision types.PluginRevision
	err := database.WithContext(ctx).
		Where("plugin_id = ? AND revision = ?", pluginID, revisionNumber).
		First(&revision).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &revision, nil
}

// GetCurrentPluginRevision returns the current immutable revision for one plugin.
func GetCurrentPluginRevision(
	ctx context.Context,
	database *gorm.DB,
	plugin *types.Plugin,
) (*types.PluginRevision, error) {
	if plugin == nil || plugin.CurrentRevision <= 0 {
		return nil, nil
	}
	return GetPluginRevisionByNumber(ctx, database, plugin.ID, plugin.CurrentRevision)
}

// CreatePluginRevisionContent inserts one immutable revision content snapshot.
func CreatePluginRevisionContent(ctx context.Context, database *gorm.DB, content *types.PluginRevisionContent) error {
	return database.WithContext(ctx).Create(content).Error
}

// GetPluginRevisionContent returns the content snapshot for one immutable revision.
func GetPluginRevisionContent(
	ctx context.Context,
	database *gorm.DB,
	revisionID uint,
) (*types.PluginRevisionContent, error) {
	var content types.PluginRevisionContent
	err := database.WithContext(ctx).
		Where("plugin_revision_id = ?", revisionID).
		First(&content).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &content, nil
}

// ListPluginRevisions returns revisions newest first for a plugin in an organization.
func ListPluginRevisions(ctx context.Context, database *gorm.DB, orgID uint, pluginPublicID string) ([]types.PluginRevision, error) {
	var revisions []types.PluginRevision
	err := database.WithContext(ctx).
		Table(types.TableNamePluginRevision+" AS r").
		Select("r.*").
		Joins("JOIN "+types.TableNamePlugin+" AS p ON p.id = r.plugin_id").
		Where("p.owner_scope = ? AND p.org_id = ? AND p.public_id = ? AND p.deleted_at IS NULL",
			types.OwnerScopeOrganization, orgID, pluginPublicID).
		Order("r.revision DESC").
		Find(&revisions).Error
	if err != nil {
		return nil, err
	}
	return revisions, nil
}

// RestorePlugin reactivates a soft-deleted plugin while preserving its public identity.
func RestorePlugin(ctx context.Context, database *gorm.DB, pluginID, updatedBy uint) error {
	return database.WithContext(ctx).
		Unscoped().
		Model(&types.Plugin{}).
		Where("id = ?", pluginID).
		Updates(map[string]interface{}{
			"deleted_at": nil,
			"status":     types.PluginStatusActive,
			"updated_by": updatedBy,
		}).Error
}

// SetPluginCurrentRevision updates the current revision number and audit actor.
func SetPluginCurrentRevision(ctx context.Context, database *gorm.DB, pluginID, revision, updatedBy uint) error {
	return database.WithContext(ctx).
		Model(&types.Plugin{}).
		Where("id = ?", pluginID).
		Select("current_revision", "updated_by").
		Updates(types.Plugin{CurrentRevision: int(revision), UpdatedBy: updatedBy}).Error
}

// SoftDeletePlugin logically deletes one organization plugin and records the actor.
func SoftDeletePlugin(ctx context.Context, database *gorm.DB, orgID uint, publicID string, updatedBy uint) (bool, error) {
	result := database.WithContext(ctx).
		Model(&types.Plugin{}).
		Where("owner_scope = ? AND org_id = ? AND public_id = ?", types.OwnerScopeOrganization, orgID, publicID).
		Select("deleted_at", "updated_by").
		Updates(types.Plugin{
			Model:     gorm.Model{DeletedAt: gorm.DeletedAt{Time: time.Now(), Valid: true}},
			UpdatedBy: updatedBy,
		})
	if result.Error != nil {
		return false, result.Error
	}
	return result.RowsAffected > 0, nil
}

// CreateProjectPluginBinding inserts a project authorization for a plugin.
func CreateProjectPluginBinding(ctx context.Context, database *gorm.DB, binding *types.ProjectPluginBinding) error {
	return database.WithContext(ctx).Create(binding).Error
}

// RemoveProjectPluginBinding soft-deletes one active project plugin binding.
func RemoveProjectPluginBinding(ctx context.Context, database *gorm.DB, projectID, pluginID uint) (bool, error) {
	result := database.WithContext(ctx).
		Where("project_id = ? AND plugin_id = ?", projectID, pluginID).
		Delete(&types.ProjectPluginBinding{})
	if result.Error != nil {
		return false, result.Error
	}
	return result.RowsAffected > 0, nil
}

// RemoveProjectPluginBindingsByPlugin soft-deletes every active project binding for a plugin.
func RemoveProjectPluginBindingsByPlugin(ctx context.Context, database *gorm.DB, pluginID uint) error {
	return database.WithContext(ctx).
		Where("plugin_id = ?", pluginID).
		Delete(&types.ProjectPluginBinding{}).Error
}

// ListProjectPlugins returns active organization plugins authorized for a project.
func ListProjectPlugins(ctx context.Context, database *gorm.DB, orgID, projectID uint, kind string) ([]types.Plugin, error) {
	query := database.WithContext(ctx).Table(types.TableNameProjectPluginBinding+" AS b").
		Select("p.*").
		Joins("JOIN "+types.TableNamePlugin+" AS p ON p.id = b.plugin_id").
		Where("b.project_id = ? AND b.enabled = ? AND b.deleted_at IS NULL AND p.owner_scope = ? AND p.org_id = ? AND p.status = ? AND p.deleted_at IS NULL",
			projectID, true, types.OwnerScopeOrganization, orgID, types.PluginStatusActive)
	if strings.TrimSpace(kind) != "" {
		query = query.Where("p.kind = ?", strings.TrimSpace(kind))
	}
	var plugins []types.Plugin
	if err := query.Order("p.kind ASC, p.code ASC, p.public_id ASC").Find(&plugins).Error; err != nil {
		return nil, err
	}
	return plugins, nil
}

// CreatePluginMarketplaceItem inserts a marketplace directory item.
func CreatePluginMarketplaceItem(ctx context.Context, database *gorm.DB, item *types.PluginMarketplaceItem) error {
	if err := validatePluginMarketplaceSource(ctx, database, item); err != nil {
		return err
	}
	return database.WithContext(ctx).Create(item).Error
}

// CreatePluginMarketplaceItemIfAbsent inserts a directory item without failing
// when another server process concurrently creates the same stable source.
func CreatePluginMarketplaceItemIfAbsent(
	ctx context.Context,
	database *gorm.DB,
	item *types.PluginMarketplaceItem,
) (bool, error) {
	if err := validatePluginMarketplaceSource(ctx, database, item); err != nil {
		return false, err
	}
	result := database.WithContext(ctx).
		Clauses(clause.OnConflict{DoNothing: true}).
		Create(item)
	return result.RowsAffected > 0, result.Error
}

// GetPluginMarketplaceItemBySource returns one non-deleted marketplace source identity.
func GetPluginMarketplaceItemBySource(
	ctx context.Context,
	database *gorm.DB,
	sourceType, sourceRef string,
) (*types.PluginMarketplaceItem, error) {
	var item types.PluginMarketplaceItem
	err := database.WithContext(ctx).
		Where("source_type = ? AND source_ref = ?", sourceType, sourceRef).
		First(&item).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &item, nil
}

// UpdatePluginMarketplaceItem updates a marketplace directory item.
func UpdatePluginMarketplaceItem(ctx context.Context, database *gorm.DB, item *types.PluginMarketplaceItem) error {
	if err := validatePluginMarketplaceSource(ctx, database, item); err != nil {
		return err
	}
	return database.WithContext(ctx).Save(item).Error
}

func validatePluginMarketplaceSource(
	ctx context.Context,
	database *gorm.DB,
	item *types.PluginMarketplaceItem,
) error {
	if item == nil || item.PluginID == 0 {
		return fmt.Errorf("marketplace source plugin_id is required")
	}
	plugin, err := GetPluginByID(ctx, database, item.PluginID)
	if err != nil {
		return err
	}
	if plugin == nil || plugin.OwnerScope != types.OwnerScopeSystem || plugin.OrgID != 0 ||
		plugin.Origin != "builtin" || plugin.Kind != item.Kind || plugin.Code != item.Code {
		return fmt.Errorf("marketplace source plugin is invalid")
	}
	return nil
}

// GetPluginMarketplaceItemByPublicID returns one non-deleted marketplace item.
func GetPluginMarketplaceItemByPublicID(ctx context.Context, database *gorm.DB, publicID string) (*types.PluginMarketplaceItem, error) {
	var item types.PluginMarketplaceItem
	err := database.WithContext(ctx).Where("public_id = ?", publicID).First(&item).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &item, nil
}

// GetPluginMarketplaceItemByPublicIDIncludingDeleted resolves an installed
// directory item even after it is unpublished or soft-deleted.
func GetPluginMarketplaceItemByPublicIDIncludingDeleted(
	ctx context.Context,
	database *gorm.DB,
	publicID string,
) (*types.PluginMarketplaceItem, error) {
	var item types.PluginMarketplaceItem
	err := database.WithContext(ctx).Unscoped().Where("public_id = ?", publicID).First(&item).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &item, nil
}

// GetPublishedPluginMarketplaceItemByPublicID returns one visible official marketplace item.
func GetPublishedPluginMarketplaceItemByPublicID(ctx context.Context, database *gorm.DB, publicID string) (*types.PluginMarketplaceItem, error) {
	var item types.PluginMarketplaceItem
	err := database.WithContext(ctx).Where("public_id = ? AND status = ?", publicID, "published").First(&item).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &item, nil
}

// GetPublishedPluginMarketplaceItemByIdentity returns one visible official item by kind and code.
func GetPublishedPluginMarketplaceItemByIdentity(
	ctx context.Context,
	database *gorm.DB,
	kind, code string,
) (*types.PluginMarketplaceItem, error) {
	var item types.PluginMarketplaceItem
	err := database.WithContext(ctx).
		Where("kind = ? AND code = ? AND status = ?", kind, code, "published").
		First(&item).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &item, nil
}

// GetPluginMarketplaceItemByIDIncludingDeleted resolves marketplace lineage for an installed revision.
// Installed revisions remain valid after an item is unpublished or soft-deleted.
func GetPluginMarketplaceItemByIDIncludingDeleted(ctx context.Context, database *gorm.DB, id uint) (*types.PluginMarketplaceItem, error) {
	var item types.PluginMarketplaceItem
	err := database.WithContext(ctx).Unscoped().Where("id = ?", id).First(&item).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &item, nil
}

// ListPluginMarketplaceItemsByIDsIncludingDeleted preserves installed
// marketplace lineage after a directory item is unpublished or soft-deleted.
func ListPluginMarketplaceItemsByIDsIncludingDeleted(
	ctx context.Context,
	database *gorm.DB,
	ids []uint,
) ([]types.PluginMarketplaceItem, error) {
	if len(ids) == 0 {
		return []types.PluginMarketplaceItem{}, nil
	}
	var items []types.PluginMarketplaceItem
	err := database.WithContext(ctx).
		Unscoped().
		Where("id IN ?", ids).
		Order("published_at DESC, public_id ASC").
		Find(&items).Error
	return items, err
}

// ListPluginMarketplaceItems returns published marketplace items ordered for stable API responses.
func ListPluginMarketplaceItems(ctx context.Context, database *gorm.DB, filter PluginMarketplaceListFilter) ([]types.PluginMarketplaceItem, error) {
	query := database.WithContext(ctx).Where("status = ?", "published")
	if kind := strings.TrimSpace(filter.Kind); kind != "" {
		query = query.Where("kind = ?", kind)
	}
	if category := strings.TrimSpace(filter.Category); category != "" {
		query = query.Where("category = ?", category)
	}
	if keyword := strings.TrimSpace(filter.Keyword); keyword != "" {
		like := "%" + keyword + "%"
		query = query.Where("code LIKE ? OR name LIKE ? OR description LIKE ?", like, like, like)
	}
	if filter.Limit > 0 {
		query = query.Limit(filter.Limit)
	}

	var items []types.PluginMarketplaceItem
	if err := query.Order("published_at DESC, public_id ASC").Find(&items).Error; err != nil {
		return nil, err
	}
	return items, nil
}
