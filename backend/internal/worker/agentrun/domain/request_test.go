package domain

import (
	"strings"
	"testing"
)

func TestBuildUserInputPrefersSenderName(t *testing.T) {
	req := &RunRequest{Input: InputContext{Type: InputTypeMessage, Messages: []InputMessage{
		{Role: "user", Content: "帮我写一个 HTTP server", SenderName: "A"},
		{Role: "assistant", Content: "好的，以下是代码...", SenderName: "AI队友Alpha"},
		{Role: "user", Content: "加上 /health 端点", SenderName: "B"},
	}}}
	got := BuildUserInput(req)
	want := "A: 帮我写一个 HTTP server\nAI队友Alpha: 好的，以下是代码...\nB: 加上 /health 端点"
	if got != want {
		t.Fatalf("BuildUserInput = %q, want %q", got, want)
	}
}

func TestBuildUserInputFallsBackToRole(t *testing.T) {
	req := &RunRequest{Input: InputContext{Type: InputTypeMessage, Messages: []InputMessage{
		{Role: "user", Content: "hello"},
		{Content: "no role"},
	}}}
	got := BuildUserInput(req)
	if !strings.Contains(got, "user: hello") || !strings.Contains(got, "user: no role") {
		t.Fatalf("expected role fallback in %q", got)
	}
}
