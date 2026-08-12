package db

import (
	"context"
	"testing"

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
