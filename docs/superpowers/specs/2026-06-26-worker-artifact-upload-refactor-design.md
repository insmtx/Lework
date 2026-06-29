# Worker 产物上传流程改造设计

> 日期：2026-06-26
> 状态：已确认

## 背景

当前 Worker 端上传产物文件的流程中，Server 端硬编码了 bucket 和 key 路径构造逻辑，Worker 端完全被动且不了解 storage 配置。URI 使用 `fmt.Sprintf` 硬拼接而非 `storage-go` 库构造。

## 目标

1. Worker 从 Server 获取 bucket/scheme，不需要自身配置 storage
2. Worker 用 `snowflake.GenerateIDBase58()` 构建 key（路径结构 `artifacts/{orgID}/{projectPublicID}/{snowflakeID}/{filename}`）
3. Worker 用 `storage.BuildURI(scheme, bucket, key)` 构造 URI 返回给 Server
4. Worker 返回 URI + 文件名即可

## 方案

### 新增接口：storage-config

```
GET /v1/internal/artifacts/storage-config

Response:
{
  "code": 0,
  "data": {
    "scheme": "s3",
    "bucket": "dev-bucket"
  }
}
```

- scheme：根据 `filestore.IsLocal()` 推导，local → `"file"`，其他 → `"s3"`
- bucket：来自 `filestore.DefaultBucket()`

### Presign 接口改造

**PresignArtifactUploadRequest** 改为由 Worker 传入 bucket 和 key：

```go
type PresignArtifactUploadRequest struct {
    Bucket   string `json:"bucket" binding:"required"`
    Key      string `json:"key" binding:"required"`
    Filename string `json:"filename" binding:"required"`
    Sha256   string `json:"sha256"`
    MimeType string `json:"mime_type"`
    FileSize int64  `json:"file_size"`
}
```

去掉：`OrgID`、`ProjectPublicID`

**PresignArtifactUploadResponse** 去掉 `StorageURI`：

```go
type PresignArtifactUploadResponse struct {
    UploadURL string `json:"upload_url"`
    ExpiresAt string `json:"expires_at"`
}
```

**Service 改造**：`project_service.go` 的 `PresignArtifactUpload` 直接用 `req.Bucket`、`req.Key` 调用 `filestore.PresignUpload`，去掉 key 构造、project 查询、硬拼接 URI 逻辑。校验 `req.Bucket` 必须等于 `filestore.DefaultBucket()`。

### Worker 端改造

调用链：`WorkspaceArtifactRecorder.Record → uploadArtifactToServer → ServerClient.PresignArtifactUpload`

改造后流程：

1. `Record` 方法入口调用 `srv.GetStorageConfig(ctx)` 获取 scheme、bucket
2. 每次 artifact 上传时：
   - 用 `snowflake.GenerateIDBase58()` 生成 randomID
   - 构造 key：`fmt.Sprintf("artifacts/%d/%s/%s/%s", identity.OrgID(), projectPublicID, randomID, record.Filename)`
   - 用 `storage.BuildURI(scheme, bucket, key)` 构造 URI
   - 请求 presign 时传入 bucket、key、filename 等
   - 上传完成后返回构造好的 URI
3. URI 写入 `ArtifactPayload.StorageURI`，随事件通知 Server

### Server 端消费不变

`session_artifact_declared.go` 中 `PersistDeclaredArtifact` 和 `recordArtifactUpload` 不变——它们只消费 `StorageURI`、`Filename` 等已有字段，不关心 URI 是谁构造的。

## 涉及文件

| 文件 | 改动 |
|------|------|
| `backend/internal/api/contract/project_type.go` | Presign 请求/响应体改造，新增 StorageConfigResponse |
| `backend/internal/api/contract/project.go` | ProjectService 接口新增 GetStorageConfig |
| `backend/internal/api/router.go` | 注册 storage-config 路由 |
| `backend/internal/api/handler/artifact_presign_handler.go` | 新增 GetStorageConfig handler，PresignArtifactUpload 请求变更 |
| `backend/internal/service/project_service.go` | PresignArtifactUpload 简化，新增 GetStorageConfig |
| `backend/internal/worker/client/server_client.go` | 新增 GetStorageConfig 方法 |
| `backend/internal/runtime/lifecycle/steps/artifact.go` | Worker 端上传流程改造：获取 storage config、构造 key、构造 URI |
