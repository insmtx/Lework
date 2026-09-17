package seed

import (
	"context"
	"strings"
	"testing"

	"gorm.io/driver/sqlite"
	"gorm.io/gorm"

	"github.com/insmtx/Leros/backend/config"
	infradb "github.com/insmtx/Leros/backend/internal/infra/db"
	"github.com/insmtx/Leros/backend/internal/llm"
	"github.com/insmtx/Leros/backend/types"
)

func newLLMTestDB(t *testing.T) *gorm.DB {
	t.Helper()
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	if err := db.AutoMigrate(&types.LLMModel{}); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	return db
}

// withProbeSuccessNoV1 用探测"无 /v1 成功"替身替换探针，避免测试打真实网络，并在测试结束后还原。
func withProbeSuccessNoV1(t *testing.T) {
	t.Helper()
	orig := probeLLMHasV1Fn
	probeLLMHasV1Fn = func(_ context.Context, _, _, _, _ string, _ bool) *llm.ProbeResult {
		return &llm.ProbeResult{NoV1Success: true}
	}
	t.Cleanup(func() { probeLLMHasV1Fn = orig })
}

// withProbeAlwaysFail 用"探测全部失败"替身替换探针，验证失败降级而非阻断。
// 同时断言探测收到带超时的 context，避免上游挂起把启动拖死。
func withProbeAlwaysFail(t *testing.T) {
	t.Helper()
	orig := probeLLMHasV1Fn
	probeLLMHasV1Fn = func(ctx context.Context, _, _, _, _ string, _ bool) *llm.ProbeResult {
		if _, ok := ctx.Deadline(); !ok {
			t.Errorf("probe context must carry a deadline to bound startup delay")
		}
		return &llm.ProbeResult{}
	}
	t.Cleanup(func() { probeLLMHasV1Fn = orig })
}

// withProbeNilResult 用"探测无任何结果"替身替换探针（上游超时等场景）。
func withProbeNilResult(t *testing.T) {
	t.Helper()
	orig := probeLLMHasV1Fn
	probeLLMHasV1Fn = func(_ context.Context, _, _, _, _ string, _ bool) *llm.ProbeResult {
		return nil
	}
	t.Cleanup(func() { probeLLMHasV1Fn = orig })
}

func TestSeedLLMDefaultAndTranslation(t *testing.T) {
	withProbeSuccessNoV1(t)
	db := newLLMTestDB(t)
	cfg := &config.LLMConfig{
		Provider: "deepseek",
		Model:    "deepseek-v4-flash",
		BaseURL:  "https://api.deepseek.com",
		APIKey:   "sk-test-0123456789",
	}

	if err := seedLLM(context.Background(), db, cfg); err != nil {
		t.Fatalf("seedLLM: %v", err)
	}

	var models []types.LLMModel
	if err := db.Find(&models).Error; err != nil {
		t.Fatalf("list models: %v", err)
	}
	if len(models) != 2 {
		t.Fatalf("expected 2 models, got %d", len(models))
	}
	if models[0].OrgID != 1 || models[0].Code != "llm_default" {
		t.Fatalf("unexpected default model: %+v", models[0])
	}
	if models[0].Name != "内置对话模型" {
		t.Fatalf("expected default model name 内置对话模型, got %q", models[0].Name)
	}
	foundTranslation := false
	for _, m := range models {
		if m.Code == infradb.SystemTranslationLLMModelCode && m.OrgID == 1 && m.IsSystem {
			foundTranslation = true
			if !m.IsDefault {
				t.Fatalf("expected translation model to be default, got %+v", m)
			}
		}
	}
	if !foundTranslation {
		t.Fatalf("expected system translation model, got %+v", models)
	}
}

func TestSeedLLMStoresProbeHasV1(t *testing.T) {
	withProbeSuccessNoV1(t)
	db := newLLMTestDB(t)
	cfg := &config.LLMConfig{
		Provider: "openai",
		Model:    "deepseek/deepseek-v4-flash",
		BaseURL:  "https://api.yygu.cn/v3/llm.chat/",
		APIKey:   "sk-test-0123456789",
	}

	if err := seedLLM(context.Background(), db, cfg); err != nil {
		t.Fatalf("seedLLM: %v", err)
	}

	var models []types.LLMModel
	if err := db.Find(&models).Error; err != nil {
		t.Fatalf("list models: %v", err)
	}
	if len(models) != 2 {
		t.Fatalf("expected 2 models, got %d", len(models))
	}
	for _, m := range models {
		if m.BaseURLHasV1 {
			t.Fatalf("expected BaseURLHasV1=false from no-V1 probe, got true for code=%s", m.Code)
		}
	}
}

// TestSeedLLMProceedsWhenProbeFails 验证模型不可用不会阻断初始化：
// 探测全部失败时仍落库两个模型，base_url_has_v1 回退到 base_url 是否显式带 /v1。
func TestSeedLLMProceedsWhenProbeFails(t *testing.T) {
	withProbeAlwaysFail(t)
	db := newLLMTestDB(t)
	cfg := &config.LLMConfig{
		Provider: "openai",
		Model:    "deepseek/deepseek-v4-flash",
		BaseURL:  "https://api.yygu.cn/v3/llm.chat/",
		APIKey:   "sk-test-0123456789",
	}

	if err := seedLLM(context.Background(), db, cfg); err != nil {
		t.Fatalf("seedLLM must not fail on probe failure, got %v", err)
	}

	var models []types.LLMModel
	if err := db.Find(&models).Error; err != nil {
		t.Fatalf("list models: %v", err)
	}
	if len(models) != 2 {
		t.Fatalf("expected 2 models seeded despite probe failure, got %d", len(models))
	}
	for _, m := range models {
		if m.BaseURLHasV1 {
			t.Fatalf("expected fallback BaseURLHasV1=false for base_url without /v1, got true for code=%s", m.Code)
		}
	}
}

// TestSeedLLMProbeFailureKeepsV1Hint 验证探测失败时的回退遵循 base_url 显式 /v1 后缀。
func TestSeedLLMProbeFailureKeepsV1Hint(t *testing.T) {
	withProbeAlwaysFail(t)
	db := newLLMTestDB(t)
	cfg := &config.LLMConfig{
		Provider: "openai",
		Model:    "gpt-4o",
		BaseURL:  "https://api.example.com/v1",
		APIKey:   "sk-test-0123456789",
	}

	if err := seedLLM(context.Background(), db, cfg); err != nil {
		t.Fatalf("seedLLM must not fail on probe failure, got %v", err)
	}

	var defaultModel types.LLMModel
	if err := db.Where("code = ?", defaultLLMModelCode).First(&defaultModel).Error; err != nil {
		t.Fatalf("find default model: %v", err)
	}
	if !defaultModel.BaseURLHasV1 {
		t.Fatal("expected fallback BaseURLHasV1=true when base_url explicitly carries /v1")
	}
}

// TestSeedLLMProceedsWhenProbeReturnsNil 验证探测完全无结果（如上游超时）时同样不阻断启动。
func TestSeedLLMProceedsWhenProbeReturnsNil(t *testing.T) {
	withProbeNilResult(t)
	db := newLLMTestDB(t)
	cfg := &config.LLMConfig{
		Provider: "openai",
		Model:    "gpt-4o",
		BaseURL:  "https://api.example.com",
		APIKey:   "sk-test-0123456789",
	}

	if err := seedLLM(context.Background(), db, cfg); err != nil {
		t.Fatalf("seedLLM must not fail on nil probe result, got %v", err)
	}

	var count int64
	db.Model(&types.LLMModel{}).Count(&count)
	if count != 2 {
		t.Fatalf("expected 2 models seeded despite nil probe result, got %d", count)
	}
}

func TestSeedLLMSkipsWhenNoAPIKey(t *testing.T) {
	withProbeSuccessNoV1(t)
	db := newLLMTestDB(t)
	if err := seedLLM(context.Background(), db, &config.LLMConfig{}); err != nil {
		t.Fatalf("seedLLM: %v", err)
	}
	var count int64
	db.Model(&types.LLMModel{}).Count(&count)
	if count != 0 {
		t.Fatalf("expected 0 models when no api key, got %d", count)
	}
}

func TestSeedLLMIdempotent(t *testing.T) {
	withProbeSuccessNoV1(t)
	db := newLLMTestDB(t)
	cfg := &config.LLMConfig{APIKey: "sk-test-0123456789", Model: "m", Provider: "custom"}
	if err := seedLLM(context.Background(), db, cfg); err != nil {
		t.Fatalf("first seedLLM: %v", err)
	}
	if err := seedLLM(context.Background(), db, cfg); err != nil {
		t.Fatalf("second seedLLM: %v", err)
	}
	var count int64
	db.Model(&types.LLMModel{}).Count(&count)
	if count != 2 {
		t.Fatalf("expected 2 models after second run, got %d", count)
	}
}

func TestSeedLLMDefaultModelVisionTrue(t *testing.T) {
	withProbeSuccessNoV1(t)
	db := newLLMTestDB(t)
	cfg := &config.LLMConfig{
		Provider: "openai",
		Model:    "gpt-4o",
		BaseURL:  "https://api.example.com",
		APIKey:   "sk-test-0123456789",
		Vision:   true,
	}

	if err := seedLLM(context.Background(), db, cfg); err != nil {
		t.Fatalf("seedLLM: %v", err)
	}

	var defaultModel types.LLMModel
	if err := db.Where("code = ?", defaultLLMModelCode).First(&defaultModel).Error; err != nil {
		t.Fatalf("find default model: %v", err)
	}
	if v, ok := defaultModel.Config["vision"].(bool); !ok || !v {
		t.Fatalf("expected default model Config[vision]=true, got config=%+v", defaultModel.Config)
	}
}

func TestSeedLLMDefaultModelVisionFalse(t *testing.T) {
	withProbeSuccessNoV1(t)
	db := newLLMTestDB(t)
	cfg := &config.LLMConfig{
		Provider: "deepseek",
		Model:    "deepseek-v4-flash",
		BaseURL:  "https://api.deepseek.com",
		APIKey:   "sk-test-0123456789",
	}

	if err := seedLLM(context.Background(), db, cfg); err != nil {
		t.Fatalf("seedLLM: %v", err)
	}

	var defaultModel types.LLMModel
	if err := db.Where("code = ?", defaultLLMModelCode).First(&defaultModel).Error; err != nil {
		t.Fatalf("find default model: %v", err)
	}
	if _, ok := defaultModel.Config["vision"]; ok {
		t.Fatalf("expected no vision flag when config Vision unset, got config=%+v", defaultModel.Config)
	}
}

func TestPreferV1ForBaseURL(t *testing.T) {
	cases := []struct {
		name    string
		baseURL string
		want    bool
	}{
		{"root", "https://api.deepseek.com", false},
		{"root with slash", "https://api.deepseek.com/", false},
		{"v1 suffix", "https://api.deepseek.com/v1", true},
		{"v1 suffix with slash", "https://api.deepseek.com/v1/", true},
		{"surrounding spaces", " https://api.deepseek.com/v1 ", true},
		{"full v1 endpoint", "https://gw.example.com/v1/chat/completions", true},
		{"endpoint without v1", "https://gw.example.com/chat/completions", false},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := preferV1ForBaseURL(c.baseURL); got != c.want {
				t.Errorf("preferV1ForBaseURL(%q) = %v, want %v", c.baseURL, got, c.want)
			}
		})
	}
}

// TestSeedLLMProbeFailureKeepsV1HintForFullEndpoint 验证探测失败时，
// 即使 base_url 写的是完整端点（.../v1/chat/completions）也能推断出 /v1 前缀。
func TestSeedLLMProbeFailureKeepsV1HintForFullEndpoint(t *testing.T) {
	withProbeAlwaysFail(t)
	db := newLLMTestDB(t)
	cfg := &config.LLMConfig{
		Provider: "openai",
		Model:    "gpt-4o",
		BaseURL:  "https://gw.example.com/v1/chat/completions",
		APIKey:   "sk-test-0123456789",
	}

	if err := seedLLM(context.Background(), db, cfg); err != nil {
		t.Fatalf("seedLLM must not fail on probe failure, got %v", err)
	}

	var defaultModel types.LLMModel
	if err := db.Where("code = ?", defaultLLMModelCode).First(&defaultModel).Error; err != nil {
		t.Fatalf("find default model: %v", err)
	}
	if !defaultModel.BaseURLHasV1 {
		t.Fatal("expected fallback BaseURLHasV1=true for base_url with explicit /v1 path segment")
	}
	if defaultModel.BaseURL != "https://gw.example.com" {
		t.Fatalf("expected normalized base_url, got %q", defaultModel.BaseURL)
	}
}

func TestMaskAPIKey(t *testing.T) {
	masked := maskAPIKey("sk-abcd1234")
	if !strings.HasPrefix(masked, "sk-") || !strings.Contains(masked, "***") {
		t.Fatalf("unexpected mask: %q", masked)
	}
	if maskAPIKey("abc") != "***" {
		t.Fatal("short key must be fully masked")
	}
}
