package handler

import (
	"net/http"

	"github.com/gin-gonic/gin"

	"github.com/insmtx/Leros/backend/internal/api/contract"
	"github.com/insmtx/Leros/backend/internal/api/dto"
)

type OrgHandler struct {
	service contract.OrgService
}

func NewOrgHandler(service contract.OrgService) *OrgHandler {
	return &OrgHandler{service: service}
}

func (h *OrgHandler) RegisterRoutes(r gin.IRouter) {
	r.POST("/CreateOrg", h.CreateOrg)
	r.POST("/GetOrg", h.GetOrg)
	r.POST("/UpdateOrg", h.UpdateOrg)
	r.POST("/DeleteOrg", h.DeleteOrg)
	r.POST("/ListOrgs", h.ListOrgs)
}

func RegisterOrgRoutes(r gin.IRouter, service contract.OrgService) {
	h := NewOrgHandler(service)
	h.RegisterRoutes(r)
}

// @Summary 创建组织
// @Description 创建一个新组织
// @Tags Organization
// @Accept json
// @Produce json
// @Param body body contract.CreateOrgRequest true "创建组织请求"
// @Success 200 {object} dto.Response "成功响应"
// @Failure 400 {object} dto.ErrorResponse "请求参数错误"
// @Failure 401 {object} dto.ErrorResponse "未认证"
// @Failure 500 {object} dto.ErrorResponse "内部服务器错误"
// @Router /CreateOrg [post]
func (h *OrgHandler) CreateOrg(ctx *gin.Context) {
	var req contract.CreateOrgRequest
	if err := ctx.ShouldBindJSON(&req); err != nil {
		ctx.JSON(http.StatusBadRequest, dto.Error(dto.CodeInvalidParams, err.Error()))
		return
	}

	result, err := h.service.CreateOrg(ctx, &req)
	if err != nil {
		handleOrgServiceError(ctx, err)
		return
	}
	ctx.JSON(http.StatusOK, dto.Success(result))
}

type GetOrgRequest struct {
	ID   *uint  `json:"id,omitempty"`
	Code string `json:"code,omitempty"`
}

// @Summary 获取组织详情
// @Description 根据ID或Code获取组织详情
// @Tags Organization
// @Accept json
// @Produce json
// @Param body body GetOrgRequest true "获取组织请求"
// @Success 200 {object} dto.Response "成功响应"
// @Failure 400 {object} dto.ErrorResponse "请求参数错误"
// @Failure 401 {object} dto.ErrorResponse "未认证"
// @Failure 404 {object} dto.ErrorResponse "资源不存在"
// @Failure 500 {object} dto.ErrorResponse "内部服务器错误"
// @Router /GetOrg [post]
func (h *OrgHandler) GetOrg(ctx *gin.Context) {
	var req GetOrgRequest
	if err := ctx.ShouldBindJSON(&req); err != nil {
		ctx.JSON(http.StatusBadRequest, dto.Error(dto.CodeInvalidParams, err.Error()))
		return
	}

	var id uint
	if req.ID != nil {
		id = *req.ID
	}

	result, err := h.service.GetOrg(ctx, id, req.Code)
	if err != nil {
		handleOrgServiceError(ctx, err)
		return
	}
	ctx.JSON(http.StatusOK, dto.Success(result))
}

type UpdateOrgRequest struct {
	ID uint `json:"id" binding:"required"`
	contract.UpdateOrgRequest
}

// @Summary 更新组织
// @Description 更新组织信息
// @Tags Organization
// @Accept json
// @Produce json
// @Param body body UpdateOrgRequest true "更新组织请求"
// @Success 200 {object} dto.Response "成功响应"
// @Failure 400 {object} dto.ErrorResponse "请求参数错误"
// @Failure 401 {object} dto.ErrorResponse "未认证"
// @Failure 404 {object} dto.ErrorResponse "资源不存在"
// @Failure 500 {object} dto.ErrorResponse "内部服务器错误"
// @Router /UpdateOrg [post]
func (h *OrgHandler) UpdateOrg(ctx *gin.Context) {
	var req UpdateOrgRequest
	if err := ctx.ShouldBindJSON(&req); err != nil {
		ctx.JSON(http.StatusBadRequest, dto.Error(dto.CodeInvalidParams, err.Error()))
		return
	}

	result, err := h.service.UpdateOrg(ctx, req.ID, &req.UpdateOrgRequest)
	if err != nil {
		handleOrgServiceError(ctx, err)
		return
	}
	ctx.JSON(http.StatusOK, dto.Success(result))
}

type DeleteOrgRequest struct {
	ID uint `json:"id" binding:"required"`
}

// @Summary 删除组织
// @Description 根据ID删除组织（软删除）
// @Tags Organization
// @Accept json
// @Produce json
// @Param body body DeleteOrgRequest true "删除组织请求"
// @Success 200 {object} dto.Response "成功响应"
// @Failure 400 {object} dto.ErrorResponse "请求参数错误"
// @Failure 401 {object} dto.ErrorResponse "未认证"
// @Failure 404 {object} dto.ErrorResponse "资源不存在"
// @Failure 500 {object} dto.ErrorResponse "内部服务器错误"
// @Router /DeleteOrg [post]
func (h *OrgHandler) DeleteOrg(ctx *gin.Context) {
	var req DeleteOrgRequest
	if err := ctx.ShouldBindJSON(&req); err != nil {
		ctx.JSON(http.StatusBadRequest, dto.Error(dto.CodeInvalidParams, err.Error()))
		return
	}

	if err := h.service.DeleteOrg(ctx, req.ID); err != nil {
		handleOrgServiceError(ctx, err)
		return
	}
	ctx.JSON(http.StatusOK, dto.Success(nil))
}

// @Summary 查询组织列表
// @Description 分页查询组织列表
// @Tags Organization
// @Accept json
// @Produce json
// @Param body body contract.ListOrgsRequest true "查询列表请求"
// @Success 200 {object} dto.Response "成功响应"
// @Failure 400 {object} dto.ErrorResponse "请求参数错误"
// @Failure 401 {object} dto.ErrorResponse "未认证"
// @Failure 500 {object} dto.ErrorResponse "内部服务器错误"
// @Router /ListOrgs [post]
func (h *OrgHandler) ListOrgs(ctx *gin.Context) {
	var req contract.ListOrgsRequest
	if err := ctx.ShouldBindJSON(&req); err != nil {
		ctx.JSON(http.StatusBadRequest, dto.Error(dto.CodeInvalidParams, err.Error()))
		return
	}

	req.Fill()

	result, err := h.service.ListOrgs(ctx, &req)
	if err != nil {
		handleOrgServiceError(ctx, err)
		return
	}
	ctx.JSON(http.StatusOK, dto.Success(result))
}

func handleOrgServiceError(ctx *gin.Context, err error) {
	errMsg := err.Error()

	if errMsg == "user not authenticated" {
		ctx.JSON(http.StatusUnauthorized, dto.Error(dto.CodeInternalError, errMsg))
		return
	}

	switch errMsg {
	case "org not found":
		ctx.JSON(http.StatusNotFound, dto.Error(dto.CodeNotFound, errMsg))
	case "name is required",
		"code is required",
		"name cannot be empty",
		"id is required",
		"id or code is required",
		"org code already exists":
		ctx.JSON(http.StatusBadRequest, dto.Error(dto.CodeInvalidParams, errMsg))
	default:
		ctx.JSON(http.StatusInternalServerError, dto.Error(dto.CodeInternalError, errMsg))
	}
}
