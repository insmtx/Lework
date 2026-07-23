package handler

import (
	"errors"
	"net/http"

	"github.com/gin-gonic/gin"

	"github.com/insmtx/Leros/backend/internal/adapter/account"
	"github.com/insmtx/Leros/backend/internal/api/contract"
	"github.com/insmtx/Leros/backend/internal/api/dto"
	"github.com/insmtx/Leros/backend/pkg/accounterror"
)

type UserHandler struct {
	service account.UserRepository
}

func NewUserHandler(service account.UserRepository) *UserHandler {
	return &UserHandler{service: service}
}

func (h *UserHandler) RegisterRoutes(r gin.IRouter) {
	r.POST("/CreateUser", h.CreateUser)
	r.POST("/GetUser", h.GetUser)
	r.POST("/UpdateUser", h.UpdateUser)
	r.POST("/DeleteUser", h.DeleteUser)
	r.POST("/ListUser", h.ListUser)
	r.POST("/ListUin", h.ListUin)
}

func RegisterUserRoutes(r gin.IRouter, service account.UserRepository) {
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

	result, err := h.service.CreateUser(ctx, &req.CreateUserInput)
	if err != nil {
		handleUserServiceError(ctx, err)
		return
	}
	ctx.JSON(http.StatusOK, dto.Success(result))
}

type GetUserRequest struct {
	PublicID string `json:"public_id,omitempty"`
	Phone    string `json:"phone,omitempty"`
}

// @Summary 获取用户详情
// @Description 根据PublicID或Phone获取用户详情
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

	result, err := h.service.GetUser(ctx, req.PublicID, req.Phone)
	if err != nil {
		handleUserServiceError(ctx, err)
		return
	}
	ctx.JSON(http.StatusOK, dto.Success(result))
}

type UpdateUserRequest struct {
	PublicID string `json:"public_id" binding:"required"`
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

	result, err := h.service.UpdateUser(ctx, req.PublicID, &req.UpdateUserInput)
	if err != nil {
		handleUserServiceError(ctx, err)
		return
	}
	ctx.JSON(http.StatusOK, dto.Success(result))
}

type DeleteUserRequest struct {
	PublicID string `json:"public_id" binding:"required"`
}

// @Summary 删除用户
// @Description 根据PublicID删除用户（软删除）
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

	if err := h.service.DeleteUser(ctx, req.PublicID); err != nil {
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
// @Param body body contract.ListUserRequest true "查询列表请求"
// @Success 200 {object} dto.Response "成功响应"
// @Failure 400 {object} dto.ErrorResponse "请求参数错误"
// @Failure 401 {object} dto.ErrorResponse "未认证"
// @Failure 500 {object} dto.ErrorResponse "内部服务器错误"
// @Router /ListUser [post]
func (h *UserHandler) ListUser(ctx *gin.Context) {
	var req contract.ListUserRequest
	if err := ctx.ShouldBindJSON(&req); err != nil {
		ctx.JSON(http.StatusBadRequest, dto.Error(dto.CodeInvalidParams, err.Error()))
		return
	}

	req.Fill()

	result, err := h.service.ListUser(ctx, &req.ListUserInput)
	if err != nil {
		handleUserServiceError(ctx, err)
		return
	}
	ctx.JSON(http.StatusOK, dto.Success(result))
}

// @Summary 查询用户身份标识列表
// @Description 获取当前登录用户的所有UIN（用户身份标识）列表，包含个人和企业身份
// @Tags User
// @Accept json
// @Produce json
// @Param body body contract.ListUinRequest true "查询请求（可为空）"
// @Success 200 {object} dto.Response "成功响应"
// @Failure 401 {object} dto.ErrorResponse "未认证"
// @Failure 500 {object} dto.ErrorResponse "内部服务器错误"
// @Router /ListUin [post]
func (h *UserHandler) ListUin(ctx *gin.Context) {
	var req contract.ListUinRequest
	if err := ctx.ShouldBindJSON(&req); err != nil {
		ctx.JSON(http.StatusBadRequest, dto.Error(dto.CodeInvalidParams, err.Error()))
		return
	}

	result, err := h.service.ListUin(ctx)
	if err != nil {
		handleUserServiceError(ctx, err)
		return
	}
	ctx.JSON(http.StatusOK, dto.Success(result))
}

func handleUserServiceError(ctx *gin.Context, err error) {
	if errors.Is(err, accounterror.ErrPhoneOrEmailRequired) {
		ctx.JSON(http.StatusBadRequest, dto.Error(dto.CodeInvalidParams, err.Error()))
		return
	}

	errMsg := err.Error()

	if errMsg == "user not authenticated" {
		ctx.JSON(http.StatusUnauthorized, dto.Error(dto.CodeInternalError, errMsg))
		return
	}

	switch errMsg {
	case "user not found":
		ctx.JSON(http.StatusNotFound, dto.Error(dto.CodeNotFound, errMsg))
	case "name is required",
		"public_id is required",
		"public_id or phone is required":
		ctx.JSON(http.StatusBadRequest, dto.Error(dto.CodeInvalidParams, errMsg))
	default:
		ctx.JSON(http.StatusInternalServerError, dto.Error(dto.CodeInternalError, errMsg))
	}
}
