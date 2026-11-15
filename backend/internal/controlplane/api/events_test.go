package api

import (
	"strings"
	"testing"
	"time"

	"github.com/insmtx/lework/backend/internal/controlplane/event"
)

// TestEventsStreamSkipsLiveByDefault 固化默认订阅只收状态事件：总览等常驻
// 连接不应被 token 级增量拖慢。
func TestEventsStreamSkipsLiveByDefault(t *testing.T) {
	env := newTestEnv(t, time.Second)
	stream := startEvents(t, env, "/api/events")
	defer stream.cancel()

	env.hub.Publish(event.Event{
		NodeID:    "node_1",
		SessionID: "ses_1",
		Type:      "session.text.delta",
		Live:      true,
		Time:      time.Now(),
	})
	env.hub.Publish(event.Event{
		NodeID:    "node_1",
		SessionID: "ses_1",
		Type:      "session.execution.succeeded",
		Time:      time.Now(),
	})

	waitBody(t, stream.rec, `"type":"session.execution.succeeded"`)
	body := stream.rec.text()
	if strings.Contains(body, "session.text.delta") {
		t.Fatalf("stream body %q contains a live event without live=1", body)
	}

	stream.cancel()
	waitStreamDone(t, stream)
}

// TestEventsStreamDeliversLiveWhenRequested 固化 live=1 才订阅增量事件。
func TestEventsStreamDeliversLiveWhenRequested(t *testing.T) {
	env := newTestEnv(t, time.Second)
	stream := startEvents(t, env, "/api/events?live=1")
	defer stream.cancel()

	env.hub.Publish(event.Event{
		NodeID:    "node_1",
		SessionID: "ses_1",
		Type:      "session.text.delta",
		Data:      []byte(`{"delta":"你"}`),
		Live:      true,
		Time:      time.Now(),
	})

	waitBody(t, stream.rec, `"type":"session.text.delta"`)
	body := stream.rec.text()
	if !strings.Contains(body, `"live":true`) {
		t.Fatalf("stream body %q is missing live=true", body)
	}

	stream.cancel()
	waitStreamDone(t, stream)
}

// TestEventsSessionFilterKeepsResyncEvent 固化会话过滤只丢弃「带 session_id
// 且不匹配」的事件：节点级 agent.resync_required 必须照常下发，否则断档后
// 页面永远不会重拉。
func TestEventsSessionFilterKeepsResyncEvent(t *testing.T) {
	env := newTestEnv(t, time.Second)
	stream := startEvents(t, env, "/api/events?session=ses_1")
	defer stream.cancel()

	env.hub.Publish(event.Event{
		NodeID: "node_1",
		Type:   "agent.resync_required",
		Time:   time.Now(),
	})
	waitBody(t, stream.rec, `"type":"agent.resync_required"`)

	env.hub.Publish(event.Event{
		NodeID:    "node_1",
		SessionID: "ses_2",
		Type:      "other-session",
		Time:      time.Now(),
	})
	env.hub.Publish(event.Event{
		NodeID:    "node_1",
		SessionID: "ses_1",
		Type:      "matching-session",
		Time:      time.Now(),
	})
	waitBody(t, stream.rec, `"type":"matching-session"`)

	body := stream.rec.text()
	if strings.Contains(body, "other-session") {
		t.Fatalf("stream body %q contains an event for another session", body)
	}

	stream.cancel()
	waitStreamDone(t, stream)
}
