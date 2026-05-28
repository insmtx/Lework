package handler

import (
	"net/http"

	"github.com/gin-gonic/gin"

	"github.com/insmtx/Leros/backend/internal/api/contract"
	"github.com/insmtx/Leros/backend/internal/api/dto"
)

type UserOrgHandler struct {
	service contract.UserOrgService
}

func NewUserOrgHandler(service contract.UserOrgService) *UserOrgHandler {
	return &UserOrgHandler{service: service}
}

func (h *UserOrgHandler) RegisterRoutes(r gin.IRouter) {
	r.POST("/CreateUserOrg", h.CreateUserOrg)
	r.POST("/GetUserOrg", h.GetUserOrg)
	r.POST("/UpdateUserOrg", h.UpdateUserOrg)
	r.POST("/DeleteUserOrg", h.DeleteUserOrg)
	r.POST("/ListUserOrgs", h.ListUserOrgs)
}

func RegisterUserOrgRoutes(r gin.IRouter, service contract.UserOrgService) {
	h := NewUserOrgHandler(service)
	h.RegisterRoutes(r)
}

// @Summary 创建用户组织关联
// @Description 创建用户与组织的关联关系
// @Tags UserOrg
// @Accept json
// @Produce json
// @Param body body contract.CreateUserOrgRequest true "创建关联请求"
// @Success 200 {object} dto.Response "成功响应"
// @Failure 400 {object} dto.ErrorResponse "请求参数错误"
// @Failure 401 {object} dto.ErrorResponse "未认证"
// @Failure 500 {object} dto.ErrorResponse "内部服务器错误"
// @Router /CreateUserOrg [post]
func (h *UserOrgHandler) CreateUserOrg(ctx *gin.Context) {
	var req contract.CreateUserOrgRequest
	if err := ctx.ShouldBindJSON(&req); err != nil {
		ctx.JSON(http.StatusBadRequest, dto.Error(dto.CodeInvalidParams, err.Error()))
		return
	}

	result, err := h.service.CreateUserOrg(ctx, &req)
	if err != nil {
		handleUserOrgServiceError(ctx, err)
		return
	}
	ctx.JSON(http.StatusOK, dto.Success(result))
}

type GetUserOrgRequest struct {
	ID  *uint `json:"id,omitempty"`
	Uin *uint `json:"uin,omitempty"`
}

// @Summary 获取用户组织关联
// @Description 根据ID或Uin获取用户组织关联详情
// @Tags UserOrg
// @Accept json
// @Produce json
// @Param body body GetUserOrgRequest true "获取关联请求"
// @Success 200 {object} dto.Response "成功响应"
// @Failure 400 {object} dto.ErrorResponse "请求参数错误"
// @Failure 401 {object} dto.ErrorResponse "未认证"
// @Failure 404 {object} dto.ErrorResponse "资源不存在"
// @Failure 500 {object} dto.ErrorResponse "内部服务器错误"
// @Router /GetUserOrg [post]
func (h *UserOrgHandler) GetUserOrg(ctx *gin.Context) {
	var req GetUserOrgRequest
	if err := ctx.ShouldBindJSON(&req); err != nil {
		ctx.JSON(http.StatusBadRequest, dto.Error(dto.CodeInvalidParams, err.Error()))
		return
	}

	var id, uin uint
	if req.ID != nil {
		id = *req.ID
	}
	if req.Uin != nil {
		uin = *req.Uin
	}

	result, err := h.service.GetUserOrg(ctx, id, uin)
	if err != nil {
		handleUserOrgServiceError(ctx, err)
		return
	}
	ctx.JSON(http.StatusOK, dto.Success(result))
}

type UpdateUserOrgRequest struct {
	ID uint `json:"id" binding:"required"`
	contract.UpdateUserOrgRequest
}

// @Summary 更新用户组织关联
// @Description 更新用户与组织的关联信息
// @Tags UserOrg
// @Accept json
// @Produce json
// @Param body body UpdateUserOrgRequest true "更新关联请求"
// @Success 200 {object} dto.Response "成功响应"
// @Failure 400 {object} dto.ErrorResponse "请求参数错误"
// @Failure 401 {object} dto.ErrorResponse "未认证"
// @Failure 404 {object} dto.ErrorResponse "资源不存在"
// @Failure 500 {object} dto.ErrorResponse "内部服务器错误"
// @Router /UpdateUserOrg [post]
func (h *UserOrgHandler) UpdateUserOrg(ctx *gin.Context) {
	var req UpdateUserOrgRequest
	if err := ctx.ShouldBindJSON(&req); err != nil {
		ctx.JSON(http.StatusBadRequest, dto.Error(dto.CodeInvalidParams, err.Error()))
		return
	}

	result, err := h.service.UpdateUserOrg(ctx, req.ID, &req.UpdateUserOrgRequest)
	if err != nil {
		handleUserOrgServiceError(ctx, err)
		return
	}
	ctx.JSON(http.StatusOK, dto.Success(result))
}

type DeleteUserOrgRequest struct {
	ID uint `json:"id" binding:"required"`
}

// @Summary 删除用户组织关联
// @Description 根据ID删除用户与组织的关联
// @Tags UserOrg
// @Accept json
// @Produce json
// @Param body body DeleteUserOrgRequest true "删除关联请求"
// @Success 200 {object} dto.Response "成功响应"
// @Failure 400 {object} dto.ErrorResponse "请求参数错误"
// @Failure 401 {object} dto.ErrorResponse "未认证"
// @Failure 404 {object} dto.ErrorResponse "资源不存在"
// @Failure 500 {object} dto.ErrorResponse "内部服务器错误"
// @Router /DeleteUserOrg [post]
func (h *UserOrgHandler) DeleteUserOrg(ctx *gin.Context) {
	var req DeleteUserOrgRequest
	if err := ctx.ShouldBindJSON(&req); err != nil {
		ctx.JSON(http.StatusBadRequest, dto.Error(dto.CodeInvalidParams, err.Error()))
		return
	}

	if err := h.service.DeleteUserOrg(ctx, req.ID); err != nil {
		handleUserOrgServiceError(ctx, err)
		return
	}
	ctx.JSON(http.StatusOK, dto.Success(nil))
}

// @Summary 查询用户组织关联列表
// @Description 分页查询用户组织关联列表
// @Tags UserOrg
// @Accept json
// @Produce json
// @Param body body contract.ListUserOrgsRequest true "查询列表请求"
// @Success 200 {object} dto.Response "成功响应"
// @Failure 400 {object} dto.ErrorResponse "请求参数错误"
// @Failure 401 {object} dto.ErrorResponse "未认证"
// @Failure 500 {object} dto.ErrorResponse "内部服务器错误"
// @Router /ListUserOrgs [post]
func (h *UserOrgHandler) ListUserOrgs(ctx *gin.Context) {
	var req contract.ListUserOrgsRequest
	if err := ctx.ShouldBindJSON(&req); err != nil {
		ctx.JSON(http.StatusBadRequest, dto.Error(dto.CodeInvalidParams, err.Error()))
		return
	}

	req.Fill()

	result, err := h.service.ListUserOrgs(ctx, &req)
	if err != nil {
		handleUserOrgServiceError(ctx, err)
		return
	}
	ctx.JSON(http.StatusOK, dto.Success(result))
}

func handleUserOrgServiceError(ctx *gin.Context, err error) {
	errMsg := err.Error()

	if errMsg == "user not authenticated" {
		ctx.JSON(http.StatusUnauthorized, dto.Error(dto.CodeInternalError, errMsg))
		return
	}

	switch errMsg {
	case "user org not found":
		ctx.JSON(http.StatusNotFound, dto.Error(dto.CodeNotFound, errMsg))
	case "user_id is required",
		"org_id is required",
		"id is required",
		"id or uin is required",
		"user not found",
		"org not found",
		"user org association already exists":
		ctx.JSON(http.StatusBadRequest, dto.Error(dto.CodeInvalidParams, errMsg))
	default:
		ctx.JSON(http.StatusInternalServerError, dto.Error(dto.CodeInternalError, errMsg))
	}
}
