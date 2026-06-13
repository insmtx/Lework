package handler

import (
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"

	"github.com/gin-gonic/gin"

	"github.com/insmtx/Leros/backend/internal/api/dto"
	"github.com/insmtx/Leros/backend/internal/infra/filestore"
)

const presignTokenQuery = "token"
const presignExpiresQuery = "expires"

// RegisterPresignedRoutes registers routes that handle presigned URL consumption
func RegisterPresignedRoutes(r gin.IRouter) {
	r.PUT("/:bucket/*key", handlePresignedPut)
	r.GET("/:bucket/*key", handlePresignedGet)
}

// handlePresignedPut consumes a presigned upload URL
// @Summary 预签名上传
// @Description 消费预签名上传 URL，验证 token 后保存文件内容到指定 bucket/key
// @Tags Storage
// @Accept octet-stream
// @Produce json
// @Param bucket path string true "存储桶名称"
// @Param key path string true "对象 key"
// @Param token query string true "预签名 token"
// @Param expires query string true "过期时间戳(秒)"
// @Success 200 {object} dto.Response "成功响应"
// @Failure 400 {object} dto.ErrorResponse "请求参数错误"
// @Failure 403 {object} dto.ErrorResponse "预签名验证失败"
// @Failure 500 {object} dto.ErrorResponse "上传失败"
// @Router /presigned/{bucket}/{key} [put]
func handlePresignedPut(ctx *gin.Context) {
	token := strings.TrimSpace(ctx.Query(presignTokenQuery))
	expires := strings.TrimSpace(ctx.Query(presignExpiresQuery))
	if token == "" || expires == "" {
		ctx.JSON(http.StatusBadRequest, dto.Error(dto.CodeInvalidParams, "missing token or expires query parameter"))
		return
	}

	bucket := strings.TrimSpace(ctx.Param("bucket"))
	key := strings.TrimPrefix(ctx.Param("key"), "/")

	if bucket == "" || key == "" {
		ctx.JSON(http.StatusBadRequest, dto.Error(dto.CodeInvalidParams, "bucket and key are required"))
		return
	}

	if err := filestore.VerifyPresignedToken(
		filestore.SignSecret(), bucket, key, "put", token, expires,
	); err != nil {
		if errors.Is(err, filestore.ErrPresignExpired) {
			ctx.JSON(http.StatusForbidden, dto.Error(dto.CodeInternalError, "presigned url expired"))
			return
		}
		if errors.Is(err, filestore.ErrPresignOpMismatch) {
			ctx.JSON(http.StatusForbidden, dto.Error(dto.CodeInternalError, "operation mismatch"))
			return
		}
		if errors.Is(err, filestore.ErrPresignKeyMismatch) {
			ctx.JSON(http.StatusForbidden, dto.Error(dto.CodeInternalError, "key mismatch"))
			return
		}
		ctx.JSON(http.StatusForbidden, dto.Error(dto.CodeInternalError, "invalid presigned token"))
		return
	}

	contentType := ctx.GetHeader("Content-Type")
	result, err := filestore.HandlePresignedPut(ctx.Request.Context(), bucket, key, ctx.Request.Body, contentType)
	if err != nil {
		ctx.JSON(http.StatusInternalServerError, dto.Error(dto.CodeInternalError, fmt.Sprintf("upload failed: %v", err)))
		return
	}

	ctx.JSON(http.StatusOK, dto.Success(map[string]string{
		"uri": result.Path.URI(),
	}))
}

// handlePresignedGet consumes a presigned download URL
// @Summary 预签名下载
// @Description 消费预签名下载 URL，验证 token 后返回文件内容
// @Tags Storage
// @Produce octet-stream
// @Param bucket path string true "存储桶名称"
// @Param key path string true "对象 key"
// @Param token query string true "预签名 token"
// @Param expires query string true "过期时间戳(秒)"
// @Success 200 {file} binary "文件内容"
// @Failure 400 {string} string "参数错误"
// @Failure 403 {string} string "预签名验证失败"
// @Failure 404 {string} string "对象不存在"
// @Failure 500 {string} string "内部错误"
// @Router /presigned/{bucket}/{key} [get]
func handlePresignedGet(ctx *gin.Context) {
	token := strings.TrimSpace(ctx.Query(presignTokenQuery))
	expires := strings.TrimSpace(ctx.Query(presignExpiresQuery))
	if token == "" || expires == "" {
		ctx.String(http.StatusBadRequest, "missing token or expires query parameter")
		return
	}

	bucket := strings.TrimSpace(ctx.Param("bucket"))
	key := strings.TrimPrefix(ctx.Param("key"), "/")

	if bucket == "" || key == "" {
		ctx.String(http.StatusBadRequest, "bucket and key are required")
		return
	}

	if err := filestore.VerifyPresignedToken(
		filestore.SignSecret(), bucket, key, "get", token, expires,
	); err != nil {
		if errors.Is(err, filestore.ErrPresignExpired) {
			ctx.String(http.StatusForbidden, "presigned url expired")
			return
		}
		if errors.Is(err, filestore.ErrPresignOpMismatch) {
			ctx.String(http.StatusForbidden, "operation mismatch")
			return
		}
		if errors.Is(err, filestore.ErrPresignKeyMismatch) {
			ctx.String(http.StatusForbidden, "key mismatch")
			return
		}
		ctx.String(http.StatusForbidden, "invalid presigned token")
		return
	}

	defer func() {
		if r := recover(); r != nil {
			ctx.String(http.StatusInternalServerError, "internal error")
		}
	}()
	body, info, err := filestore.HandlePresignedGet(ctx.Request.Context(), bucket, key)
	if err != nil {
		ctx.String(http.StatusNotFound, "object not found")
		return
	}
	defer body.Close()

	if info.ContentType != "" {
		ctx.Header("Content-Type", info.ContentType)
	}
	ctx.Header("Content-Disposition", fmt.Sprintf(`attachment; filename="%s"`, key))
	if info.Size > 0 {
		ctx.Header("Content-Length", fmt.Sprintf("%d", info.Size))
	}
	ctx.Status(http.StatusOK)
	if _, err := io.Copy(ctx.Writer, body); err != nil {
		ctx.Error(err)
	}
}
