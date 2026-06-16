# Leros Presigned URL 使用文档

## 概述

Presigned URL（预签名 URL）允许客户端直接与存储层交互，绕过后端服务完成文件上传和下载，减少服务端带宽压力。当前实现基于 `storage-go` 库，支持 `local` 和 `minio` 两种驱动。

local 驱动下 presigned URL 由 Leros 服务端生成和消费（签名 + 验证均通过 Leros）。minio 驱动下直接使用 S3 原生 presigned URL，由 MinIO 服务端处理。

---

## 架构

```
┌──────────┐    ① PUT /v1/static/:bucket/*key?presign=1    ┌──────────┐
│          │ ─────────────────────────────────────────────> │          │
│  客户端   │    ② 返回 presigned URL                        │  Leros   │
│          │ <───────────────────────────────────────────── │  服务端   │
│          │                                               │          │
│          │    ③ PUT/GET /:bucket/*key?token=...&expires=..│          │
│          │ <═══════════════════════════════════════════> │  存储层   │
└──────────┘                                               └──────────┘
```

**流程：**

1. 客户端向 Leros 请求预签名 URL（接口 `/v1/static/:bucket/*key?presign=1`）
2. Leros 返回带签名的 URL（`/:bucket/*key?token=...&expires=...`），有效期 1 小时
3. 客户端使用该 URL 向 Leros 发起上传/下载，Leros 验证 token 后操作底层存储

> **鉴权说明：** 生成端接口（`/v1/static/:bucket/*key`）需要鉴权，支持两种方式：
> - **JWT 登录态**：通过 `Authorization: Bearer <jwt>` 传递
> - **静态 API Key**：通过 `X-Static-Api-Key` header 传递，默认值 `leros-static-api-key`，可通过配置 `storage.static_api_key` 修改
>
> 两种方式任一通过即可。消费端接口（`/:bucket/*key`）无需鉴权，仅依赖 URL 自带的 HMAC 签名。

---

## 配置

### 配置结构 `backend/config/config.go:46-57`

```yaml
# config.yaml
storage:
  driver: local                # 驱动类型: local | minio
  endpoint: ""                 # minio 地址（minio 驱动必填）
  access_key: ""               # minio access key（minio 驱动必填）
  secret_key: ""               # minio secret key（minio 驱动必填）
  use_ssl: false               # 是否启用 SSL
  bucket: dev-bucket           # bucket 名称（默认 dev-bucket）
  base_url: http://localhost:8080  # 服务端对外访问基础 URL（用于生成 presigned URL 的 host 部分）
  url_style: ""                # URL 风格（path / virtual-hosted）
  local_dir: leros-storage     # 本地存储目录（local 驱动）
  sign_secret: leros-local-presign  # 预签名 HMAC-SHA256 密钥
  static_api_key: "leros-static-api-key"  # 预签名生成端 API 鉴权密钥（可选，默认 leros-static-api-key）
```

### 环境变量（无配置时的默认行为）

| 变量名                   | 说明         | 默认值                           |
| ------------------------ | ------------ | -------------------------------- |
| `LEROS_STORAGE_LOCAL_DIR` | 本地存储目录 | 可执行文件同级 `leros-storage/`   |

不配置 `storage` 字段且未设置环境变量时，自动使用 local 驱动，目录为 `$PWD/leros-storage`。

---

## API 接口

> **注意：** 以下生成端接口（步骤 1、3）需要鉴权。支持 JWT Bearer Token（`Authorization: Bearer <jwt>`）或静态 API Key（`X-Static-Api-Key: <key>`），任一通过即可。消费端接口（步骤 2、4）无需鉴权。

### 1. 获取预签名上传 URL

```
PUT /v1/static/:bucket/*key?presign=1
```

**请求头（鉴权二选一）：**

| 请求头                 | 必填 | 说明                         |
| ---------------------- | ---- | ---------------------------- |
| `Authorization`        | 否   | Bearer Token，JWT 登录态     |
| `X-Static-Api-Key`     | 否   | 静态 API Key，默认 `leros-static-api-key` |

**参数：**

| 参数      | 位置  | 必填 | 说明                         |
| --------- | ----- | ---- | ---------------------------- |
| `bucket`  | path  | 是   | 存储桶名称                   |
| `key`     | path  | 是   | 对象路径，如 `path/to/file.txt` |
| `presign` | query | 是   | 任意非空值即触发预签名模式   |

**请求示例：**

```bash
# 方式一：JWT 登录态
curl -s -X PUT "http://localhost:8080/v1/static/dev-bucket/hello.txt?presign=1" \
  -H "Authorization: Bearer <your-jwt-token>"

# 方式二：静态 API Key（需在 config.yaml 中配置 static_api_key）
curl -s -X PUT "http://localhost:8080/v1/static/dev-bucket/hello.txt?presign=1" \
  -H "X-Static-Api-Key: <your-api-key>"
```

**成功响应 (200)：**

```
http://localhost:8080/dev-bucket/hello.txt?expires=1781319910&token=eyJrZXkiOiJkZXYtYnVja2V0L2hlbGxvLnR4dCIsIm9wIjoicHV0IiwiZXhwIjoxNzgxMzE5OTEwfQ%3D%3D.7obmTC_lgvt32ZqsMO17jHqS5rClwm-hiZv-3WYdCFQ%3D
```

- `Body`：完整的预签名上传 URL
- `X-Presign-Expires-At`：URL 过期时间（RFC 3339，UTC+8）
- 有效期：1 小时
- URL 格式：`{base_url}/{bucket}/{key}?expires={unix_timestamp}&token={base64_payload}.{hmac_signature}`

> **注意：** 返回的 URL 路径不包含 `/v1` 前缀，因为消费端路由注册在根路径。如果 `base_url` 不是 `http://localhost:8080`，URL 的 host 部分会对应变化。

---

### 2. 使用 presigned URL 上传文件

```
PUT /:bucket/*key?token=...&expires=...
```

将步骤 1 返回的完整 URL 用于上传。服务端验证 token 后写入文件。

**curl 示例：**

```bash
# 用步骤 1 返回的完整 URL 替换 <PRESIGNED_URL>
curl -X PUT "<PRESIGNED_URL>" --data-binary @./local-file.txt
```

**Postman 配置：**

| 配置项 | 值 |
|--------|-----|
| Method | `PUT` |
| URL | 步骤 1 返回的完整 presigned URL |
| Body | ○ **binary** |
| Select File | 选取要上传的本地文件 |

Body 模式说明：

| 模式 | 是否可用 | 原因 |
|------|----------|------|
| **binary** | ✅ 正确 | 直接发送文件原始字节，与 curl `--data-binary` 等价 |
| form-data | ❌ 错误 | 会添加 multipart 边界和额外头部，存储内容与原始文件不一致 |
| raw | ❌ 错误 | 把文件当文本处理，二进制文件会损坏 |
| x-www-form-urlencoded | ❌ 错误 | 会做 URL 编码转换 |

**成功响应 (200)：**

```json
{
  "code": 0,
  "message": "success",
  "data": {
    "uri": "file://dev-bucket/hello.txt"
  }
}
```

- `data.uri`：存储对象的 URI 标识，格式为 `{scheme}://{bucket}/{key}`（local 驱动为 `file://`，S3 驱动为 `s3://`）

---

### 3. 获取预签名下载 URL

```
GET /v1/static/:bucket/*key?presign=1
```

**请求头（鉴权二选一，同步骤 1。）**

**参数：**

| 参数      | 位置  | 必填 | 说明                         |
| --------- | ----- | ---- | ---------------------------- |
| `bucket`  | path  | 是   | 存储桶名称                   |
| `key`     | path  | 是   | 对象路径                     |
| `presign` | query | 是   | 任意非空值即触发预签名模式   |

**请求示例：**

```bash
# 方式一：JWT 登录态
curl -s "http://localhost:8080/v1/static/dev-bucket/hello.txt?presign=1" \
  -H "Authorization: Bearer <your-jwt-token>"

# 方式二：静态 API Key
curl -s "http://localhost:8080/v1/static/dev-bucket/hello.txt?presign=1" \
  -H "X-Static-Api-Key: <your-api-key>"
```

**成功响应 (200)：** 返回格式同步骤 1，`op` 字段为 `get`。

---

### 4. 使用 presigned URL 下载文件

```
GET /:bucket/*key?token=...&expires=...
```

将步骤 3 返回的完整 URL 用于下载。服务端验证 token 后返回文件内容。

**curl 示例：**

```bash
# 用步骤 3 返回的完整 URL 替换 <PRESIGNED_URL>
curl "<PRESIGNED_URL>" -o result.txt
```

---

### 5. 错误响应

**生成阶段（/v1/static/*）：**

| HTTP 状态码 | Body                                          | 触发条件              |
| ----------- | --------------------------------------------- | --------------------- |
| 401         | `{"error":"unauthorized"}`                    | 鉴权失败（JWT 无效且 API Key 不匹配） |
| 400         | `missing presign query parameter`             | 未携带 `presign` 参数 |
| 400         | `bucket and key are required`                 | bucket 或 key 为空    |
| 500         | `failed to generate presigned upload URL`     | storage 层生成失败    |
| 500         | `failed to generate presigned download URL`   | storage 层生成失败    |

**消费阶段（/:bucket/*key）：**

| HTTP 状态码 | Body                                          | 触发条件                  |
| ----------- | --------------------------------------------- | ------------------------- |
| 400         | `{"code":40001,"message":"missing token or expires query parameter"}` | 未携带 token 或 expires   |
| 400         | `{"code":40001,"message":"bucket and key are required"}` | bucket 或 key 为空        |
| 403         | `{"code":50001,"message":"presigned url expired"}` | token 已过期              |
| 403         | `{"code":50001,"message":"operation mismatch"}` | 用 PUT 的 token 做 GET 或反之 |
| 403         | `{"code":50001,"message":"key mismatch"}`      | key 不匹配                |
| 403         | `{"code":50001,"message":"invalid presigned token"}` | token 签名验证失败        |
| 404         | `{"code":50001,"message":"object not found"}`  | 文件不存在                |
| 500         | `{"code":50001,"message":"upload failed: ..."}`| storage 写入失败          |

---

## 完整使用流程

以下以 `dev-bucket` 下的 `projects/123/assets/logo.png` 为例，演示上传和下载的完整流程。

`key` 支持任意多层级路径（如 `projects/123/assets/logo.png`、`a/b/c/d.txt`），斜杠 `/` 会保留为路径分隔符。

### 上传流程

**步骤 1：获取预签名上传 URL**

```bash
# 需要鉴权（JWT 或 API Key 二选一）
curl -s -X PUT "http://localhost:8080/v1/static/dev-bucket/projects/123/assets/logo.png?presign=1" \
  -H "X-Static-Api-Key: <your-api-key>"
```

返回示例：
```
http://localhost:8080/dev-bucket/projects/123/assets/logo.png?expires=1781320337&token=eyJrZXkiOiJkZXYtYnVja2V0L3Byb2plY3RzLzEyMy9hc3NldHMvbG9nby5wbmciLCJvcCI6InB1dCIsImV4cCI6MTc4MTMyMDMzN30%3D.6XRX7jpjY_dt70ujMHXh173TRHzSr8-2WoPrx0MqWKQ%3D
```

**步骤 2：使用返回的 presigned URL 上传文件**

```bash
# 准备测试文件
echo "logo content" > /tmp/logo.png

# 用步骤 1 返回的完整 URL 替换 <PRESIGNED_URL>
curl -X PUT "<PRESIGNED_URL>" --data-binary @/tmp/logo.png
# 返回: {"code":0,"message":"success","data":{"uri":"file://dev-bucket/projects/123/assets/logo.png"}}
```

Postman 配置：

| 配置项 | 值 |
|--------|-----|
| Method | `PUT` |
| URL | 步骤 1 返回的完整 presigned URL |
| Body | ○ **binary** |
| Select File | 选取本地文件 |

> **提示：** 步骤 1 返回的 URL 有时效（1 小时），请及时使用。

### 下载流程

**步骤 3：获取预签名下载 URL**

```bash
curl -s "http://localhost:8080/v1/static/dev-bucket/projects/123/assets/logo.png?presign=1" \
  -H "X-Static-Api-Key: <your-api-key>"
```

返回示例：
```
http://localhost:8080/dev-bucket/projects/123/assets/logo.png?expires=1781320343&token=eyJrZXkiOiJkZXYtYnVja2V0L3Byb2plY3RzLzEyMy9hc3NldHMvbG9nby5wbmciLCJvcCI6ImdldCIsImV4cCI6MTc4MTMyMDM0M30%3D.Pmw60lvritMJzjdCyF22kWeFuXiQP2KkPvkxH1CAHLo%3D
```

**步骤 4：使用返回的 presigned URL 下载文件**

```bash
# 用步骤 3 返回的完整 URL 替换 <PRESIGNED_URL>
curl "<PRESIGNED_URL>" -o /tmp/logo-downloaded.png

# 验证内容
cat /tmp/logo-downloaded.png
# 输出: logo content
```

---

## Token 结构

presigned URL 的 `token` 参数格式为 `{payload}.{signature}`：

```
token = base64url(json({key, op, exp})) . base64url(hmac_sha256(sign_secret, payload_b64))
```

其中：
- `key`：`{bucket}/{key}`，如 `dev-bucket/projects/123/assets/logo.png`
- `op`：`put` 或 `get`
- `exp`：过期时间的 Unix 时间戳（UTC）

服务端验证时会检查：
1. token 格式和签名有效性
2. `key` 与请求的 bucket+key 是否匹配
3. `op` 与请求的 HTTP method 是否匹配（PUT ↔ put，GET ↔ get）
4. `exp` 是否超时

---

## Go 内部调用

如果需要在后端代码中直接生成预签名 URL（而非通过 HTTP 接口），可使用以下函数：

### `filestore.PresignUpload`

```go
// backend/internal/infra/filestore/presign.go:11
func PresignUpload(ctx context.Context, bucket, key string) (string, time.Time, error)
```

返回预签名上传 URL 和过期时间。

### `filestore.PresignDownload`

```go
// backend/internal/infra/filestore/presign.go:22
func PresignDownload(ctx context.Context, bucket, key string) (string, time.Time, error)
```

返回预签名下载 URL 和过期时间。

### `filestore.PresignDownloadByPublicID`

```go
// backend/internal/infra/filestore/upload.go:123
func PresignDownloadByPublicID(ctx context.Context, db *gorm.DB, orgID uint, publicID string, ttl time.Duration) (string, *types.FileUpload, error)
```

通过数据库中的 `FileUpload.PublicID` 查找文件记录并生成预签名下载 URL，支持自定义 TTL。

> **注意：** 此函数无对应 HTTP 端点，仅供内部 Go 代码调用。

### `filestore.VerifyPresignedToken`

```go
// backend/internal/infra/filestore/presign_verify.go:41
func VerifyPresignedToken(signSecret, bucket, key, op, tokenStr, expiresStr string) error
```

验证 presigned token 的合法性。

### `filestore.SignSecret`

```go
// backend/internal/infra/filestore/init.go:118
func SignSecret() string
```

返回当前 presigned 签名密钥。

### `filestore.StaticAPIKey`

```go
// backend/internal/infra/filestore/init.go:123
func StaticAPIKey() string
```

返回当前预签名生成端 API 鉴权密钥。

---

## 传统上传/下载对比

| 特性           | Presigned URL                                      | 传统 API                             |
| -------------- | -------------------------------------------------- | ------------------------------------ |
| 上传接口       | `PUT /v1/static/:bucket/*key?presign=1`            | `POST /v1/files/upload` (multipart)  |
| 下载接口       | `GET /v1/static/:bucket/*key?presign=1`            | `GET /v1/files/:id/download`         |
| 数据流经 Leros | 是（服务端验证 token 后代理读写 storage）           | 是（服务端中转）                     |
| 生成端认证     | JWT Bearer Token 或 X-Static-Api-Key（任一即可）   | Bearer Token                         |
| 消费端认证     | URL 自带 HMAC 签名（无需额外认证）                 | Bearer Token                         |
| 前端实现       | 未实现                                             | 已实现（`fileApi.ts`）               |
| 数据库记录     | 不创建 FileUpload 记录                             | 创建 FileUpload 记录                 |
| 适用场景       | CI/CD、外部系统、无需登录态的文件传输               | 前端用户常规上传下载                 |

---

## 重要限制

1. **HTTP 路由仅在 local 驱动时注册**（`router.go:117`）。minio/S3 驱动下使用原生 S3 presigned URL，Leros 不做中转。
2. **TTL 固定 1 小时**（`presign.go:8`），无法通过 API 参数自定义。
3. **无上传回调机制**：客户端通过 presigned URL 完成上传后，不创建数据库记录，服务端无额外通知。
4. **前端未集成**：前端仍使用传统 multipart 上传 + JWT 认证下载流程。

---

## 测试

相关测试文件：

- `backend/internal/infra/filestore/presign_test.go` — 底层函数单元测试
- `backend/internal/infra/filestore/presign_verify.go` — token 验证与消费处理
- `backend/internal/api/handler/static_handler_test.go` — presign 生成 handler 单元测试
- `backend/internal/api/handler/presigned_handler_test.go` — presign 消费 handler 单元测试（round-trip）
- `backend/internal/api/handler/static_integration_test.go` — 集成测试

运行测试：

```bash
go test -v ./backend/internal/api/handler/ -run "Presign"
go test -v ./backend/internal/infra/filestore/ -run "Presign"
```

---

## 相关源码索引

| 文件                                                      | 说明                              |
| --------------------------------------------------------- | --------------------------------- |
| `backend/internal/infra/filestore/presign.go`             | 预签名 URL 生成核心函数           |
| `backend/internal/infra/filestore/presign_verify.go`      | token 验证与 Put/Get 消费处理     |
| `backend/internal/infra/filestore/presign_test.go`        | 预签名单元测试                    |
| `backend/internal/infra/filestore/upload.go`              | 上传下载 + PresignDownloadByPublicID |
| `backend/internal/infra/filestore/init.go`                | Storage 初始化 + SignSecret() + StaticAPIKey() |
| `backend/internal/api/handler/static_handler.go`          | Presign URL 生成 HTTP handler     |
| `backend/internal/api/handler/presigned_handler.go`       | Presign URL 消费 HTTP handler     |
| `backend/internal/api/handler/presigned_handler_test.go`  | 消费端 handler 测试（round-trip） |
| `backend/internal/api/handler/static_handler_test.go`     | 生成端 handler 单元测试           |
| `backend/internal/api/handler/static_integration_test.go` | 集成测试                          |
| `backend/internal/api/middleware/static_auth.go`          | 生成端鉴权中间件                  |
| `backend/internal/api/router.go`                          | 路由注册（仅 local 驱动）         |
| `backend/config/config.go`                                | StorageConfig 配置定义            |