package handler

import (
	"net/http"
	"strings"
	"time"

	"github.com/gin-gonic/gin"

	"github.com/insmtx/Lework/backend/internal/api/dto"
	"github.com/insmtx/Lework/backend/internal/infra/filestore"
)

// RegisterStaticRoutes registers static resource presign routes
func RegisterStaticRoutes(r gin.IRouter) {
	r.GET("/:bucket/*key", handlePresign)
	r.GET("/storage-config", handleGetStorageConfig)
}

// handlePresign generates a presigned upload or download URL based on the operation query parameter.
// @Summary 生成预签名 URL
// @Description 根据 operation=upload|download 生成预签名上传或下载 URL
// @Tags Storage
// @Produce plain
// @Param operation query string true "操作类型: upload 或 download"
// @Param bucket path string true "存储桶名称"
// @Param key path string true "对象 key"
// @Success 200 {string} string "预签名 URL"
// @Failure 400 {string} string "参数错误"
// @Failure 500 {string} string "生成预签名 URL 失败"
// @Router /static/{bucket}/{key} [get]
func handlePresign(ctx *gin.Context) {
	op := strings.TrimSpace(ctx.Query("operation"))
	if op != "upload" && op != "download" {
		ctx.String(http.StatusBadRequest, "operation query parameter must be 'upload' or 'download'")
		return
	}

	bucket := strings.TrimSpace(ctx.Param("bucket"))
	key := strings.TrimPrefix(ctx.Param("key"), "/")

	if bucket == "" || key == "" {
		ctx.String(http.StatusBadRequest, "bucket and key are required")
		return
	}

	var url string
	var expiresAt time.Time
	var err error
	if op == "upload" {
		url, expiresAt, err = filestore.PresignUpload(ctx.Request.Context(), bucket, key)
	} else {
		url, expiresAt, err = filestore.PresignDownload(ctx.Request.Context(), bucket, key)
	}
	if err != nil {
		ctx.String(http.StatusInternalServerError, "failed to generate presigned url")
		return
	}

	ctx.Header("X-Presign-Expires-At", expiresAt.Format(time.RFC3339))
	ctx.String(http.StatusOK, url)
}

// handleGetStorageConfig returns the storage configuration (scheme and bucket).
func handleGetStorageConfig(ctx *gin.Context) {
	scheme := "s3"
	if filestore.IsLocal() {
		scheme = "file"
	}
	ctx.JSON(http.StatusOK, dto.Success(map[string]string{
		"scheme": scheme,
		"bucket": filestore.DefaultBucket(),
	}))
}
