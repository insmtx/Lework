package taskconsumer

import (
	"strings"

	"github.com/insmtx/Leros/backend/internal/agent"
	"github.com/insmtx/Leros/backend/internal/worker/protocol"
)

// RequestFromWorkerTask converts the worker task protocol into the agent runtime boundary.
func RequestFromWorkerTask(msg protocol.WorkerTaskMessage) *agent.RequestContext {
	return &agent.RequestContext{
		RunID:   firstNonEmpty(msg.Trace.RunID, msg.Trace.TaskID, msg.ID),
		TraceID: msg.Trace.TraceID,
		TaskID:  msg.Trace.TaskID,
		Assistant: agent.AssistantContext{
			ID:     msg.Body.Execution.AssistantID,
			Skills: append([]string(nil), msg.Body.Execution.Skills...),
			Tools:  append([]string(nil), msg.Body.Execution.Tools...),
		},
		Actor: agent.ActorContext{
			UserID:      msg.Body.Actor.UserID,
			DisplayName: msg.Body.Actor.DisplayName,
			Channel:     msg.Body.Actor.Channel,
			ExternalID:  msg.Body.Actor.ExternalID,
			AccountID:   msg.Body.Actor.AccountID,
		},
		Conversation: agent.ConversationContext{
			ID: msg.Route.SessionID,
		},
		Workspace: agent.WorkspaceContext{
			OrgID:     msg.Route.OrgID,
			ProjectID: msg.Body.Workspace.ProjectID,
			TaskID:    msg.Trace.TaskID,
			RequestID: msg.Trace.RequestID,
		},
		Input: agent.InputContext{
			Type:        agent.InputType(msg.Body.Input.Type),
			Messages:    inputMessagesFromTask(msg.Body.Input.Messages),
			Attachments: attachmentsFromTask(msg.Body.Input.Attachments),
		},
		Runtime: agent.RuntimeOptions{
			Kind:    msg.Body.Runtime.Kind,
			WorkDir: msg.Body.Runtime.WorkDir,
			MaxStep: msg.Body.Runtime.MaxStep,
		},
		Model: agent.ModelOptions{
			Provider:     msg.Body.Model.Provider,
			Model:        msg.Body.Model.Model,
			APIKey:       msg.Body.Model.APIKey,
			BaseURL:      msg.Body.Model.BaseURL,
			BaseURLHasV1: msg.Body.Model.BaseURLHasV1,
		},
		Capability: agent.CapabilityContext{
			AllowedTools: append([]string(nil), msg.Body.Execution.Tools...),
		},
		Policy: agent.PolicyContext{
			RequireApproval: msg.Body.Policy.RequireApproval,
			PermissionMode:  msg.Body.Policy.PermissionMode,
		},
		Metadata: map[string]any{
			"message_id": msg.ID,
			"metadata":   msg.Metadata,
		},
	}
}

func inputMessagesFromTask(messages []protocol.ChatMessage) []agent.InputMessage {
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

func attachmentsFromTask(attachments []protocol.Attachment) []agent.Attachment {
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

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return value
		}
	}
	return ""
}
