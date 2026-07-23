package handler

import (
	"net/http"
	"strings"

	"github.com/gin-gonic/gin"

	"github.com/insmtx/Leros/backend/internal/api/dto"
	"github.com/insmtx/Leros/backend/internal/api/middleware"
)

const headerWorkerBootstrapToken = "X-Worker-Bootstrap-Token"

// WorkerAuthHandler exchanges worker bootstrap tokens for short-lived
// access tokens by delegating to a TokenParser.
type WorkerAuthHandler struct {
	parser middleware.TokenParser
}

type issueWorkerTokenRequest struct {
	OrgID          uint   `json:"org_id" binding:"required"`
	WorkerID       uint   `json:"worker_id" binding:"required"`
	BootstrapToken string `json:"bootstrap_token,omitempty"`
}

type issueWorkerTokenResponse struct {
	AuthToken string `json:"auth_token"`
	ExpiredAt int64  `json:"expired_at"`
	TokenType string `json:"token_type"`
}

// NewWorkerAuthHandler creates a worker auth handler backed by the given
// TokenParser.
func NewWorkerAuthHandler(parser middleware.TokenParser) *WorkerAuthHandler {
	return &WorkerAuthHandler{parser: parser}
}

// RegisterWorkerAuthRoutes registers worker auth routes.
func RegisterWorkerAuthRoutes(r gin.IRouter, parser middleware.TokenParser) {
	h := NewWorkerAuthHandler(parser)
	r.POST("/workers/token", h.IssueToken)
}

// @Summary 获取 Worker 访问令牌
// @Description 使用 Worker 启动令牌（bootstrap token）换取短期访问令牌
// @Tags WorkerAuth
// @Accept json
// @Produce json
// @Param body body handler.issueWorkerTokenRequest true "Worker 令牌请求"
// @Success 200 {object} dto.Response "成功响应"
// @Failure 400 {object} dto.ErrorResponse "请求参数错误"
// @Failure 401 {object} dto.ErrorResponse "认证失败"
// @Failure 500 {object} dto.ErrorResponse "内部服务器错误"
// @Router /workers/token [post]
func (h *WorkerAuthHandler) IssueToken(ctx *gin.Context) {
	var req issueWorkerTokenRequest
	if err := ctx.ShouldBindJSON(&req); err != nil {
		ctx.JSON(http.StatusBadRequest, dto.Error(dto.CodeInvalidParams, err.Error()))
		return
	}

	bootstrapToken := strings.TrimSpace(ctx.GetHeader(headerWorkerBootstrapToken))
	if bootstrapToken == "" {
		bootstrapToken = strings.TrimSpace(req.BootstrapToken)
	}
	if bootstrapToken == "" {
		ctx.JSON(http.StatusUnauthorized, dto.Error(dto.CodeInternalError, "worker bootstrap token is required"))
		return
	}

	token, expiredAt, err := h.parser.IssueWorker(ctx.Request.Context(), req.OrgID, req.WorkerID, bootstrapToken)
	if err != nil {
		ctx.JSON(http.StatusUnauthorized, dto.Error(dto.CodeInternalError, err.Error()))
		return
	}

	ctx.JSON(http.StatusOK, dto.Success(issueWorkerTokenResponse{
		AuthToken: token,
		ExpiredAt: expiredAt,
		TokenType: "Bearer",
	}))
}
