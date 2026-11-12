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

	"github.com/insmtx/lework/backend/internal/controlplane/event"
	"github.com/insmtx/lework/backend/internal/controlplane/node"
	"github.com/insmtx/lework/backend/internal/controlplane/overview"
	"github.com/insmtx/lework/backend/internal/controlplane/store"
)

// defaultRequestTimeout 在构造参数非正时兜底，避免零超时让所有节点调用
// 立即失败。
const defaultRequestTimeout = 30 * time.Second

// maxRequestBodyBytes 是浏览器请求体的上限（1 MiB）。控制面的请求体都很小，
// 先限制再解码可以避免超大请求体把内存交给对端控制。
const maxRequestBodyBytes = 1 << 20

// errorResponse 是浏览器 API 的统一错误体。
type errorResponse struct {
	Code    string `json:"code"`
	Message string `json:"message"`
}

// API 是控制面的浏览器接口。它只读取存储与实时注册表，不持有连接状态。
type API struct {
	store          *store.Store
	registry       *node.Registry
	hub            *event.Hub
	index          *overview.Index
	requestTimeout time.Duration
}

// New 组装浏览器 API；requestTimeout 非正时使用 30 秒默认值。
// index 是总览的派生索引，调用方负责喂事件；为 nil 时总览端点会明确报错。
func New(st *store.Store, registry *node.Registry, hub *event.Hub, index *overview.Index, requestTimeout time.Duration) *API {
	if requestTimeout <= 0 {
		requestTimeout = defaultRequestTimeout
	}
	return &API{
		store:          st,
		registry:       registry,
		hub:            hub,
		index:          index,
		requestTimeout: requestTimeout,
	}
}

// Register 把全部浏览器端点注册到 mux。方法与路径由 ServeMux 模式约束，
// 未声明的方法自动得到 405。
func (a *API) Register(mux *http.ServeMux) {
	mux.HandleFunc("GET /api/nodes", a.handleListNodes)
	mux.HandleFunc("GET /api/nodes/{id}", a.handleGetNode)
	mux.HandleFunc("GET /api/nodes/{id}/health", a.handleNodeHealth)
	mux.HandleFunc("GET /api/nodes/{id}/files", a.handleFileList)
	mux.HandleFunc("GET /api/nodes/{id}/files/content", a.handleFileContent)

	mux.HandleFunc("GET /api/sessions", a.handleListSessions)
	mux.HandleFunc("POST /api/sessions", a.handleCreateSession)
	mux.HandleFunc("GET /api/sessions/{nodeID}/{sessionID}/messages", a.handleSessionMessages)
	mux.HandleFunc("POST /api/sessions/{nodeID}/{sessionID}/prompt", a.handleSessionPrompt)
	mux.HandleFunc("GET /api/sessions/{nodeID}/{sessionID}/diff", a.handleSessionDiff)
	mux.HandleFunc("POST /api/sessions/{nodeID}/{sessionID}/interrupt", a.handleSessionInterrupt)
	mux.HandleFunc("POST /api/sessions/{nodeID}/{sessionID}/pty", a.handlePTYCreate)
	mux.HandleFunc("DELETE /api/sessions/{nodeID}/{sessionID}/pty/{ptyID}", a.handlePTYClose)
	mux.HandleFunc("GET /api/pty/{nodeID}/{sessionID}/{ptyID}/ws", a.handlePTYWebSocket)
	mux.HandleFunc("POST /api/sessions/{nodeID}/{sessionID}/permissions/{permissionID}/reply", a.handlePermissionReply)
	mux.HandleFunc("GET /api/sessions/{nodeID}/{sessionID}/permissions", a.handlePendingPermissions)

	mux.HandleFunc("GET /api/events", a.handleEvents)

	mux.HandleFunc("GET /api/overview", a.handleOverview)

	mux.HandleFunc("GET /api/devices", a.handleListDevices)
	mux.HandleFunc("POST /api/devices", a.handleRegisterDevice)
	mux.HandleFunc("DELETE /api/devices/{id}", a.handleDeleteDevice)
}

// callAgent 把一次浏览器请求转发给节点，并用请求超时约束等待。
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
	return a.registry.Request(callCtx, nodeID, method, raw)
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
