package handler

import (
	"errors"
	"net/http"
	"strconv"

	"github.com/gin-gonic/gin"

	"github.com/insmtx/Leros/backend/internal/api/auth"
	"github.com/insmtx/Leros/backend/internal/api/dto"
	"github.com/insmtx/Leros/backend/internal/service"
)

// WorkerOpsHandler 处理 Worker 运维状态查询端点。
type WorkerOpsHandler struct {
	svc *service.WorkerOpsService
}

// NewWorkerOpsHandler 创建 Worker 运维 handler。
func NewWorkerOpsHandler(svc *service.WorkerOpsService) *WorkerOpsHandler {
	return &WorkerOpsHandler{svc: svc}
}

// RegisterWorkerOpsRoutes 注册 Worker 运维查询路由。
func RegisterWorkerOpsRoutes(r gin.IRouter, svc *service.WorkerOpsService) {
	h := NewWorkerOpsHandler(svc)
	r.GET("/ops/workers/status", h.WorkerStatus)
}

// @Summary 查询 Worker 状态
// @Description 根据 orgid 和 workerid 查询指定 Worker 的本地运行状态快照。
// @Tags Ops
// @Produce json
// @Param orgid query uint true "组织 ID"
// @Param workerid query uint true "Worker ID"
// @Success 200 {object} dto.Response "Worker 状态快照"
// @Failure 400 {object} dto.ErrorResponse "请求参数错误"
// @Failure 401 {object} dto.ErrorResponse "未认证"
// @Failure 403 {object} dto.ErrorResponse "组织不匹配"
// @Failure 502 {object} dto.ErrorResponse "Worker 响应格式错误"
// @Failure 503 {object} dto.ErrorResponse "NATS 不可用"
// @Failure 504 {object} dto.ErrorResponse "Worker 响应超时"
// @Router /ops/workers/status [get]
func (h *WorkerOpsHandler) WorkerStatus(ctx *gin.Context) {
	caller, _ := auth.FromGinContext(ctx)
	if caller == nil || caller.OrgID == 0 {
		ctx.JSON(http.StatusUnauthorized, dto.Error(dto.CodeUnauthorized, "organization identity is required"))
		return
	}

	orgID, ok := parsePositiveUint(ctx, "orgid")
	if !ok {
		return
	}
	workerID, ok := parsePositiveUint(ctx, "workerid")
	if !ok {
		return
	}

	// 组织归属校验：调用方必须属于被查询的 org。
	if caller.OrgID != orgID {
		ctx.JSON(http.StatusForbidden, dto.Error(dto.CodeForbidden, "organization mismatch"))
		return
	}

	snapshot, err := h.svc.QueryWorkerStatus(ctx, orgID, workerID)
	if err != nil {
		handleWorkerStatusError(ctx, err)
		return
	}
	ctx.JSON(http.StatusOK, dto.Success(snapshot))
}

// parsePositiveUint 解析并校验正整数查询参数；失败时写入 400 并返回 false。
func parsePositiveUint(ctx *gin.Context, name string) (uint, bool) {
	raw := ctx.Query(name)
	if raw == "" {
		ctx.JSON(http.StatusBadRequest, dto.Error(dto.CodeInvalidParams, name+" is required"))
		return 0, false
	}
	val, err := strconv.ParseUint(raw, 10, 64)
	if err != nil || val == 0 {
		ctx.JSON(http.StatusBadRequest, dto.Error(dto.CodeInvalidParams, name+" must be a positive integer"))
		return 0, false
	}
	return uint(val), true
}

// handleWorkerStatusError 将服务层错误映射为 HTTP 状态码。
//   - 超时（未回复）→ 504 Gateway Timeout
//   - NATS 不可用 → 503 Service Unavailable
//   - 响应无法解析 → 502 Bad Gateway
func handleWorkerStatusError(ctx *gin.Context, err error) {
	switch {
	case errors.Is(err, service.ErrWorkerTimeout):
		ctx.JSON(http.StatusGatewayTimeout, dto.Error(dto.CodeInternalError, "worker status query timed out"))
	case errors.Is(err, service.ErrWorkerUnavailable):
		ctx.JSON(http.StatusServiceUnavailable, dto.Error(dto.CodeInternalError, "worker status query unavailable"))
	case errors.Is(err, service.ErrWorkerBadResponse):
		ctx.JSON(http.StatusBadGateway, dto.Error(dto.CodeInternalError, "worker status query bad response"))
	default:
		ctx.JSON(http.StatusInternalServerError, dto.Error(dto.CodeInternalError, "worker status query failed"))
	}
}
