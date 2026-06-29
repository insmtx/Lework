package assistantcontext

import (
	"context"
	"strings"
	"testing"

	assistantdomain "github.com/insmtx/Leros/backend/internal/assistant/domain"
)

func TestContextBuilderBuildSystemPromptLayers(t *testing.T) {
	builder := NewContextBuilder(ContextBuilder{})
	prompt, err := builder.BuildSystemPrompt(context.Background(), &assistantdomain.RunRequest{
		Assistant: assistantdomain.AssistantContext{SystemPrompt: "Assistant-specific prompt."},
		Conversation: assistantdomain.ConversationContext{
			ID: "conv-123",
			Messages: []assistantdomain.InputMessage{
				{Role: "user", Content: "hello"},
			},
		},
		Model: assistantdomain.ModelOptions{
			Provider: "openai",
			Model:    "gpt-4",
		},
		Actor: assistantdomain.ActorContext{
			Channel: "wechat",
		},
	})
	if err != nil {
		t.Fatalf("build system prompt: %v", err)
	}

	// Layer 1: 角色定义 + Assistant 自定义 SystemPrompt
	for _, expected := range []string{
		"你的名称是 lework",
		"Assistant-specific prompt.",
	} {
		if !strings.Contains(prompt, expected) {
			t.Fatalf("expected prompt to contain %q", expected)
		}
	}

	// Layer 5: Memory 使用指导
	if !strings.Contains(prompt, "Memory 工具使用指导") {
		t.Fatal("expected prompt to contain Layer 5 memory guidance")
	}

	// 旧 Layer 6 已合并至 Layer 4（Skill 使用指导），不应再出现独立的 Skill 工具使用指导 section
	if strings.Contains(prompt, "Skill 工具使用指导") {
		t.Fatal("expected prompt NOT to contain standalone 'Skill 工具使用指导' section (merged into skill loading)")
	}

	// 验证合并后的内容已出现在 Skill 使用 section 中
	for _, expected := range []string{
		"没有维护的 skill 会变成负担",
		"不要等用户要求",
	} {
		if !strings.Contains(prompt, expected) {
			t.Fatalf("expected merged skill guidance to contain %q", expected)
		}
	}

	// Layer 8: 运行信息
	if !strings.Contains(prompt, "运行信息") {
		t.Fatal("expected prompt to contain Layer 9 run meta")
	}
	if !strings.Contains(prompt, "conv-123") {
		t.Fatal("expected prompt to contain session ID")
	}
	if !strings.Contains(prompt, "gpt-4") {
		t.Fatal("expected prompt to contain model name")
	}

	// Layer 9: 平台格式指导
	if !strings.Contains(prompt, "微信") {
		t.Fatal("expected prompt to contain Layer 10 platform guidance for wechat")
	}

	// 不应包含的旧 section
	for _, unexpected := range []string{
		"<session-summary>",
		"Self-learning rules",
		"Available skills:",
	} {
		if strings.Contains(prompt, unexpected) {
			t.Fatalf("expected prompt NOT to contain %q", unexpected)
		}
	}
}
