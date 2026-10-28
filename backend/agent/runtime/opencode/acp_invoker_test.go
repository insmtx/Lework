package opencode

import (
	"context"
	"errors"
	"strings"
	"testing"

	acpsdk "github.com/coder/acp-go-sdk"
)

func TestInvocationError(t *testing.T) {
	promptErr := errors.New("prompt failed")
	tests := []struct {
		name       string
		ctxErr     error
		promptErr  error
		stopReason acpsdk.StopReason
		finalText  string
		want       string
	}{
		{"success", nil, nil, acpsdk.StopReasonEndTurn, "hello", ""},
		{"prompt error", nil, promptErr, acpsdk.StopReasonEndTurn, "hello", "prompt failed"},
		{"ctx canceled", context.Canceled, nil, acpsdk.StopReasonEndTurn, "hello", context.Canceled.Error()},
		{"refusal", nil, nil, acpsdk.StopReasonRefusal, "", "拒绝回答"},
		{"empty text", nil, nil, acpsdk.StopReasonEndTurn, "   ", fallbackReply},
		{"echo", nil, nil, acpsdk.StopReasonEndTurn, "【用户问题】重复", fallbackReply},
		{"max tokens is success", nil, nil, acpsdk.StopReasonMaxTokens, "partial", ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := invocationError(tt.ctxErr, tt.promptErr, tt.stopReason, tt.finalText)
			if tt.want == "" {
				if err != nil {
					t.Fatalf("unexpected error: %v", err)
				}
				return
			}
			if err == nil || !strings.Contains(err.Error(), tt.want) {
				t.Fatalf("error = %v, want contains %q", err, tt.want)
			}
		})
	}
}

func TestUsageFromACP(t *testing.T) {
	cachedRead := 9
	usage := usageFromACP(&acpsdk.Usage{
		InputTokens:      100,
		OutputTokens:     20,
		CachedReadTokens: &cachedRead,
	})
	if usage.InputTokens != 100 || usage.OutputTokens != 20 || usage.TotalTokens != 120 {
		t.Fatalf("usage = %+v", usage)
	}
	if usage.CacheInputTokens != 9 {
		t.Fatalf("cache input = %d, want 9", usage.CacheInputTokens)
	}

	if zero := usageFromACP(nil); zero == nil || zero.TotalTokens != 0 {
		t.Fatalf("nil usage fallback = %+v", zero)
	}
}
