package client

import (
	"context"
	"fmt"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/insmtx/Leros/backend/agent"
	"github.com/insmtx/Leros/backend/config"
	assistantdomain "github.com/insmtx/Leros/backend/internal/assistant/domain"
	"github.com/ygpkg/yg-go/logs"
)

type WorkerClient struct {
	runtime   agent.Runtime
	config    *WorkerConfig
	daID      uint
	workerID  uint
	startedAt time.Time
	status    string
	wsClient  *WSClient
}

type WorkerConfig struct {
	Runtime            agent.Runtime
	LLMConfig          *config.LLMConfig
	ServerAddr         string
	DigitalAssistantID uint
	WorkerID           uint
}

func NewWorker(ctx context.Context, cfg *WorkerConfig) (*WorkerClient, error) {
	if cfg == nil {
		return nil, fmt.Errorf("worker config is required")
	}

	workerID := cfg.WorkerID
	if workerID == 0 {
		workerID = cfg.DigitalAssistantID
	}

	w := &WorkerClient{
		config:    cfg,
		daID:      cfg.DigitalAssistantID,
		workerID:  workerID,
		startedAt: time.Now(),
		status:    "initialized",
	}

	// TODO: worker与server交互时重新实现 config 接收
	if cfg.ServerAddr != "" {
		w.wsClient = NewWSClient(cfg.ServerAddr, cfg.DigitalAssistantID,
			WithWorkerID(cfg.DigitalAssistantID),
		)
	}

	return w, nil
}

func (w *WorkerClient) Run(ctx context.Context, req *assistantdomain.RunRequest) (*assistantdomain.RunResult, error) {
	if w == nil || w.runtime == nil {
		return nil, fmt.Errorf("worker runtime is not initialized")
	}

	w.status = "processing"

	messages := make([]agent.Message, 0, len(req.Conversation.Messages))
	for _, message := range req.Conversation.Messages {
		messages = append(messages, agent.Message{Role: message.Role, Content: message.Content})
	}
	execution := agent.ExecutionRequest{
		ExecutionID:  req.RunID,
		TraceID:      req.TraceID,
		Runtime:      w.runtime.Name(),
		SessionKey:   req.Conversation.ID,
		InstanceKey:  req.Assistant.ID,
		SystemPrompt: req.SystemPrompt,
		Prompt:       assistantdomain.BuildUserInput(req),
		Messages:     messages,
		Model: agent.ModelConfig{
			Provider: req.Model.Provider,
			Model:    req.Model.Model,
			APIKey:   req.Model.APIKey,
			BaseURL:  req.Model.BaseURL,
		},
		Policy: agent.ExecutionPolicy{
			PermissionMode: req.Policy.PermissionMode,
			MaxSteps:       req.Runtime.MaxStep,
			AllowedTools:   append([]string(nil), req.Capability.AllowedTools...),
		},
		Filesystem: agent.FilesystemContext{
			WorkDir: req.Runtime.WorkDir,
			RepoDir: req.Workspace.RepoDir,
		},
	}

	runtimeResult, err := w.runtime.Execute(ctx, execution, req.EventSink)
	if err != nil {
		w.status = "error"
		return nil, err
	}

	result := &assistantdomain.RunResult{
		RunID:       req.RunID,
		TraceID:     req.TraceID,
		Status:      assistantdomain.RunStatusCompleted,
		Message:     runtimeResult.Message,
		Usage:       runtimeResult.Usage,
		ToolCalls:   runtimeResult.ToolCalls,
		StartedAt:   time.Now(),
		CompletedAt: time.Now().UTC(),
		Metadata:    &assistantdomain.RunMetadata{ProviderID: runtimeResult.ProviderConversationID},
	}

	w.status = "idle"
	return result, nil
}

func (w *WorkerClient) Start(ctx context.Context) error {
	w.status = "running"
	logs.Infof("Worker %s started", w.workerID)

	if w.wsClient != nil {
		if err := w.wsClient.Connect(ctx); err != nil {
			logs.Warnf("Failed to connect to server WebSocket: %v", err)
		} else {
			logs.Info("Connected to server via WebSocket")
		}
	}

	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, syscall.SIGINT, syscall.SIGTERM)

	select {
	case <-ctx.Done():
		logs.Info("Worker context cancelled")
		return w.Shutdown(ctx)
	case sig := <-sigChan:
		logs.Infof("Received signal %v, shutting down", sig)
		return w.Shutdown(ctx)
	}
}

func (w *WorkerClient) Shutdown(ctx context.Context) error {
	logs.Info("Worker shutting down...")
	w.status = "stopping"

	if w.wsClient != nil {
		w.wsClient.Close()
	}

	return nil
}

func (w *WorkerClient) GetWorkerID() uint {
	return w.workerID
}

func (w *WorkerClient) GetStartedAt() time.Time {
	return w.startedAt
}

func (w *WorkerClient) GetStatus() string {
	return w.status
}
