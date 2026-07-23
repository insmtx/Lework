package handler

import (
	"net/http"

	"github.com/gin-gonic/gin"

	"github.com/insmtx/Leros/backend/internal/adapter"
	"github.com/insmtx/Leros/backend/internal/api/dto"
)

type GlobalHandler struct {
	edition adapter.Edition
}

func NewGlobalHandler(edition adapter.Edition) *GlobalHandler {
	return &GlobalHandler{edition: edition}
}

func (h *GlobalHandler) RegisterRoutes(r gin.IRouter) {
	r.GET("/GlobalConfig", h.GetGlobalConfig)
}

func RegisterGlobalRoutes(r gin.IRouter, edition adapter.Edition) {
	h := NewGlobalHandler(edition)
	h.RegisterRoutes(r)
}

// GetGlobalConfig 返回服务端通用全局配置信息
//
// @Summary 获取全局配置
// @Description 返回服务端通用全局配置信息（当前返回 edition 字段）
// @Tags Global
// @Produce json
// @Success 200 {object} dto.Response{data=dto.GlobalConfigData} "成功响应"
// @Failure 500 {object} dto.ErrorResponse "内部服务器错误"
// @Router /GlobalConfig [get]
func (h *GlobalHandler) GetGlobalConfig(ctx *gin.Context) {
	ctx.JSON(http.StatusOK, dto.Success(dto.GlobalConfigData{
		Edition: h.edition.Edition(),
	}))
}
