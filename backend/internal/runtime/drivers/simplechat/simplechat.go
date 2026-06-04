package simplechat

import (
	"context"
	"fmt"
	"time"

	einomodel "github.com/cloudwego/eino/components/model"
	einoschema "github.com/cloudwego/eino/schema"
	"github.com/insmtx/Leros/backend/internal/agent"
	"github.com/insmtx/Leros/backend/internal/runtime/events"
	pkgeino "github.com/insmtx/Leros/backend/pkg/eino"
	"github.com/ygpkg/yg-go/logs"
)

var _ agent.Runner = (*SimpleChat)(nil)

type SimpleChat struct {
	chatModel    einomodel.ToolCallingChatModel
	systemPrompt string
}

type Config struct {
	LLMProvider string
	APIKey      string
	Model       string
	BaseURL     string
}

func LoadFromEnv() *Config {
	return &Config{
		LLMProvider: "openai",
		APIKey:      "",
		Model:       "gpt-4",
		BaseURL:     "",
	}
}

func New(ctx context.Context, cfg *Config) (*SimpleChat, error) {
	if cfg == nil {
		return nil, fmt.Errorf("config is required")
	}

	llmConfig := &pkgeino.ChatModelConfig{
		Provider: cfg.LLMProvider,
		APIKey:   cfg.APIKey,
		Model:    cfg.Model,
		BaseURL:  cfg.BaseURL,
	}

	chatModel, err := pkgeino.NewChatModel(ctx, llmConfig)
	if err != nil {
		return nil, fmt.Errorf("create chat model: %w", err)
	}

	return &SimpleChat{
		chatModel:    chatModel,
		systemPrompt: "You are a helpful assistant.",
	}, nil
}

func (s *SimpleChat) Run(ctx context.Context, req *agent.RequestContext) (*agent.RunResult, error) {
	if s == nil || s.chatModel == nil {
		return nil, fmt.Errorf("agent is not initialized")
	}

	startedAt := time.Now().UTC()

	if req.RunID == "" {
		req.RunID = fmt.Sprintf("run_%d", time.Now().UnixNano())
	}
	if req.TraceID == "" {
		req.TraceID = req.RunID
	}

	userInput := agent.BuildUserInput(req)
	if userInput == "" {
		return nil, fmt.Errorf("empty user input")
	}

	userMsg := agent.InputMessage{
		Role:    "user",
		Content: userInput,
	}
	req.Input.Messages = append(req.Input.Messages, userMsg)

	messages := []*einoschema.Message{
		{
			Role:    einoschema.System,
			Content: s.systemPrompt,
		},
		{
			Role:    einoschema.User,
			Content: userInput,
		},
	}

	response, err := s.chatModel.Generate(ctx, messages)
	if err != nil {
		return nil, fmt.Errorf("generate response: %w", err)
	}

	resultMessage := ""
	if response != nil && len(response.Content) > 0 {
		resultMessage = response.Content
	}

	usage := &events.UsagePayload{}
	if response != nil && response.ResponseMeta != nil && response.ResponseMeta.Usage != nil {
		usage.InputTokens = response.ResponseMeta.Usage.PromptTokens
		usage.OutputTokens = response.ResponseMeta.Usage.CompletionTokens
		usage.TotalTokens = response.ResponseMeta.Usage.TotalTokens
	}

	result := &agent.RunResult{
		RunID:       req.RunID,
		TraceID:     req.TraceID,
		Status:      agent.RunStatusCompleted,
		Message:     resultMessage,
		Usage:       usage,
		StartedAt:   startedAt,
		CompletedAt: time.Now().UTC(),
	}

	logs.InfoContextf(ctx, "SimpleChat run completed: run_id=%s status=%s message_len=%d",
		req.RunID, result.Status, len(resultMessage))

	return result, nil
}
