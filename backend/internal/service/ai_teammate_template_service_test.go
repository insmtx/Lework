package service

import (
	"testing"

	"github.com/insmtx/Leros/backend/internal/api/contract"
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
	if err := database.AutoMigrate(&types.DigitalAssistant{}, &types.AITeammateTemplate{}); err != nil {
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
		TemplateID: template.ID,
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
	if len(result.Expertise) != 2 {
		t.Fatalf("expertise length = %d, want 2", len(result.Expertise))
	}

	var stored types.AITeammateTemplate
	if err := database.First(&stored, template.ID).Error; err != nil {
		t.Fatalf("reload template: %v", err)
	}
	if stored.UseCount != 1 {
		t.Fatalf("use_count = %d, want 1", stored.UseCount)
	}
}
