package prompts

import (
	"context"
	"fmt"
	"time"

	einoschema "github.com/cloudwego/eino/schema"
	"github.com/ygpkg/yg-go/logs"

	"github.com/insmtx/Leros/backend/config"
	pkgeino "github.com/insmtx/Leros/backend/pkg/eino"
)

// EinoExecutor implements Executor using the Eino LLM framework.
type EinoExecutor struct{}

func NewEinoExecutor() *EinoExecutor {
	return &EinoExecutor{}
}

func (e *EinoExecutor) Execute(ctx context.Context, prompt string, cfg config.LLMConfig) (string, error) {
	logs.DebugContextf(ctx, "[prompts:eino] Execute: provider=%s model=%s prompt_len=%d prompt_head=%s",
		cfg.Provider, cfg.Model, len(prompt), truncate(prompt, 120))

	chatModel, err := pkgeino.NewChatModel(ctx, &pkgeino.ChatModelConfig{
		Provider: cfg.Provider,
		APIKey:   cfg.APIKey,
		Model:    cfg.Model,
		BaseURL:  cfg.BaseURL,
	})
	if err != nil {
		logs.ErrorContextf(ctx, "[prompts:eino] NewChatModel failed: provider=%s model=%s error=%v",
			cfg.Provider, cfg.Model, err)
		return "", fmt.Errorf("prompts:eino: create chat model: %w", err)
	}
	logs.DebugContextf(ctx, "[prompts:eino] NewChatModel ok: provider=%s model=%s", cfg.Provider, cfg.Model)

	messages := []*einoschema.Message{
		{Role: einoschema.User, Content: prompt},
	}
	logs.DebugContextf(ctx, "[prompts:eino] Generate: messages=%d", len(messages))

	t0 := time.Now()
	response, err := chatModel.Generate(ctx, messages)
	elapsed := time.Since(t0)

	if err != nil {
		logs.ErrorContextf(ctx, "[prompts:eino] Generate failed: elapsed=%v error=%v", elapsed, err)
		return "", fmt.Errorf("prompts:eino: generate: %w", err)
	}

	finishReason := ""
	if response != nil && response.ResponseMeta != nil {
		finishReason = response.ResponseMeta.FinishReason
	}

	responseLen := 0
	if response != nil {
		responseLen = len(response.Content)
	}

	logs.DebugContextf(ctx, "[prompts:eino] Generate ok: elapsed=%v response_len=%d finish_reason=%s response_tail=%s",
		elapsed, responseLen, finishReason, truncate(response.Content, 120))

	if response == nil {
		err = fmt.Errorf("prompts:eino: empty response from model")
		logs.ErrorContextf(ctx, "[prompts:eino] %v", err)
		return "", err
	}

	return response.Content, nil
}

func truncate(s string, maxLen int) string {
	runes := []rune(s)
	if len(runes) <= maxLen {
		return s
	}
	return string(runes[:maxLen]) + "..."
}
