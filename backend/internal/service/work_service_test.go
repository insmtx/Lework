package service

import (
	"context"
	"strings"
	"testing"
	"time"

	"gorm.io/gorm"

	"github.com/insmtx/Leros/backend/internal/api/contract"
	dbpkg "github.com/insmtx/Leros/backend/internal/infra/db"
	"github.com/insmtx/Leros/backend/pkg/messaging"
	"github.com/insmtx/Leros/backend/types"
)

func setupTestWorkService(t *testing.T) (*workService, *gorm.DB) {
	t.Helper()
	db := setupTestDB(t)
	inferrer := &mockInferrer{assistantID: 1}
	service := NewWorkService(db, &mockEventBus{}, inferrer, nil, nil, "test", nil)
	typed, ok := service.(*workService)
	if !ok {
		t.Fatalf("expected *workService, got %T", service)
	}
	return typed, db
}

// seedProjectAssistant 为项目注入一个 AI 队友成员（DigitalAssistant ID=1）+ 对应 worker deployment，
// 使 NewMessage 流程能通过 resolveProjectAssistantWorker 解析到 worker。
func seedProjectAssistant(t *testing.T, database *gorm.DB, projectID uint) {
	t.Helper()
	if err := database.Create(&types.ProjectMember{
		ProjectID:  projectID,
		MemberID:   1,
		MemberType: types.MemberTypeAssistant,
		MemberRole: types.MemberRoleMember,
	}).Error; err != nil {
		t.Fatalf("seed project assistant member: %v", err)
	}
	if err := database.Create(&types.WorkerDeployment{
		OrgID:              1,
		DigitalAssistantID: 1,
		WorkerID:           1,
		DeploymentName:     "dep-default",
		Status:             string(types.WorkerDeploymentStatusReady),
	}).Error; err != nil {
		t.Fatalf("seed worker deployment: %v", err)
	}
}

func TestWorkServiceNewMessage_PersistsAttachmentsOnFirstMessage(t *testing.T) {
	service, database := setupTestWorkService(t)
	ctx := setupTestContextWithCaller(t)

	project := &types.Project{
		PublicID: "prj_test_attachment",
		OrgID:    1,
		OwnerID:  1,
		Name:     "Attachment Test",
		Status:   string(types.ProjectStatusActive),
	}
	if err := database.Create(project).Error; err != nil {
		t.Fatalf("create project: %v", err)
	}
	seedProjectAssistant(t, database, project.ID)

	// 预先落一条 file_upload，模拟前端已完成项目文件上传。
	fileUpload := &types.FileUpload{
		PublicID:     "fu_test_attachment",
		OrgID:        1,
		OwnerID:      1,
		Filename:     "spec.pdf",
		OriginalName: "spec.pdf",
		MimeType:     "application/pdf",
		FileSize:     1024,
		StorageURI:   "project-files/spec.pdf",
		Purpose:      "project_file",
		Status:       "active",
	}
	if err := dbpkg.CreateFileUpload(context.Background(), database, fileUpload); err != nil {
		t.Fatalf("CreateFileUpload failed: %v", err)
	}

	req := &contract.NewMessageRequest{
		ProjectID: project.PublicID,
		Content:   "请基于附件开始分析",
		Attachments: []types.MessageAttachment{
			{
				FileUploadID: fileUpload.PublicID,
				Name:         "spec.pdf",
				MimeType:     "application/pdf",
				Size:         1024,
			},
		},
	}

	resp, err := service.NewMessage(ctx, req)
	if err != nil {
		t.Fatalf("NewMessage failed: %v", err)
	}

	var session types.Session
	if err := database.WithContext(context.Background()).
		Where("public_id = ?", resp.SessionID).
		First(&session).Error; err != nil {
		t.Fatalf("load session failed: %v", err)
	}

	var message types.SessionMessage
	if err := database.WithContext(context.Background()).
		Where("session_id = ? AND sequence = ?", session.ID, 1).
		First(&message).Error; err != nil {
		t.Fatalf("load first message failed: %v", err)
	}

	if len(message.Attachments) != 1 {
		t.Fatalf("expected 1 attachment, got %d", len(message.Attachments))
	}

	attachment := message.Attachments[0]
	if attachment.FileUploadID != fileUpload.PublicID {
		t.Fatalf("expected file upload id %q, got %q", fileUpload.PublicID, attachment.FileUploadID)
	}
	if attachment.Name != "spec.pdf" {
		t.Fatalf("expected attachment name spec.pdf, got %q", attachment.Name)
	}

	refreshedUpload, err := dbpkg.GetFileUploadByPublicID(context.Background(), database, 1, fileUpload.PublicID)
	if err != nil {
		t.Fatalf("reload file upload failed: %v", err)
	}
	if refreshedUpload == nil {
		t.Fatal("expected file upload to exist after new message")
	}

	projectFile, err := dbpkg.GetProjectFileByFilePublicID(context.Background(), database, 1, fileUpload.PublicID)
	if err != nil {
		t.Fatalf("reload project file failed: %v", err)
	}
	if projectFile == nil {
		t.Fatal("expected project file association after new message")
	}
	if projectFile.ProjectID != project.ID {
		t.Fatalf("expected project file project_id %d, got %d", project.ID, projectFile.ProjectID)
	}
	if projectFile.ResourceType != types.ProjectFileResourceTypeUserUpload {
		t.Fatalf(
			"expected project file resource_type %q, got %q",
			types.ProjectFileResourceTypeUserUpload,
			projectFile.ResourceType,
		)
	}
}

func TestWorkServiceNewMessage_TouchesProjectUpdatedAt(t *testing.T) {
	service, database := setupTestWorkService(t)
	ctx := setupTestContextWithCaller(t)

	project := &types.Project{
		PublicID: "prj_test_new_message_touch",
		OrgID:    1,
		OwnerID:  1,
		Name:     "Touch Project",
		Status:   string(types.ProjectStatusActive),
	}
	if err := database.Create(project).Error; err != nil {
		t.Fatalf("create project: %v", err)
	}
	seedProjectAssistant(t, database, project.ID)

	oldUpdatedAt := time.Now().Add(-time.Hour).UTC()
	if err := database.Model(&types.Project{}).
		Where("id = ?", project.ID).
		Update("updated_at", oldUpdatedAt).Error; err != nil {
		t.Fatalf("set old project updated_at: %v", err)
	}

	_, err := service.NewMessage(ctx, &contract.NewMessageRequest{
		ProjectID: project.PublicID,
		Content:   "请在这个项目里新建一个任务",
	})
	if err != nil {
		t.Fatalf("NewMessage failed: %v", err)
	}

	refreshedProject, err := dbpkg.GetProjectByID(context.Background(), database, project.ID)
	if err != nil {
		t.Fatalf("reload project failed: %v", err)
	}
	if refreshedProject == nil {
		t.Fatal("expected project to exist after NewMessage")
	}
	if !refreshedProject.UpdatedAt.After(oldUpdatedAt) {
		t.Fatalf("expected project updated_at after %v, got %v", oldUpdatedAt, refreshedProject.UpdatedAt)
	}
}

func TestWorkServiceNewMessage_EmptySummonCreatesAssistantBoundSession(t *testing.T) {
	service, database := setupTestWorkService(t)
	ctx := setupTestContextWithCaller(t)

	assistant := seedReadyAssistant(t, database, "contract-reviewer", "合同审查专家", "只做合同风险审查")

	resp, err := service.NewMessage(ctx, &contract.NewMessageRequest{
		AssistantIDs: []uint{assistant.ID},
	})
	if err != nil {
		t.Fatalf("NewMessage empty summon failed: %v", err)
	}
	if resp.MessageID != "" {
		t.Fatalf("message id = %q, want empty for empty summon", resp.MessageID)
	}
	if resp.AssistantID != assistant.ID {
		t.Fatalf("response assistant id = %d, want %d", resp.AssistantID, assistant.ID)
	}

	var session types.Session
	if err := database.Where("public_id = ?", resp.SessionID).First(&session).Error; err != nil {
		t.Fatalf("load task session: %v", err)
	}
	if session.AssistantID != assistant.ID {
		t.Fatalf("session assistant id = %d, want %d", session.AssistantID, assistant.ID)
	}

	var count int64
	if err := database.Model(&types.SessionMessage{}).Where("session_id = ?", session.ID).Count(&count).Error; err != nil {
		t.Fatalf("count session messages: %v", err)
	}
	if count != 0 {
		t.Fatalf("empty summon persisted %d messages, want 0", count)
	}

	var task types.Task
	if err := database.Where("public_id = ?", resp.TaskID).First(&task).Error; err != nil {
		t.Fatalf("load task: %v", err)
	}
	if task.Title != "与合同审查专家对话" {
		t.Fatalf("task title = %q, want teammate title", task.Title)
	}
}

func TestWorkServiceNewMessage_RejectsInactiveAssistantSummon(t *testing.T) {
	service, database := setupTestWorkService(t)
	ctx := setupTestContextWithCaller(t)

	assistant := seedReadyAssistant(t, database, "inactive-reviewer", "停用队友", "停用后不能召唤")
	if err := database.Model(assistant).Update("status", string(contract.DigitalAssistantStatusInactive)).Error; err != nil {
		t.Fatalf("update assistant status: %v", err)
	}

	_, err := service.NewMessage(ctx, &contract.NewMessageRequest{
		AssistantIDs: []uint{assistant.ID},
	})
	if err == nil {
		t.Fatal("expected inactive assistant summon to fail")
	}
	if !strings.Contains(err.Error(), "digital assistant is not active") {
		t.Fatalf("error = %q, want inactive assistant error", err.Error())
	}
}

func TestWorkServiceNewMessage_RejectsAssistantBeforeDeploymentReady(t *testing.T) {
	service, database := setupTestWorkService(t)
	ctx := setupTestContextWithCaller(t)

	assistant := seedReadyAssistant(t, database, "deploying-reviewer", "部署中队友", "部署完成前不能召唤")
	if err := database.Model(&types.WorkerDeployment{}).
		Where("digital_assistant_id = ?", assistant.ID).
		Update("status", string(types.WorkerDeploymentStatusProvisioning)).Error; err != nil {
		t.Fatalf("update deployment status: %v", err)
	}

	_, err := service.NewMessage(ctx, &contract.NewMessageRequest{
		AssistantIDs: []uint{assistant.ID},
	})
	if err == nil {
		t.Fatal("expected provisioning assistant summon to fail")
	}
	if !strings.Contains(err.Error(), "worker deployment is not ready") {
		t.Fatalf("error = %q, want deployment not ready error", err.Error())
	}
}

func TestMessagePosterPublishWorkerTaskInjectsAssistantPersona(t *testing.T) {
	database := setupTestDB(t)
	ctx := setupTestContextWithCaller(t)
	recorder := &recordingEventBus{}
	poster := NewMessagePoster(database, recorder, &mockInferrer{assistantID: 1}, nil, nil, "test", nil)

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
		AllocatedAssistantID: assistant.ID,
		ProjectID:            &project.ID,
		TaskID:               &task.ID,
		Status:               string(types.SessionStatusActive),
		Title:                "Persona Session",
	}
	if err := database.Create(session).Error; err != nil {
		t.Fatalf("create session: %v", err)
	}

	_, err := poster.PostMessage(ctx, session, "", func(sequence int64) *types.SessionMessage {
		return &types.SessionMessage{
			Role:        string(types.MessageRoleUser),
			Content:     "帮我检查投标风险",
			MessageType: string(types.MessageTypeText),
			Status:      string(types.MessageStatusPending),
			Sequence:    sequence,
			Timestamp:   time.Now().UnixMilli(),
		}
	})
	if err != nil {
		t.Fatalf("PostMessage failed: %v", err)
	}

	cmd, ok := recorder.event.(messaging.WorkerCommand)
	if !ok {
		t.Fatalf("published event = %T, want messaging.WorkerCommand", recorder.event)
	}
	payload, err := messaging.DecodeCommandPayload[messaging.RunCommandPayload](&cmd.Body)
	if err != nil {
		t.Fatalf("decode run command: %v", err)
	}
	if payload.Execution.AssistantID != "1" {
		t.Fatalf("execution assistant id = %q, want 1", payload.Execution.AssistantID)
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
	poster := NewMessagePoster(database, recorder, nil, nil, nil, "test", nil)

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
		AllocatedAssistantID: assistant.ID,
		ProjectID:            &project.ID,
		TaskID:               &task.ID,
		Status:               string(types.SessionStatusActive),
		Title:                "Persona Evolution Session",
	}
	if err := database.Create(session).Error; err != nil {
		t.Fatalf("create session: %v", err)
	}

	_, err := poster.PostMessage(ctx, session, "", func(sequence int64) *types.SessionMessage {
		return &types.SessionMessage{
			Role:        string(types.MessageRoleUser),
			Content:     "帮我审查合同风险",
			MessageType: string(types.MessageTypeText),
			Status:      string(types.MessageStatusPending),
			Sequence:    sequence,
			Timestamp:   time.Now().UnixMilli(),
		}
	})
	if err != nil {
		t.Fatalf("PostMessage failed: %v", err)
	}

	cmd, ok := recorder.event.(messaging.WorkerCommand)
	if !ok {
		t.Fatalf("published event = %T, want messaging.WorkerCommand", recorder.event)
	}
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

func TestSyncSkillEntriesToProject_SkipsNonProjectSession(t *testing.T) {
	database := setupTestDB(t)
	ctx := setupTestContextWithCaller(t)
	poster := NewMessagePoster(database, &recordingEventBus{}, &mockInferrer{}, nil, nil, "test", nil)

	// session without ProjectID should not panic or error
	session := &types.Session{PublicID: "sess_no_project", OrgID: 1}
	poster.syncSkillEntriesToProject(ctx, session, []string{"tech-design-proposal"})
}

func TestSyncSkillEntriesToProject_AddsSkill(t *testing.T) {
	database := setupTestDB(t)
	ctx := setupTestContextWithCaller(t)
	poster := NewMessagePoster(database, &recordingEventBus{}, &mockInferrer{}, nil, nil, "test", nil)

	project := &types.Project{
		PublicID: "prj_sync_skill_add",
		OrgID:    1,
		OwnerID:  1,
		Name:     "Sync Skill Test",
		Status:   string(types.ProjectStatusActive),
	}
	if err := database.Create(project).Error; err != nil {
		t.Fatalf("create project: %v", err)
	}
	session := &types.Session{
		PublicID:  "sess_sync_skill_add",
		OrgID:     1,
		Uin:       1,
		ProjectID: &project.ID,
		Status:    string(types.SessionStatusActive),
	}
	if err := database.Create(session).Error; err != nil {
		t.Fatalf("create session: %v", err)
	}

	poster.syncSkillEntriesToProject(ctx, session, []string{"tech-design-proposal"})

	var refreshed types.Project
	if err := database.First(&refreshed, project.ID).Error; err != nil {
		t.Fatalf("reload project: %v", err)
	}
	if refreshed.Metadata.Extra == nil {
		t.Fatal("expected project metadata extra to be initialized")
	}
	rawSkills, ok := refreshed.Metadata.Extra["skills"].([]interface{})
	if !ok || len(rawSkills) != 1 {
		t.Fatalf("expected 1 skill in project metadata, got %d", len(rawSkills))
	}
	entry, ok := rawSkills[0].(map[string]interface{})
	if !ok {
		t.Fatal("skill entry is not a map")
	}
	if entry["code"] != "tech-design-proposal" {
		t.Fatalf("skill code = %q, want tech-design-proposal", entry["code"])
	}
	if entry["name"] != "tech-design-proposal" {
		t.Fatalf("skill name = %q, want tech-design-proposal", entry["name"])
	}
}

func TestSyncSkillEntriesToProject_DeduplicatesSkills(t *testing.T) {
	database := setupTestDB(t)
	ctx := setupTestContextWithCaller(t)
	poster := NewMessagePoster(database, &recordingEventBus{}, &mockInferrer{}, nil, nil, "test", nil)

	project := &types.Project{
		PublicID: "prj_sync_skill_dedup",
		OrgID:    1,
		OwnerID:  1,
		Name:     "Sync Skill Dedup",
		Status:   string(types.ProjectStatusActive),
		Metadata: types.ObjectMetadata{
			Extra: map[string]interface{}{
				"skills": []interface{}{
					map[string]interface{}{"code": "code-review", "name": "Code Review"},
				},
			},
		},
	}
	if err := database.Create(project).Error; err != nil {
		t.Fatalf("create project: %v", err)
	}
	session := &types.Session{
		PublicID:  "sess_sync_skill_dedup",
		OrgID:     1,
		Uin:       1,
		ProjectID: &project.ID,
		Status:    string(types.SessionStatusActive),
	}
	if err := database.Create(session).Error; err != nil {
		t.Fatalf("create session: %v", err)
	}

	// Same skill called multiple times
	poster.syncSkillEntriesToProject(ctx, session, []string{"code-review", "code-review"})

	var refreshed types.Project
	if err := database.First(&refreshed, project.ID).Error; err != nil {
		t.Fatalf("reload project: %v", err)
	}
	rawSkills, ok := refreshed.Metadata.Extra["skills"].([]interface{})
	if !ok {
		t.Fatal("expected skills in project metadata")
	}
	if len(rawSkills) != 1 {
		t.Fatalf("expected 1 skill after dedup, got %d", len(rawSkills))
	}
}

func TestSyncSkillEntriesToProject_MultipleSkills(t *testing.T) {
	database := setupTestDB(t)
	ctx := setupTestContextWithCaller(t)
	poster := NewMessagePoster(database, &recordingEventBus{}, &mockInferrer{}, nil, nil, "test", nil)

	project := &types.Project{
		PublicID: "prj_sync_skill_multi",
		OrgID:    1,
		OwnerID:  1,
		Name:     "Sync Skill Multi",
		Status:   string(types.ProjectStatusActive),
	}
	if err := database.Create(project).Error; err != nil {
		t.Fatalf("create project: %v", err)
	}
	session := &types.Session{
		PublicID:  "sess_sync_skill_multi",
		OrgID:     1,
		Uin:       1,
		ProjectID: &project.ID,
		Status:    string(types.SessionStatusActive),
	}
	if err := database.Create(session).Error; err != nil {
		t.Fatalf("create session: %v", err)
	}

	poster.syncSkillEntriesToProject(ctx, session, []string{"tech-design-proposal", "code-review"})

	var refreshed types.Project
	if err := database.First(&refreshed, project.ID).Error; err != nil {
		t.Fatalf("reload project: %v", err)
	}
	rawSkills, ok := refreshed.Metadata.Extra["skills"].([]interface{})
	if !ok || len(rawSkills) != 2 {
		t.Fatalf("expected 2 skills, got %d", len(rawSkills))
	}
}

func TestSkillNameInProjectSkills(t *testing.T) {
	skills := []interface{}{
		map[string]interface{}{"code": "code-review", "name": "Code Review"},
		map[string]interface{}{"code": "tech-design", "name": "tech-design"},
	}

	tests := []struct {
		name      string
		skills    []interface{}
		skillName string
		want      bool
	}{
		{"exact match code", skills, "code-review", true},
		{"case insensitive code", skills, "Code-Review", true},
		{"exact match name", skills, "tech-design", true},
		{"case insensitive name", skills, "Tech-Design", true},
		{"not found", skills, "not-a-skill", false},
		{"empty", skills, "", false},
		{"nil skills", nil, "anything", false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := skillNameInProjectSkills(tt.skills, tt.skillName)
			if got != tt.want {
				t.Fatalf("skillNameInProjectSkills(%q) = %v, want %v", tt.skillName, got, tt.want)
			}
		})
	}
}

func seedReadyAssistant(t *testing.T, database *gorm.DB, code, name, systemPrompt string) *types.DigitalAssistant {
	t.Helper()
	assistant := &types.DigitalAssistant{
		Code:         code,
		OrgID:        1,
		OwnerID:      1,
		Name:         name,
		Description:  name,
		Status:       "active",
		SystemPrompt: systemPrompt,
		Source:       "custom",
	}
	if err := database.Create(assistant).Error; err != nil {
		t.Fatalf("create assistant: %v", err)
	}
	deployment := &types.WorkerDeployment{
		OrgID:              1,
		DigitalAssistantID: assistant.ID,
		WorkerID:           assistant.ID,
		DeploymentName:     "test-" + code,
		Namespace:          "test",
		Status:             string(types.WorkerDeploymentStatusReady),
	}
	if err := database.Create(deployment).Error; err != nil {
		t.Fatalf("create worker deployment: %v", err)
	}
	return assistant
}
