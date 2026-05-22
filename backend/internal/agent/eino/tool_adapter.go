package eino

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	einotool "github.com/cloudwego/eino/components/tool"
	einoschema "github.com/cloudwego/eino/schema"
	"github.com/insmtx/Leros/backend/internal/agent/runtime/events"
	runtimetodo "github.com/insmtx/Leros/backend/internal/agent/runtime/todo"
	"github.com/insmtx/Leros/backend/tools"
)

// ToolDefinition is the local bridge shape exported to an Eino integration layer.
//
// It intentionally mirrors only the fields we need from Leros tools so the
// actual cloudwego/eino binding can be added later without changing registry
// or runtime packages again.
type ToolDefinition struct {
	Name        string
	Description string
	InputSchema tools.Schema
}

// ToolCallRequest describes one model-initiated tool call.
type ToolCallRequest struct {
	Name        string
	Arguments   map[string]interface{}
	ToolContext tools.ToolContext
}

// ToolCallResult contains the execution result returned back to the model loop.
type ToolCallResult struct {
	Name   string
	Output string
}

// ToolAdapter bridges Leros tool registry to an Eino-facing API.
type ToolAdapter struct {
	registry *tools.Registry
}

// ToolBinding carries runtime-bound identity for one Eino agent execution.
type ToolBinding struct {
	ToolContext  tools.ToolContext
	AllowedTools []string
	TodoReporter runtimetodo.Reporter
}

// NewToolAdapter creates a new adapter over the shared tool registry.
func NewToolAdapter(registry *tools.Registry) *ToolAdapter {
	return &ToolAdapter{
		registry: registry,
	}
}

// Definitions returns the registry tools in an Eino-friendly description shape.
func (a *ToolAdapter) Definitions() []ToolDefinition {
	if a == nil || a.registry == nil {
		return nil
	}

	registeredTools := a.registry.List()
	definitions := make([]ToolDefinition, 0, len(registeredTools))
	for _, tool := range registeredTools {
		definitions = append(definitions, ToolDefinition{
			Name:        tool.Name(),
			Description: tool.Description(),
			InputSchema: tool.InputSchema(),
		})
	}

	return definitions
}

// AvailableToolNames returns registered tool names from the requested list.
func (a *ToolAdapter) AvailableToolNames(names []string) []string {
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

// EinoTools returns Eino wrappers that inject runtime identity at call time.
func (a *ToolAdapter) EinoTools(binding ToolBinding, sink events.Sink) ([]einotool.BaseTool, error) {
	if a == nil || a.registry == nil {
		return nil, nil
	}

	boundTools, err := a.boundTools(binding.AllowedTools)
	if err != nil {
		return nil, err
	}

	result := make([]einotool.BaseTool, 0, len(boundTools))
	for _, tool := range boundTools {
		result = append(result, &invokableTool{
			adapter: a,
			tool:    tool,
			binding: binding,
			sink:    sink,
		})
	}

	return result, nil
}

func (a *ToolAdapter) boundTools(allowedTools []string) ([]tools.Tool, error) {
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

// Invoke executes a tool call through the registry-backed adapter.
func (a *ToolAdapter) Invoke(ctx context.Context, req *ToolCallRequest) (*ToolCallResult, error) {
	if req == nil {
		return nil, fmt.Errorf("tool call request is required")
	}
	if req.Name == "" {
		return nil, fmt.Errorf("tool name is required")
	}
	if a == nil || a.registry == nil {
		return nil, fmt.Errorf("tool registry is required")
	}

	tool, err := a.registry.Get(req.Name)
	if err != nil {
		return nil, err
	}

	return invokeTool(ctx, tool, req.Arguments, req.ToolContext, nil)
}

func invokeTool(ctx context.Context, tool tools.Tool, arguments map[string]interface{}, toolCtx tools.ToolContext, reporter runtimetodo.Reporter) (*ToolCallResult, error) {
	if tool == nil {
		return nil, fmt.Errorf("tool is required")
	}

	input := cloneToolInput(arguments)
	if validator, ok := tool.(tools.Validator); ok {
		if err := validator.Validate(input); err != nil {
			return nil, fmt.Errorf("validate tool %s input: %w", tool.Name(), err)
		}
	}

	toolCtxValue := tools.ContextWithToolContext(ctx, toolCtx)
	toolCtxValue = runtimetodo.ContextWithReporter(toolCtxValue, reporter)
	output, err := tool.Execute(toolCtxValue, input)
	if err != nil {
		return nil, err
	}

	return &ToolCallResult{
		Name:   tool.Name(),
		Output: output,
	}, nil
}

type invokableTool struct {
	adapter *ToolAdapter
	tool    tools.Tool
	binding ToolBinding
	sink    events.Sink
}

func (t *invokableTool) Info(ctx context.Context) (*einoschema.ToolInfo, error) {
	if t == nil || t.tool == nil {
		return nil, fmt.Errorf("tool is required")
	}

	return toEinoToolInfo(t.tool), nil
}

func (t *invokableTool) InvokableRun(ctx context.Context, argumentsInJSON string, _ ...einotool.Option) (string, error) {
	if t == nil || t.adapter == nil {
		return errorOutput("tool adapter is required", t.tool.Name()), nil
	}

	input := make(map[string]interface{})
	if argumentsInJSON != "" {
		if err := json.Unmarshal([]byte(argumentsInJSON), &input); err != nil {
			return errorOutput(fmt.Sprintf("unmarshal tool arguments: %v", err), t.tool.Name()), nil
		}
	}

	startedAt := time.Now()
	toolCallID := fmt.Sprintf("tool_%d", startedAt.UnixNano())
	suppressToolEvents := isTodoTool(t.tool)
	if !suppressToolEvents {
		_ = t.emitToolEvent(ctx, events.NewToolCallStarted(toolCallID, t.tool.Name(), cloneArguments(input)))
	}

	result, err := invokeTool(ctx, t.tool, input, t.binding.ToolContext, t.binding.TodoReporter)
	if err != nil {
		if !suppressToolEvents {
			_ = t.emitToolEvent(ctx, events.NewToolCallFailed(toolCallID, t.tool.Name(), err.Error(), time.Since(startedAt).Milliseconds()))
		}
		return errorOutput(err.Error(), t.tool.Name()), nil
	}

	if !suppressToolEvents {
		_ = t.emitToolEvent(ctx, events.NewToolCallCompleted(toolCallID, t.tool.Name(), result.Output, time.Since(startedAt).Milliseconds()))
	}

	return result.Output, nil
}

func isTodoTool(tool tools.Tool) bool {
	return tool != nil && tool.Name() == "todo"
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

func (t *invokableTool) emitToolEvent(ctx context.Context, event *events.Event) error {
	if t == nil || t.sink == nil {
		return nil
	}
	err := t.sink.Emit(ctx, event)
	_ = err
	return nil
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

func toEinoToolInfo(tool tools.Tool) *einoschema.ToolInfo {
	if tool == nil {
		return nil
	}

	params := make(map[string]*einoschema.ParameterInfo)
	schema := tool.InputSchema()
	for name, property := range schema.Properties {
		params[name] = toEinoParameterInfo(property, schema.Required, name)
	}

	toolInfo := &einoschema.ToolInfo{
		Name: tool.Name(),
		Desc: tool.Description(),
	}
	if len(params) > 0 {
		toolInfo.ParamsOneOf = einoschema.NewParamsOneOfByParams(params)
	}

	return toolInfo
}

func toEinoParameterInfo(property *tools.Property, required []string, fieldName string) *einoschema.ParameterInfo {
	if property == nil {
		return nil
	}

	info := &einoschema.ParameterInfo{
		Type:     toEinoDataType(property.Type),
		Desc:     property.Description,
		Enum:     property.Enum,
		Required: isRequired(required, fieldName),
	}
	if property.Items != nil {
		info.ElemInfo = toEinoParameterInfo(property.Items, nil, "")
	}

	return info
}

func toEinoDataType(value string) einoschema.DataType {
	switch value {
	case "object":
		return einoschema.Object
	case "number":
		return einoschema.Number
	case "integer":
		return einoschema.Integer
	case "array":
		return einoschema.Array
	case "boolean":
		return einoschema.Boolean
	case "null":
		return einoschema.Null
	default:
		return einoschema.String
	}
}

func isRequired(required []string, fieldName string) bool {
	for _, candidate := range required {
		if candidate == fieldName {
			return true
		}
	}

	return false
}
