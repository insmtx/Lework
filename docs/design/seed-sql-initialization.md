# 基于 SQL 文件的种子初始化（Seed SQL Initialization）

## 背景与目标

`backend/internal/seed` 目前通过 Go 业务代码种入核心数据（账号、LLM）与内置内容（AI 队友模板、技能、连接器）。现在需要新增一种初始化方式：**从固定相对路径读取 `*.sql` 文件执行**，且 `*.sqltpl` 模板文件能用**环境变量**填充部分字段。参考 `roc/apps/keinit` 的实现。

> **已变更（2024-08）：改为固定路径、不依赖配置。** 本设计文档早期为"配置驱动"（`seed.sql_dir`），已将 `SeedConfig` 移除，目录由进程入口以固定相对路径常量提供（见下文"挂接入口"）。

## 需求要点（已确认）

1. SQL 脚本目录为**固定相对路径 `deployments/dev/seed`**（相对进程工作目录），不再配置。本地（cwd=仓库根）与容器（`WORKDIR /app` + Dockerfile COPY）自动对齐，无需外部配置。
2. 变量替换语法：**Go `text/template`，`{{.KEY}}`**，数据来自 `os.Environ()`。**必填变量缺失即报错**（不按空值容忍）。
3. **必填变量缺失即报错**（不按空值容忍）。
4. **逐文件持久化追踪**执行状态：成功的文件跳过，失败的文件从断点续跑。
5. 执行时机：**随 server 启动自动执行**（融入 `seed.Run`）。
6. 失败处理：**失败即阻断启动**。
7. `.sql` 与 `.sqltpl` **统一按文件名排序执行**（混合排序，`.sqltpl` 先渲染再执行）。
8. 执行记录表用**标准 `types` 模型** + AutoMigrate 注册（遵循 AGENTS.md DB 规范）。

## 架构设计

### 1. 路径提供（`backend/cmd/leros/server.go`）

SQL 脚本目录以固定相对路径常量提供，**不依赖任何配置**：

```go
// defaultSeedScriptDir 是 SQL 种子脚本目录，相对进程工作目录。
const defaultSeedScriptDir = "deployments/dev/seed"
```

- 本地开发：`cwd=仓库根` → `./deployments/dev/seed`。
- 容器：`WORKDIR /app` + Dockerfile `COPY ... /app/deployments/dev/seed` → 相对 `/app`，两端对齐。
- 目录不存在时 `RunSQLScripts` 记 Warn 并跳过，不阻断启动。

### 2. 执行记录模型（`backend/types/seed_record.go` + `tables.go`）

新建 GORM 模型 `SeedRecord`（对齐 keinit 的 `InitExecRecourd`），表名 `leros_seed_record`：

```go
// SeedExecStatus 表示一个 SQL seed 文件的执行结果。
type SeedExecStatus string

const (
	SeedExecStatusSuccess SeedExecStatus = "succ"
	SeedExecStatusFailed  SeedExecStatus = "fail"
)

// SeedRecord 记录某个 SQL seed 文件的执行情况，用于跳过已成功文件与断点续跑。
type SeedRecord struct {
	ID           uint           `gorm:"primarykey"`
	FileName     string         `gorm:"column:file_name;type:varchar(255);not null;index"`
	ExecStatus   SeedExecStatus `gorm:"column:exec_status;type:varchar(10);not null;index"`
	ErrorMessage string         `gorm:"column:error_message;type:text"`
	FailLineAt   int            `gorm:"column:fail_line_at;type:int;not null"`
	StartTime    time.Time      `gorm:"column:start_time"`
	EndTime      time.Time      `gorm:"column:end_time"`
	ExecTime     float64        `gorm:"column:exec_time"`
}

func (SeedRecord) TableName() string { return TableNameSeedRecord }
```

`tables.go` 新增：`TableNameSeedRecord = tablenamePrefix + "seed_record"`。

**DB Schema 变更合规**（AGENTS.md）：本模型为**新增表**，加入 `database.go runMigrations` 的 `models` 列表，由 GORM `AutoMigrate` 自动创建；无删除/重命名列，故无需 `legacyColumns`/`renamesToApply` 处理。

### 3. 环境变量读取（`backend/internal/seed/env.go`）

```go
// loadEnvVars 读取 os.Environ()，返回模板变量表。
func loadEnvVars(_ context.Context) (map[string]string, error)
```

### 4. SQL 模板渲染（`backend/internal/seed/sqlrun.go`）

对 `.sqltpl` 用 `text/template` 渲染。**"必填变量缺失即报错"天然由 Go template 语义满足**：Go `text/template` 从 `map[string]string` 直取 `{{.KEY}}` 时，key 不存在，`Execute` 返回 `map has no entry for key "KEY"` 错误。因此无需自行枚举缺失变量。渲染错误在返回时包裹更友好的文案。

### 5. SQL 解析器（`backend/internal/seed/sqlrun.go`）

迁移自 keinit `parseSQLReader`：

- 按行扫描，`TrimSpace`。
- 跳过空行、`--`/`#` 行注释、`/* */` 整行块注释。
- 以 `;` 结尾切分语句，记录语句起始行号。
- 末尾无分号的兜底语句。

```go
type sqlline struct {
	line   string
	number int
}

func parseSQLStatements(r io.Reader) ([]sqlline, error)
```

### 6. 文件执行器（`backend/internal/seed/sqlrun.go`）

入口：

```go
// RunSQLScripts 执行 seed 目录下的 SQL/模板文件。幂等：已成功文件跳过，失败文件断点续跑。
// sqlDir 为相对进程工作目录的脚本目录；db 为 nil 或 sqlDir 为空/目录不存在时直接返回 nil。
func RunSQLScripts(ctx context.Context, db *gorm.DB, sqlDir string) error
```

流程：
1. 若不满足启用条件（`db == nil` 或 `sqlDir` 为空）→ 返回 nil。若目录不存在 → `logs.Warn` 跳过（不阻断启动）。
2. `AutoMigrate(&types.SeedRecord{})`（防御性调用；亦已在 runMigrations 中注册）。
3. `loadEnvVars` 加载变量。
4. 列出目录下 `.sql` 与 `.sqltpl`，**统一按文件名排序**（含 `vX.Y_Z__` 版本语义）。
5. 对每个文件：
   - 查 `SeedRecord` 是否有该文件名 `succ` 记录 → 跳过。
   - `.sqltpl` 先渲染为 SQL 文本。
   - 解析 → 逐条 `db.Exec`。
   - 失败：从上次 `fail_line_at` 续跑，写 `fail` 记录（`error_message`、`fail_line_at`）→ 返回 error（上行阻断）。
   - 成功：写 `succ` 记录。

逐文件在**独立执行单元**内（非整体事务），保证断点续跑基于行号记录。

### 7. 挂接入口（`backend/cmd/leros/server.go` 与 `backend/internal/seed/seed.go`）

`Options` 增加 `SQLScriptDir string`，`Run` 在 `SeedCoreData` 之后调用：

```go
// 2.5 可选：基于固定目录的 SQL 文件初始化
if err := RunSQLScripts(ctx, db, opts.SQLScriptDir); err != nil {
	return err
}
```

`server.go:123` 调用处传入固定常量 `defaultSeedScriptDir`（不再读取配置）：

```go
seed.Options{LLMConfig: cfg.LLM, SQLScriptDir: defaultSeedScriptDir}
```

失败 → `seed.Run` 返回 error → `server.go` 现有 `log.Fatalf` 即阻断启动（无需改动该处逻辑）。

## 文件清单

| 文件 | 改动 |
|---|---|
| `backend/config/config.go` | **移除** `SeedConfig` 类型与 `Config.Seed` 字段 |
| `backend/types/seed_record.go` | 新增 `SeedRecord` 模型 |
| `backend/types/tables.go` | 新增 `TableNameSeedRecord` 常量 |
| `backend/internal/infra/db/database.go` | `runMigrations` models 列表加 `&types.SeedRecord{}` |
| `backend/internal/seed/env.go` | 新增环境变量读取（仅 `os.Environ()`） |
| `backend/internal/seed/sqlrun.go` | 新增解析/渲染/执行器 `RunSQLScripts(ctx, db, sqlDir)` |
| `backend/internal/seed/seed.go` | `Options` 加 `SQLScriptDir`，`Run` 中调用 `RunSQLScripts` |
| `backend/cmd/leros/server.go` | 定义 `defaultSeedScriptDir` 常量并传入 |

## 测试策略

- **解析器单测**（无 DB）：分号切分、注释跳过、行号、末尾无分号。
- **模板渲染单测**（无 DB）：`{{.KEY}}` 正常替换；缺失 key 报错。
- **执行器集成测试**（复用 `testutil/db.go`）：临时 SQL 目录 → 首次执行写 `succ` → 二次执行跳过 → 断言 `SeedRecord` 记录；构造失败文件断言 `fail` 记录与断点续跑。

## 风险与权衡

- **SQL 执行不在事务内**：与 keinit 一致，部分成功时靠记录表续跑恢复，而不是回滚。适用于一次性数据初始化。
- **变量依赖 Go template 语义报错**：满足"缺失即报错"，但错误信息是 template 原文，可在返回时包裹更友好文案。
- **固定路径权衡**：不依赖配置带来部署简单（本地/容器自动对齐），但目录无法外部覆盖；需修改路径必须改 `server.go` 常量或 Dockerfile COPY 目标。
- **不侵入现有账号/LLM 种子**：`RunSQLScripts` 是增量步骤，现有 `seed.Run` 行为不变。
