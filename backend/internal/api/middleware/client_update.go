package middleware

import (
	"net/http"
	"strconv"
	"strings"

	"github.com/gin-gonic/gin"
	"github.com/insmtx/Leros/backend/config"
	"github.com/insmtx/Leros/backend/internal/api/dto"
)

const (
	headerClientApp      = "X-Leros-Client-App"
	headerClientVersion  = "X-Leros-Client-Version"
	headerClientPlatform = "X-Leros-Client-Platform"
	headerClientArch     = "X-Leros-Client-Arch"
)

// ClientUpdateRequiredResponse describes why a client must update before continuing.
type ClientUpdateRequiredResponse struct {
	ForceUpdate         bool   `json:"force_update"`
	App                 string `json:"app,omitempty"`
	CurrentVersion      string `json:"current_version,omitempty"`
	MinSupportedVersion string `json:"min_supported_version,omitempty"`
	LatestVersion       string `json:"latest_version,omitempty"`
	UpdateURL           string `json:"update_url,omitempty"`
	Message             string `json:"message"`
}

// ClientUpdateMiddleware blocks business APIs when a known client app is below the minimum supported version.
func ClientUpdateMiddleware(cfg *config.ClientUpdateConfig) gin.HandlerFunc {
	return func(ctx *gin.Context) {
		if cfg == nil || shouldSkipClientUpdateCheck(ctx.Request.URL.Path) {
			ctx.Next()
			return
		}

		appName := strings.ToLower(strings.TrimSpace(ctx.GetHeader(headerClientApp)))
		clientVersion := strings.TrimSpace(ctx.GetHeader(headerClientVersion))
		if appName == "" || clientVersion == "" {
			ctx.Next()
			return
		}

		policy, ok := policyForClientApp(cfg, appName)
		if !ok || strings.TrimSpace(policy.MinSupportedVersion) == "" {
			ctx.Next()
			return
		}

		if CompareVersions(clientVersion, policy.MinSupportedVersion) >= 0 {
			ctx.Next()
			return
		}

		ctx.AbortWithStatusJSON(http.StatusUpgradeRequired, dto.ErrorWithData(
			dto.CodeClientUpgradeRequired,
			updateRequiredMessage(policy),
			ClientUpdateRequiredResponse{
				ForceUpdate:         true,
				App:                 appName,
				CurrentVersion:      clientVersion,
				MinSupportedVersion: policy.MinSupportedVersion,
				LatestVersion:       policy.LatestVersion,
				UpdateURL:           policy.UpdateURL,
				Message:             updateRequiredMessage(policy),
			},
		))
	}
}

func policyForClientApp(cfg *config.ClientUpdateConfig, appName string) (config.ClientUpdatePolicy, bool) {
	switch appName {
	case "desktop":
		return cfg.Desktop, true
	case "web":
		return cfg.Web, true
	default:
		return config.ClientUpdatePolicy{}, false
	}
}

func shouldSkipClientUpdateCheck(path string) bool {
	if path == "" {
		return false
	}

	skipPrefixes := []string{
		"/v1/ClientVersionReport",
		"/v1/Login",
		"/v1/Register",
		"/v1/SendPhoneLoginCode",
		"/v1/RefreshToken",
		"/v1/ChooseUin",
		"/v1/static",
		"/v1/swagger",
		"/worker",
		"/presigned",
	}

	for _, prefix := range skipPrefixes {
		if strings.HasPrefix(path, prefix) {
			return true
		}
	}
	return false
}

func updateRequiredMessage(policy config.ClientUpdatePolicy) string {
	if strings.TrimSpace(policy.ForceMessage) != "" {
		return policy.ForceMessage
	}
	return "当前客户端版本过低，请更新后继续使用"
}

// CompareVersions compares two dotted semantic versions.
func CompareVersions(left string, right string) int {
	leftParts := splitVersion(left)
	rightParts := splitVersion(right)
	maxLen := len(leftParts)
	if len(rightParts) > maxLen {
		maxLen = len(rightParts)
	}

	for i := 0; i < maxLen; i++ {
		leftPart := versionPartAt(leftParts, i)
		rightPart := versionPartAt(rightParts, i)
		if leftPart > rightPart {
			return 1
		}
		if leftPart < rightPart {
			return -1
		}
	}
	return 0
}

func splitVersion(version string) []int {
	version = strings.TrimSpace(version)
	version = strings.TrimPrefix(version, "v")
	version = strings.TrimPrefix(version, "V")
	if version == "" {
		return nil
	}

	base := strings.FieldsFunc(version, func(r rune) bool {
		return r == '-' || r == '+'
	})[0]
	rawParts := strings.Split(base, ".")
	parts := make([]int, 0, len(rawParts))
	for _, rawPart := range rawParts {
		part := leadingInt(rawPart)
		parts = append(parts, part)
	}
	return parts
}

func leadingInt(value string) int {
	value = strings.TrimSpace(value)
	end := 0
	for end < len(value) && value[end] >= '0' && value[end] <= '9' {
		end++
	}
	if end == 0 {
		return 0
	}

	parsed, err := strconv.Atoi(value[:end])
	if err != nil {
		return 0
	}
	return parsed
}

func versionPartAt(parts []int, index int) int {
	if index < 0 || index >= len(parts) {
		return 0
	}
	return parts[index]
}
