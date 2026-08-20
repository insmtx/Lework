package service

import (
	"context"
	"encoding/json"
	"strconv"
	"strings"
	"testing"
	"time"

	"gorm.io/gorm"

	"github.com/insmtx/Leros/backend/internal/adapter/account"
	"github.com/insmtx/Leros/backend/internal/api/auth"
	infradb "github.com/insmtx/Leros/backend/internal/infra/db"
	"github.com/insmtx/Leros/backend/pkg/messaging"
	"github.com/insmtx/Leros/backend/types"
)

func loadQueuedWorkerCommand(t *testing.T, database *gorm.DB, messageID uint) messaging.WorkerCommand {
	t.Helper()
	task, err := infradb.GetReliableTaskBySource(
		context.Background(),
		database,
		"session_message",
		strconv.FormatUint(uint64(messageID), 10),
	)
	if err != nil {
		t.Fatalf("get queued worker task: %v", err)
	}
	if task == nil {
		t.Fatal("queued worker task not found")
	}
	var command messaging.WorkerCommand
	if err := json.Unmarshal(task.Payload, &command); err != nil {
		t.Fatalf("decode queued worker command: %v", err)
	}
	return command
}

func TestMessagePosterPostMessageFillsSenderNameFromUserOrgUin(t *testing.T) {
	database := setupTestDB(t)
	// PostMessage→publishWorkerTask 会按项目解析插件快照，需补齐插件相关表。
	if err := database.AutoMigrate(
		&types.ProjectPluginBinding{},
		&types.Plugin{},
		&types.PluginRevision{},
	); err != nil {
		t.Fatalf("migrate plugin tables: %v", err)
	}
	userOrg := &types.UserOrg{
		UserID: 1,
		OrgID:  2,
	}
	if err := database.Create(userOrg).Error; err != nil {
		t.Fatalf("seed second user org: %v", err)
	}
	if err := database.Create(&types.LLMModel{
		OrgID:           2,
		Code:            "default-org2",
		Name:            "Default",
		Provider:        "openai",
		ModelName:       "gpt-test",
		BaseURL:         "https://api.openai.com",
		BaseURLHasV1:    true,
		APIKeyEncrypted: "sk-test",
		Status:          string(types.LLMModelStatusActive),
		IsDefault:       true,
	}).Error; err != nil {
		t.Fatalf("seed org 2 default llm model: %v", err)
	}

	ctx := auth.WithContext(context.Background(), &types.Caller{
		Uin:   100,
		OrgID: 2,
		State: types.AuthStateSucc,
	}, &types.Trace{
		RequestID: "test-request-id",
		TraceID:   "test-trace-id",
	})
	poster := NewMessagePoster(database, newTestPermissionService(database), &recordingEventBus{}, &mockInferrer{assistantID: 1}, nil, nil, "test", nil, newTestOrgRepoForSender("Test User"))
	assistant := seedReadyAssistant(t, database, "sender-name", "Sender Name Assistant", "answer")
	project := &types.Project{
		PublicID: "prj_sender_name",
		OrgID:    2,
		OwnerID:  100,
		Name:     "Sender Name Project",
		Status:   string(types.ProjectStatusActive),
	}
	if err := database.Create(project).Error; err != nil {
		t.Fatalf("create project: %v", err)
	}
	task := &types.Task{
		PublicID:  "task_sender_name",
		OrgID:     2,
		OwnerID:   100,
		ProjectID: project.ID,
		Title:     "Sender Name Task",
		Status:    string(types.TaskStatusCreated),
	}
	if err := database.Create(task).Error; err != nil {
		t.Fatalf("create task: %v", err)
	}
	session := &types.Session{
		PublicID:  "sess_sender_name",
		Type:      types.SessionTypeTask,
		Uin:       100,
		OrgID:     2,
		ProjectID: &project.ID,
		TaskID:    &task.ID,
		Status:    string(types.SessionStatusActive),
		Title:     "Sender Name Session",
	}
	if err := database.Create(session).Error; err != nil {
		t.Fatalf("create session: %v", err)
	}

	message, err := poster.PostMessage(ctx, session, "", func(sequence int64) *types.SessionMessage {
		return &types.SessionMessage{
			Role:        string(types.MessageRoleUser),
			Content:     "hello from switched org",
			MessageType: string(types.MessageTypeText),
			Status:      string(types.MessageStatusPending),
			Sequence:    sequence,
			Timestamp:   time.Now().UnixMilli(),
		}
	}, &MessageRoutingOverride{AssistantID: assistant.ID, WorkerID: assistant.ID})
	if err != nil {
		t.Fatalf("PostMessage failed: %v", err)
	}
	if message.SenderUin == nil || *message.SenderUin != 100 {
		t.Fatalf("sender_uin = %#v, want 100", message.SenderUin)
	}
	if message.SenderName != "Test User" {
		t.Fatalf("sender_name = %q, want Test User", message.SenderName)
	}
}

func TestMessagePosterPublishWorkerTaskInjectsAssistantPersona(t *testing.T) {
	database := setupTestDB(t)
	ctx := setupTestContextWithCaller(t)
	recorder := &recordingEventBus{}
	poster := NewMessagePoster(database, newTestPermissionService(database), recorder, &mockInferrer{assistantID: 1}, nil, nil, "test", nil, nil)

	assistant := seedReadyAssistant(t, database, "bid-strategist", "投标策略师", "按投标策略师身份回答")
	project := &types.Project{
		PublicID: "prj_persona",
		OrgID:    1,
		OwnerID:  1,
		Name:     "Persona Project",
		Status:   string(types.ProjectStatusActive),
	}
	if err := database.Create(project).Error; err != nil {
		t.Fatalf("create project: %v", err)
	}
	task := &types.Task{
		PublicID:  "task_persona",
		OrgID:     1,
		OwnerID:   1,
		ProjectID: project.ID,
		Title:     "Persona Task",
		Status:    string(types.TaskStatusCreated),
	}
	if err := database.Create(task).Error; err != nil {
		t.Fatalf("create task: %v", err)
	}
	session := &types.Session{
		PublicID:             "sess_persona",
		Type:                 types.SessionTypeTask,
		Uin:                  1,
		OrgID:                1,
		AssistantID:          assistant.ID,
		AllocatedAssistantID: assistant.ID + 10000,
		ProjectID:            &project.ID,
		TaskID:               &task.ID,
		Status:               string(types.SessionStatusActive),
		Title:                "Persona Session",
	}
	if err := database.Create(session).Error; err != nil {
		t.Fatalf("create session: %v", err)
	}

	message, err := poster.PostMessage(ctx, session, "", func(sequence int64) *types.SessionMessage {
		return &types.SessionMessage{
			Role:        string(types.MessageRoleUser),
			Content:     "帮我检查投标风险",
			MessageType: string(types.MessageTypeText),
			Status:      string(types.MessageStatusPending),
			Sequence:    sequence,
			Timestamp:   time.Now().UnixMilli(),
		}
	}, &MessageRoutingOverride{AssistantID: assistant.ID, WorkerID: assistant.ID})
	if err != nil {
		t.Fatalf("PostMessage failed: %v", err)
	}

	cmd := loadQueuedWorkerCommand(t, database, message.ID)
	if cmd.Trace.ReqID != "test-request-id" {
		t.Fatalf("command req_id = %q, want test-request-id", cmd.Trace.ReqID)
	}
	payload, err := messaging.DecodeCommandPayload[messaging.RunCommandPayload](&cmd.Body)
	if err != nil {
		t.Fatalf("decode run command: %v", err)
	}
	if payload.Execution.AssistantPublicID != "assistant-bid-strategist" {
		t.Fatalf("execution assistant id = %q, want assistant-bid-strategist", payload.Execution.AssistantPublicID)
	}
	if payload.Execution.AssistantName != assistant.Name {
		t.Fatalf("execution assistant name = %q, want %q", payload.Execution.AssistantName, assistant.Name)
	}
	if payload.Execution.SystemPrompt != assistant.SystemPrompt {
		t.Fatalf("execution system prompt = %q, want %q", payload.Execution.SystemPrompt, assistant.SystemPrompt)
	}
}

func TestMessagePosterPublishWorkerTaskInjectsAssistantEvolutionContext(t *testing.T) {
	database := setupTestDB(t)
	ctx := setupTestContextWithCaller(t)
	recorder := &recordingEventBus{}
	poster := NewMessagePoster(database, newTestPermissionService(database), recorder, nil, nil, nil, "test", nil, nil)

	assistant := seedReadyAssistant(t, database, "contract-review", "合同审查专家", "只做合同风险审查。")
	block := &types.DigitalAssistantPromptBlock{
		AssistantID: assistant.ID,
		BlockType:   "boundary",
		Title:       "合同红线",
		Content:     "必须提示用户重要合同请律师终审。",
		Priority:    100,
		Enabled:     true,
		Version:     1,
	}
	if err := database.Create(block).Error; err != nil {
		t.Fatalf("create prompt block: %v", err)
	}
	memory := &types.DigitalAssistantMemory{
		AssistantID: assistant.ID,
		MemoryType:  "experience",
		Content:     "用户常关注违约责任、付款节点和验收标准。",
		SourceType:  "manual",
		Confidence:  0.95,
		Enabled:     true,
	}
	if err := database.Create(memory).Error; err != nil {
		t.Fatalf("create memory: %v", err)
	}

	project := &types.Project{
		PublicID: "prj_persona_evolution",
		OrgID:    1,
		OwnerID:  1,
		Name:     "Persona Evolution",
		Status:   string(types.ProjectStatusActive),
	}
	if err := database.Create(project).Error; err != nil {
		t.Fatalf("create project: %v", err)
	}
	task := &types.Task{
		PublicID:  "task_persona_evolution",
		OrgID:     1,
		OwnerID:   1,
		ProjectID: project.ID,
		Title:     "Persona Evolution Task",
		Status:    string(types.TaskStatusCreated),
	}
	if err := database.Create(task).Error; err != nil {
		t.Fatalf("create task: %v", err)
	}
	session := &types.Session{
		PublicID:             "sess_persona_evolution",
		Type:                 types.SessionTypeTask,
		Uin:                  1,
		OrgID:                1,
		AssistantID:          assistant.ID,
		AllocatedAssistantID: assistant.ID + 10000,
		ProjectID:            &project.ID,
		TaskID:               &task.ID,
		Status:               string(types.SessionStatusActive),
		Title:                "Persona Evolution Session",
	}
	if err := database.Create(session).Error; err != nil {
		t.Fatalf("create session: %v", err)
	}

	message, err := poster.PostMessage(ctx, session, "", func(sequence int64) *types.SessionMessage {
		return &types.SessionMessage{
			Role:        string(types.MessageRoleUser),
			Content:     "帮我审查合同风险",
			MessageType: string(types.MessageTypeText),
			Status:      string(types.MessageStatusPending),
			Sequence:    sequence,
			Timestamp:   time.Now().UnixMilli(),
		}
	}, &MessageRoutingOverride{AssistantID: assistant.ID, WorkerID: assistant.ID})
	if err != nil {
		t.Fatalf("PostMessage failed: %v", err)
	}

	cmd := loadQueuedWorkerCommand(t, database, message.ID)
	payload, err := messaging.DecodeCommandPayload[messaging.RunCommandPayload](&cmd.Body)
	if err != nil {
		t.Fatalf("decode run command: %v", err)
	}
	if !strings.Contains(payload.Execution.SystemPrompt, "<teammate_evolution_context>") {
		t.Fatalf("system prompt missing evolution context: %q", payload.Execution.SystemPrompt)
	}
	if !strings.Contains(payload.Execution.SystemPrompt, block.Content) {
		t.Fatalf("system prompt missing prompt block content: %q", payload.Execution.SystemPrompt)
	}
	if !strings.Contains(payload.Execution.SystemPrompt, memory.Content) {
		t.Fatalf("system prompt missing memory content: %q", payload.Execution.SystemPrompt)
	}

	var trace types.AssistantPromptTrace
	if err := database.Where("session_id = ? AND assistant_id = ?", session.ID, assistant.ID).First(&trace).Error; err != nil {
		t.Fatalf("load prompt trace: %v", err)
	}
	if len(trace.InjectedBlockIDs) != 1 || trace.InjectedBlockIDs[0] != "1" {
		t.Fatalf("trace block ids = %#v, want [1]", trace.InjectedBlockIDs)
	}
	if len(trace.InjectedMemoryIDs) != 1 || trace.InjectedMemoryIDs[0] != "1" {
		t.Fatalf("trace memory ids = %#v, want [1]", trace.InjectedMemoryIDs)
	}
}

func TestResolveSkillMarketplaceScopesPluginByOrganization(t *testing.T) {
	database := setupTestDB(t)
	if err := database.AutoMigrate(&types.Plugin{}); err != nil {
		t.Fatalf("migrate plugins: %v", err)
	}
	for _, plugin := range []types.Plugin{
		{PublicID: "plugin_other", OrgID: 2, Code: "review", Kind: "skill", Name: "Other", Status: types.PluginStatusActive, Origin: "org", CreatedBy: 1, UpdatedBy: 1},
		{PublicID: "plugin_current", OrgID: 1, Code: "review", Kind: "skill", Name: "Current", Status: types.PluginStatusActive, Origin: "org", CreatedBy: 1, UpdatedBy: 1},
	} {
		if err := database.Create(&plugin).Error; err != nil {
			t.Fatalf("create plugin: %v", err)
		}
	}
	poster := NewMessagePoster(database, newTestPermissionService(database), &recordingEventBus{}, &mockInferrer{}, nil, nil, "test", nil, nil)
	source, skillID, resourceID := poster.resolveSkillMarketplace(context.Background(), 1, "review")
	if source != "organization" || skillID != "review" || resourceID != "plugin_current" {
		t.Fatalf("resolved Skill = (%q, %q, %q)", source, skillID, resourceID)
	}
}

func TestWriteSkillInvokeResourcesDoesNotMutateProjectMetadata(t *testing.T) {
	database := setupTestDB(t)
	if err := database.AutoMigrate(&types.Plugin{}, &types.MessageResource{}); err != nil {
		t.Fatalf("migrate Skill invocation models: %v", err)
	}
	project := &types.Project{
		PublicID: "project_skill_invoke",
		OrgID:    1,
		OwnerID:  1,
		Name:     "Skill Invoke",
		Status:   string(types.ProjectStatusActive),
		Metadata: types.ObjectMetadata{Extra: map[string]interface{}{"note": "keep"}},
	}
	if err := database.Create(project).Error; err != nil {
		t.Fatalf("create project: %v", err)
	}
	session := &types.Session{
		PublicID:  "session_skill_invoke",
		OrgID:     1,
		Uin:       1,
		ProjectID: &project.ID,
		Status:    string(types.SessionStatusActive),
	}
	if err := database.Create(session).Error; err != nil {
		t.Fatalf("create session: %v", err)
	}
	message := &types.SessionMessage{
		SessionID:   session.ID,
		Role:        string(types.MessageRoleUser),
		Content:     `<skill-chip data-code="review">review</skill-chip> 请检查`,
		MessageType: string(types.MessageTypeText),
		Status:      string(types.MessageStatusPending),
	}
	if err := database.Create(message).Error; err != nil {
		t.Fatalf("create message: %v", err)
	}
	plugin := &types.Plugin{
		PublicID:  "plugin_review",
		OrgID:     1,
		Code:      "review",
		Kind:      "skill",
		Name:      "Review",
		Status:    types.PluginStatusActive,
		Origin:    "org",
		CreatedBy: 1,
		UpdatedBy: 1,
	}
	if err := database.Create(plugin).Error; err != nil {
		t.Fatalf("create plugin: %v", err)
	}

	poster := NewMessagePoster(database, newTestPermissionService(database), &recordingEventBus{}, &mockInferrer{}, nil, nil, "test", nil, nil)
	poster.writeSkillInvokeResources(context.Background(), session, message)

	var resource types.MessageResource
	if err := database.First(&resource).Error; err != nil {
		t.Fatalf("load message resource: %v", err)
	}
	if resource.ResourceID != plugin.PublicID {
		t.Fatalf("resource ID = %q, want %q", resource.ResourceID, plugin.PublicID)
	}
	var refreshed types.Project
	if err := database.First(&refreshed, project.ID).Error; err != nil {
		t.Fatalf("reload project: %v", err)
	}
	if _, exists := refreshed.Metadata.Extra["skills"]; exists {
		t.Fatalf("project metadata unexpectedly contains skills: %#v", refreshed.Metadata.Extra)
	}
}

// TestPublishWorkerTaskHistoryContextUsesAssistantIDNotWorkerID 验证群聊历史注入时
// buildWorkerTask 用 session.AssistantID（DigitalAssistant.ID）而非
// session.AllocatedAssistantID（WorkerID）作为 GetLastAssistantMessageCreatedAt 过滤条件。
//
// 回归 Bug：修复前 main 分支 message_poster.go:776 把 session.AllocatedAssistantID
// 当 AssistantID 传入查询，由于 AI 回复消息写入时 SessionMessage.AssistantID 是
// DigitalAssistant.ID（PK），SQL WHERE assistant_id=<WorkerID> 永远查不到记录，
// 导致每次都从 session 创建时间增量取所有消息，把整个会话历史塞入 LLM 上下文，
// 触发"任务串线"问题。
func TestPublishWorkerTaskHistoryContextUsesAssistantIDNotWorkerID(t *testing.T) {
	database := setupTestDB(t)
	ctx := setupTestContextWithCaller(t)
	recorder := &recordingEventBus{}
	poster := NewMessagePoster(database, newTestPermissionService(database), recorder, &mockInferrer{assistantID: 1}, nil, nil, "test", nil, nil)

	assistant := seedReadyAssistant(t, database, "code-reviewer", "代码审查员", "按代码审查员身份回答")

	proj := &types.Project{PublicID: "prj_hist2", OrgID: 1, OwnerID: 1, Name: "HistProject2", Status: string(types.ProjectStatusActive)}
	if err := database.Create(proj).Error; err != nil {
		t.Fatalf("create project: %v", err)
	}

	session := &types.Session{
		PublicID:             "sess_hist2",
		Type:                 types.SessionTypeTask,
		Uin:                  1,
		OrgID:                1,
		AssistantID:          assistant.ID,
		AllocatedAssistantID: assistant.ID + 10000,
		ProjectID:            &proj.ID,
		Status:               string(types.SessionStatusActive),
		Title:                "history test 2",
	}
	if err := database.Create(session).Error; err != nil {
		t.Fatalf("create session: %v", err)
	}

	histUser := &types.SessionMessage{
		SessionID:   session.ID,
		Role:        string(types.MessageRoleUser),
		Content:     "历史用户提问",
		MessageType: string(types.MessageTypeText),
		Status:      string(types.MessageStatusCompleted),
		Sequence:    1,
		SenderUin:   uintPtr(7),
		SenderName:  "张三",
		Timestamp:   time.Now().UnixMilli(),
	}
	if err := database.Create(histUser).Error; err != nil {
		t.Fatalf("create history user message: %v", err)
	}

	histAssistant := &types.SessionMessage{
		SessionID:   session.ID,
		Role:        string(types.MessageRoleAssistant),
		Content:     "历史AI回复",
		MessageType: string(types.MessageTypeText),
		Status:      string(types.MessageStatusCompleted),
		Sequence:    2,
		SenderName:  "AI助手",
		Timestamp:   time.Now().UnixMilli(),
		AssistantID: assistant.ID,
	}
	if err := database.Create(histAssistant).Error; err != nil {
		t.Fatalf("create history assistant message: %v", err)
	}

	message := &types.SessionMessage{
		SessionID:   session.ID,
		Role:        string(types.MessageRoleUser),
		Content:     "这是当前消息",
		MessageType: string(types.MessageTypeText),
		Status:      string(types.MessageStatusPending),
		Sequence:    3,
		SenderUin:   uintPtr(8),
		SenderName:  "李四",
		Timestamp:   time.Now().UnixMilli(),
	}
	if err := database.Create(message).Error; err != nil {
		t.Fatalf("create current message: %v", err)
	}

	_, cmd, err := poster.buildWorkerTask(ctx, session, message, types.ExecutionModeDefault, &MessageRoutingOverride{AssistantID: assistant.ID, WorkerID: assistant.ID})
	if err != nil {
		t.Fatalf("buildWorkerTask failed: %v", err)
	}

	if cmd.Route.AssistantPublicID != assistant.PublicID {
		t.Errorf("cmd.Route.AssistantPublicID = %s, want %s (assistant.PublicID)",
			cmd.Route.AssistantPublicID, assistant.PublicID)
	}

	payload, err := messaging.DecodeCommandPayload[messaging.RunCommandPayload](&cmd.Body)
	if err != nil {
		t.Fatalf("decode payload: %v", err)
	}

	messages := payload.Input.Messages
	if len(messages) != 1 {
		t.Fatalf("expected 1 input message (current only, windowStart aligned to histAssistant), got %d: %+v", len(messages), messages)
	}
	if messages[0].Content != "这是当前消息" {
		t.Errorf("current content = %q, want %q", messages[0].Content, "这是当前消息")
	}
	if messages[0].SenderUserID == nil || *messages[0].SenderUserID != 8 || messages[0].SenderName != "李四" {
		t.Errorf("current sender = %#v, want user ID 8 / 李四", messages[0])
	}
}

func TestNormalizeExecutionModePreservesPlanWireValue(t *testing.T) {
	if got := normalizeExecutionMode(types.ExecutionModePlan); got != types.ExecutionModePlan {
		t.Fatalf("normalizeExecutionMode(plan) = %q, want %q", got, types.ExecutionModePlan)
	}
	if got := string(normalizeExecutionMode(types.ExecutionModePlan)); got != "plan" {
		t.Fatalf("plan wire value = %q, want plan", got)
	}
}

// testOrgRepo 是 account.OrgRepository 的测试替身，仅 GetOrgMember 返回固定 userName，
// 其余方法返回零值，供 NewMessagePoster 解析 sender_name 的测试使用。
type testOrgRepo struct {
	userName string
}

// newTestOrgRepoForSender 构造一个 GetOrgMember 返回给定 user 名的 OrgRepository 替身。
func newTestOrgRepoForSender(userName string) account.OrgRepository {
	return &testOrgRepo{userName: userName}
}

func (r *testOrgRepo) CreateOrg(ctx context.Context, req *account.CreateOrgInput) (*account.Org, error) {
	return nil, nil
}

func (r *testOrgRepo) GetOrg(ctx context.Context, publicID string, code string) (*account.Org, error) {
	return nil, nil
}

func (r *testOrgRepo) UpdateOrg(ctx context.Context, publicID string, req *account.UpdateOrgInput) (*account.Org, error) {
	return nil, nil
}

func (r *testOrgRepo) DeleteOrg(ctx context.Context, publicID string) error {
	return nil
}

func (r *testOrgRepo) ListOrgs(ctx context.Context, req *account.ListOrgsInput) (*account.OrgList, error) {
	return nil, nil
}

func (r *testOrgRepo) CreateOrgMember(ctx context.Context, req *account.CreateOrgMemberInput) (*account.OrgMember, error) {
	return nil, nil
}

func (r *testOrgRepo) GetOrgMember(ctx context.Context, id uint, uin uint) (*account.OrgMember, error) {
	return &account.OrgMember{Uin: uin, UserName: r.userName}, nil
}

func (r *testOrgRepo) UpdateOrgMember(ctx context.Context, id uint, req *account.UpdateOrgMemberInput) (*account.OrgMember, error) {
	return nil, nil
}

func (r *testOrgRepo) ListOrgMembers(ctx context.Context, req *account.ListOrgMembersInput) (*account.OrgMemberList, error) {
	return nil, nil
}

func (r *testOrgRepo) IsOrgCreator(ctx context.Context, orgID, uin uint) (bool, error) {
	return false, nil
}
