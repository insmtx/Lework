package handler

import (
	"io"
	"net/http"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/ygpkg/yg-go/logs"

	"github.com/insmtx/Leros/backend/internal/api/auth"
	"github.com/insmtx/Leros/backend/internal/api/contract"
	"github.com/insmtx/Leros/backend/internal/api/dto"
	"github.com/insmtx/Leros/backend/pkg/messaging"
	"github.com/insmtx/Leros/backend/types"
)

// globalEventsHeartbeatInterval 控制全局 SSE 连接的 keep-alive 心跳间隔。
const globalEventsHeartbeatInterval = 15 * time.Second

// GlobalEventsHandler 提供 project 级全局 SSE 通知端点。
type GlobalEventsHandler struct {
	service contract.SessionService
}

// NewGlobalEventsHandler 创建 GlobalEventsHandler 实例。
func NewGlobalEventsHandler(service contract.SessionService) *GlobalEventsHandler {
	return &GlobalEventsHandler{service: service}
}

// GlobalEventsRequest 是 GlobalEvents 端点的请求体。
// 所有身份信息均从 caller 派生，请求体仅含可选的 replay 控制位。
type GlobalEventsRequest struct {
	ReplaySinceSeq uint64 `json:"replay_since_seq,omitempty"`
}

// GlobalEvents 建立 project 级持久 SSE 连接，向调用方推送其所属所有 project 的
// 全局通知事件（message.created 等）。
//
// 连接生命周期由客户端控制：客户端断开或 ctx 取消时结束。
// 服务端通过 15s 间隔的 keep-alive 心跳维持连接活跃。
//
// @Summary 全局事件流
// @Description 建立 project 级全局 SSE 长连接，实时推送新消息等通知
// @Tags GlobalEvents
// @Accept json
// @Produce text/event-stream
// @Param request body GlobalEventsRequest false "replay 控制参数"
// @Success 200 {string} string "text/event-stream"
// @Router /v1/GlobalEvents [post]
func (h *GlobalEventsHandler) GlobalEvents(ctx *gin.Context) {
	caller, _ := auth.FromGinContext(ctx)
	if caller == nil || caller.Uin == 0 || caller.OrgID == 0 || caller.Kind != types.CallerKindUser {
		ctx.JSON(http.StatusUnauthorized, dto.Error(dto.CodeInternalError, "user not authenticated"))
		return
	}

	var req GlobalEventsRequest
	if err := ctx.ShouldBindJSON(&req); err != nil && err != io.EOF {
		ctx.JSON(http.StatusBadRequest, dto.Error(dto.CodeInvalidParams, err.Error()))
		return
	}

	ctx.Header("Content-Type", "text/event-stream")
	ctx.Header("Cache-Control", "no-cache")
	ctx.Header("Connection", "keep-alive")
	ctx.Header("Access-Control-Allow-Origin", "*")

	eventChan := make(chan *messaging.GlobalEventPayload, 32)

	go func() {
		defer close(eventChan)
		if err := h.service.StreamGlobalEvents(ctx, caller.OrgID, caller.Uin, req.ReplaySinceSeq, eventChan); err != nil {
			logs.ErrorContextf(ctx, "global events stream error for user %d: %v", caller.Uin, err)
			ctx.SSEvent("error", dto.Error(dto.CodeInternalError, err.Error()))
		}
		logs.DebugContextf(ctx, "global event stream goroutine exiting for user %d", caller.Uin)
	}()

	heartbeat := time.NewTicker(globalEventsHeartbeatInterval)
	defer heartbeat.Stop()

	for {
		select {
		case event, ok := <-eventChan:
			if !ok {
				logs.DebugContextf(ctx, "global event channel closed for user %d", caller.Uin)
				return
			}
			ctx.SSEvent(string(event.Type), event)
			ctx.Writer.Flush()
		case <-heartbeat.C:
			ctx.Writer.WriteString(": keepalive\n\n")
			ctx.Writer.Flush()
		case <-ctx.Writer.CloseNotify():
			logs.InfoContextf(ctx, "client closed global events connection for user %d", caller.Uin)
			return
		case <-ctx.Done():
			logs.InfoContextf(ctx, "global events connection done for user %d", caller.Uin)
			return
		}
	}
}

// RegisterGlobalEventRoutes 注册全局事件路由。
func RegisterGlobalEventRoutes(r gin.IRouter, service contract.SessionService) {
	h := NewGlobalEventsHandler(service)
	r.POST("/GlobalEvents", h.GlobalEvents)
}
