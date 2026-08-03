package handler

import (
	"net/http"
	"strings"

	"github.com/gin-gonic/gin"
	"github.com/ygpkg/yg-go/logs"

	"github.com/insmtx/Leros/backend/internal/api/dto"
)

// isPermissionDenied reports whether err is a permission denial from PermissionService.
func isPermissionDenied(err error) bool {
	return err != nil && strings.HasPrefix(err.Error(), "permission denied")
}

// abortPermissionDenied writes a 403 Forbidden response and aborts the handler chain.
// err is the error returned by PermissionService.GuardXxx; its message may contain the deny reason.
// "not found" errors from GuardXxx (resource missing) are mapped to 404 to avoid leaking existence.
func abortPermissionDenied(ctx *gin.Context, err error) {
	if err == nil {
		return
	}
	msg := err.Error()
	if strings.HasSuffix(msg, "not found") {
		ctx.AbortWithStatusJSON(http.StatusNotFound, dto.Error(dto.CodeNotFound, msg))
		return
	}
	if isPermissionDenied(err) {
		ctx.AbortWithStatusJSON(http.StatusForbidden, dto.Error(dto.CodeForbidden, "permission denied"))
		return
	}
	logs.ErrorContextf(ctx, "permission guard infrastructure error: %v", err)
	ctx.AbortWithStatusJSON(http.StatusInternalServerError, dto.Error(dto.CodeInternalError, "permission check unavailable"))
}
