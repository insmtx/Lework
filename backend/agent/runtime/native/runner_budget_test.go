package native

import (
	"strings"
	"testing"

	"github.com/insmtx/Leros/backend/agent"
)

func TestEstimateTokenCount(t *testing.T) {
	if got := estimateTokenCount(""); got != 0 {
		t.Fatalf("empty string tokens = %d, want 0", got)
	}
	if got := estimateTokenCount("abc"); got != 1 {
		t.Fatalf("short string tokens = %d, want 1", got)
	}
	// 8 字符 → 8/4+1 = 3
	if got := estimateTokenCount("abcdefgh"); got != 3 {
		t.Fatalf("8-char string tokens = %d, want 3", got)
	}
	// 中文按字符计（保守）；"你好世界" 4 字 → 4/4+1 = 2
	if got := estimateTokenCount("你好世界"); got != 2 {
		t.Fatalf("chinese string tokens = %d, want 2", got)
	}
}

func TestTruncateByChars(t *testing.T) {
	if got := truncateByChars("hello world", 0); got != "" {
		t.Fatalf("maxChars 0 = %q, want empty", got)
	}
	if got := truncateByChars("hello", 10); got != "hello" {
		t.Fatalf("short string = %q, want unchanged", got)
	}
	if got := truncateByChars("hello world", 5); got != "hello" {
		t.Fatalf("truncated = %q, want \"hello\"", got)
	}
	// 中文截断保证不截断半个字：按 rune 截断
	if got := truncateByChars("你好世界", 2); got != "你好" {
		t.Fatalf("rune truncation = %q, want \"你好\"", got)
	}
}

func TestApplyInputBudgetUnsetContextKeepsMessages(t *testing.T) {
	req := &agent.ExecutionRequest{
		Prompt:   "p",
		Messages: []agent.Message{{Role: "user", Content: "m"}},
		Model:    agent.ModelConfig{ContextLimit: 0},
	}
	before := len(req.Messages)
	applyInputBudget(req)
	if len(req.Messages) != before {
		t.Fatalf("unset context should not truncate messages, got %d", len(req.Messages))
	}
}

func TestApplyInputBudgetTruncatesOldestMessages(t *testing.T) {
	// 构造每条 100 字符（≈26 token）的历史消息，一条 10 字符 prompt（≈3 token）。
	long := strings.Repeat("a", 100)
	req := &agent.ExecutionRequest{
		Prompt: "short",
		Messages: []agent.Message{
			{Role: "user", Content: long},
			{Role: "assistant", Content: long},
			{Role: "user", Content: long},
		},
		// context=100 token，预算=70；每条历史≈26 token。
		Model: agent.ModelConfig{ContextLimit: 100},
	}
	applyInputBudget(req)

	if len(req.Messages) > 2 {
		t.Fatalf("expected at most 2 messages kept, got %d", len(req.Messages))
	}
	for _, m := range req.Messages {
		if len(m.Content) > 100 {
			t.Fatalf("message content should not exceed original length")
		}
	}
	// 最近的消息必须被保留。
	last := req.Messages[len(req.Messages)-1].Content
	if last != long {
		t.Fatalf("most recent message should be kept intact")
	}
}

func TestApplyInputBudgetTruncatesSingleHugeMessage(t *testing.T) {
	req := &agent.ExecutionRequest{
		Prompt:   "hi",
		Messages: []agent.Message{{Role: "user", Content: strings.Repeat("x", 10000)}},
		Model:    agent.ModelConfig{ContextLimit: 32},
	}
	applyInputBudget(req)
	if len(req.Messages) != 1 {
		t.Fatalf("expected 1 message kept, got %d", len(req.Messages))
	}
	if len(req.Messages[0].Content) >= 10000 {
		t.Fatalf("single huge message should be truncated, len=%d", len(req.Messages[0].Content))
	}
}

func TestApplyInputBudgetHugePromptDropsHistory(t *testing.T) {
	req := &agent.ExecutionRequest{
		Prompt:   strings.Repeat("p", 5000),
		Messages: []agent.Message{{Role: "user", Content: "keep or drop"}},
		Model:    agent.ModelConfig{ContextLimit: 64},
	}
	applyInputBudget(req)
	// prompt 本身已超预算，应将历史清空并截断 prompt。
	if len(req.Messages) != 0 {
		t.Fatalf("expected history dropped when prompt exceeds budget, got %d", len(req.Messages))
	}
	if len(req.Prompt) >= 5000 {
		t.Fatalf("prompt should be truncated, len=%d", len(req.Prompt))
	}
}
