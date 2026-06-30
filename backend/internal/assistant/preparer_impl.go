package assistant

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"

	"github.com/insmtx/Leros/backend/agent"
	lifecyclecontext "github.com/insmtx/Leros/backend/internal/assistant/context"
	assistantdomain "github.com/insmtx/Leros/backend/internal/assistant/domain"
	modelrouter "github.com/insmtx/Leros/backend/internal/modelrouter"
	agentworkspace "github.com/insmtx/Leros/backend/internal/workspace"
	"github.com/ygpkg/yg-go/logs"
)

// WorkspaceManager prepares task workspaces (clone/populate repo).
type WorkspaceManager interface {
	PrepareWorkspace(ctx context.Context, req *assistantdomain.RunRequest) (WorkspacePreparation, error)
}

// WorkspacePreparation is the immutable result of preparing a run workspace.
type WorkspacePreparation struct {
	WorkDir              string
	RepoDir              string
	TaskDir              string
	ArtifactManifestPath string
	BaselinePath         string
}

// AttachmentIngestor downloads and commits user attachments into the workspace.
// It is best-effort: failures are logged but do not block the run.
type AttachmentIngestor interface {
	IngestAttachments(ctx context.Context, req *assistantdomain.RunRequest)
}

// workspaceManager is the default WorkspaceManager implementation.
type workspaceManager struct {
	env   string
	gitea *giteaAccess
}

type giteaAccess struct {
	endpoint    string
	owner       string
	accessToken string
}

// NewWorkspaceManager creates a WorkspaceManager backed by the given Gitea config.
func NewWorkspaceManager(env, giteaEndpoint, giteaOwner, giteaAccessToken string) WorkspaceManager {
	wm := &workspaceManager{env: env}
	if giteaEndpoint != "" && giteaOwner != "" && giteaAccessToken != "" {
		wm.gitea = &giteaAccess{
			endpoint:    giteaEndpoint,
			owner:       giteaOwner,
			accessToken: giteaAccessToken,
		}
	}
	return wm
}

func (wm *workspaceManager) PrepareWorkspace(ctx context.Context, req *assistantdomain.RunRequest) (WorkspacePreparation, error) {
	if req == nil {
		return WorkspacePreparation{}, fmt.Errorf("workspace request is required")
	}

	projectID := strings.TrimSpace(req.Workspace.ProjectID)
	if projectID == "" {
		workDir, err := agentworkspace.PrepareTempWorkspace()
		if err != nil {
			return WorkspacePreparation{}, err
		}
		return WorkspacePreparation{WorkDir: workDir}, nil
	}

	cloneURL, err := wm.cloneURL(req.Workspace.OrgID, projectID)
	if err != nil {
		return WorkspacePreparation{}, err
	}
	plan, err := agentworkspace.PrepareTaskWorkspace(ctx, agentworkspace.TaskWorkspaceRequest{
		OrgID:            req.Workspace.OrgID,
		ProjectID:        projectID,
		TaskID:           firstNonEmpty(req.Workspace.TaskID, req.TaskID),
		RequestID:        req.Workspace.RequestID,
		RequestedWorkDir: req.Runtime.WorkDir,
		CloneURL:         cloneURL,
	})
	if err != nil {
		return WorkspacePreparation{}, err
	}
	return WorkspacePreparation{
		WorkDir:              plan.EffectiveWorkDir,
		RepoDir:              plan.RepoDir,
		TaskDir:              plan.TaskDir,
		ArtifactManifestPath: plan.ArtifactManifestPath,
		BaselinePath:         plan.BaselinePath,
	}, nil
}

func (wm *workspaceManager) cloneURL(orgID uint, projectID string) (string, error) {
	if wm == nil || wm.gitea == nil {
		return "", fmt.Errorf("gitea is required for project workspace")
	}
	endpoint, err := url.Parse(strings.TrimSpace(wm.gitea.endpoint))
	if err != nil {
		return "", fmt.Errorf("parse gitea endpoint: %w", err)
	}
	if endpoint.Scheme == "" || endpoint.Host == "" {
		return "", fmt.Errorf("invalid gitea endpoint %q", wm.gitea.endpoint)
	}
	endpoint.User = url.UserPassword(wm.gitea.owner, wm.gitea.accessToken)
	repoName := fmt.Sprintf("%s-%d-%s.git", wm.env, orgID, projectID)
	endpoint.Path = strings.TrimRight(endpoint.Path, "/") + "/" + wm.gitea.owner + "/" + repoName
	endpoint.RawQuery = ""
	endpoint.Fragment = ""
	return endpoint.String(), nil
}

// attachmentIngestor is the default AttachmentIngestor.
type attachmentIngestor struct{}

// NewAttachmentIngestor creates a new AttachmentIngestor.
func NewAttachmentIngestor() AttachmentIngestor {
	return &attachmentIngestor{}
}

func (ai *attachmentIngestor) IngestAttachments(ctx context.Context, req *assistantdomain.RunRequest) {
	if req == nil || len(req.Input.Attachments) == 0 {
		return
	}
	targetRoot := strings.TrimSpace(req.Workspace.RepoDir)
	if targetRoot == "" {
		targetRoot = req.Runtime.WorkDir
	}
	targetDir := filepath.Join(targetRoot, "uploads")
	if err := os.MkdirAll(targetDir, 0o755); err != nil {
		logs.WarnContextf(ctx, "ingest attachments: create uploads dir: %v", err)
		return
	}

	for _, att := range req.Input.Attachments {
		if strings.TrimSpace(att.URL) == "" || strings.TrimSpace(att.Name) == "" {
			continue
		}
		if err := downloadAttachment(ctx, att.URL, filepath.Join(targetDir, att.Name)); err != nil {
			logs.WarnContextf(ctx, "ingest attachment %q: %v", att.Name, err)
			continue
		}
	}
}

func downloadAttachment(ctx context.Context, url string, destPath string) error {
	httpReq, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return fmt.Errorf("create request: %w", err)
	}
	resp, err := http.DefaultClient.Do(httpReq)
	if err != nil {
		return fmt.Errorf("http get: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return fmt.Errorf("unexpected status %d", resp.StatusCode)
	}
	file, err := os.Create(destPath)
	if err != nil {
		return fmt.Errorf("create file: %w", err)
	}
	defer file.Close()
	if _, err := io.Copy(file, resp.Body); err != nil {
		return fmt.Errorf("write file: %w", err)
	}
	return nil
}

// preparer is the concrete RunPreparer implementation.
type preparer struct {
	builder       *lifecyclecontext.ContextBuilder
	modelStore    *modelrouter.ModelStore
	workspaceMgr  WorkspaceManager
	attachmentIng AttachmentIngestor
	toolProvider  ToolProvider
}

// NewPreparer creates a new RunPreparer.
func NewPreparer(builder *lifecyclecontext.ContextBuilder) Preparer {
	return NewPreparerWithPorts(builder, nil, nil, modelrouter.NewModelStore())
}

// NewPreparerWithPorts creates a RunPreparer with injected workspace and attachment ports.
func NewPreparerWithPorts(
	builder *lifecyclecontext.ContextBuilder,
	wm WorkspaceManager,
	ai AttachmentIngestor,
	modelStore *modelrouter.ModelStore,
) Preparer {
	return NewPreparerWithTools(builder, wm, ai, modelStore, nil)
}

// NewPreparerWithTools creates a RunPreparer with all external dependencies injected.
func NewPreparerWithTools(
	builder *lifecyclecontext.ContextBuilder,
	wm WorkspaceManager,
	ai AttachmentIngestor,
	modelStore *modelrouter.ModelStore,
	toolProvider ToolProvider,
) Preparer {
	return &preparer{
		builder:       builder,
		modelStore:    modelStore,
		workspaceMgr:  wm,
		attachmentIng: ai,
		toolProvider:  toolProvider,
	}
}

// Prepare validates and builds a PreparedRun from the original Request.
// The original Request is NOT modified by this method.
func (p *preparer) Prepare(ctx context.Context, req *assistantdomain.RunRequest) (*PreparedRun, error) {
	if req == nil {
		return nil, fmt.Errorf("request context is required")
	}
	if p.builder == nil {
		return nil, fmt.Errorf("context builder is required")
	}

	// Clone so we don't modify the original request.
	cloned := assistantdomain.CloneRequest(req)

	// 1. Validate model config.
	if err := validateModelConfig(cloned); err != nil {
		return nil, err
	}

	// 2. Resolve model routing (write upstream config, set proxy base URL).
	if err := p.resolveModelRouting(cloned); err != nil {
		return nil, err
	}

	// 3. Prepare the workspace before building any prompt that references it.
	workspace, err := p.prepareWorkspace(ctx, cloned)
	if err != nil {
		return nil, fmt.Errorf("prepare workspace: %w", err)
	}
	cloned.Runtime.WorkDir = workspace.WorkDir
	cloned.Workspace.RepoDir = workspace.RepoDir

	// 4. Prepare session context and skills.
	if p.builder.SessionMessages != nil {
		if err := p.builder.SessionMessages.Prepare(ctx, cloned); err != nil {
			return nil, fmt.Errorf("prepare session context: %w", err)
		}
	}
	if err := lifecyclecontext.ApplyInvokedSkills(ctx, cloned); err != nil {
		return nil, fmt.Errorf("apply invoked skills: %w", err)
	}

	// 5. Build system prompt.
	systemPrompt, err := p.builder.BuildSystemPrompt(ctx, cloned)
	if err != nil {
		return nil, fmt.Errorf("build system prompt: %w", err)
	}

	// 6. Ingest attachments after the final workspace is known.
	if p.attachmentIng != nil {
		p.attachmentIng.IngestAttachments(ctx, cloned)
	}

	// 7. Build user prompt from the prepared clone so skill rewrites are retained.
	prompt := assistantdomain.BuildUserInput(cloned)
	if attachmentText := assistantdomain.BuildAttachmentText(cloned.Input.Attachments); attachmentText != "" {
		if prompt != "" {
			prompt += "\n"
		}
		prompt += attachmentText
	}

	// 8. Build ExecutionSpec.
	model := agent.ModelConfig{
		Provider: cloned.Model.Provider,
		Model:    cloned.Model.Model,
		APIKey:   cloned.Model.APIKey,
		BaseURL:  cloned.Model.BaseURL,
	}

	messages := make([]agent.Message, 0, len(cloned.Conversation.Messages))
	for _, message := range cloned.Conversation.Messages {
		messages = append(messages, agent.Message{Role: message.Role, Content: message.Content})
	}
	var runtimeTools []agent.Tool
	if p.toolProvider != nil {
		runtimeTools, err = p.toolProvider.ToolsFor(cloned, workspace)
		if err != nil {
			return nil, fmt.Errorf("prepare runtime tools: %w", err)
		}
	}

	return &PreparedRun{
		Request: cloned,
		Execution: agent.ExecutionRequest{
			ExecutionID:  cloned.RunID,
			TraceID:      cloned.TraceID,
			Runtime:      strings.TrimSpace(cloned.Runtime.Kind),
			SessionKey:   cloned.Conversation.ID,
			InstanceKey:  cloned.Assistant.ID,
			Mode:         cloned.ExecutionMode,
			SystemPrompt: systemPrompt,
			Prompt:       prompt,
			Messages:     messages,
			Tools:        runtimeTools,
			Model:        model,
			Policy: agent.ExecutionPolicy{
				AllowedTools:   append([]string(nil), cloned.Capability.AllowedTools...),
				PermissionMode: cloned.Policy.PermissionMode,
				MaxSteps:       cloned.Runtime.MaxStep,
			},
			Filesystem: agent.FilesystemContext{
				WorkDir: workspace.WorkDir,
				RepoDir: workspace.RepoDir,
				TaskDir: workspace.TaskDir,
			},
		},
		Workspace: workspace,
	}, nil
}

// validateModelConfig validates the required model fields.
func validateModelConfig(req *assistantdomain.RunRequest) error {
	if strings.TrimSpace(req.Model.Provider) == "" {
		return fmt.Errorf("llm provider is required")
	}
	if strings.TrimSpace(req.Model.Model) == "" {
		return fmt.Errorf("llm model is required")
	}
	if strings.TrimSpace(req.Model.APIKey) == "" {
		return fmt.Errorf("llm api_key is required")
	}
	return nil
}

// resolveModelRouting writes upstream config to model store and sets proxy base URL.
func (p *preparer) resolveModelRouting(req *assistantdomain.RunRequest) error {
	upstreamCfg := modelrouter.UpstreamConfig{
		ModelName:    strings.TrimSpace(req.Model.Model),
		Provider:     strings.TrimSpace(req.Model.Provider),
		BaseURL:      strings.TrimSpace(req.Model.BaseURL),
		BaseURLHasV1: req.Model.BaseURLHasV1,
		APIKey:       strings.TrimSpace(req.Model.APIKey),
		Temperature:  req.Model.Temperature,
	}
	if p.modelStore != nil {
		p.modelStore.Put(upstreamCfg)
	}
	req.Model.BaseURL = modelrouter.WorkerProxyBaseURL()
	return nil
}

func (p *preparer) prepareWorkspace(ctx context.Context, req *assistantdomain.RunRequest) (WorkspacePreparation, error) {
	if p.workspaceMgr != nil {
		return p.workspaceMgr.PrepareWorkspace(ctx, req)
	}
	if strings.TrimSpace(req.Workspace.ProjectID) != "" {
		// 没有 WorkspaceManager（如 gitea.enabled=false），使用本地 git init
		plan, err := agentworkspace.PrepareTaskWorkspace(ctx, agentworkspace.TaskWorkspaceRequest{
			OrgID:            req.Workspace.OrgID,
			ProjectID:        strings.TrimSpace(req.Workspace.ProjectID),
			TaskID:           firstNonEmpty(req.Workspace.TaskID, req.TaskID),
			RequestID:        req.Workspace.RequestID,
			RequestedWorkDir: req.Runtime.WorkDir,
		})
		if err != nil {
			return WorkspacePreparation{}, err
		}
		return WorkspacePreparation{
			WorkDir:              plan.EffectiveWorkDir,
			RepoDir:              plan.RepoDir,
			TaskDir:              plan.TaskDir,
			ArtifactManifestPath: plan.ArtifactManifestPath,
			BaselinePath:         plan.BaselinePath,
		}, nil
	}
	workDir := strings.TrimSpace(req.Runtime.WorkDir)
	if workDir == "" {
		var err error
		workDir, err = agentworkspace.PrepareTempWorkspace()
		if err != nil {
			return WorkspacePreparation{}, err
		}
	}
	return WorkspacePreparation{WorkDir: workDir}, nil
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return strings.TrimSpace(value)
		}
	}
	return ""
}
