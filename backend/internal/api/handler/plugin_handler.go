package handler

import (
	"errors"
	"net/http"
	"net/url"
	"strings"

	"github.com/gin-gonic/gin"

	"github.com/insmtx/Leros/backend/internal/api/auth"
	"github.com/insmtx/Leros/backend/internal/api/contract"
	"github.com/insmtx/Leros/backend/internal/api/dto"
	"github.com/insmtx/Leros/backend/types"
)

// RegisterPluginRoutes registers organization plugin repository routes.
func RegisterPluginRoutes(r gin.IRouter, service contract.PluginService) {
	r.GET("/plugins", listPlugins(service))
	r.GET("/plugins/installation-status", getPluginInstallationStatus(service))
	r.POST("/plugins/skills", addSkillPlugin(service))
	r.POST("/plugins/skills/download-urls", resolveSkillDownloadURLs(service))
	r.POST("/plugins/mcp", addMCPPlugin(service))
	r.POST("/plugins/mcp/test", testMCPPlugin(service))
	r.GET("/plugins/mcp/platforms", listMCPPlatforms(service))
	r.POST("/plugins/mcp/platforms/:platform_code/connect", connectMCPPlatform(service))
	r.POST("/plugins/mcp/platforms/:platform_code/test", testMCPPlatform(service))
	r.POST("/plugins/mcp/platforms/:platform_code/oauth/start", startMCPPlatformOAuth(service))
	r.GET("/plugins/mcp/platforms/:platform_code/oauth/status", getMCPPlatformOAuthStatus(service))
	r.PUT("/plugins/mcp/:plugin_id", updateMCPPlugin(service))
	r.GET("/plugins/builtin-skills", listBuiltinSkills(service))
	r.DELETE("/plugins/:plugin_id", deletePlugin(service))
	r.GET("/plugins/:plugin_id", getPlugin(service))
	r.GET("/plugins/:plugin_id/versions", listPluginVersions(service))
	r.GET("/plugins/:plugin_id/permissions", getPluginPermissions(service))
	r.PUT("/plugins/:plugin_id/permissions", updatePluginPermissions(service))
}

// RegisterPluginOAuthCallbackRoutes registers callbacks that must remain reachable before login completes.
func RegisterPluginOAuthCallbackRoutes(r gin.IRouter, service contract.PluginService) {
	r.GET("/plugins/mcp/oauth/:platform_code/callback", completeMCPPlatformOAuth(service))
	r.GET("/plugins/callback", pluginCallback())
}

func startMCPPlatformOAuth(service contract.PluginService) gin.HandlerFunc {
	return func(ctx *gin.Context) {
		caller, ok := pluginCaller(ctx)
		if !ok {
			return
		}
		platformCode := strings.TrimSpace(ctx.Param("platform_code"))
		if platformCode == "" {
			ctx.JSON(http.StatusBadRequest, dto.Error(dto.CodeInvalidParams, "platform_code is required"))
			return
		}
		result, err := service.StartMCPPlatformOAuth(ctx, caller.OrgID, caller.Uin, platformCode)
		writePluginServiceResult(ctx, result, err)
	}
}

func getMCPPlatformOAuthStatus(service contract.PluginService) gin.HandlerFunc {
	return func(ctx *gin.Context) {
		caller, ok := pluginCaller(ctx)
		if !ok {
			return
		}
		platformCode := strings.TrimSpace(ctx.Param("platform_code"))
		attemptID := strings.TrimSpace(ctx.Query("attempt_id"))
		if platformCode == "" || attemptID == "" {
			ctx.JSON(http.StatusBadRequest, dto.Error(dto.CodeInvalidParams, "platform_code and attempt_id are required"))
			return
		}
		result, err := service.GetMCPPlatformOAuthStatus(ctx, caller.OrgID, caller.Uin, platformCode, attemptID)
		writePluginServiceResult(ctx, result, err)
	}
}

func completeMCPPlatformOAuth(service contract.PluginService) gin.HandlerFunc {
	return func(ctx *gin.Context) {
		platformCode := strings.TrimSpace(ctx.Param("platform_code"))
		result, err := service.CompleteMCPPlatformOAuth(
			ctx,
			platformCode,
			strings.TrimSpace(ctx.Query("state")),
			strings.TrimSpace(ctx.Query("code")),
			strings.TrimSpace(ctx.Query("error")),
		)
		setPluginCallbackSecurityHeaders(ctx)
		resultCode := pluginCallbackResultFailed
		if err == nil && result != nil && result.Connected {
			resultCode = pluginCallbackResultSuccess
		}
		ctx.Redirect(http.StatusSeeOther, pluginCallbackURL(platformCode, resultCode))
	}
}

const (
	pluginCallbackFlowMCPOAuth = "mcp_oauth"

	pluginCallbackResultSuccess = "success"
	pluginCallbackResultFailed  = "failed"
)

func pluginCallback() gin.HandlerFunc {
	return func(ctx *gin.Context) {
		setPluginCallbackSecurityHeaders(ctx)
		flow := strings.TrimSpace(ctx.Query("flow"))
		pluginCode := strings.TrimSpace(ctx.Query("plugin"))
		result := strings.TrimSpace(ctx.Query("result"))
		if flow != pluginCallbackFlowMCPOAuth || pluginCode == "" {
			ctx.Data(http.StatusBadRequest, "text/html; charset=utf-8", []byte(pluginCallbackInvalidHTML))
			return
		}
		switch result {
		case pluginCallbackResultSuccess:
			ctx.Data(http.StatusOK, "text/html; charset=utf-8", []byte(pluginCallbackSuccessHTML))
		case pluginCallbackResultFailed:
			ctx.Data(http.StatusOK, "text/html; charset=utf-8", []byte(pluginCallbackFailureHTML))
		default:
			ctx.Data(http.StatusBadRequest, "text/html; charset=utf-8", []byte(pluginCallbackInvalidHTML))
		}
	}
}

func pluginCallbackURL(platformCode, result string) string {
	query := url.Values{
		"flow":   {pluginCallbackFlowMCPOAuth},
		"plugin": {platformCode},
		"result": {result},
	}
	return "/v1/plugins/callback?" + query.Encode()
}

func setPluginCallbackSecurityHeaders(ctx *gin.Context) {
	ctx.Header("Content-Security-Policy", "default-src 'none'; style-src 'unsafe-inline'; base-uri 'none'; form-action 'none'; frame-ancestors 'none'")
	ctx.Header("Referrer-Policy", "no-referrer")
	ctx.Header("Cache-Control", "no-store")
}

const pluginCallbackSuccessHTML = `<!doctype html><html lang="zh-CN"><meta charset="utf-8"><title>连接成功</title><style>body{font-family:system-ui;margin:4rem;text-align:center;color:#202124}</style><h1>连接成功</h1><p>可以关闭此窗口，返回 Lework。</p></html>`

const pluginCallbackFailureHTML = `<!doctype html><html lang="zh-CN"><meta charset="utf-8"><title>连接未完成</title><style>body{font-family:system-ui;margin:4rem;text-align:center;color:#202124}</style><h1>连接未完成</h1><p>请关闭此窗口后，在 Lework 中重新连接。</p></html>`

const pluginCallbackInvalidHTML = `<!doctype html><html lang="zh-CN"><meta charset="utf-8"><title>请求无效</title><style>body{font-family:system-ui;margin:4rem;text-align:center;color:#202124}</style><h1>请求无效</h1><p>请关闭此窗口后重新发起连接。</p></html>`

func listMCPPlatforms(service contract.PluginService) gin.HandlerFunc {
	return func(ctx *gin.Context) {
		caller, ok := pluginCaller(ctx)
		if !ok {
			return
		}
		result, err := service.ListMCPPlatforms(ctx, caller.OrgID, caller.Uin)
		writePluginServiceResult(ctx, result, err)
	}
}

func connectMCPPlatform(service contract.PluginService) gin.HandlerFunc {
	return func(ctx *gin.Context) {
		caller, ok := pluginCaller(ctx)
		if !ok {
			return
		}
		platformCode := strings.TrimSpace(ctx.Param("platform_code"))
		if platformCode == "" {
			ctx.JSON(http.StatusBadRequest, dto.Error(dto.CodeInvalidParams, "platform_code is required"))
			return
		}
		var req contract.ConnectMCPPlatformRequest
		if ctx.Request.ContentLength != 0 {
			if err := ctx.ShouldBindJSON(&req); err != nil {
				ctx.JSON(http.StatusBadRequest, dto.Error(dto.CodeInvalidParams, err.Error()))
				return
			}
		}
		result, err := service.ConnectMCPPlatform(ctx, caller.OrgID, caller.Uin, platformCode, &req)
		writePluginServiceResult(ctx, result, err)
	}
}

func testMCPPlatform(service contract.PluginService) gin.HandlerFunc {
	return func(ctx *gin.Context) {
		caller, ok := pluginCaller(ctx)
		if !ok {
			return
		}
		platformCode := strings.TrimSpace(ctx.Param("platform_code"))
		if platformCode == "" {
			ctx.JSON(http.StatusBadRequest, dto.Error(dto.CodeInvalidParams, "platform_code is required"))
			return
		}
		result, err := service.TestMCPPlatform(ctx, caller.OrgID, caller.Uin, platformCode)
		writePluginServiceResult(ctx, result, err)
	}
}

func addMCPPlugin(service contract.PluginService) gin.HandlerFunc {
	return func(ctx *gin.Context) {
		caller, ok := pluginCaller(ctx)
		if !ok {
			return
		}
		var req contract.AddMCPPluginRequest
		if err := ctx.ShouldBindJSON(&req); err != nil {
			ctx.JSON(http.StatusBadRequest, dto.Error(dto.CodeInvalidParams, err.Error()))
			return
		}
		result, err := service.AddMCPPlugin(ctx, caller.OrgID, caller.Uin, &req)
		writePluginServiceResult(ctx, result, err)
	}
}

func updateMCPPlugin(service contract.PluginService) gin.HandlerFunc {
	return func(ctx *gin.Context) {
		caller, ok := pluginCaller(ctx)
		if !ok {
			return
		}
		pluginID := strings.TrimSpace(ctx.Param("plugin_id"))
		if pluginID == "" {
			ctx.JSON(http.StatusBadRequest, dto.Error(dto.CodeInvalidParams, "plugin_id is required"))
			return
		}
		var req contract.UpdateMCPPluginRequest
		if err := ctx.ShouldBindJSON(&req); err != nil {
			ctx.JSON(http.StatusBadRequest, dto.Error(dto.CodeInvalidParams, err.Error()))
			return
		}
		result, err := service.UpdateMCPPlugin(ctx, caller.OrgID, caller.Uin, pluginID, &req)
		writePluginServiceResult(ctx, result, err)
	}
}

func testMCPPlugin(service contract.PluginService) gin.HandlerFunc {
	return func(ctx *gin.Context) {
		if _, ok := pluginCaller(ctx); !ok {
			return
		}
		var req contract.TestMCPPluginRequest
		if err := ctx.ShouldBindJSON(&req); err != nil {
			ctx.JSON(http.StatusBadRequest, dto.Error(dto.CodeInvalidParams, err.Error()))
			return
		}
		result, err := service.TestMCPPlugin(ctx, &req)
		writePluginServiceResult(ctx, result, err)
	}
}

func resolveSkillDownloadURLs(service contract.PluginService) gin.HandlerFunc {
	return func(ctx *gin.Context) {
		caller, ok := pluginCaller(ctx)
		if !ok {
			return
		}
		var req contract.ResolveSkillDownloadURLsRequest
		if err := ctx.ShouldBindJSON(&req); err != nil {
			ctx.JSON(http.StatusBadRequest, dto.Error(dto.CodeInvalidParams, err.Error()))
			return
		}
		result, err := service.ResolveSkillDownloadURLs(ctx, caller.OrgID, caller.Kind, callerID(caller), &req)
		writePluginServiceResult(ctx, result, err)
	}
}

func getPluginInstallationStatus(service contract.PluginService) gin.HandlerFunc {
	return func(ctx *gin.Context) {
		caller, ok := pluginCaller(ctx)
		if !ok {
			return
		}
		var req contract.GetPluginInstallationStatusRequest
		if err := ctx.ShouldBindQuery(&req); err != nil {
			ctx.JSON(http.StatusBadRequest, dto.Error(dto.CodeInvalidParams, err.Error()))
			return
		}
		if strings.TrimSpace(req.Kind) == "" || strings.TrimSpace(req.Code) == "" {
			ctx.JSON(http.StatusBadRequest, dto.Error(dto.CodeInvalidParams, "kind and code are required"))
			return
		}
		result, err := service.GetPluginInstallationStatus(ctx, caller.OrgID, caller.Uin, &req)
		writePluginServiceResult(ctx, result, err)
	}
}

func listPlugins(service contract.PluginService) gin.HandlerFunc {
	return func(ctx *gin.Context) {
		caller, ok := pluginCaller(ctx)
		if !ok {
			return
		}
		var req contract.ListPluginsRequest
		if err := ctx.ShouldBindQuery(&req); err != nil {
			ctx.JSON(http.StatusBadRequest, dto.Error(dto.CodeInvalidParams, err.Error()))
			return
		}
		result, err := service.ListPlugins(ctx, caller.OrgID, caller.Uin, &req)
		writePluginServiceResult(ctx, result, err)
	}
}

func getPlugin(service contract.PluginService) gin.HandlerFunc {
	return func(ctx *gin.Context) {
		caller, ok := pluginCaller(ctx)
		if !ok {
			return
		}
		pluginID := strings.TrimSpace(ctx.Param("plugin_id"))
		if pluginID == "" {
			ctx.JSON(http.StatusBadRequest, dto.Error(dto.CodeInvalidParams, "plugin_id is required"))
			return
		}
		var req contract.GetPluginRequest
		if err := ctx.ShouldBindQuery(&req); err != nil {
			ctx.JSON(http.StatusBadRequest, dto.Error(dto.CodeInvalidParams, err.Error()))
			return
		}
		result, err := service.GetPlugin(ctx, caller.OrgID, caller.Uin, pluginID, &req)
		writePluginServiceResult(ctx, result, err)
	}
}

func listPluginVersions(service contract.PluginService) gin.HandlerFunc {
	return func(ctx *gin.Context) {
		caller, ok := pluginCaller(ctx)
		if !ok {
			return
		}
		pluginID := strings.TrimSpace(ctx.Param("plugin_id"))
		if pluginID == "" {
			ctx.JSON(http.StatusBadRequest, dto.Error(dto.CodeInvalidParams, "plugin_id is required"))
			return
		}
		result, err := service.ListPluginVersions(ctx, caller.OrgID, caller.Uin, pluginID)
		writePluginServiceResult(ctx, result, err)
	}
}

func deletePlugin(service contract.PluginService) gin.HandlerFunc {
	return func(ctx *gin.Context) {
		caller, ok := pluginCaller(ctx)
		if !ok {
			return
		}
		pluginID := strings.TrimSpace(ctx.Param("plugin_id"))
		if pluginID == "" {
			ctx.JSON(http.StatusBadRequest, dto.Error(dto.CodeInvalidParams, "plugin_id is required"))
			return
		}
		var req contract.DeletePluginRequest
		if err := ctx.ShouldBindQuery(&req); err != nil {
			ctx.JSON(http.StatusBadRequest, dto.Error(dto.CodeInvalidParams, err.Error()))
			return
		}
		result, err := service.DeletePlugin(ctx, caller.OrgID, caller.Uin, pluginID, &req)
		writePluginServiceResult(ctx, result, err)
	}
}

func addSkillPlugin(service contract.PluginService) gin.HandlerFunc {
	return func(ctx *gin.Context) {
		caller, ok := pluginCaller(ctx)
		if !ok {
			return
		}
		var req contract.AddSkillPluginRequest
		if err := ctx.ShouldBindJSON(&req); err != nil {
			ctx.JSON(http.StatusBadRequest, dto.Error(dto.CodeInvalidParams, err.Error()))
			return
		}
		switch strings.TrimSpace(req.Mode) {
		case contract.SkillAddModeFile:
			if strings.TrimSpace(req.FileUploadID) == "" || strings.TrimSpace(req.GitHubURL) != "" {
				ctx.JSON(http.StatusBadRequest, dto.Error(dto.CodeInvalidParams, "file mode requires file_upload_id only"))
				return
			}
		case contract.SkillAddModeGitHub:
			if strings.TrimSpace(req.GitHubURL) == "" || strings.TrimSpace(req.FileUploadID) != "" {
				ctx.JSON(http.StatusBadRequest, dto.Error(dto.CodeInvalidParams, "github mode requires github_url only"))
				return
			}
		default:
			ctx.JSON(http.StatusBadRequest, dto.Error(dto.CodeInvalidParams, "mode must be file or github"))
			return
		}
		result, err := service.AddSkillPlugin(ctx, caller.OrgID, caller.Uin, &req)
		writePluginServiceResult(ctx, result, err)
	}
}

func getPluginPermissions(service contract.PluginService) gin.HandlerFunc {
	return func(ctx *gin.Context) {
		caller, ok := pluginCaller(ctx)
		if !ok {
			return
		}
		pluginID := strings.TrimSpace(ctx.Param("plugin_id"))
		if pluginID == "" {
			ctx.JSON(http.StatusBadRequest, dto.Error(dto.CodeInvalidParams, "plugin_id is required"))
			return
		}
		result, err := service.GetPluginPermissions(ctx, caller.OrgID, caller.Uin, pluginID)
		writePluginServiceResult(ctx, result, err)
	}
}

func updatePluginPermissions(service contract.PluginService) gin.HandlerFunc {
	return func(ctx *gin.Context) {
		caller, ok := pluginCaller(ctx)
		if !ok {
			return
		}
		pluginID := strings.TrimSpace(ctx.Param("plugin_id"))
		if pluginID == "" {
			ctx.JSON(http.StatusBadRequest, dto.Error(dto.CodeInvalidParams, "plugin_id is required"))
			return
		}
		var req contract.UpdatePluginPermissionsRequest
		if err := ctx.ShouldBindJSON(&req); err != nil {
			ctx.JSON(http.StatusBadRequest, dto.Error(dto.CodeInvalidParams, err.Error()))
			return
		}
		result, err := service.UpdatePluginPermissions(ctx, caller.OrgID, caller.Uin, pluginID, &req)
		writePluginServiceResult(ctx, result, err)
	}
}

func pluginCaller(ctx *gin.Context) (*types.Caller, bool) {
	caller, _ := auth.FromGinContext(ctx)
	if caller == nil || caller.OrgID == 0 {
		ctx.JSON(http.StatusUnauthorized, dto.Error(dto.CodeUnauthorized, "organization identity is required"))
		return nil, false
	}
	return caller, true
}

func callerID(caller *types.Caller) uint {
	if caller.Kind == types.CallerKindWorker {
		return caller.WorkerID
	}
	return caller.Uin
}

func writePluginServiceResult(ctx *gin.Context, result interface{}, err error) {
	if err == nil {
		ctx.JSON(http.StatusOK, dto.Success(result))
		return
	}
	if errors.Is(err, contract.ErrPluginNotFound) {
		ctx.JSON(http.StatusNotFound, dto.Error(dto.CodeNotFound, err.Error()))
		return
	}
	if errors.Is(err, contract.ErrPluginForbidden) {
		ctx.JSON(http.StatusForbidden, dto.Error(dto.CodeForbidden, err.Error()))
		return
	}
	if errors.Is(err, contract.ErrPluginPermissionUnsupported) || errors.Is(err, contract.ErrInvalidPluginPermission) {
		ctx.JSON(http.StatusBadRequest, dto.Error(dto.CodeInvalidParams, err.Error()))
		return
	}
	if errors.Is(err, contract.ErrInvalidPluginConfig) {
		ctx.JSON(http.StatusBadRequest, dto.Error(dto.CodeValidationError, err.Error()))
		return
	}
	if strings.Contains(err.Error(), "definition") || strings.Contains(err.Error(), "plugin kind") {
		ctx.JSON(http.StatusBadRequest, dto.Error(dto.CodeValidationError, err.Error()))
		return
	}
	ctx.JSON(http.StatusInternalServerError, dto.Error(dto.CodeInternalError, err.Error()))
}

func listBuiltinSkills(service contract.PluginService) gin.HandlerFunc {
	return func(ctx *gin.Context) {
		_, ok := pluginCaller(ctx)
		if !ok {
			return
		}
		result, err := service.ListBuiltinSkills(ctx)
		writePluginServiceResult(ctx, result, err)
	}
}
