// Package mcp 通过 Model Context Protocol 暴露 Leros 运行时能力。
package mcp

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"

	"github.com/insmtx/Leros/backend/tools"
	artifactdeclaretools "github.com/insmtx/Leros/backend/tools/artifact_declare"
	memorytools "github.com/insmtx/Leros/backend/tools/memory"
	skillmanagetools "github.com/insmtx/Leros/backend/tools/skill_manage"
	mcpsdk "github.com/mark3labs/mcp-go/mcp"
	mcpserver "github.com/mark3labs/mcp-go/server"
	"github.com/ygpkg/yg-go/logs"
)

const (
	serverName    = "Leros"
	serverVersion = "0.1.0"
)

// Server 持有 MCP SDK 服务器和 HTTP 传输层。
type Server struct {
	sdk       *mcpserver.MCPServer
	http      http.Handler
	authToken string
}

// NewServer 使用当前公开的工具创建 Leros MCP 服务器。
func NewServer() *Server {
	return NewServerWithTools(NewTools()...)
}

// NewServerWithToken creates the public MCP server with instance-scoped authentication.
func NewServerWithToken(token string) *Server {
	server := NewServerWithTools(NewTools()...)
	server.authToken = token
	return server
}

// NewTools 返回当前通过 MCP 暴露的 Leros 工具。
func NewTools() []tools.Tool {
	ts := []tools.Tool{
		memorytools.NewTool(),
	}
	if skillManage, err := skillmanagetools.NewTool(); err != nil {
		logs.Warnf("MCP: skill_manage tool unavailable: %v", err)
	} else {
		ts = append(ts, skillManage)
	}
	ts = append(ts, artifactdeclaretools.NewTool())
	return ts
}

// NewServerWithTools 从 Leros 内部工具创建 Leros MCP 服务器。
func NewServerWithTools(publicTools ...tools.Tool) *Server {
	sdk := mcpserver.NewMCPServer(
		serverName,
		serverVersion,
		mcpserver.WithRecovery(),
	)

	registerTools(sdk, publicTools)

	return &Server{
		sdk:       sdk,
		http:      mcpserver.NewStreamableHTTPServer(sdk),
		authToken: "",
	}
}

// Handler 返回可流式传输的 HTTP MCP 传输处理器。
func (s *Server) Handler() http.Handler {
	if s == nil {
		return http.NotFoundHandler()
	}
	return s.http
}

// GetTool 按名称返回已注册的 MCP 工具。用于测试和诊断。
func (s *Server) GetTool(name string) *mcpserver.ServerTool {
	if s == nil || s.sdk == nil {
		return nil
	}
	return s.sdk.GetTool(name)
}

func registerTools(s *mcpserver.MCPServer, publicTools []tools.Tool) {
	for _, tool := range publicTools {
		s.AddTool(toMCPTool(tool), toMCPHandler(tool))
	}
}

func toMCPTool(tool tools.Tool) mcpsdk.Tool {
	schemaBytes, err := json.Marshal(tool.InputSchema())
	if err != nil {
		schemaBytes = []byte(`{"type":"object"}`)
	}

	var schemaMap map[string]any
	if err := json.Unmarshal(schemaBytes, &schemaMap); err == nil {
		props, _ := schemaMap["properties"].(map[string]any)
		if props == nil {
			props = make(map[string]any)
			schemaMap["properties"] = props
		}
		// 注入公共 intent 字段（可选，用于 LLM 描述调用目的）
		props["intent"] = map[string]any{
			"type":        "string",
			"description": "A brief Chinese description of the purpose of this tool call",
		}
	}

	augmented, _ := json.Marshal(schemaMap)
	return mcpsdk.NewToolWithRawSchema(tool.Name(), tool.Description(), augmented)
}

func toMCPHandler(tool tools.Tool) mcpserver.ToolHandlerFunc {
	return func(ctx context.Context, request mcpsdk.CallToolRequest) (*mcpsdk.CallToolResult, error) {
		args := request.GetArguments()
		if args == nil {
			args = map[string]any{}
		}
		delete(args, "intent") // 剥离公共 intent 字段，工具不需要处理
		rawArgs, err := json.Marshal(args)
		if err != nil {
			return mcpsdk.NewToolResultError(fmt.Sprintf("encode tool arguments: %v", err)), nil
		}

		if validator, ok := tool.(tools.Validator); ok {
			if err := validator.Validate(rawArgs); err != nil {
				return mcpsdk.NewToolResultError(err.Error()), nil
			}
		}

		output, err := tool.Execute(ctx, rawArgs)
		if err != nil {
			return mcpsdk.NewToolResultError(err.Error()), nil
		}
		if output == "" {
			return mcpsdk.NewToolResultText("{}"), nil
		}

		var structured any
		if err := json.Unmarshal([]byte(output), &structured); err == nil {
			return mcpsdk.NewToolResultStructured(structured, output), nil
		}

		return mcpsdk.NewToolResultText(fmt.Sprintf("%s", output)), nil
	}
}
