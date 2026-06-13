package handler

import (
	"net/http"
	"strings"
	"time"

	"github.com/gin-gonic/gin"

	"github.com/insmtx/Leros/backend/internal/infra/filestore"
)

const presignQueryParam = "presign"

// RegisterStaticRoutes registers static resource presign routes
func RegisterStaticRoutes(r gin.IRouter) {
	r.PUT("/:bucket/*key", handlePresignUpload)
	r.GET("/:bucket/*key", handlePresignDownload)
}

// handlePresignUpload generates a presigned upload URL
// @Summary 生成预签名上传 URL
// @Description 为指定 bucket/key 生成一个带过期时间的预签名上传 URL
// @Tags Storage
// @Produce plain
// @Param presign query string true "触发预签名标识(任意值)"
// @Param bucket path string true "存储桶名称"
// @Param key path string true "对象 key"
// @Success 200 {string} string "预签名上传 URL"
// @Failure 400 {string} string "参数错误"
// @Failure 500 {string} string "生成预签名 URL 失败"
// @Router /static/{bucket}/{key} [put]
func handlePresignUpload(ctx *gin.Context) {
	if !isPresignRequest(ctx) {
		ctx.String(http.StatusBadRequest, "missing presign query parameter")
		return
	}

	bucket := strings.TrimSpace(ctx.Param("bucket"))
	key := strings.TrimPrefix(ctx.Param("key"), "/")

	if bucket == "" || key == "" {
		ctx.String(http.StatusBadRequest, "bucket and key are required")
		return
	}

	url, expiresAt, err := filestore.PresignUpload(ctx.Request.Context(), bucket, key)
	if err != nil {
		ctx.String(http.StatusInternalServerError, "failed to generate presigned upload URL")
		return
	}

	ctx.Header("X-Presign-Expires-At", expiresAt.Format(time.RFC3339))
	ctx.String(http.StatusOK, url)
}

// handlePresignDownload generates a presigned download URL
// @Summary 生成预签名下载 URL
// @Description 为指定 bucket/key 生成一个带过期时间的预签名下载 URL
// @Tags Storage
// @Produce plain
// @Param presign query string true "触发预签名标识(任意值)"
// @Param bucket path string true "存储桶名称"
// @Param key path string true "对象 key"
// @Success 200 {string} string "预签名下载 URL"
// @Failure 400 {string} string "参数错误"
// @Failure 500 {string} string "生成预签名 URL 失败"
// @Router /static/{bucket}/{key} [get]
func handlePresignDownload(ctx *gin.Context) {
	if !isPresignRequest(ctx) {
		ctx.String(http.StatusBadRequest, "missing presign query parameter")
		return
	}

	bucket := strings.TrimSpace(ctx.Param("bucket"))
	key := strings.TrimPrefix(ctx.Param("key"), "/")

	if bucket == "" || key == "" {
		ctx.String(http.StatusBadRequest, "bucket and key are required")
		return
	}

	url, expiresAt, err := filestore.PresignDownload(ctx.Request.Context(), bucket, key)
	if err != nil {
		ctx.String(http.StatusInternalServerError, "failed to generate presigned download URL")
		return
	}

	ctx.Header("X-Presign-Expires-At", expiresAt.Format(time.RFC3339))
	ctx.String(http.StatusOK, url)
}

func isPresignRequest(ctx *gin.Context) bool {
	return strings.TrimSpace(ctx.Query(presignQueryParam)) != ""
}
