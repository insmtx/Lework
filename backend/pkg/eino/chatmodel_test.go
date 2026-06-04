package eino

import (
	"context"
	"strings"
	"testing"
)

func TestNewChatModelRequiresModel(t *testing.T) {
	t.Parallel()

	_, err := NewChatModel(context.Background(), &ChatModelConfig{
		Provider: ProviderOpenAI,
		APIKey:   "sk-test",
	})
	if err == nil || !strings.Contains(err.Error(), "llm model is required") {
		t.Fatalf("expected model required error, got %v", err)
	}
}

func TestNewChatModelRejectsUnknownProvider(t *testing.T) {
	t.Parallel()

	_, err := NewChatModel(context.Background(), &ChatModelConfig{
		Provider: "unknown",
		APIKey:   "sk-test",
		Model:    "test-model",
	})
	if err == nil || !strings.Contains(err.Error(), "not supported") {
		t.Fatalf("expected unsupported provider error, got %v", err)
	}
}
