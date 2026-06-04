package eino

import (
	"context"
	"errors"
	"fmt"
	"io"
	"strings"

	"github.com/cloudwego/eino/adk"
	einomodel "github.com/cloudwego/eino/components/model"
	einotool "github.com/cloudwego/eino/components/tool"
	"github.com/cloudwego/eino/compose"
	einoschema "github.com/cloudwego/eino/schema"
	"github.com/ygpkg/yg-go/logs"
)

// Usage describes model token usage.
type Usage struct {
	InputTokens  int
	OutputTokens int
	TotalTokens  int
}

// StreamSink receives text deltas emitted during streaming.
type StreamSink interface {
	EmitMessageDelta(ctx context.Context, messageID string, content string) error
	EmitReasoningDelta(ctx context.Context, messageID string, content string) error
}

type noopStreamSink struct{}

func (noopStreamSink) EmitMessageDelta(context.Context, string, string) error {
	return nil
}

func (noopStreamSink) EmitReasoningDelta(context.Context, string, string) error {
	return nil
}

// Flow runs a tool-calling Eino agent loop.
type Flow struct {
	agent        adk.Agent
	runner       *adk.Runner
	streamRunner *adk.Runner
	messages     []adk.Message
}

// FlowConfig contains Eino Flow dependencies and execution options.
type FlowConfig struct {
	Model        einomodel.ToolCallingChatModel
	Tools        []einotool.BaseTool
	SystemPrompt string
	MaxStep      int
	Messages     []adk.Message
}

// NewFlow creates a reusable Eino agent flow.
func NewFlow(ctx context.Context, cfg *FlowConfig) (*Flow, error) {
	if cfg == nil {
		return nil, fmt.Errorf("flow config is required")
	}
	if cfg.Model == nil {
		return nil, fmt.Errorf("tool-calling model is required")
	}

	maxStep := cfg.MaxStep
	if maxStep <= 0 {
		maxStep = 90
	}

	agent, err := adk.NewChatModelAgent(ctx, &adk.ChatModelAgentConfig{
		Name:        "EinoAgent",
		Description: "Eino runtime agent",
		Model:       cfg.Model,
		Instruction: cfg.SystemPrompt,
		ToolsConfig: adk.ToolsConfig{
			ToolsNodeConfig: compose.ToolsNodeConfig{
				Tools: cfg.Tools,
				UnknownToolsHandler: func(ctx context.Context, toolName, toolInput string) (string, error) {
					logs.WarnContextf(ctx, "[WARN] unknown tool call: %s with input: %s", toolName, toolInput)
					return fmt.Sprintf(`Tool "%s" does not exist. Please use a valid tool and retry.`, toolName), nil
				},
			},
		},
		MaxIterations: maxStep,
		ModelRetryConfig: &adk.ModelRetryConfig{
			MaxRetries: 5,
			IsRetryAble: func(_ context.Context, err error) bool {
				return strings.Contains(err.Error(), "429") ||
					strings.Contains(err.Error(), "Too Many Requests") ||
					strings.Contains(err.Error(), "qpm limit")
			},
		},
	})
	if err != nil {
		return nil, fmt.Errorf("create eino agent: %w", err)
	}

	runner := adk.NewRunner(ctx, adk.RunnerConfig{Agent: agent})
	streamRunner := adk.NewRunner(ctx, adk.RunnerConfig{Agent: agent, EnableStreaming: true})

	return &Flow{
		agent:        agent,
		runner:       runner,
		streamRunner: streamRunner,
		messages:     cfg.Messages,
	}, nil
}

// Generate runs a non-streaming agent flow.
func (f *Flow) Generate(ctx context.Context, userInput string) (*einoschema.Message, error) {
	message, _, err := f.GenerateWithUsage(ctx, userInput)
	return message, err
}

// GenerateWithUsage returns the final message and aggregated model token usage.
func (f *Flow) GenerateWithUsage(ctx context.Context, userInput string) (*einoschema.Message, *Usage, error) {
	if f == nil || f.runner == nil {
		return nil, nil, fmt.Errorf("flow is not initialized")
	}
	if strings.TrimSpace(userInput) == "" {
		return nil, nil, fmt.Errorf("user input is required")
	}

	messages := AppendUserMessage(f.messages, userInput)
	iter := f.runner.Run(ctx, messages)

	var result *einoschema.Message
	usage := &usageAccumulator{}
	for {
		event, ok := iter.Next()
		if !ok {
			break
		}
		if event.Err != nil {
			return nil, nil, event.Err
		}
		if event.Output != nil && event.Output.MessageOutput != nil {
			msg, err := event.Output.MessageOutput.GetMessage()
			if err != nil {
				return nil, nil, err
			}
			if msg != nil {
				result = msg
				usage.AddMessage(msg)
			}
		}
		if result != nil {
			logs.DebugContextf(ctx, "received message chunk: content_len=%d reasoning_len=%d tool_calls=%d", len(result.Content), len(result.ReasoningContent), len(result.ToolCalls))
		}
	}

	if result == nil {
		return nil, nil, fmt.Errorf("agent returned no message")
	}
	return result, usage.Payload(), nil
}

// Stream runs a streaming agent flow.
func (f *Flow) Stream(ctx context.Context, userInput string, sink StreamSink) (*einoschema.Message, error) {
	message, _, err := f.StreamWithUsage(ctx, userInput, sink)
	return message, err
}

// StreamWithUsage emits deltas and returns the final message plus aggregated usage.
func (f *Flow) StreamWithUsage(ctx context.Context, userInput string, sink StreamSink) (*einoschema.Message, *Usage, error) {
	if f == nil || f.streamRunner == nil {
		return nil, nil, fmt.Errorf("flow is not initialized")
	}
	if strings.TrimSpace(userInput) == "" {
		return nil, nil, fmt.Errorf("user input is required")
	}
	if sink == nil {
		sink = noopStreamSink{}
	}

	messages := AppendUserMessage(f.messages, userInput)
	iter := f.streamRunner.Run(ctx, messages)

	var lastMsg *einoschema.Message
	var currentMessageID string
	messageIDs := newMessageIDMapper()
	usage := &usageAccumulator{}

	for {
		event, ok := iter.Next()
		if !ok {
			break
		}
		if event.Err != nil {
			return nil, nil, event.Err
		}
		if event.Output != nil && event.Output.MessageOutput != nil {
			mv := event.Output.MessageOutput
			if mv.Role == einoschema.Tool {
				continue
			}

			currentMessageID = messageIDs.StartNew()
			if mv.IsStreaming && mv.MessageStream != nil {
				streams := mv.MessageStream.Copy(2)
				emitStream := streams[0]
				concatStream := streams[1]
				emitStream.SetAutomaticClose()
				for {
					chunk, err := emitStream.Recv()
					if errors.Is(err, io.EOF) {
						break
					}
					if err != nil {
						return nil, nil, fmt.Errorf("read stream chunk: %w", err)
					}
					if chunk.Content != "" {
						_ = sink.EmitMessageDelta(ctx, currentMessageID, chunk.Content)
					}
					if chunk.ReasoningContent != "" {
						_ = sink.EmitReasoningDelta(ctx, currentMessageID, chunk.ReasoningContent)
					}
				}
				lastMsg, _ = einoschema.ConcatMessageStream(concatStream)
			} else {
				msg, err := mv.GetMessage()
				if err != nil {
					return nil, nil, err
				}
				if msg != nil {
					lastMsg = msg
					_ = sink.EmitMessageDelta(ctx, currentMessageID, msg.Content)
				}
			}

			if lastMsg != nil {
				usage.AddMessage(lastMsg)
				logs.InfoContextf(ctx, "agent msg:msgID=%s, content=%s, reasoning=%s",
					currentMessageID, lastMsg.Content, lastMsg.ReasoningContent)
			}
		}
	}

	if lastMsg == nil {
		return nil, nil, fmt.Errorf("agent stream returned no messages")
	}

	lastMsg.Extra = map[string]any{"message_id": currentMessageID}
	return lastMsg, usage.Payload(), nil
}

type usageAccumulator struct {
	inputTokens  int
	outputTokens int
	totalTokens  int
}

func (u *usageAccumulator) AddMessage(message *einoschema.Message) {
	if message == nil {
		return
	}
	u.AddResponseMeta(message.ResponseMeta)
}

func (u *usageAccumulator) AddResponseMeta(meta *einoschema.ResponseMeta) {
	if u == nil || meta == nil || meta.Usage == nil {
		return
	}
	u.inputTokens += meta.Usage.PromptTokens
	u.outputTokens += meta.Usage.CompletionTokens
	u.totalTokens += meta.Usage.TotalTokens
}

func (u *usageAccumulator) Payload() *Usage {
	if u == nil || (u.inputTokens == 0 && u.outputTokens == 0 && u.totalTokens == 0) {
		return nil
	}
	totalTokens := u.totalTokens
	if totalTokens == 0 {
		totalTokens = u.inputTokens + u.outputTokens
	}
	return &Usage{
		InputTokens:  u.inputTokens,
		OutputTokens: u.outputTokens,
		TotalTokens:  totalTokens,
	}
}
