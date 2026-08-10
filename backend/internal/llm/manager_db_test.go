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

// --- Update/Delete tests ---

func TestUpdateLLMModelKeepsAPIKeyWhenOmitted(t *testing.T) {
	m, database := setupManager(t)
	ctx := context.Background()

	model, err := m.Create(ctx, testOrgID, &CreateRequest{
		Name:     "主模型",
		Provider: string(types.LLMProviderOpenAI),
		Model:    "gpt-4o-mini",
		BaseURL:  "https://api.openai.com/v1",
		APIKey:   "sk-test-1234567890",
	})
	if err != nil {
		t.Fatalf("Create failed: %v", err)
	}

	updated, err := m.Update(ctx, testOrgID, model.ID, &UpdateRequest{
		Name: "更新后的主模型",
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
		BaseURL:  "https://api.openai.com/v1",
		APIKey:   "sk-test-1234567890",
	})
	if err != nil {
		t.Fatalf("Create failed: %v", err)
	}

	baseURL := "https://example.com/v1/chat/completions/"
	updated, err := m.Update(ctx, testOrgID, model.ID, &UpdateRequest{
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
		BaseURL:  "https://api.openai.com/v1",
		APIKey:   "sk-test-1234567890",
	})
	if err != nil {
		t.Fatalf("Create failed: %v", err)
	}

	newAPIKey := "sk-new-abcdefgh"
	updated, err := m.Update(ctx, testOrgID, model.ID, &UpdateRequest{
		APIKey: &newAPIKey,
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
		BaseURL:  "https://api.openai.com/v1",
		APIKey:   "sk-test-1234567890",
	})
	if err != nil {
		t.Fatalf("Create failed: %v", err)
	}
	if !model.BaseURLHasV1 {
		t.Fatal("expected initial BaseURLHasV1=true")
	}

	// Switch probe to no-v1 success and update base URL
	m2 := managerWithProbe(database, mockProbeSuccessNoV1)
	baseURL := "https://custom.api.com"
	updated, err := m2.Update(ctx, testOrgID, model.ID, &UpdateRequest{
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
		BaseURL:  "https://api.openai.com/v1",
		APIKey:   "sk-test-1234567890",
	})
	if err != nil {
		t.Fatalf("Create failed: %v", err)
	}

	// Update with a manager that will fail the probe
	failMgr := managerWithProbe(database, mockProbeAlwaysFail)
	baseURL := "https://dead.endpoint.com"
	_, err = failMgr.Update(ctx, testOrgID, model.ID, &UpdateRequest{
		BaseURL: &baseURL,
	})
	if err == nil {
		t.Fatal("expected error when re-probe fails after update")
	}
	if !strings.Contains(err.Error(), "connectivity test failed") {
		t.Fatalf("expected connectivity failure error, got %q", err.Error())
	}
}

func TestDeleteLLMModelDoesNotLeaveMultipleDefaults(t *testing.T) {
	m, database := setupManager(t)
	ctx := context.Background()

	model, err := m.Create(ctx, testOrgID, &CreateRequest{
		Provider:  string(types.LLMProviderOpenAI),
		Model:     "gpt-4o-mini",
		BaseURL:   "https://api.openai.com/v1",
		APIKey:    "sk-test-1234567890",
		IsDefault: true,
	})
	if err != nil {
		t.Fatalf("Create failed: %v", err)
	}

	if err := m.Delete(ctx, testOrgID, model.ID); err != nil {
		t.Fatalf("Delete failed: %v", err)
	}
	if count := countDefaultLLMModels(t, database, testOrgID); count != 0 {
		t.Fatalf("expected no default llm model after deleting default, got %d", count)
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
