# Agent Workspace 与最终产物设计

## 1. 背景与目标

本文档说明当前 Agent 运行时工作区、任务多轮对话产物归属、最终产物声明、Worker 与 Server 文件路径映射关系。

当前实现目标：

- 为每个 `org/project/task/request` 准备隔离的 Agent 工作区。
- 同一个 task 内按 `request_id` 拆分 turn 目录，保证每轮产物声明可单独收集。
- 最终产物通过 manifest / `artifact_declare` 显式声明，并辅以执行前后文件变更自动兜底扫描。
- 产物通过 `FileUpload` + `ProjectFile` 持久化后按任务维度返回给前端，下载接口只暴露 `artifact_id`。
- 内部真实路径不通过对外 API 暴露。

当前实现不做：

- 不实现正文中的 inline 附件展示。
- 不实现本地 artifact 文件快照目录。
- 不通过 worker task 协议或 `RuntimeOptions` 传递 `ProjectDir`、`TmpDir`、`ArtifactManifestPath` 等内部派生路径。
- 不设计大量 workspace 环境变量。
- `ProjectFile` 不额外持久化 `request_id`、`session_id` 或 `message_id`。`request_id` 用于执行期 turn 路径和 manifest 查找；消息维度的产物引用由 `SessionMessage.Artifacts` 保存。

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
| Git ignore 规则 | `backend/internal/workspace/ignore_checker.go` |
| Git diff 产物发现与 tree 快照 | `backend/internal/workspace/git_diff.go` |
| Artifact 持久化（文件上传 + 项目文件关联） | `backend/internal/runnable/session_run_state_projector.go` |
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
- 创建 turn 的 `tmp`、`logs` 和 `artifacts.jsonl`。
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

Worker 侧生成文件后，通过预签名 URL 上传到对象存储（S3/MinIO），创建 `FileUpload` 记录并在 `ProjectFile` 中关联项目/任务。Server 侧通过 `FileUpload.StorageURI` 直接从对象存储下载，不依赖本地 Worker workspace 挂载路径。

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
12. Worker 通过预签名 URL 上传产物，并向运行事件流追加包含 `storage_uri` 的 `artifact.declared`。
13. Server 的事件投影在事务中创建 `FileUpload` + `ProjectFile`。
14. Server 创建最终 assistant message 时，把终态事件中的轻量 artifact references 写入 `SessionMessage.Artifacts`。
15. 前端通过 task artifact 接口查询，通过 artifact download 接口下载。

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

### 10.1 文件变更自动检测 (Git Diff)

当 `artifacts.jsonl` 中没有显式的 `is_final: true` 声明时，系统使用 Git tree diff 自动检测执行期间的文件变更。

**流程：**

1. **执行前**：`CapturePreRunTree()` 使用临时 `GIT_INDEX_FILE` 将当前工作树（含 staged、unstaged、untracked 文件）写入一个 Git tree 对象，记录 tree SHA 到 `WorkspacePreparation.PreRunTreeSHA`。

2. **执行后**：`GitDiffReconcile()` 首先检查 manifest 是否已有显式 `is_final: true` 条目：
   - 若有 → 跳过（显式声明优先）。
   - 若无 → 再次调用 `CapturePreRunTree()` 获取执行后 tree SHA，然后通过 `git diff-tree -r --diff-filter=ACMRT` 计算两个 tree 的差异。

3. **Diff 过滤**：
   - 纳入：新增 (A)、修改 (M)、复制 (C)、重命名 (R)、类型变化 (T) 后的文件。
   - 排除：删除 (D)、目录、`.leros/` 运行态文件、`.gitignore` 排除的文件。
   - 只保留真实存在的常规文件。

4. **结果写入**：将 diff 检测到的文件以 `is_final: true` 追加到 `artifacts.jsonl`，后续按正常 manifest 流程处理。

**source 标记：**
- 显式声明产物：`artifact_source = "agent_declared"`（不变）。
- Git diff 检测产物：`artifact_source = "diff"`。

**注意：**旧的 `baseline.jsonl` + mtime/size 对比方案（`FileSnapshot`、`WriteBaseline`、`ReconcileArtifacts`）已废弃并删除。Git tree diff 不需要落地临时文件，且天然兼容 Git 的 rename/copy 检测。

## 11. 持久化模型

产物不再使用独立的 `leros_artifact` 表。持久化统一通过 `leros_file_upload` + `leros_project_file` 两张表实现。

### FileUpload（`backend/types/file_upload.go`）

记录文件的物理存储信息：

| 字段 | 说明 |
| --- | --- |
| `public_id` | 对外文件 ID，格式 `file_xxx`，同时作为产物 ID |
| `org_id` | 归属组织 |
| `owner_id` | 归属用户 |
| `filename` | 文件名 |
| `original_name` | 原始文件名 |
| `mime_type` | MIME 类型 |
| `file_size` | 文件大小 |
| `storage_uri` | 对象存储 URI（S3/本地） |
| `sha256` | 文件内容 hash |
| `purpose` | 用途标记，产物为 `artifact` |

### ProjectFile（`backend/types/project_file.go`）

记录文件与项目/任务的关联关系：

| 字段 | 说明 |
| --- | --- |
| `file_public_id` | 关联 FileUpload.public_id（唯一索引） |
| `org_id` | 归属组织 |
| `project_id` | 关联项目 |
| `task_id` | 关联任务 |
| `resource_id` | 关联的资源 ID（FileUpload.ID） |
| `resource_type` | 资源类型，产物为 `artifact` |

### 事件投影

`artifact.declared` 事件到达后，`PersistDeclaredArtifact` 在事务中：
1. 校验 Session、Project、Task 和组织归属。
2. 通过 `RecordUpload` 创建 FileUpload（使用事件中的 `artifact_id` 作为 PublicID）。
3. 创建 ProjectFile（`resource_type=artifact`, `resource_id=FileUpload.ID`）。
4. 通过 `ProjectFile.file_public_id` 唯一索引实现事件重投幂等。
5. `storage_uri` 为空时跳过持久化（ProjectFile 无法指向不可访问文件）。

### 查询兼容

### 查询兼容

旧 `ListTaskArtifacts`、`GetArtifact` 和 `GET /v1/artifacts/{artifact_id}/download` 已删除。
产物文件现统一通过 ProjectFile 接口访问：
- 项目/任务产物列表：`GET /v1/projects/{project_id}/files?resource_type=artifact&task_id={task_id}`
- 下载：`GET /v1/files/{id}/download`
- 文件节点返回 `public_id`、`storage_uri`、文件名、大小、MIME、SHA256 和创建时间。

注意：
- 不再兼容旧的 `art_*` 格式 ID。
- 旧的 `leros_artifact` 表在启动迁移中通过 `DROP TABLE` 直接删除，不回填历史数据。
- `artifact_declare` 工具、`artifacts.jsonl` manifest、Artifact 事件和 SessionMessage 中的 Artifact 元数据继续保留。

## 12. API 设计

当前已注册接口：

```text
GET /v1/projects/{project_id}/files?resource_type=artifact&task_id={task_id}
GET /v1/projects/{project_id}/files/download?path={path}
GET /v1/files/{id}/download
```

产物文件列表通过 ProjectFile 接口返回，文件节点格式：

```json
[
  {
    "name": "report.md",
    "path": "artifacts/report.md",
    "type": "file",
    "size": 123456,
    "mime_type": "text/markdown",
    "created_at": 1700000000,
    "public_id": "file_xxx",
    "storage_uri": "s3://...",
    "sha256": "..."
  }
]
```

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
  FileUpload -> 文件元数据 + StorageURI
  ProjectFile -> project_id + task_id + FileUpload
  SessionMessage.Artifacts -> 当前 assistant message 的轻量产物引用
```

因此当前系统可以稳定回答：

- 某个 task 当前累计关联了哪些产物文件。
- 某条 assistant message 底部应该展示哪些产物引用。

当前持久化模型不能仅依赖 `ProjectFile` 直接回答：

- 某个 `request_id` 生成了哪些产物。

该能力需要增加明确的 run/turn 归属关系；文件下载不依赖 Worker workspace，而是直接使用 `FileUpload.StorageURI`。

## 14. v1 历史文件限制

当前不实现本地文件快照目录。每次产物上传都会形成独立的 `FileUpload.StorageURI`，因此后续轮次覆盖 Git 工作区中的同名文件，不会改变已经上传的历史对象。

当前持久化记录包括：

```text
FileUpload.public_id
FileUpload.storage_uri
FileUpload.sha256
ProjectFile.project_id
ProjectFile.task_id
ProjectFile.created_at
SessionMessage.Artifacts
```

这些字段可用于下载历史对象、识别项目/任务归属，并在消息中恢复当轮展示信息。当前没有独立的 `request_id` 查询索引。

## 15. 后续扩展

### 15.1 request/turn 维度查询

如果产品需要按“哪一轮 request 生成了什么”直接查询，需在持久化模型中增加：

```text
request_id
```

或引入独立 run/turn 表，将 `ProjectFile` 或消息产物引用关联到 run/turn。

### 15.3 Git 历史文件

后续可在任务完成时提交或记录 blob：

```text
git_commit
git_blob
```

下载时按 commit/blob 读取历史版本，避免同名文件覆盖问题。

### 15.5 本地文件快照目录

如需在对象存储之外保留 Worker 本地副本，可后续引入：

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
- 文档明确产物持久化统一通过 `FileUpload` + `ProjectFile`，不再使用独立 artifact 表。
- 文档明确下载接口通过 `FileUpload.StorageURI` 从对象存储获取文件。
- 文档明确 Git diff 产物发现是 best-effort，失败只记录日志不影响任务终态。
