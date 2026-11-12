package api

import (
	"net/http"
	"path/filepath"
	"strconv"
	"strings"
)

// createSessionRequest 是 POST /api/sessions 的请求体。
type createSessionRequest struct {
	NodeID    string `json:"node_id"`
	Directory string `json:"directory"`
}

// sessionRenameRequest 是 PATCH /api/sessions/{nodeID}/{sessionID} 的请求体。
type sessionRenameRequest struct {
	Title string `json:"title"`
}

// promptRequest 是 POST .../prompt 的请求体。
type promptRequest struct {
	Text           string `json:"text"`
	IdempotencyKey string `json:"idempotency_key"`
}

// permissionReplyRequest 是 POST .../permissions/{id}/reply 的请求体。
type permissionReplyRequest struct {
	Decision string `json:"decision"`
	Message  string `json:"message"`
}

// 以下参数结构与 agentd/handler 的协议参数一一对应，字段名不可随意更改。
type sessionListParams struct {
	Directory string `json:"directory"`
}

type sessionCreateParams struct {
	Directory string `json:"directory"`
}

type sessionRenameParams struct {
	SessionID string `json:"session_id"`
	Title     string `json:"title"`
}

type sessionDeleteParams struct {
	SessionID string `json:"session_id"`
}

type sessionPromptParams struct {
	SessionID      string `json:"session_id"`
	Text           string `json:"text"`
	IdempotencyKey string `json:"idempotency_key"`
}

type sessionMessagesParams struct {
	SessionID string `json:"session_id"`
}

type permissionReplyParams struct {
	SessionID    string `json:"session_id"`
	PermissionID string `json:"permission_id"`
	Decision     string `json:"decision"`
	Message      string `json:"message"`
}

type sessionDiffParams struct {
	SessionID string `json:"session_id"`
	Directory string `json:"directory"`
	From      string `json:"from,omitempty"`
	To        string `json:"to,omitempty"`
	Context   int    `json:"context,omitempty"`
}

type sessionInterruptParams struct {
	SessionID string `json:"session_id"`
}

type ptyCreateRequest struct {
	Directory string   `json:"directory"`
	Cols      int      `json:"cols"`
	Rows      int      `json:"rows"`
	Command   string   `json:"command"`
	Args      []string `json:"args"`
}

type ptyCreateParams struct {
	SessionID string   `json:"session_id"`
	Directory string   `json:"directory"`
	Cols      int      `json:"cols"`
	Rows      int      `json:"rows"`
	Command   string   `json:"command"`
	Args      []string `json:"args"`
}

type ptyCloseParams struct {
	PTYID string `json:"pty_id"`
}

// handleListSessions 透传节点的 session.list 结果。
func (a *API) handleListSessions(w http.ResponseWriter, r *http.Request) {
	query := r.URL.Query()
	nodeID := query.Get("node")
	if nodeID == "" {
		// 缺少 node 是调用方的问题，不能当成“节点离线”。
		writeAPIError(w, http.StatusBadRequest, "invalid_request", "node is required")
		return
	}

	result, err := a.callAgent(r.Context(), nodeID, "session.list",
		sessionListParams{Directory: query.Get("directory")})
	if err != nil {
		a.writeAgentError(w, err)
		return
	}
	writeRawJSON(w, http.StatusOK, result)
}

// handleCreateSession 校验绝对目录后转发 session.create。
func (a *API) handleCreateSession(w http.ResponseWriter, r *http.Request) {
	var req createSessionRequest
	if err := decodeRequest(w, r, &req); err != nil {
		writeAPIError(w, http.StatusBadRequest, "invalid_request", "invalid request body")
		return
	}
	if req.NodeID == "" {
		writeAPIError(w, http.StatusBadRequest, "invalid_request", "node_id is required")
		return
	}
	if req.Directory == "" {
		writeAPIError(w, http.StatusBadRequest, "invalid_request", "directory is required")
		return
	}
	if !filepath.IsAbs(req.Directory) {
		writeAPIError(w, http.StatusBadRequest, "invalid_request", "directory must be absolute")
		return
	}

	result, err := a.callAgent(r.Context(), req.NodeID, "session.create",
		sessionCreateParams{Directory: req.Directory})
	if err != nil {
		a.writeAgentError(w, err)
		return
	}
	writeRawJSON(w, http.StatusOK, result)
}

// handleRenameSession 转发会话改名；空白标题在控制面先拒绝，避免把无效
// 值送给节点。
func (a *API) handleRenameSession(w http.ResponseWriter, r *http.Request) {
	var req sessionRenameRequest
	if err := decodeRequest(w, r, &req); err != nil {
		writeAPIError(w, http.StatusBadRequest, "invalid_request", "invalid request body")
		return
	}
	if strings.TrimSpace(req.Title) == "" {
		writeAPIError(w, http.StatusBadRequest, "invalid_request", "title is required")
		return
	}

	result, err := a.callAgent(r.Context(), r.PathValue("nodeID"), "session.rename",
		sessionRenameParams{
			SessionID: r.PathValue("sessionID"),
			Title:     strings.TrimSpace(req.Title),
		})
	if err != nil {
		a.writeAgentError(w, err)
		return
	}
	writeRawJSON(w, http.StatusOK, result)
}

// handleDeleteSession 删除会话；控制面不持有会话副本，因此只需清空选择。
func (a *API) handleDeleteSession(w http.ResponseWriter, r *http.Request) {
	result, err := a.callAgent(r.Context(), r.PathValue("nodeID"), "session.delete",
		sessionDeleteParams{SessionID: r.PathValue("sessionID")})
	if err != nil {
		a.writeAgentError(w, err)
		return
	}
	writeRawJSON(w, http.StatusOK, result)
}

// handleSessionMessages 透传节点的 session.messages 结果。
func (a *API) handleSessionMessages(w http.ResponseWriter, r *http.Request) {
	result, err := a.callAgent(r.Context(), r.PathValue("nodeID"), "session.messages",
		sessionMessagesParams{SessionID: r.PathValue("sessionID")})
	if err != nil {
		a.writeAgentError(w, err)
		return
	}
	writeRawJSON(w, http.StatusOK, result)
}

// handleSessionPrompt 校验提示词与幂等键后转发 session.prompt。
func (a *API) handleSessionPrompt(w http.ResponseWriter, r *http.Request) {
	var req promptRequest
	if err := decodeRequest(w, r, &req); err != nil {
		writeAPIError(w, http.StatusBadRequest, "invalid_request", "invalid request body")
		return
	}
	if strings.TrimSpace(req.Text) == "" {
		// 只有空白的提示词与空提示词等价，一样拒绝；合法文本保持原样转发。
		writeAPIError(w, http.StatusBadRequest, "invalid_request", "text is required")
		return
	}
	if req.IdempotencyKey == "" {
		writeAPIError(w, http.StatusBadRequest, "invalid_request", "idempotency_key is required")
		return
	}

	result, err := a.callAgent(r.Context(), r.PathValue("nodeID"), "session.prompt",
		sessionPromptParams{
			SessionID:      r.PathValue("sessionID"),
			Text:           req.Text,
			IdempotencyKey: req.IdempotencyKey,
		})
	if err != nil {
		a.writeAgentError(w, err)
		return
	}
	writeRawJSON(w, http.StatusOK, result)
}

// handlePermissionReply 转发权限回复。
func (a *API) handlePermissionReply(w http.ResponseWriter, r *http.Request) {
	var req permissionReplyRequest
	if err := decodeRequest(w, r, &req); err != nil {
		writeAPIError(w, http.StatusBadRequest, "invalid_request", "invalid request body")
		return
	}

	if strings.TrimSpace(req.Decision) == "" {
		writeAPIError(w, http.StatusBadRequest, "invalid_request", "decision is required")
		return
	}

	result, err := a.callAgent(r.Context(), r.PathValue("nodeID"), "permission.reply",
		permissionReplyParams{
			SessionID:    r.PathValue("sessionID"),
			PermissionID: r.PathValue("permissionID"),
			Decision:     req.Decision,
			Message:      req.Message,
		})
	if err != nil {
		a.writeAgentError(w, err)
		return
	}
	writeRawJSON(w, http.StatusOK, result)
}

// handleSessionDiff 转发 turn diff；directory 只用于 agent 侧边界校验。
func (a *API) handleSessionDiff(w http.ResponseWriter, r *http.Request) {
	query := r.URL.Query()
	directory := query.Get("directory")
	if directory == "" {
		writeAPIError(w, http.StatusBadRequest, "invalid_request", "directory is required")
		return
	}
	if !filepath.IsAbs(directory) {
		writeAPIError(w, http.StatusBadRequest, "invalid_request", "directory must be absolute")
		return
	}
	contextLines := 0
	if raw := query.Get("context"); raw != "" {
		parsed, err := strconv.Atoi(raw)
		if err != nil || parsed < 0 {
			writeAPIError(w, http.StatusBadRequest, "invalid_request", "context must be a non-negative integer")
			return
		}
		contextLines = parsed
	}
	nodeID := r.PathValue("nodeID")
	if !a.requireNode(w, r, nodeID) {
		return
	}

	result, err := a.callAgent(r.Context(), nodeID, "session.diff", sessionDiffParams{
		SessionID: r.PathValue("sessionID"),
		Directory: directory,
		From:      query.Get("from"),
		To:        query.Get("to"),
		Context:   contextLines,
	})
	if err != nil {
		a.writeAgentError(w, err)
		return
	}
	writeRawJSON(w, http.StatusOK, result)
}

// handleSessionInterrupt 停止当前运行；resume 固定为 false，I1-B 不排队
// 新的 steering 消息。
func (a *API) handleSessionInterrupt(w http.ResponseWriter, r *http.Request) {
	nodeID := r.PathValue("nodeID")
	if !a.requireNode(w, r, nodeID) {
		return
	}
	result, err := a.callAgent(r.Context(), nodeID, "session.interrupt",
		sessionInterruptParams{SessionID: r.PathValue("sessionID")})
	if err != nil {
		a.writeAgentError(w, err)
		return
	}
	writeRawJSON(w, http.StatusOK, result)
}

// handlePTYCreate 创建一个绑定到 session 和目录的持久终端。
func (a *API) handlePTYCreate(w http.ResponseWriter, r *http.Request) {
	var req ptyCreateRequest
	if err := decodeRequest(w, r, &req); err != nil {
		writeAPIError(w, http.StatusBadRequest, "invalid_request", "invalid request body")
		return
	}
	if req.Directory == "" {
		writeAPIError(w, http.StatusBadRequest, "invalid_request", "directory is required")
		return
	}
	if !filepath.IsAbs(req.Directory) {
		writeAPIError(w, http.StatusBadRequest, "invalid_request", "directory must be absolute")
		return
	}
	if req.Cols == 0 {
		req.Cols = 80
	}
	if req.Rows == 0 {
		req.Rows = 24
	}
	if req.Cols < 1 || req.Cols > 1000 || req.Rows < 1 || req.Rows > 1000 {
		writeAPIError(w, http.StatusBadRequest, "invalid_request", "invalid terminal size")
		return
	}
	if req.Args == nil {
		req.Args = []string{}
	}
	nodeID := r.PathValue("nodeID")
	if !a.requireNode(w, r, nodeID) {
		return
	}

	result, err := a.callAgent(r.Context(), nodeID, "pty.create", ptyCreateParams{
		SessionID: r.PathValue("sessionID"),
		Directory: req.Directory,
		Cols:      req.Cols,
		Rows:      req.Rows,
		Command:   req.Command,
		Args:      req.Args,
	})
	if err != nil {
		a.writeAgentError(w, err)
		return
	}
	writeRawJSON(w, http.StatusOK, result)
}

// handlePTYClose 关闭一个终端；浏览器 WebSocket 断开时也会走同一条路径。
func (a *API) handlePTYClose(w http.ResponseWriter, r *http.Request) {
	ptyID := r.PathValue("ptyID")
	if ptyID == "" {
		writeAPIError(w, http.StatusBadRequest, "invalid_request", "pty_id is required")
		return
	}
	nodeID := r.PathValue("nodeID")
	if !a.requireNode(w, r, nodeID) {
		return
	}
	result, err := a.callAgent(r.Context(), nodeID, "pty.close",
		ptyCloseParams{PTYID: ptyID})
	if err != nil {
		a.writeAgentError(w, err)
		return
	}
	writeRawJSON(w, http.StatusOK, result)
}
