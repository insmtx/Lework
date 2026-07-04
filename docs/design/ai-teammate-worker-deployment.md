# AI 队友维护与 Worker 动态部署技术设计

> 状态：草案待评审
> 日期：2026-07-02
> 范围：后端（`backend/`）+ 前端（`frontend/`）

本文档描述 AI 队友维护与 Worker 动态部署的设计方案。该方案独立于群聊模式，但为群聊中的“选择队友、显示队友状态、由指定队友执行任务”提供运行时基础。

## 1. 背景

当前代码中，AI 队友已经基本等价于一个可运行 worker：

- `DigitalAssistant` 是产品侧的 AI 队友实体。
- `WorkerDeployment` 绑定一个 AI 队友与一个组织内唯一的 `worker_id`。
- 服务端通过 `org.{org_id}.worker.{worker_id}.cmd.*` 将任务投递给指定 worker。
- `WorkerDeploymentReconciler` 根据数据库中的部署状态动态拉起、停止、健康检查 worker。

现有群聊方案已经覆盖多人发言、GlobalEvents、会话状态感知与默认队友选择，但还需要单独补齐 AI 队友生命周期与 Worker 动态部署的产品和技术边界，避免把“队友身份”和“worker 路由身份”混用。

## 2. 核心结论

AI 队友不是 worker 本身，而是 `DigitalAssistant`；worker 是 AI 队友的运行时实例，由 `WorkerDeployment` 管理。

```text
DigitalAssistant
  -> WorkerDeployment
      -> runtime worker process / container / Kubernetes Deployment
```

分工如下：

| 概念 | 作用 | 使用场景 |
|------|------|----------|
| `DigitalAssistant.ID` | 产品身份，表示一个 AI 队友 | 群聊展示、项目绑定、默认队友、消息 sender、队友配置 |
| `WorkerDeployment.WorkerID` | 运行时路由身份 | NATS subject、worker 鉴权、worker 进程启动参数 |
| `WorkerDeployment.Status` | 运行状态事实来源 | 前端状态展示、是否可执行、失败重试、健康检查 |

因此群聊中“选择的是 AI 队友”，运行时“路由到它绑定的 worker”。

## 3. 现状分析

### 3.1 已有能力

`WorkerProvisioningService` 已经提供部署记录创建能力：

- 创建组织默认 AI 队友和默认 worker deployment。
- 为每个 AI 队友创建一条 `WorkerDeployment`。
- AI 队友激活时，将 deployment 置为 `pending`。
- AI 队友停用、归档或草稿时，将 deployment 置为 `stopped`。

`WorkerDeploymentReconciler` 已经提供动态部署能力：

- 周期性扫描 `pending`、`provisioning`、`failed`、`ready` 状态。
- active 队友对应的 deployment 会被启动。
- inactive/draft/archived 队友对应的 worker 会被停止。
- ready 状态会执行健康检查。
- Kubernetes scheduler 支持按 `org_id + worker_id` 创建或更新 Deployment。

### 3.2 主要缺口

1. AI 队友列表和详情没有暴露 worker deployment 状态，前端无法知道队友是“部署中、可用、失败、已停止”。
2. 项目绑定 AI 队友时没有把 worker deployment 可用性作为一等条件。
3. task/session 创建时还需要明确区分 `assistant_id` 和 `worker_id`。
4. `agent.run` payload 当前主要依赖 `AllocatedAssistantID` 路由，缺少 AI 队友身份快照。
5. 删除或归档 AI 队友时需要明确 worker 停止、deployment 保留或清理策略。
6. deployment 失败后缺少面向用户的“重新部署”操作。

## 4. 目标状态

### 4.1 产品目标

- 用户可以创建、编辑、启用、停用、归档 AI 队友。
- 每个 AI 队友都有独立运行时 worker。
- 前端可以展示队友部署状态、错误原因和最近启动时间。
- 项目可以绑定多个 AI 队友，并设置一个默认队友。
- 创建任务时可以指定 AI 队友；未指定时使用项目默认队友。
- 队友 worker 部署失败时，用户可以触发重新部署。

### 4.2 技术目标

- `DigitalAssistant` 只表达产品配置和身份。
- `WorkerDeployment` 只表达运行时部署、路由和状态。
- 所有 server -> worker 命令都用 `WorkerDeployment.WorkerID` 路由。
- 所有 UI 展示、会话记录和 GlobalEvents 都使用 `DigitalAssistant` 身份。
- 调度器通过统一 `WorkerScheduler` 接口支持 process、docker-cli、k8s。
- Reconciler 是唯一负责实际启动、停止、重建 worker 的后台组件。

## 5. 数据模型

### 5.1 DigitalAssistant

现有字段继续保留：

```go
type DigitalAssistant struct {
    ID           uint
    Code         string
    OrgID        uint
    OwnerID      uint
    Name         string
    Description  string
    Avatar       string
    Status       string
    Version      int
    SystemPrompt string
}
```

为支持“创建 AI 队友”弹窗和模板创建，建议补充以下字段：

```go
type DigitalAssistant struct {
    gorm.Model

    Code         string
    OrgID        uint
    OwnerID      uint
    Name         string
    Description  string
    Avatar       string
    Status       string
    Version      int
    SystemPrompt string

    Expertise  SkillStringList
    TemplateID *uint
    Source     string
}
```

推荐数据库字段：

| 字段 | 类型 | 必填 | 说明 |
|------|------|------|------|
| `id` | `bigserial` | 是 | 主键，GORM `Model` 提供 |
| `created_at` | `timestamp` | 是 | 创建时间，GORM `Model` 提供 |
| `updated_at` | `timestamp` | 是 | 更新时间，GORM `Model` 提供 |
| `deleted_at` | `timestamp` | 否 | 软删除时间，GORM `Model` 提供 |
| `code` | `varchar(255)` | 是 | 队友业务编码，组织内唯一；前端可按名称自动生成 |
| `org_id` | `integer` | 是 | 所属组织 ID |
| `owner_id` | `integer` | 是 | 创建人用户 ID |
| `name` | `varchar(255)` | 是 | 队友名称 |
| `description` | `text` | 否 | 短描述，用于卡片和列表展示 |
| `avatar` | `varchar(500)` | 否 | 头像 URL 或内置头像资源标识 |
| `status` | `varchar(50)` | 是 | 产品态：`draft` / `active` / `inactive` / `archived` |
| `version` | `integer` | 是 | 配置版本，后续用于触发 worker reconcile |
| `system_prompt` | `text` | 否 | 队友简介和能力边界；前端展示为“简介”，运行时作为提示词注入 |
| `expertise` | `jsonb` | 否 | 擅长领域，如 `["新媒体运营","热点追踪","内容策划"]` |
| `template_id` | `integer` | 否 | 来源模板 ID，用户自定义创建时为空 |
| `source` | `varchar(32)` | 是 | 创建来源：`custom` / `template` / `system` |

字段语义约定：

- `description` 是面向用户的短描述，不承担运行时约束。
- `system_prompt` 是队友每次执行任务时的能力边界和行为约束，前端可命名为“简介”。
- `expertise` 可由用户显式选择，也可在创建/更新时根据 `name`、`description`、`system_prompt` 自动提取。
- `template_id` 只记录创建来源，不与模板强绑定；模板后续修改不自动覆盖已创建队友。
- `source=system` 仅用于系统内置默认队友，普通用户创建使用 `custom` 或 `template`。

建议保持 `Status` 作为产品态：

| 状态 | 含义 | Worker 期望 |
|------|------|-------------|
| `draft` | 草稿，尚不可执行 | `stopped` |
| `active` | 可使用 | `pending/provisioning/ready/failed` |
| `inactive` | 临时停用 | `stopped` |
| `archived` | 归档 | `stopped` |

### 5.2 AI 队友进化预留表

AI 队友支持“提示词分层 + 动态检索”，用于让队友在长期使用中积累经验、偏好、模板和边界规则。当前阶段已启用基础检索注入：每次发布 worker task 前，服务端会根据本轮用户消息，从队友的 prompt block 和 memory 中检索相关内容并追加到队友 persona。

第一版动态检索采用数据库关键词匹配和优先级回退，不依赖向量库；`embedding_id` 先作为后续向量检索预留字段。自进化写入仍保持受控，只有用户确认、系统总结或管理员写入的内容进入长期记忆，避免把单次错误回答自动沉淀为事实。

运行时仍应保留一段每轮必带的短核心身份提示词，动态检索只作为增强层，避免召回失败时队友退回默认身份。

#### 5.2.1 DigitalAssistantPromptBlock

表名：`leros_digital_assistant_prompt_block`

用途：存放 AI 队友的分层提示词片段，例如身份、能力、边界、风格、示例。构造 LLM system prompt 时可按任务选择性注入。

```go
type DigitalAssistantPromptBlock struct {
    gorm.Model

    AssistantID uint
    BlockType   string
    Title       string
    Content     string
    Priority    int
    Enabled     bool
    Version     int
}
```

推荐字段：

| 字段 | 类型 | 必填 | 说明 |
|------|------|------|------|
| `id` | `bigserial` | 是 | 主键 |
| `assistant_id` | `bigint` | 是 | 所属 AI 队友 ID |
| `block_type` | `varchar(32)` | 是 | 分层类型：`identity` / `capability` / `boundary` / `style` / `example` |
| `title` | `varchar(255)` | 是 | 管理端展示标题 |
| `content` | `text` | 是 | 提示词片段内容 |
| `priority` | `integer` | 是 | 注入排序优先级，默认 0 |
| `enabled` | `boolean` | 是 | 是否启用，默认 true |
| `version` | `integer` | 是 | 片段版本，默认 1 |

#### 5.2.2 DigitalAssistantMemory

表名：`leros_digital_assistant_memory`

用途：存放 AI 队友可进化的长期记忆。记忆不等同于聊天历史，只有被用户确认、系统总结或管理员写入的内容才进入该表。

```go
type DigitalAssistantMemory struct {
    gorm.Model

    AssistantID uint
    MemoryType  string
    Content     string
    SourceType  string
    SourceID    string
    Confidence  float64
    EmbeddingID string
    Enabled     bool
}
```

推荐字段：

| 字段 | 类型 | 必填 | 说明 |
|------|------|------|------|
| `id` | `bigserial` | 是 | 主键 |
| `assistant_id` | `bigint` | 是 | 所属 AI 队友 ID |
| `memory_type` | `varchar(32)` | 是 | 记忆类型：`preference` / `experience` / `template` / `fact` / `rule` |
| `content` | `text` | 是 | 记忆内容 |
| `source_type` | `varchar(64)` | 是 | 来源：`user_confirmed` / `task_summary` / `manual` |
| `source_id` | `varchar(255)` | 否 | 来源任务、会话、消息或外部产物 ID |
| `confidence` | `double precision` | 是 | 可信度，默认 0.8 |
| `embedding_id` | `varchar(255)` | 否 | 向量库中的 embedding 标识，检索启用后写入 |
| `enabled` | `boolean` | 是 | 是否可被检索，默认 true |

#### 5.2.3 AssistantPromptTrace

表名：`leros_assistant_prompt_trace`

用途：记录一次 LLM 请求中实际注入了哪些核心提示词版本、分层提示词和长期记忆，用于调试“为什么这个队友这样回答”。

```go
type AssistantPromptTrace struct {
    gorm.Model

    SessionID         uint
    MessageID         uint
    AssistantID       uint
    CorePromptVersion int
    InjectedBlockIDs  SkillStringList
    InjectedMemoryIDs SkillStringList
    PromptHash        string
}
```

推荐字段：

| 字段 | 类型 | 必填 | 说明 |
|------|------|------|------|
| `id` | `bigserial` | 是 | 主键 |
| `session_id` | `bigint` | 是 | 会话内部 ID |
| `message_id` | `bigint` | 是 | 触发本次 LLM 请求的用户消息 ID |
| `assistant_id` | `bigint` | 是 | 本次请求使用的 AI 队友 ID |
| `core_prompt_version` | `integer` | 是 | 每轮必带的核心身份提示词版本 |
| `injected_block_ids` | `jsonb` | 否 | 本次注入的 prompt block ID 列表 |
| `injected_memory_ids` | `jsonb` | 否 | 本次注入的 memory ID 列表 |
| `prompt_hash` | `varchar(128)` | 否 | 组装后 prompt 的稳定哈希，便于排查且避免重复存长文本 |

后续动态注入建议顺序：

```text
默认 lework 底座
+ 队友核心身份短提示词
+ 当前任务相关的 prompt block
+ 检索到的 teammate memory
+ 项目/文件/会话上下文
+ 用户本轮输入
```

### 5.3 AITeammateTemplate

系统会预设一批 AI 队友模板，用于 AI 队友市场、推荐创建和一键添加。模板不是可执行队友，只有用户基于模板创建后才生成 `DigitalAssistant` 和 `WorkerDeployment`。

建议新增表 `leros_ai_teammate_template`：

```go
type AITeammateTemplate struct {
    gorm.Model

    Code         string
    Name         string
    Description  string
    Avatar       string
    SystemPrompt string
    Expertise    SkillStringList
    Category     string
    Tags         SkillStringList
    SortOrder      int
    UseCount       int64
    RecommendCount int64
    Status         string
    IsSystem       bool
}
```

推荐数据库字段：

| 字段 | 类型 | 必填 | 说明 |
|------|------|------|------|
| `id` | `bigserial` | 是 | 主键，GORM `Model` 提供 |
| `created_at` | `timestamp` | 是 | 创建时间，GORM `Model` 提供 |
| `updated_at` | `timestamp` | 是 | 更新时间，GORM `Model` 提供 |
| `deleted_at` | `timestamp` | 否 | 软删除时间，GORM `Model` 提供 |
| `code` | `varchar(128)` | 是 | 模板编码，全局唯一，如 `media-hotspot-hunter` |
| `name` | `varchar(255)` | 是 | 模板名称 |
| `description` | `text` | 否 | 模板短描述 |
| `avatar` | `varchar(500)` | 否 | 模板头像 URL 或内置头像资源标识 |
| `system_prompt` | `text` | 是 | 模板简介和能力边界，复制到队友 `system_prompt` |
| `expertise` | `jsonb` | 否 | 模板擅长领域，复制到队友 `expertise` |
| `category` | `varchar(100)` | 是 | 模板分类，如 `tech`、`content`、`office` |
| `tags` | `jsonb` | 否 | 模板标签，用于搜索和筛选 |
| `sort_order` | `integer` | 是 | 市场展示排序，默认 0 |
| `use_count` | `bigint` | 是 | 使用次数，用户基于该模板成功创建 AI 队友后递增 |
| `recommend_count` | `bigint` | 是 | 推荐次数，模板被系统推荐或进入推荐位时递增 |
| `status` | `varchar(32)` | 是 | `active` / `inactive` |
| `is_system` | `boolean` | 是 | 是否系统预设模板 |

模板创建队友时的字段复制规则：

| DigitalAssistant 字段 | 来源 |
|------------------------|------|
| `name` | 默认使用模板 `name`，用户可在弹窗中修改 |
| `description` | 默认使用模板 `description`，用户可在弹窗中修改 |
| `avatar` | 默认使用模板 `avatar`，用户可在弹窗中修改 |
| `system_prompt` | 默认使用模板 `system_prompt`，用户可在弹窗中修改 |
| `expertise` | 默认使用模板 `expertise`；若用户修改简介，可重新提取 |
| `template_id` | 模板 `id` |
| `source` | `template` |

### 5.4 WorkerDeployment

现有字段继续作为运行态事实来源：

```go
type WorkerDeployment struct {
    OrgID              uint
    DigitalAssistantID uint
    WorkerID           uint
    DeploymentName     string
    Namespace          string
    Status             string
    BootstrapTokenHash string
    WorkspacePath      string
    LastError          string
    LastStartedAt      *time.Time
    LastReconciledAt   *time.Time
}
```

建议后续补充字段：

| 字段 | 说明 |
|------|------|
| `RuntimeMode` | `process` / `docker-cli` / `k8s`，记录创建时使用的运行模式 |
| `Image` | worker 镜像快照，便于判断是否需要重建 |
| `DesiredRevision` | 期望配置版本，用于 assistant 配置变更触发 reconcile |
| `ObservedRevision` | worker 已应用的配置版本 |

## 6. 状态机

### 6.1 WorkerDeployment 状态

| 状态 | 说明 | 进入条件 | 下一状态 |
|------|------|----------|----------|
| `pending` | 等待部署 | AI 队友激活、重新部署、首次创建 active 队友 | `provisioning` / `failed` |
| `provisioning` | 已发起启动，等待健康检查 | Reconciler 调用 `scheduler.Start` 成功 | `ready` / `failed` |
| `ready` | worker 可用 | 健康检查通过 | `failed` / `stopped` / `provisioning` |
| `failed` | 部署或健康检查失败 | 启动失败、超时、健康检查失败 | `pending` / `stopped` |
| `stopped` | 已停止 | 队友停用、草稿、归档、删除 | `pending` |

### 6.2 状态流转

```text
Create assistant(draft)
  -> WorkerDeployment(stopped)

Activate assistant
  -> WorkerDeployment(pending)
  -> Reconciler Start
  -> provisioning
  -> ready

Health failed / Start failed
  -> failed

Retry deployment
  -> pending

Deactivate / Archive assistant
  -> scheduler.Stop
  -> stopped
```

## 7. 后端设计

### 7.1 AI 队友创建

`CreateDigitalAssistant` 流程：

1. 接收头像、名称、描述、简介、模板 ID 和擅长领域。
2. 若 `expertise` 为空，根据 `name`、`description`、`system_prompt` 自动提取擅长领域。
3. 创建 `DigitalAssistant`，默认 `status=draft`，默认 `source=custom`。
4. 调用 `WorkerProvisioningService.EnsureForAssistant`。
5. 若队友为 `draft`，创建 deployment 但状态为 `stopped`。
6. 返回队友信息和 deployment 摘要。

创建 deployment 但不立刻启动，可以保证后续项目绑定、默认队友选择、worker_id 分配都有稳定结果。

基于模板创建时：

1. 查询 `AITeammateTemplate`，要求模板 `status=active`。
2. 将模板的 `avatar`、`name`、`description`、`system_prompt`、`expertise` 作为弹窗默认值。
3. 用户确认后创建 `DigitalAssistant`，写入 `template_id` 和 `source=template`。
4. 后续 deployment 创建流程与普通创建一致。

### 7.2 AI 队友激活/停用

`UpdateDigitalAssistantStatus` 流程：

```text
status=active
  -> EnsureForAssistant
  -> MarkAssistantActive
  -> deployment.status=pending
  -> reconciler 动态拉起 worker

status=draft/inactive/archived
  -> MarkAssistantStopped
  -> scheduler.Stop(deployment_name)
  -> deployment.status=stopped
```

实际启动/停止仍由 Reconciler 或 Scheduler 统一处理，service 层只更新期望状态。

### 7.3 重新部署

新增接口：

```text
POST /v1/RedeployDigitalAssistantWorker
```

请求：

```json
{
  "id": 123
}
```

处理逻辑：

1. 校验 caller 属于同一 org，且有队友管理权限。
2. 查询 `DigitalAssistant` 和 `WorkerDeployment`。
3. 要求 assistant 为 `active`。
4. 清空 `last_error`。
5. 将 deployment 状态置为 `pending`。
6. Reconciler 下一轮执行实际重建。

### 7.4 队友详情返回部署摘要

`DigitalAssistant` contract 建议新增：

```go
type WorkerDeploymentSummary struct {
    WorkerID         uint       `json:"worker_id"`
    DeploymentName   string     `json:"deployment_name"`
    Namespace        string     `json:"namespace,omitempty"`
    Status           string     `json:"status"`
    LastError        string     `json:"last_error,omitempty"`
    LastStartedAt    *time.Time `json:"last_started_at,omitempty"`
    LastReconciledAt *time.Time `json:"last_reconciled_at,omitempty"`
}
```

`DigitalAssistant` / `DigitalAssistantDetail` 返回：

```go
WorkerDeployment *WorkerDeploymentSummary `json:"worker_deployment,omitempty"`
```

前端据此展示：

- 草稿：未启用
- pending/provisioning：部署中
- ready：在线
- failed：部署失败，可重新部署
- stopped：已停止

### 7.5 项目绑定 AI 队友

项目添加 AI 队友时：

1. 校验 assistant 属于当前 org。
2. 校验 assistant 未归档。
3. 调用 `EnsureForAssistant` 保证 deployment 存在。
4. 创建 `ProjectMember(member_type=assistant)`。
5. 如果是项目第一个 AI 队友，可自动设为默认队友。

绑定不强制要求 worker `ready`，因为 active 队友可能正在部署中；但创建任务执行前需要判断是否可执行。

### 7.6 默认 AI 队友选择

项目默认队友仍由 `ProjectMember.is_default` 表达。

创建 task session 时：

1. 若请求指定 `assistant_id`，校验该 assistant 是项目 AI 队友。
2. 若未指定，查询项目默认 AI 队友。
3. 查询该 assistant 的 `WorkerDeployment`。
4. session 写入：
   - `AssistantID = DigitalAssistant.ID`
   - `AllocatedAssistantID = WorkerDeployment.WorkerID`

无默认队友时返回 `ErrNoDefaultAssistant`。

### 7.7 组织级 Skill 同步

Skill 的安装意图属于组织，不属于某一个 worker。后台需要以组织安装表作为 source of truth：

```text
leros_org_skill_installation
- org_id
- action           # install / import
- source           # Leros / ClawHub / github
- skill_id
- version
- name
- description
- category
- tags
- status
- last_error
- installed_by
```

安装流程：

1. 用户安装 marketplace skill 时，先让默认 worker 执行安装并等待结果。
2. 安装成功后写入 `leros_org_skill_installation`，记录 source、skill_id、version、action。
3. 后台查询当前组织下 `ready/provisioning` 的 `WorkerDeployment`，把同一个 skill command 同步到每个 worker。
4. GitHub 导入使用 `action=import` + `source=github` 记录，后续可按 GitHub 源重放。

运行时兜底：

1. 创建/续聊任务投递到 worker 前，按 `org_id` 查询 active 的组织技能安装记录。
2. 对本次 `session.AllocatedAssistantID` 对应 worker 逐个补发 skill command。
3. 同步失败只记录 warning，不阻断任务投递；worker 执行时仍按用户选择的技能集决定是否调用。

这样新启动的 worker 不需要依赖人工逐个安装。即使 worker 启动时没有完整技能，第一次承接任务前也会按组织安装表补齐。

### 7.8 投递 worker command

投递时必须保持身份分离：

```go
topic := WorkerCommandSubject(orgID, deployment.WorkerID, LaneRun)
```

`RunCommandPayload` 应同时携带队友身份快照：

```go
Execution: messaging.ExecutionTarget{
    AssistantID: fmt.Sprintf("%d", session.AssistantID),
}
```

建议扩展 `ExecutionTarget`：

```go
type ExecutionTarget struct {
    AssistantID     string   `json:"assistant_id,omitempty"`
    AssistantName   string   `json:"assistant_name,omitempty"`
    SystemPrompt    string   `json:"system_prompt,omitempty"`
    Skills          []string `json:"skills,omitempty"`
    Tools           []string `json:"tools,omitempty"`
}
```

worker mapper 再透传到 `assistantdomain.AssistantContext`：

```go
Assistant: assistantdomain.AssistantContext{
    ID:           task.Execution.AssistantID,
    Name:         task.Execution.AssistantName,
    SystemPrompt: task.Execution.SystemPrompt,
}
```

这样队友身份、队友提示词、GlobalEvents 中的队友名称都不依赖 worker_id 推断。

### 7.9 Reconciler 职责

Reconciler 是运行态控制器，职责包括：

- 将 `pending` 的 active 队友拉起。
- 将 `provisioning` 的 deployment 做健康检查，超时则失败。
- 将 `ready` 的 deployment 做健康检查，失败则置为 `failed`。
- 检查运行态漂移，如镜像变化、配置版本变化，必要时重建。
- 将 inactive/draft/archived 队友对应 worker 停止。

Reconciler 不负责项目绑定、权限校验、用户接口；这些仍属于 service 层。

## 8. 前端设计

### 8.1 AI 队友列表

列表项展示：

- 队友名称、描述、头像。
- 产品状态：草稿、启用、停用、归档。
- 部署状态：未启动、部署中、在线、失败、已停止。
- 失败原因摘要。

推荐映射：

| Product Status | Deployment Status | UI |
|----------------|-------------------|----|
| `draft` | `stopped` | 草稿 |
| `active` | `pending/provisioning` | 部署中 |
| `active` | `ready` | 在线 |
| `active` | `failed` | 部署失败 |
| `inactive` | `stopped` | 已停用 |
| `archived` | `stopped` | 已归档 |

### 8.2 队友详情

详情页增加“运行状态”区域：

- Worker ID
- Deployment Name
- Namespace
- Runtime Mode
- Last Started At
- Last Reconciled At
- Last Error

操作：

- 启用
- 停用
- 重新部署
- 归档

### 8.3 项目 AI 队友

项目页 AI 队友区域展示：

- 已绑定队友列表。
- 默认队友标记。
- 每个队友的部署状态。
- 添加队友入口。
- 设置默认队友入口。

创建任务时：

- 如果默认队友部署中，允许创建任务但提示“队友部署中，稍后开始处理”。
- 如果默认队友部署失败，阻止执行并提示重新部署。
- 如果没有默认队友，提示先添加或设置默认队友。

## 9. 与群聊方案的边界

群聊方案关注：

- 多用户准入。
- 用户消息广播。
- 发言者身份。
- GlobalEvents 状态通知。
- 历史上下文注入。

本文档关注：

- AI 队友如何创建、启用、停用、归档。
- AI 队友如何绑定 worker runtime。
- worker 如何动态部署、健康检查、失败重试。
- task/session 如何从 AI 队友解析到 worker 路由。

两个方案的交汇点：

| 场景 | 群聊方案 | 本文档 |
|------|----------|--------|
| 项目默认队友 | 决定 task 由谁处理 | 提供该队友的 worker_id 和状态 |
| GlobalEvents run 状态 | 展示哪个队友在回复 | 提供 assistant identity snapshot |
| 消息 sender | AI 回复显示队友名称 | 提供 `DigitalAssistant.Name` |
| AddMessage 投递 | 触发 worker command | 路由到 `WorkerDeployment.WorkerID` |

## 10. 改动清单

| 层 | 文件 | 改动 |
|----|------|------|
| 类型 | `types/worker_deployment.go` | 可选新增 `RuntimeMode`、`Image`、`DesiredRevision`、`ObservedRevision` |
| 类型 | `types/digital_assistant.go` | 新增 `Expertise`、`TemplateID`、`Source` |
| 类型 | `types/ai_teammate_template.go` | 新增系统预设 AI 队友模板表 |
| 类型 | `types/tables.go` | 新增 `TableNameAITeammateTemplate` |
| Contract | `contract/digital_assistant_type.go` | 新增 `WorkerDeploymentSummary` |
| Contract | `contract/digital_assistant_type.go` | 新增 `expertise`、`template_id`、`source` 字段 |
| Contract | `contract/digital_assistant.go` | 新增 `RedeployDigitalAssistantWorker` service 方法 |
| Contract | AI 队友模板 contract | 新增模板列表、模板详情、基于模板创建队友请求响应 |
| DAO | `infra/db/worker_deployment_dao.go` | 新增按 assistant IDs 批量查询 deployment；新增重新部署状态更新方法 |
| DAO | `infra/db/ai_teammate_template_dao.go` | 新增模板查询、筛选、seed/upsert、使用次数和推荐次数递增方法 |
| Service | `service/digital_assistant_service.go` | 列表/详情返回 deployment；新增重新部署；删除/归档停止 worker |
| Service | `service/digital_assistant_service.go` | 创建/更新时保存简介、擅长领域、模板来源；缺省时自动提取擅长领域 |
| Service | AI 队友模板 service | 返回系统预设模板，支持一键基于模板创建队友 |
| Service | `service/message_poster.go` | 投递时补齐 `Execution.AssistantID/Name/SystemPrompt` |
| Service | `service/worker_resolution.go` | 明确返回 assistant_id 与 worker_id；区分未部署、失败、未就绪错误 |
| Service | `service/project_service.go` | 绑定 AI 队友时确保 deployment 存在；默认队友校验 |
| Worker | `worker/command/run/mapper.go` | 透传 assistant name/system prompt |
| Handler | `handler/digital_assistant_handler.go` | 新增重新部署接口 |
| Handler | AI 队友模板 handler | 新增模板列表和基于模板创建接口 |
| Frontend | AI 队友页面 | 展示 deployment 状态，支持重新部署 |
| Frontend | AI 队友创建弹窗 | 支持头像、名称、描述、简介和擅长领域；模板创建时预填 |
| Frontend | 项目页面 | 展示项目 AI 队友部署状态与默认队友 |
| 测试 | service/DAO/handler | 覆盖创建、激活、停用、失败重试、项目绑定、worker 路由 |

## 11. 推荐实施顺序

1. 后端 contract 增加 `WorkerDeploymentSummary`，列表和详情返回部署状态。
2. 补齐 `DigitalAssistant` 的简介、擅长领域和模板来源字段。
3. 新增 AI 队友模板表、模板 seed 和模板查询接口。
4. 新增重新部署接口，把 failed deployment 置回 `pending`。
5. 修正 `message_poster`，投递 `agent.run` 时补齐 assistant identity snapshot。
6. 项目 AI 队友绑定时确保 `WorkerDeployment` 存在。
7. task session 创建时引入默认队友校验和更明确的错误。
8. 前端 AI 队友创建弹窗支持头像、名称、描述、简介和模板预填。
9. 前端 AI 队友页面展示部署状态和重新部署操作。
10. 前端项目页展示绑定队友、默认队友和部署状态。
11. 补齐 Reconciler 漂移判断中的配置版本和镜像版本。
