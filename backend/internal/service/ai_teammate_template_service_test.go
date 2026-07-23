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
	if err := database.Create(&types.Organization{
		PublicID: "org_test",
		Code:     "default_org",
		Name:     "默认组织",
		Type:     "company",
		Status:   "active",
	}).Error; err != nil {
		t.Fatalf("seed org: %v", err)
	}
	if err := database.Create(&types.User{
		PublicID: "usr_test",
		Name:     "Admin",
	}).Error; err != nil {
		t.Fatalf("seed user: %v", err)
	}
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
