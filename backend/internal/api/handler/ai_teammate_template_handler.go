package handler

import (
	"net/http"

	"github.com/gin-gonic/gin"

	"github.com/insmtx/Leros/backend/internal/api/contract"
	"github.com/insmtx/Leros/backend/internal/api/dto"
)

// AITeammateTemplateHandler handles preset AI teammate template APIs.
type AITeammateTemplateHandler struct {
	service contract.AITeammateTemplateService
}

// NewAITeammateTemplateHandler creates an AI teammate template handler.
func NewAITeammateTemplateHandler(service contract.AITeammateTemplateService) *AITeammateTemplateHandler {
	return &AITeammateTemplateHandler{service: service}
}

// RegisterAITeammateTemplateRoutes registers AI teammate template routes.
func RegisterAITeammateTemplateRoutes(r gin.IRouter, service contract.AITeammateTemplateService) {
	h := NewAITeammateTemplateHandler(service)
	h.RegisterRoutes(r)
}

// RegisterRoutes registers AI teammate template routes.
func (h *AITeammateTemplateHandler) RegisterRoutes(r gin.IRouter) {
	r.POST("/ListAITeammateTemplates", h.ListAITeammateTemplates)
	r.POST("/GetAITeammateTemplate", h.GetAITeammateTemplate)
	r.POST("/IncrementAITeammateTemplateUseCount", h.IncrementAITeammateTemplateUseCount)
	r.POST("/IncrementAITeammateTemplateRecommendCount", h.IncrementAITeammateTemplateRecommendCount)
}

// ListAITeammateTemplates lists active preset AI teammate templates.
func (h *AITeammateTemplateHandler) ListAITeammateTemplates(ctx *gin.Context) {
	var req contract.ListAITeammateTemplateRequest
	if err := ctx.ShouldBindJSON(&req); err != nil {
		ctx.JSON(http.StatusBadRequest, dto.Error(dto.CodeInvalidParams, err.Error()))
		return
	}

	result, err := h.service.ListAITeammateTemplates(ctx, &req)
	if err != nil {
		handleAITeammateTemplateError(ctx, err)
		return
	}
	ctx.JSON(http.StatusOK, dto.Success(result))
}

// GetAITeammateTemplate gets a preset AI teammate template.
func (h *AITeammateTemplateHandler) GetAITeammateTemplate(ctx *gin.Context) {
	var req contract.GetAITeammateTemplateRequest
	if err := ctx.ShouldBindJSON(&req); err != nil {
		ctx.JSON(http.StatusBadRequest, dto.Error(dto.CodeInvalidParams, err.Error()))
		return
	}

	result, err := h.service.GetAITeammateTemplate(ctx, &req)
	if err != nil {
		handleAITeammateTemplateError(ctx, err)
		return
	}
	ctx.JSON(http.StatusOK, dto.Success(result))
}

// IncrementAITeammateTemplateUseCount increments a template's successful-use count.
func (h *AITeammateTemplateHandler) IncrementAITeammateTemplateUseCount(ctx *gin.Context) {
	var req contract.IncrementAITeammateTemplateCountRequest
	if err := ctx.ShouldBindJSON(&req); err != nil {
		ctx.JSON(http.StatusBadRequest, dto.Error(dto.CodeInvalidParams, err.Error()))
		return
	}

	result, err := h.service.IncrementAITeammateTemplateUseCount(ctx, &req)
	if err != nil {
		handleAITeammateTemplateError(ctx, err)
		return
	}
	ctx.JSON(http.StatusOK, dto.Success(result))
}

// IncrementAITeammateTemplateRecommendCount increments a template's recommendation count.
func (h *AITeammateTemplateHandler) IncrementAITeammateTemplateRecommendCount(ctx *gin.Context) {
	var req contract.IncrementAITeammateTemplateCountRequest
	if err := ctx.ShouldBindJSON(&req); err != nil {
		ctx.JSON(http.StatusBadRequest, dto.Error(dto.CodeInvalidParams, err.Error()))
		return
	}

	result, err := h.service.IncrementAITeammateTemplateRecommendCount(ctx, &req)
	if err != nil {
		handleAITeammateTemplateError(ctx, err)
		return
	}
	ctx.JSON(http.StatusOK, dto.Success(result))
}

func handleAITeammateTemplateError(ctx *gin.Context, err error) {
	switch err.Error() {
	case "id or code is required":
		ctx.JSON(http.StatusBadRequest, dto.Error(dto.CodeInvalidParams, err.Error()))
	case "ai teammate template not found":
		ctx.JSON(http.StatusNotFound, dto.Error(dto.CodeNotFound, err.Error()))
	default:
		ctx.JSON(http.StatusInternalServerError, dto.Error(dto.CodeInternalError, err.Error()))
	}
}
