package modelrouter

import (
	"context"
	"testing"

	"github.com/insmtx/Leros/backend/internal/llm"
)

// ━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━
// mockManager
// ━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━

type mockManager struct {
	defaultCfg *llm.ModelConfig
	byNameCfg  *llm.ModelConfig
	byCodeCfg  *llm.ModelConfig
	byIDCfg    *llm.ModelConfig
}

func (m *mockManager) Create(ctx context.Context, orgID uint, req *llm.CreateRequest) (*llm.ModelConfig, error) {
	return nil, nil
}
func (m *mockManager) Get(ctx context.Context, orgID uint, id uint, code string) (*llm.ModelConfig, error) {
	return nil, nil
}
func (m *mockManager) GetDefault(ctx context.Context, orgID uint) (*llm.ModelConfig, error) {
	return m.defaultCfg, nil
}
func (m *mockManager) GetByModelName(ctx context.Context, orgID uint, modelName string) (*llm.ModelConfig, error) {
	return m.byNameCfg, nil
}
func (m *mockManager) GetByModelCode(ctx context.Context, orgID uint, code string) (*llm.ModelConfig, error) {
	return m.byCodeCfg, nil
}
func (m *mockManager) GetByModelID(ctx context.Context, orgID uint, modelID uint) (*llm.ModelConfig, error) {
	return m.byIDCfg, nil
}
func (m *mockManager) Update(ctx context.Context, orgID uint, id uint, req *llm.UpdateRequest) (*llm.ModelConfig, error) {
	return nil, nil
}
func (m *mockManager) SetStatus(ctx context.Context, orgID uint, id uint, status string) (*llm.ModelConfig, error) {
	return nil, nil
}
func (m *mockManager) Delete(ctx context.Context, orgID uint, id uint) error {
	return nil
}
func (m *mockManager) List(ctx context.Context, orgID uint, req *llm.ListRequest) (*llm.ListModelResult, error) {
	return nil, nil
}
func (m *mockManager) TestConnectivity(ctx context.Context, orgID uint, req *llm.TestRequest) (*llm.TestResult, error) {
	return nil, nil
}

// ━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━
// Call tests
// ━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━

func TestModelRouter_Call_DefaultModel(t *testing.T) {
	mock := newMockUpstreamServer(t)
	defer mock.Close()

	chatResp := []byte(`{"id":"chatcmpl-001","object":"chat.completion","created":1700000000,"model":"gpt-5","choices":[{"index":0,"message":{"role":"assistant","content":"Hello from GPT!"},"finish_reason":"stop"}],"usage":{"prompt_tokens":10,"completion_tokens":5,"total_tokens":15}}`)
	mock.setResponse(200, chatResp)

	router := NewModelRouter(
		&mockManager{
			defaultCfg: &llm.ModelConfig{
				Provider:     "openai",
				ModelName:    "gpt-5",
				BaseURL:      mock.server.URL,
				BaseURLHasV1: true,
				APIKey:       "test-key",
				Status:       "active",
			},
		},
		llm.NewCallerHTTP(mock.client, nil),
	)

	temp := 0.5
	result, err := router.Call(context.Background(), 1, &llm.CallRequest{
		Messages:    []llm.Message{{Role: "user", Content: "hello"}},
		Temperature: &temp,
		CallerType:  "test",
	})
	if err != nil {
		t.Fatalf("Call() error: %v", err)
	}
	if result == nil {
		t.Fatal("expected non-nil result")
	}
	if result.Message == nil || result.Message.Content != "Hello from GPT!" {
		t.Errorf("expected content 'Hello from GPT!', got %q", getContent(result))
	}
	if result.Usage == nil {
		t.Fatal("expected usage")
	}
	if result.Usage.InputTokens != 10 || result.Usage.OutputTokens != 5 || result.Usage.TotalTokens != 15 {
		t.Errorf("unexpected usage: %+v", result.Usage)
	}
}

func TestModelRouter_Call_WithModelCode(t *testing.T) {
	mock := newMockUpstreamServer(t)
	defer mock.Close()

	chatResp := []byte(`{"id":"chatcmpl-002","object":"chat.completion","created":1700000000,"model":"deepseek-v3","choices":[{"index":0,"message":{"role":"assistant","content":"Hello from DeepSeek!"},"finish_reason":"stop"}],"usage":{"prompt_tokens":20,"completion_tokens":10,"total_tokens":30}}`)
	mock.setResponse(200, chatResp)

	router := NewModelRouter(
		&mockManager{
			byCodeCfg: &llm.ModelConfig{
				Provider:     "deepseek",
				ModelName:    "deepseek-v3",
				BaseURL:      mock.server.URL,
				BaseURLHasV1: true,
				APIKey:       "test-key",
				Status:       "active",
			},
		},
		llm.NewCallerHTTP(mock.client, nil),
	)

	temp := 0.5
	result, err := router.Call(context.Background(), 1, &llm.CallRequest{
		Messages:    []llm.Message{{Role: "user", Content: "hello"}},
		Temperature: &temp,
		CallerType:  "test",
	}, WithModelCode("deepseek-v3"))
	if err != nil {
		t.Fatalf("Call() error: %v", err)
	}
	if result == nil {
		t.Fatal("expected non-nil result")
	}
	if result.Message == nil || result.Message.Content != "Hello from DeepSeek!" {
		t.Errorf("expected content 'Hello from DeepSeek!', got %q", getContent(result))
	}
	if result.Usage.TotalTokens != 30 {
		t.Errorf("unexpected usage: %+v", result.Usage)
	}
}

func TestModelRouter_Call_WithModelID(t *testing.T) {
	mock := newMockUpstreamServer(t)
	defer mock.Close()

	chatResp := []byte(`{"id":"chatcmpl-004","object":"chat.completion","created":1700000000,"model":"claude-4","choices":[{"index":0,"message":{"role":"assistant","content":"Hello from Claude!"},"finish_reason":"stop"}],"usage":{"prompt_tokens":15,"completion_tokens":8,"total_tokens":23}}`)
	mock.setResponse(200, chatResp)

	router := NewModelRouter(
		&mockManager{
			byIDCfg: &llm.ModelConfig{
				Provider:     "anthropic",
				ModelName:    "claude-4",
				BaseURL:      mock.server.URL,
				BaseURLHasV1: true,
				APIKey:       "test-key",
				Status:       "active",
			},
		},
		llm.NewCallerHTTP(mock.client, nil),
	)

	result, err := router.Call(context.Background(), 1, &llm.CallRequest{
		Messages:   []llm.Message{{Role: "user", Content: "hello"}},
		CallerType: "test",
	}, WithModelID(42))
	if err != nil {
		t.Fatalf("Call() error: %v", err)
	}
	if result == nil {
		t.Fatal("expected non-nil result")
	}
	if result.Message == nil || result.Message.Content != "Hello from Claude!" {
		t.Errorf("expected content 'Hello from Claude!', got %q", getContent(result))
	}
}

func TestModelRouter_Call_JSONResponseFormat(t *testing.T) {
	mock := newMockUpstreamServer(t)
	defer mock.Close()

	chatResp := []byte(`{"id":"chatcmpl-003","object":"chat.completion","created":1700000000,"model":"gpt-5","choices":[{"index":0,"message":{"role":"assistant","content":"{\"title\": \"hello\"}"},"finish_reason":"stop"}],"usage":{"prompt_tokens":5,"completion_tokens":3,"total_tokens":8}}`)
	mock.setResponse(200, chatResp)

	router := NewModelRouter(
		&mockManager{
			defaultCfg: &llm.ModelConfig{
				Provider:     "openai",
				ModelName:    "gpt-5",
				BaseURL:      mock.server.URL,
				BaseURLHasV1: true,
				APIKey:       "test-key",
				Status:       "active",
			},
		},
		llm.NewCallerHTTP(mock.client, nil),
	)

	result, err := router.Call(context.Background(), 1, &llm.CallRequest{
		Messages:   []llm.Message{{Role: "user", Content: "say hello"}},
		CallerType: "test",
	})
	if err != nil {
		t.Fatalf("Call() error: %v", err)
	}
	if result.Message == nil || result.Message.Content != `{"title": "hello"}` {
		t.Errorf("expected content, got %q", getContent(result))
	}
}
