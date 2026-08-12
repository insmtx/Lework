package service

import (
	"context"
	"strings"
	"testing"

	"gorm.io/gorm"

	infradb "github.com/insmtx/Leros/backend/internal/infra/db"
	"github.com/insmtx/Leros/backend/types"
)

func TestValidatedMetadataTranslationAcceptsNonEmptyModelOutput(t *testing.T) {
	translated, complete := validatedMetadataTranslation(
		"English Skill",
		"An English description.",
		TranslatedSkillText{DisplayName: "English Skill", Description: "An English description."},
	)
	if !complete {
		t.Fatal("non-empty model output must be considered complete")
	}
	if translated.DisplayName != "English Skill" || translated.Description != "An English description." {
		t.Fatalf("model output was not applied: %#v", translated)
	}
}

func TestValidatedMetadataTranslationAcceptsChineseOutputWithTechnicalTerms(t *testing.T) {
	translated, complete := validatedMetadataTranslation(
		"time-query",
		"Query current time and convert Unix timestamps.",
		TranslatedSkillText{
			DisplayName: "时间查询",
			Description: "查询当前时间，在不同时区之间转换时间，计算时间差，以及转换Unix时间戳。当请求涉及当前日期或时间、询问特定地点的“现在几点”、跨区域安排日程、转换会议时间在不同时区之间、计算两个时刻之间经过或剩余的时间，或将Unix纪元时间戳与人类可读日期相互转换时，应使用此技能。",
		},
	)
	if !complete {
		t.Fatalf("Chinese output with technical terms should be complete: %#v", translated)
	}
	if translated.DisplayName != "时间查询" || !strings.Contains(translated.Description, "Unix") {
		t.Fatalf("technical-term translation = %#v", translated)
	}
}

func TestValidatedMetadataTranslationAcceptsEnglishDisplayName(t *testing.T) {
	translated, complete := validatedMetadataTranslation(
		"create-word-doc",
		"Create and edit Word documents.",
		TranslatedSkillText{DisplayName: "Create Word Doc", Description: "创建、编辑 Word 文档。"},
	)
	if !complete {
		t.Fatal("non-empty English display name must be considered complete")
	}
	if translated.DisplayName != "Create Word Doc" || translated.Description == "" {
		t.Fatalf("English display name result = %#v", translated)
	}
}

func TestValidatedMetadataTranslationAcceptsChineseOutputAtThreshold(t *testing.T) {
	translated, complete := validatedMetadataTranslation(
		"English Skill",
		"An English description.",
		TranslatedSkillText{DisplayName: "中文技能", Description: "这是中文展示描述。"},
	)
	if !complete {
		t.Fatalf("Chinese model output should be complete: %#v", translated)
	}
	if translated.DisplayName != "中文技能" || translated.Description != "这是中文展示描述。" {
		t.Fatalf("Chinese model output = %#v", translated)
	}
}

type incompleteMetadataTranslator struct{}

func (incompleteMetadataTranslator) Translate(
	context.Context,
	uint,
	[]TranslateItem,
) (map[string]TranslatedSkillText, error) {
	return map[string]TranslatedSkillText{
		"organization:11:101": {DisplayName: "中文技能", Description: ""},
	}, nil
}

func (incompleteMetadataTranslator) TranslateDocument(
	context.Context,
	uint,
	[]TranslateDocumentItem,
) (map[string]string, error) {
	return map[string]string{}, nil
}

func TestSkillMetadataRejectsPartialResultWithoutCaching(t *testing.T) {
	database := setupPluginServiceTestDB(t)
	service := newSkillDisplayTranslationService(database, incompleteMetadataTranslator{})
	result := service.translateMetadata(context.Background(), 7, []skillTranslationSource{{
		sourceType:  types.PluginTranslationSourceOrganization,
		sourceID:    11,
		revision:    &types.PluginRevision{Model: gorm.Model{ID: 101}, Revision: 1},
		name:        "English Skill",
		description: "An English description",
	}})
	if len(result) != 0 {
		t.Fatalf("partial metadata translation was applied: %#v", result)
	}
	rows, err := infradb.ListPluginTranslations(context.Background(), database, 7, types.PluginTranslationSourceOrganization, []uint{11}, translationLocale)
	if err != nil {
		t.Fatalf("list partial translation cache: %v", err)
	}
	if len(rows) != 0 {
		t.Fatalf("partial metadata translation was cached: %#v", rows)
	}
}
