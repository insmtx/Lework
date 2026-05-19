package eventengine

import (
	"context"
	"testing"

	"github.com/insmtx/Leros/backend/internal/agent"
	interactionevent "github.com/insmtx/Leros/backend/pkg/event"
	"github.com/nats-io/nats.go"
)

// 测试Orchestrator是否可以初始化并注册默认处理器
func TestOrchestratorInit(t *testing.T) {
	// 创建一个简单的subscriber实现
	subscriber := &mockSubscriber{}
	runner := &mockRunner{}

	orchestrator := NewOrchestrator(subscriber, runner)

	// 验证默认事件处理器被正确注册
	if _, exists := orchestrator.handlers["interaction.github.issue_comment"]; !exists {
		t.Errorf("Expected handler for interaction.github.issue_comment to be registered")
	}

	if _, exists := orchestrator.handlers["interaction.github.pull_request"]; !exists {
		t.Errorf("Expected handler for interaction.github.pull_request to be registered")
	}

	if _, exists := orchestrator.handlers["interaction.github.push"]; !exists {
		t.Errorf("Expected handler for interaction.github.push to be registered")
	}
}

// 辅助方法测试 - 验证注册和获取处理函数
func TestOrchestratorRegisterAndGet(t *testing.T) {
	subscriber := &mockSubscriber{}
	runner := &mockRunner{}
	orchestrator := NewOrchestrator(subscriber, runner)

	// 注册一个自定义处理器
	customTopic := "test.custom.topic"
	called := false
	handler := func(ctx context.Context, event *interactionevent.Event) error {
		called = true
		return nil
	}

	orchestrator.RegisterHandler(customTopic, handler)

	retrievedHandler, err := orchestrator.GetHandler(customTopic)
	if err != nil {
		t.Errorf("Expected to retrieve handler for %s: %v", customTopic, err)
	}

	if retrievedHandler == nil {
		t.Errorf("Expected non-nil handler for %s", customTopic)
	}

	// 调用处理器以验证它正常工作
	testEvent := &interactionevent.Event{EventID: "test"}
	err = retrievedHandler(context.Background(), testEvent)
	if err != nil {
		t.Errorf("Unexpected error when calling handler: %v", err)
	}

	if !called {
		t.Error("Expected handler to be called when invoked through GetHandler")
	}
}

// 简单的mock subscriber实现
type mockSubscriber struct{}

func (ms *mockSubscriber) Subscribe(ctx context.Context, topic string, consumer string, handler func(msg *nats.Msg)) error {
	return nil
}

func (ms *mockSubscriber) SubscribeFrom(ctx context.Context, topic string, startSeq int64, handler func(msg *nats.Msg)) error {
	return nil
}

type mockRunner struct{}

var _ agent.Runner = (*mockRunner)(nil)

func (m *mockRunner) Run(ctx context.Context, req *agent.RequestContext) (*agent.RunResult, error) {
	return &agent.RunResult{
		RunID:  req.RunID,
		Status: agent.RunStatusCompleted,
	}, nil
}
