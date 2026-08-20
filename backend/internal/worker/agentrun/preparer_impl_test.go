package agentrun

import (
	"bytes"
	"context"
	"encoding/json"
	"image"
	"image/png"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/insmtx/Leros/backend/agent"
	"github.com/insmtx/Leros/backend/internal/consts"
	modelrouter "github.com/insmtx/Leros/backend/internal/modelrouter"
	agentruncontext "github.com/insmtx/Leros/backend/internal/worker/agentrun/context"
	agentrundomain "github.com/insmtx/Leros/backend/internal/worker/agentrun/domain"
	agentworkspace "github.com/insmtx/Leros/backend/internal/workspace"
	"github.com/insmtx/Leros/backend/pkg/leros"
)

type workspaceManagerStub struct {
	preparation WorkspacePreparation
	seen        agentworkspace.TaskWorkspaceRequest
}

func (s *workspaceManagerStub) PrepareWorkspace(
	_ context.Context,
	req agentworkspace.TaskWorkspaceRequest,
) (WorkspacePreparation, error) {
	s.seen = req
	return s.preparation, nil
}

type sessionProviderStub struct {
	workDir string
}

func (s *sessionProviderStub) Prepare(_ context.Context, req *agentrundomain.RunRequest) error {
	s.workDir = req.Runtime.WorkDir
	req.Conversation.Messages = []agentrundomain.InputMessage{{Role: "assistant", Content: "history"}}
	return nil
}

func (*sessionProviderStub) CompleteClaimed(context.Context, *agentrundomain.RunRequest) error {
	return nil
}

type toolProviderStub struct {
	workspace WorkspacePreparation
}

type skillPreparerStub struct {
	dir string
}

func (s skillPreparerStub) PrepareSkills(
	context.Context,
	*agentrundomain.RunRequest,
	WorkspacePreparation,
) (string, func(), error) {
	return s.dir, func() {}, nil
}

func (s *toolProviderStub) ToolsFor(
	_ *agentrundomain.RunRequest,
	workspace WorkspacePreparation,
) ([]agent.Tool, error) {
	s.workspace = workspace
	return []agent.Tool{preparedTool{}}, nil
}

type preparedTool struct{}

func (preparedTool) Definition() agent.ToolDefinition {
	return agent.ToolDefinition{Name: "prepared_tool", Parameters: json.RawMessage(`{"type":"object"}`)}
}

func (preparedTool) Execute(context.Context, json.RawMessage) (agent.ToolResult, error) {
	return agent.ToolResult{Content: "ok"}, nil
}

func TestPreparerUsesOneWorkspaceSnapshotAndPreservesSkillPrompt(t *testing.T) {
	workspaceRoot := t.TempDir()
	t.Setenv(leros.EnvWorkspaceRoot, workspaceRoot)
	skillDir := filepath.Join(workspaceRoot, ".leros", "skills", "review")
	if err := os.MkdirAll(skillDir, 0o755); err != nil {
		t.Fatalf("mkdir skill: %v", err)
	}
	if err := os.WriteFile(filepath.Join(skillDir, "SKILL.md"), []byte(
		"---\nname: review\ndescription: review files\n---\nUse the prepared review workflow.\n",
	), 0o644); err != nil {
		t.Fatalf("write skill: %v", err)
	}

	workspace := WorkspacePreparation{
		WorkDir:              "/workspace/repo/src",
		RepoDir:              "/workspace/repo",
		TaskDir:              "/workspace/repo/.leros/tasks/task-1",
		ArtifactManifestPath: "/workspace/repo/.leros/tasks/task-1/turns/request-1/artifacts.jsonl",
	}
	workspaceManager := &workspaceManagerStub{preparation: workspace}
	sessionProvider := &sessionProviderStub{}
	toolProvider := &toolProviderStub{}
	builder := agentruncontext.NewContextBuilder(agentruncontext.ContextBuilder{
		SessionMessages: sessionProvider,
	})
	preparer := NewPreparerWithTools(
		builder,
		workspaceManager,
		nil,
		modelrouter.NewModelStore(),
		toolProvider,
	)
	request := &agentrundomain.RunRequest{
		RunID:         "run-1",
		TaskID:        "task-1",
		ExecutionMode: agentrundomain.ExecutionModePlan,
		Assistant: agentrundomain.AssistantContext{
			PublicID: "assistant-1",
		},
		Workspace: agentrundomain.WorkspaceContext{
			OrgID:     1,
			ProjectID: "project-1",
			TaskID:    "task-1",
			RequestID: "request-1",
		},
		Input: agentrundomain.InputContext{
			Type:     agentrundomain.InputTypeMessage,
			Messages: []agentrundomain.InputMessage{{Role: "user", Content: `<skill-chip data-code="review">review</skill-chip> inspect the change`}},
		},
		Model: agentrundomain.ModelOptions{
			Provider: "openai",
			Model:    "test-model",
			APIKey:   "test-key",
		},
	}

	prepared, _, err := preparer.Prepare(context.Background(), request)
	if err != nil {
		t.Fatalf("Prepare() error = %v", err)
	}
	if request.Runtime.WorkDir != "" || request.Input.Messages[0].Content != `<skill-chip data-code="review">review</skill-chip> inspect the change` {
		t.Fatalf("original request mutated: %#v", request)
	}
	if workspaceManager.seen.ProjectID != request.Workspace.ProjectID {
		t.Fatalf("workspace request = %#v", workspaceManager.seen)
	}
	if sessionProvider.workDir != workspace.WorkDir {
		t.Fatalf("session provider work dir = %q", sessionProvider.workDir)
	}
	if prepared.Workspace != workspace || toolProvider.workspace != workspace {
		t.Fatalf("workspace snapshots differ: prepared=%#v tools=%#v", prepared.Workspace, toolProvider.workspace)
	}
	if prepared.Execution.Filesystem.WorkDir != workspace.WorkDir ||
		prepared.Execution.Filesystem.RepoDir != workspace.RepoDir ||
		prepared.Execution.Filesystem.TaskDir != workspace.TaskDir {
		t.Fatalf("execution filesystem = %#v", prepared.Execution.Filesystem)
	}
	if prepared.Execution.Mode != agent.ExecutionModePlan {
		t.Fatalf("execution mode = %q, want %q", prepared.Execution.Mode, agent.ExecutionModePlan)
	}
	if len(prepared.Execution.Tools) != 1 || prepared.Execution.Tools[0].Definition().Name != "prepared_tool" {
		t.Fatalf("execution tools = %#v", prepared.Execution.Tools)
	}
	if len(prepared.Execution.Messages) != 1 || prepared.Execution.Messages[0].Content != "history" {
		t.Fatalf("execution messages = %#v", prepared.Execution.Messages)
	}
	if !strings.Contains(prepared.Execution.Prompt, "Use the prepared review workflow.") ||
		!strings.Contains(prepared.Execution.Prompt, "inspect the change") {
		t.Fatalf("prepared prompt lost skill rewrite: %s", prepared.Execution.Prompt)
	}
}

func TestPreparerInjectsTaskSkillDirectoryIntoRuntimeEnvironment(t *testing.T) {
	taskDir := filepath.Join(t.TempDir(), "task-1")
	skillDir := filepath.Join(taskDir, "skills")
	workspace := WorkspacePreparation{WorkDir: t.TempDir(), TaskDir: taskDir}
	preparer := NewPreparerWithSkillPreparer(
		agentruncontext.NewContextBuilder(agentruncontext.ContextBuilder{}),
		&workspaceManagerStub{preparation: workspace},
		nil,
		modelrouter.NewModelStore(),
		nil,
		skillPreparerStub{dir: skillDir},
	)
	request := &agentrundomain.RunRequest{
		RunID: "run-1",
		Input: agentrundomain.InputContext{
			Type:     agentrundomain.InputTypeMessage,
			Messages: []agentrundomain.InputMessage{{Role: "user", Content: "hello"}},
		},
		Model: agentrundomain.ModelOptions{Provider: "openai", Model: "test-model", APIKey: "test-key"},
	}

	prepared, cleanup, err := preparer.Prepare(context.Background(), request)
	if err != nil {
		t.Fatalf("Prepare() error = %v", err)
	}
	defer cleanup()
	if prepared.Workspace.SkillDir != skillDir || prepared.Execution.Filesystem.SkillDir != skillDir {
		t.Fatalf("Skill directories = workspace:%q execution:%q", prepared.Workspace.SkillDir, prepared.Execution.Filesystem.SkillDir)
	}
	want := agent.RunSkillsDirEnvVar + "=" + skillDir
	if len(prepared.Execution.ExtraEnv) != 1 || prepared.Execution.ExtraEnv[0] != want {
		t.Fatalf("Execution ExtraEnv = %#v, want %q", prepared.Execution.ExtraEnv, want)
	}
}

func TestPreparerDoesNotInjectPerRunUserIDIntoRuntimeEnvironment(t *testing.T) {
	workspace := WorkspacePreparation{WorkDir: t.TempDir()}
	preparer := NewPreparerWithSkillPreparer(
		agentruncontext.NewContextBuilder(agentruncontext.ContextBuilder{}),
		&workspaceManagerStub{preparation: workspace},
		nil,
		modelrouter.NewModelStore(),
		nil,
		skillPreparerStub{},
	)
	request := &agentrundomain.RunRequest{
		RunID: "run-per-run-user-id",
		Input: agentrundomain.InputContext{
			Type:     agentrundomain.InputTypeMessage,
			Messages: []agentrundomain.InputMessage{{Role: "user", Content: "hello"}},
		},
		Model:        agentrundomain.ModelOptions{Provider: "openai", Model: "test-model", APIKey: "test-key"},
		BusinessKeys: agentrundomain.BusinessKeys{UinPK: 42},
	}

	prepared, cleanup, err := preparer.Prepare(context.Background(), request)
	if err != nil {
		t.Fatalf("Prepare() error = %v", err)
	}
	defer cleanup()
	if len(prepared.Execution.ExtraEnv) != 0 {
		t.Fatalf("Execution ExtraEnv = %#v, want no per-run user ID environment", prepared.Execution.ExtraEnv)
	}
}

func TestPreparerResolvesProviderSessionForRuntimeResume(t *testing.T) {
	workspace := WorkspacePreparation{WorkDir: "/workspace/repo"}
	sessionStore := &providerSessionRecorder{
		resume: &ProviderSessionBinding{
			InternalSessionID: "conversation-1",
			Provider:          "opencode",
			ProviderSessionID: "provider-session-1",
			Status:            "active",
		},
	}
	builder := agentruncontext.NewContextBuilder(agentruncontext.ContextBuilder{})
	preparer := NewPreparerWithSessionStore(
		builder,
		&workspaceManagerStub{preparation: workspace},
		nil,
		modelrouter.NewModelStore(),
		nil,
		sessionStore,
	)
	request := &agentrundomain.RunRequest{
		RunID: "run-1",
		Runtime: agentrundomain.RuntimeOptions{
			Kind: "opencode",
		},
		Conversation: agentrundomain.ConversationContext{
			ID: "conversation-1",
		},
		Input: agentrundomain.InputContext{
			Type:     agentrundomain.InputTypeMessage,
			Messages: []agentrundomain.InputMessage{{Role: "user", Content: "continue"}},
		},
		Model: agentrundomain.ModelOptions{
			Provider: "openai",
			Model:    "test-model",
			APIKey:   "test-key",
		},
	}

	prepared, _, err := preparer.Prepare(context.Background(), request)
	if err != nil {
		t.Fatalf("Prepare() error = %v", err)
	}
	if sessionStore.getKey.InternalSessionID != "conversation-1" ||
		sessionStore.getKey.Provider != "opencode" {
		t.Fatalf("provider session lookup key = %#v", sessionStore.getKey)
	}
	if prepared.Execution.ProviderSession.ID != "provider-session-1" ||
		!prepared.Execution.ProviderSession.Resume {
		t.Fatalf("execution provider session = %#v", prepared.Execution.ProviderSession)
	}
	if prepared.Execution.Model.APIKey != "test-key" {
		t.Fatalf("execution API key = %q, want test-key", prepared.Execution.Model.APIKey)
	}
	if sessionStore.binding != nil {
		t.Fatalf("preparer should not persist provider session, got %#v", sessionStore.binding)
	}
}

func TestMultimodalAttachmentsForRuntimeDownloadsAndSkipsOthers(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte{0x89, 0x50, 0x4E, 0x47})
	}))
	defer srv.Close()

	attachments := multimodalAttachmentsForRuntime(context.Background(), []agentrundomain.Attachment{
		{Name: "cat.png", MimeType: "image/png", URL: srv.URL},
		{Name: "report.pdf", MimeType: "application/pdf", URL: srv.URL},
		{Name: "audio.mp3", MimeType: "audio/mpeg", URL: srv.URL},
		{Name: "video.mp4", MimeType: "video/mp4", URL: srv.URL},
		{Name: "note.txt", MimeType: "text/plain", URL: srv.URL},
		{Name: "bad.png", MimeType: "image/png", URL: "http://127.0.0.1:1/unreachable"},
		{Name: "none.png", MimeType: "image/png", URL: ""},
	})

	if len(attachments) != 1 {
		t.Fatalf("attachments = %#v, want 1 downloadable multimodal (image only)", attachments)
	}
	for _, got := range attachments {
		if got.Name == "" || strings.HasPrefix(got.Name, consts.RepoDirUploads) {
			t.Fatalf("Name = %q, want plain filename without uploads prefix", got.Name)
		}
		if string(got.Data) != string([]byte{0x89, 0x50, 0x4E, 0x47}) {
			t.Fatalf("attachment data = %v, want image bytes", got.Data)
		}
	}
}

func TestMultimodalAttachmentsForRuntimeSkipsOversizedInline(t *testing.T) {
	orig := maxMultimodalInlineBytes
	maxMultimodalInlineBytes = 4
	t.Cleanup(func() { maxMultimodalInlineBytes = orig })

	payload := []byte{0x89, 0x50, 0x4E, 0x47, 0x89, 0x50, 0x4E, 0x47}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write(payload)
	}))
	defer srv.Close()

	attachments := multimodalAttachmentsForRuntime(context.Background(), []agentrundomain.Attachment{
		{Name: "huge.png", MimeType: "image/png", URL: srv.URL},
	})
	if len(attachments) != 1 {
		t.Fatalf("attachments = %#v, want the oversized image still surfaced (Data empty)", attachments)
	}
	got := attachments[0]
	if got.Name != "huge.png" {
		t.Fatalf("Name = %q, want huge.png", got.Name)
	}
	if len(got.Data) != 0 {
		t.Fatalf("oversized inline should have empty Data, got %d bytes", len(got.Data))
	}
}

func TestDownscaleMultimodalImageScalesOversizedAndKeepsSmall(t *testing.T) {
	// 构造 2048x2048 的 PNG（对应线上触发放大图重编码缺陷的图片）。
	img := image.NewRGBA(image.Rect(0, 0, 2048, 2048))
	var buf bytes.Buffer
	if err := (&png.Encoder{}).Encode(&buf, img); err != nil {
		t.Fatalf("encode png: %v", err)
	}
	bigBytes := buf.Bytes()

	// 大图应当被缩放到 maxMultimodalSide 以内，并统一重编码为 JPEG。
	resized, mime, err := downscaleMultimodalImage(bigBytes, "image/jpeg")
	if err != nil {
		t.Fatalf("downscaleMultimodalImage: %v", err)
	}
	if resized == nil {
		t.Fatal("expected resized image for oversize input, got nil")
	}
	if mime != "image/jpeg" {
		t.Fatalf("mime = %q, want image/jpeg", mime)
	}
	got, _, err := image.Decode(bytes.NewReader(resized))
	if err != nil {
		t.Fatalf("decode resized image: %v", err)
	}
	b := got.Bounds()
	if b.Dx() > maxMultimodalSide || b.Dy() > maxMultimodalSide {
		t.Fatalf("resized %dx%d exceeds max side %d", b.Dx(), b.Dy(), maxMultimodalSide)
	}

	// 小图（132x132）无需缩放，返回 nil，保持原 MIME。
	small := image.NewRGBA(image.Rect(0, 0, 132, 132))
	var sbuf bytes.Buffer
	if err := (&png.Encoder{}).Encode(&sbuf, small); err != nil {
		t.Fatalf("encode small png: %v", err)
	}
	resizedSmall, mimeSmall, err := downscaleMultimodalImage(sbuf.Bytes(), "image/jpeg")
	if err != nil {
		t.Fatalf("downscaleMultimodalImage small: %v", err)
	}
	if resizedSmall != nil {
		t.Fatalf("expected nil (no resize) for small image, got %d bytes", len(resizedSmall))
	}
	if mimeSmall != "image/jpeg" {
		t.Fatalf("small mime = %q, want image/jpeg", mimeSmall)
	}

	// 非法字节解码失败时返回错误且不 panic。
	if _, _, err := downscaleMultimodalImage([]byte("not-an-image"), "image/jpeg"); err == nil {
		t.Fatal("expected error for invalid image bytes")
	}
}

// TestDownscaleMultimodalImageReencodesByteOversized 验证字节维度兜底：
// 像素尺寸未超过 maxMultimodalSide 但 base64 超过 maxMultimodalBase64Bytes
// 的图片（高熵噪声 PNG），也必须被重编码为 JPEG 压到字节阈值内。
// 这正是缩放阈值(1600)与 opencode 字节阈值(5MiB)之间的盲区，修复前会原样内联、
// 触发 opencode 内部重编码路径导致 SSE 不推送文本。
func TestDownscaleMultimodalImageReencodesByteOversized(t *testing.T) {
	// 构造 1400x1400（长边 ≤1600）的高熵随机噪声 PNG，PNG 对噪声压缩率低，
	// base64 必然远超 5MiB，复现"尺寸小但字节超限"的盲区。
	side := 1400
	img := image.NewRGBA(image.Rect(0, 0, side, side))
	for i := 0; i < side*side; i++ {
		img.Pix[i*4] = byte(i * 7919 % 256)
		img.Pix[i*4+1] = byte(i * 104729 % 256)
		img.Pix[i*4+2] = byte(i * 15485863 % 256)
		img.Pix[i*4+3] = 255
	}
	var buf bytes.Buffer
	if err := (&png.Encoder{CompressionLevel: png.NoCompression}).Encode(&buf, img); err != nil {
		t.Fatalf("encode noisy png: %v", err)
	}
	raw := buf.Bytes()
	if base64Len(raw) <= maxMultimodalBase64Bytes {
		t.Fatalf("precondition failed: noisy PNG base64 length %d should exceed %d", base64Len(raw), maxMultimodalBase64Bytes)
	}

	resized, mime, err := downscaleMultimodalImage(raw, "image/png")
	if err != nil {
		t.Fatalf("downscaleMultimodalImage: %v", err)
	}
	if resized == nil {
		t.Fatal("expected re-encoded image for byte-oversized input, got nil (would leak into opencode reencode path)")
	}
	if mime != "image/jpeg" {
		t.Fatalf("mime = %q, want image/jpeg", mime)
	}
	if base64Len(resized) > maxMultimodalBase64Bytes {
		t.Fatalf("re-encoded base64 length %d should be <= %d", base64Len(resized), maxMultimodalBase64Bytes)
	}
	got, _, err := image.Decode(bytes.NewReader(resized))
	if err != nil {
		t.Fatalf("decode resized image: %v", err)
	}
	gb := got.Bounds()
	if gb.Dx() != side || gb.Dy() != side {
		t.Fatalf("byte-oversized but pixel-fine image should keep dimensions, got %dx%d want %dx%d", gb.Dx(), gb.Dy(), side, side)
	}
}
