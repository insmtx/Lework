package handler

import (
	"context"
	"net/http"
	"strings"

	"github.com/gin-gonic/gin"

	localauth "github.com/insmtx/Leros/backend/internal/api/auth"
	"github.com/insmtx/Leros/backend/internal/api/dto"
	"github.com/insmtx/Leros/backend/types"
)

// PermissionBatchChecker 批量权限检查能力，由 api 层适配器注入，避免 handler 直接依赖 service 包。
type PermissionBatchChecker interface {
	BatchCheckByPublicID(
		ctx context.Context,
		caller types.PermissionCaller,
		items []PermissionBatchCheckItem,
	) ([]PermissionBatchCheckResult, error)
}

// PermissionBatchCheckItem 描述单次基于 public_id 的权限检查请求。
type PermissionBatchCheckItem struct {
	Action       string
	ResourceType types.ResourceType
	PublicID     string
}

// PermissionBatchCheckResult 是单次权限检查的返回结果。
type PermissionBatchCheckResult struct {
	Action       string
	ResourceType types.ResourceType
	PublicID     string
	Allowed      bool
	Reason       string
	Role         string
	Inherited    bool
}

// PermissionHandler 暴露权限查询 API，供前端批量判断按钮可见性。
type PermissionHandler struct {
	checker PermissionBatchChecker
}

// NewPermissionHandler 创建权限 handler。
func NewPermissionHandler(checker PermissionBatchChecker) *PermissionHandler {
	return &PermissionHandler{checker: checker}
}

type batchCheckPermissionRequest struct {
	Items []batchCheckPermissionItem `json:"items" binding:"required"`
}

type batchCheckPermissionItem struct {
	Action   string `json:"action" binding:"required"`
	Resource struct {
		Type     string `json:"type" binding:"required"`
		PublicID string `json:"public_id" binding:"required"`
	} `json:"resource" binding:"required"`
}

type batchCheckPermissionResult struct {
	Action   string `json:"action"`
	Resource struct {
		Type     string `json:"type"`
		PublicID string `json:"public_id"`
	} `json:"resource"`
	Allowed   bool   `json:"allowed"`
	Reason    string `json:"reason,omitempty"`
	Role      string `json:"role,omitempty"`
	Inherited bool   `json:"inherited,omitempty"`
}

// RegisterRoutes 注册权限相关路由。
func (h *PermissionHandler) RegisterRoutes(r gin.IRouter) {
	r.POST("/BatchCheckPermission", h.BatchCheckPermission)
}

// @Summary 批量检查权限
// @Description 批量检查当前用户对多个资源/动作的权限（用于前端控制按钮可见性）
// @Tags Permission
// @Accept json
// @Produce json
// @Param body body handler.batchCheckPermissionRequest true "批量权限检查请求"
// @Success 200 {object} dto.Response "成功响应"
// @Failure 400 {object} dto.ErrorResponse "请求参数错误"
// @Failure 401 {object} dto.ErrorResponse "未认证"
// @Failure 500 {object} dto.ErrorResponse "内部服务器错误"
// @Router /BatchCheckPermission [post]
func (h *PermissionHandler) BatchCheckPermission(ctx *gin.Context) {
	var req batchCheckPermissionRequest
	if err := ctx.ShouldBindJSON(&req); err != nil {
		ctx.JSON(http.StatusBadRequest, dto.Error(dto.CodeInvalidParams, err.Error()))
		return
	}
	if len(req.Items) == 0 {
		ctx.JSON(http.StatusOK, dto.Success([]batchCheckPermissionResult{}))
		return
	}

	caller, _ := localauth.FromGinContext(ctx)
	if caller == nil || caller.OrgID == 0 || caller.Uin == 0 {
		ctx.JSON(http.StatusUnauthorized, dto.Error(dto.CodeUnauthorized, "unauthorized"))
		return
	}

	items := make([]PermissionBatchCheckItem, len(req.Items))
	for i, item := range req.Items {
		resourceType := types.ResourceType(strings.TrimSpace(item.Resource.Type))
		if resourceType == "" {
			ctx.JSON(http.StatusBadRequest, dto.Error(dto.CodeInvalidParams, "resource.type is required"))
			return
		}
		items[i] = PermissionBatchCheckItem{
			Action:       strings.TrimSpace(item.Action),
			ResourceType: resourceType,
			PublicID:     strings.TrimSpace(item.Resource.PublicID),
		}
	}

	results, err := h.checker.BatchCheckByPublicID(ctx, types.PermissionCaller{
		OrgID: caller.OrgID,
		Uin:   caller.Uin,
	}, items)
	if err != nil {
		ctx.JSON(http.StatusInternalServerError, dto.Error(dto.CodeInternalError, err.Error()))
		return
	}

	resp := make([]batchCheckPermissionResult, len(results))
	for i, r := range results {
		resp[i].Action = r.Action
		resp[i].Resource.Type = string(r.ResourceType)
		resp[i].Resource.PublicID = r.PublicID
		resp[i].Allowed = r.Allowed
		resp[i].Reason = r.Reason
		resp[i].Role = r.Role
		resp[i].Inherited = r.Inherited
	}
	ctx.JSON(http.StatusOK, dto.Success(resp))
}

// RegisterPermissionRoutes 注册权限路由。
func RegisterPermissionRoutes(r gin.IRouter, checker PermissionBatchChecker) {
	h := NewPermissionHandler(checker)
	h.RegisterRoutes(r)
}
