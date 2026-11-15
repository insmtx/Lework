package api

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/insmtx/lework/backend/internal/controlplane/authz"
	"github.com/insmtx/lework/backend/internal/controlplane/event"
	"github.com/insmtx/lework/backend/internal/controlplane/store"
)

// streamAs 以账号身份（普通用户）建立事件流。
func streamAs(t *testing.T, env *testEnv, userID, target string) *eventStream {
	t.Helper()

	return startEventsAs(t, env, &authz.Principal{
		Kind:   authz.KindUser,
		UserID: userID,
		Role:   roleUser,
	}, target)
}

// streamAsPrincipal 按完整身份建立事件流，用于覆盖管理员与部署级身份。
func streamAsPrincipal(t *testing.T, env *testEnv, p authz.Principal, target string) *eventStream {
	t.Helper()

	return startEventsAs(t, env, &p, target)
}

// publishNodeEvent 发布一条节点级状态事件。
func publishNodeEvent(t *testing.T, env *testEnv, nodeID, eventType string) {
	t.Helper()

	env.hub.Publish(event.Event{NodeID: nodeID, Type: eventType, Time: time.Now()})
}

// waitNodeEvent 反复发布同一节点事件，直到它出现在流里或超时。
//
// 可见集合只在周期刷新或 node.member.* 事件时重算，重算前该事件会被丢弃；
// 重复发布让用例不依赖重算发生的具体时刻。
func waitNodeEvent(t *testing.T, env *testEnv, rec *streamRecorder, nodeID, eventType string) {
	t.Helper()

	marker := `"type":"` + eventType + `"`
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if strings.Contains(rec.text(), marker) {
			return
		}
		publishNodeEvent(t, env, nodeID, eventType)
		time.Sleep(5 * time.Millisecond)
	}
	t.Fatalf("stream body %q does not contain %q", rec.text(), marker)
}

// TestEventsStreamSkipsLiveByDefault 固化默认订阅只收状态事件：总览等常驻
// 连接不应被 token 级增量拖慢。
func TestEventsStreamSkipsLiveByDefault(t *testing.T) {
	env := newTestEnv(t, time.Second)
	node, _, _ := env.addNode("n1")
	stream := startEvents(t, env, "/api/events")
	defer stream.cancel()

	env.hub.Publish(event.Event{
		NodeID:    node.ID,
		SessionID: "ses_1",
		Type:      "session.text.delta",
		Live:      true,
		Time:      time.Now(),
	})
	env.hub.Publish(event.Event{
		NodeID:    node.ID,
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
	node, _, _ := env.addNode("n1")
	stream := startEvents(t, env, "/api/events?live=1")
	defer stream.cancel()

	env.hub.Publish(event.Event{
		NodeID:    node.ID,
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
	node, _, _ := env.addNode("n1")
	stream := startEvents(t, env, "/api/events?session=ses_1")
	defer stream.cancel()

	env.hub.Publish(event.Event{
		NodeID: node.ID,
		Type:   "agent.resync_required",
		Time:   time.Now(),
	})
	waitBody(t, stream.rec, `"type":"agent.resync_required"`)

	env.hub.Publish(event.Event{
		NodeID:    node.ID,
		SessionID: "ses_2",
		Type:      "other-session",
		Time:      time.Now(),
	})
	env.hub.Publish(event.Event{
		NodeID:    node.ID,
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

// TestEventsSkipForeignNodes 固化设计 §9：SSE 只下发可见节点的事件，别人节点
// 的事件连「有没有发生」都不该被观察到。
func TestEventsSkipForeignNodes(t *testing.T) {
	env := newTestEnv(t, time.Second)
	owner := createTestUser(t, env, "owner@x.c", testLoginPassword, "user", "active")
	mine, _, _ := env.addNode("mine")
	foreign, _, _ := env.addNode("theirs")
	mustAddMember(t, env, mine.ID, owner.ID, store.NodeRoleOwner)

	stream := streamAs(t, env, owner.ID, "/api/events")
	defer stream.cancel()

	publishNodeEvent(t, env, foreign.ID, "session.token")
	publishNodeEvent(t, env, mine.ID, "session.mine")

	waitBody(t, stream.rec, `"type":"session.mine"`)
	body := stream.rec.text()
	if strings.Contains(body, foreign.ID) {
		t.Fatalf("stream body %q contains an event of a foreign node", body)
	}

	stream.cancel()
	waitStreamDone(t, stream)
}

// TestEventsDropForeignLifecycleEvents 固化 node.* 生命周期事件同样按可见范围
// 过滤：别人的节点上线/离线不能变成普通账号的情报。
func TestEventsDropForeignLifecycleEvents(t *testing.T) {
	env := newTestEnv(t, time.Second)
	owner := createTestUser(t, env, "owner@x.c", testLoginPassword, "user", "active")
	mine, _, _ := env.addNode("mine")
	foreign, _, _ := env.addNode("theirs")
	mustAddMember(t, env, mine.ID, owner.ID, store.NodeRoleOwner)

	stream := streamAs(t, env, owner.ID, "/api/events")
	defer stream.cancel()

	publishNodeEvent(t, env, foreign.ID, "node.offline")
	publishNodeEvent(t, env, mine.ID, "node.connected")

	waitBody(t, stream.rec, `"type":"node.connected"`)
	body := stream.rec.text()
	if strings.Contains(body, foreign.ID) {
		t.Fatalf("stream body %q contains a lifecycle event of a foreign node", body)
	}

	stream.cancel()
	waitStreamDone(t, stream)
}

// TestEventsUnclaimedNodeVisibleOnlyToAdmin 固化 VisibleNodeIDs 的边界：管理员
// 账号能收到未认领节点的生命周期事件，普通账号不能。
func TestEventsUnclaimedNodeVisibleOnlyToAdmin(t *testing.T) {
	env := newTestEnv(t, time.Second)
	adminUser := createTestUser(t, env, "admin@x.c", testLoginPassword, roleAdmin, "active")
	plainUser := createTestUser(t, env, "user@x.c", testLoginPassword, roleUser, "active")
	unclaimed, _, _ := env.addNode("unclaimed")
	owned, _, _ := env.addNode("owned")
	mustAddMember(t, env, owned.ID, plainUser.ID, store.NodeRoleOwner)

	adminStream := streamAsPrincipal(t, env, authz.Principal{
		Kind:   authz.KindUser,
		UserID: adminUser.ID,
		Role:   roleAdmin,
	}, "/api/events")
	defer adminStream.cancel()

	plainStream := streamAs(t, env, plainUser.ID, "/api/events")
	defer plainStream.cancel()

	publishNodeEvent(t, env, unclaimed.ID, "node.connected")
	// 正向锚点：先用普通用户可见的节点事件证明它的流确实处理过队列，否则
	// 「没收到未认领节点事件」可能只是还没来得及消费。
	publishNodeEvent(t, env, owned.ID, "session.owned")

	waitBody(t, adminStream.rec, `"type":"node.connected"`)
	waitBody(t, plainStream.rec, `"type":"session.owned"`)

	if body := plainStream.rec.text(); strings.Contains(body, unclaimed.ID) {
		t.Fatalf("plain user stream body %q contains an unclaimed node event", body)
	}

	adminStream.cancel()
	waitStreamDone(t, adminStream)
	plainStream.cancel()
	waitStreamDone(t, plainStream)
}

// TestEventsNodeFilterCannotWidenVisibility 固化 ?node= 只能收窄范围：请求不可见
// 节点时整条流保持沉默，而不是错误码或悄悄放行。
func TestEventsNodeFilterCannotWidenVisibility(t *testing.T) {
	env := newTestEnv(t, time.Second)
	owner := createTestUser(t, env, "owner@x.c", testLoginPassword, "user", "active")
	mine, _, _ := env.addNode("mine")
	foreign, _, _ := env.addNode("theirs")
	mustAddMember(t, env, mine.ID, owner.ID, store.NodeRoleOwner)

	allowed := streamAs(t, env, owner.ID, "/api/events?node="+mine.ID)
	defer allowed.cancel()

	forbidden := streamAs(t, env, owner.ID, "/api/events?node="+foreign.ID)
	defer forbidden.cancel()

	publishNodeEvent(t, env, mine.ID, "session.mine")
	publishNodeEvent(t, env, foreign.ID, "session.foreign")

	waitBody(t, allowed.rec, `"type":"session.mine"`)
	allowedBody := allowed.rec.text()
	if strings.Contains(allowedBody, "session.foreign") {
		t.Fatalf("stream body %q contains another node's event", allowedBody)
	}

	// 没有可见节点可以做正向锚点，只能等一段时间后断言整条流只有首帧。
	time.Sleep(100 * time.Millisecond)
	if body := forbidden.rec.text(); body != readyFrame {
		t.Fatalf("forbidden node stream body = %q, want only the ready frame", body)
	}

	allowed.cancel()
	waitStreamDone(t, allowed)
	forbidden.cancel()
	waitStreamDone(t, forbidden)
}

// TestEventsMemberEventRefreshesVisibility 固化 node.member.* 事件触发立即重算：
// 刚共享出来的节点不必等 30 秒定时刷新。
func TestEventsMemberEventRefreshesVisibility(t *testing.T) {
	env := newTestEnv(t, time.Second)
	owner := createTestUser(t, env, "owner@x.c", testLoginPassword, "user", "active")
	shared, _, _ := env.addNode("shared")

	stream := streamAs(t, env, owner.ID, "/api/events")
	defer stream.cancel()

	// 共享先落库，再发一条 node.member.* 事件：重算后该节点事件应当立刻下发。
	mustAddMember(t, env, shared.ID, owner.ID, store.NodeRoleMember)
	publishNodeEvent(t, env, shared.ID, "node.member.granted")
	publishNodeEvent(t, env, shared.ID, "session.shared")

	waitBody(t, stream.rec, `"type":"session.shared"`)

	stream.cancel()
	waitStreamDone(t, stream)
}

// TestEventsRefreshesVisibilityPeriodically 固化「成员变更 30 秒内生效」的另一
// 半：没有 node.member.* 事件时，周期刷新也必须补上授予，并在撤销后收手。
func TestEventsRefreshesVisibilityPeriodically(t *testing.T) {
	env := newTestEnv(t, time.Second)
	// 缩短重算周期，免得用例真的等 30 秒；周期本身由默认值常量约束。
	env.api.eventsRefresh = 10 * time.Millisecond

	owner := createTestUser(t, env, "owner@x.c", testLoginPassword, "user", "active")
	mine, _, _ := env.addNode("mine")
	shared, _, _ := env.addNode("shared")
	mustAddMember(t, env, mine.ID, owner.ID, store.NodeRoleOwner)

	stream := streamAs(t, env, owner.ID, "/api/events")
	defer stream.cancel()

	// 授予节点但不发任何 node.member.* 事件：只能靠周期重算发现。
	mustAddMember(t, env, shared.ID, owner.ID, store.NodeRoleMember)
	waitNodeEvent(t, env, stream.rec, shared.ID, "session.shared")

	// 撤销访问后同理：等若干周期，再确认该节点事件不再下发，而仍在范围内的
	// 节点事件照常到达（用它证明流还活着）。
	if err := env.store.RemoveNodeMember(context.Background(), shared.ID, owner.ID); err != nil {
		t.Fatalf("remove member: %v", err)
	}
	time.Sleep(50 * time.Millisecond)

	publishNodeEvent(t, env, shared.ID, "session.revoked")
	publishNodeEvent(t, env, mine.ID, "session.mine")
	waitBody(t, stream.rec, `"type":"session.mine"`)

	if body := stream.rec.text(); strings.Contains(body, "session.revoked") {
		t.Fatalf("stream body %q still contains an event of a revoked node", body)
	}

	stream.cancel()
	waitStreamDone(t, stream)
}

// TestEventsNewNodeLifecycleEventRefreshesVisibility 固化「新节点不必等刷新周期」：
// 订阅之后才登记的节点，其 node.connected 应立即下发，否则界面最多晚 30 秒才
// 知道节点上线。
func TestEventsNewNodeLifecycleEventRefreshesVisibility(t *testing.T) {
	env := newTestEnv(t, time.Second)
	// 周期放大到用例不可能等到的长度：只有事件触发的重算才能让用例通过。
	env.api.eventsRefresh = time.Hour

	owner := createTestUser(t, env, "owner@x.c", testLoginPassword, "user", "active")
	stream := streamAs(t, env, owner.ID, "/api/events")
	defer stream.cancel()

	// 订阅建立之后才出现的节点（扫码认领、首次上线等）。
	fresh, _, _ := env.addNode("fresh")
	mustAddMember(t, env, fresh.ID, owner.ID, store.NodeRoleOwner)
	publishNodeEvent(t, env, fresh.ID, "node.connected")

	waitBody(t, stream.rec, `"type":"node.connected"`)

	stream.cancel()
	waitStreamDone(t, stream)
}

// TestEventsRecomputeFailureKeepsStream 固化重算失败时的降级行为：一次数据库
// 抖动只能让可见集合停在旧值，不能中断事件流、也不能放行集合外的事件。
func TestEventsRecomputeFailureKeepsStream(t *testing.T) {
	env := newTestEnv(t, time.Second)
	owner := createTestUser(t, env, "owner@x.c", testLoginPassword, "user", "active")
	mine, _, _ := env.addNode("mine")
	foreign, _, _ := env.addNode("theirs")
	mustAddMember(t, env, mine.ID, owner.ID, store.NodeRoleOwner)

	stream := streamAs(t, env, owner.ID, "/api/events")
	defer stream.cancel()

	// 基线：可见节点的事件能正常到达。
	publishNodeEvent(t, env, mine.ID, "session.before")
	waitBody(t, stream.rec, `"type":"session.before"`)

	// 关库让后续每一次重算都失败；随后用 node.member.* 事件显式触发一次。
	if err := env.store.Close(); err != nil {
		t.Fatalf("close store: %v", err)
	}
	publishNodeEvent(t, env, mine.ID, "node.member.granted")

	publishNodeEvent(t, env, foreign.ID, "session.foreign")
	publishNodeEvent(t, env, mine.ID, "session.after")
	waitBody(t, stream.rec, `"type":"session.after"`)

	body := stream.rec.text()
	if strings.Contains(body, "session.foreign") {
		t.Fatalf("stream body %q contains a foreign node event after a failed recompute", body)
	}
	if strings.Contains(body, "event: resync") {
		t.Fatalf("stream body %q was resynced by a failed recompute", body)
	}

	stream.cancel()
	waitStreamDone(t, stream)
}

// TestEventsResyncFrameSurvivesEmptyVisibleSet 固化 resync 帧不受节点过滤影响：
// 可见集合为空（这里由不可见的 ?node= 造出）时，慢消费者仍要收到 resync，
// 否则页面会永远停在旧数据上。
func TestEventsResyncFrameSurvivesEmptyVisibleSet(t *testing.T) {
	env := newTestEnv(t, time.Second)
	owner := createTestUser(t, env, "owner@x.c", testLoginPassword, "user", "active")
	mine, _, _ := env.addNode("mine")
	foreign, _, _ := env.addNode("theirs")
	mustAddMember(t, env, mine.ID, owner.ID, store.NodeRoleOwner)

	blocking := newBlockingStreamRecorder()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	principal := authz.Principal{Kind: authz.KindUser, UserID: owner.ID, Role: roleUser}
	req := httptest.NewRequest(http.MethodGet, "/api/events?node="+foreign.ID, nil).WithContext(ctx)
	req = req.WithContext(authz.WithPrincipal(req.Context(), principal))

	done := make(chan struct{})
	go func() {
		defer close(done)
		env.mux.ServeHTTP(blocking, req)
	}()

	select {
	case <-blocking.flushed:
	case <-time.After(2 * time.Second):
		t.Fatal("SSE handler did not flush headers")
	}

	// 超过订阅缓冲（256）的事件会让 Hub 关闭该订阅通道；这些事件全部不在
	// 可见集合内，正好覆盖「过滤生效」与「resync 仍下发」同时成立。
	for i := 0; i < 300; i++ {
		publishNodeEvent(t, env, mine.ID, "burst")
	}

	close(blocking.block)

	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("SSE handler did not return after subscription closed")
	}

	body := blocking.text()
	if !strings.HasSuffix(body, resyncFrame) {
		t.Fatalf("stream body does not end with resync frame: %q", body)
	}
	if strings.Contains(body, `"type":"burst"`) {
		t.Fatalf("stream body %q contains an event that node filtering must drop", body)
	}
}
