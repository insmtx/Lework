package eino

import (
	"context"
	"fmt"

	einotool "github.com/cloudwego/eino/components/tool"
	einoschema "github.com/cloudwego/eino/schema"
)

// Schema describes tool input or output in a provider-agnostic shape.
type Schema struct {
	Type       string
	Required   []string
	Properties map[string]*Property
}

// Property describes a single field inside a tool schema.
type Property struct {
	Type        string
	Description string
	Enum        []string
	Items       *Property
}

// ToolSpec contains LLM-facing tool metadata.
type ToolSpec struct {
	Name        string
	Description string
	InputSchema Schema
}

// ToolInvoker executes a tool with raw JSON arguments.
type ToolInvoker interface {
	InvokeTool(ctx context.Context, name string, argumentsInJSON string) (string, error)
}

// NewTool wraps a tool specification and invoker as an Eino invokable tool.
func NewTool(spec ToolSpec, invoker ToolInvoker) einotool.BaseTool {
	return &invokableTool{
		spec:    spec,
		invoker: invoker,
	}
}

type invokableTool struct {
	spec    ToolSpec
	invoker ToolInvoker
}

func (t *invokableTool) Info(ctx context.Context) (*einoschema.ToolInfo, error) {
	if t == nil {
		return nil, fmt.Errorf("tool is required")
	}
	if t.spec.Name == "" {
		return nil, fmt.Errorf("tool name is required")
	}
	return ToToolInfo(t.spec), nil
}

func (t *invokableTool) InvokableRun(ctx context.Context, argumentsInJSON string, _ ...einotool.Option) (string, error) {
	if t == nil || t.invoker == nil {
		return "", fmt.Errorf("tool invoker is required")
	}
	return t.invoker.InvokeTool(ctx, t.spec.Name, argumentsInJSON)
}

// ToToolInfo converts provider-neutral tool metadata to Eino tool metadata.
func ToToolInfo(spec ToolSpec) *einoschema.ToolInfo {
	params := make(map[string]*einoschema.ParameterInfo)
	for name, property := range spec.InputSchema.Properties {
		params[name] = toEinoParameterInfo(property, spec.InputSchema.Required, name)
	}

	toolInfo := &einoschema.ToolInfo{
		Name: spec.Name,
		Desc: spec.Description,
	}
	if len(params) > 0 {
		toolInfo.ParamsOneOf = einoschema.NewParamsOneOfByParams(params)
	}
	return toolInfo
}

func toEinoParameterInfo(property *Property, required []string, fieldName string) *einoschema.ParameterInfo {
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
