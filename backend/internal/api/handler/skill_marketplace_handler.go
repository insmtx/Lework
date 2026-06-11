package handler

import (
	"net/http"

	"github.com/gin-gonic/gin"

	"github.com/insmtx/Leros/backend/internal/api/contract"
	"github.com/insmtx/Leros/backend/internal/api/dto"
)

// RegisterSkillMarketplaceRoutes 注册 Skill 市场相关路由。
func RegisterSkillMarketplaceRoutes(r gin.IRouter, service contract.SkillMarketplaceService) {
	r.GET("/skill-marketplace/search", searchSkillMarketplace(service))
}

func searchSkillMarketplace(service contract.SkillMarketplaceService) gin.HandlerFunc {
	return func(ctx *gin.Context) {
		var req contract.SearchSkillMarketplaceRequest
		if err := ctx.ShouldBindQuery(&req); err != nil {
			ctx.JSON(http.StatusBadRequest, dto.Error(dto.CodeInvalidParams, err.Error()))
			return
		}

		result, err := service.SearchSkillMarketplace(ctx, &req)
		if err != nil {
			ctx.JSON(http.StatusInternalServerError, dto.Error(dto.CodeInternalError, err.Error()))
			return
		}

		ctx.JSON(http.StatusOK, dto.Success(result))
	}
}
