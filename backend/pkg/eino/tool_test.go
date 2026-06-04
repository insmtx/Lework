package eino

import (
	"context"
	"testing"

	einotool "github.com/cloudwego/eino/components/tool"
)

func TestToolInfoConvertsSchema(t *testing.T) {
	info := ToToolInfo(ToolSpec{
		Name:        "lookup",
		Description: "Lookup records",
		InputSchema: Schema{
			Type:     "object",
			Required: []string{"query"},
			Properties: map[string]*Property{
				"query": {
					Type:        "string",
					Description: "Search query",
				},
				"limit": {
					Type: "integer",
				},
			},
		},
	})
	if info == nil || info.Name != "lookup" || info.Desc != "Lookup records" {
		t.Fatalf("unexpected tool info: %#v", info)
	}
	if info.ParamsOneOf == nil {
		t.Fatalf("expected params")
	}
}

func TestToolInvokesRawJSONArguments(t *testing.T) {
	invoker := &recordingInvoker{}
	tool := NewTool(ToolSpec{Name: "lookup"}, invoker)
	runnable, ok := tool.(interface {
		InvokableRun(context.Context, string, ...einotool.Option) (string, error)
	})
	if !ok {
		t.Fatalf("expected invokable tool, got %T", tool)
	}

	output, err := runnable.InvokableRun(context.Background(), `{"query":"go"}`)
	if err != nil {
		t.Fatalf("run tool: %v", err)
	}
	if output != "ok" {
		t.Fatalf("output = %q, want ok", output)
	}
	if invoker.name != "lookup" || invoker.arguments != `{"query":"go"}` {
		t.Fatalf("unexpected invocation: %#v", invoker)
	}
}

type recordingInvoker struct {
	name      string
	arguments string
}

func (i *recordingInvoker) InvokeTool(ctx context.Context, name string, argumentsInJSON string) (string, error) {
	i.name = name
	i.arguments = argumentsInJSON
	return "ok", nil
}
