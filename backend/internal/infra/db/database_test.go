package db

import (
	"context"
	"testing"
	"time"

	"gorm.io/driver/sqlite"
	"gorm.io/gorm"

	"github.com/insmtx/Leros/backend/types"
)

func TestRunMigrationsCreatesOrganizationTables(t *testing.T) {
	database, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	if err != nil {
		t.Fatalf("failed to open test database: %v", err)
	}
	if err := database.Exec("CREATE TABLE leros_plugin_marketplace_translation (id INTEGER PRIMARY KEY, org_id INTEGER)").Error; err != nil {
		t.Fatalf("create legacy translation table: %v", err)
	}

	if err := runMigrations(database); err != nil {
		t.Fatalf("failed to run migrations: %v", err)
	}

	for _, tableName := range []string{
		types.TableNameDepartment,
		types.TableNameMemberDepartment,
		types.TableNamePlugin,
		types.TableNamePluginRevision,
		types.TableNamePluginRevisionContent,
		types.TableNameProjectPluginBinding,
		types.TableNamePluginMarketplaceItem,
		types.TableNamePluginTranslation,
		types.TableNameMCPChannel,
	} {
		if !database.Migrator().HasTable(tableName) {
			t.Fatalf("expected table %s to be migrated", tableName)
		}
	}
	if database.Migrator().HasTable("leros_plugin_marketplace_translation") {
		t.Fatal("legacy marketplace translation table should be removed without data backfill")
	}
}

func TestBackfillAutomationIntervalScheduleV2PreservesWallClockPhase(t *testing.T) {
	database, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	if err := database.AutoMigrate(&types.Automation{}); err != nil {
		t.Fatal(err)
	}
	enabled := true
	automation := &types.Automation{
		PublicID:     "auto_legacy_phase",
		OrgID:        1,
		OwnerID:      1,
		Name:         "legacy",
		Instruction:  "check",
		Enabled:      &enabled,
		ScheduleMode: string(types.AutomationScheduleModeInterval),
		ScheduleSpec: types.AutomationScheduleSpec{
			FormConfig: &types.AutomationScheduleFormConfig{
				Mode:     "interval",
				Timezone: "Asia/Shanghai",
				Interval: &types.AutomationIntervalConfig{AnchorAt: "2026-08-19T10:50:00Z", IntervalSeconds: 1800},
			},
			Spec: types.AutomationScheduleSpecItem{
				Version:         1,
				Mode:            "interval",
				AnchorAt:        "2026-08-19T10:50:00Z",
				IntervalSeconds: 1800,
				Timezone:        "Asia/Shanghai",
			},
		},
		Timezone:    "Asia/Shanghai",
		AssistantID: 1,
	}
	if err := database.Create(automation).Error; err != nil {
		t.Fatal(err)
	}
	legacyUpdatedAt := time.Date(2026, 8, 18, 3, 4, 5, 0, time.UTC)
	if err := database.Model(&types.Automation{}).Where("id = ?", automation.ID).UpdateColumn("updated_at", legacyUpdatedAt).Error; err != nil {
		t.Fatal(err)
	}
	migrationNow := time.Date(2026, 8, 19, 1, 0, 0, 0, time.UTC)
	if err := backfillAutomationIntervalScheduleV2At(database, migrationNow); err != nil {
		t.Fatal(err)
	}
	var migrated types.Automation
	if err := database.First(&migrated, automation.ID).Error; err != nil {
		t.Fatal(err)
	}
	wantNext := time.Date(2026, 8, 19, 2, 50, 0, 0, time.UTC)
	if migrated.ScheduleSpec.Spec.Version != types.AutomationScheduleVersion {
		t.Fatalf("version=%d, want %d", migrated.ScheduleSpec.Spec.Version, types.AutomationScheduleVersion)
	}
	if migrated.NextRunAt == nil || !migrated.NextRunAt.Equal(wantNext) {
		t.Fatalf("next_run_at=%v, want %s", migrated.NextRunAt, wantNext)
	}
	wantOrigin := wantNext.Add(-30 * time.Minute).Format(time.RFC3339Nano)
	if migrated.ScheduleSpec.Spec.OriginAt != wantOrigin {
		t.Fatalf("origin_at=%q, want %q", migrated.ScheduleSpec.Spec.OriginAt, wantOrigin)
	}
	if migrated.ScheduleSpec.Spec.AnchorAt != "" || migrated.ScheduleSpec.FormConfig.Interval.AnchorAt != "" {
		t.Fatal("legacy anchor_at should be cleared")
	}
	if !migrated.UpdatedAt.Equal(legacyUpdatedAt) {
		t.Fatalf("updated_at changed during data migration: got %s, want %s", migrated.UpdatedAt, legacyUpdatedAt)
	}
	if err := backfillAutomationIntervalScheduleV2At(database, migrationNow.Add(time.Hour)); err != nil {
		t.Fatalf("idempotent migration: %v", err)
	}
	var repeated types.Automation
	if err := database.First(&repeated, automation.ID).Error; err != nil {
		t.Fatal(err)
	}
	if repeated.ScheduleSpec.Spec.OriginAt != migrated.ScheduleSpec.Spec.OriginAt || repeated.NextRunAt == nil || !repeated.NextRunAt.Equal(*migrated.NextRunAt) {
		t.Fatal("repeated migration changed an already migrated schedule")
	}
}

func TestBackfillAutomationIntervalScheduleV2KeepsDisabledNextEmpty(t *testing.T) {
	database, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	if err := database.AutoMigrate(&types.Automation{}); err != nil {
		t.Fatal(err)
	}
	disabled := false
	automation := &types.Automation{
		PublicID: "auto_legacy_disabled", OrgID: 1, OwnerID: 1, Name: "legacy", Enabled: &disabled,
		ScheduleMode: "interval", Timezone: "Asia/Shanghai",
		ScheduleSpec: types.AutomationScheduleSpec{Spec: types.AutomationScheduleSpecItem{
			Version: 1, Mode: "interval", AnchorAt: "10:50", IntervalSeconds: 1800, Timezone: "Asia/Shanghai",
		}},
	}
	if err := database.Create(automation).Error; err != nil {
		t.Fatal(err)
	}
	if err := backfillAutomationIntervalScheduleV2At(database, time.Date(2026, 8, 19, 1, 0, 0, 0, time.UTC)); err != nil {
		t.Fatal(err)
	}
	var migrated types.Automation
	if err := database.First(&migrated, automation.ID).Error; err != nil {
		t.Fatal(err)
	}
	if migrated.NextRunAt != nil {
		t.Fatalf("disabled automation got next_run_at=%v", migrated.NextRunAt)
	}
	if migrated.ScheduleSpec.Spec.OriginAt == "" || migrated.ScheduleSpec.Spec.AnchorAt != "" {
		t.Fatalf("unexpected migrated spec: %+v", migrated.ScheduleSpec.Spec)
	}
}

func TestBackfillAutomationIntervalScheduleV2RejectsInvalidDataAtomically(t *testing.T) {
	database, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	if err := database.AutoMigrate(&types.Automation{}); err != nil {
		t.Fatal(err)
	}
	valid := &types.Automation{
		PublicID: "auto_legacy_valid_before_invalid", OrgID: 1, OwnerID: 1, Name: "valid", ScheduleMode: "interval", Timezone: "Asia/Shanghai",
		ScheduleSpec: types.AutomationScheduleSpec{Spec: types.AutomationScheduleSpecItem{
			Version: 1, Mode: "interval", AnchorAt: "10:50", IntervalSeconds: 1800, Timezone: "Asia/Shanghai",
		}},
	}
	invalid := &types.Automation{
		PublicID: "auto_legacy_invalid", OrgID: 1, OwnerID: 1, Name: "invalid", ScheduleMode: "interval", Timezone: "Not/AZone",
		ScheduleSpec: types.AutomationScheduleSpec{Spec: types.AutomationScheduleSpecItem{
			Version: 1, Mode: "interval", AnchorAt: "10:50", IntervalSeconds: 1800, Timezone: "Not/AZone",
		}},
	}
	if err := database.Create(valid).Error; err != nil {
		t.Fatal(err)
	}
	if err := database.Create(invalid).Error; err != nil {
		t.Fatal(err)
	}
	if err := backfillAutomationIntervalScheduleV2At(database, time.Date(2026, 8, 19, 1, 0, 0, 0, time.UTC)); err == nil {
		t.Fatal("expected invalid migration data to fail")
	}
	var unchanged types.Automation
	if err := database.First(&unchanged, valid.ID).Error; err != nil {
		t.Fatal(err)
	}
	if unchanged.ScheduleSpec.Spec.Version != 1 || unchanged.ScheduleSpec.Spec.OriginAt != "" {
		t.Fatalf("transaction did not roll back valid row: %+v", unchanged.ScheduleSpec.Spec)
	}
}

func TestBackfillMCPChannelAuthorizationMarksCoreKGManaged(t *testing.T) {
	database, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	if err != nil {
		t.Fatalf("open database: %v", err)
	}
	if err := database.AutoMigrate(&types.MCPChannel{}); err != nil {
		t.Fatalf("migrate MCP channel: %v", err)
	}
	channel := &types.MCPChannel{
		Channel: "corekg", Name: "CoreKG", Transport: "http",
		URL: "https://example.com/mcp", AuthType: types.MCPChannelAuthTypeNone,
		Status: types.MCPChannelStatusActive,
	}
	if err := database.Create(channel).Error; err != nil {
		t.Fatalf("create CoreKG channel: %v", err)
	}

	if err := backfillMCPChannelAuthorization(database); err != nil {
		t.Fatalf("backfill MCP channel authorization: %v", err)
	}
	var updated types.MCPChannel
	if err := database.First(&updated, channel.ID).Error; err != nil {
		t.Fatalf("reload CoreKG channel: %v", err)
	}
	if updated.AuthType != types.MCPChannelAuthTypeManaged ||
		types.MCPChannelAuthConfig(updated.AuthConfig).Handler != "corekg" {
		t.Fatalf("CoreKG authorization = %q %#v", updated.AuthType, updated.AuthConfig)
	}
}

func setupVerifyProjectResourceBindingsTestDB(t *testing.T) *gorm.DB {
	t.Helper()
	database, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	if err := database.AutoMigrate(&types.Project{}, &types.Resource{}, &types.ResourceBinding{}); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	return database
}

func TestVerifyProjectResourceBindingsReturnsNilOnCompleteFixture(t *testing.T) {
	database := setupVerifyProjectResourceBindingsTestDB(t)

	ctx := context.Background()
	project := &types.Project{PublicID: "prj_verify_ok", OrgID: 1, OwnerID: 10, Name: "OK", Status: string(types.ProjectStatusActive)}
	if err := CreateProject(ctx, database, project); err != nil {
		t.Fatalf("create project: %v", err)
	}
	resource := &types.Resource{OrgID: 1, Uin: 10, Type: types.ResourceTypeProject, BizID: project.ID}
	if err := CreateResource(ctx, database, resource); err != nil {
		t.Fatalf("create resource: %v", err)
	}
	ownerUin := uint(10)
	if err := CreateResourceBinding(ctx, database, &types.ResourceBinding{
		OrgID: 1, Uin: &ownerUin, ResourceID: resource.ID, Role: types.ResourceRoleOwner,
	}); err != nil {
		t.Fatalf("create owner binding: %v", err)
	}

	if err := verifyProjectResourceBindings(database); err != nil {
		t.Fatalf("verifyProjectResourceBindings: %v", err)
	}
}

// TestBackfillLLMModelClassDefaults 验证 LLM 模型默认收敛迁移：
// 补建缺失类、类内唯一默认，并同步 org1 系统对话模型名称。
func TestBackfillLLMModelClassDefaults(t *testing.T) {
	database, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	if err != nil {
		t.Fatalf("open database: %v", err)
	}
	if err := database.AutoMigrate(&types.LLMModel{}, &types.Organization{}, &types.DigitalAssistant{}); err != nil {
		t.Fatalf("migrate: %v", err)
	}

	// org1：对话系统模型（默认）+ 翻译系统模型（默认）。
	conv := &types.LLMModel{OrgID: 1, Code: "llm_default", Name: "旧名", Provider: "openai", ModelName: "gpt-4o", Status: string(types.LLMModelStatusActive), Purpose: types.LLMModelPurposeConversation, IsDefault: true, IsSystem: true}
	trans := &types.LLMModel{OrgID: 1, Code: SystemTranslationLLMModelCode, Name: "内置翻译模型", Provider: "deepseek", ModelName: "deepseek-v4-flash", Status: string(types.LLMModelStatusActive), Purpose: types.LLMModelPurposeTranslation, IsDefault: true, IsSystem: true}
	if err := database.Create(conv).Error; err != nil {
		t.Fatalf("create org1 conv: %v", err)
	}
	if err := database.Create(trans).Error; err != nil {
		t.Fatalf("create org1 trans: %v", err)
	}

	// org2：仅拥有对话系统模型（非默认），缺少翻译类，且对话类无默认。
	org2 := &types.Organization{Model: gorm.Model{ID: 2}, Code: "org2", Name: "Org2"}
	if err := database.Create(org2).Error; err != nil {
		t.Fatalf("create org2: %v", err)
	}
	conv2 := &types.LLMModel{OrgID: org2.ID, Code: "llm_default", Name: "对话", Provider: "openai", ModelName: "gpt-4o", Status: string(types.LLMModelStatusActive), Purpose: types.LLMModelPurposeConversation, IsDefault: false, IsSystem: true}
	if err := database.Create(conv2).Error; err != nil {
		t.Fatalf("create org2 conv: %v", err)
	}

	if err := backfillLLMModelClassDefaults(database); err != nil {
		t.Fatalf("backfillLLMModelClassDefaults: %v", err)
	}

	// org2 现在应拥有翻译类模型，且对话/翻译各一个默认。
	var org2Models []types.LLMModel
	if err := database.Where("org_id = ?", org2.ID).Find(&org2Models).Error; err != nil {
		t.Fatalf("list org2 models: %v", err)
	}
	var org2Default int64
	if err := database.Model(&types.LLMModel{}).Where("org_id = ? AND is_default = ?", org2.ID, true).Count(&org2Default).Error; err != nil {
		t.Fatalf("count org2 default: %v", err)
	}
	if org2Default != 2 {
		t.Fatalf("expected org2 to have 2 defaults (conv+trans), got %d", org2Default)
	}
	var org2HasTrans int64
	if err := database.Model(&types.LLMModel{}).Where("org_id = ? AND code = ?", org2.ID, SystemTranslationLLMModelCode).Count(&org2HasTrans).Error; err != nil {
		t.Fatalf("count org2 trans: %v", err)
	}
	if org2HasTrans == 0 {
		t.Fatal("expected org2 translation class backfilled")
	}

	// org1 系统对话模型名称已同步。
	if err := database.First(conv, conv.ID).Error; err != nil {
		t.Fatalf("reload org1 conv: %v", err)
	}
	if conv.Name != "内置对话模型" {
		t.Fatalf("expected org1 conv name synced to 内置对话模型, got %q", conv.Name)
	}
}

func TestVerifyProjectResourceBindingsReturnsNilWhenProjectMissingOwnerBinding(t *testing.T) {
	database := setupVerifyProjectResourceBindingsTestDB(t)

	ctx := context.Background()
	project := &types.Project{PublicID: "prj_verify_gap", OrgID: 1, OwnerID: 10, Name: "Gap", Status: string(types.ProjectStatusActive)}
	if err := CreateProject(ctx, database, project); err != nil {
		t.Fatalf("create project: %v", err)
	}
	resource := &types.Resource{OrgID: 1, Uin: 10, Type: types.ResourceTypeProject, BizID: project.ID}
	if err := CreateResource(ctx, database, resource); err != nil {
		t.Fatalf("create resource: %v", err)
	}

	if err := verifyProjectResourceBindings(database); err != nil {
		t.Fatalf("verifyProjectResourceBindings should warn-only, got err: %v", err)
	}
}

func TestBackfillProjectFileVersionsGroupsExistingPaths(t *testing.T) {
	database, err := gorm.Open(sqlite.Open("file:"+t.Name()+"?mode=memory&cache=shared"), &gorm.Config{})
	if err != nil {
		t.Fatalf("open database: %v", err)
	}
	if err := database.AutoMigrate(&types.FileUpload{}, &types.ProjectFile{}); err != nil {
		t.Fatalf("migrate database: %v", err)
	}

	uploads := []types.FileUpload{
		{PublicID: "file_old_1", OrgID: 1, OwnerID: 1, Filename: "report.md", OriginalName: "report.md", StorageURI: "file:///bucket/v1.md", Status: "active"},
		{PublicID: "file_old_2", OrgID: 1, OwnerID: 1, Filename: "report.md", OriginalName: "report.md", StorageURI: "file:///bucket/v2.md", Status: "active"},
	}
	for i := range uploads {
		if err := database.Create(&uploads[i]).Error; err != nil {
			t.Fatalf("create upload %d: %v", i, err)
		}
		projectFile := &types.ProjectFile{
			FilePublicID: uploads[i].PublicID,
			OrgID:        1,
			ProjectID:    10,
			TaskID:       20,
			ResourceID:   uploads[i].ID,
			ResourceType: types.ProjectFileResourceTypeArtifact,
			Uin:          1,
		}
		if err := database.Create(projectFile).Error; err != nil {
			t.Fatalf("create project file %d: %v", i, err)
		}
	}

	if err := backfillProjectFileVersions(database); err != nil {
		t.Fatalf("backfill project file versions: %v", err)
	}
	var files []types.ProjectFile
	if err := database.Order("version_no ASC").Find(&files).Error; err != nil {
		t.Fatalf("list project files: %v", err)
	}
	if len(files) != 2 {
		t.Fatalf("project file count = %d", len(files))
	}
	for i := range files {
		if files[i].RelativePath != "artifacts/report.md" || files[i].InitialFilePublicID != "file_old_1" || files[i].VersionNo != i+1 {
			t.Fatalf("project file %d = %#v", i, files[i])
		}
	}
	if !database.Migrator().HasIndex(&types.ProjectFile{}, "idx_project_file_version") {
		t.Fatal("project file version index was not created")
	}
	if !database.Migrator().HasIndex(&types.ProjectFile{}, "idx_project_file_path_version") {
		t.Fatal("project file path version index was not created")
	}
}

func TestBackfillPluginResourcesIsIdempotent(t *testing.T) {
	database, err := gorm.Open(sqlite.Open("file:"+t.Name()+"?mode=memory&cache=shared"), &gorm.Config{})
	if err != nil {
		t.Fatalf("open database: %v", err)
	}
	if err := database.AutoMigrate(
		&types.Plugin{},
		&types.Resource{},
		&types.ResourceBinding{},
		&types.User{},
		&types.UserOrg{},
		&types.Organization{},
	); err != nil {
		t.Fatalf("migrate database: %v", err)
	}

	user := &types.User{PublicID: "usr_backfill_owner", Name: "Owner"}
	if err := database.Create(user).Error; err != nil {
		t.Fatalf("create user: %v", err)
	}
	uo := &types.UserOrg{UserID: user.ID, OrgID: 7}
	if err := database.Create(uo).Error; err != nil {
		t.Fatalf("create user org: %v", err)
	}
	plugin := &types.Plugin{
		PublicID: "plugin_backfill", OwnerScope: types.OwnerScopeOrganization,
		OrgID: 7, Code: "backfill", Kind: "skill", Name: "Backfill",
		Status: types.PluginStatusActive, Origin: "org", CreatedBy: uo.ID, UpdatedBy: uo.ID,
	}
	if err := database.Create(plugin).Error; err != nil {
		t.Fatalf("create plugin: %v", err)
	}

	for i := 0; i < 2; i++ {
		if err := backfillPluginResources(database); err != nil {
			t.Fatalf("backfillPluginResources run %d: %v", i, err)
		}
	}

	var resources []types.Resource
	if err := database.Where("org_id = ? AND type = ? AND biz_id = ?",
		7, types.ResourceTypePlugin, plugin.ID).Find(&resources).Error; err != nil {
		t.Fatalf("list resources: %v", err)
	}
	if len(resources) != 1 {
		t.Fatalf("plugin resources = %d, want 1", len(resources))
	}
	var bindings []types.ResourceBinding
	if err := database.Where("resource_id = ?", resources[0].ID).Find(&bindings).Error; err != nil {
		t.Fatalf("list bindings: %v", err)
	}
	if len(bindings) != 1 || bindings[0].Role != types.ResourceRoleOwner ||
		bindings[0].Uin == nil || *bindings[0].Uin != uo.ID {
		t.Fatalf("plugin owner bindings = %#v", bindings)
	}
}
