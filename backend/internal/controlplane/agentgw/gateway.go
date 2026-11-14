// Package agentgw 实现控制面的 agent 接入网关。
//
// agent 始终主动外拨到 /api/agent/connect，网关负责校验注册令牌、读取
// 一次 hello 完成节点登记，随后把心跳、响应和事件分别交给节点存储、
// 注册表与事件中心。注册令牌只在 HTTP 握手头中出现，不进入任何帧、
// 日志或错误信息。
package agentgw

import (
	"context"
	"crypto/subtle"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/gorilla/websocket"

	"github.com/insmtx/lework/backend/internal/agentd/protocol"
	"github.com/insmtx/lework/backend/internal/controlplane/event"
	"github.com/insmtx/lework/backend/internal/controlplane/node"
	"github.com/insmtx/lework/backend/internal/controlplane/store"
)

const (
	// defaultHeartbeatTimeout 在配置缺省时兜底，与控制面配置的默认值一致。
	defaultHeartbeatTimeout = 35 * time.Second

	// handshakeTimeout 限制升级与 hello 读取的耗时，避免半开连接长期占用资源。
	handshakeTimeout = 10 * time.Second

	// writeTimeout 是单帧写入的上限：节点读循环变慢时不能无限阻塞请求方。
	writeTimeout = 10 * time.Second

	// bearerPrefix 是注册令牌在握手头中的前缀。
	bearerPrefix = "Bearer "

	// maxFrameBytes 是单个入站帧的上限（1 MiB）。gorilla/websocket 默认不限
	// 帧大小，没有上限时一个恶意或错乱的节点就能让控制面按帧内容分配内存；
	// 该上限同时覆盖 hello 与后续所有帧。
	maxFrameBytes = 1 << 20
)

// Config 是网关的启动配置。
type Config struct {
	// EnrollmentToken 是节点注册令牌，属于机密信息，只参与常量时间比较。
	EnrollmentToken string
	// HeartbeatTimeout 是两次入站帧之间允许的最大间隔。
	HeartbeatTimeout time.Duration
}

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
type welcomeResult struct {
	NodeID string `json:"node_id"`
}

// Gateway 是 agent 的 WebSocket 接入点。
//
// 网关不持有单连接状态：每次 ServeHTTP 都在局部变量中维护该连接的
// 注册信息与清理逻辑，因此多个节点可以并发接入。
type Gateway struct {
	cfg      Config
	store    *store.Store
	registry *node.Registry
	hub      *event.Hub
	upgrader websocket.Upgrader

	// newWriter 构造单连接写入端；默认是 wsWriter，测试可替换为可控写入端，
	// 用于确定性地阻塞 welcome 写出。
	newWriter func(*websocket.Conn) node.Writer
}

// New 创建网关；HeartbeatTimeout 非正时使用默认值。
func New(cfg Config, st *store.Store, registry *node.Registry, hub *event.Hub) *Gateway {
	if cfg.HeartbeatTimeout <= 0 {
		cfg.HeartbeatTimeout = defaultHeartbeatTimeout
	}
	return &Gateway{
		cfg:      cfg,
		store:    st,
		registry: registry,
		hub:      hub,
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
// 令牌校验发生在升级之前：未通过时返回 401 且不占用 WebSocket 连接。
func (g *Gateway) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if !g.authorized(r) {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return
	}

	ws, err := g.upgrader.Upgrade(w, r, nil)
	if err != nil {
		// Upgrade 已经写过 HTTP 错误响应，这里不再追加内容。
		return
	}
	// 单帧上限必须在读取 hello 之前设置，才能同时约束握手帧与后续帧。
	ws.SetReadLimit(maxFrameBytes)
	g.serve(r.Context(), ws)
}

// authorized 以常量时间比较握手头中的令牌。
//
// 未配置令牌时一律拒绝，避免空令牌被空请求头意外匹配。
func (g *Gateway) authorized(r *http.Request) bool {
	if g.cfg.EnrollmentToken == "" {
		return false
	}
	token := strings.TrimPrefix(r.Header.Get("Authorization"), bearerPrefix)
	return subtle.ConstantTimeCompare([]byte(token), []byte(g.cfg.EnrollmentToken)) == 1
}

// serve 读取 hello、登记节点、发送 welcome，然后进入读循环。
//
// 登记注册表发生在 welcome 成功写出之后：agent 只认 welcome 帧，会丢弃
// 之前收到的所有帧，提前登记会让浏览器请求被写进一条尚未就绪的连接并
// 静默丢失，直到 API 超时。
//
// 无论从哪条路径退出，都只注销本连接、刷新最后活跃时间并关闭套接字。
func (g *Gateway) serve(ctx context.Context, ws *websocket.Conn) {
	writer := g.newWriter(ws)

	hello, params, err := readHello(ws)
	if err != nil {
		closeWithError(ws, websocket.CloseProtocolError, "invalid hello")
		_ = writer.Close()
		return
	}

	rec, err := g.store.UpsertNode(ctx, store.Node{
		Name:           params.NodeName,
		OS:             params.OS,
		Arch:           params.Arch,
		AgentVersion:   params.AgentVersion,
		Capabilities:   params.Capabilities,
		WorkspaceRoots: params.WorkspaceRoots,
		LastSeen:       time.Now(),
	})
	if err != nil {
		closeWithError(ws, websocket.CloseInternalServerErr, "register node failed")
		_ = writer.Close()
		return
	}

	conn := node.NewConnection(rec.ID, writer)

	// 退出清理必须在 welcome 写出之前安装：welcome 失败时连接尚未登记，
	// Unregister 会按指针比较安全地跳过，存储刷新与连接关闭仍然生效。
	defer func() {
		g.registry.Unregister(rec.ID, conn)
		// 节点行可能已被删除，这里不因刷新失败而改变退出路径。
		_ = g.store.TouchNode(ctx, rec.ID, time.Now())
		_ = conn.Close()
	}()

	result, err := json.Marshal(welcomeResult{NodeID: rec.ID})
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
		ID:             rec.ID,
		Name:           rec.Name,
		OS:             rec.OS,
		Arch:           rec.Arch,
		AgentVersion:   rec.AgentVersion,
		Capabilities:   rec.Capabilities,
		WorkspaceRoots: rec.WorkspaceRoots,
		Connected:      true,
		LastSeen:       time.Now(),
	}, conn)

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
			if err := g.store.TouchNode(ctx, rec.ID, time.Now()); err != nil {
				return
			}
		case protocol.FrameResponse:
			// 响应只交给发起请求的连接；未知 ID 由 Deliver 丢弃。
			_ = conn.Deliver(frame)
		case protocol.FrameEvent:
			g.publishEvent(rec.ID, frame.Event)
		case protocol.FrameRequest:
			// 控制面不处理节点主动发起的请求。
			closeWithError(ws, websocket.CloseProtocolError, "unexpected request frame")
			return
		}
	}
}

// publishEvent 把节点事件交给事件中心。
//
// NodeID 一律以连接身份为准，覆盖 agent 自带的值，避免节点伪造或复用
// 其它节点的身份；Time 缺失时补上控制面接收时间。
func (g *Gateway) publishEvent(nodeID string, ev *protocol.Event) {
	if ev == nil {
		return
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
	g.hub.Publish(published)
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
