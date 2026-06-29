package assistant

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/insmtx/Leros/backend/agent"
	assistantdomain "github.com/insmtx/Leros/backend/internal/assistant/domain"
	"github.com/insmtx/Leros/backend/tools"
)

// Adapt binds a business tools.Tool to run-scoped context and the agent.Tool result contract.
func Adapt(legacy tools.Tool, toolCtx tools.ToolContext) agent.Tool {
	return adapter{tool: legacy, toolCtx: toolCtx}
}

type adapter struct {
	tool    tools.Tool
	toolCtx tools.ToolContext
}

func (a adapter) Definition() agent.ToolDefinition {
	if a.tool == nil {
		return agent.ToolDefinition{}
	}
	schema := a.tool.InputSchema()
	parameters, _ := json.Marshal(schema)
	return agent.ToolDefinition{
		Name:        a.tool.Name(),
		Description: a.tool.Description(),
		Parameters:  parameters,
	}
}

func (a adapter) Execute(ctx context.Context, input json.RawMessage) (agent.ToolResult, error) {
	if a.tool == nil {
		return agent.ToolResult{}, fmt.Errorf("tool is required")
	}
	if validator, ok := a.tool.(tools.Validator); ok {
		if err := validator.Validate(input); err != nil {
			return agent.ToolResult{Error: err.Error(), IsError: true}, nil
		}
	}
	ctx = tools.ContextWithToolContext(ctx, a.toolCtx)
	result, err := a.tool.Execute(ctx, input)
	if err != nil {
		return agent.ToolResult{Error: err.Error(), IsError: true}, nil
	}
	return agent.ToolResult{Content: result}, nil
}

type registryToolProvider struct {
	registry *tools.Registry
}

// NewToolProvider adapts the application tool registry at the assistant boundary.
func NewToolProvider(registry *tools.Registry) ToolProvider {
	return &registryToolProvider{registry: registry}
}

func (p *registryToolProvider) ToolsFor(
	req *assistantdomain.RunRequest,
	workspace WorkspacePreparation,
) ([]agent.Tool, error) {
	if p == nil || p.registry == nil {
		return nil, fmt.Errorf("tool registry is required")
	}
	if req == nil {
		return nil, fmt.Errorf("tool request is required")
	}

	legacyTools := p.registry.List()
	result := make([]agent.Tool, 0, len(legacyTools))
	toolCtx := tools.ToolContext{
		RunID:          req.RunID,
		TraceID:        req.TraceID,
		AssistantID:    req.Assistant.ID,
		UserID:         req.Actor.UserID,
		AccountID:      req.Actor.AccountID,
		Channel:        req.Actor.Channel,
		ConversationID: req.Conversation.ID,
		ExternalID:     req.Actor.ExternalID,
		WorkDir:        workspace.WorkDir,
	}
	if workspace.RepoDir != "" || workspace.ArtifactManifestPath != "" {
		toolCtx.Metadata = tools.ToolMetadata{
			RepoDir:              workspace.RepoDir,
			ArtifactManifestPath: workspace.ArtifactManifestPath,
		}
	}
	for _, legacy := range legacyTools {
		if legacy == nil || strings.TrimSpace(legacy.Name()) == "" {
			continue
		}
		result = append(result, Adapt(legacy, toolCtx))
	}
	return result, nil
}
