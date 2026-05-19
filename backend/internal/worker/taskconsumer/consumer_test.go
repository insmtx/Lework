package taskconsumer

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/nats-io/nats.go"

	"github.com/insmtx/Leros/backend/internal/agent/runtime/events"
	"github.com/insmtx/Leros/backend/internal/infra/mq"
	"github.com/insmtx/Leros/backend/pkg/dm"
)

// const AgentRuntime = "leros"

const AgentRuntime = "claude"

// const AgentRuntime = "codex"

func TestPublishWorkerTaskMessageToNATS(t *testing.T) {
	natsURL := getenv("LEROS_TEST_NATS_URL", "nats://localhost:4222")
	orgID := getenvUint("LEROS_TEST_ORG_ID", 1)
	workerID := getenvUint("LEROS_TEST_WORKER_ID", 1)
	sessionID := getenv("LEROS_TEST_SESSION_ID", "session_1")
	healthURL := getenv("LEROS_TEST_WORKER_HEALTH_URL", "http://127.0.0.1:8081/health")

	checkWorkerHealthOrSkip(t, healthURL, orgID, workerID)

	bus, err := mq.NewNATS(natsURL)
	if err != nil {
		t.Skipf("skip real NATS publish test: %v", err)
	}
	defer bus.Close()

	topic, _ := dm.WorkerTaskSubject(orgID, workerID)
	streamTopic, _ := dm.SessionResultStreamSubject(orgID, sessionID)
	completedTopic, _ := dm.SessionMessageCompletedSubject(orgID, sessionID)

	task := newTestWorkerTaskMessage(t, orgID, workerID, sessionID)

	ctx, cancel := context.WithTimeout(context.Background(), getenvDuration("LEROS_TEST_AGENT_RUN_TIMEOUT", 2*time.Minute))
	defer cancel()

	receiveReady := make(chan error, 1)
	receiveDone := make(chan error, 1)
	go func() {
		receiveDone <- receiveWorkerTaskReply(ctx, t, bus, streamTopic, completedTopic, task.Trace.TaskID, task.Trace.RunID, receiveReady)
	}()

	if err := <-receiveReady; err != nil {
		t.Skipf("skip real NATS publish test: subscribe reply topics completed=%s: %v", completedTopic, err)
	}

	sendWorkerTaskMessage(ctx, t, bus, natsURL, topic, task)

	if err := <-receiveDone; err != nil {
		t.Fatal(err)
	}
}

// newTestWorkerTaskMessage 构造真实 NATS 集成测试使用的 worker.task 消息。
// 这里显式生成 trace/task/run 标识，后续接收回复时只认这次测试发出的任务，避免被同一 topic 上其他消息干扰。
func newTestWorkerTaskMessage(t *testing.T, orgID uint, workerID uint, sessionID string) events.WorkerTaskMessage {
	t.Helper()

	return events.WorkerTaskMessage{
		ID:        randomTestID(t, "msg"),
		Type:      events.MessageTypeWorkerTask,
		CreatedAt: time.Now().UTC(),
		Trace: events.TraceContext{
			TraceID:   randomTestID(t, "trace"),
			RequestID: randomTestID(t, "request"),
			TaskID:    randomTestID(t, "task"),
			RunID:     randomTestID(t, "run"),
		},
		Route: events.RouteContext{
			OrgID:     orgID,
			SessionID: sessionID,
			WorkerID:  workerID,
		},
		Body: events.WorkerTaskBody{
			TaskType: events.TaskTypeAgentRun,
			Actor: events.ActorContext{
				UserID:      "user_test",
				DisplayName: "Test User",
				Channel:     "go_test",
			},
			Execution: events.ExecutionTarget{
				AssistantID: "assistant_test",
				AgentID:     "agent_test",
				Tools:       []string{},
			},
			Input: events.TaskInput{
				Type: events.InputTypeTaskInstruction,
				Text: "选择合适的工具查询当前系统时间，先告诉我你要怎么查，再执行操作，查询完毕后告诉我几点了，生成一个200字的报告",
			},
			Runtime: events.RuntimeOptions{
				Kind:    AgentRuntime,
				WorkDir: ".",
			},
			Model: events.ModelOptions{
				ID: 1,
			},
		},
		Metadata: map[string]any{
			"source": "go_test",
		},
	}
}

// sendWorkerTaskMessage 负责把测试消息发送到 worker 任务 topic。
// 发送日志保留关键链路 ID，便于和 worker 日志、回复 topic 中的 trace 字段对齐排查。
func sendWorkerTaskMessage(ctx context.Context, t *testing.T, publisher mq.Publisher, natsURL string, topic string, msg events.WorkerTaskMessage) {
	t.Helper()

	if err := publisher.Publish(ctx, topic, msg); err != nil {
		t.Fatalf("Publish(%q) error = %v", topic, err)
	}
	t.Logf(
		"published worker task:\n  topic: %s\n  nats_url: %s\n  message_id: %s\n  trace_id: %s\n  request_id: %s\n  task_id: %s\n  run_id: %s",
		topic,
		natsURL,
		msg.ID,
		msg.Trace.TraceID,
		msg.Trace.RequestID,
		msg.Trace.TaskID,
		msg.Trace.RunID,
	)
}

// receiveWorkerTaskReply 订阅 agent 运行回复 topic，并等待当前测试任务的完成消息。
// completed topic 使用 JetStream 订阅以匹配最终落盘语义。
func receiveWorkerTaskReply(ctx context.Context, t *testing.T, subscriber mq.Subscriber, streamTopic string, completedTopic string, taskID string, runID string, ready chan<- error) error {
	t.Helper()

	completedCh := make(chan events.MessageStreamMessage, 1)

	// 注意：Subscribe 是阻塞调用，会一直运行直到 context 取消。
	// 因此在启动订阅后立即发送 ready，告知调用者订阅已启动（而非已完成）。
	go func() {
		// 先发送 ready，表示订阅尝试已开始
		ready <- nil

		err := subscriber.SubscribeFrom(ctx, streamTopic, 0, func(natsMsg *nats.Msg) {
			var streamMsg events.MessageStreamMessage
			if err := json.Unmarshal(natsMsg.Data, &streamMsg); err != nil {
				t.Logf("\ntopic:\n【%s】\nmalformed:%v\n%s\n\n", streamTopic, err, string(natsMsg.Data))
				return
			}
			t.Logf("\ntopic:\n【%s】\n%s:%s\n%s\n\n",
				streamTopic,
				streamMsg.Body.Event,
				streamMsg.Body.Payload.Content,
				string(natsMsg.Data),
			)
		})
		// Subscribe 阻塞直到 context 取消，这里仅记录错误
		if err != nil {
			t.Logf("stream topic subscription error: %v", err)
		}
	}()

	go func() {
		// 先发送 ready，表示订阅尝试已开始
		ready <- nil

		err := subscriber.SubscribeFrom(ctx, completedTopic, 0, func(natsMsg *nats.Msg) {
			var completedMsg events.MessageStreamMessage
			if err := json.Unmarshal(natsMsg.Data, &completedMsg); err != nil {
				t.Logf("\ntopic:\n【%s】\nmalformed:%v\n%s\n\n", completedTopic, err, string(natsMsg.Data))
				return
			}
			t.Logf("\ntopic:\n【%s】\n%s:%s\n%s\n\n",
				completedTopic,
				completedMsg.Body.Event,
				completedMsg.Body.Payload.Content,
				string(natsMsg.Data),
			)
			if completedMsg.Trace.TaskID != taskID || completedMsg.Trace.RunID != runID {
				return
			}
			if completedMsg.Body.Event == events.StreamEventRunCompleted {
				completedPayload, err := runCompletedPayloadFromCompletedMessage(completedMsg)
				if err != nil {
					t.Logf("decode run completed payload from %s: %v", completedTopic, err)
				} else if payloadJSON, err := json.MarshalIndent(completedPayload, "", "  "); err != nil {
					t.Logf("marshal run completed payload json from %s: %v", completedTopic, err)
				} else {
					t.Logf("run completed payload json from %s:\n%s", completedTopic, string(payloadJSON))
				}
			}
			select {
			case completedCh <- completedMsg:
			case <-ctx.Done():
			default:
				t.Logf("drop completed message because result channel is full: event=%s seq=%d", completedMsg.Body.Event, completedMsg.Body.Seq)
			}
		})
		// Subscribe 阻塞直到 context 取消，这里仅记录错误
		if err != nil {
			t.Logf("completed topic subscription error: %v", err)
		}
	}()

	for {
		select {
		case completedMsg := <-completedCh:
			switch completedMsg.Body.Event {
			case events.StreamEventRunCompleted:
				return nil
			case events.StreamEventRunFailed:
				if completedMsg.Body.Error != nil {
					return fmt.Errorf("worker run failed: %s", completedMsg.Body.Error.Message)
				}
				return fmt.Errorf("worker run failed: %s", completedMsg.Body.Payload.Content)
			default:
				return fmt.Errorf("unexpected session completed event on %s: %s", completedTopic, completedMsg.Body.Event)
			}
		case <-ctx.Done():
			return fmt.Errorf("timed out waiting for session completed event on %s: %w", completedTopic, ctx.Err())
		}
	}
}

func runCompletedPayloadFromCompletedMessage(msg events.MessageStreamMessage) (events.RunCompletedPayload, error) {
	if msg.Body.RunCompleted != nil {
		return *msg.Body.RunCompleted, nil
	}

	content := strings.TrimSpace(msg.Body.Payload.Content)
	if content == "" {
		return events.RunCompletedPayload{}, fmt.Errorf("run completed payload is empty")
	}

	var payload events.RunCompletedPayload
	if err := json.Unmarshal([]byte(content), &payload); err != nil {
		return events.RunCompletedPayload{}, err
	}
	return payload, nil
}

type workerHealthResponse struct {
	Status   string `json:"status"`
	Healthy  bool   `json:"healthy"`
	OrgID    uint   `json:"org_id"`
	WorkerID uint   `json:"worker_id"`
}

func checkWorkerHealthOrSkip(t *testing.T, healthURL string, wantOrgID uint, wantWorkerID uint) {
	t.Helper()

	client := &http.Client{Timeout: 3 * time.Second}
	resp, err := client.Get(healthURL)
	if err != nil {
		t.Skipf("skip real NATS publish test: worker health check unavailable at %s: %v", healthURL, err)
	}
	defer resp.Body.Close()

	var health workerHealthResponse
	if err := json.NewDecoder(resp.Body).Decode(&health); err != nil {
		t.Skipf("skip real NATS publish test: decode worker health response from %s: %v", healthURL, err)
	}
	if resp.StatusCode != http.StatusOK || !health.Healthy || strings.ToLower(health.Status) != "healthy" {
		t.Skipf("skip real NATS publish test: worker is not healthy: status_code=%d status=%q healthy=%t", resp.StatusCode, health.Status, health.Healthy)
	}
	if health.OrgID != wantOrgID || health.WorkerID != wantWorkerID {
		t.Skipf("skip real NATS publish test: worker identity mismatch: got org_id=%d worker_id=%d, want org_id=%d worker_id=%d",
			health.OrgID,
			health.WorkerID,
			wantOrgID,
			wantWorkerID,
		)
	}
}

func getenv(key string, fallback string) string {
	value := os.Getenv(key)
	if value == "" {
		return fallback
	}
	return value
}

func getenvUint(key string, fallback uint) uint {
	valueStr := os.Getenv(key)
	if valueStr == "" {
		return fallback
	}
	value, err := strconv.ParseUint(valueStr, 10, 32)
	if err != nil {
		return fallback
	}
	return uint(value)
}

func getenvDuration(key string, fallback time.Duration) time.Duration {
	valueStr := os.Getenv(key)
	if valueStr == "" {
		return fallback
	}
	duration, err := time.ParseDuration(valueStr)
	if err != nil {
		return fallback
	}
	return duration
}

func randomTestID(t *testing.T, prefix string) string {
	t.Helper()

	var buf [8]byte
	if _, err := rand.Read(buf[:]); err != nil {
		t.Fatalf("generate %s id: %v", prefix, err)
	}
	return fmt.Sprintf("%s_test_agent_run_%s", prefix, hex.EncodeToString(buf[:]))
}
