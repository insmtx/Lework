package handler

import (
	"net/http"
	"path/filepath"
	"regexp"
	"strings"
	"unicode"
	"unicode/utf8"

	"github.com/gin-gonic/gin"

	"github.com/insmtx/Leros/backend/config"
	"github.com/insmtx/Leros/backend/internal/adapter"
	"github.com/insmtx/Leros/backend/internal/api/dto"
)

type GlobalHandler struct {
	edition adapter.Edition
	cfg     *config.Config
}

func NewGlobalHandler(edition adapter.Edition, cfg *config.Config) *GlobalHandler {
	return &GlobalHandler{edition: edition, cfg: cfg}
}

func (h *GlobalHandler) RegisterRoutes(r gin.IRouter) {
	r.GET("/GlobalConfig", h.GetGlobalConfig)
}

func RegisterGlobalRoutes(r gin.IRouter, edition adapter.Edition, cfg *config.Config) {
	h := NewGlobalHandler(edition, cfg)
	h.RegisterRoutes(r)
}

// GetGlobalConfig 返回服务端通用全局配置信息
//
// @Summary 获取全局配置
// @Description 返回服务端通用全局配置信息（edition、deploy_mode、max_orgs_per_user）
// @Tags Global
// @Produce json
// @Success 200 {object} dto.Response{data=dto.GlobalConfigData} "成功响应"
// @Failure 500 {object} dto.ErrorResponse "内部服务器错误"
// @Router /GlobalConfig [get]
func (h *GlobalHandler) GetGlobalConfig(ctx *gin.Context) {
	phoneCodeLoginEnabled := true
	if h.cfg.Auth != nil && h.cfg.Auth.PhoneCodeLoginEnabled != nil {
		phoneCodeLoginEnabled = *h.cfg.Auth.PhoneCodeLoginEnabled
	}
	ctx.JSON(http.StatusOK, dto.Success(dto.GlobalConfigData{
		Device:                sparkDeviceDeclaration(h.cfg.Spark),
		Edition:               h.edition.Edition(),
		DeployMode:            h.edition.DeployMode(),
		MaxOrgsPerUser:        h.edition.MaxOrgsPerUser(),
		PhoneCodeLoginEnabled: phoneCodeLoginEnabled,
	}))
}

var sparkDeviceIDPattern = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._-]{0,127}$`)

func sparkDeviceDeclaration(cfg config.SparkConfig) *dto.SparkDevice {
	// 兼容已有部署，但零散配置不能把普通后端声明成 Spark。
	bound := cfg.OrgID != 0 && sparkDeviceIDPattern.MatchString(cfg.HostID) &&
		filepath.IsAbs(cfg.SocketPath) && len(cfg.WorkerIDs) <= 32
	seen := make(map[uint]bool)
	for _, id := range cfg.WorkerIDs {
		if id == 0 || seen[id] {
			bound = false
		}
		seen[id] = true
	}
	id := cfg.DeviceID
	if id == "" && bound {
		id = cfg.HostID
	}
	if !sparkDeviceIDPattern.MatchString(id) {
		return nil
	}
	name := cfg.DeviceName
	if name == "" {
		name = "Spark"
	}
	if !utf8.ValidString(name) || len(name) > 256 || strings.TrimSpace(name) != name {
		return nil
	}
	for _, r := range name {
		if unicode.IsControl(r) {
			return nil
		}
	}
	return &dto.SparkDevice{Type: "spark", ProtocolVersion: 1, ID: id, Name: name, Management: bound}
}
