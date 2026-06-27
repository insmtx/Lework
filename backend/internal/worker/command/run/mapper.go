package run

import (
	"strings"

	"github.com/insmtx/Leros/backend/internal/agent"
	"github.com/insmtx/Leros/backend/pkg/messaging"
)

// RequestFromWorkerTask converts the internal runTask into the agent runtime boundary.
func RequestFromWorkerTask(task runTask) *agent.RequestContext {
	return &agent.RequestContext{
		RunID:   firstNonEmpty(task.Trace.RunID, task.Trace.TaskID, task.ID),
		TraceID: task.Trace.TraceID,
		TaskID:  task.Trace.TaskID,
		Assistant: agent.AssistantContext{
			ID:     task.Execution.AssistantID,
			Skills: append([]string(nil), task.Execution.Skills...),
			Tools:  append([]string(nil), task.Execution.Tools...),
		},
		Actor: agent.ActorContext{
			UserID:      task.Actor.UserID,
			DisplayName: task.Actor.DisplayName,
			Channel:     task.Actor.Channel,
			ExternalID:  task.Actor.ExternalID,
			AccountID:   task.Actor.AccountID,
		},
		Conversation: agent.ConversationContext{
			ID: task.Route.SessionID,
		},
		Workspace: agent.WorkspaceContext{
			OrgID:     task.Route.OrgID,
			ProjectID: task.Workspace.ProjectID,
			TaskID:    task.Trace.TaskID,
			RequestID: task.Trace.RequestID,
		},
		Input: agent.InputContext{
			Type:        agent.InputType(task.Input.Type),
			Messages:    inputMessagesFromTask(task.Input.Messages),
			Attachments: attachmentsFromTask(task.Input.Attachments),
		},
		Runtime: agent.RuntimeOptions{
			Kind:    task.Runtime.Kind,
			WorkDir: task.Runtime.WorkDir,
			MaxStep: task.Runtime.MaxStep,
		},
		Model: agent.ModelOptions{
			Provider:     task.Model.Provider,
			Model:        task.Model.Model,
			APIKey:       task.Model.APIKey,
			BaseURL:      task.Model.BaseURL,
			BaseURLHasV1: task.Model.BaseURLHasV1,
		},
		Capability: agent.CapabilityContext{
			AllowedTools: append([]string(nil), task.Execution.Tools...),
		},
		Policy: agent.PolicyContext{
			RequireApproval: task.Policy.RequireApproval,
			PermissionMode:  task.Policy.PermissionMode,
		},
		Metadata: mergedMetadata(task),
	}
}

func inputMessagesFromTask(messages []messaging.ChatMessage) []agent.InputMessage {
	if len(messages) == 0 {
		return nil
	}
	result := make([]agent.InputMessage, 0, len(messages))
	for _, message := range messages {
		result = append(result, agent.InputMessage{
			Role:    string(message.Role),
			Content: message.Content,
		})
	}
	return result
}

func attachmentsFromTask(attachments []messaging.Attachment) []agent.Attachment {
	if len(attachments) == 0 {
		return nil
	}
	result := make([]agent.Attachment, 0, len(attachments))
	for _, attachment := range attachments {
		result = append(result, agent.Attachment{
			ID:       attachment.ID,
			Name:     attachment.Name,
			MimeType: attachment.MimeType,
			URL:      attachment.URL,
		})
	}
	return result
}

func mergedMetadata(task runTask) map[string]any {
	metadata := make(map[string]any, len(task.Metadata)+1)
	for k, v := range task.Metadata {
		metadata[k] = v
	}
	if task.ID != "" {
		metadata["message_id"] = task.ID
	}
	return metadata
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return value
		}
	}
	return ""
}
