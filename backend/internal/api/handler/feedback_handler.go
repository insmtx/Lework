package handler

import (
	"errors"
	"net/http"
	"strings"

	"github.com/gin-gonic/gin"
	"github.com/insmtx/Leros/backend/internal/api/auth"
	"github.com/insmtx/Leros/backend/internal/api/dto"
	"github.com/insmtx/Leros/backend/internal/service"
)

type FeedbackHandler struct {
	service *service.FeedbackService
}

func NewFeedbackHandler(service *service.FeedbackService) *FeedbackHandler {
	return &FeedbackHandler{service: service}
}

func RegisterFeedbackRoutes(r gin.IRouter, svc *service.FeedbackService) {
	if svc == nil {
		return
	}
	h := NewFeedbackHandler(svc)
	r.POST("/SubmitFeedback", h.SubmitFeedback)
}

type submitFeedbackRequest struct {
	Type          string   `json:"type"`
	Content       string   `json:"content"`
	AttachmentIDs []string `json:"attachment_ids"`
	Client        struct {
		Platform string `json:"platform"`
		Version  string `json:"version"`
	} `json:"client"`
}

func (h *FeedbackHandler) SubmitFeedback(ctx *gin.Context) {
	var req submitFeedbackRequest
	if err := ctx.ShouldBindJSON(&req); err != nil {
		ctx.JSON(http.StatusBadRequest, dto.Error(dto.CodeInvalidParams, err.Error()))
		return
	}

	caller, _ := auth.FromGinContext(ctx)
	if caller == nil || caller.OrgID == 0 || caller.Uin == 0 {
		ctx.JSON(http.StatusUnauthorized, dto.Error(dto.CodeInvalidParams, "not authenticated"))
		return
	}

	result, err := h.service.SubmitFeedback(ctx, &service.SubmitFeedbackRequest{
		OrgID:         caller.OrgID,
		Uin:           caller.Uin,
		Type:          strings.TrimSpace(req.Type),
		Content:       strings.TrimSpace(req.Content),
		AttachmentIDs: req.AttachmentIDs,
		Client: service.SubmitFeedbackClientInfo{
			Platform: strings.TrimSpace(req.Client.Platform),
			Version:  strings.TrimSpace(req.Client.Version),
		},
	})
	if err != nil {
		switch {
		case errors.Is(err, service.ErrFeedbackNotConfigured):
			ctx.JSON(http.StatusServiceUnavailable, dto.Error(dto.CodeInternalError, "feedback service unavailable"))
		case errors.Is(err, service.ErrFeedbackNotAuthenticated):
			ctx.JSON(http.StatusUnauthorized, dto.Error(dto.CodeInvalidParams, err.Error()))
		case errors.Is(err, service.ErrFeedbackTypeRequired),
			errors.Is(err, service.ErrFeedbackContentRequired),
			errors.Is(err, service.ErrFeedbackInvalidType),
			errors.Is(err, service.ErrFeedbackTooManyFiles),
			errors.Is(err, service.ErrFeedbackAttachmentMissing):
			ctx.JSON(http.StatusBadRequest, dto.Error(dto.CodeInvalidParams, err.Error()))
		case errors.Is(err, service.ErrFeedbackContentTooLong):
			ctx.JSON(http.StatusBadRequest, dto.Error(dto.CodeInvalidParams, "feedback content exceeds 300 characters"))
		case errors.Is(err, service.ErrFeedbackFeishuPermission):
			ctx.JSON(http.StatusBadGateway, dto.Error(dto.CodeInternalError, err.Error()))
		default:
			ctx.JSON(http.StatusBadGateway, dto.Error(dto.CodeInternalError, "submit feedback failed"))
		}
		return
	}

	ctx.JSON(http.StatusOK, dto.Success(result))
}
