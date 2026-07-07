package service

import (
	"strings"
	"testing"
	"time"

	"github.com/insmtx/Leros/backend/pkg/messaging"
	"github.com/insmtx/Leros/backend/types"
)

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
