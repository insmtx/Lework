//go:build integration

package controlplane_test

// 多实例验收测试：两个真实控制面实例共享同一个 PostgreSQL 与同一个 NATS，
// agent 只连其中一个，浏览器请求打在另一个上。
//
// 这些测试需要真实外部依赖，因此带 integration 标签并放在默认测试集之外：
//   - LEWORK_TEST_DATABASE_URL 指向共享的 PostgreSQL；缺失时跳过。
//   - LEWORK_TEST_NATS_URL 指向启用了 JetStream 的 NATS；未设置时使用本机
//     devcj 的 14224。连接或 JetStream 初始化失败时跳过。
//
// 每个用例使用带时间戳的节点名/节点 ID，只清理自己写入的行（这里只读节点
// 列表），绝不 truncate 或删除共享表/stream。

import (
	"bufio"
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"net/http"
	"os"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/gorilla/websocket"

	appconfig "github.com/insmtx/lework/backend/config"
	"github.com/insmtx/lework/backend/internal/agentd/protocol"
	"github.com/insmtx/lework/backend/internal/agentd/transport"
	"github.com/insmtx/lework/backend/internal/controlplane"
	"github.com/insmtx/lework/backend/internal/controlplane/config"
	cpevent "github.com/insmtx/lework/backend/internal/controlplane/event"
	"github.com/insmtx/lework/backend/internal/controlplane/push"
	"github.com/insmtx/lework/backend/internal/controlplane/store"
	"github.com/insmtx/lework/backend/internal/infra/mq"
	"github.com/insmtx/lework/backend/pkg/messaging"
)

const (
	// miToken 是测试实例的注册令牌。
	miToken = "mi-enrollment-token"

	// miRequestTimeout 是浏览器请求与跨实例转发的超时。
	miRequestTimeout = 5 * time.Second

	// miHTTPTimeout 是单次测试 HTTP 调用的上限。
	miHTTPTimeout = 5 * time.Second

	// miHeartbeatTimeout 是实例判定节点离线的窗口；明显短于等待上限，
	// 让离线用例在超时前留出余量。
	miHeartbeatTimeout = 2 * time.Second

	// miAgentHeartbeat 是假 agent 的心跳间隔，必须短于心跳窗口。
	miAgentHeartbeat = 300 * time.Millisecond

	// miWaitTimeout 是等待跨实例传播的上限。JetStream/Core NATS 都是
	// 异步链路，单次往返通常远小于这个值。
	miWaitTimeout = 10 * time.Second

	// miPollInterval 是轮询间隔。
	miPollInterval = 25 * time.Millisecond

	// miSessionID 是假 agent 返回/接受的对会话 ID。
	miSessionID = "ses-mi-1"

	// miDirectory 是创建 PTY 时使用的绝对目录。
	miDirectory = "/work"
)

// TestCrossInstanceEventsReachSSE 证明 agent 连在 A 上产生的事件能经集群
// 总线扇出到 B 的本地 Hub，并由 B 的 /api/events 推给浏览器。
func TestCrossInstanceEventsReachSSE(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	a := miStartInstance(t, ctx, "A")
	b := miStartInstance(t, ctx, "B")
	agent := miStartAgent(t, ctx, a.agentWSURL(), miNodeName("events"))

	sse := miOpenSSE(t, ctx, b.baseURL, "")
	miWaitAgentReachable(t, ctx, b.baseURL, agent.nodeID)

	wantType := "session.updated"
	payload, err := json.Marshal(map[string]string{"title": "multi-instance"})
	if err != nil {
		t.Fatalf("marshal event payload: %v", err)
	}
	ev := protocol.Event{
		SessionID: miSessionID,
		Type:      wantType,
		Data:      payload,
		Time:      time.Now(),
	}

	// 桥接订阅使用 DeliverNew：订阅生效前发布的事件会丢失。没有“订阅已就绪”
	// 的回调，因此重复发布同一事件直到 B 收到，既避免固定 sleep，也让断言
	// 只依赖真实的端到端传播。
	emitCtx, stopEmitting := context.WithCancel(ctx)
	defer stopEmitting()
	go miEmitUntilCanceled(emitCtx, agent, ev)

	got := miWaitEvent(t, ctx, sse.events, "session.updated from the other instance",
		func(candidate cpevent.Event) bool {
			return candidate.Type == wantType && candidate.NodeID == agent.nodeID
		})
	stopEmitting()

	if got.SessionID != miSessionID {
		t.Fatalf("event session = %q, want %q", got.SessionID, miSessionID)
	}
}

// TestCrossInstanceOverviewConverges 证明新实例启动后不需要重放历史事件，仅靠
// owner 周期性发布的 node.snapshot 就能收敛到相同总览。
func TestCrossInstanceOverviewConverges(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	defer cancel()

	a := miStartInstance(t, ctx, "A")
	b := miStartInstance(t, ctx, "B")
	agent := miStartAgent(t, ctx, a.agentWSURL(), miNodeName("overview"))

	// 先证明 event 链路已就绪，避免把“事件没到 A”误判成快照问题。
	miWaitAgentReachable(t, ctx, b.baseURL, agent.nodeID)

	emitCtx, stopEmitting := context.WithCancel(ctx)
	go miEmitUntilCanceled(emitCtx, agent, protocol.Event{
		SessionID: miSessionID,
		Type:      "session.updated",
		Data:      json.RawMessage(`{"title":"overview-converge"}`),
		Time:      time.Now(),
	})
	miWaitOverviewSession(t, ctx, a.baseURL, agent.nodeID, miSessionID, miWaitTimeout)
	stopEmitting()

	// 停止发布后再启动 C：C 的桥接订阅使用 DeliverNew，收不到此前的事件，
	// 因此它只能靠 A 的整节点快照建立视图。
	time.Sleep(200 * time.Millisecond)
	c := miStartInstance(t, ctx, "C")
	miWaitOverviewSession(t, ctx, c.baseURL, agent.nodeID, miSessionID, 15*time.Second)
}

// TestCrossInstanceRequestRouting 证明 HTTP 请求打在 B、agent 连在 A 时，
// 请求会经 cp.node.<id>.request 转发给持有连接的实例，并由 agent 应答。
func TestCrossInstanceRequestRouting(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	a := miStartInstance(t, ctx, "A")
	b := miStartInstance(t, ctx, "B")
	agent := miStartAgent(t, ctx, a.agentWSURL(), miNodeName("routing"))

	// 注册表与转发订阅在 welcome 之后才生效，转发确认无人应答时 API 会回
	// 409；轮询到 200 即证明整条链路可用。
	body := miWaitStatus(t, ctx, http.MethodGet,
		b.baseURL+"/api/sessions?node="+agent.nodeID, "", http.StatusOK,
		"cross-instance session.list")

	var sessions []json.RawMessage
	if err := json.Unmarshal(body, &sessions); err != nil {
		t.Fatalf("decode session list %q: %v", body, err)
	}

	// 只认真正到达 agent 的请求，而不是控制面自己造的响应。
	agent.waitForRequest(ctx, "session.list", nil)
}

// TestCrossInstancePTY 证明终端链路跨实例可用：PTY 在 B 上创建、agent 在 A，
// 浏览器输入/输出/resize/exit 都经过 B，且输出只写进对应 PTY 的 socket。
func TestCrossInstancePTY(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	a := miStartInstance(t, ctx, "A")
	b := miStartInstance(t, ctx, "B")
	agent := miStartAgent(t, ctx, a.agentWSURL(), miNodeName("pty"))

	miWaitAgentReachable(t, ctx, b.baseURL, agent.nodeID)

	ptyID := miCreatePTY(t, ctx, b.baseURL, agent.nodeID, miSessionID)
	otherPTYID := miCreatePTY(t, ctx, b.baseURL, agent.nodeID, miSessionID)
	if ptyID == otherPTYID {
		t.Fatalf("two pty.create calls returned the same id %q", ptyID)
	}

	ws := miDialBrowserPTY(t, ctx, b.baseURL, agent.nodeID, miSessionID, ptyID)
	// 关闭浏览器连接会让服务端再发一次 pty.close；假 agent 对未知方法一律
	// 回 null，因此不会阻塞清理。
	agent.waitForRequest(ctx, "pty.attach", func(rec miAgentRequest) bool {
		return miPTYIDParam(t, rec.Params) == ptyID
	})
	// subscribePTY 在 attach 之前启动，但 Core NATS 订阅没有“已生效”回调；
	// 留一个很短的窗口，避免紧接着发出的输出落在订阅建立之前。
	time.Sleep(100 * time.Millisecond)

	// (a) 浏览器二进制帧原样变成 pty.write 的 base64。
	input := []byte{0x00, 0xff, 'h', 'i', '\r'}
	miWriteBrowserBinary(t, ws, input)
	agent.waitForRequest(ctx, "pty.write", func(rec miAgentRequest) bool {
		var params struct {
			PTYID string `json:"pty_id"`
			Data  string `json:"data"`
		}
		if err := json.Unmarshal(rec.Params, &params); err != nil {
			t.Fatalf("decode pty.write params: %v", err)
		}
		return params.PTYID == ptyID && params.Data == base64.StdEncoding.EncodeToString(input)
	})

	// (b) agent 经 A 发布的 pty.output 变成浏览器上的一个二进制帧。
	firstOutput := []byte("out-from-agent")
	if err := agent.emit(ctx, miPTYOutputEvent(miSessionID, ptyID, firstOutput)); err != nil {
		t.Fatalf("emit pty.output: %v", err)
	}
	kind, data := miReadBrowser(t, ws, miWaitTimeout)
	if kind != websocket.BinaryMessage {
		t.Fatalf("pty output frame kind = %d, want binary", kind)
	}
	if !bytes.Equal(data, firstOutput) {
		t.Fatalf("pty output = %v, want %v", data, firstOutput)
	}

	// (c) 另一个 PTY 的输出不能写进本 socket。Core NATS 保证同一连接上的
	// 发布顺序，因此先发“别的终端”、再发一个哨兵；读到哨兵前若出现别的
	// 内容即失败。用哨兵而不是读超时，避免超时破坏 gorilla 连接状态。
	foreignOutput := []byte("this-must-not-arrive")
	if err := agent.emit(ctx, miPTYOutputEvent(miSessionID, otherPTYID, foreignOutput)); err != nil {
		t.Fatalf("emit foreign pty.output: %v", err)
	}
	sentinel := []byte("sentinel-output")
	if err := agent.emit(ctx, miPTYOutputEvent(miSessionID, ptyID, sentinel)); err != nil {
		t.Fatalf("emit sentinel pty.output: %v", err)
	}
	deadline := time.Now().Add(miWaitTimeout)
	for {
		remaining := time.Until(deadline)
		if remaining <= 0 {
			t.Fatal("timed out waiting for the sentinel terminal output")
		}
		kind, data = miReadBrowser(t, ws, remaining)
		if kind != websocket.BinaryMessage {
			t.Fatalf("unexpected terminal frame kind %d: %s", kind, data)
		}
		if bytes.Equal(data, foreignOutput) {
			t.Fatalf("terminal received output from another pty: %q", data)
		}
		if bytes.Equal(data, sentinel) {
			break
		}
		t.Fatalf("unexpected terminal output %q", data)
	}

	// (d) resize 文本帧转成 pty.resize 请求。
	if err := ws.SetWriteDeadline(time.Now().Add(miHTTPTimeout)); err != nil {
		t.Fatalf("set browser write deadline: %v", err)
	}
	if err := ws.WriteMessage(websocket.TextMessage, []byte(`{"type":"resize","cols":80,"rows":24}`)); err != nil {
		t.Fatalf("write resize frame: %v", err)
	}
	agent.waitForRequest(ctx, "pty.resize", func(rec miAgentRequest) bool {
		var params struct {
			PTYID string `json:"pty_id"`
			Cols  int    `json:"cols"`
			Rows  int    `json:"rows"`
		}
		if err := json.Unmarshal(rec.Params, &params); err != nil {
			t.Fatalf("decode pty.resize params: %v", err)
		}
		return params.PTYID == ptyID && params.Cols == 80 && params.Rows == 24
	})

	// (e) pty.exit 先变成 {"type":"exit"} 文本帧，然后关闭浏览器流。
	exitData, err := json.Marshal(map[string]string{"pty_id": ptyID})
	if err != nil {
		t.Fatalf("marshal pty.exit: %v", err)
	}
	if err := agent.emit(ctx, protocol.Event{
		SessionID: miSessionID,
		Type:      cpevent.EventTypePTYExit,
		Data:      exitData,
		Time:      time.Now(),
	}); err != nil {
		t.Fatalf("emit pty.exit: %v", err)
	}

	kind, data = miReadBrowser(t, ws, miWaitTimeout)
	if kind != websocket.TextMessage {
		t.Fatalf("pty exit frame kind = %d, want text", kind)
	}
	var control struct {
		Type string `json:"type"`
	}
	if err := json.Unmarshal(data, &control); err != nil {
		t.Fatalf("decode pty exit frame %q: %v", data, err)
	}
	if control.Type != "exit" {
		t.Fatalf("pty exit frame = %q, want {\"type\":\"exit\"}", data)
	}

	_ = ws.SetReadDeadline(time.Now().Add(miWaitTimeout))
	if _, _, err := ws.ReadMessage(); err == nil {
		t.Fatal("terminal stream stayed open after pty.exit")
	} else {
		var netErr net.Error
		if errors.As(err, &netErr) && netErr.Timeout() {
			t.Fatalf("terminal stream did not close after pty.exit: %v", err)
		}
	}
}

// TestPushNotificationOnlyOnce 证明两个实例的推送消费者共享同一个 durable
// 消费组：一条 permission.asked 只产生一次推送。
func TestPushNotificationOnlyOnce(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 45*time.Second)
	defer cancel()

	natsURL := miNATSURL()
	busA, err := mq.NewNATS(natsURL)
	if err != nil {
		t.Skipf("NATS with JetStream is unavailable for the push consumer test: %v", err)
	}
	t.Cleanup(func() { _ = busA.Close() })
	busB, err := mq.NewNATS(natsURL)
	if err != nil {
		t.Skipf("second NATS connection is unavailable for the push consumer test: %v", err)
	}
	t.Cleanup(func() { _ = busB.Close() })

	sender := &miPushSender{}
	devices := miPushDevices{}
	notifierA := push.NewNotifier(devices, sender, nil)
	notifierB := push.NewNotifier(devices, sender, nil)

	runCtx, stopRun := context.WithCancel(ctx)
	var running sync.WaitGroup
	for name, notifier := range map[string]*push.Notifier{"A": notifierA, "B": notifierB} {
		bus := busA
		if name == "B" {
			bus = busB
		}
		running.Add(1)
		go func(name string, notifier *push.Notifier) {
			defer running.Done()
			if err := notifier.Run(runCtx, bus); err != nil && runCtx.Err() == nil {
				t.Errorf("notifier %s stopped: %v", name, err)
			}
		}(name, notifier)
	}
	t.Cleanup(func() {
		stopRun()
		if !miWaitGroup(&running, 5*time.Second) {
			t.Errorf("push notifiers did not stop within 5s")
		}
	})

	// 每次运行使用唯一的节点与会话，避免同一条 durable 上遗留的消息影响计数。
	nano := time.Now().UnixNano()
	nodeID := fmt.Sprintf("mi-push-%d", nano)
	sessionID := fmt.Sprintf("ses-mi-push-%d", nano)
	subject, err := messaging.ControlPlaneEventSubject(nodeID)
	if err != nil {
		t.Fatalf("build event subject: %v", err)
	}
	data, err := json.Marshal(map[string]string{
		"id":      "perm-1",
		"action":  "ask",
		"message": "needs approval",
	})
	if err != nil {
		t.Fatalf("marshal permission payload: %v", err)
	}
	if err := busA.Publish(ctx, subject, cpevent.Event{
		NodeID:    nodeID,
		SessionID: sessionID,
		Type:      "permission.asked",
		Data:      data,
		Time:      time.Now(),
	}); err != nil {
		t.Fatalf("publish permission.asked: %v", err)
	}

	miWaitPushCount(t, ctx, sender, sessionID, 1)

	// 再等一段时间，确认消费组不会让第二个实例重复推送同一条事件。
	time.Sleep(time.Second)
	if got := sender.count(sessionID); got != 1 {
		t.Fatalf("push notifications for session %s = %d, want exactly 1", sessionID, got)
	}
}

// TestOfflineAfterOwnerCrash 证明节点连接消失后，另一个实例仅凭共享存储里的
// last_seen 也能把节点标记为离线（并收到 node.offline 事件）。
func TestOfflineAfterOwnerCrash(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	a := miStartInstance(t, ctx, "A")
	b := miStartInstance(t, ctx, "B")
	agent := miStartAgent(t, ctx, a.agentWSURL(), miNodeName("offline"))

	miWaitNodeConnected(t, ctx, b.baseURL, agent.nodeID, true)

	// 先确认 B 的桥接订阅生效（收到一次普通事件），否则 node.offline 可能
	// 在订阅建立前发布而丢失。
	sse := miOpenSSE(t, ctx, b.baseURL, "")
	warmupType := "session.updated"
	warmupData, err := json.Marshal(map[string]string{"title": "warmup"})
	if err != nil {
		t.Fatalf("marshal warmup payload: %v", err)
	}
	warmupCtx, stopWarmup := context.WithCancel(ctx)
	defer stopWarmup()
	go miEmitUntilCanceled(warmupCtx, agent, protocol.Event{
		SessionID: miSessionID,
		Type:      warmupType,
		Data:      warmupData,
		Time:      time.Now(),
	})
	miWaitEvent(t, ctx, sse.events, "warmup event on the other instance",
		func(candidate cpevent.Event) bool {
			return candidate.Type == warmupType && candidate.NodeID == agent.nodeID
		})
	stopWarmup()

	// 关闭连接模拟 owner 崩溃：agent 不再心跳，网关会发布 node.offline，
	// 对账器也会在心跳窗口之后补发。
	agent.disconnect()

	miWaitNodeConnected(t, ctx, b.baseURL, agent.nodeID, false)
	miWaitEvent(t, ctx, sse.events, "node.offline on the other instance",
		func(candidate cpevent.Event) bool {
			return candidate.Type == "node.offline" && candidate.NodeID == agent.nodeID
		})
}

// ---- 实例 ----

// miInstance 是一个进程内启动的真实控制面实例。
//
// 每个实例都有自己的 PostgreSQL 连接池与 NATS 连接：测试验证的正是跨实例
// 共享，因此不能复用同一个 store 或 bus。
type miInstance struct {
	t       *testing.T
	name    string
	baseURL string

	srv   *controlplane.Server
	bus   interface{ Close() error }
	store *store.Store

	cancel context.CancelFunc
	done   chan error
}

// miStartInstance 启动一个控制面实例并等待 /api/overview 就绪。
func miStartInstance(t *testing.T, parent context.Context, name string) *miInstance {
	t.Helper()

	dsn := miDatabaseURL(t)
	natsURL := miNATSURL()
	addr := miReserveAddr(t)

	st, err := store.OpenPostgres(appconfig.DatabaseConfig{URL: dsn})
	if err != nil {
		t.Fatalf("open postgres for instance %s: %v", name, err)
	}

	bus, err := mq.NewNATS(natsURL)
	if err != nil {
		_ = st.Close()
		t.Skipf("NATS with JetStream is unavailable for multi-instance test: %v", err)
	}

	// 从 Default 出发保证新增的认证配置字段始终合法。
	cfg := config.Default()
	cfg.ListenAddr = addr
	cfg.EnrollmentToken = miToken
	cfg.RequestTimeout = miRequestTimeout
	cfg.HeartbeatTimeout = miHeartbeatTimeout
	srv, err := controlplane.New(cfg, st, controlplane.Deps{
		Bus:     bus,
		Core:    bus,
		CoreSub: bus,
		Request: bus,
		Pull:    bus,
	})
	if err != nil {
		_ = bus.Close()
		_ = st.Close()
		t.Fatalf("new control plane instance %s: %v", name, err)
	}

	ctx, cancel := context.WithCancel(parent)
	done := make(chan error, 1)
	go func() { done <- srv.Run(ctx) }()

	inst := &miInstance{
		t:       t,
		name:    name,
		baseURL: "http://" + addr,
		srv:     srv,
		bus:     bus,
		store:   st,
		cancel:  cancel,
		done:    done,
	}
	t.Cleanup(inst.stop)
	inst.waitReady(ctx)
	return inst
}

// stop 先停止后台循环，再按打开顺序释放资源；Close 都是幂等的。
func (i *miInstance) stop() {
	i.cancel()
	select {
	case <-i.done:
	case <-time.After(miWaitTimeout):
		i.t.Errorf("instance %s did not stop within %s", i.name, miWaitTimeout)
	}
	if err := i.srv.Close(); err != nil {
		i.t.Errorf("close instance %s: %v", i.name, err)
	}
	if err := i.bus.Close(); err != nil {
		i.t.Errorf("close instance %s bus: %v", i.name, err)
	}
	if err := i.store.Close(); err != nil {
		i.t.Errorf("close instance %s store: %v", i.name, err)
	}
}

// agentWSURL 返回该实例的 agent 接入地址。
func (i *miInstance) agentWSURL() string {
	return "ws" + strings.TrimPrefix(i.baseURL, "http") + "/api/agent/connect"
}

// waitReady 轮询 /api/overview 直到返回 200。控制面的监听器在 Run 内创建，
// 因此客户端就绪前通常会短暂连接失败。
func (i *miInstance) waitReady(ctx context.Context) {
	i.t.Helper()

	client := &http.Client{Timeout: miHTTPTimeout}
	deadline := time.Now().Add(miWaitTimeout)
	var lastErr error
	for {
		req, err := http.NewRequestWithContext(ctx, http.MethodGet, i.baseURL+"/api/overview", nil)
		if err != nil {
			i.t.Fatalf("build readiness request for instance %s: %v", i.name, err)
		}
		resp, err := client.Do(req)
		if err == nil {
			_ = resp.Body.Close()
			if resp.StatusCode == http.StatusOK {
				return
			}
			lastErr = fmt.Errorf("GET /api/overview returned %d", resp.StatusCode)
		} else {
			lastErr = err
		}
		if time.Now().After(deadline) {
			i.t.Fatalf("instance %s did not become ready: %v", i.name, lastErr)
		}
		select {
		case <-ctx.Done():
			i.t.Fatalf("context ended while waiting for instance %s: %v", i.name, ctx.Err())
		case <-time.After(miPollInterval):
		}
	}
}

// miReserveAddr 取一个空闲回环端口后立即关闭监听器。
//
// 控制面的监听器在 Run 内部创建，测试无法直接拿到实际端口，因此先预留
// 一个地址再把它交给配置。端口被他人抢占的概率极低，且失败会明确报错。
func miReserveAddr(t *testing.T) string {
	t.Helper()

	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("reserve loopback port: %v", err)
	}
	addr := listener.Addr().String()
	if err := listener.Close(); err != nil {
		t.Fatalf("release reserved port %s: %v", addr, err)
	}
	return addr
}

// miDatabaseURL 读取共享 PostgreSQL 的连接串；未设置时跳过。
func miDatabaseURL(t *testing.T) string {
	t.Helper()

	dsn := strings.TrimSpace(os.Getenv("LEWORK_TEST_DATABASE_URL"))
	if dsn == "" {
		t.Skip("LEWORK_TEST_DATABASE_URL is not set")
	}
	return dsn
}

// miNATSURL 返回启用了 JetStream 的 NATS 地址。
//
// 默认地址是本机 devcj 的 14224；显式 LEWORK_TEST_NATS_URL 优先。连接失败
// 由调用方跳过，错误信息不回显凭据。
func miNATSURL() string {
	if custom := strings.TrimSpace(os.Getenv("LEWORK_TEST_NATS_URL")); custom != "" {
		return custom
	}
	return "nats://127.0.0.1:14224/"
}

// ---- 假 agent ----

// miAgentRequest 是假 agent 收到的一次方法调用。
type miAgentRequest struct {
	Method string
	Params json.RawMessage
}

// miAgent 是用真实 transport.Client 实现的最小 agent。
//
// 它只负责协议层：注册、心跳、按方法返回固定结果、发布事件。这样多实例
// 用例可以确定性地断言“请求是否真的到达节点”，不依赖 OpenCode。
type miAgent struct {
	t      *testing.T
	label  string
	conn   *transport.Client
	nodeID string

	writeMu sync.Mutex

	mu       sync.Mutex
	requests []miAgentRequest
	ptySeq   int

	cancel   context.CancelFunc
	done     chan struct{}
	stopOnce sync.Once
}

// miStartAgent 注册一个节点并返回就绪的假 agent。
func miStartAgent(t *testing.T, parent context.Context, wsURL, nodeName string) *miAgent {
	t.Helper()

	dialCtx, cancelDial := context.WithTimeout(parent, miWaitTimeout)
	defer cancelDial()

	conn, err := transport.Dial(dialCtx, transport.DialConfig{
		URL:    wsURL,
		Header: http.Header{"Authorization": {"Bearer " + miToken}},
	})
	if err != nil {
		t.Fatalf("dial control plane: %v", err)
	}

	params, err := json.Marshal(map[string]any{
		"node_name":       nodeName,
		"os":              "linux",
		"arch":            "amd64",
		"agent_version":   "integration",
		"capabilities":    []string{"session.list", "pty.create"},
		"workspace_roots": []string{miDirectory},
	})
	if err != nil {
		_ = conn.Close()
		t.Fatalf("marshal hello params: %v", err)
	}
	if err := conn.Write(dialCtx, protocol.Frame{
		Type:   protocol.FrameHello,
		ID:     "hello-1",
		Params: params,
	}); err != nil {
		_ = conn.Close()
		t.Fatalf("write hello: %v", err)
	}

	var nodeID string
	for nodeID == "" {
		frame, err := conn.Read(dialCtx)
		if err != nil {
			_ = conn.Close()
			t.Fatalf("read welcome: %v", err)
		}
		if frame.Type != protocol.FrameWelcome {
			continue
		}
		var welcome struct {
			NodeID string `json:"node_id"`
		}
		if err := json.Unmarshal(frame.Result, &welcome); err != nil {
			_ = conn.Close()
			t.Fatalf("decode welcome %s: %v", frame.Result, err)
		}
		nodeID = welcome.NodeID
	}

	runCtx, cancelRun := context.WithCancel(parent)
	agent := &miAgent{
		t:      t,
		label:  nodeName,
		conn:   conn,
		nodeID: nodeID,
		cancel: cancelRun,
		done:   make(chan struct{}),
	}
	go agent.loop(runCtx)
	t.Cleanup(agent.stop)
	return agent
}

// loop 持续读取帧并独立发送心跳；两个方向共用 writeMu，因为 transport.Client
// 只允许一个并发写者。
func (a *miAgent) loop(ctx context.Context) {
	defer close(a.done)

	heartbeatCtx, stopHeartbeat := context.WithCancel(ctx)
	defer stopHeartbeat()

	var heartbeat sync.WaitGroup
	heartbeat.Add(1)
	go func() {
		defer heartbeat.Done()
		ticker := time.NewTicker(miAgentHeartbeat)
		defer ticker.Stop()
		for {
			select {
			case <-heartbeatCtx.Done():
				return
			case <-ticker.C:
				if err := a.write(heartbeatCtx, protocol.Frame{Type: protocol.FrameHeartbeat}); err != nil {
					return
				}
			}
		}
	}()

	for {
		frame, err := a.conn.Read(ctx)
		if err != nil {
			break
		}
		a.handleFrame(ctx, frame)
	}

	stopHeartbeat()
	heartbeat.Wait()
}

// handleFrame 只处理控制面发来的请求：记录方法，并回一个合法响应。
func (a *miAgent) handleFrame(ctx context.Context, frame protocol.Frame) {
	if frame.Type != protocol.FrameRequest {
		return
	}

	a.mu.Lock()
	a.requests = append(a.requests, miAgentRequest{
		Method: frame.Method,
		Params: append(json.RawMessage(nil), frame.Params...),
	})
	a.mu.Unlock()

	if err := a.write(ctx, protocol.Frame{
		Type:   protocol.FrameResponse,
		ID:     frame.ID,
		Result: a.resultFor(frame.Method),
	}); err != nil {
		a.t.Logf("agent %s reply to %s failed: %v", a.label, frame.Method, err)
	}
}

// resultFor 返回与真实 handler 线格式一致的固定结果。
func (a *miAgent) resultFor(method string) json.RawMessage {
	switch method {
	case "session.list":
		// handler 直接返回 []opencode.Session，线格式是数组。
		return json.RawMessage("[]")
	case "pty.create":
		a.mu.Lock()
		a.ptySeq++
		id := fmt.Sprintf("pty-mi-%d", a.ptySeq)
		a.mu.Unlock()
		// handler 返回 opencode.PersistentTerminal，浏览器从 id 字段取 PTY ID。
		// 这里不调用 testing.T：resultFor 运行在读取协程里。
		return json.RawMessage(fmt.Sprintf(
			`{"id":%q,"title":"Lework Terminal","command":"/bin/sh","cwd":%q,"status":"running"}`,
			id, miDirectory,
		))
	case "pty.attach":
		return json.RawMessage(`{"attached":true}`)
	default:
		// pty.write/resize/close 与多数方法在真实 handler 里返回 nil。
		return json.RawMessage("null")
	}
}

// write 串行化所有出站帧，并给写入加上超时。
func (a *miAgent) write(ctx context.Context, frame protocol.Frame) error {
	a.writeMu.Lock()
	defer a.writeMu.Unlock()

	writeCtx, cancel := context.WithTimeout(ctx, miHTTPTimeout)
	defer cancel()
	return a.conn.Write(writeCtx, frame)
}

// emit 发布一个异步事件。
func (a *miAgent) emit(ctx context.Context, ev protocol.Event) error {
	return a.write(ctx, protocol.Frame{Type: protocol.FrameEvent, Event: &ev})
}

// disconnect 模拟 agent 崩溃：直接关闭套接字，不再心跳。
func (a *miAgent) disconnect() {
	_ = a.conn.Close()
	select {
	case <-a.done:
	case <-time.After(miWaitTimeout):
		a.t.Fatalf("agent %s did not stop after disconnect", a.label)
	}
}

// stop 是 t.Cleanup 使用的幂等关闭路径。
func (a *miAgent) stop() {
	a.stopOnce.Do(func() {
		a.cancel()
		_ = a.conn.Close()
		select {
		case <-a.done:
		case <-time.After(miWaitTimeout):
			a.t.Errorf("agent %s did not stop within %s", a.label, miWaitTimeout)
		}
	})
}

// waitForRequest 等待一条满足条件的方法调用；match 为 nil 时匹配任意参数。
func (a *miAgent) waitForRequest(
	ctx context.Context,
	method string,
	match func(miAgentRequest) bool,
) miAgentRequest {
	a.t.Helper()

	deadline := time.Now().Add(miWaitTimeout)
	for {
		a.mu.Lock()
		var methods []string
		for _, rec := range a.requests {
			methods = append(methods, rec.Method)
			if rec.Method == method && (match == nil || match(rec)) {
				a.mu.Unlock()
				return rec
			}
		}
		a.mu.Unlock()

		if time.Now().After(deadline) {
			a.t.Fatalf("agent %s never received %s (seen: %v)", a.label, method, methods)
		}
		select {
		case <-ctx.Done():
			a.t.Fatalf("context ended while waiting for %s on agent %s: %v", method, a.label, ctx.Err())
		case <-time.After(miPollInterval):
		}
	}
}

// miEmitUntilCanceled 周期性发布同一个事件，直到 ctx 取消。
//
// 集群订阅使用 DeliverNew，没有“订阅已建立”的回调；重复发布让测试在真实
// 的异步链路上前进，而不是靠固定 sleep 猜时序。
func miEmitUntilCanceled(ctx context.Context, agent *miAgent, ev protocol.Event) {
	ticker := time.NewTicker(150 * time.Millisecond)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			if err := agent.emit(ctx, ev); err != nil {
				return
			}
		}
	}
}

// miPTYOutputEvent 构造一条 pty.output 事件。
func miPTYOutputEvent(sessionID, ptyID string, data []byte) protocol.Event {
	payload, _ := json.Marshal(map[string]string{
		"pty_id": ptyID,
		"data":   base64.StdEncoding.EncodeToString(data),
	})
	return protocol.Event{
		SessionID: sessionID,
		Type:      cpevent.EventTypePTYOutput,
		Data:      payload,
		Time:      time.Now(),
	}
}

// miPTYIDParam 从 PTY 方法参数中取 pty_id，便于断言匹配的是同一个终端。
func miPTYIDParam(t *testing.T, raw json.RawMessage) string {
	t.Helper()

	var params struct {
		PTYID string `json:"pty_id"`
	}
	if err := json.Unmarshal(raw, &params); err != nil {
		t.Fatalf("decode pty params %s: %v", raw, err)
	}
	return params.PTYID
}

// ---- 浏览器侧 ----

// miNodeView 是 GET /api/nodes 的元素。
type miNodeView struct {
	ID        string `json:"id"`
	Name      string `json:"name"`
	Connected bool   `json:"connected"`
}

// miRequest 发送一次 HTTP 调用并读取完整响应体。
func miRequest(
	ctx context.Context,
	client *http.Client,
	method, url, body string,
) (int, []byte, error) {
	var reader *strings.Reader
	if body != "" {
		reader = strings.NewReader(body)
	}
	var req *http.Request
	var err error
	if reader != nil {
		req, err = http.NewRequestWithContext(ctx, method, url, reader)
	} else {
		req, err = http.NewRequestWithContext(ctx, method, url, nil)
	}
	if err != nil {
		return 0, nil, err
	}
	if body != "" {
		req.Header.Set("Content-Type", "application/json")
	}

	resp, err := client.Do(req)
	if err != nil {
		return 0, nil, err
	}
	defer func() { _ = resp.Body.Close() }()

	var buf bytes.Buffer
	if _, err := buf.ReadFrom(resp.Body); err != nil {
		return 0, nil, err
	}
	return resp.StatusCode, buf.Bytes(), nil
}

// miWaitStatus 轮询直到返回期望状态码，返回最终响应体。
func miWaitStatus(
	t *testing.T,
	ctx context.Context,
	method, url, body string,
	want int,
	description string,
) []byte {
	t.Helper()

	client := &http.Client{Timeout: miHTTPTimeout}
	deadline := time.Now().Add(miWaitTimeout)
	var (
		lastStatus int
		lastBody   []byte
		lastErr    error
	)
	for {
		status, data, err := miRequest(ctx, client, method, url, body)
		if err == nil && status == want {
			return data
		}
		lastStatus, lastBody, lastErr = status, data, err
		if time.Now().After(deadline) {
			t.Fatalf("timed out waiting for %s: status=%d body=%s err=%v",
				description, lastStatus, miTruncate(lastBody), lastErr)
		}
		select {
		case <-ctx.Done():
			t.Fatalf("context ended while waiting for %s: %v", description, ctx.Err())
		case <-time.After(miPollInterval):
		}
	}
}

// miWaitAgentReachable 轮询跨实例请求直到成功，证明 agent 连接与转发订阅
// 都已就绪。
func miWaitAgentReachable(t *testing.T, ctx context.Context, baseURL, nodeID string) {
	t.Helper()

	miWaitStatus(t, ctx, http.MethodGet, baseURL+"/api/sessions?node="+nodeID, "",
		http.StatusOK, "agent reachable through the other instance")
}

// miWaitNodeConnected 轮询 /api/nodes 直到目标节点的在线状态符合期望。
func miWaitNodeConnected(
	t *testing.T,
	ctx context.Context,
	baseURL, nodeID string,
	want bool,
) {
	t.Helper()

	client := &http.Client{Timeout: miHTTPTimeout}
	deadline := time.Now().Add(miWaitTimeout)
	for {
		status, body, err := miRequest(ctx, client, http.MethodGet, baseURL+"/api/nodes", "")
		if err == nil && status == http.StatusOK {
			var nodes []miNodeView
			if err := json.Unmarshal(body, &nodes); err != nil {
				t.Fatalf("decode nodes %q: %v", miTruncate(body), err)
			}
			for _, node := range nodes {
				if node.ID == nodeID && node.Connected == want {
					return
				}
			}
		}
		if time.Now().After(deadline) {
			t.Fatalf("timed out waiting for node %s connected=%v (status=%d err=%v)",
				nodeID, want, status, err)
		}
		select {
		case <-ctx.Done():
			t.Fatalf("context ended while waiting for node %s: %v", nodeID, ctx.Err())
		case <-time.After(miPollInterval):
		}
	}
}

// miOverviewNode 是 GET /api/overview 里的节点视图；只声明断言需要的字段。
type miOverviewNode struct {
	NodeID   string `json:"node_id"`
	Sessions []struct {
		SessionID string `json:"session_id"`
	} `json:"sessions"`
}

// miWaitOverviewSession 轮询总览直到目标节点出现指定会话。
//
// 单独的等待上限让新实例收敛用例可以跨过一个完整的快照发布周期，而不必
// 放宽所有跨实例用例的 10s 上限。
func miWaitOverviewSession(
	t *testing.T,
	ctx context.Context,
	baseURL, nodeID, sessionID string,
	timeout time.Duration,
) {
	t.Helper()

	client := &http.Client{Timeout: miHTTPTimeout}
	deadline := time.Now().Add(timeout)
	var (
		lastStatus int
		lastBody   []byte
		lastErr    error
	)
	for {
		status, body, err := miRequest(ctx, client, http.MethodGet, baseURL+"/api/overview", "")
		if err == nil && status == http.StatusOK {
			var overview struct {
				Nodes []miOverviewNode `json:"nodes"`
			}
			if decodeErr := json.Unmarshal(body, &overview); decodeErr == nil {
				for _, node := range overview.Nodes {
					if node.NodeID != nodeID {
						continue
					}
					for _, sess := range node.Sessions {
						if sess.SessionID == sessionID {
							return
						}
					}
				}
			}
		}
		lastStatus, lastBody, lastErr = status, body, err
		if time.Now().After(deadline) {
			t.Fatalf("timed out waiting for overview session %s/%s: status=%d body=%s err=%v",
				nodeID, sessionID, lastStatus, miTruncate(lastBody), lastErr)
		}
		select {
		case <-ctx.Done():
			t.Fatalf("context ended while waiting for overview session %s: %v", nodeID, ctx.Err())
		case <-time.After(miPollInterval):
		}
	}
}

// miCreatePTY 通过浏览器 API 创建 PTY 并返回终端 ID。
func miCreatePTY(t *testing.T, ctx context.Context, baseURL, nodeID, sessionID string) string {
	t.Helper()

	body := fmt.Sprintf(`{"directory":%q}`, miDirectory)
	status, data, err := miRequest(ctx, &http.Client{Timeout: miHTTPTimeout}, http.MethodPost,
		fmt.Sprintf("%s/api/sessions/%s/%s/pty", baseURL, nodeID, sessionID), body)
	if err != nil {
		t.Fatalf("create pty: %v", err)
	}
	if status != http.StatusOK {
		t.Fatalf("create pty status = %d, want 200 (body=%s)", status, miTruncate(data))
	}

	var terminal struct {
		ID string `json:"id"`
	}
	if err := json.Unmarshal(data, &terminal); err != nil {
		t.Fatalf("decode pty.create response %q: %v", miTruncate(data), err)
	}
	if terminal.ID == "" {
		t.Fatalf("pty.create returned no id: %s", miTruncate(data))
	}
	return terminal.ID
}

// miDialBrowserPTY 打开浏览器终端 WebSocket。
func miDialBrowserPTY(
	t *testing.T,
	ctx context.Context,
	baseURL, nodeID, sessionID, ptyID string,
) *websocket.Conn {
	t.Helper()

	url := fmt.Sprintf("ws%s/api/pty/%s/%s/%s/ws",
		strings.TrimPrefix(baseURL, "http"), nodeID, sessionID, ptyID)
	dialer := *websocket.DefaultDialer
	conn, resp, err := dialer.DialContext(ctx, url, nil)
	if err != nil {
		if resp != nil && resp.Body != nil {
			_ = resp.Body.Close()
		}
		t.Fatalf("dial browser pty websocket: %v", err)
	}
	t.Cleanup(func() { _ = conn.Close() })
	return conn
}

// miWriteBrowserBinary 写入一个二进制终端输入帧。
func miWriteBrowserBinary(t *testing.T, conn *websocket.Conn, data []byte) {
	t.Helper()

	if err := conn.SetWriteDeadline(time.Now().Add(miHTTPTimeout)); err != nil {
		t.Fatalf("set browser write deadline: %v", err)
	}
	if err := conn.WriteMessage(websocket.BinaryMessage, data); err != nil {
		t.Fatalf("write browser terminal input: %v", err)
	}
}

// miReadBrowser 读取一帧并返回类型与负载。
func miReadBrowser(t *testing.T, conn *websocket.Conn, timeout time.Duration) (int, []byte) {
	t.Helper()

	if err := conn.SetReadDeadline(time.Now().Add(timeout)); err != nil {
		t.Fatalf("set browser read deadline: %v", err)
	}
	kind, data, err := conn.ReadMessage()
	if err != nil {
		t.Fatalf("read browser terminal frame: %v", err)
	}
	return kind, data
}

// miSSE 是浏览器事件流：解析后的 data 帧通过通道交付。
type miSSE struct {
	events <-chan cpevent.Event
}

// miOpenSSE 建立 /api/events 长连接并在后台解析 data 帧。
func miOpenSSE(t *testing.T, parent context.Context, baseURL, query string) *miSSE {
	t.Helper()

	ctx, cancel := context.WithCancel(parent)
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, baseURL+"/api/events"+query, nil)
	if err != nil {
		cancel()
		t.Fatalf("build events request: %v", err)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		cancel()
		t.Fatalf("open event stream: %v", err)
	}
	if resp.StatusCode != http.StatusOK {
		_ = resp.Body.Close()
		cancel()
		t.Fatalf("open event stream status = %d, want 200", resp.StatusCode)
	}

	events := make(chan cpevent.Event, 32)
	go func() {
		defer close(events)
		defer func() { _ = resp.Body.Close() }()

		scanner := bufio.NewScanner(resp.Body)
		scanner.Buffer(make([]byte, 0, 64*1024), 1<<20)
		for scanner.Scan() {
			line := scanner.Text()
			if !strings.HasPrefix(line, "data: ") {
				continue
			}
			var ev cpevent.Event
			if err := json.Unmarshal([]byte(strings.TrimPrefix(line, "data: ")), &ev); err != nil {
				continue
			}
			select {
			case events <- ev:
			case <-ctx.Done():
				return
			}
		}
	}()
	t.Cleanup(cancel)

	return &miSSE{events: events}
}

// miWaitEvent 等待一条满足条件的事件。
func miWaitEvent(
	t *testing.T,
	ctx context.Context,
	events <-chan cpevent.Event,
	description string,
	match func(cpevent.Event) bool,
) cpevent.Event {
	t.Helper()

	deadline := time.After(miWaitTimeout)
	for {
		select {
		case <-ctx.Done():
			t.Fatalf("context ended while waiting for %s: %v", description, ctx.Err())
		case <-deadline:
			t.Fatalf("timed out waiting for %s", description)
		case ev, ok := <-events:
			if !ok {
				t.Fatalf("event stream closed while waiting for %s", description)
			}
			if match(ev) {
				return ev
			}
		}
	}
}

// ---- 推送假实现 ----

// miPushDevices 始终返回一个设备。
type miPushDevices struct{}

// ListDevices 返回单个 iOS 设备。
func (miPushDevices) ListDevices(context.Context) ([]store.Device, error) {
	return []store.Device{{
		ID:        "device-mi-1",
		Name:      "test phone",
		Platform:  "ios",
		PushToken: "ExponentPushToken[mi-test]",
	}}, nil
}

// miPushSender 记录每个会话收到的推送条数，供“只推一次”断言使用。
type miPushSender struct {
	mu         sync.Mutex
	perSession map[string]int
}

// Send 按会话累计消息数。
func (s *miPushSender) Send(_ context.Context, msgs []push.Message) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	if s.perSession == nil {
		s.perSession = make(map[string]int)
	}
	for _, msg := range msgs {
		s.perSession[msg.Data["session"]]++
	}
	return nil
}

// count 返回指定会话累计收到的推送条数。
func (s *miPushSender) count(sessionID string) int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.perSession[sessionID]
}

// miWaitPushCount 等待指定会话的推送计数达到期望值。
func miWaitPushCount(
	t *testing.T,
	ctx context.Context,
	sender *miPushSender,
	sessionID string,
	want int,
) {
	t.Helper()

	deadline := time.Now().Add(miWaitTimeout)
	for {
		if got := sender.count(sessionID); got == want {
			return
		}
		if time.Now().After(deadline) {
			t.Fatalf("push count for session %s = %d, want %d",
				sessionID, sender.count(sessionID), want)
		}
		select {
		case <-ctx.Done():
			t.Fatalf("context ended while waiting for pushes: %v", ctx.Err())
		case <-time.After(miPollInterval):
		}
	}
}

// miWaitGroup 等待 WaitGroup 结束，返回是否在超时前完成。
func miWaitGroup(group *sync.WaitGroup, timeout time.Duration) bool {
	done := make(chan struct{})
	go func() {
		group.Wait()
		close(done)
	}()
	select {
	case <-done:
		return true
	case <-time.After(timeout):
		return false
	}
}

// miNodeName 生成集群内唯一的节点名。
func miNodeName(scenario string) string {
	return fmt.Sprintf("mi-%s-%d", scenario, time.Now().UnixNano())
}

// miTruncate 限制错误信息里回显的响应体长度。
func miTruncate(data []byte) string {
	const limit = 512
	if len(data) > limit {
		return string(data[:limit]) + "..."
	}
	return string(data)
}
