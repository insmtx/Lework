package handler

import (
	"net/http"

	"github.com/gin-gonic/gin"

	"github.com/insmtx/Leros/backend/internal/api/contract"
	"github.com/insmtx/Leros/backend/internal/api/dto"
)

// AutomationHandler 处理自动化相关的 HTTP 请求。
//
// Phase 1 采用 owner 作用域权限：service 层根据 caller.Uin 校验自动化归属，
// 不依赖 Resource/PermGuard 中间件。
type AutomationHandler struct {
	service contract.AutomationService
}

// NewAutomationHandler 构造自动化 handler
func NewAutomationHandler(service contract.AutomationService) *AutomationHandler {
	return &AutomationHandler{service: service}
}

// RegisterRoutes 注册自动化相关路由
func (h *AutomationHandler) RegisterRoutes(r gin.IRouter) {
	r.POST("/CreateAutomation", h.CreateAutomation)
	r.POST("/GetAutomation", h.GetAutomation)
	r.POST("/UpdateAutomation", h.UpdateAutomation)
	r.POST("/DeleteAutomation", h.DeleteAutomation)
	r.POST("/ListAutomations", h.ListAutomations)
	r.POST("/RunAutomationNow", h.RunAutomationNow)
	r.POST("/ListAutomationExecutions", h.ListAutomationExecutions)
	r.POST("/GetAutomationExecution", h.GetAutomationExecution)
}

// RegisterAutomationRoutes 注册自动化路由的便捷入口
func RegisterAutomationRoutes(r gin.IRouter, service contract.AutomationService) {
	h := NewAutomationHandler(service)
	h.RegisterRoutes(r)
}

// CreateAutomation 创建自动化
// @Summary 创建自动化
// @Description 创建一条自动化定时任务计划
// @Tags Automation
// @Accept json
// @Produce json
// @Param body body contract.CreateAutomationRequest true "创建自动化请求"
// @Success 200 {object} dto.Response
// @Failure 400 {object} dto.ErrorResponse "请求参数错误"
// @Failure 401 {object} dto.ErrorResponse "未认证"
// @Failure 500 {object} dto.ErrorResponse "内部服务器错误"
func (h *AutomationHandler) CreateAutomation(ctx *gin.Context) {
	var req contract.CreateAutomationRequest
	if err := ctx.ShouldBindJSON(&req); err != nil {
		ctx.JSON(http.StatusBadRequest, dto.Error(dto.CodeInvalidParams, err.Error()))
		return
	}
	result, err := h.service.CreateAutomation(ctx, &req)
	if err != nil {
		handleAutomationServiceError(ctx, err)
		return
	}
	ctx.JSON(http.StatusOK, dto.Success(result))
}

// GetAutomation 查询自动化详情
// @Summary 查询自动化
// @Description 根据 public_id 查询自动化详情
// @Tags Automation
// @Accept json
// @Produce json
// @Param body body contract.GetAutomationRequest true "查询自动化请求"
// @Success 200 {object} dto.Response
// @Failure 400 {object} dto.ErrorResponse "请求参数错误"
// @Failure 401 {object} dto.ErrorResponse "未认证"
// @Failure 404 {object} dto.ErrorResponse "资源不存在"
// @Failure 500 {object} dto.ErrorResponse "内部服务器错误"
func (h *AutomationHandler) GetAutomation(ctx *gin.Context) {
	var req contract.GetAutomationRequest
	if err := ctx.ShouldBindJSON(&req); err != nil {
		ctx.JSON(http.StatusBadRequest, dto.Error(dto.CodeInvalidParams, err.Error()))
		return
	}
	result, err := h.service.GetAutomation(ctx, req.PublicID)
	if err != nil {
		handleAutomationServiceError(ctx, err)
		return
	}
	ctx.JSON(http.StatusOK, dto.Success(result))
}

// UpdateAutomation 更新自动化
// @Summary 更新自动化
// @Description 部分更新自动化配置
// @Tags Automation
// @Accept json
// @Produce json
// @Param body body automationUpdateRequestBody true "更新自动化请求"
// @Success 200 {object} dto.Response
// @Failure 400 {object} dto.ErrorResponse "请求参数错误"
// @Failure 401 {object} dto.ErrorResponse "未认证"
// @Failure 404 {object} dto.ErrorResponse "资源不存在"
// @Failure 403 {object} dto.ErrorResponse "无权访问"
// @Failure 500 {object} dto.ErrorResponse "内部服务器错误"
func (h *AutomationHandler) UpdateAutomation(ctx *gin.Context) {
	var req automationUpdateRequestBody
	if err := ctx.ShouldBindJSON(&req); err != nil {
		ctx.JSON(http.StatusBadRequest, dto.Error(dto.CodeInvalidParams, err.Error()))
		return
	}
	if req.PublicID == "" {
		ctx.JSON(http.StatusBadRequest, dto.Error(dto.CodeInvalidParams, "public_id is required"))
		return
	}
	result, err := h.service.UpdateAutomation(ctx, req.PublicID, &req.UpdateAutomationRequest)
	if err != nil {
		handleAutomationServiceError(ctx, err)
		return
	}
	ctx.JSON(http.StatusOK, dto.Success(result))
}

// DeleteAutomation 删除自动化
// @Summary 删除自动化
// @Description 软删除自动化计划
// @Tags Automation
// @Accept json
// @Produce json
// @Param body body contract.DeleteAutomationRequest true "删除自动化请求"
// @Success 200 {object} dto.Response
// @Failure 400 {object} dto.ErrorResponse "请求参数错误"
// @Failure 401 {object} dto.ErrorResponse "未认证"
// @Failure 404 {object} dto.ErrorResponse "资源不存在"
// @Failure 403 {object} dto.ErrorResponse "无权访问"
// @Failure 500 {object} dto.ErrorResponse "内部服务器错误"
func (h *AutomationHandler) DeleteAutomation(ctx *gin.Context) {
	var req contract.DeleteAutomationRequest
	if err := ctx.ShouldBindJSON(&req); err != nil {
		ctx.JSON(http.StatusBadRequest, dto.Error(dto.CodeInvalidParams, err.Error()))
		return
	}
	if err := h.service.DeleteAutomation(ctx, req.PublicID); err != nil {
		handleAutomationServiceError(ctx, err)
		return
	}
	ctx.JSON(http.StatusOK, dto.Success(nil))
}

// ListAutomations 查询自动化列表
// @Summary 查询自动化列表
// @Description 分页查询当前用户自动化列表
// @Tags Automation
// @Accept json
// @Produce json
// @Param body body contract.ListAutomationsRequest true "查询自动化列表请求"
// @Success 200 {object} dto.Response
// @Failure 400 {object} dto.ErrorResponse "请求参数错误"
// @Failure 401 {object} dto.ErrorResponse "未认证"
// @Failure 500 {object} dto.ErrorResponse "内部服务器错误"
func (h *AutomationHandler) ListAutomations(ctx *gin.Context) {
	var req contract.ListAutomationsRequest
	if err := ctx.ShouldBindJSON(&req); err != nil {
		ctx.JSON(http.StatusBadRequest, dto.Error(dto.CodeInvalidParams, err.Error()))
		return
	}
	result, err := h.service.ListAutomations(ctx, &req)
	if err != nil {
		handleAutomationServiceError(ctx, err)
		return
	}
	ctx.JSON(http.StatusOK, dto.Success(result))
}

// RunAutomationNow 立即运行自动化
// @Summary 立即运行
// @Description 手动触发一次自动化执行；有活动执行时返回 409
// @Tags Automation
// @Accept json
// @Produce json
// @Param body body contract.RunAutomationNowRequest true "立即运行请求"
// @Success 200 {object} dto.Response
// @Failure 400 {object} dto.ErrorResponse "请求参数错误"
// @Failure 401 {object} dto.ErrorResponse "未认证"
// @Failure 404 {object} dto.ErrorResponse "资源不存在"
// @Failure 409 {object} dto.ErrorResponse "已有活动执行"
// @Failure 500 {object} dto.ErrorResponse "内部服务器错误"
func (h *AutomationHandler) RunAutomationNow(ctx *gin.Context) {
	var req contract.RunAutomationNowRequest
	if err := ctx.ShouldBindJSON(&req); err != nil {
		ctx.JSON(http.StatusBadRequest, dto.Error(dto.CodeInvalidParams, err.Error()))
		return
	}
	result, err := h.service.RunAutomationNow(ctx, req.PublicID)
	if err != nil {
		handleAutomationServiceError(ctx, err)
		return
	}
	ctx.JSON(http.StatusOK, dto.Success(result))
}

// ListAutomationExecutions 查询执行历史
// @Summary 查询执行历史
// @Description 分页查询某自动化的执行记录
// @Tags Automation
// @Accept json
// @Produce json
// @Param body body contract.ListAutomationExecutionsRequest true "查询执行历史请求"
// @Success 200 {object} dto.Response
// @Failure 400 {object} dto.ErrorResponse "请求参数错误"
// @Failure 401 {object} dto.ErrorResponse "未认证"
// @Failure 404 {object} dto.ErrorResponse "资源不存在"
// @Failure 500 {object} dto.ErrorResponse "内部服务器错误"
func (h *AutomationHandler) ListAutomationExecutions(ctx *gin.Context) {
	var req contract.ListAutomationExecutionsRequest
	if err := ctx.ShouldBindJSON(&req); err != nil {
		ctx.JSON(http.StatusBadRequest, dto.Error(dto.CodeInvalidParams, err.Error()))
		return
	}
	result, err := h.service.ListAutomationExecutions(ctx, &req)
	if err != nil {
		handleAutomationServiceError(ctx, err)
		return
	}
	ctx.JSON(http.StatusOK, dto.Success(result))
}

// GetAutomationExecution 查询执行详情
// @Summary 查询执行详情
// @Description 根据 execution public_id 查询某次执行详情
// @Tags Automation
// @Accept json
// @Produce json
// @Param body body contract.GetAutomationExecutionRequest true "查询执行详情请求"
// @Success 200 {object} dto.Response
// @Failure 401 {object} dto.ErrorResponse "未认证"
// @Failure 404 {object} dto.ErrorResponse "执行记录不存在"
// @Failure 500 {object} dto.ErrorResponse "内部服务器错误"
func (h *AutomationHandler) GetAutomationExecution(ctx *gin.Context) {
	var req contract.GetAutomationExecutionRequest
	if err := ctx.ShouldBindJSON(&req); err != nil {
		ctx.JSON(http.StatusBadRequest, dto.Error(dto.CodeInvalidParams, err.Error()))
		return
	}
	result, err := h.service.GetAutomationExecution(ctx, req.PublicID)
	if err != nil {
		handleAutomationServiceError(ctx, err)
		return
	}
	ctx.JSON(http.StatusOK, dto.Success(result))
}

// automationUpdateRequestBody 更新请求，组合路由所需的 public_id
type automationUpdateRequestBody struct {
	PublicID string `json:"public_id" binding:"required"`
	contract.UpdateAutomationRequest
}

// handleAutomationServiceError 将 automation service 错误映射为 HTTP 状态码
func handleAutomationServiceError(ctx *gin.Context, err error) {
	errMsg := err.Error()

	switch errMsg {
	case "user not authenticated or org not set":
		ctx.JSON(http.StatusUnauthorized, dto.Error(dto.CodeUnauthorized, errMsg))
		return
	}

	switch errMsg {
	case "automation not found",
		"automation_execution_not_found",
		"automation link project not found":
		ctx.JSON(http.StatusNotFound, dto.Error(dto.CodeNotFound, errMsg))
	case "automation forbidden",
		"automation link project forbidden":
		ctx.JSON(http.StatusForbidden, dto.Error(dto.CodeForbidden, errMsg))
	case "automation_run_in_progress",
		"automation_project_change_conflict":
		ctx.JSON(http.StatusConflict, dto.Error(dto.CodeConflict, errMsg))
	case "invalid automation name",
		"invalid automation instruction",
		"invalid_automation_schedule",
		"invalid_automation_timezone",
		"no default assistant in organization",
		"automation link project unavailable",
		"public_id is required":
		ctx.JSON(http.StatusBadRequest, dto.Error(dto.CodeInvalidParams, errMsg))
	default:
		ctx.JSON(http.StatusInternalServerError, dto.Error(dto.CodeInternalError, errMsg))
	}
}
