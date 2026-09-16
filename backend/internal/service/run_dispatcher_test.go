package service

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/nats-io/nats.go"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"

	"github.com/insmtx/Leros/backend/types"
)

// wakeRecordingPublisher 记录被发布的任务主题；并发安全，供派发器 goroutine 使用。
type wakeRecordingPublisher struct {
	mu      sync.Mutex
	topics  []string
	publish chan struct{}
}

func newWakeRecordingPublisher() *wakeRecordingPublisher {
	return &wakeRecordingPublisher{publish: make(chan struct{}, 8)}
}

func (p *wakeRecordingPublisher) Publish(_ context.Context, topic string, _ any) error {
	p.mu.Lock()
	p.topics = append(p.topics, topic)
	p.mu.Unlock()
	select {
	case p.publish <- struct{}{}:
	default:
	}
	return nil
}

func (p *wakeRecordingPublisher) Request(_ context.Context, _ string, _ any) (*nats.Msg, error) {
	return nil, nil
}

func (p *wakeRecordingPublisher) publishedCount() int {
	p.mu.Lock()
	defer p.mu.Unlock()
	return len(p.topics)
}

func newRunDispatcherTestDB(t *testing.T) *gorm.DB {
	t.Helper()
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	if err != nil {
		t.Fatalf("open test db: %v", err)
	}
	if err := db.AutoMigrate(&types.ReliableTask{}); err != nil {
		t.Fatalf("migrate reliable tasks: %v", err)
	}
	return db
}

func seedPendingRunTask(t *testing.T, db *gorm.DB) {
	t.Helper()
	now := time.Now().UTC()
	task := &types.ReliableTask{
		TaskID:        "task-" + now.Format("150405.000000"),
		Kind:          "worker.run",
		Destination:   "org.1.session.1.run",
		ContentType:   "application/json",
		Payload:       []byte(`{"id":"task"}`),
		SourceType:    "session_message",
		SourceID:      "1",
		Status:        types.ReliableTaskPending,
		NextAttemptAt: now,
		DeadlineAt:    now.Add(30 * time.Minute),
	}
	if err := db.Create(task).Error; err != nil {
		t.Fatalf("seed reliable task: %v", err)
	}
}

// 发件箱唤醒的核心契约：事务提交后唤醒派发器，任务不必等到下一次兜底轮询。
func TestReliableTaskDispatcherPublishesOnNotify(t *testing.T) {
	db := newRunDispatcherTestDB(t)
	publisher := newWakeRecordingPublisher()
	dispatcher := NewReliableTaskDispatcher(db, publisher)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go dispatcher.Run(ctx)

	// 让首次扫描先跑完，之后的发布只可能来自唤醒信号或兜底轮询。
	waitForScan(t)

	seedPendingRunTask(t, db)
	start := time.Now()
	dispatcher.NotifyRunDispatch()

	select {
	case <-publisher.publish:
	case <-time.After(3 * time.Second):
		t.Fatalf("task was not published after notify")
	}
	if elapsed := time.Since(start); elapsed >= reliableTaskScanInterval {
		t.Fatalf("publish took %s, expected to beat the %s fallback poll interval", elapsed, reliableTaskScanInterval)
	}
}

// 对照组：没有唤醒信号时必须等兜底轮询，用于证明上一条测试确实在验证唤醒路径。
func TestReliableTaskDispatcherWaitsForPollWithoutNotify(t *testing.T) {
	db := newRunDispatcherTestDB(t)
	publisher := newWakeRecordingPublisher()
	dispatcher := NewReliableTaskDispatcher(db, publisher)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go dispatcher.Run(ctx)
	waitForScan(t)

	seedPendingRunTask(t, db)

	select {
	case <-publisher.publish:
		t.Fatalf("task published without notify; expected to wait for the fallback poll")
	case <-time.After(300 * time.Millisecond):
	}
}

// 通知必须非阻塞：容量为 1 的通道在重复唤醒时不能阻塞调用方（消息写入路径）。
func TestNotifyRunDispatchIsNonBlocking(t *testing.T) {
	dispatcher := NewReliableTaskDispatcher(nil, nil)

	done := make(chan struct{})
	go func() {
		for range 10 {
			dispatcher.NotifyRunDispatch()
		}
		close(done)
	}()

	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatalf("NotifyRunDispatch blocked")
	}
}

// 零值/未初始化派发器不得 panic：服务可能在未配置消费器时被调用。
func TestNotifyRunDispatchNilReceiver(t *testing.T) {
	var dispatcher *ReliableTaskDispatcher
	dispatcher.NotifyRunDispatch()
}

// waitForScan 让派发器完成首次扫描并进入阻塞等待。
// Run 会先同步执行一次 scan 再 select，因此短暂让出调度即可越过该窗口；
// 之后的发布只可能来自唤醒信号或兜底轮询。
func waitForScan(t *testing.T) {
	t.Helper()
	time.Sleep(50 * time.Millisecond)
}
