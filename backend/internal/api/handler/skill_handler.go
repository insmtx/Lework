package handler

import (
	"net/http"
	"strconv"

	"github.com/gin-gonic/gin"

	"github.com/insmtx/Leros/backend/internal/api/auth"
	"github.com/insmtx/Leros/backend/internal/api/contract"
	"github.com/insmtx/Leros/backend/internal/api/dto"
)

// RegisterSkillRoutes registers skill management routes.
func RegisterSkillRoutes(r gin.IRouter, service contract.SkillService) {
	r.GET("/skills/recent", listRecentUsedSkills(service))
}

func listRecentUsedSkills(service contract.SkillService) gin.HandlerFunc {
	return func(ctx *gin.Context) {
		caller, _ := auth.FromGinContext(ctx)
		orgID, uin := uint(0), uint(0)
		if caller != nil {
			orgID = caller.OrgID
			uin = caller.Uin
		}

		limit := 10
		if l := ctx.Query("limit"); l != "" {
			if v, err := strconv.Atoi(l); err == nil && v > 0 {
				limit = v
			}
		}

		skills, err := service.ListRecentUsedSkills(ctx, orgID, uin, limit)
		if err != nil {
			ctx.JSON(http.StatusInternalServerError, dto.Error(dto.CodeInternalError, err.Error()))
			return
		}
		if skills == nil {
			skills = []contract.SkillInstalledItem{}
		}

		ctx.JSON(http.StatusOK, dto.Success(skills))
	}
}
