// Package taskconsumer consumes worker task messages and executes agent runs.
package taskconsumer

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/insmtx/Leros/backend/internal/agent"
	"github.com/insmtx/Leros/backend/internal/agent/runtime/events"
	eventbus "github.com/insmtx/Leros/backend/internal/infra/mq"
	"github.com/insmtx/Leros/backend/pkg/dm"
	"github.com/nats-io/nats.go"
	"github.com/ygpkg/yg-go/logs"
)

// Config controls one standalone worker task consumer.
type Config struct {
	OrgID    uint
	WorkerID uint
}

// Consumer subscribes to one worker task topic and dispatches tasks to an agent runtime.
type Consumer struct {
	cfg        Config
	subscriber eventbus.Subscriber
	publisher  ResultPublisher
	runner     agent.Runner
}

// New creates a worker task consumer.
func New(cfg Config, subscriber eventbus.Subscriber, publisher ResultPublisher, runner agent.Runner) (*Consumer, error) {
	if cfg.OrgID == 0 {
		return nil, fmt.Errorf("worker org_id is required")
	}
	if cfg.WorkerID == 0 {
		return nil, fmt.Errorf("worker worker_id is required")
	}
	if subscriber == nil {
		return nil, fmt.Errorf("subscriber is required")
	}
	if publisher == nil {
		return nil, fmt.Errorf("publisher is required")
	}
	if runner == nil {
		return nil, fmt.Errorf("agent runner is required")
	}
	return &Consumer{
		cfg:        cfg,
		subscriber: subscriber,
		publisher:  publisher,
		runner:     runner,
	}, nil
}

// TaskTopic returns the NATS subject consumed by this worker.
func (c *Consumer) TaskTopic() string {
	topic, err := dm.WorkerTaskSubject(c.cfg.OrgID, c.cfg.WorkerID)
	if err != nil {
		logs.Errorf("Failed to get worker task topic for org_id=%d worker_id=%d: %v", c.cfg.OrgID, c.cfg.WorkerID, err)
	}
	return topic
}

// Start subscribes to the worker task topic.
func (c *Consumer) Start(ctx context.Context) error {
	topic := c.TaskTopic()
	logs.InfoContextf(ctx, "Starting worker task subscription: %s", topic)
	return c.subscriber.Subscribe(ctx, topic, dm.WorkerTaskConsumer(), func(msg *nats.Msg) {
		logs.InfoContextf(ctx, "Received worker task event from topic: %s", topic)
		if err := c.handleEvent(ctx, msg); err != nil {
			logs.ErrorContextf(ctx, "Failed to handle worker task: %v", err)
		}
	})
}

func (c *Consumer) handleEvent(ctx context.Context, msg *nats.Msg) error {
	taskMsg, err := decodeWorkerTask(msg)
	if err != nil {
		return err
	}
	if err := c.validateRoute(taskMsg); err != nil {
		return err
	}
	if taskMsg.Body.TaskType != events.TaskTypeAgentRun {
		return fmt.Errorf("unsupported worker task type %q", taskMsg.Body.TaskType)
	}

	logs.InfoContextf(ctx,
		"Received worker task: msg_id=%s task_id=%s run_id=%s org_id=%s worker_id=%s session_id=%s task_type=%s",
		taskMsg.ID,
		taskMsg.Trace.TaskID,
		taskMsg.Trace.RunID,
		taskMsg.Route.OrgID,
		taskMsg.Route.WorkerID,
		taskMsg.Route.SessionID,
		taskMsg.Body.TaskType,
	)

	req := RequestFromWorkerTask(taskMsg)
	req.EventSink = NewMQStreamSink(c.publisher, taskMsg)

	logs.InfoContextf(ctx,
		"Starting worker task run: task_id=%s run_id=%s runtime=%s assistant_id=%s agent_id=%s",
		req.TaskID,
		req.RunID,
		req.Runtime.Kind,
		req.Assistant.ID,
		taskMsg.Body.Execution.AgentID,
	)

	result, err := c.runner.Run(ctx, req)
	if err != nil {
		return err
	}
	if result != nil {
		logs.InfoContextf(ctx, "Worker task completed: task_id=%s run_id=%s status=%s", req.TaskID, result.RunID, result.Status)
	}
	return nil
}

func decodeWorkerTask(msg *nats.Msg) (events.WorkerTaskMessage, error) {
	var taskMsg events.WorkerTaskMessage
	if err := json.Unmarshal(msg.Data, &taskMsg); err != nil {
		return taskMsg, fmt.Errorf("unmarshal worker task: %w", err)
	}
	if taskMsg.Type != "" && taskMsg.Type != events.MessageTypeWorkerTask {
		return taskMsg, fmt.Errorf("unexpected worker task message type %q", taskMsg.Type)
	}
	return taskMsg, nil
}

func (c *Consumer) validateRoute(msg events.WorkerTaskMessage) error {
	if msg.Route.OrgID != 0 && msg.Route.OrgID != c.cfg.OrgID {
		return fmt.Errorf("task org_id %q does not match worker org_id %q", msg.Route.OrgID, c.cfg.OrgID)
	}
	if msg.Route.WorkerID != 0 && msg.Route.WorkerID != c.cfg.WorkerID {
		return fmt.Errorf("task worker_id %q does not match worker_id %q", msg.Route.WorkerID, c.cfg.WorkerID)
	}
	return nil
}

// RequestFromWorkerTask converts the domain message protocol into the agent runtime boundary.
func RequestFromWorkerTask(msg events.WorkerTaskMessage) *agent.RequestContext {
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
		Input: agent.InputContext{
			Type:        agent.InputType(msg.Body.Input.Type),
			Text:        msg.Body.Input.Text,
			Messages:    inputMessagesFromTask(msg.Body.Input.Messages),
			Attachments: attachmentsFromTask(msg.Body.Input.Attachments),
		},
		Runtime: agent.RuntimeOptions{
			Kind:    msg.Body.Runtime.Kind,
			WorkDir: msg.Body.Runtime.WorkDir,
			MaxStep: msg.Body.Runtime.MaxStep,
		},
		Model: agent.ModelOptions{
			ID: msg.Body.Model.ID,
		},
		Capability: agent.CapabilityContext{
			AllowedTools: append([]string(nil), msg.Body.Execution.Tools...),
		},
		Policy: agent.PolicyContext{
			RequireApproval: msg.Body.Policy.RequireApproval,
		},
		Metadata: map[string]any{
			"message_id": msg.ID,
			"org_id":     msg.Route.OrgID,
			"worker_id":  msg.Route.WorkerID,
			"session_id": msg.Route.SessionID,
			"agent_id":   msg.Body.Execution.AgentID,
			"metadata":   msg.Metadata,
		},
	}
}

func inputMessagesFromTask(messages []events.ChatMessage) []agent.InputMessage {
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

func attachmentsFromTask(attachments []events.Attachment) []agent.Attachment {
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
