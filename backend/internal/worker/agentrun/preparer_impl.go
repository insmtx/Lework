package agentrun

import (
	"bytes"
	"context"
	"encoding/base64"
	"fmt"
	"image"
	"image/jpeg"
	_ "image/png"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"

	"golang.org/x/image/draw"

	"github.com/insmtx/Leros/backend/agent"
	"github.com/insmtx/Leros/backend/internal/consts"
	modelrouter "github.com/insmtx/Leros/backend/internal/modelrouter"
	"github.com/insmtx/Leros/backend/internal/service"
	agentruncontext "github.com/insmtx/Leros/backend/internal/worker/agentrun/context"
	agentrundomain "github.com/insmtx/Leros/backend/internal/worker/agentrun/domain"
	"github.com/insmtx/Leros/backend/internal/worker/identity"
	agentworkspace "github.com/insmtx/Leros/backend/internal/workspace"
	"github.com/ygpkg/yg-go/logs"
)

// WorkspaceManager prepares task workspaces (clone/populate repo).
type WorkspaceManager interface {
	PrepareWorkspace(
		ctx context.Context,
		req agentworkspace.TaskWorkspaceRequest,
	) (WorkspacePreparation, error)
}

// WorkspacePreparation is the immutable result of preparing a run workspace.
type WorkspacePreparation struct {
	WorkDir              string
	ProjectRoot          string
	RepoDir              string
	TaskDir              string
	SkillDir             string
	ArtifactManifestPath string
	PreRunTreeSHA        string // Git tree SHA captured before agent execution
}

// AttachmentIngestor downloads and commits user attachments into the workspace.
// It is best-effort: failures are logged but do not block the run.
type AttachmentIngestor interface {
	IngestAttachments(ctx context.Context, req *agentrundomain.RunRequest)
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

func (wm *workspaceManager) PrepareWorkspace(
	ctx context.Context,
	req agentworkspace.TaskWorkspaceRequest,
) (WorkspacePreparation, error) {
	projectID := strings.TrimSpace(req.ProjectID)
	if projectID == "" {
		workDir, err := agentworkspace.PrepareTempWorkspace()
		if err != nil {
			return WorkspacePreparation{}, err
		}
		return WorkspacePreparation{WorkDir: workDir}, nil
	}

	cloneURL, err := wm.cloneURL(req.OrgID, projectID)
	if err != nil {
		return WorkspacePreparation{}, err
	}
	plan, err := agentworkspace.PrepareTaskWorkspace(ctx, agentworkspace.TaskWorkspaceRequest{
		OrgID:            req.OrgID,
		ProjectID:        projectID,
		TaskID:           req.TaskID,
		RequestID:        req.RequestID,
		RequestedWorkDir: req.RequestedWorkDir,
		CloneURL:         cloneURL,
	})
	if err != nil {
		return WorkspacePreparation{}, err
	}
	return WorkspacePreparation{
		WorkDir:              plan.EffectiveWorkDir,
		ProjectRoot:          plan.ProjectRoot,
		RepoDir:              plan.RepoDir,
		TaskDir:              plan.TaskDir,
		ArtifactManifestPath: plan.ArtifactManifestPath,
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

func (ai *attachmentIngestor) IngestAttachments(ctx context.Context, req *agentrundomain.RunRequest) {
	if req == nil || len(req.Input.Attachments) == 0 {
		return
	}
	targetRoot := strings.TrimSpace(req.Workspace.RepoDir)
	if targetRoot == "" {
		targetRoot = req.Runtime.WorkDir
	}
	targetDir := filepath.Join(targetRoot, consts.RepoDirUploads)
	if err := os.MkdirAll(targetDir, 0o755); err != nil {
		logs.WarnContextf(ctx, "ingest attachments: create uploads dir: %v", err)
		return
	}

	for _, att := range req.Input.Attachments {
		if strings.TrimSpace(att.URL) == "" || strings.TrimSpace(att.Name) == "" {
			continue
		}
		relativeName := filepath.ToSlash(strings.TrimSpace(att.Name))
		destPath := filepath.Join(targetDir, filepath.FromSlash(relativeName))
		if err := os.MkdirAll(filepath.Dir(destPath), 0o755); err != nil {
			logs.WarnContextf(ctx, "ingest attachment mkdir %q: %v", att.Name, err)
			continue
		}
		if err := downloadAttachment(ctx, att.URL, destPath); err != nil {
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

// maxMultimodalInlineBytes 限制以 data: URL 内联进 runtime 的多模态附件大小。
// 超过该阈值的大文件不再读取字节内联（Data 为空），其上落盘位置由调用方通过
// Filesystem.UploadRelDir 提供给 runtime，由 runtime 侧按路径读取，避免单条消息
// 字节/上下文膨胀超限。
var maxMultimodalInlineBytes = 100 * 1024 * 1024 // 100MiB

// multimodalAttachmentsForRuntime 从输入附件中筛出多模态文件（仅图片）并下载其字节，
// 用于 runtime 的多模态输入。失败的下载跳过并仅告警（非致命）。
// 返回的每个 agent.Attachment 携带 MIME 与原始文件名 Name；是否内联由 Data 决定。
// 成功内联的字节会同步回填到源 attachments[i].Data，供 BuildAttachmentText 判定
// 该图片是否真正内联成功：内联成功的图片提示"无需 read"，失败的图片降级为按路径读取，
// 避免模型在既无像素又无路径的情况下凭空脑补（文不对题）。
// PDF/音视频已退出多模态管线：不在此下载内联，统一走 BuildAttachmentText 的按路径读取。
func multimodalAttachmentsForRuntime(ctx context.Context, attachments []agentrundomain.Attachment) []agent.Attachment {
	var result []agent.Attachment
	for i := range attachments {
		att := &attachments[i]
		if !agentrundomain.IsVisualMime(att.MimeType) {
			continue
		}
		base := strings.TrimSpace(att.Name)
		if base == "" {
			continue
		}
		u := strings.TrimSpace(att.URL)
		if u == "" {
			continue
		}
		logs.Infof("[forensic][multimodal] target: name=%q mime=%q url=%q", base, att.MimeType, u)
		a := agent.Attachment{
			MIME: att.MimeType,
			Name: base,
		}
		data, err := downloadAttachmentBytes(ctx, u)
		if err != nil {
			logs.WarnContextf(ctx, "fetch multimodal attachment %q: %v", att.Name, err)
			continue
		}
		// FORENSIC-DOWNSCALE: 记录内联前的原始字节与声明的 MIME
		logs.Infof("[forensic][downscale] before: name=%q mime=%q size=%d", base, att.MimeType, len(data))
		if resized, newMIME, err := downscaleMultimodalImage(data, att.MimeType); err == nil && resized != nil {
			data = resized
			// 缩放到 jpeg 后固定声明 image/jpeg，避免 opencode 对大图执行 PNG 重编码路径导致 SSE 不推送
			a.MIME = newMIME
			logs.Infof("[forensic][downscale] resized: name=%q size=%d input_mime=%q output_mime=%q", base, len(data), att.MimeType, newMIME)
		} else if err != nil {
			logs.Debugf("downscale multimodal attachment %q skipped: %v", base, err)
		}
		if len(data) > maxMultimodalInlineBytes {
			logs.WarnContextf(ctx, "multimodal attachment %q exceeds %d bytes, skip inline and read via uploads path",
				att.Name, maxMultimodalInlineBytes)
		} else {
			a.Data = data
			// 回填源附件，标记内联成功，供 BuildAttachmentText 与 opencode 分流判定。
			att.Data = data
		}
		result = append(result, a)
	}
	return result
}

// maxMultimodalSide 内联多模态图片的最长边像素阈值。
// opencode 对超出此尺寸的图片（如 2048x2048）会触发内部 PNG 重编码路径，
// 该路径下生成的文本不会通过 SSE 流式上报，导致 worker 只拿到用户输入回声。
// 此处预先缩放到阈值内，规避该缺陷并减小传输体积。
const maxMultimodalSide = 1600

// maxMultimodalBase64Bytes 内联多模态图片 base64 编码后的字节数阈值。
// 对齐 opencode Image.normalize 的 MAX_BASE64_BYTES（5MiB）：opencode 对
// base64 超过该阈值的图片同样触发内部重编码路径，即使像素尺寸未超过
// maxMultimodalSide 也会导致 SSE 不推送文本。仅靠像素阈值无法覆盖此类图片，
// 故按字节维度预先重编码为 JPEG 压到阈值内。
const maxMultimodalBase64Bytes = 5 * 1024 * 1024

// jpegQualityLadder 是 base64 字节超限时按降序尝试的 JPEG 质量阶梯。
// 与 opencode 的 JPEG_QUALITIES 思路一致：优先画质，逐步降质压到阈值内。
var jpegQualityLadder = []int{85, 70, 55, 40}

// downscaleMultimodalImage 将图片归一化到 opencode 不会触发内部重编码的范围内：
//   - 像素维度：最长边缩放到 maxMultimodalSide 以内（等比）；
//   - 字节维度：base64 超过 maxMultimodalBase64Bytes 时按质量阶梯重新编码为 JPEG。
//
// 解码失败时返回 error（不修改原数据）；无需任何处理时返回 nil（调用方沿用原数据）；
// 处理后统一返回 "image/jpeg"。
func downscaleMultimodalImage(data []byte, mime string) ([]byte, string, error) {
	src, _, err := image.Decode(bytes.NewReader(data))
	if err != nil {
		return nil, "", err
	}
	b := src.Bounds()
	w, h := b.Dx(), b.Dy()
	if w <= 0 || h <= 0 {
		return nil, "", fmt.Errorf("invalid image bounds %dx%d", w, h)
	}

	// 目标像素尺寸：长边超过阈值时等比缩到 maxMultimodalSide。
	sw, sh := w, h
	if w > maxMultimodalSide || h > maxMultimodalSide {
		if w > h {
			sh = h * maxMultimodalSide / w
			sw = maxMultimodalSide
		} else {
			sw = w * maxMultimodalSide / h
			sh = maxMultimodalSide
		}
		if sw < 1 {
			sw = 1
		}
		if sh < 1 {
			sh = 1
		}
	}

	// 预处理阶段仅做像素归一化（含解码重采样）；只有当像素或字节超限时才进入编码。
	needEncode := sw != w || sh != h || base64Len(data) > maxMultimodalBase64Bytes
	if !needEncode {
		// 尺寸与字节均达标，无需处理（保持原 MIME 不变）
		return nil, strings.TrimSpace(mime), nil
	}

	srcImg := src
	if sw != w || sh != h {
		dst := image.NewRGBA(image.Rect(0, 0, sw, sh))
		draw.CatmullRom.Scale(dst, dst.Bounds(), src, b, draw.Over, nil)
		srcImg = dst
	}

	// 优先以中等质量编码，若字节仍超限则按质量阶梯逐级降质。
	for _, quality := range jpegQualityLadder {
		var buf bytes.Buffer
		if err := jpeg.Encode(&buf, srcImg, &jpeg.Options{Quality: quality}); err != nil {
			return nil, "", err
		}
		if base64Len(buf.Bytes()) <= maxMultimodalBase64Bytes {
			return buf.Bytes(), "image/jpeg", nil
		}
	}

	// 阶梯降质仍无法压到阈值内：返回最低质量结果，避免内联体积无界膨胀。
	// 此时 opencode 可能仍会触发重编码，但已是最优可内联结果。
	var buf bytes.Buffer
	if err := jpeg.Encode(&buf, srcImg, &jpeg.Options{Quality: jpegQualityLadder[len(jpegQualityLadder)-1]}); err != nil {
		return nil, "", err
	}
	return buf.Bytes(), "image/jpeg", nil
}

// base64Len 返回 data 的 base64 编码（标准无换行）后的字符串长度。
// 与 opencode 的 Buffer.byteLength(base64, "utf8") 对齐：均按 base64 文本长度计。
func base64Len(data []byte) int {
	return base64.StdEncoding.EncodedLen(len(data))
}

func downloadAttachmentBytes(ctx context.Context, url string) ([]byte, error) {
	httpReq, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, fmt.Errorf("create request: %w", err)
	}
	resp, err := http.DefaultClient.Do(httpReq)
	if err != nil {
		return nil, fmt.Errorf("http get: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, fmt.Errorf("unexpected status %d", resp.StatusCode)
	}
	return io.ReadAll(resp.Body)
}

// preparer is the concrete RunPreparer implementation.
type preparer struct {
	builder       *agentruncontext.ContextBuilder
	modelStore    *modelrouter.ModelStore
	workspaceMgr  WorkspaceManager
	attachmentIng AttachmentIngestor
	toolProvider  ToolProvider
	sessionStore  ProviderSessionStore
	skillPreparer SkillPreparer
}

// NewPreparer creates a new RunPreparer.
func NewPreparer(builder *agentruncontext.ContextBuilder) Preparer {
	return NewPreparerWithPorts(builder, nil, nil, modelrouter.NewModelStore())
}

// NewPreparerWithPorts creates a RunPreparer with injected workspace and attachment ports.
func NewPreparerWithPorts(
	builder *agentruncontext.ContextBuilder,
	wm WorkspaceManager,
	ai AttachmentIngestor,
	modelStore *modelrouter.ModelStore,
) Preparer {
	return NewPreparerWithTools(builder, wm, ai, modelStore, nil)
}

// NewPreparerWithTools creates a RunPreparer with all external dependencies injected.
func NewPreparerWithTools(
	builder *agentruncontext.ContextBuilder,
	wm WorkspaceManager,
	ai AttachmentIngestor,
	modelStore *modelrouter.ModelStore,
	toolProvider ToolProvider,
) Preparer {
	return NewPreparerWithSkillPreparer(builder, wm, ai, modelStore, toolProvider, nil)
}

// NewPreparerWithSkillPreparer creates a preparer with a run-scoped Skill view.
func NewPreparerWithSkillPreparer(
	builder *agentruncontext.ContextBuilder,
	wm WorkspaceManager,
	ai AttachmentIngestor,
	modelStore *modelrouter.ModelStore,
	toolProvider ToolProvider,
	skillPreparer SkillPreparer,
) Preparer {
	return &preparer{
		builder:       builder,
		modelStore:    modelStore,
		workspaceMgr:  wm,
		attachmentIng: ai,
		toolProvider:  toolProvider,
		skillPreparer: skillPreparer,
	}
}

// NewPreparerWithSessionStoreAndSkills adds both session resume and run-scoped Skills.
func NewPreparerWithSessionStoreAndSkills(
	builder *agentruncontext.ContextBuilder,
	wm WorkspaceManager,
	ai AttachmentIngestor,
	modelStore *modelrouter.ModelStore,
	toolProvider ToolProvider,
	sessionStore ProviderSessionStore,
	skillPreparer SkillPreparer,
) Preparer {
	return &preparer{
		builder:       builder,
		modelStore:    modelStore,
		workspaceMgr:  wm,
		attachmentIng: ai,
		toolProvider:  toolProvider,
		sessionStore:  sessionStore,
		skillPreparer: skillPreparer,
	}
}

// NewPreparerWithSessionStore creates a RunPreparer with ProviderSessionStore for resume support.
func NewPreparerWithSessionStore(
	builder *agentruncontext.ContextBuilder,
	wm WorkspaceManager,
	ai AttachmentIngestor,
	modelStore *modelrouter.ModelStore,
	toolProvider ToolProvider,
	sessionStore ProviderSessionStore,
) Preparer {
	return NewPreparerWithSessionStoreAndSkills(builder, wm, ai, modelStore, toolProvider, sessionStore, nil)
}

// Prepare validates and builds a PreparedRun from the original Request.
// The original Request is NOT modified by this method.
func (p *preparer) Prepare(ctx context.Context, req *agentrundomain.RunRequest) (*PreparedRun, func(), error) {
	noop := func() {}
	if req == nil {
		return nil, noop, fmt.Errorf("request context is required")
	}
	if p.builder == nil {
		return nil, noop, fmt.Errorf("context builder is required")
	}

	// Clone so we don't modify the original request.
	cloned := agentrundomain.CloneRequest(req)
	applyDisabledPluginPolicy(ctx, cloned)

	// 1. Validate model config.
	if err := validateModelConfig(cloned); err != nil {
		return nil, noop, err
	}

	// 2. Resolve model routing (write upstream config, set proxy base URL).
	proxyModel := p.resolveModelRouting(cloned)

	cleanup := func() {
		if p.modelStore != nil && proxyModel != "" {
			p.modelStore.RemoveBiz(proxyModel)
		}
	}

	// 3. Prepare the workspace before building any prompt that references it.
	workspace, err := p.prepareWorkspace(ctx, cloned)
	if err != nil {
		return nil, cleanup, fmt.Errorf("prepare workspace: %w", err)
	}
	cloned.Runtime.WorkDir = workspace.WorkDir
	cloned.Workspace.RepoDir = workspace.RepoDir
	if p.skillPreparer != nil {
		skillDir, skillCleanup, skillErr := p.skillPreparer.PrepareSkills(ctx, cloned, workspace)
		cleanup = chainCleanup(cleanup, skillCleanup)
		if skillErr != nil {
			return nil, cleanup, fmt.Errorf("prepare run skills: %w", skillErr)
		}
		workspace.SkillDir = skillDir
		cloned.Workspace.SkillDir = skillDir
	}

	// Capture pre-run Git tree SHA for diff-based artifact discovery (best-effort).
	if workspace.RepoDir != "" {
		workspace.PreRunTreeSHA = agentworkspace.CapturePreRunTreeSafe(ctx, workspace.RepoDir)
	}

	// 4. Prepare session context and skills.
	if p.builder.SessionMessages != nil {
		if err := p.builder.SessionMessages.Prepare(ctx, cloned); err != nil {
			return nil, cleanup, fmt.Errorf("prepare session context: %w", err)
		}
	}
	if err := agentruncontext.ApplyInvokedSkills(ctx, cloned); err != nil {
		return nil, cleanup, fmt.Errorf("apply invoked skills: %w", err)
	}

	// 5. Build system prompt.
	systemPrompt, err := p.builder.BuildSystemPrompt(ctx, cloned)
	if err != nil {
		return nil, cleanup, fmt.Errorf("build system prompt: %w", err)
	}

	// 6. Ingest attachments after the final workspace is known.
	if p.attachmentIng != nil {
		p.attachmentIng.IngestAttachments(ctx, cloned)
	}

	// 6.5. Download multimodal (image) bytes and backfill inline state before
	// building the prompt, so BuildAttachmentText can correctly distinguish
	// successfully-inlined images (no read hint) from failed/large ones (read via
	// uploads path) and avoid prompting the model to hallucinate.
	attachments := multimodalAttachmentsForRuntime(ctx, cloned.Input.Attachments)

	// 7. Build user prompt from the prepared clone so skill rewrites are retained.
	prompt := agentrundomain.BuildUserInput(cloned)
	if attachmentText := agentrundomain.BuildAttachmentText(cloned.Input.Attachments); attachmentText != "" {
		if prompt != "" {
			prompt += "\n"
		}
		prompt += attachmentText
	}

	// 7.5. Resolve provider session for resume.
	var providerSession agent.ProviderSession
	if p.sessionStore != nil {
		providerSession = p.resolveProviderSession(ctx, cloned)
	}

	// 8. Build ExecutionSpec.
	model := agent.ModelConfig{
		Provider:         cloned.Model.Provider,
		Model:            cloned.Model.Model,
		APIKey:           cloned.Model.APIKey,
		BaseURL:          cloned.Model.BaseURL,
		Vision:           cloned.Model.Vision,
		MaxTokens:        cloned.Model.MaxTokens,
		Temperature:      cloned.Model.Temperature,
		TopP:             cloned.Model.TopP,
		FrequencyPenalty: cloned.Model.FrequencyPenalty,
		PresencePenalty:  cloned.Model.PresencePenalty,
		ContextLimit:     cloned.Model.ContextLimit,
		OutputLimit:      cloned.Model.OutputLimit,
	}

	messages := make([]agent.Message, 0, len(cloned.Conversation.Messages))
	for _, message := range cloned.Conversation.Messages {
		messages = append(messages, agent.Message{Role: message.Role, Content: message.Content})
	}
	var runtimeTools []agent.Tool
	if p.toolProvider != nil {
		runtimeTools, err = p.toolProvider.ToolsFor(cloned, workspace)
		if err != nil {
			return nil, cleanup, fmt.Errorf("prepare runtime tools: %w", err)
		}
	}
	var mcpServers []agent.MCPServerConfig
	if strings.EqualFold(strings.TrimSpace(cloned.Runtime.Kind), agent.RuntimeKindOpenCode) {
		mcpServers = prepareMCPServers(ctx, cloned.Plugins)
	}
	connectorEnv := prepareConnectorRuntimeEnv(ctx, cloned.Plugins)
	runtimeEnv := append([]string(nil), connectorEnv...)
	if workspace.SkillDir != "" {
		runtimeEnv = append(runtimeEnv, agent.RunSkillsDirEnvVar+"="+workspace.SkillDir)
	}
	sort.Strings(runtimeEnv)

	return &PreparedRun{
		Request: req,
		Execution: agent.ExecutionRequest{
			ExecutionID:  cloned.RunID,
			TraceID:      cloned.TraceID,
			Runtime:      strings.TrimSpace(cloned.Runtime.Kind),
			SessionKey:   cloned.Conversation.ID,
			InstanceKey:  cloned.Assistant.PublicID,
			Mode:         agent.ExecutionMode(cloned.ExecutionMode),
			SystemPrompt: systemPrompt,
			Prompt:       prompt,
			Messages:     messages,
			Attachments:  attachments,
			Tools:        runtimeTools,
			MCPServers:   mcpServers,
			ExtraEnv:     runtimeEnv,
			Model:        model,
			Policy: agent.ExecutionPolicy{
				AllowedTools:   append([]string(nil), cloned.Capability.AllowedTools...),
				PermissionMode: cloned.Policy.PermissionMode,
			},
			ProviderSession: providerSession,
			Filesystem: agent.FilesystemContext{
				WorkDir:      workspace.WorkDir,
				RepoDir:      workspace.RepoDir,
				TaskDir:      workspace.TaskDir,
				SkillDir:     workspace.SkillDir,
				UploadRelDir: consts.RepoDirUploads,
			},
		},
		Workspace: workspace,
	}, cleanup, nil
}

func prepareMCPServers(
	ctx context.Context,
	snapshots []agentrundomain.PluginSnapshot,
) []agent.MCPServerConfig {
	configs := make([]agent.MCPServerConfig, 0, len(snapshots))
	for _, snapshot := range snapshots {
		if !strings.EqualFold(strings.TrimSpace(snapshot.Kind), "mcp") {
			continue
		}
		if connector, err := service.ConnectorFromDefinition(snapshot.Definition); err == nil &&
			connector != nil && connector.MCP == nil {
			continue
		}
		config, err := MCPServerConfigFromPluginSnapshot(snapshot)
		if err != nil {
			logs.WarnContextf(
				ctx,
				"skip invalid MCP plugin snapshot: plugin_id=%s code=%s revision=%d error=%v",
				snapshot.PluginID,
				snapshot.Code,
				snapshot.Revision,
				err,
			)
			continue
		}
		configs = append(configs, config)
	}
	sort.Slice(configs, func(i, j int) bool {
		return strings.ToLower(configs[i].Name) < strings.ToLower(configs[j].Name)
	})
	return configs
}

func chainCleanup(first, second func()) func() {
	if first == nil {
		first = func() {}
	}
	if second == nil {
		return first
	}
	return func() {
		second()
		first()
	}
}

// validateModelConfig validates the required model fields.
func validateModelConfig(req *agentrundomain.RunRequest) error {
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
// It returns the proxy model key used for later cleanup.
func (p *preparer) resolveModelRouting(req *agentrundomain.RunRequest) string {
	realModelName := strings.TrimSpace(req.Model.Model)
	upstreamCfg := modelrouter.UpstreamConfig{
		ModelID:      req.Model.ModelID,
		ModelName:    realModelName,
		Provider:     strings.TrimSpace(req.Model.Provider),
		BaseURL:      strings.TrimSpace(req.Model.BaseURL),
		BaseURLHasV1: req.Model.BaseURLHasV1,
		APIKey:       strings.TrimSpace(req.Model.APIKey),
		Temperature:  req.Model.Temperature,
	}
	var proxyModel string
	if p.modelStore != nil {
		proxyModel = realModelName + ":" + req.RunID
		upstreamCfg.ModelName = proxyModel
		p.modelStore.Put(upstreamCfg)
		p.modelStore.PutBiz(proxyModel, modelrouter.BusinessKeys{
			ProjectID:   req.BusinessKeys.ProjectPKID,
			SessionID:   req.BusinessKeys.SessionPKID,
			MessageID:   req.BusinessKeys.MessagePKID,
			AssistantID: req.BusinessKeys.AssistantID,
			Uin:         req.BusinessKeys.UinPK,
		})
		req.Model.Model = proxyModel
	}
	req.Model.BaseURL = modelrouter.ProxyBaseURL(identity.WorkerAddr())
	return proxyModel
}

func parseStrUint(s string) uint {
	v, _ := strconv.ParseUint(strings.TrimSpace(s), 10, 64)
	return uint(v)
}

func (p *preparer) prepareWorkspace(ctx context.Context, req *agentrundomain.RunRequest) (WorkspacePreparation, error) {
	if p.workspaceMgr != nil {
		return p.workspaceMgr.PrepareWorkspace(ctx, workspaceRequest(req))
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

func workspaceRequest(req *agentrundomain.RunRequest) agentworkspace.TaskWorkspaceRequest {
	if req == nil {
		return agentworkspace.TaskWorkspaceRequest{}
	}
	return agentworkspace.TaskWorkspaceRequest{
		OrgID:            req.Workspace.OrgID,
		ProjectID:        strings.TrimSpace(req.Workspace.ProjectID),
		TaskID:           firstNonEmpty(req.Workspace.TaskID, req.TaskID),
		RequestID:        strings.TrimSpace(req.Workspace.RequestID),
		RequestedWorkDir: strings.TrimSpace(req.Runtime.WorkDir),
	}
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return strings.TrimSpace(value)
		}
	}
	return ""
}

func (p *preparer) resolveProviderSession(ctx context.Context, req *agentrundomain.RunRequest) agent.ProviderSession {
	sessionKey := strings.TrimSpace(req.Conversation.ID)
	provider := strings.TrimSpace(req.Runtime.Kind)
	if sessionKey == "" || provider == "" || p.sessionStore == nil {
		return agent.ProviderSession{}
	}
	binding, err := p.sessionStore.GetProviderSession(ctx, ProviderSessionKey{
		InternalSessionID: sessionKey,
		Provider:          provider,
	})
	if err != nil {
		logs.WarnContextf(ctx, "Resolve provider session failed: provider=%s session=%s error=%v", provider, sessionKey, err)
		return agent.ProviderSession{}
	}
	if binding != nil && strings.TrimSpace(binding.ProviderSessionID) != "" && binding.Status != "failed" {
		return agent.ProviderSession{
			ID:     strings.TrimSpace(binding.ProviderSessionID),
			Resume: true,
		}
	}
	return agent.ProviderSession{}
}
