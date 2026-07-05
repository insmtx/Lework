package agentruncontext

import (
	"context"
	"strings"
	"testing"

	agentrundomain "github.com/insmtx/Leros/backend/internal/worker/agentrun/domain"
)

func TestContextBuilderBuildSystemPromptLayers(t *testing.T) {
	builder := NewContextBuilder(ContextBuilder{})
	prompt, err := builder.BuildSystemPrompt(context.Background(), &agentrundomain.RunRequest{
		Assistant: agentrundomain.AssistantContext{
			Name:         "合同审查专家",
			SystemPrompt: "Assistant-specific prompt.",
		},
		Conversation: agentrundomain.ConversationContext{
			ID: "conv-123",
			Messages: []agentrundomain.InputMessage{
				{Role: "user", Content: "hello"},
			},
		},
		Model: agentrundomain.ModelOptions{
			Provider: "openai",
			Model:    "gpt-4",
		},
		Actor: agentrundomain.ActorContext{
			Channel: "wechat",
		},
	})
	if err != nil {
		t.Fatalf("build system prompt: %v", err)
	}

	for _, expected := range []string{
		"当前对用户展示和执行任务的第一身份是被召唤的 AI 队友",
		"队友名称：合同审查专家",
		"Assistant-specific prompt.",
	} {
		if !strings.Contains(prompt, expected) {
			t.Fatalf("expected prompt to contain %q", expected)
		}
	}
	if strings.Contains(prompt, "我是 lework，你工作和生活中的 AI 队友") {
		t.Fatal("expected teammate prompt not to contain default lework self-introduction")
	}

	if !strings.Contains(prompt, "Memory 工具使用指导") {
		t.Fatal("expected prompt to contain Layer 5 memory guidance")
	}

	if strings.Contains(prompt, "Skill 工具使用指导") {
		t.Fatal("expected prompt NOT to contain standalone 'Skill 工具使用指导' section (merged into skill loading)")
	}

	for _, expected := range []string{
		"没有维护的 skill 会变成负担",
		"不要等用户要求",
	} {
		if !strings.Contains(prompt, expected) {
			t.Fatalf("expected merged skill guidance to contain %q", expected)
		}
	}

	if !strings.Contains(prompt, "运行信息") {
		t.Fatal("expected prompt to contain Layer 9 run meta")
	}
	if !strings.Contains(prompt, "conv-123") {
		t.Fatal("expected prompt to contain session ID")
	}
	if !strings.Contains(prompt, "gpt-4") {
		t.Fatal("expected prompt to contain model name")
	}

	if !strings.Contains(prompt, "微信") {
		t.Fatal("expected prompt to contain Layer 10 platform guidance for wechat")
	}

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
