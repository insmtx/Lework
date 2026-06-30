package run

import (
	"strings"

	"github.com/insmtx/Leros/backend/agent"
	assistantdomain "github.com/insmtx/Leros/backend/internal/assistant/domain"
	"github.com/insmtx/Leros/backend/pkg/messaging"
)

// RequestFromWorkerTask converts the internal runTask into the agent runtime boundary.
func RequestFromWorkerTask(task runTask) *assistantdomain.RunRequest {
	return &assistantdomain.RunRequest{
		RunID:         firstNonEmpty(task.Trace.RunID, task.Trace.TaskID, task.ID),
		TraceID:       task.Trace.TraceID,
		TaskID:        task.Trace.TaskID,
		ExecutionMode: agent.ExecutionMode(task.ExecutionMode),
		Assistant: assistantdomain.AssistantContext{
			ID:     task.Execution.AssistantID,
			Skills: append([]string(nil), task.Execution.Skills...),
			Tools:  append([]string(nil), task.Execution.Tools...),
		},
		Actor: assistantdomain.ActorContext{
			UserID:      task.Actor.UserID,
			DisplayName: task.Actor.DisplayName,
			Channel:     task.Actor.Channel,
			ExternalID:  task.Actor.ExternalID,
			AccountID:   task.Actor.AccountID,
		},
		Conversation: assistantdomain.ConversationContext{
			ID: task.Route.SessionID,
		},
		Workspace: assistantdomain.WorkspaceContext{
			OrgID:     task.Route.OrgID,
			ProjectID: task.Workspace.ProjectID,
			TaskID:    task.Trace.TaskID,
			RequestID: firstNonEmpty(task.Trace.RequestID, task.ID),
		},
		Input: assistantdomain.InputContext{
			Type:        assistantdomain.InputType(task.Input.Type),
			Messages:    inputMessagesFromTask(task.Input.Messages),
			Attachments: attachmentsFromTask(task.Input.Attachments),
		},
		Runtime: assistantdomain.RuntimeOptions{
			Kind:    task.Runtime.Kind,
			WorkDir: task.Runtime.WorkDir,
			MaxStep: task.Runtime.MaxStep,
		},
		Model: assistantdomain.ModelOptions{
			Provider:     task.Model.Provider,
			Model:        task.Model.Model,
			APIKey:       task.Model.APIKey,
			BaseURL:      task.Model.BaseURL,
			BaseURLHasV1: task.Model.BaseURLHasV1,
		},
		Capability: assistantdomain.CapabilityContext{
			AllowedTools: append([]string(nil), task.Execution.Tools...),
		},
		Policy: assistantdomain.PolicyContext{
			RequireApproval: task.Policy.RequireApproval,
			PermissionMode:  task.Policy.PermissionMode,
		},
	}
}

func inputMessagesFromTask(messages []messaging.ChatMessage) []assistantdomain.InputMessage {
	if len(messages) == 0 {
		return nil
	}
	result := make([]assistantdomain.InputMessage, 0, len(messages))
	for _, message := range messages {
		result = append(result, assistantdomain.InputMessage{
			Role:    string(message.Role),
			Content: message.Content,
		})
	}
	return result
}

func attachmentsFromTask(attachments []messaging.Attachment) []assistantdomain.Attachment {
	if len(attachments) == 0 {
		return nil
	}
	result := make([]assistantdomain.Attachment, 0, len(attachments))
	for _, attachment := range attachments {
		result = append(result, assistantdomain.Attachment{
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
