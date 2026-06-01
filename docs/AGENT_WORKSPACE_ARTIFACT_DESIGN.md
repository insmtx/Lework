# Agent Workspace 与最终产物设计

## 1. 背景与目标

本文档说明当前 Agent 运行时工作区、任务多轮对话产物归属、最终产物声明、Worker 与 Server 文件路径映射关系。

当前实现目标：

- 为每个 `org/project/task/request` 准备隔离的 Agent 工作区。
- 同一个 task 内按 `request_id` 拆分 turn 目录，保证每轮产物声明可单独收集。
- 最终产物通过 manifest / `artifact_declare` 显式声明，并辅以执行前后文件变更自动兜底扫描。
- Artifact 持久化后通过任务维度接口返回给前端，下载接口只暴露 `artifact_id`。
- 内部真实路径不通过对外 API 暴露。

当前实现不做：

- 不实现正文中的 inline 附件展示。
- 不实现本地 artifact 文件快照目录。
- 不通过 worker task 协议或 `RuntimeOptions` 传递 `ProjectDir`、`TmpDir`、`ArtifactManifestPath` 等内部派生路径。
- 不设计大量 workspace 环境变量。
- 不在 artifact 表中持久化 `request_id`。`request_id` 目前用于执行期 turn 路径和 manifest 查找，持久化归属依赖 `project_id`、`task_id`、`session_id`、`message_id`。

## 2. 当前代码入口

| 职责 | 当前实现 |
| --- | --- |
| Server 创建用户消息并投递 worker task | `backend/internal/service/message_poster.go` |
| Worker task 协议结构 | `backend/internal/worker/protocol/task.go` |
| Worker task 到 runtime request 的映射 | `backend/internal/worker/taskconsumer/mapper.go` |
| Worker 准备 workspace 并回写实际 `WorkDir` | `backend/internal/worker/taskconsumer/consumer.go` |
| Workspace 路径计算和校验 | `backend/internal/workspace/workspace.go` |
| Manifest 读取和 artifact 校验 | `backend/internal/workspace/artifacts.go` |
| 内置 runtime 注入工具上下文 | `backend/internal/runtime/drivers/native/runner.go` |
| `artifact_declare` 工具 | `backend/tools/artifact_declare/tool.go` |
| File 扫描与 ignore 规则 | `backend/internal/workspace/scanner.go` |
| Baseline 捕获 (执行前) | `backend/internal/runtime/lifecycle/steps/baseline.go` |
| Reconcile 兜底扫描 (执行后) | `backend/internal/runtime/lifecycle/steps/reconcile.go` |
| Artifact 持久化 | `backend/internal/runtime/lifecycle/steps/artifact.go` |
| Server 侧 artifact 下载路径解析 | `backend/internal/workspace/server_paths.go` |
| Artifact 查询和下载服务 | `backend/internal/service/artifact_service.go` |

## 3. Worker 侧路径设计

Worker 本地 workspace root 使用 `LEROS_WORKSPACE_ROOT`。如果未设置，Linux 默认 `/workspace`，Windows 默认 `%LOCALAPPDATA%/Leros/workspace`。

有 project 上下文时，Worker 侧目录结构如下：

```text
{WORKER_LEROS_WORKSPACE_ROOT}/
  projects/
    {org_id}/
      {project_id}/
        repo/
          .git/
          .leros/
            tasks/
              {task_id}/
                turns/
                  {request_id}/
                    tmp/
                    logs/
                    baseline.jsonl
                    artifacts.jsonl
```

路径职责：

| 路径 | 职责 |
| --- | --- |
| `projects/{org_id}/{project_id}/repo/` | 当前项目 Git 工作区，也是默认 Agent 执行目录 |
| `repo/.git/` | Worker 自动初始化的 Git 管理目录 |
| `repo/.leros/` | Leros 运行态目录，写入 `repo/.git/info/exclude`，不进入项目 Git |
| `repo/.leros/tasks/{task_id}/turns/{request_id}/tmp/` | 当前 turn 临时目录 |
| `repo/.leros/tasks/{task_id}/turns/{request_id}/logs/` | 当前 turn 日志目录 |
| `repo/.leros/tasks/{task_id}/turns/{request_id}/baseline.jsonl` | 执行前文件基线快照，用于 diff 检测新增/修改文件 |
| `repo/.leros/tasks/{task_id}/turns/{request_id}/artifacts.jsonl` | 当前 turn 最终产物 manifest |

没有 project 上下文时，Worker 不创建上述 project/task/turn 目录，而是使用：

```text
{WORKER_LEROS_WORKSPACE_ROOT}/temp
```

该 fallback 只解决无项目上下文的运行目录问题，不产生可持久化 artifact workspace。

## 4. Workspace Resolver

当前 resolver 位于 `backend/internal/workspace`。

核心输入：

```go
type TaskWorkspaceRequest struct {
    OrgID            uint
    ProjectID        string
    TaskID           string
    RequestID        string
    RequestedWorkDir string
}
```

核心输出：

```go
type TaskWorkspace struct {
    WorkspaceRoot        string
    ProjectRoot          string
    RepoDir              string
    TaskDir              string
    TurnDir              string
    TurnTmpDir           string
    TurnLogDir           string
    ArtifactManifestPath string
    EffectiveWorkDir     string
}
```

`PrepareTaskWorkspace` 的职责：

- 根据 `org_id/project_id/task_id/request_id` 计算路径。
- 创建 turn 的 `tmp`、`logs`、`baseline.jsonl` 和 `artifacts.jsonl`。
- 初始化 `repo/.git`。
- 将 `.leros/` 写入 `repo/.git/info/exclude`。
- 校验和解析请求传入的 `runtime.work_dir`。
- 创建并返回最终 `EffectiveWorkDir`。

`ResolveTaskWorkspace` 只计算路径，不创建目录。`FromAgentRequest` 从标准化后的 `agent.RequestContext` 反推出当前 run 的 workspace plan，主要供 artifact 收集和工具上下文注入使用。

## 5. `work_dir` 约束

`runtime.work_dir` 是上游指定的期望执行目录，但 Worker 不直接信任它。当前规则：

- 空值：使用当前 project 的 `repo/`。
- 相对路径：解析为 `repo/` 内子目录。
- 绝对路径：必须位于当前 project 的 `repo/` 内。
- 禁止 `..` 逃逸。
- 禁止软链逃逸。
- 禁止跨 project workspace。
- 禁止指向 `.git`、`.leros` 等运行态目录。

Worker 准备 workspace 后，会把 `req.Runtime.WorkDir` 覆盖为 resolver 返回的 `EffectiveWorkDir`。后续内置 runtime、外部 CLI、node 工具都应以该值作为实际工作目录。

## 6. Server 到 Worker 的协议映射

Server 不把内部路径传给 Worker。Server 发布的 `WorkerTaskMessage` 只携带业务标识和运行控制字段。

当前字段映射：

| 语义 | Worker task 字段 | 来源 |
| --- | --- | --- |
| org id | `route.org_id` | session/caller |
| worker id | `route.worker_id` | `session.allocated_assistant_id` |
| session id | `route.session_id` | `session.public_id` |
| project id | `body.workspace.project_id` | session 关联 project 的 public id |
| task id | `trace.task_id` | session 关联 task 的 public id；缺失时 fallback 为 `task_{message.ID}` |
| request id | `trace.request_id` | `req_{message.ID}` |
| run id | `trace.run_id` | 当前等于 `request_id` |
| worker task message id | `id` | `msg_{session.ID}_{message.Sequence}` |
| runtime kind/work_dir/max_step | `body.runtime` | 上游运行控制字段；当前消息投递路径通常为空 |

Worker 收到 task 后，`RequestFromWorkerTask` 映射为 `agent.RequestContext`：

```text
route.org_id                 -> req.Workspace.OrgID
body.workspace.project_id    -> req.Workspace.ProjectID
trace.task_id                -> req.Workspace.TaskID / req.TaskID
trace.request_id             -> req.Workspace.RequestID
route.session_id             -> req.Conversation.ID
body.execution.assistant_id  -> req.Assistant.ID
body.runtime                 -> req.Runtime
```

随后 `Consumer.prepareWorkspace` 做真正的路径准备：

1. 如果 `body.workspace.project_id` 为空，调用 `PrepareTempWorkspace()`，并把 `req.Runtime.WorkDir` 设置为 `{workspace_root}/temp`。
2. 如果 `project_id` 存在，使用 `route.org_id`、`body.workspace.project_id`、`trace.task_id`、`trace.request_id` 调用 `PrepareTaskWorkspace()`。
3. `PrepareTaskWorkspace()` 返回 `EffectiveWorkDir`。
4. Worker 把 `req.Runtime.WorkDir` 覆盖为 `EffectiveWorkDir`，再交给 runtime 执行。

因此，路径由 Worker 本地 resolver 决定；Server 只决定业务归属和目标 worker。

## 7. Worker 与 Server 的文件系统映射

Worker 侧 artifact 持久化时，`TaskWorkspace.StorageKey(relativePath)` 会把 artifact 绝对路径转换为相对 Worker workspace root 的 key：

```text
projects/{org_id}/{project_id}/repo/{relative_path}
```

该 `storage_key` 被写入 artifact 表。它不是容器绝对路径，也不是下载 URL。

Server 下载时需要从自己的视角找到对应 Worker workspace 的挂载目录。当前实现使用：

```text
{SERVER_LEROS_WORKSPACE_ROOT}/{org_id}/{worker_id}/workspace/{storage_key}
```

对应代码：

```text
WorkerMountedWorkspacePath(orgID, workerID)
  = {SERVER_LEROS_WORKSPACE_ROOT}/{orgID}/{workerID}/workspace

ArtifactStoragePath(orgID, workerID, storageKey)
  = WorkerMountedWorkspacePath(...) + storageKey
```

这意味着部署层需要保证：

```text
Worker 进程看到的 LEROS_WORKSPACE_ROOT
  == Server 进程看到的 {SERVER_LEROS_WORKSPACE_ROOT}/{org_id}/{worker_id}/workspace
```

举例：

```text
Worker:
  LEROS_WORKSPACE_ROOT=/workspace
  artifact file=/workspace/projects/1/prj_a/repo/report.md
  storage_key=projects/1/prj_a/repo/report.md

Server:
  LEROS_WORKSPACE_ROOT=/data/leros/workers
  worker mounted workspace=/data/leros/workers/1/1/workspace
  download path=/data/leros/workers/1/1/workspace/projects/1/prj_a/repo/report.md
```

当前限制：artifact 表还没有持久化生产该 artifact 的 `worker_id`，`artifact_service.go` 下载时暂时使用 `defaultArtifactWorkerID = 1`。多 worker 场景下需要补齐 `worker_id` 持久化，否则 Server 无法可靠定位产物所在 worker workspace。

## 8. 执行流程

1. Server 收到用户消息，创建或定位 project、task、session，并创建 user message。
2. Server 生成 `request_id = req_{message.ID}`，并发布 worker task。
3. Worker 校验 `route.org_id/route.worker_id` 是否匹配当前 consumer。
4. Worker 将 worker task 映射为 `agent.RequestContext`。
5. Worker 根据 project 上下文准备 project workspace 或 temp fallback。
6. Worker 把 resolver 返回的 `EffectiveWorkDir` 写入 `req.Runtime.WorkDir`。
7. Runtime 在 `req.Runtime.WorkDir` 下执行 Agent。
8. Agent 创建最终文件，文件必须位于当前 project `repo/` 内。
9. Agent 调用 `artifact_declare`，或写入本轮 `artifacts.jsonl`。
10. Runtime lifecycle 在完成事件前读取当前 turn manifest。
11. 系统校验产物路径、文件存在性、mime type、file size 和 sha256。
12. Artifact recorder 创建 artifact 表记录，并向运行事件流追加 `artifact.declared`。
13. Server 创建最终 assistant message 时，根据事件里的 artifact 引用把已有 artifact 绑定到 `message_id`，并把轻量 artifact references 写入 message。
14. 前端通过 task artifact 接口查询，通过 artifact download 接口下载。

## 9. Agent 产物声明

当前推荐使用 `artifact_declare` 工具声明最终产物：

```text
artifact_declare(path, title, description, mime_type, artifact_type, is_final)
```

工具边界要求：

- `path` 必须是完整绝对路径。
- 文件必须位于当前 project `repo/` 内。
- 文件必须真实存在，且不能是目录。
- 不能声明 `.git`、`.leros`、`tmp`、`logs`、`cache` 等运行态或临时路径。
- 工具内部会把绝对路径转换成 repo-relative path，再追加写入当前 turn 的 `artifacts.jsonl`。

Manifest 仍是 JSON Lines 格式。每一行表示一个产物声明：

```json
{"path":"report.md","title":"项目报告","description":"最终报告","mime_type":"text/markdown","artifact_type":"file","is_final":true}
```

字段：

| 字段 | 必填 | 说明 |
| --- | --- | --- |
| `path` | 是 | 相对 project repo 的文件路径；由 `artifact_declare` 写入时自动转换 |
| `title` | 否 | 前端展示名，空值时使用文件名 |
| `description` | 否 | 产物说明 |
| `mime_type` | 否 | 空值时由系统探测 |
| `artifact_type` | 否 | 默认 `file` |
| `is_final` | 是 | 只有 `true` 才进入最终产物列表 |

内置 runtime 会通过 `ToolContext.Metadata` 注入 `repo_dir` 和 `artifact_manifest_path`，供 `artifact_declare` 定位 manifest。

外部 CLI/MCP 路径当前有临时 fallback：如果工具上下文没有注入 manifest 信息，`artifact_declare` 会从 artifact 文件路径向上查找 `.leros`，再选择最新 task/turn 的 `artifacts.jsonl`。这是过渡方案，后续应改为给外部 CLI MCP 请求注入真实 run-scoped `ToolContext`。

## 10. 产物收集规则

Runtime 完成任务前读取当前 turn 的 `artifacts.jsonl`。

收集规则：

- 只处理 `is_final: true` 的记录。
- `path` 必须是相对 project repo 的路径。
- 不允许绝对路径。
- 不允许 `..`。
- 不允许软链逃逸。
- 不允许指向 `.git`、`.leros` 等运行态目录。
- 文件必须真实存在。
- 文件不能是目录。
- 系统补充 `mime_type`、`file_size`、`sha256`。
- 同一路径重复声明时，保留最后一次有效声明。

未声明文件：

- 如果 `artifacts.jsonl` 中没有 `is_final: true` 的记录，系统会自动通过 baseline diff 检测执行期间的新增/修改文件。
- 自动检测到的文件会补写入 `artifacts.jsonl`，后续按正常 manifest 流程处理。

### 10.1 文件变更自动检测 (Baseline & Reconcile)

Pipeline 在执行前后各插入一个新步骤，用于自动检测 Agent 执行期间产生但未显式声明的新文件。

**步骤顺序：**
```
NormalizeStep → ... → ArtifactBaselineStep → ExecuteStep → ArtifactReconcileStep → ArtifactStep → ...
```

**ArtifactBaselineStep：**
- 扫描 `repo/` 下所有文件，应用忽略规则后写入 `baseline.jsonl`。
- 每条记录：`{"path":"src/report.md","size":1234,"mtime_unix_nano":1780000000000}`

**ArtifactReconcileStep：**
- 再次扫描 `repo/`，读取 `baseline.jsonl`，对比执行前后文件状态。
- 若 `artifacts.jsonl` 已有 `is_final:true` 的显式声明 → 跳过 diff（显式声明优先）。
- 若无显式声明 → 将新增或变更文件作为候选补写入 `artifacts.jsonl`。
- 删除的文件、未变化文件、被忽略文件不进入候选。

**忽略规则：**
1. Git 标准 ignore：`.gitignore`、`.git/info/exclude`，通过 `git check-ignore --stdin --no-index` 实现。
2. Leros 内置排除：`.git/`、`.leros/`、`tmp/`、`temp/`、`logs/`、`log/`、`.cache/`、`node_modules/`、`vendor/`、`dist/`、`build/`、`target/`。
3. 文件级排除：`.DS_Store`、`Thumbs.db`、`*.swp`、`*.swo`、`*.log`。

**变更检测：**
- baseline 中不存在的文件 → 新增。
- size 或 mtime 不同 → 候选修改。
- size 和 mtime 都相同 → 跳过。

**source 标记：**
- 显式声明产物：`artifact_source = "agent_declared"`（不变）。
- 自动检测产物：`artifact_source = "diff"`（新增常量 `types.ArtifactSourceDiff`）。

## 11. 持久化模型

当前 artifact 表结构位于 `backend/types/artifact.go`。

当前关键字段：

```text
public_id
org_id
owner_id
project_id
task_id
session_id
message_id
title
filename
description
artifact_type
file_url
mime_type
file_size
relative_path
storage_key
sha256
source
status
created_at
```

字段说明：

| 字段 | 说明 |
| --- | --- |
| `public_id` | 对外稳定 artifact id，例如 `art_xxx` |
| `project_id` | DB 内部 project id |
| `task_id` | DB 内部 task id |
| `session_id` | DB 内部 session id |
| `message_id` | DB 内部 assistant message id；artifact 初始创建时可为空，message 完成时绑定 |
| `relative_path` | 相对 project repo 的路径 |
| `storage_key` | 相对 Worker workspace root 的路径，用于 Server 下载映射 |
| `sha256` | 文件内容 hash |
| `source` | `agent_declared`（显式声明）或 `diff`（自动检测） |
| `status` | 当前完成态为 `completed` |

注意：

- 当前 artifact 表没有 `request_id` 字段。
- 当前 artifact 表没有 `worker_id` 字段；下载临时假设 `worker_id = 1`。
- 多轮归属在执行期由 `request_id` 区分 turn manifest；持久化后主要通过 `message_id` 和 session/task 归属区分。

## 12. API 设计

当前已注册接口：

```text
GET /v1/tasks/{task_id}/artifacts
GET /v1/artifacts/{artifact_id}/download
```

当前未实现但可后续扩展：

```text
GET /v1/messages/{message_id}/artifacts
GET /v1/tasks/{task_id}/artifacts?group_by=turn
```

任务产物列表当前返回轻量信息，不返回下载 URL，也不返回内部路径：

```json
[
  {
    "artifact_id": "art_xxx",
    "title": "项目报告",
    "filename": "report.md",
    "description": "最终报告",
    "artifact_type": "file",
    "mime_type": "text/markdown",
    "file_size": 123456,
    "sha256": "..."
  }
]
```

下载接口当前流程：

- 根据 `artifact_id` 和当前用户 org 查询 artifact。
- 使用 artifact 的 `storage_key` 和 Server 侧 worker workspace 挂载规则解析真实文件路径。
- 校验 `storage_key` 不为空、不是绝对路径、不会逃逸 worker workspace。
- 打开文件并返回文件内容。
- 不向前端暴露容器或宿主机绝对路径。

## 13. 多轮对话产物归属

同一个 task 内可能发生多轮对话：

```text
task_1
  request_1 -> assistant message A -> artifacts: a.pptx
  request_2 -> assistant message B -> artifacts: b.xlsx
  request_3 -> assistant message C -> artifacts: a.pptx
```

当前实现中的归属链：

```text
执行期:
  org_id + project_id + task_id + request_id
  -> repo/.leros/tasks/{task_id}/turns/{request_id}/artifacts.jsonl

持久化:
  artifact -> project_id + task_id + session_id
  assistant message 完成后 -> artifact.message_id
```

因此当前系统可以稳定回答：

- 某个 task 当前累计有哪些 completed artifacts。
- 某条 assistant message 底部应该展示哪些已绑定附件。

当前系统还不能仅依赖 artifact 表直接回答：

- 某个 `request_id` 生成了哪些 artifact。
- 多 worker 场景下某个 artifact 必然位于哪个 worker workspace。

这两个能力分别需要补齐 `request_id` 或明确的 turn 归属字段，以及 `worker_id` 持久化。

## 14. v1 历史文件限制

当前不实现本地文件快照目录。因此如果后续轮次覆盖了同名文件，历史 artifact 的 `relative_path` 和 `storage_key` 可能指向被覆盖后的当前文件。

当前仍记录：

```text
message_id
relative_path
storage_key
sha256
created_at
```

这些字段可用于识别归属和检测内容是否已变化，但不能冻结历史文件内容。

如果需要冻结历史文件内容，应进入后续版本，通过 Git、S3/MinIO 或本地快照目录实现。

## 15. 后续扩展

### 15.1 持久化 worker_id

Artifact 下载当前需要知道产物来自哪个 worker workspace，但 artifact 表没有 `worker_id`。多 worker 场景应优先补齐：

```text
worker_id
```

下载时使用 artifact 自身的 `org_id + worker_id + storage_key` 解析 Server 侧文件路径。

### 15.2 request/turn 维度查询

如果产品需要按“哪一轮 request 生成了什么”直接查询，需在持久化模型中增加：

```text
request_id
```

或引入独立 run/turn 表，将 artifact 关联到 run/turn。

### 15.3 Git 历史文件

后续可在任务完成时提交或记录 blob：

```text
git_commit
git_blob
```

下载时按 commit/blob 读取历史版本，避免同名文件覆盖问题。

### 15.4 S3 / MinIO

后续可在任务完成后上传 artifact 文件到对象存储：

```text
storage_backend = s3
storage_key = ...
```

下载接口保持不变，前端无感知。

### 15.5 本地文件快照目录

如需本地冻结历史文件，可后续引入：

```text
repo/.leros/tasks/{task_id}/artifacts/{artifact_id}/
```

该目录不属于当前实现范围。

### 15.6 attempt / retry

如果同一个 request 需要多次执行，可扩展：

```text
turns/{request_id}/attempts/{attempt_id}/
```

当前暂不引入 attempt 维度。

## 16. 当前验收标准

- 文档明确 Worker 侧 workspace 路径使用 `projects/{org_id}/{project_id}/repo`。
- 文档明确多轮 manifest 目录使用 `tasks/{task_id}/turns/{request_id}`。
- 文档明确 Server 不通过 worker task 协议传递内部派生路径。
- 文档明确 Worker 准备 workspace 后会把 `req.Runtime.WorkDir` 改写为 `EffectiveWorkDir`。
- 文档明确 Worker 侧 `storage_key` 与 Server 侧挂载路径的映射关系。
- 文档明确当前 artifact 表不持久化 `request_id` 和 `worker_id`，以及这两个缺口对查询和下载的影响。
- 文档明确最终产物来自 manifest 或 `artifact_declare` 显式声明。
- 文档不要求当前实现本地 artifact 快照目录。
