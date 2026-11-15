// Package agentgw 实现控制面的 agent 接入网关。
//
// agent 始终主动外拨到 /api/agent/connect，网关在握手头里按凭证识别节点：
// Authorization: Bearer 既可能是节点凭证（sha256 查库命中），也可能是部署级
// enrollment token（admin 直通，只用于登记新的未认领节点）；Authorization: Pair
// 用于尚未认领的待定连接：握手值只标识这次配对，不是身份凭证，安全性来自
// 连接配额、短生存期以及认领必须在已登录账号里完成；节点要等账号认领后才产生。
// hello 里的 node_name 只更新展示字段，绝不参与身份
// 判定。任何凭证只在握手头与 welcome/pair.result 的下发字段中出现，不进入
// 日志、错误信息、事件或其它帧。
package agentgw

import (
	"context"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/gorilla/websocket"
	"github.com/ygpkg/yg-go/logs"

	"github.com/insmtx/lework/backend/internal/agentd/protocol"
	"github.com/insmtx/lework/backend/internal/controlplane/authn"
	"github.com/insmtx/lework/backend/internal/controlplane/event"
	"github.com/insmtx/lework/backend/internal/controlplane/node"
	"github.com/insmtx/lework/backend/internal/controlplane/store"
	"github.com/insmtx/lework/backend/internal/infra/mq"
	"github.com/insmtx/lework/backend/pkg/messaging"
)

const (
	// defaultHeartbeatTimeout 在配置缺省时兜底，与控制面配置的默认值一致。
	defaultHeartbeatTimeout = 35 * time.Second

	// handshakeTimeout 限制升级与 hello 读取的耗时，避免半开连接长期占用资源。
	handshakeTimeout = 10 * time.Second

	// writeTimeout 是单帧写入的上限：节点读循环变慢时不能无限阻塞请求方。
	writeTimeout = 10 * time.Second

	// bearerScheme 是节点凭证与 admin 直通令牌在握手头中的方案名。
	bearerScheme = "Bearer"

	// pairScheme 标识一条尚未认领的待定连接。握手值只是这次配对的标识而不是
	// 认证：真正的约束来自连接配额、短暂的绝对生存期，以及认领必须在已登录
	// 账号里完成。
	pairScheme = "Pair"

	// maxFrameBytes 是单个入站帧的上限，与协议侧共享同一个常量。
	//
	// gorilla/websocket 默认不限帧大小，没有上限时一个恶意或错乱的节点就能
	// 让控制面按帧内容分配内存；该上限同时覆盖 hello 与后续所有帧。
	maxFrameBytes = protocol.MaxFrameBytes

	// forwardRequestTimeout 限制单次跨实例转发的耗时。
	//
	// 转发 handler 拿到的是订阅的生命周期上下文，若节点迟迟不应答，请求会
	// 一直挂在 Connection 的等待表里，即使调用方早已放弃。每次 handler 都
	// 用该上限再包一层超时，保证待处理项一定被清理。
	forwardRequestTimeout = 30 * time.Second

	// lifecyclePublishTimeout 是清理阶段发布生命周期事件的上限。
	//
	// 连接退出时请求上下文可能已被取消；用脱离取消的短超时上下文发布，
	// 才能把 node.offline 尽力送出去，而不是必然失败。
	lifecyclePublishTimeout = 5 * time.Second

	// ptyEventPrefix 标记易失的 PTY 事件：发布失败只影响当前终端视图。
	ptyEventPrefix = "pty."

	// eventTypeNodeConnected 表示节点连接已就绪、可以接收转发请求。
	eventTypeNodeConnected = "node.connected"

	// eventTypeNodeOffline 表示节点连接已断开。
	eventTypeNodeOffline = "node.offline"
)

// EventPublisher 是网关发布节点事件的面。
//
// 抽成接口而非直接依赖 *event.Publisher，是为了让单测用本地 Hub 就能覆盖
// 发布成功与失败两条路径，不必启动 NATS。
type EventPublisher interface {
	PublishEvent(ctx context.Context, ev event.Event) error
}

// Config 是网关的启动配置。
type Config struct {
	// EnrollmentToken 是部署级共享令牌，属于机密信息，只参与常量时间比较。
	//
	// 保留它是因为旧版本 agent 与单机自托管仍靠它登记；在新的身份模型里它等价
	// 于 admin 直通，只能创建未认领节点，不能冒用任何已激活节点的身份。
	EnrollmentToken string
	// HeartbeatTimeout 是两次入站帧之间允许的最大间隔。
	HeartbeatTimeout time.Duration
}

// authMode 表示一次握手使用的鉴权方式。
type authMode int

const (
	// authNone 表示握手头缺失、方案未知或凭证无效。
	authNone authMode = iota
	// authCredential 表示 Bearer 值命中了某个节点的 credential_hash。
	authCredential
	// authLegacy 表示 Bearer 值是部署级 enrollment token（admin 直通）。
	authLegacy
	// authPair 表示待定连接：握手值只标识这次配对，身份在认领成功后产生。
	authPair
)

// helloParams 是 agent 注册帧的参数，字段与 runner 发送的 hello 一一对应。
type helloParams struct {
	NodeName       string   `json:"node_name"`
	AgentVersion   string   `json:"agent_version"`
	OS             string   `json:"os"`
	Arch           string   `json:"arch"`
	Capabilities   []string `json:"capabilities"`
	WorkspaceRoots []string `json:"workspace_roots"`
}

// welcomeResult 是 welcome 帧的结果；节点 ID 由控制面分配或复用。
//
// Credential 在任何一次旧令牌登记中都会填充：旧二进制忽略它并继续用令牌登记
// （服务端按名复用同一行、每次轮换凭证哈希），升级后的 agent 读到后落盘并改用
// 凭证重连。凭证路径的 welcome 永远不会带它。
type welcomeResult struct {
	NodeID     string `json:"node_id"`
	Credential string `json:"credential,omitempty"`
}

// Gateway 是 agent 的 WebSocket 接入点。
//
// 普通节点的单连接状态仍在该连接的局部变量里；网关只额外记录待定连接的当前
// 连接（pendingConns），用于保证一个 claim 不会被并发投递两次凭证。因此多个
// 节点可以并发接入。
type Gateway struct {
	cfg      Config
	store    *store.Store
	registry *node.Registry
	upgrader websocket.Upgrader

	// publisher 把节点事件送进集群总线；本地 Hub 只是它的一个订阅端。
	publisher EventPublisher

	// coreSub 用于订阅本节点的跨实例转发请求。
	coreSub mq.CoreSubscriber

	// newWriter 构造单连接写入端；默认是 wsWriter，测试可替换为可控写入端，
	// 用于确定性地阻塞 welcome 写出。
	newWriter func(*websocket.Conn) node.Writer

	// pending 是待定连接的并发配额：升级之前获取，连接结束时归还。
	pending *pendingLimiter

	// pendingMu 串行化「同一 claim 只有一条当前连接」的登记与凭证投递。
	//
	// 当前连接检查与凭证轮换必须在同一把锁下完成：否则两条连接可以各自
	// 通过检查、各写一份 credential_hash，后写的那份让先拿到的凭证失效。
	pendingMu sync.Mutex

	// pendingConns 记录每个 claim 当前的待定连接；key 是 claim id。
	pendingConns map[string]*websocket.Conn

	// claimAttempts 限制 mode=claim 的猜码失败次数（按来源 IP 与 pair_key）。
	claimAttempts *claimAttemptLimiter
}

// New 创建网关；HeartbeatTimeout 非正时使用默认值。
func New(
	cfg Config,
	st *store.Store,
	registry *node.Registry,
	publisher EventPublisher,
	coreSub mq.CoreSubscriber,
) *Gateway {
	if cfg.HeartbeatTimeout <= 0 {
		cfg.HeartbeatTimeout = defaultHeartbeatTimeout
	}
	return &Gateway{
		cfg:          cfg,
		store:        st,
		registry:     registry,
		publisher:    publisher,
		coreSub:      coreSub,
		pending:      newPendingLimiter(),
		pendingConns: make(map[string]*websocket.Conn),
		claimAttempts: newClaimAttemptLimiter(
			claimAttemptsPerIP, claimAttemptsPerKey, claimAttemptWindowSize),
		newWriter: func(ws *websocket.Conn) node.Writer {
			return &wsWriter{conn: ws}
		},
		upgrader: websocket.Upgrader{
			HandshakeTimeout: handshakeTimeout,
			// agent 不是浏览器，合法请求不带 Origin；带 Origin 的一律拒绝，
			// 防止网页借本机回环地址偷偷注册节点。
			CheckOrigin: func(r *http.Request) bool { return r.Header.Get("Origin") == "" },
		},
	}
}

// ServeHTTP 完成一次 agent 注册，并持续服务该连接直到它关闭或违规。
//
// 凭证鉴权发生在升级之前：未通过时返回 401 且不占用 WebSocket 连接。Pair
// 头只做待定连接的标识，空值仍在升级前拒绝。成功时把握手方式一并传给 serve，
// 身份解析因此只发生一次，hello 里的名字无法参与。
//
// 待定连接额外受并发配额约束：配额在升级之前获取，升级失败或连接结束时归还，
// 因此被限流的请求只消耗一次 HTTP 往返。来源 IP 解析失败时不计入每 IP 名额
// （见 pendingLimiter），但仍占用全局名额。
func (g *Gateway) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	mode, secret, ok := g.authorize(r)
	if !ok {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return
	}

	var sourceIP string
	if mode == authPair {
		sourceIP = pendingSourceIP(r)
		if !g.pending.acquire(sourceIP) {
			http.Error(w, "too many pending pairings", http.StatusTooManyRequests)
			return
		}
		defer g.pending.release(sourceIP)
	}

	ws, err := g.upgrader.Upgrade(w, r, nil)
	if err != nil {
		// Upgrade 已经写过 HTTP 错误响应，这里不再追加内容。
		return
	}
	// 单帧上限必须在读取 hello 之前设置，才能同时约束握手帧与后续帧。
	ws.SetReadLimit(maxFrameBytes)
	g.serve(r.Context(), ws, mode, secret)
}

// authorize 解析握手头，返回鉴权方式与对应秘密。
//
// Bearer 值先按节点凭证解释：sha256 摘要查 credential_hash 命中即为该节点的
// 身份，secret 返回节点 ID，后续流程无需再保留或比较凭证明文。未命中时才做
// 常量时间比较，与 enrollment token 一致则降级为 admin 直通（authLegacy）。
// 空令牌永不匹配，避免未配置密钥时被空请求头意外放行。
//
// Pair 头不是认证：它只是待定连接的标识。任何非空值都放行到配对流程（空值
// 仍按 401 拒绝），安全性来自连接配额、短暂的绝对生存期，以及认领动作必须
// 在已登录账号里完成；mode=claim 还会按来源 IP 与 pair_key 限制失败次数。
//
// store 查询失败时关闭放行（返回 ok=false）。错误只记日志、绝不回显请求头，
// 因此日志里不会出现被出示的凭证。
func (g *Gateway) authorize(r *http.Request) (authMode, string, bool) {
	scheme, value, ok := strings.Cut(r.Header.Get("Authorization"), " ")
	if !ok {
		return authNone, "", false
	}
	value = strings.TrimSpace(value)
	if value == "" {
		return authNone, "", false
	}

	switch {
	case strings.EqualFold(scheme, bearerScheme):
		rec, found, err := g.store.LookupNodeByCredential(r.Context(), authn.HashToken(value))
		if err != nil {
			logs.Warnf("agentgw: lookup node credential: %v", err)
			return authNone, "", false
		}
		if found {
			return authCredential, rec.ID, true
		}
		if g.matchesEnrollmentToken(value) {
			return authLegacy, value, true
		}
		return authNone, "", false
	case strings.EqualFold(scheme, pairScheme):
		return authPair, value, true
	default:
		return authNone, "", false
	}
}

// matchesEnrollmentToken 以常量时间比较候选值与本机配置的 enrollment token。
//
// 两侧先取 sha256 再比较定长摘要，避免直接比较原文时长度差异泄漏令牌长度。
// 空令牌不参与比较——没有配置密钥就没有可匹配的内容。
func (g *Gateway) matchesEnrollmentToken(candidate string) bool {
	if g.cfg.EnrollmentToken == "" || candidate == "" {
		return false
	}
	want := sha256.Sum256([]byte(g.cfg.EnrollmentToken))
	got := sha256.Sum256([]byte(candidate))
	return subtle.ConstantTimeCompare(got[:], want[:]) == 1
}

// serve 读取 hello、登记节点、发送 welcome，然后进入读循环。
//
// 待定连接（authPair）在进入登记流程之前就转给 servePair：那时还没有节点，
// 只有一条认领码，登记与 welcome 都不适用。
//
// mode 与 secret 来自握手鉴权：authCredential 时 secret 是凭证命中的节点 ID，
// authLegacy 时 secret 不参与登记（鉴权已经完成，旧令牌只能按 hello 的名字新建
// 或复用未认领节点）。节点身份与展示字段在这里分开处理：身份只来自凭证，
// hello 的 node_name 只更新展示信息。
//
// 登记注册表发生在 welcome 成功写出之后：agent 只认 welcome 帧，会丢弃
// 之前收到的所有帧，提前登记会让浏览器请求被写进一条尚未就绪的连接并
// 静默丢失，直到 API 超时。
//
// 登记之后还会订阅本节点的转发请求，使集群内任一实例都能把方法调用交给
// 这条连接应答。无论从哪条路径退出，都只注销本连接、停止转发订阅、发布
// 离线事件、刷新最后活跃时间并关闭套接字。
func (g *Gateway) serve(ctx context.Context, ws *websocket.Conn, mode authMode, secret string) {
	// 待定连接在读完 hello 之前就分派：它不参与 identify/welcome/注册表，
	// 节点要等账号认领后才产生。servePair 自己读取并校验 hello。
	if mode == authPair {
		g.servePair(ctx, ws, mode, secret)
		return
	}

	writer := g.newWriter(ws)

	hello, params, err := readHello(ws)
	if err != nil {
		closeWithError(ws, websocket.CloseProtocolError, "invalid hello")
		_ = writer.Close()
		return
	}

	nodeID, credential, err := g.identify(ctx, mode, secret, params)
	if err != nil {
		// 错误里只有节点 ID 与存储细节，不含任何凭证明文。
		logs.Warnf("agentgw: identify node failed: %v", err)
		closeWithError(ws, websocket.CloseInternalServerErr, "register node failed")
		_ = writer.Close()
		return
	}

	conn := node.NewConnection(nodeID, writer)

	// 转发订阅的取消函数必须在 defer 之前创建：welcome 失败时登记尚未发生，
	// 清理路径仍会调用它，不能是 nil。
	subCtx, cancelSub := context.WithCancel(ctx)

	// 退出清理必须在 welcome 写出之前安装：welcome 失败时连接尚未登记，
	// Unregister 会按指针比较安全地跳过，存储刷新与连接关闭仍然生效。
	//
	// 顺序固定为「注销 -> 停止转发订阅 -> 发布离线事件 -> 关闭连接」：
	// 先注销能让新的本地请求立刻失败，而不是写进一条正在退出的连接；
	// 最后关闭连接保证清理期间不会再有字节被写出。
	defer func() {
		g.registry.Unregister(nodeID, conn)
		cancelSub()
		g.publishLifecycle(ctx, nodeID, eventTypeNodeOffline)
		// 节点行可能已被删除，这里不因刷新失败而改变退出路径。
		_ = g.store.TouchNode(ctx, nodeID, time.Now())
		_ = conn.Close()
	}()

	result, err := json.Marshal(welcomeResult{NodeID: nodeID, Credential: credential})
	if err != nil {
		return
	}
	if err := writer.Write(ctx, protocol.Frame{
		Type:   protocol.FrameWelcome,
		ID:     hello.ID,
		Result: result,
	}); err != nil {
		return
	}

	// welcome 已成功写出，agent 进入就绪状态，此时才允许请求路由到该连接。
	g.registry.Register(node.Info{
		ID:             nodeID,
		Name:           params.NodeName,
		OS:             params.OS,
		Arch:           params.Arch,
		AgentVersion:   params.AgentVersion,
		Capabilities:   params.Capabilities,
		WorkspaceRoots: params.WorkspaceRoots,
		Connected:      true,
		LastSeen:       time.Now(),
	}, conn)

	// 转发订阅必须建立在登记之后：请求落到 subject 时，本实例已经能把方法
	// 调用路由到连接的写入端。
	subscribed := make(chan struct{})
	go func() {
		// SubscribeCore 没有「订阅生效」回调，这里在进入阻塞订阅前放行；
		// 之后发布的 node.connected 因此不会早于订阅动作。
		close(subscribed)
		if err := g.respondRequests(subCtx, nodeID, conn); err != nil && subCtx.Err() == nil {
			logs.Warnf("agentgw: node %s request responder stopped: %v", nodeID, err)
		}
	}()
	<-subscribed
	g.publishLifecycle(ctx, nodeID, eventTypeNodeConnected)

	for {
		// 每个入站帧都必须落在心跳窗口内，否则视为节点失联。
		if err := ws.SetReadDeadline(time.Now().Add(g.cfg.HeartbeatTimeout)); err != nil {
			return
		}
		var frame protocol.Frame
		if err := ws.ReadJSON(&frame); err != nil {
			return
		}
		if err := frame.Validate(); err != nil {
			closeWithError(ws, websocket.CloseProtocolError, "invalid frame")
			return
		}

		switch frame.Type {
		case protocol.FrameHeartbeat:
			if err := g.store.TouchNode(ctx, nodeID, time.Now()); err != nil {
				return
			}
		case protocol.FrameResponse:
			// 响应只交给发起请求的连接；未知 ID 由 Deliver 丢弃。
			_ = conn.Deliver(frame)
		case protocol.FrameEvent:
			if err := g.publishEvent(ctx, nodeID, frame.Event); err != nil {
				// PTY 事件是易失数据，发布失败只影响当前终端视图；丢掉它们
				// 不能让整个节点的会话事件一起中断。
				if frame.Event != nil && strings.HasPrefix(frame.Event.Type, ptyEventPrefix) {
					logs.Warnf("agentgw: node %s publish %s failed: %v", nodeID, frame.Event.Type, err)
					continue
				}
				logs.Warnf("agentgw: node %s publish event failed: %v", nodeID, err)
				return
			}
		case protocol.FrameRequest:
			// 控制面不处理节点主动发起的请求。
			closeWithError(ws, websocket.CloseProtocolError, "unexpected request frame")
			return
		}
	}
}

// identify 按鉴权方式登记节点，返回节点 ID 与需要随 welcome 下发的凭证。
//
// 凭证路径只更新展示字段与在线时间：节点 ID 已经由握手鉴权确定，重写任何身份
// 相关列都会让一条 hello 拥有改身份的能力。旧令牌路径按名复用未认领节点，并且
// **每次**都签发一份新凭证交给调用方经 welcome 下发：连接可能来自读不懂该字段
// 的旧二进制（哈希被轮换、行被复用），也可能来自尚未落盘凭证的升级版 agent
// （它必须能借此迁移到凭证路径）。签发失败必须让握手失败——静默发送不带凭证
// 的 welcome 会让这个节点在每次重连时都被当成新节点，并让升级中的 agent 永远
// 无法迁移。
func (g *Gateway) identify(ctx context.Context, mode authMode, secret string, params helloParams) (string, string, error) {
	meta := nodeFromHello(params)

	switch mode {
	case authCredential:
		if err := g.store.UpdateNodeMeta(ctx, secret, meta); err != nil {
			return "", "", fmt.Errorf("update node meta: %w", err)
		}
		// 握手成功本身就证明节点在线；不刷新 last_seen 会让重连后的节点在
		// 首个心跳到达前被 API 投影成离线，与旧的登记路径行为不一致。
		if err := g.store.TouchNode(ctx, secret, time.Now()); err != nil {
			return "", "", fmt.Errorf("touch node: %w", err)
		}
		return secret, "", nil
	case authLegacy:
		meta.LastSeen = time.Now()
		rec, err := g.store.UpsertLegacyNode(ctx, meta)
		if err != nil {
			return "", "", fmt.Errorf("upsert legacy node: %w", err)
		}
		raw, hash, err := authn.NewToken()
		if err != nil {
			return "", "", fmt.Errorf("issue node credential: %w", err)
		}
		if err := g.store.SetNodeCredential(ctx, rec.ID, hash); err != nil {
			return "", "", fmt.Errorf("store node credential: %w", err)
		}
		return rec.ID, raw, nil
	default:
		// authNone / 未知方式在握手阶段就被拒绝；authPair 由 servePair 处理。
		return "", "", fmt.Errorf("unsupported auth mode %d", mode)
	}
}

// nodeFromHello 把 hello 参数转成只含展示字段的节点记录。
//
// 单独抽出来是为了让两条登记路径共用同一份字段映射：任何一方多写一个身份
// 相关字段（ID、凭证、认领时间）都会绕过鉴权模型，这里刻意不提供那些字段。
func nodeFromHello(params helloParams) store.Node {
	return store.Node{
		Name:           params.NodeName,
		OS:             params.OS,
		Arch:           params.Arch,
		AgentVersion:   params.AgentVersion,
		Capabilities:   params.Capabilities,
		WorkspaceRoots: params.WorkspaceRoots,
	}
}

// publishEvent 把节点事件交给集群总线。
//
// NodeID 一律以连接身份为准，覆盖 agent 自带的值，避免节点伪造或复用
// 其它节点的身份；Time 缺失时补上控制面接收时间。
func (g *Gateway) publishEvent(ctx context.Context, nodeID string, ev *protocol.Event) error {
	if ev == nil {
		return nil
	}
	// node.* 是控制面的生命周期/快照命名空间，agent 不得伪造；丢弃而不是
	// 断开连接，避免一个有 bug 的节点因此完全失联。
	if strings.HasPrefix(ev.Type, "node.") {
		logs.Warnf("agentgw: node %s sent reserved event type %q; dropping", nodeID, ev.Type)
		return nil
	}
	published := event.Event{
		NodeID:    nodeID,
		SessionID: ev.SessionID,
		Seq:       ev.Seq,
		Type:      ev.Type,
		Data:      ev.Data,
		Time:      ev.Time,
	}
	if published.Time.IsZero() {
		published.Time = time.Now()
	}
	if err := g.publisher.PublishEvent(ctx, published); err != nil {
		return fmt.Errorf("publish %s: %w", published.Type, err)
	}
	return nil
}

// publishLifecycle 发布连接生命周期事件。
//
// 生命周期事件只用于其它实例的状态投影，发布失败不能让连接建立或退出失败，
// 因此只记日志；清理阶段的上下文可能已被取消，这里脱离取消再带短超时。
func (g *Gateway) publishLifecycle(ctx context.Context, nodeID, eventType string) {
	ctx, cancel := context.WithTimeout(context.WithoutCancel(ctx), lifecyclePublishTimeout)
	defer cancel()

	ev := event.Event{NodeID: nodeID, Type: eventType, Time: time.Now()}
	if err := g.publisher.PublishEvent(ctx, ev); err != nil {
		logs.Warnf("agentgw: publish %s for node %s: %v", eventType, nodeID, err)
	}
}

// respondRequests 订阅本节点的跨实例转发请求，直到 ctx 取消。
//
// queue 使用节点 ID：同一节点重连时新旧订阅短暂并存，队列组保证一个请求
// 只被其中一个订阅处理。
func (g *Gateway) respondRequests(ctx context.Context, nodeID string, conn *node.Connection) error {
	subject, err := messaging.ControlPlaneNodeRequestSubject(nodeID)
	if err != nil {
		return fmt.Errorf("forward requests: %w", err)
	}
	err = g.coreSub.SubscribeCore(ctx, subject, nodeID, func(ctx context.Context, payload []byte) []byte {
		return g.forwardRequest(ctx, conn, payload)
	})
	if err != nil {
		return fmt.Errorf("subscribe %s: %w", subject, err)
	}
	return nil
}

// forwardRequest 把一条转发请求写到节点连接，并把结果编码成 ForwardResponse。
//
// handler 必须总是返回可解析的应答，返回 nil 只留给编码失败：请求方拿到
// 空回复会把它当成没有 responder，更难排查。
func (g *Gateway) forwardRequest(ctx context.Context, conn *node.Connection, payload []byte) []byte {
	reqCtx, cancel := context.WithTimeout(ctx, forwardRequestTimeout)
	defer cancel()

	var req node.ForwardRequest
	if err := json.Unmarshal(payload, &req); err != nil {
		// 不回显负载：转发请求可能带提示词等用户内容。
		return marshalForwardResponse(node.ForwardResponse{
			Error: &node.ForwardError{Code: "bad_request", Message: "invalid forward request"},
		})
	}

	var resp node.ForwardResponse
	result, err := conn.Request(reqCtx, req.Method, req.Params)
	switch {
	case err == nil:
		resp.Result = result
	case errors.Is(err, node.ErrNodeOffline), errors.Is(err, node.ErrConnectionClosed):
		// 路由检查之后节点才断开：语义仍是离线，不能让调用方看到 502。
		resp.Error = &node.ForwardError{Code: "node_offline", Message: "node is offline"}
	case errors.Is(err, context.DeadlineExceeded):
		// 不把内部超时细节带出去；调用方只需要知道是 agent 太慢。
		resp.Error = &node.ForwardError{Code: "agent_timeout", Message: "agent request timed out"}
	default:
		var remote *node.RemoteError
		if errors.As(err, &remote) {
			resp.Error = &node.ForwardError{Code: remote.Code, Message: remote.Message}
		} else {
			resp.Error = &node.ForwardError{Code: "agent_error", Message: err.Error()}
		}
	}
	return marshalForwardResponse(resp)
}

// marshalForwardResponse 编码转发应答；失败时返回 nil，避免返回半截 JSON。
func marshalForwardResponse(resp node.ForwardResponse) []byte {
	data, err := json.Marshal(resp)
	if err != nil {
		logs.Warnf("agentgw: marshal forward response: %v", err)
		return nil
	}
	return data
}

// readHello 读取并校验注册帧，返回原帧（复用其 ID）与参数。
func readHello(ws *websocket.Conn) (protocol.Frame, helloParams, error) {
	if err := ws.SetReadDeadline(time.Now().Add(handshakeTimeout)); err != nil {
		return protocol.Frame{}, helloParams{}, fmt.Errorf("set hello read deadline: %w", err)
	}

	var frame protocol.Frame
	if err := ws.ReadJSON(&frame); err != nil {
		return protocol.Frame{}, helloParams{}, fmt.Errorf("read hello: %w", err)
	}
	if err := frame.Validate(); err != nil {
		return protocol.Frame{}, helloParams{}, fmt.Errorf("invalid hello frame: %w", err)
	}
	if frame.Type != protocol.FrameHello {
		return protocol.Frame{}, helloParams{}, fmt.Errorf("first frame type %q is not hello", frame.Type)
	}
	if len(frame.Params) == 0 {
		return protocol.Frame{}, helloParams{}, errors.New("hello frame has no params")
	}

	var params helloParams
	if err := json.Unmarshal(frame.Params, &params); err != nil {
		return protocol.Frame{}, helloParams{}, fmt.Errorf("decode hello params: %w", err)
	}
	params.NodeName = strings.TrimSpace(params.NodeName)
	if params.NodeName == "" {
		return protocol.Frame{}, helloParams{}, errors.New("hello params require node_name")
	}
	return frame, params, nil
}

// closeWithError 发送关闭帧后由调用方关闭套接字。关闭帧只携带固定文本，
// 不回显入站内容，避免把节点数据写进对端错误信息。
func closeWithError(ws *websocket.Conn, code int, reason string) {
	_ = ws.WriteControl(
		websocket.CloseMessage,
		websocket.FormatCloseMessage(code, reason),
		time.Now().Add(time.Second),
	)
}

// wsWriter 把 protocol.Frame 写成 WebSocket JSON 文本帧。
//
// gorilla/websocket 只允许一个并发写者，而 node.Connection 的请求和网关
// 自身的 welcome、关闭帧可能同时写同一连接，因此这里用互斥锁串行化。
type wsWriter struct {
	conn *websocket.Conn
	mu   sync.Mutex
}

// Write 在上下文截止时间前写出一个帧。
func (w *wsWriter) Write(ctx context.Context, f protocol.Frame) error {
	if err := ctx.Err(); err != nil {
		return fmt.Errorf("agentgw: write frame: %w", err)
	}

	w.mu.Lock()
	defer w.mu.Unlock()

	deadline := time.Now().Add(writeTimeout)
	if d, ok := ctx.Deadline(); ok && d.Before(deadline) {
		deadline = d
	}
	if err := w.conn.SetWriteDeadline(deadline); err != nil {
		return fmt.Errorf("agentgw: set write deadline: %w", err)
	}
	if err := w.conn.WriteJSON(f); err != nil {
		return fmt.Errorf("agentgw: write frame: %w", err)
	}
	return nil
}

// Close 关闭底层 WebSocket 连接。
func (w *wsWriter) Close() error {
	if err := w.conn.Close(); err != nil {
		return fmt.Errorf("agentgw: close connection: %w", err)
	}
	return nil
}
