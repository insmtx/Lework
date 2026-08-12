package db

import (
	"context"
	"testing"
	"time"

	"gorm.io/driver/sqlite"
	"gorm.io/gorm"

	"github.com/insmtx/Leros/backend/types"
)

func setupPluginDAOTestDB(t *testing.T) *gorm.DB {
	t.Helper()
	database, err := gorm.Open(sqlite.Open("file:"+t.Name()+"?mode=memory&cache=shared"), &gorm.Config{})
	if err != nil {
		t.Fatalf("open database: %v", err)
	}
	if err := database.AutoMigrate(
		&types.Project{},
		&types.Plugin{},
		&types.PluginRevision{},
		&types.PluginRevisionContent{},
		&types.ProjectPluginBinding{},
		&types.PluginMarketplaceItem{},
		&types.PluginTranslation{},
		&types.FileUpload{},
	); err != nil {
		t.Fatalf("migrate plugin models: %v", err)
	}
	if err := createPluginIndexes(database); err != nil {
		t.Fatalf("create plugin indexes: %v", err)
	}
	return database
}

func TestPluginSchemaIndexesAndNoForeignKeys(t *testing.T) {
	database := setupPluginDAOTestDB(t)

	for _, tableName := range []string{
		types.TableNamePlugin,
		types.TableNamePluginRevision,
		types.TableNamePluginRevisionContent,
		types.TableNameProjectPluginBinding,
		types.TableNamePluginMarketplaceItem,
		types.TableNamePluginTranslation,
	} {
		if !database.Migrator().HasTable(tableName) {
			t.Fatalf("table %s was not created", tableName)
		}
	}
	for _, indexName := range []string{
		"ux_plugin_org_scope_code",
		"ux_plugin_system_code",
		"ux_plugin_public_id",
		"ux_plugin_revision_number",
		"ux_plugin_revision_content_revision",
		"ux_project_plugin_active",
		"ux_plugin_marketplace_public_id",
		"ux_plugin_marketplace_source",
		"ux_plugin_marketplace_plugin",
		"ux_plugin_translation_scope",
		"idx_plugin_translation_source_revision",
	} {
		if !database.Migrator().HasIndex(types.TableNamePlugin, indexName) &&
			!database.Migrator().HasIndex(types.TableNamePluginRevision, indexName) &&
			!database.Migrator().HasIndex(types.TableNamePluginRevisionContent, indexName) &&
			!database.Migrator().HasIndex(types.TableNameProjectPluginBinding, indexName) &&
			!database.Migrator().HasIndex(types.TableNamePluginMarketplaceItem, indexName) &&
			!database.Migrator().HasIndex(types.TableNamePluginTranslation, indexName) {
			t.Fatalf("index %s was not created", indexName)
		}
	}
	var translationIndexColumns []struct {
		Seq  int
		Name string
	}
	if err := database.Raw("PRAGMA index_info('idx_plugin_translation_source_revision')").Scan(&translationIndexColumns).Error; err != nil {
		t.Fatalf("inspect plugin translation query index: %v", err)
	}
	if len(translationIndexColumns) != 3 || translationIndexColumns[0].Name != "source_type" ||
		translationIndexColumns[1].Name != "source_id" || translationIndexColumns[2].Name != "plugin_revision_id" {
		t.Fatalf("plugin translation query index columns = %#v", translationIndexColumns)
	}
	if !database.Migrator().HasColumn(types.TableNamePluginRevision, "definition") {
		t.Fatalf("table %s is missing definition", types.TableNamePluginRevision)
	}
	for _, tableName := range []string{types.TableNamePluginRevision, types.TableNamePluginMarketplaceItem} {
		for _, column := range []string{"artifact_uri", "artifact_sha256", "package_size_bytes", "content_type"} {
			if database.Migrator().HasColumn(tableName, column) {
				t.Fatalf("table %s unexpectedly has %s", tableName, column)
			}
		}
	}
	for _, column := range []string{"version", "definition"} {
		if database.Migrator().HasColumn(types.TableNamePluginMarketplaceItem, column) {
			t.Fatalf("marketplace table unexpectedly has legacy %s", column)
		}
	}

	for _, tableName := range []string{
		types.TableNamePlugin,
		types.TableNamePluginRevision,
		types.TableNamePluginRevisionContent,
		types.TableNameProjectPluginBinding,
		types.TableNamePluginMarketplaceItem,
		types.TableNamePluginTranslation,
	} {
		var foreignKeys []struct{ Table string }
		if err := database.Raw("PRAGMA foreign_key_list(" + tableName + ")").Scan(&foreignKeys).Error; err != nil {
			t.Fatalf("inspect foreign keys for %s: %v", tableName, err)
		}
		if len(foreignKeys) != 0 {
			t.Fatalf("table %s unexpectedly has foreign keys: %#v", tableName, foreignKeys)
		}
	}
}

func TestPluginTranslationUpsertsBySourceRevisionAndLocale(t *testing.T) {
	database := setupPluginDAOTestDB(t)
	ctx := context.Background()
	metadata := &types.PluginTranslation{
		OrgID: 7, SourceType: types.PluginTranslationSourceMarketplace, SourceID: 11, PluginRevisionID: 101, SourceRevision: 1,
		Locale: "zh-CN", MetadataSourceHash: "metadata-v1", TranslatedName: "中文技能",
		TranslatedDescription: "中文描述",
	}
	if err := UpsertPluginTranslationMetadata(ctx, database, metadata); err != nil {
		t.Fatalf("upsert metadata translation: %v", err)
	}
	if err := UpsertPluginTranslationDocument(ctx, database, &types.PluginTranslation{
		OrgID: 7, SourceType: types.PluginTranslationSourceMarketplace, SourceID: 11, PluginRevisionID: 101, SourceRevision: 1,
		Locale: "zh-CN", SkillMDSourceHash: "document-v1", TranslatedSkillMD: "中文正文",
	}); err != nil {
		t.Fatalf("upsert document translation: %v", err)
	}
	rows, err := ListPluginTranslations(ctx, database, 7, types.PluginTranslationSourceMarketplace, []uint{11}, "zh-CN")
	if err != nil || len(rows) != 1 {
		t.Fatalf("list translation cache = %#v, %v", rows, err)
	}
	if rows[0].TranslatedName != "中文技能" || rows[0].TranslatedSkillMD != "中文正文" {
		t.Fatalf("translation cache fields = %#v", rows[0])
	}

	if err := UpsertPluginTranslationMetadata(ctx, database, &types.PluginTranslation{
		OrgID: 7, SourceType: types.PluginTranslationSourceMarketplace, SourceID: 11, PluginRevisionID: 102, SourceRevision: 2,
		Locale: "zh-CN", MetadataSourceHash: "metadata-v2", TranslatedName: "新版技能",
	}); err != nil {
		t.Fatalf("upsert second revision translation: %v", err)
	}
	rows, err = ListPluginTranslations(ctx, database, 7, types.PluginTranslationSourceMarketplace, []uint{11}, "zh-CN")
	if err != nil || len(rows) != 2 {
		t.Fatalf("list versioned translation cache = %#v, %v", rows, err)
	}
	otherOrg, err := ListPluginTranslations(ctx, database, 8, types.PluginTranslationSourceMarketplace, []uint{11}, "zh-CN")
	if err != nil || len(otherOrg) != 0 {
		t.Fatalf("cross-organization translation cache = %#v, %v", otherOrg, err)
	}
	if err := UpsertPluginTranslationMetadata(ctx, database, &types.PluginTranslation{
		OrgID: 7, SourceType: types.PluginTranslationSourceOrganization, SourceID: 11, PluginRevisionID: 101, SourceRevision: 1,
		Locale: "zh-CN", MetadataSourceHash: "organization-metadata-v1", TranslatedName: "组织技能",
	}); err != nil {
		t.Fatalf("upsert organization translation: %v", err)
	}
	organizationRows, err := ListPluginTranslations(ctx, database, 7, types.PluginTranslationSourceOrganization, []uint{11}, "zh-CN")
	if err != nil || len(organizationRows) != 1 || organizationRows[0].TranslatedName != "组织技能" {
		t.Fatalf("organization translation cache = %#v, %v", organizationRows, err)
	}
	marketplaceRows, err := ListPluginTranslations(ctx, database, 7, types.PluginTranslationSourceMarketplace, []uint{11}, "zh-CN")
	if err != nil || len(marketplaceRows) != 2 {
		t.Fatalf("marketplace cache was mixed with organization cache = %#v, %v", marketplaceRows, err)
	}
}

func TestPluginDAOEnforcesBusinessConstraints(t *testing.T) {
	database := setupPluginDAOTestDB(t)
	ctx := context.Background()
	if err := database.Create(&types.PluginTranslation{
		OrgID: 1, SourceType: "invalid", SourceID: 1, PluginRevisionID: 1, SourceRevision: 1, Locale: "zh-CN",
	}).Error; err == nil {
		t.Fatal("expected invalid plugin translation source type to fail")
	}

	plugin := &types.Plugin{PublicID: "plg_alpha", OrgID: 1, Code: "alpha", Kind: "skill", Name: "Alpha", Status: types.PluginStatusActive, Origin: "org", CreatedBy: 9, UpdatedBy: 9}
	if err := CreatePlugin(ctx, database, plugin); err != nil {
		t.Fatalf("create plugin: %v", err)
	}
	if got, err := GetPluginByPublicID(ctx, database, 2, plugin.PublicID); err != nil || got != nil {
		t.Fatalf("organization isolation result = %#v, %v", got, err)
	}
	if err := CreatePlugin(ctx, database, &types.Plugin{PublicID: "plg_duplicate", OrgID: 1, Code: "alpha", Kind: "skill", Name: "Duplicate", Status: types.PluginStatusActive, Origin: "org", CreatedBy: 9, UpdatedBy: 9}); err == nil {
		t.Fatal("expected duplicate organization code to fail")
	}
	if err := database.Delete(plugin).Error; err != nil {
		t.Fatalf("soft delete plugin: %v", err)
	}
	if err := CreatePlugin(ctx, database, &types.Plugin{PublicID: "plg_recreated", OrgID: 1, Code: "alpha", Kind: "skill", Name: "Recreated", Status: types.PluginStatusActive, Origin: "org", CreatedBy: 9, UpdatedBy: 9}); err != nil {
		t.Fatalf("recreate code after soft delete: %v", err)
	}

	plugin = &types.Plugin{PublicID: "plg_revision", OrgID: 1, Code: "revision", Kind: "skill", Name: "Revision", Status: types.PluginStatusActive, Origin: "org", CreatedBy: 9, UpdatedBy: 9}
	if err := CreatePlugin(ctx, database, plugin); err != nil {
		t.Fatalf("create revision plugin: %v", err)
	}
	baseRevision := &types.PluginRevision{PluginID: plugin.ID, Revision: 1, Status: "published", Definition: []byte(`{"schema":"skill/v1","artifact":{"file_upload_id":"file_alpha","sha256":"aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"}}`), PublishedByType: "user", PublishedByID: 9, PublishedAt: time.Now()}
	if err := CreatePluginRevision(ctx, database, baseRevision); err != nil {
		t.Fatalf("create revision: %v", err)
	}
	if err := CreatePluginRevision(ctx, database, &types.PluginRevision{PluginID: plugin.ID, Revision: 1, Status: "published", Definition: []byte(`{"schema":"skill/v1","artifact":{"file_upload_id":"file_beta","sha256":"bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb"}}`), PublishedByType: "user", PublishedByID: 9, PublishedAt: time.Now()}); err == nil {
		t.Fatal("expected duplicate revision number to fail")
	}
	secondRevision := &types.PluginRevision{PluginID: plugin.ID, Revision: 2, Status: "published", Definition: append([]byte(nil), baseRevision.Definition...), PublishedByType: "user", PublishedByID: 9, PublishedAt: time.Now()}
	if err := CreatePluginRevision(ctx, database, secondRevision); err != nil {
		t.Fatalf("same hash is allowed at database level: %v", err)
	}
	content := &types.PluginRevisionContent{
		PluginRevisionID:  baseRevision.ID,
		Schema:            types.PluginRevisionContentSchemaSkillV1,
		ArtifactSHA256:    "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa",
		EntrypointPath:    "SKILL.md",
		EntrypointContent: "# Alpha",
		FileIndex: types.PluginRevisionFileList{
			{Path: "SKILL.md", SizeBytes: 7, SHA256: "bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb"},
		},
	}
	if err := CreatePluginRevisionContent(ctx, database, content); err != nil {
		t.Fatalf("create revision content: %v", err)
	}
	if err := CreatePluginRevisionContent(ctx, database, &types.PluginRevisionContent{
		PluginRevisionID: baseRevision.ID, Schema: types.PluginRevisionContentSchemaSkillV1,
		ArtifactSHA256: content.ArtifactSHA256, EntrypointPath: "SKILL.md", EntrypointContent: "# Duplicate",
		FileIndex: types.PluginRevisionFileList{},
	}); err == nil {
		t.Fatal("expected duplicate revision content identity to fail")
	}
	secondContent := *content
	secondContent.Model = gorm.Model{}
	secondContent.PluginRevisionID = secondRevision.ID
	if err := CreatePluginRevisionContent(ctx, database, &secondContent); err != nil {
		t.Fatalf("create content for another revision: %v", err)
	}
	gotContent, err := GetPluginRevisionContent(ctx, database, baseRevision.ID)
	if err != nil || gotContent == nil || len(gotContent.FileIndex) != 1 || gotContent.FileIndex[0].Path != "SKILL.md" {
		t.Fatalf("revision content = %#v, %v", gotContent, err)
	}

	binding := &types.ProjectPluginBinding{ProjectID: 7, PluginID: plugin.ID, Enabled: true, Config: []byte(`{}`), CreatedBy: 9, UpdatedBy: 9}
	if err := CreateProjectPluginBinding(ctx, database, binding); err != nil {
		t.Fatalf("create binding: %v", err)
	}
	if err := CreateProjectPluginBinding(ctx, database, &types.ProjectPluginBinding{ProjectID: 7, PluginID: plugin.ID, Enabled: true, Config: []byte(`{}`), CreatedBy: 9, UpdatedBy: 9}); err == nil {
		t.Fatal("expected duplicate binding to fail")
	}
	if removed, err := RemoveProjectPluginBinding(ctx, database, 7, plugin.ID); err != nil || !removed {
		t.Fatalf("remove binding = %v, %v", removed, err)
	}
	if err := CreateProjectPluginBinding(ctx, database, &types.ProjectPluginBinding{ProjectID: 7, PluginID: plugin.ID, Enabled: true, Config: []byte(`{}`), CreatedBy: 9, UpdatedBy: 9}); err != nil {
		t.Fatalf("recreate binding after soft delete: %v", err)
	}

	systemPlugin := &types.Plugin{
		PublicID: "plg_system_alpha", OwnerScope: types.OwnerScopeSystem,
		OrgID: 0, Code: "alpha", Kind: "skill", Name: "Alpha",
		Status: types.PluginStatusActive, Origin: "builtin",
	}
	if err := CreatePlugin(ctx, database, systemPlugin); err != nil {
		t.Fatalf("create marketplace system plugin: %v", err)
	}
	if err := CreatePluginMarketplaceItem(ctx, database, &types.PluginMarketplaceItem{
		PublicID: "mkt_invalid_org_source", PluginID: plugin.ID, Kind: "skill",
		Code: plugin.Code, Name: "Invalid", Author: "Lework",
		SourceType: "builtin", SourceRef: "invalid-org-source",
		Status: "published", Tags: types.PluginStringList{}, PublishedAt: time.Now(),
	}); err == nil {
		t.Fatal("marketplace item accepted an organization source plugin")
	}
	item := &types.PluginMarketplaceItem{PublicID: "mkt_alpha", PluginID: systemPlugin.ID, Kind: "skill", Code: "alpha", Name: "Alpha", Author: "Lework", SourceType: "builtin", SourceRef: "alpha", Status: "published", Tags: types.PluginStringList{}, PublishedAt: time.Now()}
	if err := CreatePluginMarketplaceItem(ctx, database, item); err != nil {
		t.Fatalf("create marketplace item: %v", err)
	}
	if got, err := GetPublishedPluginMarketplaceItemByIdentity(
		ctx,
		database,
		"skill",
		"alpha",
	); err != nil || got == nil || got.ID != item.ID {
		t.Fatalf("marketplace identity lookup = %#v, %v", got, err)
	}
	if got, err := GetPublishedPluginMarketplaceItemByIdentity(
		ctx,
		database,
		"mcp",
		"alpha",
	); err != nil || got != nil {
		t.Fatalf("marketplace kind isolation = %#v, %v", got, err)
	}
	if err := CreatePluginMarketplaceItem(ctx, database, &types.PluginMarketplaceItem{PublicID: "mkt_duplicate", PluginID: systemPlugin.ID, Kind: "skill", Code: "alpha", Name: "Alpha", Author: "Lework", SourceType: "builtin", SourceRef: "alpha", Status: "published", Tags: types.PluginStringList{}, PublishedAt: time.Now()}); err == nil {
		t.Fatal("expected duplicate marketplace source to fail")
	}
}

func TestListProjectPluginSnapshotsIncludesOnlyBoundMCPCurrentRevision(t *testing.T) {
	database := setupPluginDAOTestDB(t)
	ctx := context.Background()
	project := &types.Project{PublicID: "project_mcp_snapshot", OrgID: 1, OwnerID: 9, Name: "MCP"}
	if err := database.Create(project).Error; err != nil {
		t.Fatalf("create project: %v", err)
	}
	bound := &types.Plugin{
		PublicID: "plugin_bound_mcp", OrgID: 1, Code: "docs", Kind: "mcp",
		Name: "Docs", Status: types.PluginStatusActive, Origin: "org",
		CurrentRevision: 1, CreatedBy: 9, UpdatedBy: 9,
	}
	if err := CreatePlugin(ctx, database, bound); err != nil {
		t.Fatalf("create bound MCP: %v", err)
	}
	revisionOne := []byte(
		`{"schema":"mcp/v1","transport":"http","name":"docs","url":"https://v1.example.com/mcp"}`,
	)
	if err := CreatePluginRevision(ctx, database, &types.PluginRevision{
		PluginID: bound.ID, Revision: 1, Status: "published", Definition: revisionOne,
		PublishedByType: "user", PublishedByID: 9, PublishedAt: time.Now(),
	}); err != nil {
		t.Fatalf("create MCP revision 1: %v", err)
	}
	if err := CreateProjectPluginBinding(ctx, database, &types.ProjectPluginBinding{
		ProjectID: project.ID, PluginID: bound.ID, Enabled: true, Config: []byte(`{}`),
		CreatedBy: 9, UpdatedBy: 9,
	}); err != nil {
		t.Fatalf("bind MCP: %v", err)
	}
	unbound := &types.Plugin{
		PublicID: "plugin_unbound_mcp", OrgID: 1, Code: "crm", Kind: "mcp",
		Name: "CRM", Status: types.PluginStatusActive, Origin: "org",
		CurrentRevision: 1, CreatedBy: 9, UpdatedBy: 9,
	}
	if err := CreatePlugin(ctx, database, unbound); err != nil {
		t.Fatalf("create unbound MCP: %v", err)
	}
	if err := CreatePluginRevision(ctx, database, &types.PluginRevision{
		PluginID: unbound.ID, Revision: 1, Status: "published",
		Definition: []byte(
			`{"schema":"mcp/v1","transport":"http","name":"crm","url":"https://crm.example.com/mcp"}`,
		),
		PublishedByType: "user", PublishedByID: 9, PublishedAt: time.Now(),
	}); err != nil {
		t.Fatalf("create unbound revision: %v", err)
	}

	frozen, err := ListProjectPluginSnapshots(ctx, database, 1, project.ID)
	if err != nil {
		t.Fatalf("ListProjectPluginSnapshots() error = %v", err)
	}
	if len(frozen) != 1 || frozen[0].Code != "docs" || frozen[0].Revision != 1 {
		t.Fatalf("frozen snapshots = %#v", frozen)
	}

	revisionTwo := []byte(
		`{"schema":"mcp/v1","transport":"http","name":"docs","url":"https://v2.example.com/mcp"}`,
	)
	if err := CreatePluginRevision(ctx, database, &types.PluginRevision{
		PluginID: bound.ID, Revision: 2, Status: "published", Definition: revisionTwo,
		PublishedByType: "user", PublishedByID: 9, PublishedAt: time.Now(),
	}); err != nil {
		t.Fatalf("create MCP revision 2: %v", err)
	}
	if err := database.Model(&types.Plugin{}).Where("id = ?", bound.ID).
		Update("current_revision", 2).Error; err != nil {
		t.Fatalf("activate MCP revision 2: %v", err)
	}
	if frozen[0].Revision != 1 || string(frozen[0].Definition) != string(revisionOne) {
		t.Fatalf("previously frozen snapshot changed = %#v", frozen[0])
	}
}

func TestPluginOwnerScopeConstraintsAndOrganizationVisibility(t *testing.T) {
	database := setupPluginDAOTestDB(t)
	ctx := context.Background()

	invalid := []types.Plugin{
		{
			PublicID: "plugin_invalid_org", OwnerScope: types.OwnerScopeOrganization,
			OrgID: 0, Code: "invalid-org", Kind: "skill", Name: "Invalid",
			Status: types.PluginStatusActive, Origin: "org", CreatedBy: 1, UpdatedBy: 1,
		},
		{
			PublicID: "plugin_invalid_system", OwnerScope: types.OwnerScopeSystem,
			OrgID: 1, Code: "invalid-system", Kind: "skill", Name: "Invalid",
			Status: types.PluginStatusActive, Origin: "builtin",
		},
	}
	for index := range invalid {
		if err := CreatePlugin(ctx, database, &invalid[index]); err == nil {
			t.Fatalf("invalid owner scope combination %d was accepted", index)
		}
	}
	if err := database.Create(&types.Plugin{
		PublicID: "plugin_invalid_database_constraint", OwnerScope: types.OwnerScopeSystem,
		OrgID: 3, Code: "invalid-database", Kind: "skill", Name: "Invalid",
		Status: types.PluginStatusActive, Origin: "builtin",
	}).Error; err == nil {
		t.Fatal("database accepted an invalid system plugin owner scope")
	}

	organization := &types.Plugin{
		PublicID: "plugin_org_shared", OwnerScope: types.OwnerScopeOrganization,
		OrgID: 1, Code: "shared", Kind: "skill", Name: "Organization",
		Status: types.PluginStatusActive, Origin: "org", CreatedBy: 1, UpdatedBy: 1,
	}
	system := &types.Plugin{
		PublicID: "plugin_system_shared", OwnerScope: types.OwnerScopeSystem,
		OrgID: 0, Code: "shared", Kind: "skill", Name: "System",
		Status: types.PluginStatusActive, Origin: "builtin",
	}
	if err := CreatePlugin(ctx, database, organization); err != nil {
		t.Fatalf("create organization plugin: %v", err)
	}
	if err := CreatePlugin(ctx, database, system); err != nil {
		t.Fatalf("create system plugin with shared code: %v", err)
	}
	if err := CreatePlugin(ctx, database, &types.Plugin{
		PublicID: "plugin_system_duplicate", OwnerScope: types.OwnerScopeSystem,
		OrgID: 0, Code: "shared", Kind: "skill", Name: "Duplicate",
		Status: types.PluginStatusActive, Origin: "builtin",
	}); err == nil {
		t.Fatal("duplicate system plugin code was accepted")
	}

	plugins, err := ListPlugins(ctx, database, 1, PluginListFilter{})
	if err != nil || len(plugins) != 1 || plugins[0].ID != organization.ID {
		t.Fatalf("organization list = %#v, %v", plugins, err)
	}
	if got, err := GetPluginByPublicID(ctx, database, 1, system.PublicID); err != nil || got != nil {
		t.Fatalf("system plugin leaked through organization detail: %#v, %v", got, err)
	}
	if got, err := GetOrganizationPluginByIdentity(
		ctx,
		database,
		1,
		"skill",
		"shared",
	); err != nil || got == nil || got.ID != organization.ID {
		t.Fatalf("organization identity lookup = %#v, %v", got, err)
	}
	if got, err := GetOrganizationPluginByIdentity(
		ctx,
		database,
		2,
		"skill",
		"shared",
	); err != nil || got != nil {
		t.Fatalf("organization identity isolation = %#v, %v", got, err)
	}
}

func TestListPluginsFiltersOnlyMCPByCreator(t *testing.T) {
	database := setupPluginDAOTestDB(t)
	ctx := context.Background()
	plugins := []types.Plugin{
		{
			PublicID: "plugin_mcp_mine", OwnerScope: types.OwnerScopeOrganization,
			OrgID: 1, Code: "mcp-mine", Kind: "mcp", Name: "Mine",
			Status: types.PluginStatusActive, Origin: "org", CreatedBy: 10, UpdatedBy: 10,
		},
		{
			PublicID: "plugin_mcp_other", OwnerScope: types.OwnerScopeOrganization,
			OrgID: 1, Code: "mcp-other", Kind: "mcp", Name: "Other",
			Status: types.PluginStatusActive, Origin: "org", CreatedBy: 11, UpdatedBy: 11,
		},
		{
			PublicID: "plugin_skill_shared", OwnerScope: types.OwnerScopeOrganization,
			OrgID: 1, Code: "skill-shared", Kind: "skill", Name: "Shared",
			Status: types.PluginStatusActive, Origin: "org", CreatedBy: 11, UpdatedBy: 11,
		},
	}
	for index := range plugins {
		if err := CreatePlugin(ctx, database, &plugins[index]); err != nil {
			t.Fatalf("create plugin %d: %v", index, err)
		}
	}

	visible, err := ListPlugins(ctx, database, 1, PluginListFilter{ViewerUin: 10})
	if err != nil {
		t.Fatalf("ListPlugins() error = %v", err)
	}
	ids := make(map[string]bool, len(visible))
	for _, plugin := range visible {
		ids[plugin.PublicID] = true
	}
	if !ids["plugin_mcp_mine"] || !ids["plugin_skill_shared"] || ids["plugin_mcp_other"] {
		t.Fatalf("visible plugins = %#v", visible)
	}

	mcps, err := ListPlugins(ctx, database, 1, PluginListFilter{
		Kind: "mcp", ViewerUin: 10,
	})
	if err != nil || len(mcps) != 1 || mcps[0].PublicID != "plugin_mcp_mine" {
		t.Fatalf("personal MCP list = %#v, %v", mcps, err)
	}
	skills, err := ListPlugins(ctx, database, 1, PluginListFilter{
		Kind: "skill", ViewerUin: 10,
	})
	if err != nil || len(skills) != 1 || skills[0].PublicID != "plugin_skill_shared" {
		t.Fatalf("shared Skill list = %#v, %v", skills, err)
	}
}
