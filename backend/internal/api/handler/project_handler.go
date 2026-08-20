package handler

import (
	"encoding/json"
	"errors"
	"net/http"
	"strings"

	"github.com/gin-gonic/gin"

	"github.com/insmtx/Leros/backend/internal/api/contract"
	"github.com/insmtx/Leros/backend/internal/api/dto"
	"github.com/insmtx/Leros/backend/types"
)

type ProjectHandler struct {
	service contract.ProjectService
	permSvc PermGuarder
}

func NewProjectHandler(service contract.ProjectService, permSvc PermGuarder) *ProjectHandler {
	return &ProjectHandler{service: service, permSvc: permSvc}
}

// ================================================================
// Route Registration
// ================================================================

func (h *ProjectHandler) RegisterRoutes(r gin.IRouter) {
	r.POST("/CreateProject", h.CreateProject)
	r.POST("/GetProject",
		PermGuard(h.permSvc, types.ResourceTypeProject, types.ActionProjectView, extractProjectPublicID),
		h.GetProject,
	)
	r.POST("/DetailProject",
		PermGuardActions(h.permSvc, types.ResourceTypeProject, extractProjectPublicID,
			types.ActionProjectView, types.ActionProjectMemberList),
		h.DetailProject,
	)
	r.POST("/UpdateProject",
		PermGuard(h.permSvc, types.ResourceTypeProject, types.ActionProjectUpdate, extractUpdateProjectPublicID),
		h.UpdateProject,
	)
	// Project Plugin permissions are evaluated in the service because Worker
	// identities must first resolve worker_id to the bound AI teammate.
	r.POST("/ListProjectPlugins", h.ListProjectPlugins)
	r.POST("/AddProjectPlugin", h.AddProjectPlugin)
	r.POST("/RemoveProjectPlugin", h.RemoveProjectPlugin)
	r.POST("/DeleteProject",
		PermGuard(h.permSvc, types.ResourceTypeProject, types.ActionProjectDelete, extractDeleteProjectPublicID),
		h.DeleteProject,
	)
	r.POST("/LeaveProject",
		PermGuard(h.permSvc, types.ResourceTypeProject, types.ActionProjectMemberLeave, extractLeaveProjectPublicID),
		h.LeaveProject,
	)
	r.POST("/ListProjects", h.ListProjects)
	r.POST("/ListProjectActivities",
		PermGuardOptionalProject(h.permSvc, extractOptionalProjectID),
		h.ListProjectActivities,
	)
	r.POST("/GetWorkbenchRecentContext", h.GetWorkbenchRecentContext)
	r.POST("/SaveWorkbenchRecentContext",
		PermGuard(h.permSvc, types.ResourceTypeProject, types.ActionProjectView, extractWorkbenchProjectID),
		h.SaveWorkbenchRecentContext,
	)
}

// extractProjectPublicID 从请求 body 提取项目 public_id，供 PermGuard 使用。
func extractProjectPublicID(body []byte) (string, error) {
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

func extractUpdateProjectPublicID(body []byte) (string, error) {
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

func extractDeleteProjectPublicID(body []byte) (string, error) {
	return extractUpdateProjectPublicID(body)
}

func extractLeaveProjectPublicID(body []byte) (string, error) {
	return extractUpdateProjectPublicID(body)
}

func extractWorkbenchProjectID(body []byte) (string, error) {
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

func extractOptionalProjectID(body []byte) (string, error) {
	var req struct {
		ProjectID string `json:"project_id"`
	}
	if err := json.Unmarshal(body, &req); err != nil {
		return "", err
	}
	if req.ProjectID == "" {
		return "", errors.New("project_id is optional")
	}
	return req.ProjectID, nil
}

func RegisterProjectRoutes(r gin.IRouter, service contract.ProjectService, permSvc PermGuarder) {
	h := NewProjectHandler(service, permSvc)
	h.RegisterRoutes(r)
}

// ================================================================
// Handler Methods
// ================================================================

// @Summary 创建项目
// @Description 创建一个新项目
// @Tags Project
// @Accept json
// @Produce json
// @Param body body contract.CreateProjectRequest true "创建项目请求"
// @Success 200 {object} dto.Response "成功响应"
// @Failure 400 {object} dto.ErrorResponse "请求参数错误"
// @Failure 401 {object} dto.ErrorResponse "未认证"
// @Failure 500 {object} dto.ErrorResponse "内部服务器错误"
// @Router /CreateProject [post]
func (h *ProjectHandler) CreateProject(ctx *gin.Context) {
	var req contract.CreateProjectRequest
	if err := ctx.ShouldBindJSON(&req); err != nil {
		ctx.JSON(http.StatusBadRequest, dto.Error(dto.CodeInvalidParams, err.Error()))
		return
	}

	result, err := h.service.CreateProject(ctx, &req)
	if err != nil {
		handleProjectServiceError(ctx, err)
		return
	}
	ctx.JSON(http.StatusOK, dto.Success(result))
}

type GetProjectRequest struct {
	PublicID *string `json:"public_id,omitempty"`
}

// @Summary 获取项目详情
// @Description 根据PublicId获取项目详情
// @Tags Project
// @Accept json
// @Produce json
// @Param body body GetProjectRequest true "获取项目请求"
// @Success 200 {object} dto.Response "成功响应"
// @Failure 400 {object} dto.ErrorResponse "请求参数错误"
// @Failure 401 {object} dto.ErrorResponse "未认证"
// @Failure 404 {object} dto.ErrorResponse "资源不存在"
// @Failure 500 {object} dto.ErrorResponse "内部服务器错误"
// @Router /GetProject [post]
func (h *ProjectHandler) GetProject(ctx *gin.Context) {
	var req GetProjectRequest
	if err := ctx.ShouldBindJSON(&req); err != nil {
		ctx.JSON(http.StatusBadRequest, dto.Error(dto.CodeInvalidParams, err.Error()))
		return
	}
	if req.PublicID == nil || *req.PublicID == "" {
		ctx.JSON(http.StatusBadRequest, dto.Error(dto.CodeInvalidParams, "public_id is required"))
		return
	}

	// 权限已由路由链上的 PermGuard 在 handler 执行前完成检查，此处直接调用服务
	result, err := h.service.GetProject(ctx, *req.PublicID)
	if err != nil {
		handleProjectServiceError(ctx, err)
		return
	}
	ctx.JSON(http.StatusOK, dto.Success(result))
}

// @Summary 获取项目详情（含任务、会话、产物、成员）
// @Description 根据PublicId获取项目完整详情
// @Tags Project
// @Accept json
// @Produce json
// @Param body body GetProjectRequest true "获取项目详情请求"
// @Success 200 {object} dto.Response "成功响应"
// @Failure 400 {object} dto.ErrorResponse "请求参数错误"
// @Failure 401 {object} dto.ErrorResponse "未认证"
// @Failure 404 {object} dto.ErrorResponse "资源不存在"
// @Failure 500 {object} dto.ErrorResponse "内部服务器错误"
// @Router /DetailProject [post]
func (h *ProjectHandler) DetailProject(ctx *gin.Context) {
	var req GetProjectRequest
	if err := ctx.ShouldBindJSON(&req); err != nil {
		ctx.JSON(http.StatusBadRequest, dto.Error(dto.CodeInvalidParams, err.Error()))
		return
	}
	if req.PublicID == nil || *req.PublicID == "" {
		ctx.JSON(http.StatusBadRequest, dto.Error(dto.CodeInvalidParams, "public_id is required"))
		return
	}

	result, err := h.service.DetailProject(ctx, *req.PublicID)
	if err != nil {
		handleProjectServiceError(ctx, err)
		return
	}
	ctx.JSON(http.StatusOK, dto.Success(result))
}

type UpdateProjectRequest struct {
	PublicID string `json:"public_id" binding:"required"`
	contract.UpdateProjectRequest
}

// @Summary 更新项目
// @Description 更新项目信息
// @Tags Project
// @Accept json
// @Produce json
// @Param body body UpdateProjectRequest true "更新项目请求"
// @Success 200 {object} dto.Response "成功响应"
// @Failure 400 {object} dto.ErrorResponse "请求参数错误"
// @Failure 401 {object} dto.ErrorResponse "未认证"
// @Failure 404 {object} dto.ErrorResponse "资源不存在"
// @Failure 500 {object} dto.ErrorResponse "内部服务器错误"
// @Router /UpdateProject [post]
func (h *ProjectHandler) UpdateProject(ctx *gin.Context) {
	var req UpdateProjectRequest
	if err := ctx.ShouldBindJSON(&req); err != nil {
		ctx.JSON(http.StatusBadRequest, dto.Error(dto.CodeInvalidParams, err.Error()))
		return
	}

	result, err := h.service.UpdateProject(ctx, req.PublicID, &req.UpdateProjectRequest)
	if err != nil {
		handleProjectServiceError(ctx, err)
		return
	}
	ctx.JSON(http.StatusOK, dto.Success(result))
}

// ListProjectPlugins returns project bindings, optionally restricted to one plugin kind.
func (h *ProjectHandler) ListProjectPlugins(ctx *gin.Context) {
	var req contract.ListProjectPluginsRequest
	if err := ctx.ShouldBindJSON(&req); err != nil {
		ctx.JSON(http.StatusBadRequest, dto.Error(dto.CodeInvalidParams, err.Error()))
		return
	}
	result, err := h.service.ListProjectPlugins(ctx, &req)
	if err != nil {
		ctx.JSON(http.StatusInternalServerError, dto.Error(dto.CodeInternalError, err.Error()))
		return
	}
	ctx.JSON(http.StatusOK, dto.Success(result))
}

// AddProjectPlugin authorizes an organization plugin for a project.
func (h *ProjectHandler) AddProjectPlugin(ctx *gin.Context) {
	var req contract.UpdateProjectPluginRequest
	if err := ctx.ShouldBindJSON(&req); err != nil {
		ctx.JSON(http.StatusBadRequest, dto.Error(dto.CodeInvalidParams, err.Error()))
		return
	}
	if err := validateProjectPluginMutationRequest(&req); err != nil {
		ctx.JSON(http.StatusBadRequest, dto.Error(dto.CodeInvalidParams, err.Error()))
		return
	}
	result, err := h.service.AddProjectPlugin(ctx, &req)
	if err != nil {
		ctx.JSON(http.StatusInternalServerError, dto.Error(dto.CodeInternalError, err.Error()))
		return
	}
	ctx.JSON(http.StatusOK, dto.Success(result))
}

// RemoveProjectPlugin removes one project plugin authorization.
func (h *ProjectHandler) RemoveProjectPlugin(ctx *gin.Context) {
	var req contract.UpdateProjectPluginRequest
	if err := ctx.ShouldBindJSON(&req); err != nil {
		ctx.JSON(http.StatusBadRequest, dto.Error(dto.CodeInvalidParams, err.Error()))
		return
	}
	if err := validateProjectPluginMutationRequest(&req); err != nil {
		ctx.JSON(http.StatusBadRequest, dto.Error(dto.CodeInvalidParams, err.Error()))
		return
	}
	result, err := h.service.RemoveProjectPlugin(ctx, &req)
	if err != nil {
		ctx.JSON(http.StatusInternalServerError, dto.Error(dto.CodeInternalError, err.Error()))
		return
	}
	ctx.JSON(http.StatusOK, dto.Success(result))
}

func validateProjectPluginMutationRequest(req *contract.UpdateProjectPluginRequest) error {
	pluginID := strings.TrimSpace(req.PluginID)
	pluginCode := strings.TrimSpace(req.PluginCode)
	if pluginID == "" && pluginCode == "" {
		return errors.New("plugin_id or plugin_code is required")
	}
	if pluginID != "" && pluginCode != "" {
		return errors.New("plugin_id and plugin_code cannot be used together")
	}
	if pluginCode != "" && strings.TrimSpace(req.Kind) == "" {
		return errors.New("kind is required with plugin_code")
	}
	return nil
}

type DeleteProjectRequest struct {
	PublicID string `json:"public_id" binding:"required"`
}

// @Summary 删除项目
// @Description 根据PublicId删除项目（软删除）
// @Tags Project
// @Accept json
// @Produce json
// @Param body body DeleteProjectRequest true "删除项目请求"
// @Success 200 {object} dto.Response "成功响应"
// @Failure 400 {object} dto.ErrorResponse "请求参数错误"
// @Failure 401 {object} dto.ErrorResponse "未认证"
// @Failure 404 {object} dto.ErrorResponse "资源不存在"
// @Failure 500 {object} dto.ErrorResponse "内部服务器错误"
// @Router /DeleteProject [post]
func (h *ProjectHandler) DeleteProject(ctx *gin.Context) {
	var req DeleteProjectRequest
	if err := ctx.ShouldBindJSON(&req); err != nil {
		ctx.JSON(http.StatusBadRequest, dto.Error(dto.CodeInvalidParams, err.Error()))
		return
	}

	if err := h.service.DeleteProject(ctx, req.PublicID); err != nil {
		handleProjectServiceError(ctx, err)
		return
	}
	ctx.JSON(http.StatusOK, dto.Success(nil))
}

type LeaveProjectRequest struct {
	PublicID string `json:"public_id" binding:"required"`
}

// LeaveProject 当前用户退出项目。
func (h *ProjectHandler) LeaveProject(ctx *gin.Context) {
	var req LeaveProjectRequest
	if err := ctx.ShouldBindJSON(&req); err != nil {
		ctx.JSON(http.StatusBadRequest, dto.Error(dto.CodeInvalidParams, err.Error()))
		return
	}

	if err := h.service.LeaveProject(ctx, req.PublicID); err != nil {
		handleProjectServiceError(ctx, err)
		return
	}
	ctx.JSON(http.StatusOK, dto.Success(nil))
}

// @Summary 查询项目列表
// @Description 分页查询项目列表
// @Tags Project
// @Accept json
// @Produce json
// @Param body body contract.ListProjectsRequest true "查询列表请求"
// @Success 200 {object} dto.Response "成功响应"
// @Failure 400 {object} dto.ErrorResponse "请求参数错误"
// @Failure 401 {object} dto.ErrorResponse "未认证"
// @Failure 500 {object} dto.ErrorResponse "内部服务器错误"
// @Router /ListProjects [post]
func (h *ProjectHandler) ListProjects(ctx *gin.Context) {
	var req contract.ListProjectsRequest
	if err := ctx.ShouldBindJSON(&req); err != nil {
		ctx.JSON(http.StatusBadRequest, dto.Error(dto.CodeInvalidParams, err.Error()))
		return
	}

	req.Fill()

	result, err := h.service.ListProjects(ctx, &req)
	if err != nil {
		handleProjectServiceError(ctx, err)
		return
	}
	ctx.JSON(http.StatusOK, dto.Success(result))
}

// @Summary 查询项目操作动态
// @Description 按项目和操作人筛选项目操作动态
// @Tags Project
// @Accept json
// @Produce json
// @Param body body contract.ListProjectActivitiesRequest true "查询项目动态请求"
// @Success 200 {object} dto.Response "成功响应"
// @Failure 400 {object} dto.ErrorResponse "请求参数错误"
// @Failure 401 {object} dto.ErrorResponse "未认证"
// @Failure 500 {object} dto.ErrorResponse "内部服务器错误"
// @Router /ListProjectActivities [post]
func (h *ProjectHandler) ListProjectActivities(ctx *gin.Context) {
	var req contract.ListProjectActivitiesRequest
	if err := ctx.ShouldBindJSON(&req); err != nil {
		ctx.JSON(http.StatusBadRequest, dto.Error(dto.CodeInvalidParams, err.Error()))
		return
	}

	result, err := h.service.ListProjectActivities(ctx, &req)
	if err != nil {
		handleProjectServiceError(ctx, err)
		return
	}
	ctx.JSON(http.StatusOK, dto.Success(result))
}

func (h *ProjectHandler) GetWorkbenchRecentContext(ctx *gin.Context) {
	result, err := h.service.GetWorkbenchRecentContext(ctx)
	if err != nil {
		handleProjectServiceError(ctx, err)
		return
	}
	ctx.JSON(http.StatusOK, dto.Success(result))
}

func (h *ProjectHandler) SaveWorkbenchRecentContext(ctx *gin.Context) {
	var req contract.SaveWorkbenchRecentContextRequest
	if err := ctx.ShouldBindJSON(&req); err != nil {
		ctx.JSON(http.StatusBadRequest, dto.Error(dto.CodeInvalidParams, err.Error()))
		return
	}

	result, err := h.service.SaveWorkbenchRecentContext(ctx, &req)
	if err != nil {
		handleProjectServiceError(ctx, err)
		return
	}
	ctx.JSON(http.StatusOK, dto.Success(result))
}

// ================================================================
// Error Handling
// ================================================================

func handleProjectServiceError(ctx *gin.Context, err error) {
	errMsg := err.Error()

	switch errMsg {
	case "user not authenticated or org not set":
		ctx.JSON(http.StatusUnauthorized, dto.Error(dto.CodeUnauthorized, errMsg))
		return
	}

	switch errMsg {
	case "project not found":
		ctx.JSON(http.StatusNotFound, dto.Error(dto.CodeNotFound, errMsg))
	case "name is required",
		"name cannot be empty",
		"public_id is required",
		"project_id is required",
		"invalid cursor",
		"task does not belong to project":
		ctx.JSON(http.StatusBadRequest, dto.Error(dto.CodeInvalidParams, errMsg))
	case "task not found":
		ctx.JSON(http.StatusNotFound, dto.Error(dto.CodeNotFound, errMsg))
	default:
		if isPermissionDenied(err) {
			ctx.JSON(http.StatusForbidden, dto.Error(dto.CodeForbidden, errMsg))
			return
		}
		ctx.JSON(http.StatusInternalServerError, dto.Error(dto.CodeInternalError, errMsg))
	}
}
