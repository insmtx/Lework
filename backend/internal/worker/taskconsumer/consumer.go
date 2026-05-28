// Package taskconsumer consumes worker task messages and executes agent runs.
package taskconsumer

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/insmtx/Leros/backend/internal/agent"
	eventbus "github.com/insmtx/Leros/backend/internal/infra/mq"
	"github.com/insmtx/Leros/backend/internal/modelrouter"
	"github.com/insmtx/Leros/backend/internal/worker/protocol"
	agentworkspace "github.com/insmtx/Leros/backend/internal/workspace"
	"github.com/insmtx/Leros/backend/pkg/dm"
	"github.com/insmtx/Leros/backend/pkg/utils"
	"github.com/nats-io/nats.go"
	"github.com/ygpkg/yg-go/logs"
)

// Config controls one standalone worker task consumer.
type Config struct {
	OrgID          uint
	WorkerID       uint
	DebounceWindow time.Duration
}

// Consumer subscribes to one worker task topic and dispatches tasks to an agent runtime.
type Consumer struct {
	cfg        Config
	subscriber eventbus.Subscriber
	publisher  ResultPublisher
	runner     agent.Runner
	debouncer  *utils.TrailingDebouncer[protocol.WorkerTaskMessage]
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
	window := cfg.DebounceWindow
	if window <= 0 {
		window = time.Second
	}
	consumer := &Consumer{
		cfg:        cfg,
		subscriber: subscriber,
		publisher:  publisher,
		runner:     runner,
	}
	debouncer, err := utils.NewTrailingDebouncer(window, consumer.runTask, func(ctx context.Context, err error) {
		logs.ErrorContextf(ctx, "Failed to run worker task: %v", err)
	})
	if err != nil {
		return nil, err
	}
	consumer.debouncer = debouncer
	return consumer, nil
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
	if taskMsg.Body.TaskType != protocol.TaskTypeAgentRun {
		return fmt.Errorf("unsupported worker task type %q", taskMsg.Body.TaskType)
	}
	if err := validateModelConfig(taskMsg.Body.Model); err != nil {
		return err
	}
	modelrouter.DefaultStore().Put(modelConfigFromTask(taskMsg.Body.Model))

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

	c.schedule(ctx, taskMsg)
	return nil
}

func validateModelConfig(model protocol.ModelOptions) error {
	if strings.TrimSpace(model.Provider) == "" {
		return fmt.Errorf("llm provider is required")
	}
	if strings.TrimSpace(model.Model) == "" {
		return fmt.Errorf("llm model is required")
	}
	if strings.TrimSpace(model.APIKey) == "" {
		return fmt.Errorf("llm api_key is required")
	}
	return nil
}

func modelConfigFromTask(model protocol.ModelOptions) modelrouter.UpstreamConfig {
	return modelrouter.UpstreamConfig{
		ModelName:    strings.TrimSpace(model.Model),
		Provider:     strings.TrimSpace(model.Provider),
		BaseURL:      strings.TrimSpace(model.BaseURL),
		BaseURLHasV1: model.BaseURLHasV1,
		APIKey:       strings.TrimSpace(model.APIKey),
		Protocol:     modelrouter.DefaultProtocolForProvider(model.Provider),
	}
}

func (c *Consumer) schedule(ctx context.Context, taskMsg protocol.WorkerTaskMessage) {
	key := sessionTaskKey(taskMsg)
	if key == "" {
		go func() {
			if err := c.runTask(ctx, taskMsg); err != nil {
				logs.ErrorContextf(ctx, "Failed to run worker task: %v", err)
			}
		}()
		return
	}

	c.debouncer.Call(ctx, key, taskMsg)
}

func (c *Consumer) runTask(ctx context.Context, taskMsg protocol.WorkerTaskMessage) error {
	req := RequestFromWorkerTask(taskMsg)
	_, err := c.prepareWorkspace(ctx, taskMsg, req)
	if err != nil {
		return err
	}
	req.EventSink = NewMQStreamSink(c.publisher, taskMsg)

	logs.InfoContextf(ctx,
		"Starting worker task run: task_id=%s run_id=%s runtime=%s assistant_id=%s",
		req.TaskID,
		req.RunID,
		req.Runtime.Kind,
		req.Assistant.ID,
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

func (c *Consumer) prepareWorkspace(ctx context.Context, taskMsg protocol.WorkerTaskMessage, req *agent.RequestContext) (*agentworkspace.TaskWorkspace, error) {
	projectID := strings.TrimSpace(taskMsg.Body.Workspace.ProjectID)
	if projectID == "" {
		workDir, err := agentworkspace.PrepareTempWorkspace()
		if err != nil {
			return nil, err
		}
		req.Runtime.WorkDir = workDir
		return nil, nil
	}
	requestID := strings.TrimSpace(taskMsg.Trace.RequestID)
	if requestID == "" {
		requestID = strings.TrimSpace(taskMsg.ID)
	}
	plan, err := agentworkspace.PrepareTaskWorkspace(ctx, agentworkspace.TaskWorkspaceRequest{
		OrgID:            taskMsg.Route.OrgID,
		ProjectID:        projectID,
		TaskID:           taskMsg.Trace.TaskID,
		RequestID:        requestID,
		RequestedWorkDir: taskMsg.Body.Runtime.WorkDir,
	})
	if err != nil {
		return nil, err
	}
	req.Runtime.WorkDir = plan.EffectiveWorkDir
	return plan, nil
}

func sessionTaskKey(msg protocol.WorkerTaskMessage) string {
	if msg.Route.OrgID == 0 || msg.Route.WorkerID == 0 || strings.TrimSpace(msg.Route.SessionID) == "" {
		return ""
	}
	return fmt.Sprintf("%d:%d:%s", msg.Route.OrgID, msg.Route.WorkerID, strings.TrimSpace(msg.Route.SessionID))
}

func decodeWorkerTask(msg *nats.Msg) (protocol.WorkerTaskMessage, error) {
	var taskMsg protocol.WorkerTaskMessage
	if err := json.Unmarshal(msg.Data, &taskMsg); err != nil {
		return taskMsg, fmt.Errorf("unmarshal worker task: %w", err)
	}
	if taskMsg.Type != "" && taskMsg.Type != protocol.MessageTypeWorkerTask {
		return taskMsg, fmt.Errorf("unexpected worker task message type %q", taskMsg.Type)
	}
	return taskMsg, nil
}

func (c *Consumer) validateRoute(msg protocol.WorkerTaskMessage) error {
	if msg.Route.OrgID != 0 && msg.Route.OrgID != c.cfg.OrgID {
		return fmt.Errorf("task org_id %q does not match worker org_id %q", msg.Route.OrgID, c.cfg.OrgID)
	}
	if msg.Route.WorkerID != 0 && msg.Route.WorkerID != c.cfg.WorkerID {
		return fmt.Errorf("task worker_id %q does not match worker_id %q", msg.Route.WorkerID, c.cfg.WorkerID)
	}
	return nil
}
