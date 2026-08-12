package handler

import (
	"net/http"

	"github.com/gin-gonic/gin"

	"github.com/insmtx/Leros/backend/internal/api/contract"
	"github.com/insmtx/Leros/backend/internal/api/dto"
)

type LLMModelHandler struct {
	service contract.LLMModelService
}

func NewLLMModelHandler(service contract.LLMModelService) *LLMModelHandler {
	return &LLMModelHandler{service: service}
}

// ================================================================
// Route Registration
// ================================================================

func (h *LLMModelHandler) RegisterRoutes(r gin.IRouter) {
	r.POST("/CreateLLMModel", h.CreateLLMModel)
	r.POST("/GetLLMModel", h.GetLLMModel)
	r.POST("/GetDefaultLLMModel", h.GetDefaultLLMModel)
	r.POST("/UpdateLLMModel", h.UpdateLLMModel)
	r.POST("/SetLLMModelStatus", h.SetLLMModelStatus)
	r.POST("/DeleteLLMModel", h.DeleteLLMModel)
	r.POST("/ListLLMModels", h.ListLLMModels)
	r.POST("/TestLLMModel", h.TestLLMModel)
}

func RegisterLLMModelRoutes(r gin.IRouter, service contract.LLMModelService) {
	h := NewLLMModelHandler(service)
	h.RegisterRoutes(r)
}

// ================================================================
// Handler Methods
// ================================================================

// @Summary 创建LLM模型
// @Description 创建一个新的LLM模型配置
// @Tags LLMModel
// @Accept json
// @Produce json
// @Param body body contract.CreateLLMModelRequest true "创建LLM模型请求"
// @Success 200 {object} dto.Response "成功响应"
// @Failure 400 {object} dto.ErrorResponse "请求参数错误"
// @Failure 401 {object} dto.ErrorResponse "未认证"
// @Failure 500 {object} dto.ErrorResponse "内部服务器错误"
// @Router /CreateLLMModel [post]
func (h *LLMModelHandler) CreateLLMModel(ctx *gin.Context) {
	var req contract.CreateLLMModelRequest
	if err := ctx.ShouldBindJSON(&req); err != nil {
		ctx.JSON(http.StatusBadRequest, dto.Error(dto.CodeInvalidParams, err.Error()))
		return
	}

	result, err := h.service.CreateLLMModel(ctx, &req)
	if err != nil {
		handleLLMModelServiceError(ctx, err)
		return
	}
	ctx.JSON(http.StatusOK, dto.Success(result))
}

type GetLLMModelRequest struct {
	ID   *uint   `json:"id,omitempty"`
	Code *string `json:"code,omitempty"`
}

// @Summary 获取LLM模型详情
// @Description 根据ID或Code获取LLM模型配置详情
// @Tags LLMModel
// @Accept json
// @Produce json
// @Param body body GetLLMModelRequest true "获取LLM模型请求"
// @Success 200 {object} dto.Response "成功响应"
// @Failure 400 {object} dto.ErrorResponse "请求参数错误"
// @Failure 401 {object} dto.ErrorResponse "未认证"
// @Failure 403 {object} dto.ErrorResponse "权限不足"
// @Failure 404 {object} dto.ErrorResponse "资源不存在"
// @Failure 500 {object} dto.ErrorResponse "内部服务器错误"
// @Router /GetLLMModel [post]
func (h *LLMModelHandler) GetLLMModel(ctx *gin.Context) {
	var req GetLLMModelRequest
	if err := ctx.ShouldBindJSON(&req); err != nil {
		ctx.JSON(http.StatusBadRequest, dto.Error(dto.CodeInvalidParams, err.Error()))
		return
	}
	if req.ID == nil && req.Code == nil {
		ctx.JSON(http.StatusBadRequest, dto.Error(dto.CodeInvalidParams, "id or code is required"))
		return
	}

	var id uint
	var code string
	if req.ID != nil {
		id = *req.ID
	}
	if req.Code != nil {
		code = *req.Code
	}

	result, err := h.service.GetLLMModel(ctx, id, code)
	if err != nil {
		handleLLMModelServiceError(ctx, err)
		return
	}
	ctx.JSON(http.StatusOK, dto.Success(result))
}

// @Summary 获取默认LLM模型
// @Description 获取组织的默认LLM模型配置
// @Tags LLMModel
// @Accept json
// @Produce json
// @Success 200 {object} dto.Response "成功响应"
// @Failure 401 {object} dto.ErrorResponse "未认证"
// @Failure 404 {object} dto.ErrorResponse "默认模型不存在"
// @Failure 500 {object} dto.ErrorResponse "内部服务器错误"
// @Router /GetDefaultLLMModel [post]
func (h *LLMModelHandler) GetDefaultLLMModel(ctx *gin.Context) {
	result, err := h.service.GetDefaultLLMModel(ctx)
	if err != nil {
		handleLLMModelServiceError(ctx, err)
		return
	}
	ctx.JSON(http.StatusOK, dto.Success(result))
}

type UpdateLLMModelRequest struct {
	ID uint `json:"id" binding:"required"`
	contract.UpdateLLMModelRequest
}

// @Summary 更新LLM模型
// @Description 更新LLM模型配置信息
// @Tags LLMModel
// @Accept json
// @Produce json
// @Param body body UpdateLLMModelRequest true "更新LLM模型请求"
// @Success 200 {object} dto.Response "成功响应"
// @Failure 400 {object} dto.ErrorResponse "请求参数错误"
// @Failure 401 {object} dto.ErrorResponse "未认证"
// @Failure 403 {object} dto.ErrorResponse "权限不足"
// @Failure 404 {object} dto.ErrorResponse "资源不存在"
// @Failure 500 {object} dto.ErrorResponse "内部服务器错误"
// @Router /UpdateLLMModel [post]
func (h *LLMModelHandler) UpdateLLMModel(ctx *gin.Context) {
	var req UpdateLLMModelRequest
	if err := ctx.ShouldBindJSON(&req); err != nil {
		ctx.JSON(http.StatusBadRequest, dto.Error(dto.CodeInvalidParams, err.Error()))
		return
	}

	result, err := h.service.UpdateLLMModel(ctx, req.ID, &req.UpdateLLMModelRequest)
	if err != nil {
		handleLLMModelServiceError(ctx, err)
		return
	}
	ctx.JSON(http.StatusOK, dto.Success(result))
}

// @Summary 启用或禁用LLM模型
// @Description 根据ID启用或禁用LLM模型配置
// @Tags LLMModel
// @Accept json
// @Produce json
// @Param body body contract.SetLLMModelStatusRequest true "启用/禁用LLM模型请求"
// @Success 200 {object} dto.Response "成功响应"
// @Failure 400 {object} dto.ErrorResponse "请求参数错误"
// @Failure 401 {object} dto.ErrorResponse "未认证"
// @Failure 403 {object} dto.ErrorResponse "权限不足"
// @Failure 404 {object} dto.ErrorResponse "资源不存在"
// @Failure 500 {object} dto.ErrorResponse "内部服务器错误"
// @Router /SetLLMModelStatus [post]
func (h *LLMModelHandler) SetLLMModelStatus(ctx *gin.Context) {
	var req contract.SetLLMModelStatusRequest
	if err := ctx.ShouldBindJSON(&req); err != nil {
		ctx.JSON(http.StatusBadRequest, dto.Error(dto.CodeInvalidParams, err.Error()))
		return
	}

	result, err := h.service.SetLLMModelStatus(ctx, req.ID, req.Status)
	if err != nil {
		handleLLMModelServiceError(ctx, err)
		return
	}
	ctx.JSON(http.StatusOK, dto.Success(result))
}

type DeleteLLMModelRequest struct {
	ID uint `json:"id" binding:"required"`
}

// @Summary 删除LLM模型
// @Description 根据ID删除LLM模型配置
// @Tags LLMModel
// @Accept json
// @Produce json
// @Param body body DeleteLLMModelRequest true "删除LLM模型请求"
// @Success 200 {object} dto.Response "成功响应"
// @Failure 400 {object} dto.ErrorResponse "请求参数错误"
// @Failure 401 {object} dto.ErrorResponse "未认证"
// @Failure 403 {object} dto.ErrorResponse "权限不足"
// @Failure 404 {object} dto.ErrorResponse "资源不存在"
// @Failure 500 {object} dto.ErrorResponse "内部服务器错误"
// @Router /DeleteLLMModel [post]
func (h *LLMModelHandler) DeleteLLMModel(ctx *gin.Context) {
	var req DeleteLLMModelRequest
	if err := ctx.ShouldBindJSON(&req); err != nil {
		ctx.JSON(http.StatusBadRequest, dto.Error(dto.CodeInvalidParams, err.Error()))
		return
	}

	if err := h.service.DeleteLLMModel(ctx, req.ID); err != nil {
		handleLLMModelServiceError(ctx, err)
		return
	}
	ctx.JSON(http.StatusOK, dto.Success(nil))
}

// @Summary 查询LLM模型列表
// @Description 分页查询LLM模型配置列表
// @Tags LLMModel
// @Accept json
// @Produce json
// @Param body body contract.ListLLMModelsRequest true "查询列表请求"
// @Success 200 {object} dto.Response "成功响应"
// @Failure 400 {object} dto.ErrorResponse "请求参数错误"
// @Failure 401 {object} dto.ErrorResponse "未认证"
// @Failure 500 {object} dto.ErrorResponse "内部服务器错误"
// @Router /ListLLMModels [post]
func (h *LLMModelHandler) ListLLMModels(ctx *gin.Context) {
	var req contract.ListLLMModelsRequest
	if err := ctx.ShouldBindJSON(&req); err != nil {
		ctx.JSON(http.StatusBadRequest, dto.Error(dto.CodeInvalidParams, err.Error()))
		return
	}

	req.Pagination.Fill()

	result, err := h.service.ListLLMModels(ctx, &req)
	if err != nil {
		handleLLMModelServiceError(ctx, err)
		return
	}
	ctx.JSON(http.StatusOK, dto.Success(result))
}

// @Summary 测试LLM模型
// @Description 测试LLM模型配置的连通性
// @Tags LLMModel
// @Accept json
// @Produce json
// @Param body body contract.TestLLMModelRequest true "测试LLM模型请求"
// @Success 200 {object} dto.Response "成功响应"
// @Failure 400 {object} dto.ErrorResponse "请求参数错误"
// @Failure 401 {object} dto.ErrorResponse "未认证"
// @Failure 500 {object} dto.ErrorResponse "内部服务器错误"
// @Router /TestLLMModel [post]
func (h *LLMModelHandler) TestLLMModel(ctx *gin.Context) {
	var req contract.TestLLMModelRequest
	if err := ctx.ShouldBindJSON(&req); err != nil {
		ctx.JSON(http.StatusBadRequest, dto.Error(dto.CodeInvalidParams, err.Error()))
		return
	}

	result, err := h.service.TestLLMModel(ctx, &req)
	if err != nil {
		handleLLMModelServiceError(ctx, err)
		return
	}
	ctx.JSON(http.StatusOK, dto.Success(result))
}

// ================================================================
// Error Handling
// ================================================================

func handleLLMModelServiceError(ctx *gin.Context, err error) {
	errMsg := err.Error()

	if isPermissionDenied(err) {
		ctx.JSON(http.StatusForbidden, dto.Error(dto.CodeForbidden, errMsg))
		return
	}

	// 通用错误处理
	switch errMsg {
	case "user not authenticated or org not set":
		ctx.JSON(http.StatusUnauthorized, dto.Error(dto.CodeInternalError, errMsg))
		return
	}

	// LLM模型特有错误处理
	switch errMsg {
	case "llm model not found":
		ctx.JSON(http.StatusNotFound, dto.Error(dto.CodeNotFound, errMsg))
	case "id or code is required",
		"provider is required",
		"model is required",
		"base_url is required",
		"api_key is required",
		"llm model with this code already exists",
		"invalid status",
		"启用中的模型不可编辑，请先禁用",
		"只能将启用中的模型设为默认",
		"启用中的模型不可删除，请先禁用",
		"该用途下没有其他启用中的模型可设为默认，无法禁用当前默认模型":
		ctx.JSON(http.StatusBadRequest, dto.Error(dto.CodeInvalidParams, errMsg))
	default:
		ctx.JSON(http.StatusInternalServerError, dto.Error(dto.CodeInternalError, errMsg))
	}
}
