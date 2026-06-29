package native

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/insmtx/Leros/backend/agent"
	"github.com/insmtx/Leros/backend/agent/runtime/events"
	runtimetodo "github.com/insmtx/Leros/backend/agent/runtime/todo"
	pkgeino "github.com/insmtx/Leros/backend/pkg/eino"
)

type toolBinding struct {
	Tools        []agent.Tool
	AllowedTools []string
	TodoReporter runtimetodo.Reporter
}

func buildRuntimeTools(binding toolBinding, sink activitySink) ([]pkgeino.ToolSpec, pkgeino.ToolInvoker, error) {
	boundTools, err := filterRuntimeTools(binding.Tools, binding.AllowedTools)
	if err != nil {
		return nil, nil, err
	}

	specs := make([]pkgeino.ToolSpec, 0, len(boundTools))
	for _, tool := range boundTools {
		spec, err := toolSpecFor(tool)
		if err != nil {
			return nil, nil, err
		}
		specs = append(specs, spec)
	}

	return specs, &toolInvoker{
		tools:   indexTools(boundTools),
		binding: binding,
		sink:    sink,
	}, nil
}

func filterRuntimeTools(runtimeTools []agent.Tool, allowedTools []string) ([]agent.Tool, error) {
	available := indexTools(runtimeTools)
	if len(allowedTools) == 0 {
		result := make([]agent.Tool, 0, len(runtimeTools))
		seen := make(map[string]struct{}, len(runtimeTools))
		for _, tool := range runtimeTools {
			if tool == nil {
				continue
			}
			name := strings.TrimSpace(tool.Definition().Name)
			if name == "" {
				return nil, fmt.Errorf("runtime tool name is required")
			}
			if _, exists := seen[name]; exists {
				continue
			}
			seen[name] = struct{}{}
			result = append(result, tool)
		}
		return result, nil
	}

	result := make([]agent.Tool, 0, len(allowedTools))
	seen := make(map[string]struct{}, len(allowedTools))
	for _, name := range allowedTools {
		name = strings.TrimSpace(name)
		if name == "" {
			continue
		}
		if _, exists := seen[name]; exists {
			continue
		}
		tool := available[name]
		if tool == nil {
			return nil, fmt.Errorf("runtime tool %s not found", name)
		}
		seen[name] = struct{}{}
		result = append(result, tool)
	}
	return result, nil
}

type toolInvoker struct {
	tools   map[string]agent.Tool
	binding toolBinding
	sink    activitySink
}

func (i *toolInvoker) InvokeTool(ctx context.Context, name string, argumentsInJSON string) (string, error) {
	if i == nil {
		return errorOutput("tool invoker is required", name), nil
	}

	tool := i.tools[name]
	if tool == nil {
		return errorOutput(fmt.Sprintf("tool %s not found", name), name), nil
	}

	arguments := json.RawMessage(argumentsInJSON)
	if len(arguments) == 0 {
		arguments = json.RawMessage(`{}`)
	}
	if !json.Valid(arguments) {
		return errorOutput("invalid tool arguments JSON", name), nil
	}

	startedAt := time.Now()
	toolCallID := fmt.Sprintf("tool_%d", startedAt.UnixNano())
	suppressToolEvents := name == runtimetodo.ToolName
	if !suppressToolEvents {
		if err := i.emitToolEvent(ctx, events.NewToolCallStarted(toolCallID, name, arguments)); err != nil {
			return "", err
		}
	}

	toolCtx := runtimetodo.ContextWithReporter(ctx, i.binding.TodoReporter)
	result, err := tool.Execute(toolCtx, arguments)
	if err != nil {
		if !suppressToolEvents {
			if emitErr := i.emitToolEvent(ctx, events.NewToolCallFailed(
				toolCallID,
				name,
				err.Error(),
				time.Since(startedAt).Milliseconds(),
			)); emitErr != nil {
				return "", emitErr
			}
		}
		return errorOutput(err.Error(), name), nil
	}
	if result.IsError {
		detail := strings.TrimSpace(result.Error)
		if detail == "" {
			detail = strings.TrimSpace(result.Content)
		}
		if !suppressToolEvents {
			if emitErr := i.emitToolEvent(ctx, events.NewToolCallFailed(
				toolCallID,
				name,
				detail,
				time.Since(startedAt).Milliseconds(),
			)); emitErr != nil {
				return "", emitErr
			}
		}
		return errorOutput(detail, name), nil
	}

	if !suppressToolEvents {
		if err := i.emitToolEvent(ctx, events.NewToolCallCompleted(
			toolCallID,
			name,
			rawResult(result.Content),
			time.Since(startedAt).Milliseconds(),
		)); err != nil {
			return "", err
		}
	}
	return result.Content, nil
}

func (i *toolInvoker) emitToolEvent(ctx context.Context, event *agent.Event) error {
	if i == nil || i.sink == nil {
		return nil
	}
	return i.sink.Emit(ctx, event)
}

type toolErrorOutput struct {
	Error    bool   `json:"error"`
	Message  string `json:"message"`
	Detail   string `json:"detail"`
	ToolName string `json:"tool_name"`
}

func errorOutput(detail, toolName string) string {
	encoded, err := json.Marshal(toolErrorOutput{
		Error:    true,
		Message:  "工作运行异常",
		Detail:   detail,
		ToolName: toolName,
	})
	if err != nil {
		return `{"error":true,"message":"工作运行异常"}`
	}
	return string(encoded)
}

func rawResult(content string) json.RawMessage {
	content = strings.TrimSpace(content)
	if content == "" {
		return nil
	}
	raw := json.RawMessage(content)
	if json.Valid(raw) {
		return append(json.RawMessage(nil), raw...)
	}
	encoded, err := json.Marshal(content)
	if err != nil {
		return nil
	}
	return encoded
}

func indexTools(runtimeTools []agent.Tool) map[string]agent.Tool {
	result := make(map[string]agent.Tool, len(runtimeTools))
	for _, tool := range runtimeTools {
		if tool == nil {
			continue
		}
		name := strings.TrimSpace(tool.Definition().Name)
		if name != "" {
			result[name] = tool
		}
	}
	return result
}

func toolSpecFor(tool agent.Tool) (pkgeino.ToolSpec, error) {
	if tool == nil {
		return pkgeino.ToolSpec{}, fmt.Errorf("runtime tool is required")
	}
	definition := tool.Definition()
	if strings.TrimSpace(definition.Name) == "" {
		return pkgeino.ToolSpec{}, fmt.Errorf("runtime tool name is required")
	}

	schema := pkgeino.Schema{Type: "object"}
	if len(definition.Parameters) > 0 {
		if err := json.Unmarshal(definition.Parameters, &schema); err != nil {
			return pkgeino.ToolSpec{}, fmt.Errorf("decode tool %s schema: %w", definition.Name, err)
		}
	}
	return pkgeino.ToolSpec{
		Name:        definition.Name,
		Description: definition.Description,
		InputSchema: schema,
	}, nil
}
