# Token 消耗统计设计方案

## 背景

需要按 message / session / project 三个维度统计 token 消耗。现有数据已能支撑单 message 维度，但 session 和 project 维度的聚合查询尚未实现。

## 现状分析

### 已具备的基础

- `types.SessionMessage.Usage`（JSONB 列，gorm tag `column:usage;type:jsonb`）已记录每条消息的 token 消耗明细
- `types.MessageUsage` 结构体覆盖全部所需字段：`input_tokens`、`output_tokens`、`total_tokens`、`cache_input_tokens`、`cache_output_tokens`
- LLM 调用链（Native Eino / OpenCode / Claude / Codex）均已正确解析并持久化 Usage 到 message
- `normalizeMessageUsage`（`session_service.go:1035`）已有 `TotalTokens = InputTokens + OutputTokens` 的归一化逻辑

### 缺失的能力

- 无 session 维度的聚合查询（需实时 SUM 所有 message.usage）
- 无 project 维度的聚合查询（需 JOIN session 表后 SUM message.usage）
- 无对应的 Service 层和 API 层暴露

## Schema 优化：usage 字段提取为标量列

### 问题

当前 `usage` 是单个 JSONB 列，聚合查询需对每行做 `CAST(usage->>'input_tokens' AS INTEGER)` 5 次。JSONB 字段提取无法走索引，大表上逐行解析 + 类型转换 CPU 开销高，project 维度 JOIN 后全表扫描更严重。

### 方案

把 `MessageUsage` 的 5 个字段从 JSONB 拆为独立 INTEGER 标量列，删除原 `usage` JSONB 列。聚合查询退化为普通 `SUM(integer)`，可走索引。

### Schema 改造（`backend/types/session.go`）

替换原 `Usage MessageUsage` JSONB 列为 5 个标量列：

```go
// session_message - token 输入数（用于聚合统计）
InputTokens int `gorm:"column:input_tokens;type:integer;default:0;index:idx_session_msg_usage,priority:2"`
// session_message - token 输出数
OutputTokens int `gorm:"column:output_tokens;type:integer;default:0"`
// session_message - token 总数
TotalTokens int `gorm:"column:total_tokens;type:integer;default:0"`
// session_message - 缓存输入 token
CacheInputTokens int `gorm:"column:cache_input_tokens;type:integer;default:0"`
// session_message - 缓存输出 token
CacheOutputTokens int `gorm:"column:cache_output_tokens;type:integer;default:0"`
```

复合索引 `idx_session_msg_usage` 覆盖 `(session_id, deleted_at)` 维度，按 session 聚合走索引扫描。project 维度靠 `lework_session.project_id` 已有索引覆盖 JOIN 条件。

`MessageUsage` 结构体保留，用于 Service/Contract 层传递聚合结果，不再作为列存储。

### 存量数据迁移（`backend/internal/infra/db/database.go`）

在 `runMigrations` 的 `dbtools.InitModel` 之后新增幂等 backfill 函数 `backfillSessionMessageUsageColumns`，仅当旧 `usage` 列存在时执行：

```sql
UPDATE lework_session_message
SET
  input_tokens        = COALESCE(CAST(usage->>'input_tokens' AS INTEGER), 0),
  output_tokens       = COALESCE(CAST(usage->>'output_tokens' AS INTEGER), 0),
  total_tokens        = COALESCE(CAST(usage->>'total_tokens' AS INTEGER), 0),
  cache_input_tokens  = COALESCE(CAST(usage->>'cache_input_tokens' AS INTEGER), 0),
  cache_output_tokens = COALESCE(CAST(usage->>'cache_output_tokens' AS INTEGER), 0)
WHERE usage IS NOT NULL AND usage != '{}'::jsonb;
```

幂等性靠 `HasColumn("usage")` 判断：旧列存在才执行。执行后将 `usage` 加入 `legacyColumns` 列表，由 `dropLegacyColumns` 删除。与现有 `backfillWorkerDeploymentPublicIDs`（`database.go:199`）范式一致。

### 写路径适配

写入 `Usage` 字段的调用点改为赋值 5 个标量列：

- `session_service.go:380` `message.Usage = normalizeMessageUsage(req.Usage)` — 拆成 5 个赋值
- `session_service.go:1311` `msgEntity.Usage = normalizeMessageUsage(req.Usage)` — 同上
- `session_service.go:1400` — 同上
- `session_run_state_projector.go:160/197/226` — `messagingUsageToMessageUsage` 返回的 `*MessageUsage` 字段拆到标量列

`normalizeMessageUsage`（`session_service.go:1035`）的 `TotalTokens = Input + Output` 归一化逻辑保留，结果写到标量列。

### 索引策略

- `(session_id, deleted_at)` 复合索引 `idx_session_msg_usage` — session 维度聚合走索引扫描
- project 维度靠 `lework_session.project_id` 已有 `uniqueIndex:uni_session_project_task` 覆盖 JOIN
- 不额外加 `assistant_id` 维度索引（YAGNI，当前统计不涉及）

### 回滚与风险

- AutoMigrate 自动加新列（`default 0`，对旧行不影响）
- backfill 仅在旧列存在时跑，幂等
- 旧 `usage` 列通过 `legacyColumns` 兜底删除，与现有 `config` 列清理模式一致
- 启动时一次性迁移，期间服务不可用，与现有迁移模式一致

## 方案：实时聚合查询

使用场景仅为前端展示和实时查询，不需要预聚合或缓存。

### DAO 层（`backend/internal/infra/db/session_message_dao.go`）

新增两个方法，对标量列求和实现：

```go
// 按 session 聚合所有 message 的 token 消耗
func SumSessionUsage(ctx context.Context, db *gorm.DB, sessionID uint) (*types.MessageUsage, error)

// 按 project 聚合（JOIN lework_session 获取 project 下所有 session）
func SumProjectUsage(ctx context.Context, db *gorm.DB, projectID uint) (*types.MessageUsage, error)
```

**session 聚合 SQL**：

```sql
SELECT
  COALESCE(SUM(input_tokens), 0)        AS input_tokens,
  COALESCE(SUM(output_tokens), 0)       AS output_tokens,
  COALESCE(SUM(total_tokens), 0)        AS total_tokens,
  COALESCE(SUM(cache_input_tokens), 0)  AS cache_input_tokens,
  COALESCE(SUM(cache_output_tokens), 0) AS cache_output_tokens
FROM lework_session_message
WHERE session_id = ? AND deleted_at IS NULL;
```

**project 聚合 SQL**（JOIN session 表，过滤 `project_id` 为 NULL 的记录）：

```sql
SELECT
  COALESCE(SUM(m.input_tokens), 0)        AS input_tokens,
  COALESCE(SUM(m.output_tokens), 0)       AS output_tokens,
  COALESCE(SUM(m.total_tokens), 0)        AS total_tokens,
  COALESCE(SUM(m.cache_input_tokens), 0)  AS cache_input_tokens,
  COALESCE(SUM(m.cache_output_tokens), 0) AS cache_output_tokens
FROM lework_session_message m
JOIN lework_session s ON s.id = m.session_id
WHERE s.project_id = ? AND s.deleted_at IS NULL AND m.deleted_at IS NULL;
```

**聚合范围说明**：聚合所有未删除的 message，包括 `pending` / `processing` 状态的消息。这些消息的标量列默认值为 0，`COALESCE` 会将其贡献为 0，无需额外过滤 status。

### Service 层（`backend/internal/service/session_service.go`）

新增两个方法：

```go
func (s *SessionService) GetSessionTokenUsage(ctx context.Context, sessionID uint) (*types.MessageUsage, error)
func (s *SessionService) GetProjectTokenUsage(ctx context.Context, projectID uint) (*types.MessageUsage, error)
```

返回值无需再调 `normalizeMessageUsage`，因为 `total_tokens` 已在 SQL 层算好。

### Contract 层

在 `contract.SessionService` 和 `contract.ProjectService` 接口中新增对应方法签名，保持与现有 handler 调用方式一致。

### API 层

遵循现有路由风格（`POST /ActionName`，非 RESTful）：

- `session_handler.go` 的 `RegisterRoutes` 中新增：`r.POST("/GetSessionTokenUsage", h.GetSessionTokenUsage)`
- `project_handler.go` 的 `RegisterRoutes` 中新增：`r.POST("/GetProjectTokenUsage", h.GetProjectTokenUsage)`

请求体：

```go
type GetSessionTokenUsageRequest struct {
    SessionID string `json:"session_id"` // PublicID
}

type GetProjectTokenUsageRequest struct {
    ProjectID string `json:"project_id"` // PublicID
}
```

注意：handler 收到的是 PublicID（`sess_xxx` / `proj_xxx`），需在 Service 层先转成内部 uint ID 再调 DAO。参考现有 `GetSession` / `GetProject` 的 PublicID 解析方式。

### 鉴权

复用现有 `session_handler` 和 `project_handler` 的鉴权中间件与 org 维度校验逻辑，确保用户只能查到自己组织内的 token 统计。

### message 维度

无需新增任何代码。message 维度的 token 消耗已存储在 `SessionMessage` 的 5 个标量列中，通过现有的 `GetSessionMessages` API 即可返回。

### 后续扩展

当前 `MessageUsage` 结构不含费用金额。如需后续支持成本统计，可在 `MessageUsage` 中增加 `Cost` 字段，通过 `lework_llm_model` 表的单价换算。

## 变更清单

| 层级 | 文件 | 变更 |
|------|------|------|
| Types | `backend/types/session.go` | `Usage` JSONB 列拆为 5 个 INTEGER 标量列 |
| Migration | `backend/internal/infra/db/database.go` | 新增 `backfillSessionMessageUsageColumns`；`legacyColumns` 加入 `usage` |
| DAO | `backend/internal/infra/db/session_message_dao.go` | 新增 `SumSessionUsage`、`SumProjectUsage` |
| Service | `backend/internal/service/session_service.go` | 写路径适配标量列；新增 `GetSessionTokenUsage` |
| Runnable | `backend/internal/runnable/session_run_state_projector.go` | `messagingUsageToMessageUsage` 结果适配标量列 |
| Contract | `contract.SessionService` / `contract.ProjectService` | 新增方法签名 |
| Service | `backend/internal/service/project_service.go` | 新增 `GetProjectTokenUsage` |
| Handler | `backend/internal/api/handler/session_handler.go` | 新增 `GetSessionTokenUsage` 端点 |
| Handler | `backend/internal/api/handler/project_handler.go` | 新增 `GetProjectTokenUsage` 端点 |

路由注册在各自的 `RegisterRoutes` 方法中完成，无需修改 `router.go`。
