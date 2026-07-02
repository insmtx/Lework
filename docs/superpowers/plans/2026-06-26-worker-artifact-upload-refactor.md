# Worker 产物上传流程改造 Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Worker 从 Server 获取 storage 配置（scheme/bucket），自主构造 key 路径并使用 storage.BuildURI 构造 URI，Server 只负责生成 presign URL。

**Architecture:** 新增 GET `/v1/internal/artifacts/storage-config` 接口，改造 `PresignArtifactUploadRequest` 为 Worker 传入 bucket 和 key。Worker 端在 `Record` 时获取 storage config，用 `snowflake.GenerateIDBase58()` 生成随机 ID 构造 key 路径，用 `storage.BuildURI` 构造 URI。

**Tech Stack:** Go 1.26, Gin (HTTP), GORM, `ygpkg/storage-go` v0.0.7, `ygpkg/yg-go/encryptor/snowflake`

---

### Task 1: 改造 contract 类型定义

**Files:**
- Modify: `backend/internal/api/contract/project_type.go:113-128`
- Modify: `backend/internal/api/contract/project.go:32`

- [ ] **Step 1: 改造 PresignArtifactUploadRequest 和 PresignArtifactUploadResponse**

编辑 `backend/internal/api/contract/project_type.go`，替换 113-128 行：

```go
// PresignArtifactUploadRequest Worker 请求产物文件预签名上传 URL 的请求体
type PresignArtifactUploadRequest struct {
	Bucket   string `json:"bucket" binding:"required"`
	Key      string `json:"key" binding:"required"`
	Filename string `json:"filename" binding:"required"`
	Sha256   string `json:"sha256"`
	MimeType string `json:"mime_type"`
	FileSize int64  `json:"file_size"`
}

// PresignArtifactUploadResponse Worker 请求产物文件预签名上传 URL 的响应体
type PresignArtifactUploadResponse struct {
	UploadURL string `json:"upload_url"`
	ExpiresAt string `json:"expires_at"`
}

// StorageConfigResponse Worker 请求 storage 配置的响应体
type StorageConfigResponse struct {
	Scheme string `json:"scheme"`
	Bucket string `json:"bucket"`
}
```

- [ ] **Step 2: 更新 ProjectService 接口，新增 GetStorageConfig 方法**

编辑 `backend/internal/api/contract/project.go`，在接口末追加：

```go
	GetStorageConfig(ctx context.Context) (*StorageConfigResponse, error)
```

- [ ] **Step 3: 编译验证 contract 包无语法错误**

```bash
go build ./backend/internal/api/contract/
```
Expected: 编译通过，无错误。

- [ ] **Step 4: Commit**

```bash
git add backend/internal/api/contract/project_type.go backend/internal/api/contract/project.go
git commit -m "feat(artifact): 改造 Presign 请求/响应体，新增 StorageConfigResponse"
```

---

### Task 2: Server 端 service 层改造

**Files:**
- Modify: `backend/internal/service/project_service.go:723-762`

- [ ] **Step 1: 改造 PresignArtifactUpload 方法**

编辑 `backend/internal/service/project_service.go`，替换 723-762 行的 `PresignArtifactUpload` 函数：

```go
func (s *projectService) PresignArtifactUpload(ctx context.Context, req *contract.PresignArtifactUploadRequest) (*contract.PresignArtifactUploadResponse, error) {
	bucket := strings.TrimSpace(req.Bucket)
	if bucket == "" {
		return nil, errors.New("bucket is required")
	}
	key := strings.TrimSpace(req.Key)
	if key == "" {
		return nil, errors.New("key is required")
	}
	filename := strings.TrimSpace(req.Filename)
	if filename == "" {
		return nil, errors.New("filename is required")
	}

	defaultBucket := filestore.DefaultBucket()
	if bucket != defaultBucket {
		return nil, fmt.Errorf("bucket %q is not allowed, only %q is supported", bucket, defaultBucket)
	}

	uploadURL, expiresAt, err := filestore.PresignUpload(ctx, bucket, key)
	if err != nil {
		return nil, fmt.Errorf("generate presigned upload url: %w", err)
	}

	return &contract.PresignArtifactUploadResponse{
		UploadURL: uploadURL,
		ExpiresAt: expiresAt.Format(time.RFC3339),
	}, nil
}
```

- [ ] **Step 2: 新增 GetStorageConfig 方法**

在 `project_service.go` 的 service 接口实现中添加（放在 `PresignArtifactUpload` 方法之后）：

```go
func (s *projectService) GetStorageConfig(_ context.Context) (*contract.StorageConfigResponse, error) {
	scheme := "s3"
	if filestore.IsLocal() {
		scheme = "file"
	}
	return &contract.StorageConfigResponse{
		Scheme: scheme,
		Bucket: filestore.DefaultBucket(),
	}, nil
}
```

- [ ] **Step 3: 清理无用的 import**

确认 `PresignArtifactUpload` 原来依赖的 `db`、`filepath` 等 import 如果不再被用到了需要移除。检查编译：

```bash
go build ./backend/internal/service/
```

Expected: 编译通过，无错误。

- [ ] **Step 4: 更新 mock，新增 GetStorageConfig 方法**

编辑 `backend/internal/service/project_file_handler_gin_test.go`，为 `mockProjectServiceForAddFile` 新增 `GetStorageConfig` 方法。

在 struct 定义（~行 33 之后）新增字段：

```go
	getStorageConfigFn func(ctx context.Context) (*contract.StorageConfigResponse, error)
```

在 `PresignArtifactUpload` 方法之后新增：

```go
func (m *mockProjectServiceForAddFile) GetStorageConfig(ctx context.Context) (*contract.StorageConfigResponse, error) {
	if m.getStorageConfigFn != nil {
		return m.getStorageConfigFn(ctx)
	}
	return nil, nil
}
```

- [ ] **Step 5: Commit**

```bash
git add backend/internal/service/project_service.go backend/internal/service/project_file_handler_gin_test.go
git commit -m "feat(artifact): 改造 PresignArtifactUpload 并新增 GetStorageConfig"
```

---

### Task 3: Server 端 handler + 路由注册

**Files:**
- Modify: `backend/internal/api/handler/artifact_presign_handler.go`
- Modify: `backend/internal/api/router.go:109`

- [ ] **Step 1: 新增 GetStorageConfig handler**

在 `backend/internal/api/handler/artifact_presign_handler.go` 中添加新的 handler 方法：

```go
// GetStorageConfig returns the storage configuration (scheme and bucket) for Worker.
func (h *ProjectFileHandler) GetStorageConfig(ctx *gin.Context) {
	resp, err := h.service.GetStorageConfig(ctx.Request.Context())
	if err != nil {
		logs.ErrorContextf(ctx, "get storage config failed: %v", err)
		ctx.JSON(http.StatusInternalServerError, dto.Error(dto.CodeInternalError, err.Error()))
		return
	}
	ctx.JSON(http.StatusOK, dto.Success(resp))
}
```

- [ ] **Step 2: 注册 GET storage-config 路由**

编辑 `backend/internal/api/router.go`，在 109 行（presign-upload POST 路由）附近新增 GET 路由：

```go
		v1.POST("/internal/artifacts/presign-upload", projectFileHandler.PresignArtifactUpload)
		v1.GET("/internal/artifacts/storage-config", projectFileHandler.GetStorageConfig)
```

- [ ] **Step 3: 编译验证**

```bash
go build ./backend/internal/api/...
```

Expected: 编译通过，无错误。

- [ ] **Step 4: Commit**

```bash
git add backend/internal/api/handler/artifact_presign_handler.go backend/internal/api/router.go
git commit -m "feat(artifact): 新增 storage-config 接口和路由"
```

---

### Task 4: ServerClient 新增 GetStorageConfig 方法

**Files:**
- Modify: `backend/internal/worker/client/server_client.go:141-147`

- [ ] **Step 1: 新增 GetStorageConfig 客户端方法**

在 `server_client.go` 的 `PresignArtifactUpload` 方法之后添加：

```go
func (c *ServerClient) GetStorageConfig(ctx context.Context) (*contract.StorageConfigResponse, error) {
	var resp contract.StorageConfigResponse
	if err := c.doGet(ctx, "/v1/internal/artifacts/storage-config", &resp); err != nil {
		return nil, err
	}
	return &resp, nil
}
```

- [ ] **Step 2: 编译验证**

```bash
go build ./backend/internal/worker/client/
```

Expected: 编译通过，无错误。

- [ ] **Step 3: Commit**

```bash
git add backend/internal/worker/client/server_client.go
git commit -m "feat(artifact): ServerClient 新增 GetStorageConfig 方法"
```

---

### Task 5: Worker 端 artifact.go 上传流程改造

**Files:**
- Modify: `backend/internal/runtime/lifecycle/steps/artifact.go`

- [ ] **Step 1: 添加新的 import**

编辑 `artifact.go` 的 import 块，新增 `snowflake` 和 `storage` 的引用：

在现有 import 中：
- 新增 `"github.com/ygpkg/yg-go/encryptor/snowflake"`（在 `"github.com/ygpkg/yg-go/logs"` 之后）
- 新增 `"github.com/ygpkg/storage-go"`（在所有 import 之后、内部 import 之前）

最终 import 块变为：

```go
import (
	"bytes"
	"context"
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/ygpkg/yg-go/encryptor/snowflake"
	"github.com/ygpkg/yg-go/logs"
	"github.com/ygpkg/storage-go"

	"github.com/insmtx/Lework/backend/internal/agent"
	"github.com/insmtx/Lework/backend/internal/api/contract"
	"github.com/insmtx/Lework/backend/internal/runtime/events"
	"github.com/insmtx/Lework/backend/internal/worker/client"
	"github.com/insmtx/Lework/backend/internal/worker/identity"
	agentworkspace "github.com/insmtx/Lework/backend/internal/workspace"
	"github.com/insmtx/Lework/backend/types"
)
```

- [ ] **Step 2: 改造 Record 方法，获取 storage config**

编辑 `WorkspaceArtifactRecorder.Record` 方法（约 90-104 行），改造为：

```go
	serverAddr := identity.ServerAddr()
	serverOrgID := identity.OrgID()
	projectPublicID := strings.TrimSpace(req.Workspace.ProjectID)
	if serverAddr != "" && serverOrgID > 0 && projectPublicID != "" {
		srv := client.NewServerClient(serverAddr)

		storageCfg, cfgErr := srv.GetStorageConfig(ctx)
		if cfgErr != nil {
			logs.WarnContextf(ctx, "get storage config from server: %v", cfgErr)
			storageCfg = nil
		}

		for i, record := range records {
			storageURI, err := uploadArtifactToServer(ctx, srv, projectPublicID, record, storageCfg)
			if err != nil {
				logs.WarnContextf(ctx, "upload artifact %s to server: %v", record.RelativePath, err)
				continue
			}
			payloads[i].StorageURI = storageURI
		}
	}
```

- [ ] **Step 3: 改造 uploadArtifactToServer 函数签名和实现**

替换 `uploadArtifactToServer` 函数（约 109-152 行）为：

```go
func uploadArtifactToServer(ctx context.Context, srv *client.ServerClient, projectPublicID string, record agentworkspace.ArtifactRecord, storageCfg *contract.StorageConfigResponse) (string, error) {
	absolute, err := agentworkspace.SafeJoin("", record.RelativePath)
	if err != nil {
		return "", err
	}
	data, err := os.ReadFile(absolute)
	if err != nil {
		return "", fmt.Errorf("read artifact file: %w", err)
	}

	randomID := snowflake.GenerateIDBase58()
	orgID := identity.OrgID()
	key := fmt.Sprintf("artifacts/%d/%s/%s/%s", orgID, projectPublicID, randomID, record.Filename)

	bucket := ""
	scheme := "s3"
	if storageCfg != nil {
		bucket = storageCfg.Bucket
		scheme = storageCfg.Scheme
	}

	storageURI := ""
	if bucket != "" {
		uri, err := storage.BuildURI(scheme, bucket, key)
		if err != nil {
			return "", fmt.Errorf("build storage uri: %w", err)
		}
		storageURI = uri
	}

	reqBody := contract.PresignArtifactUploadRequest{
		Bucket:   bucket,
		Key:      key,
		Filename: record.Filename,
		Sha256:   record.Sha256,
		MimeType: record.MimeType,
		FileSize: record.FileSize,
	}

	respData, err := srv.PresignArtifactUpload(ctx, &reqBody)
	if err != nil {
		return "", fmt.Errorf("request presign upload: %w", err)
	}

	putReq, err := http.NewRequestWithContext(ctx, http.MethodPut, respData.UploadURL, bytes.NewReader(data))
	if err != nil {
		return "", err
	}
	putReq.Header.Set("Content-Type", record.MimeType)
	putReq.ContentLength = record.FileSize

	putResp, err := http.DefaultClient.Do(putReq)
	if err != nil {
		return "", fmt.Errorf("upload artifact file: %w", err)
	}
	defer putResp.Body.Close()

	if putResp.StatusCode >= 300 {
		body, _ := io.ReadAll(io.LimitReader(putResp.Body, 4096))
		return "", fmt.Errorf("upload artifact file returned %d: %s", putResp.StatusCode, strings.TrimSpace(string(body)))
	}

	return storageURI, nil
}
```

- [ ] **Step 4: 编译验证**

```bash
go build ./backend/internal/runtime/lifecycle/steps/
```

Expected: 编译通过，无错误。

- [ ] **Step 5: Commit**

```bash
git add backend/internal/runtime/lifecycle/steps/artifact.go
git commit -m "feat(artifact): Worker 端自主构造 key 和 URI，基于 Server 的 storage config"
```

---

### Task 6: 全量编译 + 测试验证

**Files:** 无新增，验证改动完整性

- [ ] **Step 1: 全量编译**

```bash
go build ./...
```

Expected: 编译通过，无错误。

- [ ] **Step 2: 运行现有测试**

```bash
go test ./backend/internal/service/ -run TestAddProjectFile -v
go test ./backend/internal/runtime/lifecycle/... -v
go test ./backend/internal/api/handler/ -run TestStatic -v
```

Expected: 所有现有测试通过。

- [ ] **Step 3: go vet 检查**

```bash
go vet ./backend/...
```

Expected: 无警告。

- [ ] **Step 4: Commit（如有 swagger 变更）**

如果 swagger 文档因为 contract 变更需要重新生成：

```bash
make swagger
git add docs/swagger/
git commit -m "docs(swagger): 更新 API 文档"
```

---

### Task 7: 清理 Server 端旧代码中无用的 import

**Files:**
- Modify: `backend/internal/service/project_service.go`

- [ ] **Step 1: 检查并移除无用 import**

`PresignArtifactUpload` 原来使用了 `db`、`filepath`、`fmt.Sprintf("s3://...")` 等。确认这些 import 在 `project_service.go` 中是否仍被其他方法使用。

如果不再使用 `db`，可能需要在 `project_service.go` 的 import 中移除。先检查：

```bash
cd backend && go build ./internal/service/
```

如果编译通过且无 unused import 警告，说明已有方法使用这些 import，无需改动。

- [ ] **Step 2: 最终提交**

```bash
git add -A
git commit -m "chore(artifact): 清理无用依赖"
```
