package api

import (
	"net/http"
	"path/filepath"
)

// createSessionRequest 是 POST /api/sessions 的请求体。
type createSessionRequest struct {
	NodeID    string `json:"node_id"`
	Directory string `json:"directory"`
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

// handleListSessions 透传节点的 session.list 结果。
func (a *API) handleListSessions(w http.ResponseWriter, r *http.Request) {
	query := r.URL.Query()
	result, err := a.callAgent(r.Context(), query.Get("node"), "session.list",
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
	if err := decodeRequest(r, &req); err != nil {
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
	if err := decodeRequest(r, &req); err != nil {
		writeAPIError(w, http.StatusBadRequest, "invalid_request", "invalid request body")
		return
	}
	if req.Text == "" {
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
	if err := decodeRequest(r, &req); err != nil {
		writeAPIError(w, http.StatusBadRequest, "invalid_request", "invalid request body")
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
