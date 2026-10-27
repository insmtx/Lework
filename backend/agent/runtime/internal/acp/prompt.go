package acp

import (
	"context"
	"fmt"
	"strings"
	"time"

	acpsdk "github.com/coder/acp-go-sdk"
)

// DefaultPromptTimeout 是探针单轮的上限；生产实现会换成「心跳/空闲超时」
// （设计文档 §10），而不是固定的整轮墙钟。
const DefaultPromptTimeout = 3 * time.Minute

// Prompt 发送一轮文本提示并等待 agent 停止。
//
// 取消语义：ctx 取消时 SDK 已自动向 agent 发 session/cancel
// （client_gen.go:291-298），这里不重复发送；但 Prompt 返回后是否已产生
// 终态由调用方判定（设计文档 §3.3）。
func (s *Session) Prompt(ctx context.Context, text string) (acpsdk.PromptResponse, error) {
	if strings.TrimSpace(text) == "" {
		return acpsdk.PromptResponse{}, fmt.Errorf("acp prompt text is required")
	}
	promptCtx, cancel := context.WithTimeout(ctx, DefaultPromptTimeout)
	defer cancel()

	resp, err := s.conn.sdk.Prompt(promptCtx, acpsdk.PromptRequest{
		SessionId: s.ID,
		Prompt:    []acpsdk.ContentBlock{acpsdk.TextBlock(text)},
	})
	if err != nil {
		return acpsdk.PromptResponse{}, fmt.Errorf("acp session/prompt: %w", err)
	}
	return resp, nil
}
