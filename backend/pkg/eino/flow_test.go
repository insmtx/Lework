package eino

import (
	"context"
	"fmt"
	"strings"
	"testing"

	einomodel "github.com/cloudwego/eino/components/model"
	einotool "github.com/cloudwego/eino/components/tool"
	einoschema "github.com/cloudwego/eino/schema"
)

func TestFlowGenerate(t *testing.T) {
	model := &fakeToolCallingModel{}
	flow, err := NewFlow(context.Background(), &FlowConfig{
		Model:        model,
		Tools:        []einotool.BaseTool{NewTool(ToolSpec{Name: "test.account.get_current_user"}, staticInvoker("current_user"))},
		SystemPrompt: "You are Eino.",
	})
	if err != nil {
		t.Fatalf("new flow: %v", err)
	}

	message, usage, err := flow.GenerateWithUsage(context.Background(), "who am I?")
	if err != nil {
		t.Fatalf("generate response: %v", err)
	}
	if message == nil {
		t.Fatalf("expected non-nil message")
	}
	if !strings.Contains(message.Content, "final answer") {
		t.Fatalf("unexpected final content: %s", message.Content)
	}
	if usage == nil || usage.InputTokens != 30 || usage.OutputTokens != 3 || usage.TotalTokens != 33 {
		t.Fatalf("expected aggregated usage from all model messages, got %#v", usage)
	}
}

func TestFlowStreamEmitsMessageEvents(t *testing.T) {
	flow, err := NewFlow(context.Background(), &FlowConfig{
		Model: &streamingTextModel{},
	})
	if err != nil {
		t.Fatalf("new flow: %v", err)
	}

	sink := &recordingStreamSink{}
	message, err := flow.Stream(context.Background(), "say hello", sink)
	if err != nil {
		t.Fatalf("stream response: %v", err)
	}
	if message == nil || strings.TrimSpace(message.Content) != "hello world" {
		t.Fatalf("unexpected streamed message: %+v", message)
	}
	if len(sink.messageIDs) == 0 || sink.content != "hello world" {
		t.Fatalf("expected message deltas, got %#v", sink)
	}
	for _, messageID := range sink.messageIDs {
		if messageID == "" {
			t.Fatalf("expected message id")
		}
	}
}

type fakeToolCallingModel struct {
	state      *fakeToolCallingModelState
	boundTools []*einoschema.ToolInfo
}

var _ einomodel.ToolCallingChatModel = (*fakeToolCallingModel)(nil)

type fakeToolCallingModelState struct {
	calls [][]*einoschema.Message
}

func (m *fakeToolCallingModel) Generate(ctx context.Context, input []*einoschema.Message, opts ...einomodel.Option) (*einoschema.Message, error) {
	if m.state == nil {
		m.state = &fakeToolCallingModelState{}
	}
	copied := make([]*einoschema.Message, len(input))
	copy(copied, input)
	m.state.calls = append(m.state.calls, copied)

	last := input[len(input)-1]
	if last.Role == einoschema.Tool {
		return messageWithUsage(einoschema.AssistantMessage(fmt.Sprintf("final answer: %s", last.Content), nil), 20, 2), nil
	}

	toolName := "test.account.get_current_user"
	if len(m.boundTools) > 0 && m.boundTools[0] != nil && m.boundTools[0].Name != "" {
		toolName = m.boundTools[0].Name
	}

	return messageWithUsage(einoschema.AssistantMessage("", []einoschema.ToolCall{
		{
			ID:   "call_1",
			Type: "function",
			Function: einoschema.FunctionCall{
				Name:      toolName,
				Arguments: `{}`,
			},
		},
	}), 10, 1), nil
}

func (m *fakeToolCallingModel) Stream(ctx context.Context, input []*einoschema.Message, opts ...einomodel.Option) (*einoschema.StreamReader[*einoschema.Message], error) {
	msg, err := m.Generate(ctx, input, opts...)
	if err != nil {
		return nil, err
	}
	return einoschema.StreamReaderFromArray([]*einoschema.Message{msg}), nil
}

func (m *fakeToolCallingModel) WithTools(tools []*einoschema.ToolInfo) (einomodel.ToolCallingChatModel, error) {
	state := m.state
	if state == nil {
		state = &fakeToolCallingModelState{}
		m.state = state
	}
	return &fakeToolCallingModel{
		state:      state,
		boundTools: tools,
	}, nil
}

type streamingTextModel struct{}

var _ einomodel.ToolCallingChatModel = (*streamingTextModel)(nil)

func (m *streamingTextModel) Generate(ctx context.Context, input []*einoschema.Message, opts ...einomodel.Option) (*einoschema.Message, error) {
	return einoschema.AssistantMessage("hello world", nil), nil
}

func (m *streamingTextModel) Stream(ctx context.Context, input []*einoschema.Message, opts ...einomodel.Option) (*einoschema.StreamReader[*einoschema.Message], error) {
	return einoschema.StreamReaderFromArray([]*einoschema.Message{
		einoschema.AssistantMessage("hello ", nil),
		einoschema.AssistantMessage("world", nil),
	}), nil
}

func (m *streamingTextModel) WithTools(tools []*einoschema.ToolInfo) (einomodel.ToolCallingChatModel, error) {
	return m, nil
}

func messageWithUsage(message *einoschema.Message, promptTokens int, completionTokens int) *einoschema.Message {
	if message == nil {
		return nil
	}
	message.ResponseMeta = &einoschema.ResponseMeta{
		Usage: &einoschema.TokenUsage{
			PromptTokens:     promptTokens,
			CompletionTokens: completionTokens,
			TotalTokens:      promptTokens + completionTokens,
		},
	}
	return message
}

type recordingStreamSink struct {
	messageIDs []string
	content    string
	reasoning  string
}

func (s *recordingStreamSink) EmitMessageDelta(ctx context.Context, messageID string, content string) error {
	s.messageIDs = append(s.messageIDs, messageID)
	s.content += content
	return nil
}

func (s *recordingStreamSink) EmitReasoningDelta(ctx context.Context, messageID string, content string) error {
	s.reasoning += content
	return nil
}

type staticInvoker string

func (i staticInvoker) InvokeTool(ctx context.Context, name string, argumentsInJSON string) (string, error) {
	return string(i), nil
}
