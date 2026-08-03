package handler

import (
	"net/http"
	"strings"

	"github.com/gin-gonic/gin"

	"github.com/insmtx/Leros/backend/internal/api/contract"
	"github.com/insmtx/Leros/backend/internal/api/dto"
)

// RegisterOfficialPluginMarketplaceRoutes registers the official plugin catalogue separately from organization plugins.
func RegisterOfficialPluginMarketplaceRoutes(r gin.IRouter, service contract.OfficialPluginMarketplaceService) {
	r.GET("/plugin-marketplace/items", listOfficialPluginMarketplaceItems(service))
	r.GET("/plugin-marketplace/items/latest-version", getOfficialPluginLatestVersion(service))
	r.GET("/plugin-marketplace/items/:item_id", getOfficialPluginMarketplaceItem(service))
	r.POST("/plugin-marketplace/items/:item_id/install", installOfficialPlugin(service))
}

func listOfficialPluginMarketplaceItems(service contract.OfficialPluginMarketplaceService) gin.HandlerFunc {
	return func(ctx *gin.Context) {
		caller, ok := pluginCaller(ctx)
		if !ok {
			return
		}
		var req contract.ListOfficialPluginMarketplaceItemsRequest
		if err := ctx.ShouldBindQuery(&req); err != nil {
			ctx.JSON(http.StatusBadRequest, dto.Error(dto.CodeInvalidParams, err.Error()))
			return
		}
		result, err := service.ListOfficialPluginMarketplaceItems(ctx, caller.OrgID, &req)
		writeOfficialPluginMarketplaceResult(ctx, result, err)
	}
}

func getOfficialPluginLatestVersion(service contract.OfficialPluginMarketplaceService) gin.HandlerFunc {
	return func(ctx *gin.Context) {
		var req contract.GetOfficialPluginLatestVersionRequest
		if err := ctx.ShouldBindQuery(&req); err != nil {
			ctx.JSON(http.StatusBadRequest, dto.Error(dto.CodeInvalidParams, err.Error()))
			return
		}
		if strings.TrimSpace(req.Kind) == "" || strings.TrimSpace(req.Code) == "" {
			ctx.JSON(http.StatusBadRequest, dto.Error(dto.CodeInvalidParams, "kind and code are required"))
			return
		}
		result, err := service.GetOfficialPluginLatestVersion(ctx, &req)
		writeOfficialPluginMarketplaceResult(ctx, result, err)
	}
}

func getOfficialPluginMarketplaceItem(service contract.OfficialPluginMarketplaceService) gin.HandlerFunc {
	return func(ctx *gin.Context) {
		caller, ok := pluginCaller(ctx)
		if !ok {
			return
		}
		itemID := strings.TrimSpace(ctx.Param("item_id"))
		if itemID == "" {
			ctx.JSON(http.StatusBadRequest, dto.Error(dto.CodeInvalidParams, "item_id is required"))
			return
		}
		result, err := service.GetOfficialPluginMarketplaceItem(ctx, caller.OrgID, itemID)
		writeOfficialPluginMarketplaceResult(ctx, result, err)
	}
}

func installOfficialPlugin(service contract.OfficialPluginMarketplaceService) gin.HandlerFunc {
	return func(ctx *gin.Context) {
		caller, ok := pluginCaller(ctx)
		if !ok {
			return
		}
		itemID := strings.TrimSpace(ctx.Param("item_id"))
		if itemID == "" {
			ctx.JSON(http.StatusBadRequest, dto.Error(dto.CodeInvalidParams, "item_id is required"))
			return
		}
		result, err := service.InstallOfficialPlugin(ctx, caller.OrgID, caller.Uin, itemID)
		writeOfficialPluginMarketplaceResult(ctx, result, err)
	}
}

func writeOfficialPluginMarketplaceResult(ctx *gin.Context, result interface{}, err error) {
	if err == nil {
		ctx.JSON(http.StatusOK, dto.Success(result))
		return
	}
	if err == contract.ErrPluginNotFound {
		ctx.JSON(http.StatusNotFound, dto.Error(dto.CodeNotFound, err.Error()))
		return
	}
	ctx.JSON(http.StatusInternalServerError, dto.Error(dto.CodeInternalError, err.Error()))
}
