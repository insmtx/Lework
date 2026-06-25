package db

import (
	"context"
	"testing"

	"gorm.io/driver/sqlite"
	"gorm.io/gorm"

	"github.com/insmtx/Leros/backend/config"
	"github.com/insmtx/Leros/backend/types"
)

func setupLLMModelTestDB(t *testing.T) *gorm.DB {
	t.Helper()

	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	if err != nil {
		t.Fatalf("failed to open test database: %v", err)
	}

	if err := db.AutoMigrate(&types.LLMModel{}); err != nil {
		t.Fatalf("failed to migrate test database: %v", err)
	}

	return db
}

func newTestLLMModel(orgID uint, code string) *types.LLMModel {
	return &types.LLMModel{
		OrgID:           orgID,
		Code:            code,
		Name:            "测试模型",
		Provider:        string(types.LLMProviderOpenAI),
		ModelName:       "gpt-4o-mini",
		BaseURL:         "https://api.openai.com/v1",
		APIKeyEncrypted: "encrypted-key",
		APIKeyMasked:    "sk-***1234",
		MaxTokens:       4096,
		Temperature:     0.7,
		TimeoutSec:      120,
		Status:          string(types.LLMModelStatusActive),
	}
}

func TestCreateAndGetLLMModel(t *testing.T) {
	database := setupLLMModelTestDB(t)
	ctx := context.Background()

	model := newTestLLMModel(1, "main-openai")
	if err := CreateLLMModel(ctx, database, model); err != nil {
		t.Fatalf("CreateLLMModel failed: %v", err)
	}
	if model.ID == 0 {
		t.Fatal("expected model ID to be set")
	}

	byID, err := GetLLMModelByID(ctx, database, model.ID)
	if err != nil {
		t.Fatalf("GetLLMModelByID failed: %v", err)
	}
	if byID == nil || byID.Code != model.Code {
		t.Fatalf("unexpected model by id: %#v", byID)
	}

	byCode, err := GetLLMModelByCode(ctx, database, 1, "main-openai")
	if err != nil {
		t.Fatalf("GetLLMModelByCode failed: %v", err)
	}
	if byCode == nil || byCode.ID != model.ID {
		t.Fatalf("unexpected model by code: %#v", byCode)
	}
}

func TestGetLLMModelNotFound(t *testing.T) {
	database := setupLLMModelTestDB(t)
	ctx := context.Background()

	byID, err := GetLLMModelByID(ctx, database, 999)
	if err != nil {
		t.Fatalf("GetLLMModelByID failed: %v", err)
	}
	if byID != nil {
		t.Fatalf("expected nil model by id, got %#v", byID)
	}

	byCode, err := GetLLMModelByCode(ctx, database, 1, "missing")
	if err != nil {
		t.Fatalf("GetLLMModelByCode failed: %v", err)
	}
	if byCode != nil {
		t.Fatalf("expected nil model by code, got %#v", byCode)
	}
}

func TestUpdateLLMModel(t *testing.T) {
	database := setupLLMModelTestDB(t)
	ctx := context.Background()

	model := newTestLLMModel(1, "update-openai")
	if err := CreateLLMModel(ctx, database, model); err != nil {
		t.Fatalf("CreateLLMModel failed: %v", err)
	}

	model.Name = "更新后的模型"
	model.ModelName = "gpt-4o"
	if err := UpdateLLMModel(ctx, database, model); err != nil {
		t.Fatalf("UpdateLLMModel failed: %v", err)
	}

	retrieved, err := GetLLMModelByID(ctx, database, model.ID)
	if err != nil {
		t.Fatalf("GetLLMModelByID failed: %v", err)
	}
	if retrieved.Name != "更新后的模型" || retrieved.ModelName != "gpt-4o" {
		t.Fatalf("model was not updated: %#v", retrieved)
	}
}

func TestListLLMModels(t *testing.T) {
	database := setupLLMModelTestDB(t)
	ctx := context.Background()

	models := []*types.LLMModel{
		newTestLLMModel(1, "openai-main"),
		newTestLLMModel(1, "deepseek-main"),
		newTestLLMModel(2, "other-org"),
	}
	models[1].Name = "DeepSeek 主模型"
	models[1].Provider = string(types.LLMProviderDeepSeek)
	models[1].ModelName = "deepseek-chat"
	models[2].Name = "其他组织模型"

	for _, model := range models {
		if err := CreateLLMModel(ctx, database, model); err != nil {
			t.Fatalf("CreateLLMModel failed: %v", err)
		}
	}

	orgID := uint(1)
	provider := string(types.LLMProviderDeepSeek)
	items, total, err := ListLLMModels(ctx, database, &types.PageQuery{Caller: types.Caller{OrgID: orgID}, Pagination: types.Pagination{Limit: 20}, Filters: []types.Filter{{Field: "provider", Value: []string{provider}}}})
	if err != nil {
		t.Fatalf("ListLLMModels failed: %v", err)
	}
	if total != 1 || len(items) != 1 || items[0].Code != "deepseek-main" {
		t.Fatalf("unexpected provider filtered result: total=%d items=%#v", total, items)
	}

	keyword := "openai"
	items, total, err = ListLLMModels(ctx, database, &types.PageQuery{Caller: types.Caller{OrgID: orgID}, Pagination: types.Pagination{Limit: 20}, Filters: []types.Filter{{Field: "keyword", Value: []string{keyword}}}})
	if err != nil {
		t.Fatalf("ListLLMModels failed: %v", err)
	}
	if total != 1 || len(items) != 1 || items[0].Code != "openai-main" {
		t.Fatalf("unexpected keyword filtered result: total=%d items=%#v", total, items)
	}
}

func TestGetDefaultLLMModel(t *testing.T) {
	database := setupLLMModelTestDB(t)
	ctx := context.Background()

	normal := newTestLLMModel(1, "normal")
	defaultModel := newTestLLMModel(1, "default")
	defaultModel.IsDefault = true
	inactiveDefault := newTestLLMModel(2, "inactive-default")
	inactiveDefault.IsDefault = true
	inactiveDefault.Status = string(types.LLMModelStatusInactive)

	for _, model := range []*types.LLMModel{normal, defaultModel, inactiveDefault} {
		if err := CreateLLMModel(ctx, database, model); err != nil {
			t.Fatalf("CreateLLMModel failed: %v", err)
		}
	}

	retrieved, err := GetDefaultLLMModel(ctx, database, 1)
	if err != nil {
		t.Fatalf("GetDefaultLLMModel failed: %v", err)
	}
	if retrieved == nil || retrieved.Code != "default" {
		t.Fatalf("unexpected default model: %#v", retrieved)
	}

	missing, err := GetDefaultLLMModel(ctx, database, 2)
	if err != nil {
		t.Fatalf("GetDefaultLLMModel failed: %v", err)
	}
	if missing != nil {
		t.Fatalf("expected nil for inactive default model, got %#v", missing)
	}
}

func TestGetSystemTranslationLLMModel(t *testing.T) {
	database := setupLLMModelTestDB(t)
	ctx := context.Background()

	fallback := newTestLLMModel(1, SystemTranslationLLMModelCode)
	fallback.IsSystem = true
	fallback.Provider = string(types.LLMProviderDeepSeek)
	fallback.ModelName = "deepseek-v4-flash"

	orgModel := newTestLLMModel(2, SystemTranslationLLMModelCode)
	orgModel.IsSystem = true
	orgModel.Provider = string(types.LLMProviderDeepSeek)
	orgModel.ModelName = "deepseek-v4-flash"

	nonSystem := newTestLLMModel(3, SystemTranslationLLMModelCode)
	inactive := newTestLLMModel(4, SystemTranslationLLMModelCode)
	inactive.IsSystem = true
	inactive.Status = string(types.LLMModelStatusInactive)

	for _, model := range []*types.LLMModel{fallback, orgModel, nonSystem, inactive} {
		if err := CreateLLMModel(ctx, database, model); err != nil {
			t.Fatalf("CreateLLMModel failed: %v", err)
		}
	}

	retrieved, err := GetSystemTranslationLLMModel(ctx, database, 2)
	if err != nil {
		t.Fatalf("GetSystemTranslationLLMModel failed: %v", err)
	}
	if retrieved == nil || retrieved.OrgID != 2 || retrieved.ModelName != "deepseek-v4-flash" {
		t.Fatalf("unexpected org translation model: %#v", retrieved)
	}

	retrieved, err = GetSystemTranslationLLMModel(ctx, database, 3)
	if err != nil {
		t.Fatalf("GetSystemTranslationLLMModel fallback failed: %v", err)
	}
	if retrieved == nil || retrieved.OrgID != 1 {
		t.Fatalf("expected fallback system translation model, got %#v", retrieved)
	}

	missing, err := GetSystemTranslationLLMModel(ctx, database, 4)
	if err != nil {
		t.Fatalf("GetSystemTranslationLLMModel inactive fallback failed: %v", err)
	}
	if missing == nil || missing.OrgID != 1 {
		t.Fatalf("expected fallback for inactive org model, got %#v", missing)
	}
}

func TestSeedSystemLLMModelsCreatesTranslationModel(t *testing.T) {
	database := setupLLMModelTestDB(t)

	err := seedSystemLLMModels(database, &config.LLMConfig{
		APIKey:  "main-key",
		BaseURL: "https://gateway.example.com/v1/",
		Translation: &config.LLMTranslationConfig{
			Provider: string(types.LLMProviderDeepSeek),
			APIKey:   "translation-key",
			Model:    "deepseek-v4-flash",
			BaseURL:  "https://api.deepseek.com/v1",
		},
	})
	if err != nil {
		t.Fatalf("seedSystemLLMModels failed: %v", err)
	}

	model, err := GetSystemTranslationLLMModel(context.Background(), database, 1)
	if err != nil {
		t.Fatalf("GetSystemTranslationLLMModel failed: %v", err)
	}
	if model == nil {
		t.Fatal("expected system translation model")
	}
	if model.Provider != string(types.LLMProviderDeepSeek) || model.ModelName != "deepseek-v4-flash" {
		t.Fatalf("unexpected translation model: %#v", model)
	}
	if model.APIKeyEncrypted != "translation-key" || model.IsDefault || !model.IsSystem {
		t.Fatalf("unexpected translation model flags/key: %#v", model)
	}
}

func TestSeedSystemLLMModelsDoesNotOverwriteExistingSystemTranslationModel(t *testing.T) {
	database := setupLLMModelTestDB(t)

	existing := newTestLLMModel(1, SystemTranslationLLMModelCode)
	existing.IsSystem = true
	existing.Provider = string(types.LLMProviderOpenAI)
	existing.ModelName = "old-model"
	existing.APIKeyEncrypted = "old-key"
	if err := CreateLLMModel(context.Background(), database, existing); err != nil {
		t.Fatalf("CreateLLMModel failed: %v", err)
	}

	err := seedSystemLLMModels(database, &config.LLMConfig{
		APIKey: "main-key",
		Translation: &config.LLMTranslationConfig{
			Provider: string(types.LLMProviderDeepSeek),
			APIKey:   "new-key",
			Model:    "deepseek-v4-flash",
			BaseURL:  "https://api.deepseek.com/v1",
		},
	})
	if err != nil {
		t.Fatalf("seedSystemLLMModels failed: %v", err)
	}

	model, err := GetLLMModelByCode(context.Background(), database, 1, SystemTranslationLLMModelCode)
	if err != nil {
		t.Fatalf("GetLLMModelByCode failed: %v", err)
	}
	if model.Provider != string(types.LLMProviderOpenAI) || model.ModelName != "old-model" || model.APIKeyEncrypted != "old-key" {
		t.Fatalf("expected existing system model to remain unchanged, got %#v", model)
	}
}

func TestSeedSystemLLMModelsDoesNotOverwriteNonSystemModel(t *testing.T) {
	database := setupLLMModelTestDB(t)

	existing := newTestLLMModel(1, SystemTranslationLLMModelCode)
	existing.IsSystem = false
	existing.Provider = string(types.LLMProviderOpenAI)
	existing.ModelName = "user-model"
	if err := CreateLLMModel(context.Background(), database, existing); err != nil {
		t.Fatalf("CreateLLMModel failed: %v", err)
	}

	err := seedSystemLLMModels(database, &config.LLMConfig{
		APIKey: "main-key",
		Translation: &config.LLMTranslationConfig{
			Provider: string(types.LLMProviderDeepSeek),
			APIKey:   "translation-key",
			Model:    "deepseek-v4-flash",
		},
	})
	if err != nil {
		t.Fatalf("seedSystemLLMModels failed: %v", err)
	}

	model, err := GetLLMModelByCode(context.Background(), database, 1, SystemTranslationLLMModelCode)
	if err != nil {
		t.Fatalf("GetLLMModelByCode failed: %v", err)
	}
	if model.IsSystem || model.ModelName != "user-model" {
		t.Fatalf("expected non-system model to remain unchanged, got %#v", model)
	}
}

func TestDeleteLLMModel(t *testing.T) {
	database := setupLLMModelTestDB(t)
	ctx := context.Background()

	model := newTestLLMModel(1, "delete-openai")
	if err := CreateLLMModel(ctx, database, model); err != nil {
		t.Fatalf("CreateLLMModel failed: %v", err)
	}

	if err := DeleteLLMModel(ctx, database, model.ID); err != nil {
		t.Fatalf("DeleteLLMModel failed: %v", err)
	}

	retrieved, err := GetLLMModelByID(ctx, database, model.ID)
	if err != nil {
		t.Fatalf("GetLLMModelByID failed: %v", err)
	}
	if retrieved != nil {
		t.Fatalf("expected deleted model to be nil, got %#v", retrieved)
	}
}

func TestLLMModelCodeExists(t *testing.T) {
	database := setupLLMModelTestDB(t)
	ctx := context.Background()

	model := newTestLLMModel(1, "exists-openai")
	if err := CreateLLMModel(ctx, database, model); err != nil {
		t.Fatalf("CreateLLMModel failed: %v", err)
	}

	exists, err := LLMModelCodeExists(ctx, database, 1, "exists-openai", 0)
	if err != nil {
		t.Fatalf("LLMModelCodeExists failed: %v", err)
	}
	if !exists {
		t.Fatal("expected code to exist")
	}

	exists, err = LLMModelCodeExists(ctx, database, 1, "exists-openai", model.ID)
	if err != nil {
		t.Fatalf("LLMModelCodeExists failed: %v", err)
	}
	if exists {
		t.Fatal("expected excluded code to not exist")
	}
}
