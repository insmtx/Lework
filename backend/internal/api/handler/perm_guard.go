package handler

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strings"

	"github.com/gin-gonic/gin"

	localauth "github.com/insmtx/Lework/backend/internal/api/auth"
	"github.com/insmtx/Lework/backend/internal/api/dto"
	"github.com/insmtx/Lework/backend/types"
)

// PermGuarder 是 handler 层对权限校验能力的依赖接口。
// 由 *service.PermissionService 实现，通过构造函数注入。
// 使用接口而非具体类型，避免 handler → service → handler（测试）的导入环。
type PermGuarder interface {
	GuardProject(ctx context.Context, caller types.PermissionCaller, publicID string, actions ...types.Action) error
	GuardTask(ctx context.Context, caller types.PermissionCaller, publicID string, actions ...types.Action) error
	GuardProjectTaskAction(ctx context.Context, caller types.PermissionCaller, projectPublicID string, action types.Action) error
	GuardByPublicID(ctx context.Context, caller types.PermissionCaller, resourceType types.ResourceType, publicID string, actions ...types.Action) error
	GuardSessionAccess(ctx context.Context, caller types.PermissionCaller, sessionPublicID string) error
	GuardSessionAccessByMessageID(ctx context.Context, caller types.PermissionCaller, messageID uint) error
	GuardNewMessageRequest(ctx context.Context, caller types.PermissionCaller, projectID, taskID string) error
}

// toPermCaller 将 *types.Caller 转换为权限服务所需的 PermissionCaller。
func toPermCaller(c *types.Caller) types.PermissionCaller {
	if c == nil {
		return types.PermissionCaller{}
	}
	pc := types.PermissionCaller{
		OrgID: c.OrgID,
		Uin:   c.Uin,
	}
	if c.Kind == types.CallerKindWorker && c.WorkerID != 0 {
		pc.AssistantID = c.WorkerID
	}
	return pc
}

// PublicIDExtractor 从原始请求 body 字节中提取资源 public_id。
// 由调用方根据具体请求结构提供；返回的 string 为资源的 public_id，
// error 表示提取失败（通常是 body 格式不合法或字段缺失）。
type PublicIDExtractor func(body []byte) (string, error)

// PermGuard 返回一个声明式权限校验中间件（单 action），可直接挂在路由链上。
func PermGuard(
	permSvc PermGuarder,
	resourceType types.ResourceType,
	action types.Action,
	extractPublicID PublicIDExtractor,
) gin.HandlerFunc {
	return PermGuardActions(permSvc, resourceType, extractPublicID, action)
}

// PermGuardActions 与 PermGuard 相同，但支持对同一资源校验多个 action。
func PermGuardActions(
	permSvc PermGuarder,
	resourceType types.ResourceType,
	extractPublicID PublicIDExtractor,
	actions ...types.Action,
) gin.HandlerFunc {
	return func(ctx *gin.Context) {
		bodyBytes, err := io.ReadAll(ctx.Request.Body)
		_ = ctx.Request.Body.Close()
		if err != nil {
			ctx.AbortWithStatusJSON(http.StatusInternalServerError,
				dto.Error(dto.CodeInternalError, "failed to read request body"))
			return
		}
		ctx.Request.Body = io.NopCloser(bytes.NewReader(bodyBytes))

		publicID, err := extractPublicID(bodyBytes)
		if err != nil {
			ctx.AbortWithStatusJSON(http.StatusBadRequest,
				dto.Error(dto.CodeInvalidParams, err.Error()))
			return
		}

		caller, _ := localauth.FromGinContext(ctx)
		if err := permSvc.GuardByPublicID(ctx, toPermCaller(caller), resourceType, publicID, actions...); err != nil {
			abortPermissionDenied(ctx, err)
			return
		}

		ctx.Next()
	}
}

// PermGuardPath 从 URL path 参数提取 public_id 并校验权限，适用于 REST 风格路由。
func PermGuardPath(
	permSvc PermGuarder,
	resourceType types.ResourceType,
	paramName string,
	actions ...types.Action,
) gin.HandlerFunc {
	return func(ctx *gin.Context) {
		publicID := strings.TrimSpace(ctx.Param(paramName))
		if publicID == "" {
			ctx.AbortWithStatusJSON(http.StatusBadRequest,
				dto.Error(dto.CodeInvalidParams, paramName+" is required"))
			return
		}

		caller, _ := localauth.FromGinContext(ctx)
		if err := permSvc.GuardByPublicID(ctx, toPermCaller(caller), resourceType, publicID, actions...); err != nil {
			abortPermissionDenied(ctx, err)
			return
		}

		ctx.Next()
	}
}

// PermGuardProjectTaskAction 校验 caller 是否能在项目下执行 task 类 action（如 task:create）。
func PermGuardProjectTaskAction(
	permSvc PermGuarder,
	extractProjectPublicID PublicIDExtractor,
	action types.Action,
) gin.HandlerFunc {
	return func(ctx *gin.Context) {
		bodyBytes, err := io.ReadAll(ctx.Request.Body)
		_ = ctx.Request.Body.Close()
		if err != nil {
			ctx.AbortWithStatusJSON(http.StatusInternalServerError,
				dto.Error(dto.CodeInternalError, "failed to read request body"))
			return
		}
		ctx.Request.Body = io.NopCloser(bytes.NewReader(bodyBytes))

		publicID, err := extractProjectPublicID(bodyBytes)
		if err != nil {
			ctx.AbortWithStatusJSON(http.StatusBadRequest,
				dto.Error(dto.CodeInvalidParams, err.Error()))
			return
		}

		caller, _ := localauth.FromGinContext(ctx)
		if err := permSvc.GuardProjectTaskAction(ctx, toPermCaller(caller), publicID, action); err != nil {
			abortPermissionDenied(ctx, err)
			return
		}

		ctx.Next()
	}
}

// extractSessionID 从请求 body 提取 session_id。
func extractSessionID(body []byte) (string, error) {
	var req struct {
		SessionID string `json:"session_id"`
	}
	if err := json.Unmarshal(body, &req); err != nil {
		return "", err
	}
	if strings.TrimSpace(req.SessionID) == "" {
		return "", errors.New("session_id is required")
	}
	return strings.TrimSpace(req.SessionID), nil
}

// PermGuardViaSession 从 body 提取 session_id 并校验 session 访问权限。
func PermGuardViaSession(permSvc PermGuarder) gin.HandlerFunc {
	return func(ctx *gin.Context) {
		bodyBytes, err := io.ReadAll(ctx.Request.Body)
		_ = ctx.Request.Body.Close()
		if err != nil {
			ctx.AbortWithStatusJSON(http.StatusInternalServerError,
				dto.Error(dto.CodeInternalError, "failed to read request body"))
			return
		}
		ctx.Request.Body = io.NopCloser(bytes.NewReader(bodyBytes))

		sessionID, err := extractSessionID(bodyBytes)
		if err != nil {
			ctx.AbortWithStatusJSON(http.StatusBadRequest,
				dto.Error(dto.CodeInvalidParams, err.Error()))
			return
		}

		caller, _ := localauth.FromGinContext(ctx)
		if err := permSvc.GuardSessionAccess(ctx, toPermCaller(caller), sessionID); err != nil {
			abortPermissionDenied(ctx, err)
			return
		}

		ctx.Next()
	}
}

// PermGuardViaSessionPath 从 URL path 参数提取 session_id 并校验访问权限。
func PermGuardViaSessionPath(permSvc PermGuarder, paramName string) gin.HandlerFunc {
	return func(ctx *gin.Context) {
		sessionID := strings.TrimSpace(ctx.Param(paramName))
		if sessionID == "" {
			ctx.AbortWithStatusJSON(http.StatusBadRequest,
				dto.Error(dto.CodeInvalidParams, paramName+" is required"))
			return
		}

		caller, _ := localauth.FromGinContext(ctx)
		if err := permSvc.GuardSessionAccess(ctx, toPermCaller(caller), sessionID); err != nil {
			abortPermissionDenied(ctx, err)
			return
		}

		ctx.Next()
	}
}

// PermGuardViaMessage 从 body 提取 message_id，反查 session 并校验访问权限。
func PermGuardViaMessage(permSvc PermGuarder) gin.HandlerFunc {
	return func(ctx *gin.Context) {
		bodyBytes, err := io.ReadAll(ctx.Request.Body)
		_ = ctx.Request.Body.Close()
		if err != nil {
			ctx.AbortWithStatusJSON(http.StatusInternalServerError,
				dto.Error(dto.CodeInternalError, "failed to read request body"))
			return
		}
		ctx.Request.Body = io.NopCloser(bytes.NewReader(bodyBytes))

		var req struct {
			MessageID uint `json:"message_id"`
		}
		if err := json.Unmarshal(bodyBytes, &req); err != nil {
			ctx.AbortWithStatusJSON(http.StatusBadRequest,
				dto.Error(dto.CodeInvalidParams, err.Error()))
			return
		}
		if req.MessageID == 0 {
			ctx.AbortWithStatusJSON(http.StatusBadRequest,
				dto.Error(dto.CodeInvalidParams, "message_id is required"))
			return
		}

		caller, _ := localauth.FromGinContext(ctx)
		if err := permSvc.GuardSessionAccessByMessageID(ctx, toPermCaller(caller), req.MessageID); err != nil {
			abortPermissionDenied(ctx, err)
			return
		}

		ctx.Next()
	}
}

// PermGuardNewMessage 校验 CreateInitialMessage 的资源权限组合。
func PermGuardNewMessage(permSvc PermGuarder) gin.HandlerFunc {
	return func(ctx *gin.Context) {
		bodyBytes, err := io.ReadAll(ctx.Request.Body)
		_ = ctx.Request.Body.Close()
		if err != nil {
			ctx.AbortWithStatusJSON(http.StatusInternalServerError,
				dto.Error(dto.CodeInternalError, "failed to read request body"))
			return
		}
		ctx.Request.Body = io.NopCloser(bytes.NewReader(bodyBytes))

		var req struct {
			ProjectID string `json:"project_id"`
			TaskID    string `json:"task_id"`
		}
		if err := json.Unmarshal(bodyBytes, &req); err != nil {
			ctx.AbortWithStatusJSON(http.StatusBadRequest,
				dto.Error(dto.CodeInvalidParams, err.Error()))
			return
		}

		caller, _ := localauth.FromGinContext(ctx)
		if err := permSvc.GuardNewMessageRequest(ctx, toPermCaller(caller), req.ProjectID, req.TaskID); err != nil {
			abortPermissionDenied(ctx, err)
			return
		}

		ctx.Next()
	}
}

// PermGuardOptionalProject 当 body 含 project_id 时校验 project:view，否则放行。
func PermGuardOptionalProject(permSvc PermGuarder, extractProjectID PublicIDExtractor) gin.HandlerFunc {
	return func(ctx *gin.Context) {
		bodyBytes, err := io.ReadAll(ctx.Request.Body)
		_ = ctx.Request.Body.Close()
		if err != nil {
			ctx.AbortWithStatusJSON(http.StatusInternalServerError,
				dto.Error(dto.CodeInternalError, "failed to read request body"))
			return
		}
		ctx.Request.Body = io.NopCloser(bytes.NewReader(bodyBytes))

		publicID, err := extractProjectID(bodyBytes)
		if err != nil {
			if err.Error() == "project_id is optional" {
				ctx.Next()
				return
			}
			ctx.AbortWithStatusJSON(http.StatusBadRequest,
				dto.Error(dto.CodeInvalidParams, err.Error()))
			return
		}
		if publicID == "" {
			ctx.Next()
			return
		}

		caller, _ := localauth.FromGinContext(ctx)
		if err := permSvc.GuardByPublicID(ctx, toPermCaller(caller), types.ResourceTypeProject, publicID, types.ActionProjectView); err != nil {
			abortPermissionDenied(ctx, err)
			return
		}

		ctx.Next()
	}
}
