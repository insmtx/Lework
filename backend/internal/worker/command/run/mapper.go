package run

import (
	"strings"

	agentrundomain "github.com/insmtx/Leros/backend/internal/worker/agentrun/domain"
	"github.com/insmtx/Leros/backend/pkg/messaging"
)

// RequestFromWorkerTask converts the internal runTask into the agent runtime boundary.
func RequestFromWorkerTask(task runTask) *agentrundomain.RunRequest {
	return &agentrundomain.RunRequest{
		RunID:         firstNonEmpty(task.Trace.RunID, task.Trace.TaskID, task.ID),
		TraceID:       task.Trace.TraceID,
		TaskID:        task.Trace.TaskID,
		ExecutionMode: agentrundomain.ExecutionMode(task.ExecutionMode),
		Assistant: agentrundomain.AssistantContext{
			ID:           task.Execution.AssistantID,
			Name:         task.Execution.AssistantName,
			SystemPrompt: task.Execution.SystemPrompt,
			Skills:       append([]string(nil), task.Execution.Skills...),
			Tools:        append([]string(nil), task.Execution.Tools...),
		},
		Actor: agentrundomain.ActorContext{
			UserID:      task.Actor.UserID,
			DisplayName: task.Actor.DisplayName,
			Channel:     task.Actor.Channel,
			ExternalID:  task.Actor.ExternalID,
			AccountID:   task.Actor.AccountID,
		},
		Conversation: agentrundomain.ConversationContext{
			ID: task.Route.SessionID,
		},
		Workspace: agentrundomain.WorkspaceContext{
			OrgID:     task.Route.OrgID,
			ProjectID: task.Workspace.ProjectID,
			TaskID:    task.Trace.TaskID,
			RequestID: firstNonEmpty(task.Trace.RequestID, task.ID),
		},
		Input: agentrundomain.InputContext{
			Type:        agentrundomain.InputType(task.Input.Type),
			Messages:    inputMessagesFromTask(task.Input.Messages),
			Attachments: attachmentsFromTask(task.Input.Attachments),
		},
		Runtime: agentrundomain.RuntimeOptions{
			Kind:    task.Runtime.Kind,
			WorkDir: task.Runtime.WorkDir,
		},
		Model: agentrundomain.ModelOptions{
			Provider:     task.Model.Provider,
			Model:        task.Model.Model,
			APIKey:       task.Model.APIKey,
			BaseURL:      task.Model.BaseURL,
			BaseURLHasV1: task.Model.BaseURLHasV1,
		},
		Capability: agentrundomain.CapabilityContext{
			AllowedTools: append([]string(nil), task.Execution.Tools...),
		},
		Policy: agentrundomain.PolicyContext{
			RequireApproval: task.Policy.RequireApproval,
			PermissionMode:  task.Policy.PermissionMode,
		},
	}
}

func inputMessagesFromTask(messages []messaging.ChatMessage) []agentrundomain.InputMessage {
	if len(messages) == 0 {
		return nil
	}
	result := make([]agentrundomain.InputMessage, 0, len(messages))
	for _, message := range messages {
		result = append(result, agentrundomain.InputMessage{
			Role:    string(message.Role),
			Content: message.Content,
		})
	}
	return result
}

func attachmentsFromTask(attachments []messaging.Attachment) []agentrundomain.Attachment {
	if len(attachments) == 0 {
		return nil
	}
	result := make([]agentrundomain.Attachment, 0, len(attachments))
	for _, attachment := range attachments {
		result = append(result, agentrundomain.Attachment{
			ID:       attachment.ID,
			Name:     attachment.Name,
			MimeType: attachment.MimeType,
			URL:      attachment.URL,
		})
	}
	return result
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return value
		}
	}
	return ""
}
