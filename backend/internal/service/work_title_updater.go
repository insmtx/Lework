package service

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	einoopenai "github.com/cloudwego/eino-ext/components/model/openai"

	"gorm.io/gorm"

	"github.com/insmtx/Leros/backend/internal/api/auth"
	infradb "github.com/insmtx/Leros/backend/internal/infra/db"
	eventbus "github.com/insmtx/Leros/backend/internal/infra/mq"
	"github.com/insmtx/Leros/backend/internal/llm"
	"github.com/insmtx/Leros/backend/internal/modelrouter"
	"github.com/insmtx/Leros/backend/pkg/messaging"
	"github.com/insmtx/Leros/backend/prompts"
	"github.com/insmtx/Leros/backend/types"
	"github.com/ygpkg/yg-go/logs"
)

const (
	workTitleAttemptedAtKey = "auto_title_generated_at"
	shortWorkTitleMaxRunes  = 20
)

// WorkTitleUpdater updates user-facing work titles after the first task turn.
type WorkTitleUpdater struct {
	db           *gorm.DB
	eventbus     eventbus.EventBus
	modelInvoker modelrouter.Invoker
}

// NewWorkTitleUpdater creates a best-effort work title updater.
func NewWorkTitleUpdater(database *gorm.DB, eb eventbus.EventBus, modelInvoker modelrouter.Invoker) *WorkTitleUpdater {
	return &WorkTitleUpdater{db: database, eventbus: eb, modelInvoker: modelInvoker}
}

type generatedWorkTitles struct {
	ProjectTitle string `json:"project_title"`
	TaskTitle    string `json:"task_title"`
	SessionTitle string `json:"session_title"`
}

type workTitleGenerationInput struct {
	OrgID            uint
	UserMessage      string
	AssistantMessage string
	ReqID            string
	ProjectID        uint
	SessionID        uint
	MessageID        uint
	Uin              uint
}

var generateShortWorkTitles func(
	ctx context.Context, database *gorm.DB, modelInvoker modelrouter.Invoker, input workTitleGenerationInput,
) (generatedWorkTitles, error) = generateShortWorkTitlesWithLLM

// UpdateAfterFirstTurn updates Project/Task/Session titles after the first
// assistant turn. It is intentionally best-effort: callers should log errors
// but never fail the main message flow because of them.
func (u *WorkTitleUpdater) UpdateAfterFirstTurn(ctx context.Context, sessionPublicID string, assistantMessage string) error {
	if u == nil || u.db == nil || strings.TrimSpace(sessionPublicID) == "" {
		return nil
	}
	logs.DebugContextf(ctx, "work title: checking first-turn title update session=%s", sessionPublicID)

	session, err := infradb.GetSessionByPublicID(ctx, u.db, sessionPublicID)
	if err != nil {
		return fmt.Errorf("get session %s: %w", sessionPublicID, err)
	}
	if session == nil || session.Type != types.SessionTypeTask || session.ProjectID == nil || session.TaskID == nil {
		logs.DebugContextf(ctx, "work title: skip non-task or unbound session=%s", sessionPublicID)
		return nil
	}
	firstAssistantTurn, err := u.isFirstAssistantTurn(ctx, session.ID)
	if err != nil {
		return fmt.Errorf("check first assistant turn: %w", err)
	}
	if !firstAssistantTurn {
		logs.DebugContextf(ctx, "work title: skip non-first assistant turn session=%s", session.PublicID)
		return nil
	}

	project, err := infradb.GetProjectByID(ctx, u.db, *session.ProjectID)
	if err != nil {
		return fmt.Errorf("get project %d: %w", *session.ProjectID, err)
	}
	if project == nil {
		return nil
	}

	task, err := infradb.GetTaskByID(ctx, u.db, session.OrgID, *session.TaskID)
	if err != nil {
		return fmt.Errorf("get task %d: %w", *session.TaskID, err)
	}
	if task == nil {
		return nil
	}

	firstMsg, err := u.firstUserMessage(ctx, session.ID)
	if err != nil {
		return fmt.Errorf("get first user message: %w", err)
	}
	if firstMsg == nil || strings.TrimSpace(firstMsg.Content) == "" {
		logs.DebugContextf(ctx, "work title: skip empty first user message session=%s", session.PublicID)
		return nil
	}

	fallbackTitle := fallbackWorkTitle(firstMsg.Content)
	isFirstTask, err := u.isFirstProjectTask(ctx, task)
	if err != nil {
		return fmt.Errorf("check first project task: %w", err)
	}

	projectAttempt := isFirstTask && !metadataHasAttempt(project.Metadata)
	taskAttempt := !metadataHasAttempt(task.Metadata)
	sessionAttempt := !metadataHasAttempt(session.Metadata) && !session.TitleManuallySet

	projectWritable := projectAttempt && isAutoWorkTitleFallback(project.Name, fallbackTitle)
	taskWritable := taskAttempt && isAutoWorkTitleFallback(task.Title, fallbackTitle)
	sessionWritable := sessionAttempt && isAutoWorkTitleFallback(session.Title, fallbackTitle)

	logs.DebugContextf(ctx,
		"work title: decision session=%s project=%s task=%s first_task=%t attempts(project=%t task=%t session=%t) writable(project=%t task=%t session=%t) current(project=%q task=%q session=%q) fallback=%q manual_session=%t",
		session.PublicID,
		project.PublicID,
		task.PublicID,
		isFirstTask,
		projectAttempt,
		taskAttempt,
		sessionAttempt,
		projectWritable,
		taskWritable,
		sessionWritable,
		project.Name,
		task.Title,
		session.Title,
		fallbackTitle,
		session.TitleManuallySet,
	)

	if !projectAttempt && !taskAttempt && !sessionAttempt {
		logs.DebugContextf(ctx, "work title: skip all attempts already recorded session=%s", session.PublicID)
		return nil
	}

	now := time.Now()
	if projectAttempt {
		markAutoTitleAttempt(&project.Metadata, now)
	}
	if taskAttempt {
		markAutoTitleAttempt(&task.Metadata, now)
	}
	if sessionAttempt {
		markAutoTitleAttempt(&session.Metadata, now)
	}

	if !projectWritable && !taskWritable && !sessionWritable {
		logs.InfoContextf(ctx, "work title: no writable fallback titles, marking attempts only session=%s project=%s task=%s", session.PublicID, project.PublicID, task.PublicID)
		return u.saveTitleAttemptMarkers(ctx, project, task, session, projectAttempt, taskAttempt, sessionAttempt)
	}

	_, trace := auth.FromContext(ctx)
	reqID := ""
	if trace != nil {
		reqID = trace.TraceID
	}

	logs.InfoContextf(ctx, "work title: generating short titles session=%s project=%s task=%s project_writable=%t include_assistant=%t", session.PublicID, project.PublicID, task.PublicID, projectWritable, strings.TrimSpace(assistantMessage) != "")

	var titles generatedWorkTitles
	if u.modelInvoker != nil {
		titles, err = generateShortWorkTitles(ctx, u.db, u.modelInvoker, workTitleGenerationInput{
			OrgID:            session.OrgID,
			UserMessage:      firstMsg.Content,
			AssistantMessage: assistantMessage,
			ReqID:            reqID,
			ProjectID:        project.ID,
			SessionID:        session.ID,
			MessageID:        firstMsg.ID,
			Uin:              session.Uin,
		})
		if err != nil {
			if saveErr := u.saveTitleAttemptMarkers(ctx, project, task, session, projectAttempt, taskAttempt, sessionAttempt); saveErr != nil {
				logs.WarnContextf(ctx, "work title: save attempt markers after generation failure: %v", saveErr)
			}
			return err
		}
	} else {
		err = fmt.Errorf("model store not configured")
		if saveErr := u.saveTitleAttemptMarkers(ctx, project, task, session, projectAttempt, taskAttempt, sessionAttempt); saveErr != nil {
			logs.WarnContextf(ctx, "work title: save attempt markers after generation failure: %v", saveErr)
		}
		return err
	}
	logs.DebugContextf(ctx, "work title: generated titles session=%s project_title=%q task_title=%q session_title=%q", session.PublicID, titles.ProjectTitle, titles.TaskTitle, titles.SessionTitle)

	projectUpdated := false
	if projectWritable {
		title := sanitizeShortWorkTitle(titles.ProjectTitle)
		if title == "" {
			title = sanitizeShortWorkTitle(titles.TaskTitle)
			if title == "" {
				title = sanitizeShortWorkTitle(titles.SessionTitle)
			}
			if title != "" {
				logs.WarnContextf(ctx, "work title: generated empty project title, fallback to non-empty title session=%s project=%s title=%q", session.PublicID, project.PublicID, title)
			}
		}
		if title != "" {
			project.Name = title
			project.UpdatedAt = now
			projectUpdated = true
		}
	}

	taskUpdated := false
	if taskWritable {
		if title := sanitizeShortWorkTitle(titles.TaskTitle); title != "" {
			task.Title = title
			task.UpdatedAt = now
			taskUpdated = true
		}
	}

	sessionUpdated := false
	if sessionWritable {
		title := sanitizeShortWorkTitle(titles.SessionTitle)
		if title == "" {
			title = sanitizeShortWorkTitle(titles.TaskTitle)
		}
		if title != "" {
			session.Title = title
			session.UpdatedAt = now
			sessionUpdated = true
		}
	}

	if err := u.saveUpdatedTitles(ctx, project, task, session, projectAttempt, taskAttempt, sessionAttempt); err != nil {
		logs.ErrorContextf(ctx, "work title: save updated titles failed session=%s project=%s task=%s: %v", session.PublicID, project.PublicID, task.PublicID, err)
		return err
	}

	if projectUpdated || taskUpdated || sessionUpdated {
		if err := u.publishWorkTitleUpdated(ctx, session, project, task); err != nil {
			logs.ErrorContextf(ctx, "work title: publish title updated failed session=%s project=%s task=%s: %v", session.PublicID, project.PublicID, task.PublicID, err)
			return fmt.Errorf("publish work title updated: %w", err)
		}
		logs.InfoContextf(ctx, "work title: updated titles session=%s project=%s task=%s updated(project=%t task=%t session=%t)", session.PublicID, project.PublicID, task.PublicID, projectUpdated, taskUpdated, sessionUpdated)
	} else {
		logs.InfoContextf(ctx, "work title: generation returned no usable title session=%s project=%s task=%s", session.PublicID, project.PublicID, task.PublicID)
	}
	return nil
}

func (u *WorkTitleUpdater) firstUserMessage(ctx context.Context, sessionID uint) (*types.SessionMessage, error) {
	var message types.SessionMessage
	err := u.db.WithContext(ctx).
		Where("session_id = ? AND role = ?", sessionID, string(types.MessageRoleUser)).
		Order("sequence ASC, id ASC").
		First(&message).Error
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, nil
		}
		return nil, err
	}
	return &message, nil
}

func (u *WorkTitleUpdater) isFirstAssistantTurn(ctx context.Context, sessionID uint) (bool, error) {
	var count int64
	if err := u.db.WithContext(ctx).
		Model(&types.SessionMessage{}).
		Where("session_id = ? AND role = ?", sessionID, string(types.MessageRoleAssistant)).
		Count(&count).Error; err != nil {
		return false, err
	}
	return count == 1, nil
}

func (u *WorkTitleUpdater) isFirstProjectTask(ctx context.Context, task *types.Task) (bool, error) {
	if task == nil {
		return false, nil
	}
	var first types.Task
	err := u.db.WithContext(ctx).
		Where("org_id = ? AND project_id = ? AND deleted_at IS NULL", task.OrgID, task.ProjectID).
		Order("created_at ASC, id ASC").
		First(&first).Error
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return false, nil
		}
		return false, err
	}
	return first.ID == task.ID, nil
}

func (u *WorkTitleUpdater) saveTitleAttemptMarkers(
	ctx context.Context,
	project *types.Project,
	task *types.Task,
	session *types.Session,
	projectAttempt bool,
	taskAttempt bool,
	sessionAttempt bool,
) error {
	return u.saveUpdatedTitles(ctx, project, task, session, projectAttempt, taskAttempt, sessionAttempt)
}

func (u *WorkTitleUpdater) saveUpdatedTitles(
	ctx context.Context,
	project *types.Project,
	task *types.Task,
	session *types.Session,
	projectAttempt bool,
	taskAttempt bool,
	sessionAttempt bool,
) error {
	if err := u.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if projectAttempt {
			if err := infradb.UpdateProject(ctx, tx, project); err != nil {
				return fmt.Errorf("update project %s: %w", project.PublicID, err)
			}
		}
		if taskAttempt {
			if err := infradb.UpdateTask(ctx, tx, task); err != nil {
				return fmt.Errorf("update task %s: %w", task.PublicID, err)
			}
		}
		if sessionAttempt {
			if err := infradb.UpdateSession(ctx, tx, session); err != nil {
				return fmt.Errorf("update session %s: %w", session.PublicID, err)
			}
		}
		return nil
	}); err != nil {
		return err
	}
	return nil
}

func (u *WorkTitleUpdater) publishWorkTitleUpdated(
	ctx context.Context,
	session *types.Session,
	project *types.Project,
	task *types.Task,
) error {
	if u == nil || u.eventbus == nil || session == nil || project == nil || task == nil {
		return nil
	}
	if session.OrgID == 0 || session.PublicID == "" {
		return nil
	}

	workTitle := messaging.WorkTitleUpdatedPayload{
		ProjectID:    project.PublicID,
		ProjectName:  project.Name,
		TaskID:       task.PublicID,
		TaskTitle:    task.Title,
		SessionID:    session.PublicID,
		SessionTitle: session.Title,
	}
	topic, err := messaging.RunEventSubject(session.OrgID, session.PublicID, messaging.RunEventLaneState)
	if err != nil {
		return err
	}

	msg := messaging.RunEvent{
		ID:        fmt.Sprintf("work-title:%s:%d", session.PublicID, time.Now().UnixMilli()),
		Type:      messaging.MessageTypeRunEvent,
		CreatedAt: time.Now().UTC(),
		Route: messaging.RouteContext{
			OrgID:     session.OrgID,
			SessionID: session.PublicID,
		},
		Body: messaging.RunEventBody{
			Seq:   time.Now().UnixMilli(),
			Event: messaging.RunEventWorkTitleUpdated,
			Payload: messaging.RunEventPayload{
				WorkTitle: &workTitle,
			},
		},
	}
	if err := u.eventbus.Publish(ctx, topic, msg); err != nil {
		return err
	}
	logs.InfoContextf(ctx, "work title: published session event topic=%s session=%s project=%s task=%s", topic, session.PublicID, project.PublicID, task.PublicID)

	if err := u.publishGlobalWorkTitleUpdated(ctx, session, project, workTitle); err != nil {
		return err
	}
	return nil
}

func (u *WorkTitleUpdater) publishGlobalWorkTitleUpdated(
	ctx context.Context,
	session *types.Session,
	project *types.Project,
	workTitle messaging.WorkTitleUpdatedPayload,
) error {
	subject, err := messaging.ProjectNotifySubject(session.OrgID, project.ID)
	if err != nil {
		return err
	}
	data, err := json.Marshal(workTitle)
	if err != nil {
		return err
	}
	payload := messaging.GlobalEventPayload{
		Type:      messaging.GlobalEventWorkTitleUpdated,
		ProjectID: project.ID,
		SessionID: session.PublicID,
		Timestamp: time.Now().UnixMilli(),
		Data:      data,
	}
	if err := u.eventbus.Publish(ctx, subject, payload); err != nil {
		return err
	}
	logs.InfoContextf(ctx, "work title: published global event subject=%s session=%s project=%s", subject, session.PublicID, project.PublicID)
	return nil
}

func generateShortWorkTitlesWithLLM(ctx context.Context, database *gorm.DB, modelInvoker modelrouter.Invoker, input workTitleGenerationInput) (generatedWorkTitles, error) {
	model, err := llm.ResolveDefaultLLMModel(ctx, database, input.OrgID)
	if err != nil {
		return generatedWorkTitles{}, fmt.Errorf("get default model: %w", err)
	}
	if model == nil {
		return generatedWorkTitles{}, fmt.Errorf("no default LLM model configured for org %d", input.OrgID)
	}

	template := prompts.Get(prompts.KeyWorkShortTitle)
	if template == "" {
		return generatedWorkTitles{}, fmt.Errorf("prompt %q not registered", prompts.KeyWorkShortTitle)
	}
	prompt := renderWorkShortTitlePrompt(template, input)

	temperature := 0.1
	result, err := modelInvoker.Call(ctx, input.OrgID, &llm.CallRequest{
		ModelID: model.ID,
		Messages: []llm.Message{
			{Role: "user", Content: prompt},
		},
		Temperature: &temperature,
		ResponseFormat: &einoopenai.ChatCompletionResponseFormat{
			Type: einoopenai.ChatCompletionResponseFormatTypeJSONObject,
		},
		ReasoningEffort: einoopenai.ReasoningEffortLevelLow,
		CallerType:      "work_title_updater",
		ReqID:           input.ReqID,
		ProjectID:       input.ProjectID,
		SessionID:       input.SessionID,
		MessageID:       input.MessageID,
		Uin:             input.Uin,
	})
	if err != nil {
		return generatedWorkTitles{}, fmt.Errorf("generate title: %w", err)
	}
	if result == nil || result.Message == nil {
		return generatedWorkTitles{}, errors.New("generate title: empty response")
	}

	return parseGeneratedWorkTitles(result.Message.Content)
}

func renderWorkShortTitlePrompt(template string, input workTitleGenerationInput) string {
	replacer := strings.NewReplacer(
		"{user_message}", strings.TrimSpace(input.UserMessage),
		"{assistant_message}", strings.TrimSpace(input.AssistantMessage),
	)
	return replacer.Replace(template)
}

func parseGeneratedWorkTitles(content string) (generatedWorkTitles, error) {
	content = strings.TrimSpace(content)
	content = strings.TrimPrefix(content, "```json")
	content = strings.TrimPrefix(content, "```")
	content = strings.TrimSuffix(content, "```")
	content = strings.TrimSpace(content)

	var titles generatedWorkTitles
	if err := json.Unmarshal([]byte(content), &titles); err != nil {
		return generatedWorkTitles{}, fmt.Errorf("parse generated titles: %w", err)
	}
	titles.ProjectTitle = sanitizeShortWorkTitle(titles.ProjectTitle)
	titles.TaskTitle = sanitizeShortWorkTitle(titles.TaskTitle)
	titles.SessionTitle = sanitizeShortWorkTitle(titles.SessionTitle)
	return titles, nil
}

func sanitizeShortWorkTitle(title string) string {
	title = strings.TrimSpace(title)
	title = strings.Trim(title, "\"'`“”‘’「」『』.,，。:：;；!?！？")
	title = strings.TrimSpace(title)
	if title == "" {
		return ""
	}
	for _, prefix := range []string{"帮我", "请帮我", "任务", "需求", "分析一下", "处理一下"} {
		title = strings.TrimPrefix(title, prefix)
		title = strings.TrimSpace(title)
	}
	runes := []rune(title)
	if len(runes) > shortWorkTitleMaxRunes {
		return string(runes[:shortWorkTitleMaxRunes])
	}
	return title
}

func isAutoWorkTitleFallback(current string, firstMessageFallback string) bool {
	title := strings.TrimSpace(current)
	if title == "" {
		return true
	}
	if title == strings.TrimSpace(firstMessageFallback) {
		return true
	}
	if title == "新的队友对话" || title == "新的队友任务" {
		return true
	}
	return strings.HasPrefix(title, "与") && strings.HasSuffix(title, "对话")
}

func metadataHasAttempt(metadata types.ObjectMetadata) bool {
	if metadata.Extra == nil {
		return false
	}
	value, ok := metadata.Extra[workTitleAttemptedAtKey]
	if !ok {
		return false
	}
	if s, ok := value.(string); ok {
		return strings.TrimSpace(s) != ""
	}
	return value != nil
}

func markAutoTitleAttempt(metadata *types.ObjectMetadata, at time.Time) {
	if metadata.Extra == nil {
		metadata.Extra = map[string]interface{}{}
	}
	metadata.Extra[workTitleAttemptedAtKey] = at.UTC().Format(time.RFC3339Nano)
}
