package service

import (
	"context"
	"strings"
	"testing"
	"time"

	"gorm.io/gorm"

	infradb "github.com/insmtx/Leros/backend/internal/infra/db"
	"github.com/insmtx/Leros/backend/internal/llm"
	"github.com/insmtx/Leros/backend/internal/modelrouter"
	"github.com/insmtx/Leros/backend/pkg/messaging"
	"github.com/insmtx/Leros/backend/types"
)

// stubModelInvoker is a non-nil Invoker used in tests where the actual
// Call() is replaced by a mock via generateShortWorkTitles.
type stubModelInvoker struct{}

func (s *stubModelInvoker) Call(ctx context.Context, orgID uint, req *llm.CallRequest, opts ...modelrouter.InvokeOption) (*llm.CallResult, error) {
	return nil, nil
}

var _ modelrouter.Invoker = (*stubModelInvoker)(nil)

func withMockShortTitleGenerator(t *testing.T, fn func(context.Context, *gorm.DB, modelrouter.Invoker, workTitleGenerationInput) (generatedWorkTitles, error)) {
	t.Helper()
	old := generateShortWorkTitles
	generateShortWorkTitles = fn
	t.Cleanup(func() {
		generateShortWorkTitles = old
	})
}

func createTaskInProjectForTitleTest(
	t *testing.T,
	database *gorm.DB,
	project *types.Project,
	taskID string,
	sessionID string,
	content string,
	title string,
) *types.Session {
	t.Helper()
	ctx := context.Background()
	task := &types.Task{
		PublicID:  taskID,
		OrgID:     project.OrgID,
		OwnerID:   project.OwnerID,
		ProjectID: project.ID,
		TaskType:  types.TaskTypeGeneral,
		Title:     title,
		Status:    string(types.TaskStatusCreated),
	}
	if err := infradb.CreateTask(ctx, database, task); err != nil {
		t.Fatalf("CreateTask failed: %v", err)
	}
	session := &types.Session{
		PublicID:  sessionID,
		Type:      types.SessionTypeTask,
		Uin:       project.OwnerID,
		OrgID:     project.OrgID,
		ProjectID: &project.ID,
		TaskID:    &task.ID,
		Status:    string(types.SessionStatusActive),
		Title:     title,
	}
	if err := infradb.CreateSession(ctx, database, session); err != nil {
		t.Fatalf("CreateSession failed: %v", err)
	}
	if err := infradb.CreateMessage(ctx, database, &types.SessionMessage{
		SessionID: session.ID,
		Role:      string(types.MessageRoleUser),
		Content:   content,
		Sequence:  1,
		Timestamp: time.Now().UnixMilli(),
		Status:    string(types.MessageStatusCompleted),
	}); err != nil {
		t.Fatalf("CreateMessage user failed: %v", err)
	}
	if err := infradb.CreateMessage(ctx, database, &types.SessionMessage{
		SessionID: session.ID,
		Role:      string(types.MessageRoleAssistant),
		Content:   "assistant response",
		Sequence:  2,
		Timestamp: time.Now().UnixMilli(),
		Status:    string(types.MessageStatusCompleted),
	}); err != nil {
		t.Fatalf("CreateMessage assistant failed: %v", err)
	}
	return session
}

func TestWorkTitleUpdater_FirstTaskUpdatesProjectTaskSession(t *testing.T) {
	database := setupTestDB(t)
	bus := &recordingEventBus{}
	ctx := setupTestContextWithCaller(t)
	content := "请帮我做一份季度经营分析报告"
	fallback := fallbackWorkTitle(content)
	project := &types.Project{
		PublicID: "prj_first_title",
		OrgID:    1,
		OwnerID:  1,
		Name:     fallback,
		Status:   string(types.ProjectStatusActive),
	}
	if err := infradb.CreateProject(ctx, database, project); err != nil {
		t.Fatalf("CreateProject failed: %v", err)
	}
	session := createTaskInProjectForTitleTest(t, database, project, "task_first_title", "sess_first_title", content, fallback)

	withMockShortTitleGenerator(t, func(_ context.Context, _ *gorm.DB, _ modelrouter.Invoker, input workTitleGenerationInput) (generatedWorkTitles, error) {
		if !strings.Contains(input.AssistantMessage, "最终回复") {
			t.Fatalf("expected assistant content in successful generation, got %q", input.AssistantMessage)
		}
		return generatedWorkTitles{
			ProjectTitle: "季度经营",
			TaskTitle:    "季度经营分析",
			SessionTitle: "季度经营分析",
		}, nil
	})

	updater := NewWorkTitleUpdater(database, bus, &stubModelInvoker{})
	if err := updater.UpdateAfterFirstTurn(ctx, session.PublicID, "最终回复"); err != nil {
		t.Fatalf("UpdateAfterFirstTurn failed: %v", err)
	}

	gotProject, _ := infradb.GetProjectByID(ctx, database, project.ID)
	if gotProject.Name != "季度经营" {
		t.Fatalf("project name = %q, want %q", gotProject.Name, "季度经营")
	}
	gotTask, _ := infradb.GetTaskByID(ctx, database, 1, *session.TaskID)
	if gotTask.Title != "季度经营分析" {
		t.Fatalf("task title = %q, want %q", gotTask.Title, "季度经营分析")
	}
	gotSession, _ := infradb.GetSessionByPublicID(ctx, database, session.PublicID)
	if gotSession.Title != "季度经营分析" {
		t.Fatalf("session title = %q, want %q", gotSession.Title, "季度经营分析")
	}
	if bus.event == nil {
		t.Fatal("expected work title event")
	}
	if len(bus.events) != 2 {
		t.Fatalf("publish count = %d, want 2", len(bus.events))
	}
	event, ok := bus.events[0].event.(messaging.RunEvent)
	if !ok || event.Body.Event != messaging.RunEventWorkTitleUpdated {
		t.Fatalf("unexpected session event: %#v", bus.events[0].event)
	}
	globalEvent, ok := bus.events[1].event.(messaging.GlobalEventPayload)
	if !ok || globalEvent.Type != messaging.GlobalEventWorkTitleUpdated {
		t.Fatalf("unexpected global event: %#v", bus.events[1].event)
	}
}

func TestWorkTitleUpdater_SecondTaskDoesNotUpdateProject(t *testing.T) {
	database := setupTestDB(t)
	ctx := setupTestContextWithCaller(t)
	project := &types.Project{
		PublicID: "prj_second_title",
		OrgID:    1,
		OwnerID:  1,
		Name:     "旧项目名",
		Status:   string(types.ProjectStatusActive),
	}
	if err := infradb.CreateProject(ctx, database, project); err != nil {
		t.Fatalf("CreateProject failed: %v", err)
	}
	firstContent := "第一个任务"
	createTaskInProjectForTitleTest(t, database, project, "task_first_in_project", "sess_first_in_project", firstContent, fallbackWorkTitle(firstContent))

	secondContent := "请帮我整理投标文件"
	secondFallback := fallbackWorkTitle(secondContent)
	session := createTaskInProjectForTitleTest(t, database, project, "task_second_in_project", "sess_second_in_project", secondContent, secondFallback)

	withMockShortTitleGenerator(t, func(_ context.Context, _ *gorm.DB, _ modelrouter.Invoker, input workTitleGenerationInput) (generatedWorkTitles, error) {
		return generatedWorkTitles{
			ProjectTitle: "投标整理项目",
			TaskTitle:    "投标文件整理",
			SessionTitle: "投标文件整理",
		}, nil
	})

	updater := NewWorkTitleUpdater(database, &mockEventBus{}, &stubModelInvoker{})
	if err := updater.UpdateAfterFirstTurn(ctx, session.PublicID, "最终回复"); err != nil {
		t.Fatalf("UpdateAfterFirstTurn failed: %v", err)
	}

	gotProject, _ := infradb.GetProjectByID(ctx, database, project.ID)
	if gotProject.Name != "旧项目名" {
		t.Fatalf("project name = %q, want unchanged", gotProject.Name)
	}
	gotTask, _ := infradb.GetTaskByID(ctx, database, 1, *session.TaskID)
	if gotTask.Title != "投标文件整理" {
		t.Fatalf("task title = %q, want %q", gotTask.Title, "投标文件整理")
	}
}

func TestWorkTitleUpdater_ProjectFallsBackWhenGeneratedProjectTitleEmpty(t *testing.T) {
	database := setupTestDB(t)
	ctx := setupTestContextWithCaller(t)
	content := "几点了"
	fallback := fallbackWorkTitle(content)
	project := &types.Project{
		PublicID: "prj_empty_project_title",
		OrgID:    1,
		OwnerID:  1,
		Name:     fallback,
		Status:   string(types.ProjectStatusActive),
	}
	if err := infradb.CreateProject(ctx, database, project); err != nil {
		t.Fatalf("CreateProject failed: %v", err)
	}
	session := createTaskInProjectForTitleTest(t, database, project, "task_empty_project_title", "sess_empty_project_title", content, fallback)

	withMockShortTitleGenerator(t, func(_ context.Context, _ *gorm.DB, _ modelrouter.Invoker, input workTitleGenerationInput) (generatedWorkTitles, error) {
		return generatedWorkTitles{
			ProjectTitle: "",
			TaskTitle:    "查询当前时间",
			SessionTitle: "查询当前时间",
		}, nil
	})

	updater := NewWorkTitleUpdater(database, &mockEventBus{}, &stubModelInvoker{})
	if err := updater.UpdateAfterFirstTurn(ctx, session.PublicID, "当前时间是下午四点"); err != nil {
		t.Fatalf("UpdateAfterFirstTurn failed: %v", err)
	}
	gotProject, _ := infradb.GetProjectByID(ctx, database, project.ID)
	if gotProject.Name != "查询当前时间" {
		t.Fatalf("project name = %q, want fallback task title", gotProject.Name)
	}
}

func TestWorkTitleUpdater_FailedTurnDoesNotUseAssistantResult(t *testing.T) {
	database := setupTestDB(t)
	ctx := setupTestContextWithCaller(t)
	content := "登录接口一直报错"
	fallback := fallbackWorkTitle(content)
	project := &types.Project{
		PublicID: "prj_failed_title",
		OrgID:    1,
		OwnerID:  1,
		Name:     fallback,
		Status:   string(types.ProjectStatusActive),
	}
	if err := infradb.CreateProject(ctx, database, project); err != nil {
		t.Fatalf("CreateProject failed: %v", err)
	}
	session := createTaskInProjectForTitleTest(t, database, project, "task_failed_title", "sess_failed_title", content, fallback)

	withMockShortTitleGenerator(t, func(_ context.Context, _ *gorm.DB, _ modelrouter.Invoker, input workTitleGenerationInput) (generatedWorkTitles, error) {
		if input.AssistantMessage != "" {
			t.Fatalf("failed turn should not pass assistant result, got %q", input.AssistantMessage)
		}
		return generatedWorkTitles{ProjectTitle: "登录排查", TaskTitle: "登录异常排查", SessionTitle: "登录异常排查"}, nil
	})

	updater := NewWorkTitleUpdater(database, &mockEventBus{}, &stubModelInvoker{})
	if err := updater.UpdateAfterFirstTurn(ctx, session.PublicID, ""); err != nil {
		t.Fatalf("UpdateAfterFirstTurn failed: %v", err)
	}
	gotTask, _ := infradb.GetTaskByID(ctx, database, 1, *session.TaskID)
	if gotTask.Title != "登录异常排查" {
		t.Fatalf("task title = %q, want %q", gotTask.Title, "登录异常排查")
	}
}

func TestWorkTitleUpdater_MissingLLMIsBestEffortAndMarksAttempt(t *testing.T) {
	database := setupTestDB(t)
	ctx := setupTestContextWithCaller(t)
	content := "请帮我做一份周报"
	fallback := fallbackWorkTitle(content)
	project := &types.Project{
		PublicID: "prj_missing_llm_title",
		OrgID:    1,
		OwnerID:  1,
		Name:     fallback,
		Status:   string(types.ProjectStatusActive),
	}
	if err := infradb.CreateProject(ctx, database, project); err != nil {
		t.Fatalf("CreateProject failed: %v", err)
	}
	session := createTaskInProjectForTitleTest(t, database, project, "task_missing_llm_title", "sess_missing_llm_title", content, fallback)

	updater := NewWorkTitleUpdater(database, &mockEventBus{}, &stubModelInvoker{})
	if err := updater.UpdateAfterFirstTurn(ctx, session.PublicID, "最终回复"); err == nil {
		t.Fatal("expected missing llm error from direct updater call")
	}
	gotProject, _ := infradb.GetProjectByID(ctx, database, project.ID)
	if gotProject.Name != fallback {
		t.Fatalf("project name = %q, want fallback unchanged", gotProject.Name)
	}
	if !metadataHasAttempt(gotProject.Metadata) {
		t.Fatal("expected project attempt marker even when llm is missing")
	}
}

func TestWorkTitleUpdater_PassesPlainTextUserMessageToLLM(t *testing.T) {
	database := setupTestDB(t)
	bus := &recordingEventBus{}
	ctx := setupTestContextWithCaller(t)
	raw := `<skill-chip data-code="bid-plan">投标计划制定</skill-chip> 测试`
	fallback := fallbackWorkTitle(raw)
	if fallback != "投标计划制定 测试" {
		t.Fatalf("fallback title for UI must stay Chinese, got %q", fallback)
	}
	project := &types.Project{
		PublicID: "prj_plain_title",
		OrgID:    1,
		OwnerID:  1,
		Name:     fallback,
		Status:   string(types.ProjectStatusActive),
	}
	if err := infradb.CreateProject(ctx, database, project); err != nil {
		t.Fatalf("CreateProject failed: %v", err)
	}
	session := createTaskInProjectForTitleTest(t, database, project, "task_plain_title", "sess_plain_title", raw, fallback)

	withMockShortTitleGenerator(t, func(_ context.Context, _ *gorm.DB, _ modelrouter.Invoker, input workTitleGenerationInput) (generatedWorkTitles, error) {
		if strings.Contains(input.UserMessage, "skill-chip") {
			t.Fatalf("title LLM must not receive raw skill-chip HTML, got %q", input.UserMessage)
		}
		if strings.Contains(input.UserMessage, "投标计划制定") {
			t.Fatalf("title LLM must receive catalog code, not Chinese label, got %q", input.UserMessage)
		}
		if input.UserMessage != "bid-plan 测试" {
			t.Fatalf("UserMessage = %q", input.UserMessage)
		}
		return generatedWorkTitles{
			ProjectTitle: "投标计划",
			TaskTitle:    "投标计划制定",
			SessionTitle: "投标计划制定",
		}, nil
	})

	updater := NewWorkTitleUpdater(database, bus, &stubModelInvoker{})
	if err := updater.UpdateAfterFirstTurn(ctx, session.PublicID, "assistant reply"); err != nil {
		t.Fatalf("UpdateAfterFirstTurn failed: %v", err)
	}
}
