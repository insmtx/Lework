// Package api 提供控制面的浏览器 HTTP/JSON 接口与 SSE 事件流。
//
// 浏览器只能通过这里声明的显式端点访问节点，不会拿到节点连接或注册
// 令牌；每次节点调用都带请求超时，慢节点不会无限占用 HTTP 处理协程。
package api

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"time"

	"github.com/insmtx/lework/backend/internal/controlplane/authz"
	"github.com/insmtx/lework/backend/internal/controlplane/event"
	"github.com/insmtx/lework/backend/internal/controlplane/node"
	"github.com/insmtx/lework/backend/internal/controlplane/overview"
	"github.com/insmtx/lework/backend/internal/controlplane/store"
	"github.com/insmtx/lework/backend/internal/infra/mq"
)

// defaultRequestTimeout 在构造参数非正时兜底，避免零超时让所有节点调用
// 立即失败。
const defaultRequestTimeout = 30 * time.Second

// defaultHeartbeatTimeout 在构造参数非正时兜底，避免把所有节点都判成离线。
const defaultHeartbeatTimeout = 35 * time.Second

// maxRequestBodyBytes 是浏览器请求体的上限（1 MiB）。控制面的请求体都很小，
// 先限制再解码可以避免超大请求体把内存交给对端控制。
const maxRequestBodyBytes = 1 << 20

// defaultEventsRefresh 是 SSE 连接重算可见节点集合的周期。
//
// 成员变更没有强一致的广播通道（node.member.* 事件也可能跨实例丢失），
// 因此设计 §9 允许最多 30 秒的生效延迟；订阅期间按这个周期兜底重算。
const defaultEventsRefresh = 30 * time.Second

// errorResponse 是浏览器 API 的统一错误体。
type errorResponse struct {
	Code    string `json:"code"`
	Message string `json:"message"`
}

// API 是控制面的浏览器接口。它只读取存储、实时注册表与集群总线，不持有
// 任何业务状态。
type API struct {
	store    *store.Store
	registry *node.Registry
	hub      *event.Hub
	index    *overview.Index
	router   *node.Router
	coreSub  mq.CoreSubscriber

	// resolver 用于初始化接口的 enrollment token 比较；身份解析本身发生在
	// 中间件里，API 不重复解释 cookie 与 Bearer。
	resolver authz.Resolver
	// authCfg 是会话有效期与注册策略。
	authCfg AuthConfig

	requestTimeout time.Duration
	// heartbeatTimeout 决定存储里的 LastSeen 多久之后算离线。跨实例部署时
	// 节点可能连在别的实例上，因此在线判定不能只看本实例注册表。
	heartbeatTimeout time.Duration
	// eventsRefresh 是 SSE 连接重算可见节点集合的周期。放进字段而不是常量，
	// 是为了让测试能把它缩短到毫秒级，不必真的等 30 秒。
	eventsRefresh time.Duration
}

// New 组装浏览器 API；requestTimeout 与 heartbeatTimeout 非正时使用默认值。
// index 是总览的派生索引，调用方负责喂事件；为 nil 时总览端点会明确报错。
// registry 保留用于同进程的节点元数据查询，在线状态以存储的心跳窗口为准。
func New(
	st *store.Store,
	registry *node.Registry,
	hub *event.Hub,
	index *overview.Index,
	router *node.Router,
	coreSub mq.CoreSubscriber,
	resolver authz.Resolver,
	authCfg AuthConfig,
	requestTimeout, heartbeatTimeout time.Duration,
) *API {
	if requestTimeout <= 0 {
		requestTimeout = defaultRequestTimeout
	}
	if heartbeatTimeout <= 0 {
		heartbeatTimeout = defaultHeartbeatTimeout
	}
	return &API{
		store:            st,
		registry:         registry,
		hub:              hub,
		index:            index,
		router:           router,
		coreSub:          coreSub,
		resolver:         resolver,
		authCfg:          authCfg,
		requestTimeout:   requestTimeout,
		heartbeatTimeout: heartbeatTimeout,
		eventsRefresh:    defaultEventsRefresh,
	}
}

// nodeConnected 用存储里的最后心跳判断节点是否在线。
//
// LastSeen 为零（从未上报心跳）或早于心跳窗口都视为离线：只有窗口内的
// 心跳才能证明节点仍被某个实例连着。
func (a *API) nodeConnected(rec store.Node) bool {
	if rec.LastSeen.IsZero() {
		return false
	}
	return time.Since(rec.LastSeen) <= a.heartbeatTimeout
}

// Register 把全部浏览器端点注册到 mux。方法与路径由 ServeMux 模式约束，
// 未声明的方法会落到静态资源处理器（通常得到 404，而不是 401）。
func (a *API) Register(mux *http.ServeMux) {
	mux.HandleFunc("POST /api/auth/bootstrap", a.handleBootstrap)
	mux.HandleFunc("POST /api/auth/login", a.handleLogin)
	mux.HandleFunc("POST /api/auth/register", a.handleRegister)
	mux.HandleFunc("POST /api/auth/logout", a.handleLogout)
	// 设计 §8 用 DELETE /api/auth/session 描述登出；任务书与前端使用
	// POST /api/auth/logout。两个路径指向同一 handler，避免文档与实现漂移时
	// 任一方客户端拿到 404。
	mux.HandleFunc("DELETE /api/auth/session", a.handleLogout)
	mux.HandleFunc("GET /api/me", a.handleMe)

	mux.HandleFunc("GET /api/nodes", a.handleListNodes)
	// 认领不走 nodeRoute：未认领节点没有成员行，成员授权必然把请求挡成 404。
	// 这条路由的授权在 handler 里按「管理员 + 账号」判定。
	mux.HandleFunc("POST /api/nodes/{id}/claim", a.handleClaimNode)
	a.nodeRoute(mux, "GET /api/nodes/{id}", a.handleGetNode)
	a.nodeRoute(mux, "GET /api/nodes/{id}/health", a.handleNodeHealth)
	a.nodeRoute(mux, "POST /api/nodes/{id}/opencode/start", a.handleOpenCodeStart)
	a.nodeRoute(mux, "POST /api/nodes/{id}/opencode/restart", a.handleOpenCodeRestart)
	a.nodeRoute(mux, "POST /api/nodes/{id}/opencode/stop", a.handleOpenCodeStop)
	a.nodeRoute(mux, "GET /api/nodes/{id}/files", a.handleFileList)
	a.nodeRoute(mux, "GET /api/nodes/{id}/files/content", a.handleFileContent)

	mux.HandleFunc("GET /api/sessions", a.handleListSessions)
	mux.HandleFunc("POST /api/sessions", a.handleCreateSession)
	a.nodeRoute(mux, "PATCH /api/sessions/{nodeID}/{sessionID}", a.handleRenameSession)
	a.nodeRoute(mux, "DELETE /api/sessions/{nodeID}/{sessionID}", a.handleDeleteSession)
	a.nodeRoute(mux, "GET /api/sessions/{nodeID}/{sessionID}/messages", a.handleSessionMessages)
	a.nodeRoute(mux, "POST /api/sessions/{nodeID}/{sessionID}/prompt", a.handleSessionPrompt)
	a.nodeRoute(mux, "GET /api/sessions/{nodeID}/{sessionID}/diff", a.handleSessionDiff)
	a.nodeRoute(mux, "POST /api/sessions/{nodeID}/{sessionID}/interrupt", a.handleSessionInterrupt)
	a.nodeRoute(mux, "POST /api/sessions/{nodeID}/{sessionID}/pty", a.handlePTYCreate)
	a.nodeRoute(mux, "DELETE /api/sessions/{nodeID}/{sessionID}/pty/{ptyID}", a.handlePTYClose)
	a.nodeRoute(mux, "GET /api/pty/{nodeID}/{sessionID}/{ptyID}/ws", a.handlePTYWebSocket)
	a.nodeRoute(mux, "POST /api/sessions/{nodeID}/{sessionID}/permissions/{permissionID}/reply",
		a.handlePermissionReply)
	a.nodeRoute(mux, "GET /api/sessions/{nodeID}/{sessionID}/permissions", a.handleListPermissions)

	mux.HandleFunc("GET /api/events", a.handleEvents)

	mux.HandleFunc("GET /api/overview", a.handleOverview)

	mux.HandleFunc("GET /api/projects", a.handleListProjects)
	a.nodeRoute(mux, "GET /api/nodes/{id}/projects", a.handleNodeProjects)
	mux.HandleFunc("PUT /api/projects/{key}/primary-node", a.handleSetProjectPrimaryNode)
	mux.HandleFunc("GET /api/preferences", a.handleListPreferences)
	mux.HandleFunc("PUT /api/preferences/{key}", a.handleSetPreference)

	mux.HandleFunc("GET /api/devices", a.handleListDevices)
	mux.HandleFunc("POST /api/devices", a.handleRegisterDevice)
	mux.HandleFunc("DELETE /api/devices/{id}", a.handleDeleteDevice)
}

// callAgent 把一次浏览器请求交给路由器：本地连接优先，否则转发到持有连接
// 的实例；两种路径都受同一个请求超时约束。
// params 为 nil 时不携带参数；编码失败的错误只包含方法名，不含参数内容。
func (a *API) callAgent(ctx context.Context, nodeID, method string, params any) (json.RawMessage, error) {
	var raw json.RawMessage
	if params != nil {
		encoded, err := json.Marshal(params)
		if err != nil {
			return nil, fmt.Errorf("encode %s params: %w", method, err)
		}
		raw = encoded
	}

	callCtx, cancel := context.WithTimeout(ctx, a.requestTimeout)
	defer cancel()
	return a.router.Request(callCtx, nodeID, method, raw)
}

// decodeRequest 解析 JSON 请求体，并把请求体限制在 maxRequestBodyBytes 之内；
// 未知字段被忽略，便于前端先行添加字段。超限请求体按解码失败处理，调用方
// 统一回 400。
func decodeRequest(w http.ResponseWriter, r *http.Request, dst any) error {
	r.Body = http.MaxBytesReader(w, r.Body, maxRequestBodyBytes)
	if err := json.NewDecoder(r.Body).Decode(dst); err != nil {
		return fmt.Errorf("decode request body: %w", err)
	}
	return nil
}

// writeJSON 写出 JSON 响应。
func writeJSON(w http.ResponseWriter, status int, value any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(value)
}

// writeRawJSON 原样透传节点结果；空结果写成 null，保持响应始终是 JSON。
func writeRawJSON(w http.ResponseWriter, status int, raw json.RawMessage) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	if len(raw) == 0 {
		_, _ = w.Write([]byte("null\n"))
		return
	}
	_, _ = w.Write(raw)
	_, _ = w.Write([]byte("\n"))
}

// writeAPIError 写出统一错误体。
func writeAPIError(w http.ResponseWriter, status int, code, message string) {
	writeJSON(w, status, errorResponse{Code: code, Message: message})
}

// writeAgentError 把节点调用错误映射为稳定的 HTTP 状态与错误码。
//
// 连接在查询之后、请求之前消失同样说明节点离线，因此 ErrConnectionClosed
// 与 ErrNodeOffline 使用相同映射；远端错误的 Message 由协议保证不含机密
// 信息，可以直接展示给浏览器。
func (a *API) writeAgentError(w http.ResponseWriter, err error) {
	var remote *node.RemoteError

	switch {
	case errors.Is(err, node.ErrNodeOffline), errors.Is(err, node.ErrConnectionClosed):
		writeAPIError(w, http.StatusConflict, "node_offline", "node is offline")
	case errors.Is(err, context.DeadlineExceeded):
		writeAPIError(w, http.StatusGatewayTimeout, "agent_timeout", "agent request timed out")
	case errors.As(err, &remote):
		message := "agent returned an error"
		if remote.Message != "" {
			message = remote.Message
		}
		writeAPIError(w, http.StatusBadGateway, "agent_error", message)
	default:
		writeAPIError(w, http.StatusInternalServerError, "internal_error", "internal error")
	}
}
