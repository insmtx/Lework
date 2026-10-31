package handler

import (
	"encoding/json"
	"errors"
	"net/http"
	"strings"

	"github.com/gin-gonic/gin"

	"github.com/insmtx/Lework/backend/internal/api/contract"
	"github.com/insmtx/Lework/backend/internal/api/dto"
	"github.com/insmtx/Lework/backend/internal/service"
	"github.com/insmtx/Lework/backend/types"
)

type TaskHandler struct {
	service contract.TaskService
	permSvc PermGuarder
}

func NewTaskHandler(service contract.TaskService, permSvc PermGuarder) *TaskHandler {
	return &TaskHandler{service: service, permSvc: permSvc}
}

// ================================================================
// Route Registration
// ================================================================

func (h *TaskHandler) RegisterRoutes(r gin.IRouter) {
	r.POST("/CreateTask",
		PermGuardProjectTaskAction(h.permSvc, extractCreateTaskProjectID, types.ActionTaskCreate),
		h.CreateTask,
	)
	r.POST("/GetTask",
		PermGuard(h.permSvc, types.ResourceTypeTask, types.ActionTaskView, extractTaskPublicID),
		h.GetTask,
	)
	r.POST("/UpdateTask",
		PermGuard(h.permSvc, types.ResourceTypeTask, types.ActionTaskUpdate, extractUpdateTaskPublicID),
		h.UpdateTask,
	)
	r.POST("/DeleteTask",
		PermGuard(h.permSvc, types.ResourceTypeTask, types.ActionTaskDelete, extractDeleteTaskPublicID),
		h.DeleteTask,
	)
	r.POST("/ListTasks", h.ListTasks)
	r.POST("/GetTaskDeliverables",
		PermGuard(h.permSvc, types.ResourceTypeTask, types.ActionTaskView, extractTaskIDField),
		h.GetTaskDeliverables,
	)
	r.POST("/AcceptTask",
		PermGuard(h.permSvc, types.ResourceTypeTask, types.ActionTaskUpdate, extractTaskIDField),
		h.AcceptTask,
	)
	r.POST("/RejectTask",
		PermGuard(h.permSvc, types.ResourceTypeTask, types.ActionTaskUpdate, extractTaskIDField),
		h.RejectTask,
	)
}

// extractTaskIDField 从 task_id 字段提取任务 public_id（验收相关接口统一使用 task_id）。
func extractTaskIDField(body []byte) (string, error) {
	var req struct {
		TaskID string `json:"task_id"`
	}
	if err := json.Unmarshal(body, &req); err != nil {
		return "", err
	}
	if req.TaskID == "" {
		return "", errors.New("task_id is required")
	}
	return req.TaskID, nil
}

// extractTaskPublicID 从请求 body 提取任务 public_id，供 PermGuard 使用。
func extractTaskPublicID(body []byte) (string, error) {
	var req struct {
		PublicID *string `json:"public_id"`
	}
	if err := json.Unmarshal(body, &req); err != nil {
		return "", err
	}
	if req.PublicID == nil || *req.PublicID == "" {
		return "", errors.New("public_id is required")
	}
	return *req.PublicID, nil
}

func extractCreateTaskProjectID(body []byte) (string, error) {
	var req struct {
		ProjectID string `json:"project_id"`
	}
	if err := json.Unmarshal(body, &req); err != nil {
		return "", err
	}
	if req.ProjectID == "" {
		return "", errors.New("project_id is required")
	}
	return req.ProjectID, nil
}

func extractUpdateTaskPublicID(body []byte) (string, error) {
	var req struct {
		PublicID string `json:"public_id"`
	}
	if err := json.Unmarshal(body, &req); err != nil {
		return "", err
	}
	if req.PublicID == "" {
		return "", errors.New("public_id is required")
	}
	return req.PublicID, nil
}

func extractDeleteTaskPublicID(body []byte) (string, error) {
	return extractUpdateTaskPublicID(body)
}

func RegisterTaskRoutes(r gin.IRouter, service contract.TaskService, permSvc PermGuarder) {
	h := NewTaskHandler(service, permSvc)
	h.RegisterRoutes(r)
}

// ================================================================
// Handler Methods
// ================================================================

// @Summary 创建任务
// @Description 创建一个新任务
// @Tags Task
// @Accept json
// @Produce json
// @Param body body contract.CreateTaskRequest true "创建任务请求"
// @Success 200 {object} dto.Response "成功响应"
// @Failure 400 {object} dto.ErrorResponse "请求参数错误"
// @Failure 401 {object} dto.ErrorResponse "未认证"
// @Failure 500 {object} dto.ErrorResponse "内部服务器错误"
// @Router /CreateTask [post]
func (h *TaskHandler) CreateTask(ctx *gin.Context) {
	var req contract.CreateTaskRequest
	if err := ctx.ShouldBindJSON(&req); err != nil {
		ctx.JSON(http.StatusBadRequest, dto.Error(dto.CodeInvalidParams, err.Error()))
		return
	}
	if strings.TrimSpace(req.Title) == "" {
		ctx.JSON(http.StatusBadRequest, dto.Error(dto.CodeInvalidParams, "title is required"))
		return
	}

	result, err := h.service.CreateTask(ctx, &req)
	if err != nil {
		handleTaskServiceError(ctx, err)
		return
	}
	ctx.JSON(http.StatusOK, dto.Success(result))
}

type GetTaskRequest struct {
	PublicID *string `json:"public_id,omitempty"`
}

// @Summary 获取任务详情
// @Description 根据PublicId获取任务详情
// @Tags Task
// @Accept json
// @Produce json
// @Param body body GetTaskRequest true "获取任务请求"
// @Success 200 {object} dto.Response "成功响应"
// @Failure 400 {object} dto.ErrorResponse "请求参数错误"
// @Failure 401 {object} dto.ErrorResponse "未认证"
// @Failure 404 {object} dto.ErrorResponse "资源不存在"
// @Failure 500 {object} dto.ErrorResponse "内部服务器错误"
// @Router /GetTask [post]
func (h *TaskHandler) GetTask(ctx *gin.Context) {
	var req GetTaskRequest
	if err := ctx.ShouldBindJSON(&req); err != nil {
		ctx.JSON(http.StatusBadRequest, dto.Error(dto.CodeInvalidParams, err.Error()))
		return
	}
	if req.PublicID == nil || *req.PublicID == "" {
		ctx.JSON(http.StatusBadRequest, dto.Error(dto.CodeInvalidParams, "public_id is required"))
		return
	}

	// 权限已由路由链上的 PermGuard 在 handler 执行前完成检查，此处直接调用服务
	result, err := h.service.GetTask(ctx, *req.PublicID)
	if err != nil {
		handleTaskServiceError(ctx, err)
		return
	}
	ctx.JSON(http.StatusOK, dto.Success(result))
}

type UpdateTaskRequest struct {
	PublicID string `json:"public_id" binding:"required"`
	contract.UpdateTaskRequest
}

// @Summary 更新任务
// @Description 更新任务信息
// @Tags Task
// @Accept json
// @Produce json
// @Param body body UpdateTaskRequest true "更新任务请求"
// @Success 200 {object} dto.Response "成功响应"
// @Failure 400 {object} dto.ErrorResponse "请求参数错误"
// @Failure 401 {object} dto.ErrorResponse "未认证"
// @Failure 404 {object} dto.ErrorResponse "资源不存在"
// @Failure 500 {object} dto.ErrorResponse "内部服务器错误"
// @Router /UpdateTask [post]
func (h *TaskHandler) UpdateTask(ctx *gin.Context) {
	var req UpdateTaskRequest
	if err := ctx.ShouldBindJSON(&req); err != nil {
		ctx.JSON(http.StatusBadRequest, dto.Error(dto.CodeInvalidParams, err.Error()))
		return
	}

	result, err := h.service.UpdateTask(ctx, req.PublicID, &req.UpdateTaskRequest)
	if err != nil {
		handleTaskServiceError(ctx, err)
		return
	}
	ctx.JSON(http.StatusOK, dto.Success(result))
}

type DeleteTaskRequest struct {
	PublicID string `json:"public_id" binding:"required"`
}

// @Summary 删除任务
// @Description 根据PublicId删除任务（软删除）
// @Tags Task
// @Accept json
// @Produce json
// @Param body body DeleteTaskRequest true "删除任务请求"
// @Success 200 {object} dto.Response "成功响应"
// @Failure 400 {object} dto.ErrorResponse "请求参数错误"
// @Failure 401 {object} dto.ErrorResponse "未认证"
// @Failure 404 {object} dto.ErrorResponse "资源不存在"
// @Failure 500 {object} dto.ErrorResponse "内部服务器错误"
// @Router /DeleteTask [post]
func (h *TaskHandler) DeleteTask(ctx *gin.Context) {
	var req DeleteTaskRequest
	if err := ctx.ShouldBindJSON(&req); err != nil {
		ctx.JSON(http.StatusBadRequest, dto.Error(dto.CodeInvalidParams, err.Error()))
		return
	}

	if err := h.service.DeleteTask(ctx, req.PublicID); err != nil {
		handleTaskServiceError(ctx, err)
		return
	}
	ctx.JSON(http.StatusOK, dto.Success(nil))
}

// @Summary 查询任务列表
// @Description 分页查询任务列表
// @Tags Task
// @Accept json
// @Produce json
// @Param body body contract.ListTasksRequest true "查询列表请求"
// @Success 200 {object} dto.Response "成功响应"
// @Failure 400 {object} dto.ErrorResponse "请求参数错误"
// @Failure 401 {object} dto.ErrorResponse "未认证"
// @Failure 500 {object} dto.ErrorResponse "内部服务器错误"
// @Router /ListTasks [post]
func (h *TaskHandler) ListTasks(ctx *gin.Context) {
	var req contract.ListTasksRequest
	if err := ctx.ShouldBindJSON(&req); err != nil {
		ctx.JSON(http.StatusBadRequest, dto.Error(dto.CodeInvalidParams, err.Error()))
		return
	}

	req.Fill()

	result, err := h.service.ListTasks(ctx, &req)
	if err != nil {
		handleTaskServiceError(ctx, err)
		return
	}
	ctx.JSON(http.StatusOK, dto.Success(result))
}

// @Summary 查询任务交付物
// @Description 聚合任务会话产物、项目文件与 run 结果摘要
// @Tags Task
// @Accept json
// @Produce json
// @Param body body contract.GetTaskDeliverablesRequest true "查询交付物请求"
// @Success 200 {object} dto.Response "成功响应"
// @Router /GetTaskDeliverables [post]
func (h *TaskHandler) GetTaskDeliverables(ctx *gin.Context) {
	var req contract.GetTaskDeliverablesRequest
	if err := ctx.ShouldBindJSON(&req); err != nil {
		ctx.JSON(http.StatusBadRequest, dto.Error(dto.CodeInvalidParams, err.Error()))
		return
	}
	result, err := h.service.GetTaskDeliverables(ctx, req.TaskID)
	if err != nil {
		handleTaskServiceError(ctx, err)
		return
	}
	ctx.JSON(http.StatusOK, dto.Success(result))
}

// @Summary 验收通过任务
// @Tags Task
// @Accept json
// @Produce json
// @Param body body contract.AcceptTaskRequest true "验收请求"
// @Success 200 {object} dto.Response "成功响应"
// @Router /AcceptTask [post]
func (h *TaskHandler) AcceptTask(ctx *gin.Context) {
	var req contract.AcceptTaskRequest
	if err := ctx.ShouldBindJSON(&req); err != nil {
		ctx.JSON(http.StatusBadRequest, dto.Error(dto.CodeInvalidParams, err.Error()))
		return
	}
	result, err := h.service.AcceptTask(ctx, req.TaskID, req.Note)
	if err != nil {
		handleTaskServiceError(ctx, err)
		return
	}
	ctx.JSON(http.StatusOK, dto.Success(result))
}

// @Summary 验收驳回任务
// @Tags Task
// @Accept json
// @Produce json
// @Param body body contract.RejectTaskRequest true "驳回请求"
// @Success 200 {object} dto.Response "成功响应"
// @Router /RejectTask [post]
func (h *TaskHandler) RejectTask(ctx *gin.Context) {
	var req contract.RejectTaskRequest
	if err := ctx.ShouldBindJSON(&req); err != nil {
		ctx.JSON(http.StatusBadRequest, dto.Error(dto.CodeInvalidParams, err.Error()))
		return
	}
	result, err := h.service.RejectTask(ctx, req.TaskID, req.Note)
	if err != nil {
		handleTaskServiceError(ctx, err)
		return
	}
	ctx.JSON(http.StatusOK, dto.Success(result))
}

// ================================================================
// Error Handling
// ================================================================

func handleTaskServiceError(ctx *gin.Context, err error) {
	errMsg := err.Error()

	switch errMsg {
	case "user not authenticated or org not set":
		ctx.JSON(http.StatusUnauthorized, dto.Error(dto.CodeUnauthorized, errMsg))
		return
	}

	switch errMsg {
	case "task not found",
		"project not found":
		ctx.JSON(http.StatusNotFound, dto.Error(dto.CodeNotFound, errMsg))
	case "title is required",
		"title cannot be empty",
		"public_id is required",
		"project_id is required",
		"task_id is required",
		"acceptance note is required",
		"digital assistant not found",
		"digital assistant is not active":
		ctx.JSON(http.StatusBadRequest, dto.Error(dto.CodeInvalidParams, errMsg))
	default:
		if isPermissionDenied(err) {
			ctx.JSON(http.StatusForbidden, dto.Error(dto.CodeForbidden, errMsg))
			return
		}
		if errors.Is(err, service.ErrTaskNotPendingAcceptance) ||
			errors.Is(err, service.ErrInvalidTaskStatusTransition) ||
			errors.Is(err, service.ErrAssistantWorkerNotReady) {
			ctx.JSON(http.StatusConflict, dto.Error(dto.CodeConflict, errMsg))
			return
		}
		ctx.JSON(http.StatusInternalServerError, dto.Error(dto.CodeInternalError, errMsg))
	}
}
