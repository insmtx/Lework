package handler

import (
	"net/http"

	"github.com/gin-gonic/gin"

	"github.com/insmtx/Leros/backend/internal/api/contract"
	"github.com/insmtx/Leros/backend/internal/api/dto"
)

type UserHandler struct {
	service contract.UserService
}

func NewUserHandler(service contract.UserService) *UserHandler {
	return &UserHandler{service: service}
}

func (h *UserHandler) RegisterRoutes(r gin.IRouter) {
	r.POST("/CreateUser", h.CreateUser)
	r.POST("/GetUser", h.GetUser)
	r.POST("/UpdateUser", h.UpdateUser)
	r.POST("/DeleteUser", h.DeleteUser)
	r.POST("/ListUsers", h.ListUsers)
}

func RegisterUserRoutes(r gin.IRouter, service contract.UserService) {
	h := NewUserHandler(service)
	h.RegisterRoutes(r)
}

// @Summary 创建用户
// @Description 创建一个新用户
// @Tags User
// @Accept json
// @Produce json
// @Param body body contract.CreateUserRequest true "创建用户请求"
// @Success 200 {object} dto.Response "成功响应"
// @Failure 400 {object} dto.ErrorResponse "请求参数错误"
// @Failure 401 {object} dto.ErrorResponse "未认证"
// @Failure 500 {object} dto.ErrorResponse "内部服务器错误"
// @Router /CreateUser [post]
func (h *UserHandler) CreateUser(ctx *gin.Context) {
	var req contract.CreateUserRequest
	if err := ctx.ShouldBindJSON(&req); err != nil {
		ctx.JSON(http.StatusBadRequest, dto.Error(dto.CodeInvalidParams, err.Error()))
		return
	}

	result, err := h.service.CreateUser(ctx, &req)
	if err != nil {
		handleUserServiceError(ctx, err)
		return
	}
	ctx.JSON(http.StatusOK, dto.Success(result))
}

type GetUserRequest struct {
	ID          *uint  `json:"id,omitempty"`
	GithubLogin string `json:"github_login,omitempty"`
}

// @Summary 获取用户详情
// @Description 根据ID或GithubLogin获取用户详情
// @Tags User
// @Accept json
// @Produce json
// @Param body body GetUserRequest true "获取用户请求"
// @Success 200 {object} dto.Response "成功响应"
// @Failure 400 {object} dto.ErrorResponse "请求参数错误"
// @Failure 401 {object} dto.ErrorResponse "未认证"
// @Failure 404 {object} dto.ErrorResponse "资源不存在"
// @Failure 500 {object} dto.ErrorResponse "内部服务器错误"
// @Router /GetUser [post]
func (h *UserHandler) GetUser(ctx *gin.Context) {
	var req GetUserRequest
	if err := ctx.ShouldBindJSON(&req); err != nil {
		ctx.JSON(http.StatusBadRequest, dto.Error(dto.CodeInvalidParams, err.Error()))
		return
	}

	var id uint
	if req.ID != nil {
		id = *req.ID
	}

	result, err := h.service.GetUser(ctx, id, req.GithubLogin)
	if err != nil {
		handleUserServiceError(ctx, err)
		return
	}
	ctx.JSON(http.StatusOK, dto.Success(result))
}

type UpdateUserRequest struct {
	ID uint `json:"id" binding:"required"`
	contract.UpdateUserRequest
}

// @Summary 更新用户
// @Description 更新用户信息
// @Tags User
// @Accept json
// @Produce json
// @Param body body UpdateUserRequest true "更新用户请求"
// @Success 200 {object} dto.Response "成功响应"
// @Failure 400 {object} dto.ErrorResponse "请求参数错误"
// @Failure 401 {object} dto.ErrorResponse "未认证"
// @Failure 404 {object} dto.ErrorResponse "资源不存在"
// @Failure 500 {object} dto.ErrorResponse "内部服务器错误"
// @Router /UpdateUser [post]
func (h *UserHandler) UpdateUser(ctx *gin.Context) {
	var req UpdateUserRequest
	if err := ctx.ShouldBindJSON(&req); err != nil {
		ctx.JSON(http.StatusBadRequest, dto.Error(dto.CodeInvalidParams, err.Error()))
		return
	}

	result, err := h.service.UpdateUser(ctx, req.ID, &req.UpdateUserRequest)
	if err != nil {
		handleUserServiceError(ctx, err)
		return
	}
	ctx.JSON(http.StatusOK, dto.Success(result))
}

type DeleteUserRequest struct {
	ID uint `json:"id" binding:"required"`
}

// @Summary 删除用户
// @Description 根据ID删除用户（软删除）
// @Tags User
// @Accept json
// @Produce json
// @Param body body DeleteUserRequest true "删除用户请求"
// @Success 200 {object} dto.Response "成功响应"
// @Failure 400 {object} dto.ErrorResponse "请求参数错误"
// @Failure 401 {object} dto.ErrorResponse "未认证"
// @Failure 404 {object} dto.ErrorResponse "资源不存在"
// @Failure 500 {object} dto.ErrorResponse "内部服务器错误"
// @Router /DeleteUser [post]
func (h *UserHandler) DeleteUser(ctx *gin.Context) {
	var req DeleteUserRequest
	if err := ctx.ShouldBindJSON(&req); err != nil {
		ctx.JSON(http.StatusBadRequest, dto.Error(dto.CodeInvalidParams, err.Error()))
		return
	}

	if err := h.service.DeleteUser(ctx, req.ID); err != nil {
		handleUserServiceError(ctx, err)
		return
	}
	ctx.JSON(http.StatusOK, dto.Success(nil))
}

// @Summary 查询用户列表
// @Description 分页查询用户列表
// @Tags User
// @Accept json
// @Produce json
// @Param body body contract.ListUsersRequest true "查询列表请求"
// @Success 200 {object} dto.Response "成功响应"
// @Failure 400 {object} dto.ErrorResponse "请求参数错误"
// @Failure 401 {object} dto.ErrorResponse "未认证"
// @Failure 500 {object} dto.ErrorResponse "内部服务器错误"
// @Router /ListUsers [post]
func (h *UserHandler) ListUsers(ctx *gin.Context) {
	var req contract.ListUsersRequest
	if err := ctx.ShouldBindJSON(&req); err != nil {
		ctx.JSON(http.StatusBadRequest, dto.Error(dto.CodeInvalidParams, err.Error()))
		return
	}

	req.Fill()

	result, err := h.service.ListUsers(ctx, &req)
	if err != nil {
		handleUserServiceError(ctx, err)
		return
	}
	ctx.JSON(http.StatusOK, dto.Success(result))
}

func handleUserServiceError(ctx *gin.Context, err error) {
	errMsg := err.Error()

	if errMsg == "user not authenticated" {
		ctx.JSON(http.StatusUnauthorized, dto.Error(dto.CodeInternalError, errMsg))
		return
	}

	switch errMsg {
	case "user not found":
		ctx.JSON(http.StatusNotFound, dto.Error(dto.CodeNotFound, errMsg))
	case "name is required",
		"github_login is required",
		"github_login already exists",
		"id is required",
		"id or github_login is required":
		ctx.JSON(http.StatusBadRequest, dto.Error(dto.CodeInvalidParams, errMsg))
	default:
		ctx.JSON(http.StatusInternalServerError, dto.Error(dto.CodeInternalError, errMsg))
	}
}
