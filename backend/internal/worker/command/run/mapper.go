package run

import (
	"strings"

	agentrundomain "github.com/insmtx/Leros/backend/internal/worker/agentrun/domain"
	"github.com/insmtx/Leros/backend/pkg/messaging"
	"github.com/insmtx/Leros/backend/types"
	"github.com/ygpkg/yg-go/logs"
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
			PublicID:     task.Execution.AssistantPublicID,
			Name:         task.Execution.AssistantName,
			Description:  task.Execution.AssistantDesc,
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
		Project: agentrundomain.ProjectContext{
			Name:        task.Project.Name,
			Description: task.Project.Description,
			Objective:   task.Project.Objective,
			Members:     membersFromTask(task.Project.Members),
		},
		Input: agentrundomain.InputContext{
			Type:         agentrundomain.InputType(task.Input.Type),
			Scene:        task.Input.Scene,
			OutputFormat: task.Input.OutputFormat,
			Messages:     inputMessagesFromTask(task.Input.Messages),
			Attachments:  attachmentsFromTask(task.Input.Attachments),
		},
		Runtime: agentrundomain.RuntimeOptions{
			Kind:    task.Runtime.Kind,
			WorkDir: task.Runtime.WorkDir,
		},
		Model: agentrundomain.ModelOptions{
			ModelID:          task.Model.ModelID,
			Provider:         task.Model.Provider,
			Model:            task.Model.Model,
			APIKey:           task.Model.APIKey,
			BaseURL:          task.Model.BaseURL,
			BaseURLHasV1:     task.Model.BaseURLHasV1,
			Vision:           task.Model.Vision,
			Temperature:      task.Model.Temperature,
			MaxTokens:        task.Model.MaxTokens,
			TopP:             task.Model.TopP,
			FrequencyPenalty: task.Model.FrequencyPenalty,
			PresencePenalty:  task.Model.PresencePenalty,
			ContextLimit:     task.Model.ContextLimit,
			OutputLimit:      task.Model.OutputLimit,
		},
		Capability: agentrundomain.CapabilityContext{
			AllowedTools: append([]string(nil), task.Execution.Tools...),
		},
		Policy: agentrundomain.PolicyContext{
			RequireApproval: task.Policy.RequireApproval,
			PermissionMode:  task.Policy.PermissionMode,
			DisabledPlugins: append([]types.DisabledPlugin(nil), task.Policy.DisabledPlugins...),
		},
		Plugins: pluginSnapshotsFromTask(task.Plugins),
		BusinessKeys: agentrundomain.BusinessKeys{
			ProjectPKID:       task.ProjectID,
			SessionPKID:       task.SessionID,
			MessagePKID:       task.MessageID,
			AssistantID:       task.AssistantID,
			AssistantPublicID: task.Route.AssistantPublicID,
			WorkerPublicID:    task.Route.WorkerPublicID,
			UinPK:             task.Uin,
		},
	}
}

func pluginSnapshotsFromTask(snapshots []messaging.PluginSnapshot) []agentrundomain.PluginSnapshot {
	if len(snapshots) == 0 {
		return nil
	}
	result := make([]agentrundomain.PluginSnapshot, 0, len(snapshots))
	for _, snapshot := range snapshots {
		result = append(result, agentrundomain.PluginSnapshot{PluginID: snapshot.PluginID, Code: snapshot.Code, Kind: snapshot.Kind, Revision: snapshot.Revision, Definition: append([]byte(nil), snapshot.Definition...)})
	}
	return result
}

func inputMessagesFromTask(messages []messaging.ChatMessage) []agentrundomain.InputMessage {
	if len(messages) == 0 {
		return nil
	}
	result := make([]agentrundomain.InputMessage, 0, len(messages))
	for _, message := range messages {
		result = append(result, agentrundomain.InputMessage{
			ID:           message.ID,
			Role:         string(message.Role),
			Content:      message.Content,
			SenderUserID: cloneUserID(message.SenderUserID),
			SenderName:   message.SenderName,
		})
	}
	return result
}

func cloneUserID(value *uint) *uint {
	if value == nil {
		return nil
	}
	cloned := *value
	return &cloned
}

func attachmentsFromTask(attachments []messaging.Attachment) []agentrundomain.Attachment {
	if len(attachments) == 0 {
		return nil
	}
	result := make([]agentrundomain.Attachment, 0, len(attachments))
	for _, attachment := range attachments {
		result = append(result, agentrundomain.Attachment{
			ID:             attachment.ID,
			Name:           attachment.Name,
			MimeType:       attachment.MimeType,
			URL:            attachment.URL,
			AttachmentRole: attachment.AttachmentRole,
		})
	}
	for _, a := range result {
		logs.Infof("[forensic][mapper] attachment from task: name=%q mime=%q attachment_role=%q url_nonempty=%v", a.Name, a.MimeType, a.AttachmentRole, a.URL != "")
	}
	return result
}

func membersFromTask(members []messaging.MemberBrief) []agentrundomain.MemberBrief {
	if len(members) == 0 {
		return nil
	}
	result := make([]agentrundomain.MemberBrief, 0, len(members))
	for _, m := range members {
		result = append(result, agentrundomain.MemberBrief{
			MemberID:      m.MemberID,
			MemberType:    m.MemberType,
			MemberRole:    m.MemberRole,
			Name:          m.Name,
			IsDefault:     m.IsDefault,
			IsCurrentExec: m.IsCurrentExec,
			IsCurrentUser: m.IsCurrentUser,
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
