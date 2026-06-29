package handler

import (
	"encoding/json"
	"net/http"

	"github.com/gin-gonic/gin"
	"github.com/ygpkg/yg-go/logs"

	"github.com/insmtx/Leros/backend/agent"
	"github.com/insmtx/Leros/backend/agent/runtime/events"
	"github.com/insmtx/Leros/backend/internal/api/auth"
	"github.com/insmtx/Leros/backend/internal/api/contract"
	"github.com/insmtx/Leros/backend/internal/api/dto"
)

type SessionHandler struct {
	service contract.SessionService
}

func NewSessionHandler(service contract.SessionService) *SessionHandler {
	return &SessionHandler{
		service: service,
	}
}

func (h *SessionHandler) RegisterRoutes(r gin.IRouter) {
	r.POST("/CreateSession", h.CreateSession)
	r.POST("/sessions/:session_id/approvals", h.SubmitApproval)
	r.POST("/GetSession", h.GetSession)
	r.POST("/UpdateSession", h.UpdateSession)
	r.POST("/DeleteSession", h.DeleteSession)
	r.POST("/ListSessions", h.ListSessions)
	r.POST("/SessionEvents", h.SessionEvents)
	r.POST("/AddMessage", h.AddMessage)
	r.POST("/GetSessionMessages", h.GetSessionMessages)
	r.POST("/DeleteMessage", h.DeleteMessage)
	r.POST("/ClearSessionMessages", h.ClearSessionMessages)
	r.POST("/CancelSessionRun", h.CancelSessionRun)
}

func RegisterSessionRoutes(r gin.IRouter, service contract.SessionService) {
	h := NewSessionHandler(service)
	h.RegisterRoutes(r)
}

// @Summary 创建会话
// @Description 创建一个新的会话实例
// @Tags Session
// @Accept json
// @Produce json
// @Param body body contract.CreateSessionRequest true "创建会话请求"
// @Success 200 {object} dto.BaseResponse "成功响应"
// @Failure 400 {object} dto.ErrorResponse "请求参数错误"
// @Failure 401 {object} dto.ErrorResponse "未认证"
// @Failure 500 {object} dto.ErrorResponse "内部服务器错误"
// @Router /CreateSession [post]
func (h *SessionHandler) CreateSession(ctx *gin.Context) {
	var req contract.CreateSessionRequest
	if err := ctx.ShouldBindJSON(&req); err != nil {
		ctx.JSON(http.StatusBadRequest, dto.Error(dto.CodeInvalidParams, err.Error()))
		return
	}

	result, err := h.service.CreateSession(ctx, &req)
	if err != nil {
		handleSessionServiceError(ctx, err)
		return
	}

	ctx.JSON(http.StatusOK, dto.Success(result))
}

type GetSessionRequest struct {
	SessionID string `json:"session_id" binding:"required"`
}

// @Summary 获取会话详情
// @Description 根据SessionID获取会话详情
// @Tags Session
// @Accept json
// @Produce json
// @Param body body GetSessionRequest true "获取会话请求"
// @Success 200 {object} dto.BaseResponse "成功响应"
// @Failure 400 {object} dto.ErrorResponse "请求参数错误"
// @Failure 401 {object} dto.ErrorResponse "未认证"
// @Failure 404 {object} dto.ErrorResponse "资源不存在"
// @Failure 500 {object} dto.ErrorResponse "内部服务器错误"
// @Router /GetSession [post]
func (h *SessionHandler) GetSession(ctx *gin.Context) {
	var req GetSessionRequest
	if err := ctx.ShouldBindJSON(&req); err != nil {
		ctx.JSON(http.StatusBadRequest, dto.Error(dto.CodeInvalidParams, err.Error()))
		return
	}

	result, err := h.service.GetSession(ctx, req.SessionID)
	if err != nil {
		handleSessionServiceError(ctx, err)
		return
	}

	ctx.JSON(http.StatusOK, dto.Success(result))
}

type UpdateSessionRequest struct {
	SessionID string `json:"session_id" binding:"required"`
	contract.UpdateSessionRequest
}

// @Summary 更新会话
// @Description 更新会话基本信息
// @Tags Session
// @Accept json
// @Produce json
// @Param body body UpdateSessionRequest true "更新会话请求"
// @Success 200 {object} dto.BaseResponse "成功响应"
// @Failure 400 {object} dto.ErrorResponse "请求参数错误"
// @Failure 401 {object} dto.ErrorResponse "未认证"
// @Failure 404 {object} dto.ErrorResponse "资源不存在"
// @Failure 500 {object} dto.ErrorResponse "内部服务器错误"
// @Router /UpdateSession [post]
func (h *SessionHandler) UpdateSession(ctx *gin.Context) {
	var req UpdateSessionRequest
	if err := ctx.ShouldBindJSON(&req); err != nil {
		ctx.JSON(http.StatusBadRequest, dto.Error(dto.CodeInvalidParams, err.Error()))
		return
	}

	result, err := h.service.UpdateSession(ctx, req.SessionID, &req.UpdateSessionRequest)
	if err != nil {
		handleSessionServiceError(ctx, err)
		return
	}

	ctx.JSON(http.StatusOK, dto.Success(result))
}

type DeleteSessionRequest struct {
	SessionID string `json:"session_id" binding:"required"`
}

// @Summary 删除会话
// @Description 根据ID删除会话
// @Tags Session
// @Accept json
// @Produce json
// @Param body body DeleteSessionRequest true "删除会话请求"
// @Success 200 {object} dto.BaseResponse "成功响应"
// @Failure 400 {object} dto.ErrorResponse "请求参数错误"
// @Failure 401 {object} dto.ErrorResponse "未认证"
// @Failure 404 {object} dto.ErrorResponse "资源不存在"
// @Failure 500 {object} dto.ErrorResponse "内部服务器错误"
// @Router /DeleteSession [post]
func (h *SessionHandler) DeleteSession(ctx *gin.Context) {
	var req DeleteSessionRequest
	if err := ctx.ShouldBindJSON(&req); err != nil {
		ctx.JSON(http.StatusBadRequest, dto.Error(dto.CodeInvalidParams, err.Error()))
		return
	}

	err := h.service.DeleteSession(ctx, req.SessionID)
	if err != nil {
		handleSessionServiceError(ctx, err)
		return
	}

	ctx.JSON(http.StatusOK, dto.Success(nil))
}

// @Summary 查询会话列表
// @Description 分页查询会话列表
// @Tags Session
// @Accept json
// @Produce json
// @Param body body contract.ListSessionsRequest true "查询列表请求"
// @Success 200 {object} dto.BaseResponse "成功响应"
// @Failure 400 {object} dto.ErrorResponse "请求参数错误"
// @Failure 401 {object} dto.ErrorResponse "未认证"
// @Failure 500 {object} dto.ErrorResponse "内部服务器错误"
// @Router /ListSessions [post]
func (h *SessionHandler) ListSessions(ctx *gin.Context) {
	var req contract.ListSessionsRequest
	if err := ctx.ShouldBindJSON(&req); err != nil {
		ctx.JSON(http.StatusBadRequest, dto.Error(dto.CodeInvalidParams, err.Error()))
		return
	}

	req.Pagination.Fill()

	result, err := h.service.ListSessions(ctx, &req)
	if err != nil {
		handleSessionServiceError(ctx, err)
		return
	}

	ctx.JSON(http.StatusOK, dto.Success(result))
}

type SessionEventsRequest struct {
	SessionID string `json:"session_id" binding:"required"`
	Replay    bool   `json:"replay,omitempty"`
}

// @Summary 订阅会话事件流
// @Description 通过SSE订阅会话的事件流
// @Tags Session
// @Accept json
// @Produce text/event-stream
// @Param body body SessionEventsRequest true "订阅事件请求"
// @Success 200 {string} string "SSE事件流"
// @Failure 400 {object} dto.ErrorResponse "请求参数错误"
// @Failure 401 {object} dto.ErrorResponse "未认证"
// @Failure 500 {object} dto.ErrorResponse "内部服务器错误"
// @Router /SessionEvents [post]
func (h *SessionHandler) SessionEvents(ctx *gin.Context) {
	var req SessionEventsRequest
	if err := ctx.ShouldBindJSON(&req); err != nil {
		ctx.JSON(http.StatusBadRequest, dto.Error(dto.CodeInvalidParams, err.Error()))
		return
	}

	ctx.Header("Content-Type", "text/event-stream")
	ctx.Header("Cache-Control", "no-cache")
	ctx.Header("Connection", "keep-alive")
	ctx.Header("Access-Control-Allow-Origin", "*")

	eventChan := make(chan *agent.Event, 16)
	sink := events.ChannelSink{C: eventChan}

	go func() {
		defer close(eventChan)
		err := h.service.StreamSessionEvents(ctx, req.SessionID, req.Replay, sink)
		if err != nil {
			logs.ErrorContextf(ctx, "failed to stream session events for session %s: %v", req.SessionID, err)
			ctx.SSEvent("error", dto.Error(dto.CodeInternalError, err.Error()))
		}
		logs.DebugContextf(ctx, "session event stream goroutine exiting for session %s", req.SessionID)
	}()

	for {
		select {
		case event, ok := <-eventChan:
			if !ok {
				logs.WarnContextf(ctx, "event channel closed for session %s", req.SessionID)
				return
			}
			data := event.Content
			if len(event.Payload) > 0 {
				data = string(event.Payload)
			}
			ctx.SSEvent(string(event.Type), data)
			ctx.Writer.Flush()
		case <-ctx.Writer.CloseNotify():
			logs.InfoContextf(ctx, "client closed connection for session %s event stream", req.SessionID)
			return
		case <-ctx.Done():
			logs.InfoContextf(ctx, "client closed connection for session %s event stream (Done)", req.SessionID)
			return
		}
	}
}

type AddMessageRequest struct {
	SessionID string `json:"session_id" binding:"required"`
	contract.AddMessageRequest
}

// @Summary 添加会话消息
// @Description 向指定会话添加一条消息
// @Tags Session
// @Accept json
// @Produce json
// @Param body body AddMessageRequest true "添加消息请求"
// @Success 200 {object} dto.BaseResponse "成功响应"
// @Failure 400 {object} dto.ErrorResponse "请求参数错误"
// @Failure 401 {object} dto.ErrorResponse "未认证"
// @Failure 404 {object} dto.ErrorResponse "资源不存在"
// @Failure 500 {object} dto.ErrorResponse "内部服务器错误"
// @Router /AddMessage [post]
func (h *SessionHandler) AddMessage(ctx *gin.Context) {
	var req AddMessageRequest
	if err := ctx.ShouldBindJSON(&req); err != nil {
		ctx.JSON(http.StatusBadRequest, dto.Error(dto.CodeInvalidParams, err.Error()))
		return
	}

	result, err := h.service.AddMessage(ctx, req.SessionID, &req.AddMessageRequest)
	if err != nil {
		handleSessionServiceError(ctx, err)
		return
	}

	ctx.JSON(http.StatusOK, dto.Success(result))
}

type GetSessionMessagesRequest struct {
	SessionID string `json:"session_id" binding:"required"`
	Page      int    `json:"page,omitempty"`
	PerPage   int    `json:"per_page,omitempty"`
}

// @Summary 获取会话消息列表
// @Description 分页获取指定会话的消息列表
// @Tags Session
// @Accept json
// @Produce json
// @Param body body GetSessionMessagesRequest true "获取消息列表请求"
// @Success 200 {object} dto.BaseResponse "成功响应"
// @Failure 400 {object} dto.ErrorResponse "请求参数错误"
// @Failure 401 {object} dto.ErrorResponse "未认证"
// @Failure 404 {object} dto.ErrorResponse "资源不存在"
// @Failure 500 {object} dto.ErrorResponse "内部服务器错误"
// @Router /GetSessionMessages [post]
func (h *SessionHandler) GetSessionMessages(ctx *gin.Context) {
	var req GetSessionMessagesRequest
	if err := ctx.ShouldBindJSON(&req); err != nil {
		ctx.JSON(http.StatusBadRequest, dto.Error(dto.CodeInvalidParams, err.Error()))
		return
	}

	page := req.Page
	if page == 0 {
		page = 1
	}
	perPage := req.PerPage
	if perPage == 0 {
		perPage = 20
	}

	result, err := h.service.GetSessionMessages(ctx, req.SessionID, page, perPage)
	if err != nil {
		handleSessionServiceError(ctx, err)
		return
	}

	ctx.JSON(http.StatusOK, dto.Success(result))
}

type DeleteMessageRequest struct {
	MessageID uint `json:"message_id" binding:"required"`
}

// @Summary 删除会话消息
// @Description 根据ID删除会话消息
// @Tags Session
// @Accept json
// @Produce json
// @Param body body DeleteMessageRequest true "删除消息请求"
// @Success 200 {object} dto.BaseResponse "成功响应"
// @Failure 400 {object} dto.ErrorResponse "请求参数错误"
// @Failure 401 {object} dto.ErrorResponse "未认证"
// @Failure 404 {object} dto.ErrorResponse "资源不存在"
// @Failure 500 {object} dto.ErrorResponse "内部服务器错误"
// @Router /DeleteMessage [post]
func (h *SessionHandler) DeleteMessage(ctx *gin.Context) {
	var req DeleteMessageRequest
	if err := ctx.ShouldBindJSON(&req); err != nil {
		ctx.JSON(http.StatusBadRequest, dto.Error(dto.CodeInvalidParams, err.Error()))
		return
	}

	err := h.service.DeleteMessage(ctx, req.MessageID)
	if err != nil {
		handleSessionServiceError(ctx, err)
		return
	}

	ctx.JSON(http.StatusOK, dto.Success(nil))
}

type ClearSessionMessagesRequest struct {
	SessionID string `json:"session_id" binding:"required"`
}

// @Summary 清空会话消息
// @Description 清空指定会话的所有消息
// @Tags Session
// @Accept json
// @Produce json
// @Param body body ClearSessionMessagesRequest true "清空消息请求"
// @Success 200 {object} dto.BaseResponse "成功响应"
// @Failure 400 {object} dto.ErrorResponse "请求参数错误"
// @Failure 401 {object} dto.ErrorResponse "未认证"
// @Failure 404 {object} dto.ErrorResponse "资源不存在"
// @Failure 500 {object} dto.ErrorResponse "内部服务器错误"
// @Router /ClearSessionMessages [post]
func (h *SessionHandler) ClearSessionMessages(ctx *gin.Context) {
	var req ClearSessionMessagesRequest
	if err := ctx.ShouldBindJSON(&req); err != nil {
		ctx.JSON(http.StatusBadRequest, dto.Error(dto.CodeInvalidParams, err.Error()))
		return
	}

	err := h.service.ClearSessionMessages(ctx, req.SessionID)
	if err != nil {
		handleSessionServiceError(ctx, err)
		return
	}

	ctx.JSON(http.StatusOK, dto.Success(nil))
}

// SubmitApproval 处理前端提交的审批决策或问题答案，委托给 service 层通过 NATS 转发。
// POST /v1/sessions/:session_id/approvals
//
// 支持的交互类型：
//   - "approval.decide": 审批决策，payload 为 {request_id, action, reason}
//   - "question.answer": 问题答案，payload 为 {request_id, answers}
func (h *SessionHandler) SubmitApproval(ctx *gin.Context) {
	sessionID := ctx.Param("session_id")
	var interaction struct {
		Type    string          `json:"type"`
		Payload json.RawMessage `json:"payload"`
	}
	if err := ctx.ShouldBindJSON(&interaction); err != nil {
		ctx.JSON(http.StatusBadRequest, dto.Error(dto.CodeInvalidParams, err.Error()))
		return
	}

	switch interaction.Type {
	case "approval.decide":
		var payload struct {
			RequestID string `json:"request_id"`
			Action    string `json:"action"`
			Reason    string `json:"reason,omitempty"`
		}
		if err := json.Unmarshal(interaction.Payload, &payload); err != nil {
			ctx.JSON(http.StatusBadRequest, dto.Error(dto.CodeInvalidParams, err.Error()))
			return
		}

		caller, _ := auth.FromGinContext(ctx)
		if caller == nil || caller.OrgID == 0 {
			ctx.JSON(http.StatusUnauthorized, dto.Error(dto.CodeInvalidParams, "not authenticated"))
			return
		}

		if err := h.service.SubmitApproval(ctx, &contract.SubmitApprovalRequest{
			OrgID:     caller.OrgID,
			SessionID: sessionID,
			RequestID: payload.RequestID,
			Action:    payload.Action,
			Reason:    payload.Reason,
		}); err != nil {
			logs.WarnContextf(ctx, "submit approval failed: %v", err)
			ctx.JSON(http.StatusInternalServerError, dto.Error(dto.CodeInternalError, err.Error()))
			return
		}

		ctx.JSON(http.StatusOK, dto.Success(map[string]string{
			"request_id": payload.RequestID,
			"action":     payload.Action,
		}))

	case "question.answer":
		var payload struct {
			RequestID string     `json:"request_id"`
			Answers   [][]string `json:"answers"`
		}
		if err := json.Unmarshal(interaction.Payload, &payload); err != nil {
			ctx.JSON(http.StatusBadRequest, dto.Error(dto.CodeInvalidParams, err.Error()))
			return
		}

		caller, _ := auth.FromGinContext(ctx)
		if caller == nil || caller.OrgID == 0 {
			ctx.JSON(http.StatusUnauthorized, dto.Error(dto.CodeInvalidParams, "not authenticated"))
			return
		}

		if err := h.service.SubmitQuestionAnswer(ctx, &contract.SubmitQuestionAnswerRequest{
			OrgID:     caller.OrgID,
			SessionID: sessionID,
			RequestID: payload.RequestID,
			Answers:   payload.Answers,
		}); err != nil {
			logs.WarnContextf(ctx, "submit question answer failed: %v", err)
			ctx.JSON(http.StatusInternalServerError, dto.Error(dto.CodeInternalError, err.Error()))
			return
		}

		ctx.JSON(http.StatusOK, dto.Success(map[string]any{
			"request_id": payload.RequestID,
			"status":     "answered",
		}))

	default:
		ctx.JSON(http.StatusBadRequest, dto.Error(dto.CodeInvalidParams, "unknown interaction type: "+interaction.Type))
	}
}

func handleSessionServiceError(ctx *gin.Context, err error) {
	if err.Error() == "user not authenticated or org not set" {
		ctx.JSON(http.StatusUnauthorized, dto.Error(dto.CodeInternalError, err.Error()))
		return
	}
	if err.Error() == "permission denied" {
		ctx.JSON(http.StatusForbidden, dto.Error(dto.CodeInternalError, err.Error()))
		return
	}
	if err.Error() == "session not found" || err.Error() == "message not found" {
		ctx.JSON(http.StatusNotFound, dto.Error(dto.CodeNotFound, err.Error()))
		return
	}
	ctx.JSON(http.StatusInternalServerError, dto.Error(dto.CodeInternalError, err.Error()))
}

// CancelSessionRun cancels an active agent run for the given session.
// POST /CancelSessionRun
func (h *SessionHandler) CancelSessionRun(ctx *gin.Context) {
	var req contract.CancelSessionRunRequest
	if err := ctx.ShouldBindJSON(&req); err != nil {
		ctx.JSON(http.StatusBadRequest, dto.Error(dto.CodeInvalidParams, err.Error()))
		return
	}

	result, err := h.service.CancelSessionRun(ctx, req.SessionID, &req)
	if err != nil {
		handleSessionServiceError(ctx, err)
		return
	}

	ctx.JSON(http.StatusOK, dto.Success(result))
}
