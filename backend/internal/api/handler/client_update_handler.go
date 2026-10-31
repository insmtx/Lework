package handler

import (
	"net/http"
	"strings"

	"github.com/gin-gonic/gin"
	"github.com/insmtx/Lework/backend/config"
	"github.com/insmtx/Lework/backend/internal/api/dto"
	"github.com/insmtx/Lework/backend/internal/api/middleware"
)

// ClientVersionReportRequest reports the client build that is currently running.
type ClientVersionReportRequest struct {
	App      string `json:"app"`
	Version  string `json:"version"`
	Platform string `json:"platform,omitempty"`
	Arch     string `json:"arch,omitempty"`
	Channel  string `json:"channel,omitempty"`
}

// ClientVersionReportResponse describes update policy for the reported client.
type ClientVersionReportResponse struct {
	ForceUpdate         bool   `json:"force_update"`
	App                 string `json:"app,omitempty"`
	CurrentVersion      string `json:"current_version,omitempty"`
	MinSupportedVersion string `json:"min_supported_version,omitempty"`
	LatestVersion       string `json:"latest_version,omitempty"`
	UpdateURL           string `json:"update_url,omitempty"`
	Message             string `json:"message,omitempty"`
}

// RegisterClientUpdateRoutes registers client update policy endpoints.
func RegisterClientUpdateRoutes(r gin.IRouter, cfg *config.ClientUpdateConfig) {
	r.POST("/ClientVersionReport", func(ctx *gin.Context) {
		var req ClientVersionReportRequest
		if err := ctx.ShouldBindJSON(&req); err != nil {
			ctx.JSON(http.StatusBadRequest, dto.Error(dto.CodeInvalidParams, err.Error()))
			return
		}

		appName := strings.ToLower(strings.TrimSpace(req.App))
		version := strings.TrimSpace(req.Version)
		if appName == "" || version == "" {
			ctx.JSON(http.StatusBadRequest, dto.Error(dto.CodeInvalidParams, "app and version are required"))
			return
		}

		result := evaluateClientUpdate(cfg, appName, version)
		ctx.JSON(http.StatusOK, dto.Success(result))
	})
}

func evaluateClientUpdate(cfg *config.ClientUpdateConfig, appName string, version string) ClientVersionReportResponse {
	result := ClientVersionReportResponse{
		ForceUpdate:    false,
		App:            appName,
		CurrentVersion: version,
	}
	if cfg == nil {
		return result
	}

	policy, ok := clientPolicy(cfg, appName)
	if !ok {
		return result
	}

	result.MinSupportedVersion = policy.MinSupportedVersion
	result.LatestVersion = policy.LatestVersion
	result.UpdateURL = policy.UpdateURL
	result.Message = policy.ForceMessage
	if result.Message == "" {
		result.Message = "当前客户端版本过低，请更新后继续使用"
	}

	if policy.MinSupportedVersion != "" && compareClientVersions(version, policy.MinSupportedVersion) < 0 {
		result.ForceUpdate = true
	}

	return result
}

func clientPolicy(cfg *config.ClientUpdateConfig, appName string) (config.ClientUpdatePolicy, bool) {
	switch appName {
	case "desktop":
		return cfg.Desktop, true
	case "web":
		return cfg.Web, true
	default:
		return config.ClientUpdatePolicy{}, false
	}
}

func compareClientVersions(left string, right string) int {
	return middleware.CompareVersions(left, right)
}
