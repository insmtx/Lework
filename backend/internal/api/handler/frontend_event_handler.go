package handler

import (
	"fmt"
	"net/http"

	"github.com/gin-gonic/gin"
	"github.com/insmtx/Leros/backend/internal/api/dto"
	"github.com/ygpkg/yg-go/logs"
)

type FrontendEventHandler struct{}

func NewFrontendEventHandler() *FrontendEventHandler {
	return &FrontendEventHandler{}
}

func RegisterFrontendEventRoutes(r gin.IRouter) {
	h := NewFrontendEventHandler()
	r.POST("/CollectFrontendEvent", h.CollectFrontendEvent)
}

type frontendEventRequest struct {
	Fingerprint string           `json:"fingerprint"`
	Events      []*frontendEvent `json:"events"`
}

type frontendEvent struct {
	EventType    string `json:"event_type"`
	Timestamp    int64  `json:"timestamp"`
	PageURL      string `json:"page_url,omitempty"`
	PageTitle    string `json:"page_title,omitempty"`
	EventName    string `json:"event_name,omitempty"`
	DurationMs   int64  `json:"duration_ms,omitempty"`
	Extra        any    `json:"extra,omitempty"`
	ErrorMessage string `json:"error_message,omitempty"`
	ErrorStack   string `json:"error_stack,omitempty"`
	Component    string `json:"component,omitempty"`
}

func (h *FrontendEventHandler) CollectFrontendEvent(ctx *gin.Context) {
	var req frontendEventRequest
	if err := ctx.ShouldBindJSON(&req); err != nil {
		ctx.JSON(http.StatusBadRequest, dto.Error(dto.CodeInvalidParams, err.Error()))
		return
	}

	if len(req.Events) == 0 {
		ctx.JSON(http.StatusBadRequest, dto.Error(dto.CodeInvalidParams, "events is required"))
		return
	}

	for _, e := range req.Events {
		if e.EventType == "" {
			continue
		}

		kvs := []any{
			"frontend_event_type", e.EventType,
			"frontend_event_timestamp", e.Timestamp,
			"frontend_event_fingerprint", req.Fingerprint,
			"frontend_event_page_url", e.PageURL,
			"frontend_event_page_title", e.PageTitle,
		}

		if e.EventName != "" {
			kvs = append(kvs, "frontend_event_event_name", e.EventName)
		}
		if e.DurationMs > 0 {
			kvs = append(kvs, "frontend_event_duration_ms", e.DurationMs)
		}
		if e.Extra != nil {
			kvs = append(kvs, "frontend_event_extra", fmt.Sprintf("%+v", e.Extra))
		}
		if e.ErrorMessage != "" {
			kvs = append(kvs, "frontend_event_error_message", e.ErrorMessage)
		}
		if e.ErrorStack != "" {
			kvs = append(kvs, "frontend_event_error_stack", e.ErrorStack)
		}
		if e.Component != "" {
			kvs = append(kvs, "frontend_event_component", e.Component)
		}

		logs.InfoContextw(ctx, "frontend_event", kvs...)
	}

	ctx.JSON(http.StatusOK, dto.Success(nil))
}
