package handler

import (
	"net/http"

	"github.com/gin-gonic/gin"

	"github.com/insmtx/Leros/backend/internal/api/contract"
	"github.com/insmtx/Leros/backend/internal/api/dto"
)

type AuthHandler struct {
	service contract.AuthService
}

func NewAuthHandler(service contract.AuthService) *AuthHandler {
	return &AuthHandler{service: service}
}

func (h *AuthHandler) RegisterRoutes(r gin.IRouter) {
	r.POST("/RegisterByEmail", h.RegisterByEmail)
	r.POST("/LoginByEmail", h.LoginByEmail)
	r.POST("/RefreshToken", h.RefreshToken)
}

func RegisterAuthRoutes(r gin.IRouter, service contract.AuthService) {
	h := NewAuthHandler(service)
	h.RegisterRoutes(r)
}

// @Summary 邮箱注册
// @Description 使用邮箱和密码注册本地用户
// @Tags Auth
// @Accept json
// @Produce json
// @Param body body contract.RegisterByEmailRequest true "邮箱注册请求"
// @Success 200 {object} dto.Response "成功响应"
// @Failure 400 {object} dto.ErrorResponse "请求参数错误"
// @Failure 500 {object} dto.ErrorResponse "内部服务器错误"
// @Router /RegisterByEmail [post]
func (h *AuthHandler) RegisterByEmail(ctx *gin.Context) {
	var req contract.RegisterByEmailRequest
	if err := ctx.ShouldBindJSON(&req); err != nil {
		ctx.JSON(http.StatusBadRequest, dto.Error(dto.CodeInvalidParams, err.Error()))
		return
	}

	result, err := h.service.RegisterByEmail(ctx, &req)
	if err != nil {
		handleAuthServiceError(ctx, err)
		return
	}
	ctx.JSON(http.StatusOK, dto.Success(result))
}

// @Summary 邮箱登录
// @Description 使用邮箱和密码登录并获取访问令牌
// @Tags Auth
// @Accept json
// @Produce json
// @Param body body contract.LoginByEmailRequest true "邮箱登录请求"
// @Success 200 {object} dto.Response "成功响应"
// @Failure 400 {object} dto.ErrorResponse "请求参数错误"
// @Failure 401 {object} dto.ErrorResponse "认证失败"
// @Failure 429 {object} dto.ErrorResponse "登录失败次数过多"
// @Failure 500 {object} dto.ErrorResponse "内部服务器错误"
// @Router /LoginByEmail [post]
func (h *AuthHandler) LoginByEmail(ctx *gin.Context) {
	var req contract.LoginByEmailRequest
	if err := ctx.ShouldBindJSON(&req); err != nil {
		ctx.JSON(http.StatusBadRequest, dto.Error(dto.CodeInvalidParams, err.Error()))
		return
	}

	result, err := h.service.LoginByEmail(ctx, &req)
	if err != nil {
		handleAuthServiceError(ctx, err)
		return
	}
	ctx.JSON(http.StatusOK, dto.Success(result))
}

// @Summary 刷新令牌
// @Description 使用 refresh token 换取新的访问令牌
// @Tags Auth
// @Accept json
// @Produce json
// @Param body body contract.RefreshTokenRequest true "刷新令牌请求"
// @Success 200 {object} dto.Response "成功响应"
// @Failure 400 {object} dto.ErrorResponse "请求参数错误"
// @Failure 401 {object} dto.ErrorResponse "认证失败"
// @Failure 500 {object} dto.ErrorResponse "内部服务器错误"
// @Router /RefreshToken [post]
func (h *AuthHandler) RefreshToken(ctx *gin.Context) {
	var req contract.RefreshTokenRequest
	if err := ctx.ShouldBindJSON(&req); err != nil {
		ctx.JSON(http.StatusBadRequest, dto.Error(dto.CodeInvalidParams, err.Error()))
		return
	}

	result, err := h.service.RefreshToken(ctx, &req)
	if err != nil {
		handleAuthServiceError(ctx, err)
		return
	}
	ctx.JSON(http.StatusOK, dto.Success(result))
}

func handleAuthServiceError(ctx *gin.Context, err error) {
	errMsg := err.Error()

	switch {
	case isAuthBadRequestError(err):
		ctx.JSON(http.StatusBadRequest, dto.Error(dto.CodeInvalidParams, errMsg))
	case isAuthUnauthorizedError(err):
		ctx.JSON(http.StatusUnauthorized, dto.Error(dto.CodeInternalError, errMsg))
	case errMsg == "登录失败次数过多，请稍后再试":
		ctx.JSON(http.StatusTooManyRequests, dto.Error(dto.CodeInternalError, errMsg))
	default:
		ctx.JSON(http.StatusInternalServerError, dto.Error(dto.CodeInternalError, errMsg))
	}
}

func isAuthBadRequestError(err error) bool {
	switch err.Error() {
	case "请输入邮箱",
		"请输入正确的邮箱",
		"请输入密码",
		"密码不一致",
		"密码长度不能少于8位",
		"密码长度不能超过20位",
		"密码不能包含中文",
		"密码不能包含空格",
		"8-20位，数字/大写字母/小写字母/字符至少3种",
		"该邮箱已注册",
		"刷新令牌不能为空":
		return true
	default:
		return false
	}
}

func isAuthUnauthorizedError(err error) bool {
	switch err.Error() {
	case "邮箱或密码错误",
		"登录已过期，请重新登录",
		"用户不存在":
		return true
	default:
		return false
	}
}
