package handler

import (
	"net/http"

	"github.com/gin-gonic/gin"

	"github.com/insmtx/Leros/backend/internal/api/contract"
	"github.com/insmtx/Leros/backend/internal/api/dto"
)

type ProjectHandler struct {
	service contract.ProjectService
}

func NewProjectHandler(service contract.ProjectService) *ProjectHandler {
	return &ProjectHandler{service: service}
}

// ================================================================
// Route Registration
// ================================================================

func (h *ProjectHandler) RegisterRoutes(r gin.IRouter) {
	r.POST("/CreateProject", h.CreateProject)
	r.POST("/GetProject", h.GetProject)
	r.POST("/UpdateProject", h.UpdateProject)
	r.POST("/DeleteProject", h.DeleteProject)
	r.POST("/ListProjects", h.ListProjects)
}

func RegisterProjectRoutes(r gin.IRouter, service contract.ProjectService) {
	h := NewProjectHandler(service)
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

	result, err := h.service.GetProject(ctx, *req.PublicID)
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

	req.Pagination.Fill()

	result, err := h.service.ListProjects(ctx, &req)
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
		ctx.JSON(http.StatusUnauthorized, dto.Error(dto.CodeInternalError, errMsg))
		return
	}

	switch errMsg {
	case "project not found":
		ctx.JSON(http.StatusNotFound, dto.Error(dto.CodeNotFound, errMsg))
	case "name is required",
		"name cannot be empty",
		"public_id is required":
		ctx.JSON(http.StatusBadRequest, dto.Error(dto.CodeInvalidParams, errMsg))
	default:
		ctx.JSON(http.StatusInternalServerError, dto.Error(dto.CodeInternalError, errMsg))
	}
}
