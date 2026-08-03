package handler

import (
	"net/http"

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
		Edition:               h.edition.Edition(),
		DeployMode:            h.edition.DeployMode(),
		MaxOrgsPerUser:        h.edition.MaxOrgsPerUser(),
		PhoneCodeLoginEnabled: phoneCodeLoginEnabled,
	}))
}
