package push

import (
	"context"
	"encoding/json"
	"errors"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/insmtx/lework/backend/internal/controlplane/event"
	"github.com/insmtx/lework/backend/internal/controlplane/store"
	"github.com/insmtx/lework/backend/internal/infra/mq/mqtest"
	"github.com/insmtx/lework/backend/pkg/messaging"
)

type fakeDevices struct {
	devices []store.Device
	err     error
}

// ListDevicesForNode 忽略节点返回固定设备：多数用例只关心「有设备可推」。
// 节点维度的过滤由 scopedDevices 与 store 的 ListDevicesForNode 用例覆盖。
func (f fakeDevices) ListDevicesForNode(context.Context, string) ([]store.Device, error) {
	return f.devices, f.err
}

// scopedDevices 按节点返回各自的成员设备，模拟 store 里 devices JOIN
// node_members 的过滤结果；同时记录调用的节点 id 供断言。
type scopedDevices struct {
	mu     sync.Mutex
	byNode map[string][]store.Device
	asked  []string
}

func (s *scopedDevices) ListDevicesForNode(_ context.Context, nodeID string) ([]store.Device, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.asked = append(s.asked, nodeID)
	return s.byNode[nodeID], nil
}

func (s *scopedDevices) nodes() []string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]string(nil), s.asked...)
}

type fakeSender struct {
	mu      sync.Mutex
	batches [][]Message
	err     error
}

func (f *fakeSender) Send(_ context.Context, msgs []Message) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.batches = append(f.batches, msgs)
	return f.err
}

func (f *fakeSender) count() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return len(f.batches)
}

func (f *fakeSender) firstBatch() []Message {
	f.mu.Lock()
	defer f.mu.Unlock()
	if len(f.batches) == 0 {
		return nil
	}
	return f.batches[0]
}

// batch 返回第 i 批消息，越界时返回 nil，避免直接读 batches 时绕过锁。
func (f *fakeSender) batch(i int) []Message {
	f.mu.Lock()
	defer f.mu.Unlock()
	if i < 0 || i >= len(f.batches) {
		return nil
	}
	return f.batches[i]
}

func permissionAsked(node, session, permission string) event.Event {
	data, _ := json.Marshal(map[string]string{
		"id":      permission,
		"action":  "bash",
		"message": "run tests",
	})
	return event.Event{NodeID: node, SessionID: session, Type: "permission.asked", Data: data, Time: time.Now()}
}

func TestNotifierSendsOneMessagePerDevice(t *testing.T) {
	sender := &fakeSender{}
	n := NewNotifier(fakeDevices{devices: []store.Device{
		{ID: "d1", PushToken: "tok-a", Platform: "android"},
		{ID: "d2", PushToken: "tok-b", Platform: "android"},
	}}, sender, func(string) string { return "dev-box" })

	n.Handle(context.Background(), permissionAsked("node_1", "ses_1", "perm_1"))

	if sender.count() != 1 || len(sender.firstBatch()) != 2 {
		t.Fatalf("batches = %+v, want one batch of two", sender.batches)
	}
	msg := sender.firstBatch()[0]
	if msg.To != "tok-a" || msg.Tag != "ses_1" || msg.ChannelID != "permissions-v3" || msg.Priority != "high" {
		t.Fatalf("message = %+v", msg)
	}
	if msg.Data["node"] != "node_1" || msg.Data["session"] != "ses_1" || msg.Data["permission_id"] != "perm_1" {
		t.Fatalf("data = %+v", msg.Data)
	}
	if !strings.Contains(msg.Title, "dev-box") {
		t.Fatalf("title = %q, want node label", msg.Title)
	}
	if !strings.Contains(msg.Body, "bash") {
		t.Fatalf("body = %q, want the permission action", msg.Body)
	}
}

// TestNotifierPushesOnlyDevicesOfTheEventNode 证明通知器把事件里的节点交给
// 设备源，并且只发设备源针对该节点返回的设备：另一个账号的手机不会收到
// 别人节点上的审批。
func TestNotifierPushesOnlyDevicesOfTheEventNode(t *testing.T) {
	sender := &fakeSender{}
	devices := &scopedDevices{byNode: map[string][]store.Device{
		"node_1": {{ID: "d1", PushToken: "tok-member", Platform: "ios"}},
		"node_2": {{ID: "d2", PushToken: "tok-stranger", Platform: "ios"}},
	}}
	n := NewNotifier(devices, sender, func(string) string { return "n" })

	n.Handle(context.Background(), permissionAsked("node_1", "ses_1", "perm_1"))

	if got := devices.nodes(); len(got) != 1 || got[0] != "node_1" {
		t.Fatalf("device source asked for %v, want [node_1]", got)
	}
	batch := sender.firstBatch()
	if len(batch) != 1 || batch[0].To != "tok-member" {
		t.Fatalf("batch = %+v, want exactly the member device", batch)
	}

	// 另一个节点的事件只推给那个节点的成员设备，不回退到全量。
	n.Handle(context.Background(), permissionAsked("node_2", "ses_2", "perm_2"))

	if got := sender.count(); got != 2 {
		t.Fatalf("pushes = %d, want 2", got)
	}
	second := sender.batch(1)
	if len(second) != 1 || second[0].To != "tok-stranger" {
		t.Fatalf("second batch = %+v, want the other node's member device", second)
	}
}

func TestNotifierPrefersSessionTitle(t *testing.T) {
	sender := &fakeSender{}
	n := NewNotifier(fakeDevices{devices: []store.Device{{ID: "d1", PushToken: "tok"}}}, sender,
		func(string) string { return "dev-box" },
		WithSessionTitle(func(nodeID, sessionID string) string {
			if nodeID != "node_1" || sessionID != "ses_1" {
				t.Fatalf("sessionTitle(%q, %q)", nodeID, sessionID)
			}
			return "审查移动端 P1"
		}))

	n.Handle(context.Background(), permissionAsked("node_1", "ses_1", "perm_1"))

	batch := sender.firstBatch()
	if len(batch) != 1 {
		t.Fatalf("batch = %+v, want one message", batch)
	}
	if !strings.Contains(batch[0].Title, "审查移动端 P1") {
		t.Fatalf("title = %q, want the session title", batch[0].Title)
	}
}

func TestNotifierFallsBackToNodeLabelWithoutSessionTitle(t *testing.T) {
	sender := &fakeSender{}
	n := NewNotifier(fakeDevices{devices: []store.Device{{ID: "d1", PushToken: "tok"}}}, sender,
		func(string) string { return "dev-box" },
		WithSessionTitle(func(string, string) string { return "  " }))

	n.Handle(context.Background(), permissionAsked("node_1", "ses_1", "perm_1"))

	batch := sender.firstBatch()
	if len(batch) != 1 || !strings.Contains(batch[0].Title, "dev-box") {
		t.Fatalf("batch = %+v, want the node label in the title", batch)
	}
}

func TestNotifierIgnoresOtherEvents(t *testing.T) {
	sender := &fakeSender{}
	n := NewNotifier(fakeDevices{devices: []store.Device{{ID: "d1", PushToken: "tok"}}}, sender,
		func(string) string { return "n" })

	n.Handle(context.Background(), event.Event{
		NodeID: "node_1", SessionID: "ses_1", Type: "message.part.updated",
	})

	if sender.count() != 0 {
		t.Fatalf("sent %d batches, want 0", sender.count())
	}
}

func TestNotifierDeduplicatesSamePermission(t *testing.T) {
	sender := &fakeSender{}
	n := NewNotifier(fakeDevices{devices: []store.Device{{ID: "d1", PushToken: "tok"}}}, sender,
		func(string) string { return "n" })

	ev := permissionAsked("node_1", "ses_1", "perm_1")
	n.Handle(context.Background(), ev)
	n.Handle(context.Background(), ev)

	if sender.count() != 1 {
		t.Fatalf("sent %d batches, want 1", sender.count())
	}
}

func TestNotifierSurvivesSendAndLookupFailure(t *testing.T) {
	sender := &fakeSender{err: errors.New("expo down")}
	n := NewNotifier(fakeDevices{devices: []store.Device{{ID: "d1", PushToken: "tok"}}}, sender,
		func(string) string { return "n" })

	n.Handle(context.Background(), permissionAsked("node_1", "ses_1", "perm_1"))
	n.Handle(context.Background(), permissionAsked("node_1", "ses_1", "perm_2"))

	if sender.count() != 2 {
		t.Fatalf("sent %d batches, want 2 (failures must not stop later events)", sender.count())
	}

	broken := NewNotifier(fakeDevices{err: errors.New("db down")}, sender, func(string) string { return "n" })
	broken.Handle(context.Background(), permissionAsked("node_1", "ses_2", "perm_3"))
}

func TestNotifierSkipsPermissionWithoutID(t *testing.T) {
	sender := &fakeSender{}
	n := NewNotifier(fakeDevices{devices: []store.Device{{ID: "d1", PushToken: "tok"}}}, sender,
		func(string) string { return "n" })

	n.Handle(context.Background(), event.Event{
		NodeID: "node_1", SessionID: "ses_1", Type: "permission.asked", Data: json.RawMessage(`{"action":"bash"}`),
	})

	if sender.count() != 0 {
		t.Fatalf("sent %d batches, want 0", sender.count())
	}
}

func completionEvent(node, session, eventType string, at time.Time) event.Event {
	return event.Event{NodeID: node, SessionID: session, Type: eventType, Time: at}
}

func TestNotifierPushesCompletion(t *testing.T) {
	sender := &fakeSender{}
	n := NewNotifier(fakeDevices{devices: []store.Device{{ID: "d1", PushToken: "tok"}}}, sender,
		func(string) string { return "dev-box" })

	n.Handle(context.Background(), completionEvent("n1", "ses_1", "session.execution.succeeded", time.Now()))

	batch := sender.firstBatch()
	if len(batch) != 1 {
		t.Fatalf("batch = %+v, want one message", batch)
	}
	msg := batch[0]
	if !strings.HasPrefix(msg.Title, "执行完成 · ") {
		t.Fatalf("title = %q, want the completion prefix", msg.Title)
	}
	if !strings.Contains(msg.Title, "dev-box") {
		t.Fatalf("title = %q, want the node label", msg.Title)
	}
	if msg.Body != "点开查看结果" {
		t.Fatalf("body = %q, want 点开查看结果", msg.Body)
	}
	if msg.Data["kind"] != "done" {
		t.Fatalf("data = %+v, want kind done", msg.Data)
	}
	if msg.ChannelID != "completions-v4" {
		t.Fatalf("channel = %q, want completions", msg.ChannelID)
	}
	if msg.Tag != "ses_1" {
		t.Fatalf("tag = %q, want the session id", msg.Tag)
	}
	if msg.Data["node"] != "n1" || msg.Data["session"] != "ses_1" {
		t.Fatalf("data = %+v, want node and session", msg.Data)
	}
}

func TestNotifierPushesFailure(t *testing.T) {
	sender := &fakeSender{}
	n := NewNotifier(fakeDevices{devices: []store.Device{{ID: "d1", PushToken: "tok"}}}, sender,
		func(string) string { return "dev-box" })

	n.Handle(context.Background(), completionEvent("n1", "ses_1", "session.execution.failed", time.Now()))

	batch := sender.firstBatch()
	if len(batch) != 1 {
		t.Fatalf("batch = %+v, want one message", batch)
	}
	msg := batch[0]
	if !strings.HasPrefix(msg.Title, "执行失败 · ") {
		t.Fatalf("title = %q, want the failure prefix", msg.Title)
	}
	if msg.Body != "点开查看错误" {
		t.Fatalf("body = %q, want 点开查看错误", msg.Body)
	}
	if msg.Data["kind"] != "failed" {
		t.Fatalf("data = %+v, want kind failed", msg.Data)
	}
	if msg.ChannelID != "completions-v4" {
		t.Fatalf("channel = %q, want completions", msg.ChannelID)
	}
	if msg.Tag != "ses_1" {
		t.Fatalf("tag = %q, want the session id", msg.Tag)
	}
}

// TestNotifierDedupesRedeliveredCompletion 固化按事件时间去重：同一条事件
// 重投只推一次，但同一会话下一轮结束（时间不同）仍要推送。
func TestNotifierDedupesRedeliveredCompletion(t *testing.T) {
	sender := &fakeSender{}
	n := NewNotifier(fakeDevices{devices: []store.Device{{ID: "d1", PushToken: "tok"}}}, sender,
		func(string) string { return "n" })

	at := time.Now()
	ev := completionEvent("n1", "ses_1", "session.execution.succeeded", at)
	n.Handle(context.Background(), ev)
	n.Handle(context.Background(), ev)

	if got := sender.count(); got != 1 {
		t.Fatalf("pushes = %d, want 1 after redelivery", got)
	}

	next := ev
	next.Time = at.Add(time.Second)
	n.Handle(context.Background(), next)

	if got := sender.count(); got != 2 {
		t.Fatalf("pushes = %d, want 2 for the next execution", got)
	}
}

// TestNotifierSkipsChildCompletion 固化后台子代理语义：子会话跑完不推“执行完成”，
// 否则主会话还在跑时用户就被提前叫走。审批仍然照推（见下一个用例）。
func TestNotifierSkipsChildCompletion(t *testing.T) {
	sender := &fakeSender{}
	n := NewNotifier(fakeDevices{devices: []store.Device{{ID: "d1", PushToken: "tok"}}}, sender,
		func(string) string { return "dev-box" },
		WithParentSession(func(nodeID, sessionID string) string {
			if nodeID == "n1" && sessionID == "ses_child" {
				return "ses_parent"
			}
			return ""
		}))

	n.Handle(context.Background(), completionEvent("n1", "ses_child", "session.execution.succeeded", time.Now()))
	n.Handle(context.Background(), completionEvent("n1", "ses_child", "session.execution.failed", time.Now()))

	if got := sender.count(); got != 0 {
		t.Fatalf("pushes = %d, want 0 for child completions", got)
	}

	// 根会话的完成照旧推送。
	n.Handle(context.Background(), completionEvent("n1", "ses_parent", "session.execution.succeeded", time.Now()))
	if got := sender.count(); got != 1 {
		t.Fatalf("pushes = %d, want 1 for the root session completion", got)
	}
}

// TestNotifierStillPushesChildPermission 证明过滤只针对完成通知：子会话的审批
// 同样需要用户处理，不能因为它是子代理就静默。
func TestNotifierStillPushesChildPermission(t *testing.T) {
	sender := &fakeSender{}
	n := NewNotifier(fakeDevices{devices: []store.Device{{ID: "d1", PushToken: "tok"}}}, sender,
		func(string) string { return "dev-box" },
		WithParentSession(func(string, string) string { return "ses_parent" }))

	n.Handle(context.Background(), permissionAsked("n1", "ses_child", "perm_1"))

	if got := sender.count(); got != 1 {
		t.Fatalf("pushes = %d, want the child permission pushed", got)
	}
}

type blockingSender struct {
	release chan struct{}
}

func (b *blockingSender) Send(context.Context, []Message) error {
	<-b.release
	return nil
}

// countingPull 包装内存总线，记录已进入 SubscribePull 的消费组成员数。
//
// SubscribePull 注册后立即阻塞，调用方无法从返回值判断注册是否生效；计数
// 是测试等待两个 Run 都加入同一 durable 的唯一可观察信号。
type countingPull struct {
	bus    *mqtest.Bus
	joined int32
}

func (c *countingPull) SubscribePull(
	ctx context.Context,
	stream, subject, durable string,
	handler func(context.Context, []byte) error,
) error {
	atomic.AddInt32(&c.joined, 1)
	return c.bus.SubscribePull(ctx, stream, subject, durable, handler)
}

func (c *countingPull) members() int {
	return int(atomic.LoadInt32(&c.joined))
}

// waitForPullMembers 轮询到指定数量的消费组成员进入 SubscribePull。
func waitForPullMembers(t *testing.T, pull *countingPull, want int) {
	t.Helper()

	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if pull.members() == want {
			return
		}
		time.Sleep(time.Millisecond)
	}
	t.Fatalf("pull members = %d, want %d", pull.members(), want)
}

// waitForPushes 轮询到至少 want 次推送。
func waitForPushes(t *testing.T, sender *fakeSender, want int) {
	t.Helper()

	deadline := time.Now().Add(2 * time.Second)
	for sender.count() < want && time.Now().Before(deadline) {
		time.Sleep(time.Millisecond)
	}
	if sender.count() != want {
		t.Fatalf("pushes = %d, want %d", sender.count(), want)
	}
}

// TestNotifierPullGroupDeduplicates 证明两个实例消费同一 durable 时，
// 一条审批事件只会产生一次推送。
func TestNotifierPullGroupDeduplicates(t *testing.T) {
	bus := mqtest.New()
	sender := &fakeSender{}
	devices := fakeDevices{devices: []store.Device{{ID: "d1", PushToken: "tok"}}}

	first := NewNotifier(devices, sender, func(string) string { return "n" })
	second := NewNotifier(devices, sender, func(string) string { return "n" })

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	pull := &countingPull{bus: bus}
	errCh := make(chan error, 2)
	go func() { errCh <- first.Run(ctx, pull) }()
	go func() { errCh <- second.Run(ctx, pull) }()
	waitForPullMembers(t, pull, 2)
	// members 在真正写入总线订阅表之前递增，留一点调度窗口让两个订阅生效。
	time.Sleep(10 * time.Millisecond)

	subject, err := messaging.ControlPlaneEventSubject("node_1")
	if err != nil {
		t.Fatalf("event subject: %v", err)
	}
	if err := bus.Publish(ctx, subject, permissionAsked("node_1", "ses_1", "perm_1")); err != nil {
		t.Fatalf("publish permission event: %v", err)
	}

	waitForPushes(t, sender, 1)
	// 消费组语义下第二条推送是重复消费：等一个去重窗口之外的短暂间隙再断言。
	time.Sleep(50 * time.Millisecond)
	if got := sender.count(); got != 1 {
		t.Fatalf("pushes = %d, want exactly 1 for one event", got)
	}

	cancel()
	for i := 0; i < 2; i++ {
		select {
		case err := <-errCh:
			if err != nil {
				t.Fatalf("Run returned %v, want nil after cancellation", err)
			}
		case <-time.After(2 * time.Second):
			t.Fatal("Run did not exit after cancellation")
		}
	}
}

// TestNotifierDedupesRedelivery 模拟 Ack 前崩溃后的重投：同一事件投递两次，
// 仍在去重窗口内，只应推送一次。
func TestNotifierDedupesRedelivery(t *testing.T) {
	sender := &fakeSender{}
	n := NewNotifier(fakeDevices{devices: []store.Device{{ID: "d1", PushToken: "tok"}}}, sender,
		func(string) string { return "n" })

	payload, err := json.Marshal(permissionAsked("node_1", "ses_1", "perm_1"))
	if err != nil {
		t.Fatalf("marshal event: %v", err)
	}

	ctx := context.Background()
	if err := n.handlePayload(ctx, payload); err != nil {
		t.Fatalf("first handlePayload returned %v, want nil", err)
	}
	waitForPushes(t, sender, 1)

	if err := n.handlePayload(ctx, payload); err != nil {
		t.Fatalf("second handlePayload returned %v, want nil", err)
	}
	time.Sleep(50 * time.Millisecond)
	if got := sender.count(); got != 1 {
		t.Fatalf("pushes = %d, want 1 after redelivery", got)
	}
}

// TestNotifierIgnoresMalformedPayload 证明坏负载被 Ack 而不是无限重投。
func TestNotifierIgnoresMalformedPayload(t *testing.T) {
	sender := &fakeSender{}
	n := NewNotifier(fakeDevices{devices: []store.Device{{ID: "d1", PushToken: "tok"}}}, sender,
		func(string) string { return "n" })

	if err := n.handlePayload(context.Background(), []byte("not-json")); err != nil {
		t.Fatalf("handlePayload returned %v, want nil so the message is acked", err)
	}

	time.Sleep(20 * time.Millisecond)
	if got := sender.count(); got != 0 {
		t.Fatalf("pushes = %d, want 0 for a malformed payload", got)
	}
}

// TestRunExitsOnCancelWhileSending 证明阻塞的发送不会拖住 Run 的退出。
func TestRunExitsOnCancelWhileSending(t *testing.T) {
	sender := &blockingSender{release: make(chan struct{})}
	n := NewNotifier(fakeDevices{devices: []store.Device{{ID: "d1", PushToken: "tok"}}}, sender,
		func(string) string { return "n" })

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	bus := mqtest.New()
	pull := &countingPull{bus: bus}
	done := make(chan error, 1)
	go func() { done <- n.Run(ctx, pull) }()
	waitForPullMembers(t, pull, 1)

	subject, err := messaging.ControlPlaneEventSubject("node_1")
	if err != nil {
		t.Fatalf("event subject: %v", err)
	}
	for i := 0; i < 20; i++ {
		if err := bus.Publish(ctx, subject, permissionAsked("node_1", "ses_1", strconv.Itoa(i))); err != nil {
			t.Fatalf("publish %d: %v", i, err)
		}
	}

	cancel()
	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("Run returned %v, want nil after cancellation", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("Run did not exit after cancellation while a send was blocked")
	}
	close(sender.release)
}

func formCreated(node, session, formID string, global bool) event.Event {
	data, _ := json.Marshal(map[string]any{
		"form": map[string]any{
			"id":        formID,
			"sessionID": session,
			"title":     "Questions",
			"metadata":  map[string]any{"kind": "question"},
			"fields": []map[string]any{
				{"key": "q0", "type": "string", "title": "选哪个方案？"},
			},
		},
	})
	_ = global
	return event.Event{NodeID: node, SessionID: session, Type: "form.created", Data: data, Time: time.Now()}
}

func TestNotifierPushesFormCreated(t *testing.T) {
	sender := &fakeSender{}
	n := NewNotifier(fakeDevices{devices: []store.Device{{PushToken: "ExponentPushToken[1]"}}}, sender, nil)
	n.Handle(context.Background(), formCreated("node_1", "ses_1", "frm_1", false))

	msg := sender.firstBatch()
	if len(msg) != 1 {
		t.Fatalf("messages = %#v", msg)
	}
	if !strings.HasPrefix(msg[0].Title, "需要回答 · ") || !strings.Contains(msg[0].Title, "node_1") {
		t.Fatalf("title = %q", msg[0].Title)
	}
	if msg[0].Body != "选哪个方案？" {
		t.Fatalf("body = %q", msg[0].Body)
	}
	if msg[0].Tag != "node_1/form/frm_1" {
		t.Fatalf("tag = %q", msg[0].Tag)
	}
	if msg[0].Data["form_id"] != "frm_1" || msg[0].Data["kind"] != "question" {
		t.Fatalf("data = %#v", msg[0].Data)
	}
}

func TestNotifierGlobalFormUsesGlobalTitleAndData(t *testing.T) {
	sender := &fakeSender{}
	n := NewNotifier(fakeDevices{devices: []store.Device{{PushToken: "t"}}}, sender, nil)
	n.Handle(context.Background(), formCreated("node_1", "global", "frm_2", true))

	msg := sender.firstBatch()
	if len(msg) != 1 || msg[0].Title != "需要回答 · 全局表单" {
		t.Fatalf("messages = %#v", msg)
	}
	if msg[0].Data["global"] != "true" || msg[0].Data["session"] != "global" {
		t.Fatalf("data = %#v", msg[0].Data)
	}
	if msg[0].Tag != "node_1/form/frm_2" {
		t.Fatalf("tag = %q", msg[0].Tag)
	}
}

func TestNotifierDedupesSameForm(t *testing.T) {
	sender := &fakeSender{}
	n := NewNotifier(fakeDevices{devices: []store.Device{{PushToken: "t"}}}, sender, nil)
	ev := formCreated("node_1", "ses_1", "frm_1", false)
	n.Handle(context.Background(), ev)
	n.Handle(context.Background(), ev)
	if sender.count() != 1 {
		t.Fatalf("batches = %d", sender.count())
	}
}

func TestNotifierIgnoresFormReplied(t *testing.T) {
	sender := &fakeSender{}
	n := NewNotifier(fakeDevices{devices: []store.Device{{PushToken: "t"}}}, sender, nil)
	data, _ := json.Marshal(map[string]string{"id": "frm_1", "sessionID": "ses_1"})
	n.Handle(context.Background(), event.Event{
		NodeID: "node_1", SessionID: "ses_1", Type: "form.replied", Data: data, Time: time.Now(),
	})
	if sender.count() != 0 {
		t.Fatalf("batches = %d", sender.count())
	}
}
