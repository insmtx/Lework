package native

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/insmtx/Leros/backend/internal/runtime/events"
	runtimetodo "github.com/insmtx/Leros/backend/internal/runtime/todo"
	pkgeino "github.com/insmtx/Leros/backend/pkg/eino"
	"github.com/insmtx/Leros/backend/tools"
	todotools "github.com/insmtx/Leros/backend/tools/todo"
)

type toolAdapter struct {
	registry *tools.Registry
}

type toolBinding struct {
	ToolContext  tools.ToolContext
	AllowedTools []string
	TodoReporter runtimetodo.Reporter
}

func newToolAdapter(registry *tools.Registry) *toolAdapter {
	return &toolAdapter{registry: registry}
}

func (a *toolAdapter) AvailableToolNames(names []string) []string {
	if a == nil || a.registry == nil || len(names) == 0 {
		return nil
	}
	result := make([]string, 0, len(names))
	seen := make(map[string]struct{}, len(names))
	for _, name := range names {
		name = strings.TrimSpace(name)
		if name == "" {
			continue
		}
		if _, exists := seen[name]; exists {
			continue
		}
		if _, err := a.registry.Get(name); err == nil {
			result = append(result, name)
			seen[name] = struct{}{}
		}
	}
	return result
}

func (a *toolAdapter) EinoTools(binding toolBinding, sink events.Sink) ([]pkgeino.ToolSpec, pkgeino.ToolInvoker, error) {
	if a == nil || a.registry == nil {
		return nil, nil, nil
	}

	boundTools, err := a.boundTools(binding.AllowedTools)
	if err != nil {
		return nil, nil, err
	}

	specs := make([]pkgeino.ToolSpec, 0, len(boundTools))
	for _, tool := range boundTools {
		specs = append(specs, toolSpecFor(tool))
	}

	return specs, &toolInvoker{
		tools:   indexTools(boundTools),
		binding: binding,
		sink:    sink,
	}, nil
}

func (a *toolAdapter) boundTools(allowedTools []string) ([]tools.Tool, error) {
	if len(allowedTools) == 0 {
		return a.registry.List(), nil
	}

	result := make([]tools.Tool, 0, len(allowedTools))
	seen := make(map[string]struct{}, len(allowedTools))
	for _, name := range allowedTools {
		name = strings.TrimSpace(name)
		if name == "" {
			continue
		}
		if _, exists := seen[name]; exists {
			continue
		}
		seen[name] = struct{}{}

		tool, err := a.registry.Get(name)
		if err != nil {
			return nil, err
		}
		result = append(result, tool)
	}
	return result, nil
}

type toolInvoker struct {
	tools   map[string]tools.Tool
	binding toolBinding
	sink    events.Sink
}

func (i *toolInvoker) InvokeTool(ctx context.Context, name string, argumentsInJSON string) (string, error) {
	if i == nil {
		return errorOutput("tool invoker is required", name), nil
	}

	tool := i.tools[name]
	if tool == nil {
		return errorOutput(fmt.Sprintf("tool %s not found", name), name), nil
	}

	input := make(map[string]interface{})
	if argumentsInJSON != "" {
		if err := json.Unmarshal([]byte(argumentsInJSON), &input); err != nil {
			return errorOutput(fmt.Sprintf("unmarshal tool arguments: %v", err), name), nil
		}
	}

	startedAt := time.Now()
	toolCallID := fmt.Sprintf("tool_%d", startedAt.UnixNano())
	suppressToolEvents := isTodoTool(tool)
	if !suppressToolEvents {
		_ = i.emitToolEvent(ctx, events.NewToolCallStarted(toolCallID, tool.Name(), cloneArguments(input)))
	}

	result, err := invokeTool(ctx, tool, input, i.binding.ToolContext, i.binding.TodoReporter)
	if err != nil {
		if !suppressToolEvents {
			_ = i.emitToolEvent(ctx, events.NewToolCallFailed(toolCallID, tool.Name(), err.Error(), time.Since(startedAt).Milliseconds()))
		}
		return errorOutput(err.Error(), tool.Name()), nil
	}

	if !suppressToolEvents {
		_ = i.emitToolEvent(ctx, events.NewToolCallCompleted(toolCallID, tool.Name(), result, time.Since(startedAt).Milliseconds()))
	}
	return result, nil
}

func (i *toolInvoker) emitToolEvent(ctx context.Context, event *events.Event) error {
	if i == nil || i.sink == nil {
		return nil
	}
	_ = i.sink.Emit(ctx, event)
	return nil
}

func invokeTool(ctx context.Context, tool tools.Tool, arguments map[string]interface{}, toolCtx tools.ToolContext, reporter runtimetodo.Reporter) (string, error) {
	if tool == nil {
		return "", fmt.Errorf("tool is required")
	}

	input := cloneToolInput(arguments)
	if validator, ok := tool.(tools.Validator); ok {
		if err := validator.Validate(input); err != nil {
			return "", fmt.Errorf("validate tool %s input: %w", tool.Name(), err)
		}
	}

	toolCtxValue := tools.ContextWithToolContext(ctx, toolCtx)
	toolCtxValue = runtimetodo.ContextWithReporter(toolCtxValue, reporter)
	output, err := tool.Execute(toolCtxValue, input)
	if err != nil {
		return "", err
	}
	return output, nil
}

func isTodoTool(tool tools.Tool) bool {
	return tool != nil && tool.Name() == todotools.ToolNameTodo
}

func errorOutput(detail, toolName string) string {
	errStr, _ := tools.JSONString(map[string]interface{}{
		"error":     true,
		"message":   "工作运行异常",
		"detail":    detail,
		"tool_name": toolName,
	})
	return errStr
}

func cloneArguments(input map[string]interface{}) map[string]any {
	if len(input) == 0 {
		return nil
	}
	cloned := make(map[string]any, len(input))
	for key, value := range input {
		cloned[key] = value
	}
	return cloned
}

func cloneToolInput(input map[string]interface{}) map[string]interface{} {
	if input == nil {
		return make(map[string]interface{})
	}
	cloned := make(map[string]interface{}, len(input))
	for key, value := range input {
		cloned[key] = value
	}
	return cloned
}

func indexTools(boundTools []tools.Tool) map[string]tools.Tool {
	result := make(map[string]tools.Tool, len(boundTools))
	for _, tool := range boundTools {
		if tool != nil {
			result[tool.Name()] = tool
		}
	}
	return result
}

func toolSpecFor(tool tools.Tool) pkgeino.ToolSpec {
	if tool == nil {
		return pkgeino.ToolSpec{}
	}
	return pkgeino.ToolSpec{
		Name:        tool.Name(),
		Description: tool.Description(),
		InputSchema: schemaFor(tool.InputSchema()),
	}
}

func schemaFor(schema tools.Schema) pkgeino.Schema {
	properties := make(map[string]*pkgeino.Property, len(schema.Properties))
	for name, property := range schema.Properties {
		properties[name] = propertyFor(property)
	}
	return pkgeino.Schema{
		Type:       schema.Type,
		Required:   schema.Required,
		Properties: properties,
	}
}

func propertyFor(property *tools.Property) *pkgeino.Property {
	if property == nil {
		return nil
	}
	return &pkgeino.Property{
		Type:        property.Type,
		Description: property.Description,
		Enum:        property.Enum,
		Items:       propertyFor(property.Items),
	}
}
