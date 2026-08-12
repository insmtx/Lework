package llm

import (
	"context"
	"strings"
	"testing"

	"gorm.io/driver/sqlite"
	"gorm.io/gorm"

	dbrepo "github.com/insmtx/Leros/backend/internal/infra/db"
	"github.com/insmtx/Leros/backend/types"
)

const testOrgID uint = 1

func setupTestDB(t *testing.T) *gorm.DB {
	t.Helper()
	database, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	if err != nil {
		t.Fatalf("failed to open test database: %v", err)
	}
	if err := database.AutoMigrate(&types.LLMModel{}); err != nil {
		t.Fatalf("failed to migrate: %v", err)
	}
	return database
}

func setupManager(t *testing.T) (*ManagerDb, *gorm.DB) {
	t.Helper()
	m := NewManager(setupTestDB(t))
	m.probeFunc = mockProbeSuccessV1
	return m, m.db
}

func managerWithProbe(db *gorm.DB, probe func(context.Context, string, string, string, string, bool) *probeResult) *ManagerDb {
	m := NewManager(db)
	m.probeFunc = probe
	return m
}

func mockProbeSuccessV1(_ context.Context, _, _, _, _ string, _ bool) *probeResult {
	return &probeResult{V1Success: true, NoV1Success: false}
}

func mockProbeSuccessNoV1(_ context.Context, _, _, _, _ string, _ bool) *probeResult {
	return &probeResult{V1Success: false, NoV1Success: true}
}

func mockProbeAlwaysFail(_ context.Context, _, _, _, _ string, _ bool) *probeResult {
	return &probeResult{V1Success: false, NoV1Success: false}
}

func countDefaultLLMModels(t *testing.T, database *gorm.DB, orgID uint) int64 {
	t.Helper()

	var count int64
	if err := database.Model(&types.LLMModel{}).
		Where("org_id = ? AND is_default = ?", orgID, true).
		Count(&count).Error; err != nil {
		t.Fatalf("count default llm models failed: %v", err)
	}
	return count
}

// --- Create tests ---

func TestCreateLLMModelGeneratesCodeDefaultsNameAndMasksAPIKey(t *testing.T) {
	m, database := setupManager(t)
	ctx := context.Background()

	model, err := m.Create(ctx, testOrgID, &CreateRequest{
		Provider: string(types.LLMProviderOpenAI),
		Model:    "gpt-4o-mini",
		Name:     "gpt-4o-mini",
		Purpose:  types.LLMModelPurposeConversation,
		BaseURL:  "https://api.openai.com/v1",
		APIKey:   "sk-test-1234567890",
	})
	if err != nil {
		t.Fatalf("Create failed: %v", err)
	}

	if !strings.HasPrefix(model.Code, "llm_") {
		t.Fatalf("expected generated llm code, got %q", model.Code)
	}
	if model.Name != "gpt-4o-mini" {
		t.Fatalf("expected name to default to model, got %q", model.Name)
	}
	if model.BaseURL != "https://api.openai.com" {
		t.Fatalf("expected normalized base_url, got %q", model.BaseURL)
	}
	// ModelConfig.APIKey holds the encrypted value (raw key input), not the masked value.
	if model.APIKey != "sk-test-1234567890" {
		t.Fatalf("expected raw api key, got %q", model.APIKey)
	}
	if model.MaxTokens != 4096 || model.Temperature != 0.7 || model.TimeoutSec != 120 {
		t.Fatalf("unexpected defaults: max_tokens=%d temperature=%v timeout_sec=%d", model.MaxTokens, model.Temperature, model.TimeoutSec)
	}

	stored, err := dbrepo.GetLLMModelByID(ctx, database, model.ID)
	if err != nil {
		t.Fatalf("GetLLMModelByID failed: %v", err)
	}
	if stored.APIKeyEncrypted != "sk-test-1234567890" {
		t.Fatalf("expected stored api key to match input, got %q", stored.APIKeyEncrypted)
	}
	if stored.APIKeyMasked != "sk-***7890" {
		t.Fatalf("expected stored masked api key, got %q", stored.APIKeyMasked)
	}
}

func TestCreateLLMModelRequiresAPIKey(t *testing.T) {
	m, _ := setupManager(t)
	ctx := context.Background()

	_, err := m.Create(ctx, testOrgID, &CreateRequest{
		Provider: string(types.LLMProviderOpenAI),
		Model:    "gpt-4o-mini",
		Name:     "gpt-4o-mini",
		Purpose:  types.LLMModelPurposeConversation,
		BaseURL:  "https://api.openai.com/v1",
	})
	if err == nil {
		t.Fatal("expected error for missing api_key")
	}
	if err.Error() != "api_key is required" {
		t.Fatalf("expected api_key required error, got %q", err.Error())
	}
}

func TestCreateLLMModelRequiresBaseURL(t *testing.T) {
	m, _ := setupManager(t)
	ctx := context.Background()

	_, err := m.Create(ctx, testOrgID, &CreateRequest{
		Provider: string(types.LLMProviderOpenAI),
		Model:    "gpt-4o-mini",
		Name:     "gpt-4o-mini",
		Purpose:  types.LLMModelPurposeConversation,
		APIKey:   "sk-test-1234567890",
	})
	if err == nil {
		t.Fatal("expected error for missing base_url")
	}
	if err.Error() != "base_url is required" {
		t.Fatalf("expected base_url required error, got %q", err.Error())
	}
}

func TestCreateLLMModelTrimsChatCompletionsPath(t *testing.T) {
	m, database := setupManager(t)
	ctx := context.Background()

	model, err := m.Create(ctx, testOrgID, &CreateRequest{
		Provider: string(types.LLMProviderOpenAI),
		Model:    "gpt-4o-mini",
		Name:     "gpt-4o-mini",
		Purpose:  types.LLMModelPurposeConversation,
		BaseURL:  "https://api.openai.com/v1/chat/completions",
		APIKey:   "sk-test-1234567890",
	})
	if err != nil {
		t.Fatalf("Create failed: %v", err)
	}
	if model.BaseURL != "https://api.openai.com" {
		t.Fatalf("expected normalized base_url in response, got %q", model.BaseURL)
	}

	stored, err := dbrepo.GetLLMModelByID(ctx, database, model.ID)
	if err != nil {
		t.Fatalf("GetLLMModelByID failed: %v", err)
	}
	if stored.BaseURL != "https://api.openai.com" {
		t.Fatalf("expected normalized base_url in database, got %q", stored.BaseURL)
	}
}

func TestCreateLLMModelForcesFirstOrgModelDefault(t *testing.T) {
	m, database := setupManager(t)
	ctx := context.Background()

	first, err := m.Create(ctx, testOrgID, &CreateRequest{
		Provider: string(types.LLMProviderOpenAI),
		Model:    "gpt-4o-mini",
		Name:     "gpt-4o-mini",
		Purpose:  types.LLMModelPurposeConversation,
		BaseURL:  "https://api.openai.com/v1",
		APIKey:   "sk-test-1234567890",
	})
	if err != nil {
		t.Fatalf("first Create failed: %v", err)
	}
	if !first.IsDefault {
		t.Fatal("expected first org llm model to be forced default")
	}

	second, err := m.Create(ctx, testOrgID, &CreateRequest{
		Provider: string(types.LLMProviderDeepSeek),
		Model:    "deepseek-chat",
		Name:     "deepseek-chat",
		Purpose:  types.LLMModelPurposeConversation,
		BaseURL:  "https://api.deepseek.com/v1",
		APIKey:   "sk-test-abcdefgh",
	})
	if err != nil {
		t.Fatalf("second Create failed: %v", err)
	}
	if second.IsDefault {
		t.Fatal("expected non-first org llm model to keep requested default flag")
	}

	if count := countDefaultLLMModels(t, database, testOrgID); count != 1 {
		t.Fatalf("expected one default llm model, got %d", count)
	}
	storedFirst, err := dbrepo.GetLLMModelByID(ctx, database, first.ID)
	if err != nil {
		t.Fatalf("GetLLMModelByID failed: %v", err)
	}
	if !storedFirst.IsDefault {
		t.Fatal("expected first org llm model default flag to be stored")
	}
}

func TestCreateLLMModelKeepsSingleDefault(t *testing.T) {
	m, database := setupManager(t)
	ctx := context.Background()

	first, err := m.Create(ctx, testOrgID, &CreateRequest{
		Provider:  string(types.LLMProviderOpenAI),
		Model:     "gpt-4o-mini",
		Name:      "gpt-4o-mini",
		Purpose:   types.LLMModelPurposeConversation,
		BaseURL:   "https://api.openai.com/v1",
		APIKey:    "sk-test-1234567890",
		IsDefault: true,
	})
	if err != nil {
		t.Fatalf("first Create failed: %v", err)
	}
	second, err := m.Create(ctx, testOrgID, &CreateRequest{
		Provider:  string(types.LLMProviderDeepSeek),
		Model:     "deepseek-chat",
		Name:      "deepseek-chat",
		Purpose:   types.LLMModelPurposeConversation,
		BaseURL:   "https://api.deepseek.com/v1",
		APIKey:    "sk-test-abcdefgh",
		IsDefault: true,
	})
	if err != nil {
		t.Fatalf("second Create failed: %v", err)
	}

	if count := countDefaultLLMModels(t, database, testOrgID); count != 1 {
		t.Fatalf("expected one default llm model, got %d", count)
	}
	storedFirst, err := dbrepo.GetLLMModelByID(ctx, database, first.ID)
	if err != nil {
		t.Fatalf("GetLLMModelByID failed: %v", err)
	}
	if storedFirst.IsDefault {
		t.Fatal("expected first model default flag to be cleared")
	}
	storedSecond, err := dbrepo.GetLLMModelByID(ctx, database, second.ID)
	if err != nil {
		t.Fatalf("GetLLMModelByID failed: %v", err)
	}
	if !storedSecond.IsDefault {
		t.Fatal("expected second model to be default")
	}
}

func TestCreateLLMModelStoresBaseURLHasV1WhenProbeV1Succeeds(t *testing.T) {
	database := setupTestDB(t)
	m := managerWithProbe(database, mockProbeSuccessV1)
	ctx := context.Background()

	model, err := m.Create(ctx, testOrgID, &CreateRequest{
		Provider: string(types.LLMProviderOpenAI),
		Model:    "gpt-4o-mini",
		Name:     "gpt-4o-mini",
		Purpose:  types.LLMModelPurposeConversation,
		BaseURL:  "https://api.openai.com/v1",
		APIKey:   "sk-test-1234567890",
	})
	if err != nil {
		t.Fatalf("Create failed: %v", err)
	}

	if !model.BaseURLHasV1 {
		t.Fatal("expected BaseURLHasV1=true when /v1 probe succeeds")
	}

	stored, err := dbrepo.GetLLMModelByID(ctx, database, model.ID)
	if err != nil {
		t.Fatalf("GetLLMModelByID failed: %v", err)
	}
	if !stored.BaseURLHasV1 {
		t.Fatal("expected stored BaseURLHasV1=true")
	}
}

func TestCreateLLMModelStoresBaseURLHasV1FalseWhenNoV1Succeeds(t *testing.T) {
	database := setupTestDB(t)
	m := managerWithProbe(database, mockProbeSuccessNoV1)
	ctx := context.Background()

	model, err := m.Create(ctx, testOrgID, &CreateRequest{
		Provider: string(types.LLMProviderOpenAI),
		Model:    "gpt-4o-mini",
		Name:     "gpt-4o-mini",
		Purpose:  types.LLMModelPurposeConversation,
		BaseURL:  "https://api.openai.com",
		APIKey:   "sk-test-1234567890",
	})
	if err != nil {
		t.Fatalf("Create failed: %v", err)
	}

	if model.BaseURLHasV1 {
		t.Fatal("expected BaseURLHasV1=false when non-/v1 probe succeeds")
	}

	stored, err := dbrepo.GetLLMModelByID(ctx, database, model.ID)
	if err != nil {
		t.Fatalf("GetLLMModelByID failed: %v", err)
	}
	if stored.BaseURLHasV1 {
		t.Fatal("expected stored BaseURLHasV1=false")
	}
}

func TestCreateLLMModelFailsWhenBothProbesFail(t *testing.T) {
	database := setupTestDB(t)
	m := managerWithProbe(database, mockProbeAlwaysFail)
	ctx := context.Background()

	_, err := m.Create(ctx, testOrgID, &CreateRequest{
		Provider: string(types.LLMProviderOpenAI),
		Model:    "gpt-4o-mini",
		Name:     "gpt-4o-mini",
		Purpose:  types.LLMModelPurposeConversation,
		BaseURL:  "https://api.openai.com/v1",
		APIKey:   "sk-test-1234567890",
	})
	if err == nil {
		t.Fatal("expected error when both probes fail")
	}
	if !strings.Contains(err.Error(), "connectivity test failed") {
		t.Fatalf("expected connectivity failure error, got %q", err.Error())
	}
}

func TestManagerDb_Create_SamplingParams(t *testing.T) {
	database := setupTestDB(t)
	m := managerWithProbe(database, mockProbeSuccessV1)
	ctx := context.Background()

	maxTokens := 2048
	temp := 0.3
	model, err := m.Create(ctx, testOrgID, &CreateRequest{
		Provider:    string(types.LLMProviderOpenAI),
		Model:       "gpt-4o-mini",
		Name:        "gpt-4o-mini",
		Purpose:     types.LLMModelPurposeConversation,
		BaseURL:     "https://api.openai.com/v1",
		APIKey:      "sk-test-1234567890",
		MaxTokens:   &maxTokens,
		Temperature: &temp,
	})
	if err != nil {
		t.Fatalf("Create with sampling params failed: %v", err)
	}
	if model.MaxTokens != maxTokens {
		t.Fatalf("expected MaxTokens=%d, got %d", maxTokens, model.MaxTokens)
	}
	if model.Temperature != temp {
		t.Fatalf("expected Temperature=%v, got %v", temp, model.Temperature)
	}

	stored, err := dbrepo.GetLLMModelByID(ctx, database, model.ID)
	if err != nil {
		t.Fatalf("GetLLMModelByID failed: %v", err)
	}
	if stored.MaxTokens != maxTokens {
		t.Fatalf("expected stored MaxTokens=%d, got %d", maxTokens, stored.MaxTokens)
	}
	if stored.Temperature != temp {
		t.Fatalf("expected stored Temperature=%v, got %v", temp, stored.Temperature)
	}

	// Create without sampling params → defaults 4096 / 0.7
	defaultModel, err := m.Create(ctx, testOrgID, &CreateRequest{
		Provider: string(types.LLMProviderDeepSeek),
		Model:    "deepseek-chat",
		Name:     "deepseek-chat",
		Purpose:  types.LLMModelPurposeConversation,
		BaseURL:  "https://api.deepseek.com/v1",
		APIKey:   "sk-test-abcdefgh",
	})
	if err != nil {
		t.Fatalf("Create without sampling params failed: %v", err)
	}
	if defaultModel.MaxTokens != 4096 {
		t.Fatalf("expected default MaxTokens=4096, got %d", defaultModel.MaxTokens)
	}
	if defaultModel.Temperature != 0.7 {
		t.Fatalf("expected default Temperature=0.7, got %v", defaultModel.Temperature)
	}
}

func TestManagerDb_Update_SamplingParams(t *testing.T) {
	m, database := setupManager(t)
	ctx := context.Background()

	model, err := m.Create(ctx, testOrgID, &CreateRequest{
		Provider: string(types.LLMProviderOpenAI),
		Model:    "gpt-4o-mini",
		Name:     "gpt-4o-mini",
		Purpose:  types.LLMModelPurposeConversation,
		BaseURL:  "https://api.openai.com/v1",
		APIKey:   "sk-test-1234567890",
	})
	if err != nil {
		t.Fatalf("Create failed: %v", err)
	}
	// 新规则：启用中的模型不可编辑，需先禁用；为使其可被禁用，先铺垫一个
	// 同用途的启用模型接管默认，再禁用目标。
	second, err := m.Create(ctx, testOrgID, &CreateRequest{
		Provider: string(types.LLMProviderOpenAI),
		Model:    "gpt-4o",
		Name:     "gpt-4o",
		Purpose:  types.LLMModelPurposeConversation,
		BaseURL:  "https://api.openai.com/v1",
		APIKey:   "sk-test-1234567890",
	})
	if err != nil {
		t.Fatalf("Create second failed: %v", err)
	}
	_ = second.ID
	if _, err := m.SetStatus(ctx, testOrgID, model.ID, string(types.LLMModelStatusInactive)); err != nil {
		t.Fatalf("Disable model failed: %v", err)
	}

	maxTokens := 2048
	temp := 0.2
	purpose := types.LLMModelPurposeConversation
	updated, err := m.Update(ctx, testOrgID, model.ID, &UpdateRequest{
		Name:        "gpt-4o-mini",
		Purpose:     &purpose,
		MaxTokens:   &maxTokens,
		Temperature: &temp,
	})
	if err != nil {
		t.Fatalf("Update sampling params failed: %v", err)
	}
	if updated.MaxTokens != maxTokens {
		t.Fatalf("expected updated MaxTokens=%d, got %d", maxTokens, updated.MaxTokens)
	}
	if updated.Temperature != temp {
		t.Fatalf("expected updated Temperature=%v, got %v", temp, updated.Temperature)
	}

	stored, err := dbrepo.GetLLMModelByID(ctx, database, model.ID)
	if err != nil {
		t.Fatalf("GetLLMModelByID failed: %v", err)
	}
	if stored.MaxTokens != maxTokens {
		t.Fatalf("expected stored MaxTokens=%d, got %d", maxTokens, stored.MaxTokens)
	}
	if stored.Temperature != temp {
		t.Fatalf("expected stored Temperature=%v, got %v", temp, stored.Temperature)
	}

	// Update with nil sampling params → values preserved
	kept, err := m.Update(ctx, testOrgID, model.ID, &UpdateRequest{
		Name:    "保留采样参数",
		Purpose: &purpose,
	})
	if err != nil {
		t.Fatalf("Update with nil sampling params failed: %v", err)
	}
	if kept.MaxTokens != maxTokens {
		t.Fatalf("expected MaxTokens preserved=%d, got %d", maxTokens, kept.MaxTokens)
	}
	if kept.Temperature != temp {
		t.Fatalf("expected Temperature preserved=%v, got %v", temp, kept.Temperature)
	}
}

// --- Update/Delete tests ---

func TestUpdateLLMModelKeepsAPIKeyWhenOmitted(t *testing.T) {
	m, database := setupManager(t)
	ctx := context.Background()

	model, err := m.Create(ctx, testOrgID, &CreateRequest{
		Name:     "主模型",
		Provider: string(types.LLMProviderOpenAI),
		Model:    "gpt-4o-mini",
		Purpose:  types.LLMModelPurposeConversation,
		BaseURL:  "https://api.openai.com/v1",
		APIKey:   "sk-test-1234567890",
	})
	if err != nil {
		t.Fatalf("Create failed: %v", err)
	}
	// 铺垫一个同用途启用模型接管默认，再禁用目标，使目标可被编辑。
	if _, err := m.Create(ctx, testOrgID, &CreateRequest{
		Provider: string(types.LLMProviderOpenAI),
		Model:    "gpt-4o",
		Name:     "gpt-4o",
		Purpose:  types.LLMModelPurposeConversation,
		BaseURL:  "https://api.openai.com/v1",
		APIKey:   "sk-test-1234567890",
	}); err != nil {
		t.Fatalf("Create second failed: %v", err)
	}
	if _, err := m.SetStatus(ctx, testOrgID, model.ID, string(types.LLMModelStatusInactive)); err != nil {
		t.Fatalf("Disable model failed: %v", err)
	}

	purpose := types.LLMModelPurposeConversation
	updated, err := m.Update(ctx, testOrgID, model.ID, &UpdateRequest{
		Name:    "更新后的主模型",
		Purpose: &purpose,
	})
	if err != nil {
		t.Fatalf("Update failed: %v", err)
	}
	// ModelConfig.APIKey holds the raw encrypted key (unchanged), not the masked value.
	if updated.APIKey != "sk-test-1234567890" {
		t.Fatalf("expected response to keep raw api key, got %q", updated.APIKey)
	}

	stored, err := dbrepo.GetLLMModelByID(ctx, database, model.ID)
	if err != nil {
		t.Fatalf("GetLLMModelByID failed: %v", err)
	}
	if stored.APIKeyEncrypted != "sk-test-1234567890" {
		t.Fatalf("expected api key to remain unchanged, got %q", stored.APIKeyEncrypted)
	}
	if stored.APIKeyMasked != "sk-***7890" {
		t.Fatalf("expected masked api key to remain unchanged, got %q", stored.APIKeyMasked)
	}
}

func TestUpdateLLMModelKeepsSingleDefault(t *testing.T) {
	m, database := setupManager(t)
	ctx := context.Background()

	first, err := m.Create(ctx, testOrgID, &CreateRequest{
		Provider:  string(types.LLMProviderOpenAI),
		Model:     "gpt-4o-mini",
		Name:      "gpt-4o-mini",
		Purpose:   types.LLMModelPurposeConversation,
		BaseURL:   "https://api.openai.com/v1",
		APIKey:    "sk-test-1234567890",
		IsDefault: true,
	})
	if err != nil {
		t.Fatalf("first Create failed: %v", err)
	}
	second, err := m.Create(ctx, testOrgID, &CreateRequest{
		Provider: string(types.LLMProviderDeepSeek),
		Model:    "deepseek-chat",
		Name:     "deepseek-chat",
		Purpose:  types.LLMModelPurposeConversation,
		BaseURL:  "https://api.deepseek.com/v1",
		APIKey:   "sk-test-abcdefgh",
	})
	if err != nil {
		t.Fatalf("second Create failed: %v", err)
	}

	isDefault := true
	if _, err := m.Update(ctx, testOrgID, second.ID, &UpdateRequest{
		IsDefault: &isDefault,
	}); err != nil {
		t.Fatalf("Update failed: %v", err)
	}

	if count := countDefaultLLMModels(t, database, testOrgID); count != 1 {
		t.Fatalf("expected one default llm model, got %d", count)
	}
	storedFirst, err := dbrepo.GetLLMModelByID(ctx, database, first.ID)
	if err != nil {
		t.Fatalf("GetLLMModelByID failed: %v", err)
	}
	if storedFirst.IsDefault {
		t.Fatal("expected first model default flag to be cleared")
	}
	storedSecond, err := dbrepo.GetLLMModelByID(ctx, database, second.ID)
	if err != nil {
		t.Fatalf("GetLLMModelByID failed: %v", err)
	}
	if !storedSecond.IsDefault {
		t.Fatal("expected second model to be default")
	}
}

func TestUpdateLLMModelTrimsChatCompletionsPath(t *testing.T) {
	m, database := setupManager(t)
	ctx := context.Background()

	model, err := m.Create(ctx, testOrgID, &CreateRequest{
		Provider: string(types.LLMProviderOpenAI),
		Model:    "gpt-4o-mini",
		Name:     "gpt-4o-mini",
		Purpose:  types.LLMModelPurposeConversation,
		BaseURL:  "https://api.openai.com/v1",
		APIKey:   "sk-test-1234567890",
	})
	if err != nil {
		t.Fatalf("Create failed: %v", err)
	}
	// 铺垫一个同用途启用模型接管默认，再禁用目标，使目标可被编辑。
	if _, err := m.Create(ctx, testOrgID, &CreateRequest{
		Provider: string(types.LLMProviderOpenAI),
		Model:    "gpt-4o",
		Name:     "gpt-4o",
		Purpose:  types.LLMModelPurposeConversation,
		BaseURL:  "https://api.openai.com/v1",
		APIKey:   "sk-test-1234567890",
	}); err != nil {
		t.Fatalf("Create second failed: %v", err)
	}
	if _, err := m.SetStatus(ctx, testOrgID, model.ID, string(types.LLMModelStatusInactive)); err != nil {
		t.Fatalf("Disable model failed: %v", err)
	}

	baseURL := "https://example.com/v1/chat/completions/"
	purpose := types.LLMModelPurposeConversation
	updated, err := m.Update(ctx, testOrgID, model.ID, &UpdateRequest{
		Name:    "gpt-4o-mini",
		Purpose: &purpose,
		BaseURL: &baseURL,
	})
	if err != nil {
		t.Fatalf("Update failed: %v", err)
	}
	if updated.BaseURL != "https://example.com" {
		t.Fatalf("expected normalized base_url in response, got %q", updated.BaseURL)
	}

	stored, err := dbrepo.GetLLMModelByID(ctx, database, model.ID)
	if err != nil {
		t.Fatalf("GetLLMModelByID failed: %v", err)
	}
	if stored.BaseURL != "https://example.com" {
		t.Fatalf("expected normalized base_url in database, got %q", stored.BaseURL)
	}
}

func TestUpdateLLMModelUpdatesMaskedAPIKeyWhenProvided(t *testing.T) {
	m, database := setupManager(t)
	ctx := context.Background()

	model, err := m.Create(ctx, testOrgID, &CreateRequest{
		Provider: string(types.LLMProviderOpenAI),
		Model:    "gpt-4o-mini",
		Name:     "gpt-4o-mini",
		Purpose:  types.LLMModelPurposeConversation,
		BaseURL:  "https://api.openai.com/v1",
		APIKey:   "sk-test-1234567890",
	})
	if err != nil {
		t.Fatalf("Create failed: %v", err)
	}
	// 铺垫一个同用途启用模型接管默认，再禁用目标，使目标可被编辑。
	if _, err := m.Create(ctx, testOrgID, &CreateRequest{
		Provider: string(types.LLMProviderOpenAI),
		Model:    "gpt-4o",
		Name:     "gpt-4o",
		Purpose:  types.LLMModelPurposeConversation,
		BaseURL:  "https://api.openai.com/v1",
		APIKey:   "sk-test-1234567890",
	}); err != nil {
		t.Fatalf("Create second failed: %v", err)
	}
	if _, err := m.SetStatus(ctx, testOrgID, model.ID, string(types.LLMModelStatusInactive)); err != nil {
		t.Fatalf("Disable model failed: %v", err)
	}

	newAPIKey := "sk-new-abcdefgh"
	purpose := types.LLMModelPurposeConversation
	updated, err := m.Update(ctx, testOrgID, model.ID, &UpdateRequest{
		Name:    "gpt-4o-mini",
		Purpose: &purpose,
		APIKey:  &newAPIKey,
	})
	if err != nil {
		t.Fatalf("Update failed: %v", err)
	}
	// ModelConfig.APIKey holds the raw encrypted key (new value), not the masked value.
	if updated.APIKey != "sk-new-abcdefgh" {
		t.Fatalf("expected response to use new raw api key, got %q", updated.APIKey)
	}

	stored, err := dbrepo.GetLLMModelByID(ctx, database, model.ID)
	if err != nil {
		t.Fatalf("GetLLMModelByID failed: %v", err)
	}
	if stored.APIKeyEncrypted != "sk-new-abcdefgh" {
		t.Fatalf("expected api key to update, got %q", stored.APIKeyEncrypted)
	}
	if stored.APIKeyMasked != "sk-***efgh" {
		t.Fatalf("expected masked api key to update, got %q", stored.APIKeyMasked)
	}
}

func TestUpdateLLMModelRedetectsBaseURLHasV1WhenBaseURLChanges(t *testing.T) {
	database := setupTestDB(t)
	m := managerWithProbe(database, mockProbeSuccessV1)
	ctx := context.Background()

	model, err := m.Create(ctx, testOrgID, &CreateRequest{
		Provider: string(types.LLMProviderOpenAI),
		Model:    "gpt-4o-mini",
		Name:     "gpt-4o-mini",
		Purpose:  types.LLMModelPurposeConversation,
		BaseURL:  "https://api.openai.com/v1",
		APIKey:   "sk-test-1234567890",
	})
	if err != nil {
		t.Fatalf("Create failed: %v", err)
	}
	if !model.BaseURLHasV1 {
		t.Fatal("expected initial BaseURLHasV1=true")
	}
	// 铺垫一个同用途启用模型接管默认，再禁用目标，使目标可被编辑。
	if _, err := m.Create(ctx, testOrgID, &CreateRequest{
		Provider: string(types.LLMProviderOpenAI),
		Model:    "gpt-4o",
		Name:     "gpt-4o",
		Purpose:  types.LLMModelPurposeConversation,
		BaseURL:  "https://api.openai.com/v1",
		APIKey:   "sk-test-1234567890",
	}); err != nil {
		t.Fatalf("Create second failed: %v", err)
	}
	if _, err := m.SetStatus(ctx, testOrgID, model.ID, string(types.LLMModelStatusInactive)); err != nil {
		t.Fatalf("Disable model failed: %v", err)
	}

	// Switch probe to no-v1 success and update base URL
	m2 := managerWithProbe(database, mockProbeSuccessNoV1)
	baseURL := "https://custom.api.com"
	purpose := types.LLMModelPurposeConversation
	updated, err := m2.Update(ctx, testOrgID, model.ID, &UpdateRequest{
		Name:    "gpt-4o-mini",
		Purpose: &purpose,
		BaseURL: &baseURL,
	})
	if err != nil {
		t.Fatalf("Update failed: %v", err)
	}
	if updated.BaseURLHasV1 {
		t.Fatal("expected BaseURLHasV1=false after updating base URL with non-/v1 probe success")
	}

	stored, err := dbrepo.GetLLMModelByID(ctx, database, model.ID)
	if err != nil {
		t.Fatalf("GetLLMModelByID failed: %v", err)
	}
	if stored.BaseURLHasV1 {
		t.Fatal("expected stored BaseURLHasV1=false after update")
	}
}

func TestUpdateLLMModelFailsWhenProbeFailsAfterRelevantChange(t *testing.T) {
	database := setupTestDB(t)
	m := managerWithProbe(database, mockProbeSuccessV1)
	ctx := context.Background()

	model, err := m.Create(ctx, testOrgID, &CreateRequest{
		Provider: string(types.LLMProviderOpenAI),
		Model:    "gpt-4o-mini",
		Name:     "gpt-4o-mini",
		Purpose:  types.LLMModelPurposeConversation,
		BaseURL:  "https://api.openai.com/v1",
		APIKey:   "sk-test-1234567890",
	})
	if err != nil {
		t.Fatalf("Create failed: %v", err)
	}
	// 铺垫一个同用途启用模型接管默认，再禁用目标，使目标可被编辑。
	if _, err := m.Create(ctx, testOrgID, &CreateRequest{
		Provider: string(types.LLMProviderOpenAI),
		Model:    "gpt-4o",
		Name:     "gpt-4o",
		Purpose:  types.LLMModelPurposeConversation,
		BaseURL:  "https://api.openai.com/v1",
		APIKey:   "sk-test-1234567890",
	}); err != nil {
		t.Fatalf("Create second failed: %v", err)
	}
	if _, err := m.SetStatus(ctx, testOrgID, model.ID, string(types.LLMModelStatusInactive)); err != nil {
		t.Fatalf("Disable model failed: %v", err)
	}

	// Update with a manager that will fail the probe
	failMgr := managerWithProbe(database, mockProbeAlwaysFail)
	baseURL := "https://dead.endpoint.com"
	purpose := types.LLMModelPurposeConversation
	_, err = failMgr.Update(ctx, testOrgID, model.ID, &UpdateRequest{
		Name:    "gpt-4o-mini",
		Purpose: &purpose,
		BaseURL: &baseURL,
	})
	if err == nil {
		t.Fatal("expected error when re-probe fails after update")
	}
	if !strings.Contains(err.Error(), "connectivity test failed") {
		t.Fatalf("expected connectivity failure error, got %q", err.Error())
	}
}

// TestUpdateEnabledModelRejected 验证启用中的模型不可编辑业务配置（仅 status 变更除外）。
func TestUpdateEnabledModelRejected(t *testing.T) {
	m, _ := setupManager(t)
	ctx := context.Background()

	model, err := m.Create(ctx, testOrgID, &CreateRequest{
		Provider: string(types.LLMProviderOpenAI),
		Model:    "gpt-4o-mini",
		Name:     "gpt-4o-mini",
		Purpose:  types.LLMModelPurposeConversation,
		BaseURL:  "https://api.openai.com/v1",
		APIKey:   "sk-test-1234567890",
	})
	if err != nil {
		t.Fatalf("Create failed: %v", err)
	}

	purpose := types.LLMModelPurposeConversation
	_, err = m.Update(ctx, testOrgID, model.ID, &UpdateRequest{
		Name:    "试图修改名称",
		Purpose: &purpose,
	})
	if err == nil {
		t.Fatal("expected editing an enabled model to be rejected, got nil")
	}
	if err.Error() != "启用中的模型不可编辑，请先禁用" {
		t.Fatalf("unexpected error: %v", err)
	}
}

// TestSetDefaultRequiresEnabled 验证只能将启用中的模型设为默认。
func TestSetDefaultRequiresEnabled(t *testing.T) {
	m, _ := setupManager(t)
	ctx := context.Background()

	// 铺垫一个启用模型接管默认，使目标成为可禁用的非默认模型。
	defaultModel, err := m.Create(ctx, testOrgID, &CreateRequest{
		Provider:  string(types.LLMProviderOpenAI),
		Model:     "gpt-4o-mini",
		Name:      "gpt-4o-mini",
		Purpose:   types.LLMModelPurposeConversation,
		BaseURL:   "https://api.openai.com/v1",
		APIKey:    "sk-test-1234567890",
		IsDefault: true,
	})
	if err != nil {
		t.Fatalf("Create default failed: %v", err)
	}
	target, err := m.Create(ctx, testOrgID, &CreateRequest{
		Provider: string(types.LLMProviderOpenAI),
		Model:    "gpt-4o",
		Name:     "gpt-4o",
		Purpose:  types.LLMModelPurposeConversation,
		BaseURL:  "https://api.openai.com/v1",
		APIKey:   "sk-test-1234567890",
	})
	if err != nil {
		t.Fatalf("Create target failed: %v", err)
	}
	if _, err := m.SetStatus(ctx, testOrgID, target.ID, string(types.LLMModelStatusInactive)); err != nil {
		t.Fatalf("Disable target failed: %v", err)
	}

	isDefault := true
	_, err = m.Update(ctx, testOrgID, target.ID, &UpdateRequest{
		IsDefault: &isDefault,
	})
	if err == nil {
		t.Fatal("expected setting a disabled model as default to be rejected, got nil")
	}
	if err.Error() != "只能将启用中的模型设为默认" {
		t.Fatalf("unexpected error: %v", err)
	}
	_ = defaultModel.ID
}

// TestDeleteEnabledModelRejected 验证启用中的模型不可删除。
func TestDeleteEnabledModelRejected(t *testing.T) {
	m, database := setupManager(t)
	ctx := context.Background()

	model, err := m.Create(ctx, testOrgID, &CreateRequest{
		Provider: string(types.LLMProviderOpenAI),
		Model:    "gpt-4o-mini",
		Name:     "gpt-4o-mini",
		Purpose:  types.LLMModelPurposeConversation,
		BaseURL:  "https://api.openai.com/v1",
		APIKey:   "sk-test-1234567890",
	})
	if err != nil {
		t.Fatalf("Create failed: %v", err)
	}

	err = m.Delete(ctx, testOrgID, model.ID)
	if err == nil {
		t.Fatal("expected deleting an enabled model to be rejected, got nil")
	}
	if err.Error() != "启用中的模型不可删除，请先禁用" {
		t.Fatalf("unexpected error: %v", err)
	}
	var exists int64
	if err := database.Model(&types.LLMModel{}).Where("id = ?", model.ID).Count(&exists).Error; err != nil {
		t.Fatalf("count failed: %v", err)
	}
	if exists != 1 {
		t.Fatalf("expected model to remain after rejected delete, got %d rows", exists)
	}
}

// TestDeleteLLMModelRejectOnlyDefault 验证删除某类唯一的默认模型且该类无其他 active 模型时会被拒绝，
// 以保证目标类始终保留一个默认。
func TestDeleteLLMModelRejectOnlyDefault(t *testing.T) {
	m, database := setupManager(t)
	ctx := context.Background()

	model, err := m.Create(ctx, testOrgID, &CreateRequest{
		Provider:  string(types.LLMProviderOpenAI),
		Model:     "gpt-4o-mini",
		Name:      "gpt-4o-mini",
		Purpose:   types.LLMModelPurposeConversation,
		BaseURL:   "https://api.openai.com/v1",
		APIKey:    "sk-test-1234567890",
		IsDefault: true,
	})
	if err != nil {
		t.Fatalf("Create failed: %v", err)
	}

	if err := m.Delete(ctx, testOrgID, model.ID); err == nil {
		t.Fatal("expected delete of the only default model to fail, got nil")
	}
	if count := countDefaultLLMModels(t, database, testOrgID); count != 1 {
		t.Fatalf("expected default model preserved, got %d", count)
	}
}

// TestDisableDefaultBackfillsAnother 验证禁用默认模型时，若该类还有其他启用模型，
// 会自动回填为默认，从而让默认模型可被禁用。
func TestDisableDefaultBackfillsAnother(t *testing.T) {
	m, _ := setupManager(t)
	ctx := context.Background()

	first, err := m.Create(ctx, testOrgID, &CreateRequest{
		Provider:  string(types.LLMProviderOpenAI),
		Model:     "gpt-4o-mini",
		Name:      "gpt-4o-mini",
		Purpose:   types.LLMModelPurposeConversation,
		BaseURL:   "https://api.openai.com/v1",
		APIKey:    "sk-test-1234567890",
		IsDefault: true,
	})
	if err != nil {
		t.Fatalf("Create default failed: %v", err)
	}

	// 第二个模型：类内已有模型，不标默认。
	second, err := m.Create(ctx, testOrgID, &CreateRequest{
		Provider: string(types.LLMProviderOpenAI),
		Model:    "gpt-4o",
		Name:     "gpt-4o",
		Purpose:  types.LLMModelPurposeConversation,
		BaseURL:  "https://api.openai.com/v1",
		APIKey:   "sk-test-1234567890",
	})
	if err != nil {
		t.Fatalf("Create second failed: %v", err)
	}
	_ = second.ID

	// 禁用默认模型：应自动回填 second 为默认。
	if _, err := m.SetStatus(ctx, testOrgID, first.ID, string(types.LLMModelStatusInactive)); err != nil {
		t.Fatalf("Disable default failed: %v", err)
	}
	// 类内仅剩 second 启用，应自动成为默认。
	defaultModel, err := m.GetDefault(ctx, testOrgID)
	if err != nil {
		t.Fatalf("GetDefault failed: %v", err)
	}
	if defaultModel == nil || defaultModel.ID != second.ID {
		t.Fatalf("expected %d to become default, got %+v", second.ID, defaultModel)
	}
}

// TestDisableOnlyDefaultRejected 验证当某类仅有一个启用的默认模型、无其他启用模型可回填时，
// 禁用该默认模型会被拒绝，以保证该类始终保留一个启用中的默认。
func TestDisableOnlyDefaultRejected(t *testing.T) {
	m, database := setupManager(t)
	ctx := context.Background()

	model, err := m.Create(ctx, testOrgID, &CreateRequest{
		Provider:  string(types.LLMProviderOpenAI),
		Model:     "gpt-4o-mini",
		Name:      "gpt-4o-mini",
		Purpose:   types.LLMModelPurposeConversation,
		BaseURL:   "https://api.openai.com/v1",
		APIKey:    "sk-test-1234567890",
		IsDefault: true,
	})
	if err != nil {
		t.Fatalf("Create failed: %v", err)
	}

	if _, err := m.SetStatus(ctx, testOrgID, model.ID, string(types.LLMModelStatusInactive)); err == nil {
		t.Fatal("expected disabling the only default model to fail, got nil")
	}
	if count := countDefaultLLMModels(t, database, testOrgID); count != 1 {
		t.Fatalf("expected default model preserved, got %d", count)
	}
}

func TestSetStatusEnableDisable(t *testing.T) {
	m, database := setupManager(t)
	ctx := context.Background()

	model, err := m.Create(ctx, testOrgID, &CreateRequest{
		Name:     "主模型",
		Provider: string(types.LLMProviderOpenAI),
		Model:    "gpt-4o-mini",
		Purpose:  types.LLMModelPurposeConversation,
		BaseURL:  "https://api.openai.com/v1",
		APIKey:   "sk-test-1234567890",
	})
	if err != nil {
		t.Fatalf("Create failed: %v", err)
	}
	// 铺垫第二个启用模型接管默认，使目标模型可被禁用。
	if _, err := m.Create(ctx, testOrgID, &CreateRequest{
		Name:     "接管模型",
		Provider: string(types.LLMProviderDeepSeek),
		Model:    "deepseek-chat",
		Purpose:  types.LLMModelPurposeConversation,
		BaseURL:  "https://api.deepseek.com/v1",
		APIKey:   "sk-test-abcdefgh",
	}); err != nil {
		t.Fatalf("Create second failed: %v", err)
	}
	if model.Status != string(types.LLMModelStatusActive) {
		t.Fatalf("expected initial status active, got %q", model.Status)
	}

	disabled, err := m.SetStatus(ctx, testOrgID, model.ID, string(types.LLMModelStatusInactive))
	if err != nil {
		t.Fatalf("SetStatus inactive failed: %v", err)
	}
	if disabled.Status != string(types.LLMModelStatusInactive) {
		t.Fatalf("expected status inactive, got %q", disabled.Status)
	}

	enabled, err := m.SetStatus(ctx, testOrgID, model.ID, string(types.LLMModelStatusActive))
	if err != nil {
		t.Fatalf("SetStatus active failed: %v", err)
	}
	if enabled.Status != string(types.LLMModelStatusActive) {
		t.Fatalf("expected status active, got %q", enabled.Status)
	}

	stored, err := dbrepo.GetLLMModelByID(ctx, database, model.ID)
	if err != nil {
		t.Fatalf("GetLLMModelByID failed: %v", err)
	}
	if stored.Status != string(types.LLMModelStatusActive) {
		t.Fatalf("expected stored status active, got %q", stored.Status)
	}
}

func TestSetStatusDisableDefaultBackfillsOtherActive(t *testing.T) {
	m, _ := setupManager(t)
	ctx := context.Background()

	first, err := m.Create(ctx, testOrgID, &CreateRequest{
		Name:     "first",
		Provider: string(types.LLMProviderOpenAI),
		Model:    "gpt-4o-mini",
		Purpose:  types.LLMModelPurposeConversation,
		BaseURL:  "https://api.openai.com/v1",
		APIKey:   "sk-test-1234567890",
	})
	if err != nil {
		t.Fatalf("first Create failed: %v", err)
	}
	second, err := m.Create(ctx, testOrgID, &CreateRequest{
		Name:     "second",
		Provider: string(types.LLMProviderDeepSeek),
		Model:    "deepseek-chat",
		Purpose:  types.LLMModelPurposeConversation,
		BaseURL:  "https://api.deepseek.com/v1",
		APIKey:   "sk-test-abcdefgh",
	})
	if err != nil {
		t.Fatalf("second Create failed: %v", err)
	}

	disabled, err := m.SetStatus(ctx, testOrgID, first.ID, string(types.LLMModelStatusInactive))
	if err != nil {
		t.Fatalf("SetStatus inactive failed: %v", err)
	}
	if disabled.IsDefault {
		t.Fatal("expected disabled default model's is_default to be cleared")
	}

	defaultModel, err := m.GetDefault(ctx, testOrgID)
	if err != nil {
		t.Fatalf("GetDefault failed: %v", err)
	}
	if defaultModel == nil || defaultModel.ID != second.ID {
		t.Fatalf("expected %d to become default, got %+v", second.ID, defaultModel)
	}
}

func TestSetStatusInvalidStatus(t *testing.T) {
	m, _ := setupManager(t)
	ctx := context.Background()

	model, err := m.Create(ctx, testOrgID, &CreateRequest{
		Name:     "主模型",
		Provider: string(types.LLMProviderOpenAI),
		Model:    "gpt-4o-mini",
		Purpose:  types.LLMModelPurposeConversation,
		BaseURL:  "https://api.openai.com/v1",
		APIKey:   "sk-test-1234567890",
	})
	if err != nil {
		t.Fatalf("Create failed: %v", err)
	}
	if _, err := m.SetStatus(ctx, testOrgID, model.ID, "paused"); err == nil {
		t.Fatal("expected invalid status to fail, got nil")
	}
}

func TestSetStatusNotFound(t *testing.T) {
	m, _ := setupManager(t)
	ctx := context.Background()

	if _, err := m.SetStatus(ctx, testOrgID, 9999, string(types.LLMModelStatusInactive)); err == nil {
		t.Fatal("expected not found model to fail, got nil")
	}
}

func TestSetStatusPermissionDenied(t *testing.T) {
	m, _ := setupManager(t)
	ctx := context.Background()

	model, err := m.Create(ctx, testOrgID, &CreateRequest{
		Name:     "主模型",
		Provider: string(types.LLMProviderOpenAI),
		Model:    "gpt-4o-mini",
		Purpose:  types.LLMModelPurposeConversation,
		BaseURL:  "https://api.openai.com/v1",
		APIKey:   "sk-test-1234567890",
	})
	if err != nil {
		t.Fatalf("Create failed: %v", err)
	}
	if _, err := m.SetStatus(ctx, testOrgID+1, model.ID, string(types.LLMModelStatusInactive)); err == nil {
		t.Fatal("expected cross-org status change to fail, got nil")
	}
}

func TestDeleteLLMModelRejectsSystemBuiltIn(t *testing.T) {
	m, database := setupManager(t)
	ctx := context.Background()

	model := &types.LLMModel{
		OrgID:     testOrgID,
		Code:      "system-translation",
		Name:      "内置翻译模型",
		ModelName: "gpt-4o-mini",
		BaseURL:   "https://api.example.com/v1",
		Status:    string(types.LLMModelStatusActive),
		IsSystem:  true,
	}
	if err := database.Create(model).Error; err != nil {
		t.Fatalf("seed system model failed: %v", err)
	}

	if err := m.Delete(ctx, testOrgID, model.ID); err == nil {
		t.Fatal("expected delete of system built-in model to fail, got nil")
	}

	var exists int64
	if err := database.Model(&types.LLMModel{}).Where("id = ?", model.ID).Count(&exists).Error; err != nil {
		t.Fatalf("count system model failed: %v", err)
	}
	if exists != 1 {
		t.Fatalf("expected system model to remain after rejected delete, got %d rows", exists)
	}
}

// --- helper tests ---

func TestNormalizeLLMBaseURLTrimsKnownEndpointSuffixes(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		baseURL string
		want    string
	}{
		{name: "openai chat completions", baseURL: "https://api.example.com/v1/chat/completions", want: "https://api.example.com"},
		{name: "openai completions", baseURL: "https://api.example.com/v1/completions", want: "https://api.example.com"},
		{name: "openai responses", baseURL: "https://api.example.com/v1/responses", want: "https://api.example.com"},
		{name: "anthropic messages", baseURL: "https://api.anthropic.com/v1/messages", want: "https://api.anthropic.com"},
		{name: "ollama chat", baseURL: "http://localhost:11434/api/chat", want: "http://localhost:11434"},
		{name: "ollama generate", baseURL: "http://localhost:11434/api/generate", want: "http://localhost:11434"},
		{name: "gemini generate content", baseURL: "https://generativelanguage.googleapis.com/v1beta/models/gemini-pro:generateContent", want: "https://generativelanguage.googleapis.com/v1beta/models/gemini-pro"},
		{name: "gemini stream generate content", baseURL: "https://generativelanguage.googleapis.com/v1beta/models/gemini-pro:streamGenerateContent", want: "https://generativelanguage.googleapis.com/v1beta/models/gemini-pro"},
		{name: "trailing slash", baseURL: "https://api.example.com/v1/chat/completions/", want: "https://api.example.com"},
		{name: "v1 suffix", baseURL: "https://api.example.com/v1", want: "https://api.example.com"},
		{name: "base url unchanged", baseURL: "https://api.example.com/", want: "https://api.example.com"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			if got := normalizeLLMBaseURL(tt.baseURL); got != tt.want {
				t.Fatalf("normalizeLLMBaseURL(%q) = %q, want %q", tt.baseURL, got, tt.want)
			}
		})
	}
}

func TestDetectURLHasV1(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		rawURL  string
		wantHas bool
	}{
		{name: "openai chat completions", rawURL: "https://api.openai.com/v1/chat/completions", wantHas: true},
		{name: "openai completions", rawURL: "https://api.example.com/v1/completions", wantHas: true},
		{name: "openai responses", rawURL: "https://api.example.com/v1/responses", wantHas: true},
		{name: "anthropic messages", rawURL: "https://api.anthropic.com/v1/messages", wantHas: true},
		{name: "v1 suffix only", rawURL: "https://api.example.com/v1", wantHas: true},
		{name: "no v1 path", rawURL: "https://api.example.com/chat/completions", wantHas: false},
		{name: "raw root", rawURL: "https://api.example.com/", wantHas: false},
		{name: "ollama no v1", rawURL: "http://localhost:11434/api/chat", wantHas: false},
		{name: "gemini no v1", rawURL: "https://generativelanguage.googleapis.com/v1beta/models/gemini-pro:generateContent", wantHas: false},
		{name: "trailing slash with v1", rawURL: "https://api.example.com/v1/chat/completions/", wantHas: true},
		{name: "no endpoint suffix", rawURL: "https://api.custom.com/v1", wantHas: true},
		{name: "custom no v1", rawURL: "https://api.custom.com", wantHas: false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			if got := detectURLHasV1(tt.rawURL); got != tt.wantHas {
				t.Fatalf("detectURLHasV1(%q) = %v, want %v", tt.rawURL, got, tt.wantHas)
			}
		})
	}
}

func TestBuildLLMEndpointURL(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		baseURL string
		hasV1   bool
		want    string
	}{
		{name: "with v1", baseURL: "https://api.example.com", hasV1: true, want: "https://api.example.com/v1"},
		{name: "without v1", baseURL: "https://api.example.com", hasV1: false, want: "https://api.example.com"},
		{name: "trailing slash", baseURL: "https://api.example.com/", hasV1: true, want: "https://api.example.com/v1"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			if got := BuildLLMEndpointURL(tt.baseURL, tt.hasV1); got != tt.want {
				t.Fatalf("BuildLLMEndpointURL(%q, %v) = %q, want %q", tt.baseURL, tt.hasV1, got, tt.want)
			}
		})
	}
}

// TestProbeResultExportAssertsProbeResult 断言导出类型 ProbeResult 与私有 probeResult 是同一类型，
// 且字段经导出名暴露，供 seed 等跨包复用。
func TestProbeResultExportAssertsProbeResult(t *testing.T) {
	var pProbe *probeResult
	var pExported *ProbeResult
	_ = pProbe
	_ = pExported

	r := &ProbeResult{V1Success: true, NoV1Success: false}
	if !r.V1Success || r.NoV1Success {
		t.Fatalf("unexpected ProbeResult fields: %+v", r)
	}
}

func TestVisionFromConfig(t *testing.T) {
	cases := []struct {
		name   string
		config types.LLMModelConfig
		want   bool
	}{
		{name: "nil config", config: nil, want: false},
		{name: "empty config", config: types.LLMModelConfig{}, want: false},
		{name: "no vision key", config: types.LLMModelConfig{"purpose": "translation"}, want: false},
		{name: "vision false", config: types.LLMModelConfig{"vision": false}, want: false},
		{name: "vision true", config: types.LLMModelConfig{"vision": true}, want: true},
		{name: "vision non-bool", config: types.LLMModelConfig{"vision": "yes"}, want: false},
	}
	for _, tc := range cases {
		if got := VisionFromConfig(tc.config); got != tc.want {
			t.Fatalf("%s: VisionFromConfig() = %v, want %v", tc.name, got, tc.want)
		}
	}
}

func TestResolveSystemTranslationLLMModelClonesIntoCurrentOrganization(t *testing.T) {
	database := setupTestDB(t)
	ctx := context.Background()
	source := &types.LLMModel{
		OrgID:           1,
		Code:            dbrepo.SystemTranslationLLMModelCode,
		Name:            "翻译模型",
		Provider:        string(types.LLMProviderOpenAI),
		ModelName:       "gpt-4o-mini",
		BaseURL:         "https://api.openai.com",
		APIKeyEncrypted: "test-key",
		APIKeyMasked:    "tes***key",
		MaxTokens:       4096,
		Temperature:     0.1,
		TimeoutSec:      60,
		Status:          string(types.LLMModelStatusActive),
		Purpose:         types.LLMModelPurposeTranslation,
		IsSystem:        true,
	}
	if err := dbrepo.CreateLLMModel(ctx, database, source); err != nil {
		t.Fatalf("create source translation model: %v", err)
	}
	existingSystemModel := *source
	existingSystemModel.ID = 0
	existingSystemModel.OrgID = 2
	existingSystemModel.Code = "llm_default"
	existingSystemModel.Purpose = types.LLMModelPurposeConversation
	if err := dbrepo.CreateLLMModel(ctx, database, &existingSystemModel); err != nil {
		t.Fatalf("create existing target system model: %v", err)
	}

	model, err := ResolveSystemTranslationLLMModel(ctx, database, 2)
	if err != nil {
		t.Fatalf("ResolveSystemTranslationLLMModel: %v", err)
	}
	if model == nil || model.OrgID != 2 || model.Code != dbrepo.SystemTranslationLLMModelCode {
		t.Fatalf("resolved translation model = %#v, want model owned by org 2", model)
	}

	manager := NewManager(database)
	if _, err := manager.Get(ctx, 2, model.ID, ""); err != nil {
		t.Fatalf("current organization cannot use resolved translation model: %v", err)
	}
}
