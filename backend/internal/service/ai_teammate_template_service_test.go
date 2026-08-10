package service

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/insmtx/Leros/backend/config"
	"github.com/insmtx/Leros/backend/internal/api/contract"
	"github.com/insmtx/Leros/backend/internal/infra/filestore"
	"github.com/insmtx/Leros/backend/types"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
)

func setupAITeammateTemplateDB(t *testing.T) *gorm.DB {
	t.Helper()
	database, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	if err != nil {
		t.Fatalf("failed to open test database: %v", err)
	}
	if err := database.AutoMigrate(
		&types.DigitalAssistant{},
		&types.AITeammateTemplate{},
		&types.FileUpload{},
		&types.Organization{},
		&types.User{},
		&types.WorkerDeployment{},
	); err != nil {
		t.Fatalf("failed to migrate test database: %v", err)
	}
	return database
}

func TestAITeammateTemplateCounters(t *testing.T) {
	database := setupAITeammateTemplateDB(t)
	template := &types.AITeammateTemplate{
		Code:         "media-hotspot-hunter",
		Name:         "自媒体热点猎手",
		SystemPrompt: "追踪热点并输出选题建议。",
		Category:     "content",
		Status:       string(contract.AITeammateTemplateStatusActive),
		IsSystem:     true,
	}
	if err := database.Create(template).Error; err != nil {
		t.Fatalf("create template: %v", err)
	}

	service := NewAITeammateTemplateService(database)
	useResult, err := service.IncrementAITeammateTemplateUseCount(setupTestContextWithCaller(t), &contract.IncrementAITeammateTemplateCountRequest{ID: &template.ID})
	if err != nil {
		t.Fatalf("increment use count: %v", err)
	}
	if useResult.UseCount != 1 {
		t.Fatalf("use_count = %d, want 1", useResult.UseCount)
	}

	recommendResult, err := service.IncrementAITeammateTemplateRecommendCount(setupTestContextWithCaller(t), &contract.IncrementAITeammateTemplateCountRequest{Code: &template.Code})
	if err != nil {
		t.Fatalf("increment recommend count: %v", err)
	}
	if recommendResult.RecommendCount != 1 {
		t.Fatalf("recommend_count = %d, want 1", recommendResult.RecommendCount)
	}
}

func TestCreateDigitalAssistantFromTemplateIncrementsUseCount(t *testing.T) {
	database := setupAITeammateTemplateDB(t)
	template := &types.AITeammateTemplate{
		Code:         "content-editor",
		Name:         "内容主编",
		Description:  "统筹品牌内容。",
		SystemPrompt: "负责内容策划和编辑。",
		Expertise:    types.SkillStringList{"内容策划", "品牌传播"},
		Category:     "content",
		Status:       string(contract.AITeammateTemplateStatusActive),
		IsSystem:     true,
	}
	if err := database.Create(template).Error; err != nil {
		t.Fatalf("create template: %v", err)
	}

	service := NewDigitalAssistantService(database, nil)
	result, err := service.CreateDigitalAssistantFromTemplate(setupTestContextWithCaller(t), &contract.CreateDigitalAssistantFromTemplateRequest{
		TemplateID:   template.ID,
		Name:         "品牌内容助手",
		Description:  "负责日常品牌内容。",
		RoleName:     "请求覆盖的角色",
		SystemPrompt: "请求覆盖的角色设定",
		Expertise:    []string{"请求覆盖的能力"},
	})
	if err != nil {
		t.Fatalf("create assistant from template: %v", err)
	}
	if result.TemplateID == nil || *result.TemplateID != template.ID {
		t.Fatalf("template_id = %v, want %d", result.TemplateID, template.ID)
	}
	if result.Source != "template" {
		t.Fatalf("source = %q, want template", result.Source)
	}
	if result.Name != "品牌内容助手" || result.Description != "负责日常品牌内容。" {
		t.Fatalf("template user fields = %q/%q, want request values", result.Name, result.Description)
	}
	if result.RoleName != template.Name || result.SystemPrompt != template.SystemPrompt {
		t.Fatalf("template protected fields = %q/%q, want template values", result.RoleName, result.SystemPrompt)
	}
	if len(result.Expertise) != 2 || result.Expertise[0] != "内容策划" || result.Expertise[1] != "品牌传播" {
		t.Fatalf("expertise = %#v, want template expertise", result.Expertise)
	}

	var stored types.AITeammateTemplate
	if err := database.First(&stored, template.ID).Error; err != nil {
		t.Fatalf("reload template: %v", err)
	}
	if stored.UseCount != 1 {
		t.Fatalf("use_count = %d, want 1", stored.UseCount)
	}
}

func TestSeedAITeammateTemplatesUploadsMissingAvatarAndPreservesExisting(t *testing.T) {
	database := setupAITeammateTemplateDB(t)
	if err := database.Create(&types.AITeammateTemplate{
		Code:         "contract-review-expert",
		Name:         "旧合同专家",
		Avatar:       "file_existing_avatar",
		SystemPrompt: "旧提示词",
		Category:     "legal",
		Status:       string(contract.AITeammateTemplateStatusActive),
		IsSystem:     true,
	}).Error; err != nil {
		t.Fatalf("seed existing template: %v", err)
	}

	if err := filestore.Init(&config.StorageConfig{
		Driver:     "local",
		LocalDir:   t.TempDir(),
		Bucket:     "test-bucket",
		SignSecret: "test-secret",
	}); err != nil {
		t.Fatalf("init filestore: %v", err)
	}
	avatarDir := t.TempDir()
	if err := os.WriteFile(filepath.Join(avatarDir, "data-analysis-expert.png"), []byte("png image data"), 0644); err != nil {
		t.Fatalf("write avatar: %v", err)
	}

	if err := SeedAITeammateTemplates(context.Background(), database, avatarDir); err != nil {
		t.Fatalf("seed ai teammate templates: %v", err)
	}

	var dataTemplate types.AITeammateTemplate
	if err := database.Where("code = ?", "data-analysis-expert").First(&dataTemplate).Error; err != nil {
		t.Fatalf("find data template: %v", err)
	}
	if dataTemplate.Avatar == "" {
		t.Fatal("expected data template avatar public_id to be set")
	}
	var upload types.FileUpload
	if err := database.Where("public_id = ?", dataTemplate.Avatar).First(&upload).Error; err != nil {
		t.Fatalf("find uploaded avatar: %v", err)
	}
	if upload.Purpose != filestore.PurposeAvatar {
		t.Fatalf("upload purpose = %q, want %q", upload.Purpose, filestore.PurposeAvatar)
	}

	var contractTemplate types.AITeammateTemplate
	if err := database.Where("code = ?", "contract-review-expert").First(&contractTemplate).Error; err != nil {
		t.Fatalf("find contract template: %v", err)
	}
	if contractTemplate.Avatar != "file_existing_avatar" {
		t.Fatalf("existing avatar = %q, want file_existing_avatar", contractTemplate.Avatar)
	}
	if contractTemplate.Name == "旧合同专家" {
		t.Fatal("expected existing template fields to be updated")
	}
}

func TestEmbeddedAITeammateTemplateAvatarsMatchDefaultTemplates(t *testing.T) {
	for _, template := range defaultAITeammateTemplates() {
		data, source, err := readAITeammateTemplateAvatar("", template.Code)
		if err != nil {
			t.Fatalf("read embedded avatar %s: %v", source, err)
		}
		if len(data) == 0 {
			t.Fatalf("embedded avatar %s is empty", source)
		}
	}
}

func TestSeedAITeammateTemplatesReinitBrokenSystemAvatar(t *testing.T) {
	database := setupAITeammateTemplateDB(t)
	if err := filestore.Init(&config.StorageConfig{
		Driver:     "local",
		LocalDir:   t.TempDir(),
		Bucket:     "test-bucket",
		BaseURL:    "http://localhost:8080",
		SignSecret: "test-secret",
	}); err != nil {
		t.Fatalf("init filestore: %v", err)
	}

	// 预置模板，其头像指向不存在的 system 文件记录（记录缺失 → 需要重传）。
	if err := database.Create(&types.AITeammateTemplate{
		Code:         "bid-strategist",
		Name:         "旧投标策略师",
		Avatar:       "file_stale_missing",
		SystemPrompt: "旧提示词",
		Category:     "bidding",
		Status:       string(contract.AITeammateTemplateStatusActive),
		IsSystem:     true,
	}).Error; err != nil {
		t.Fatalf("create template with stale avatar: %v", err)
	}

	if err := SeedAITeammateTemplates(context.Background(), database, ""); err != nil {
		t.Fatalf("seed ai teammate templates: %v", err)
	}

	var template types.AITeammateTemplate
	if err := database.Where("code = ?", "bid-strategist").First(&template).Error; err != nil {
		t.Fatalf("find reloaded template: %v", err)
	}
	if template.Avatar == "" || template.Avatar == "file_stale_missing" {
		t.Fatalf("stale avatar was not reinitialized: %q", template.Avatar)
	}
	var upload types.FileUpload
	if err := database.Where("public_id = ?", template.Avatar).First(&upload).Error; err != nil {
		t.Fatalf("reinitialized avatar not backed by file record: %v", err)
	}
	if upload.OwnerScope != types.OwnerScopeSystem {
		t.Fatalf("reinitialized avatar scope = %q, want system", upload.OwnerScope)
	}
}

func TestSeedAITeammateTemplatesPreservesValidSystemAvatar(t *testing.T) {
	database := setupAITeammateTemplateDB(t)
	if err := filestore.Init(&config.StorageConfig{
		Driver:     "local",
		LocalDir:   t.TempDir(),
		Bucket:     "test-bucket",
		BaseURL:    "http://localhost:8080",
		SignSecret: "test-secret",
	}); err != nil {
		t.Fatalf("init filestore: %v", err)
	}

	// 预置一个真实、有效（StorageURI 指向可读对象）的 system 头像。
	existing, err := uploadAITeammateTemplateAvatar(context.Background(), database, "", "data-analysis-expert")
	if err != nil {
		t.Fatalf("upload initial avatar: %v", err)
	}
	if existing == "" {
		t.Fatal("upload initial avatar returned empty")
	}
	if err := database.Create(&types.AITeammateTemplate{
		Code:         "data-analysis-expert",
		Name:         "旧数据分析专家",
		Avatar:       existing,
		SystemPrompt: "旧提示词",
		Category:     "data",
		Status:       string(contract.AITeammateTemplateStatusActive),
		IsSystem:     true,
	}).Error; err != nil {
		t.Fatalf("create template with valid avatar: %v", err)
	}

	avatarDir := t.TempDir()
	if err := os.WriteFile(filepath.Join(avatarDir, "data-analysis-expert.png"), []byte("png"), 0644); err != nil {
		t.Fatalf("write avatar: %v", err)
	}
	if err := SeedAITeammateTemplates(context.Background(), database, avatarDir); err != nil {
		t.Fatalf("seed ai teammate templates: %v", err)
	}

	var template types.AITeammateTemplate
	if err := database.Where("code = ?", "data-analysis-expert").First(&template).Error; err != nil {
		t.Fatalf("find reloaded template: %v", err)
	}
	if template.Avatar != existing {
		t.Fatalf("valid avatar not preserved: got %q, want %q", template.Avatar, existing)
	}
	if template.Name == "旧数据分析专家" {
		t.Fatal("expected non-avatar fields to be updated even when avatar is kept")
	}
}
