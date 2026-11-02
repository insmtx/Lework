# 项目产物文件版本管理设计

## 1. 文档信息

| 项目 | 内容 |
| --- | --- |
| 设计范围 | Agent 产物版本识别、最新版本查询、历史版本查询、版本恢复 |
| 核心原则 | 复用现有工作区、Git diff、产物上传和 `FileUpload + ProjectFile` 模型 |
| 数据库改动 | `project_file` 仅新增 3 个字段 |
| 版本状态 | 最新版本由 `MAX(version_no)` 计算，不增加 `is_latest` 字段 |

## 2. 背景与结论

当前 Worker 在项目共享仓库中创建或修改产物。一次运行结束时，`finalizer` 会：

1. 使用 Git diff 补充未显式声明的变更文件。
2. 从 `artifacts.jsonl` 收集最终产物。
3. 按仓库相对路径读取文件并上传；上传后的存储文件名每次都不同。
4. 为产物生成新的 `file_public_id`，发送 `artifact.declared` 事件。
5. 提交并推送项目工作区变更。
6. Server 将产物落为一条 `FileUpload` 和一条 `ProjectFile`。

当前缺口是 `ProjectFile` 没有保存仓库相对路径、版本链根和版本号。因此，相同路径的多次产物上传会表现为多个独立文件，无法聚合最新版本，也无法查询历史版本。

本设计只给 `project_file` 增加以下字段：

```text
relative_path           相对文件路径
initial_file_public_id  初始文件 ID（版本链根）
version_no              版本号
```

同一项目内，相同 `relative_path` 的后续上传视为同一个逻辑文件的新版本。Git 明确认定为 rename 时，新路径通过本次事件携带的旧路径继承原版本链。首版的 `initial_file_public_id` 等于自己的 `file_public_id`；后续版本沿用首版 ID，版本号递增。

## 3. 设计目标

1. 同一路径文件再次生成时自动形成 `V2、V3...`。
2. 项目文件列表按 `initial_file_public_id` 聚合，只返回最大 `version_no`。
3. 任务文件列表可继续按 `task_id` 过滤，同时展示该过滤范围内每条版本链的最新版本。
4. 通过任意版本 `file_public_id` 查询完整历史。
5. 恢复历史版本时，将目标版本内容写回相对路径，走现有 Git 提交和产物上传流程，生成新的最大版本。
6. 不引入新的版本表，不依赖 `git_commit_sha`，不增加最新版本状态字段。

## 4. 当前实现依据

### 4.1 工作区与相对路径

同一项目的任务共享仓库：

```text
{workspaceRoot}/projects/{orgID}/{projectID}/repo
```

任务 turn 的 manifest 位于：

```text
repo/.lework/tasks/{taskID}/turns/{requestID}/artifacts.jsonl
```

产物相对路径以项目 `repo` 为根，例如：

```text
artifacts/投标交付材料清单.docx
```

### 4.2 Git diff 发现产物

`FinalizeRequired` 当前执行顺序：

```text
reconcileWorkspace
  -> GitDiffReconcile
  -> CollectFinalArtifacts
  -> uploadArtifacts
  -> pushWorkspace
  -> build RunResult
```

`GitDiffReconcile` 会识别新增、复制、修改、重命名和类型变化文件，并把仓库相对路径写入 `artifacts.jsonl`。如果 Agent 已调用 `artifact_declare`，则显式声明优先；匹配到 rename 时仍会为该声明补充临时 `previous_path` 元数据。

因此，版本识别需要的 `relative_path` 已经存在于：

```text
ManifestArtifact.Path
ArtifactRecord.RelativePath
ArtifactPayload.RelativePath
```

重命名链路额外使用 `ManifestArtifact.PreviousPath` 和 `ArtifactPayload.PreviousRelativePath` 传递旧路径。它们只用于本次版本归属判断，不写入 `project_file`，因此数据库仍只新增三个字段。

不需要重新扫描文件名，也不需要通过最终存储文件名推断路径。

### 4.3 产物上传

`uploadArtifact` 当前从以下路径读取内容：

```text
repoDir + record.RelativePath
```

最终存储 key 使用随机 ID 和扩展名：

```text
projects/{orgID}/{projectPublicID}/artifacts/{randomID}.docx
```

所以同一路径再次修改时：

- Worker 工作区修改的是同一个相对路径文件。
- 最终存储产生一个新的唯一文件，不覆盖旧版本。
- `artifact.declared` 产生新的 `file_public_id`。
- 数据库可以用新的 `file_public_id` 保存一个新版本。

### 4.4 当前数据库记录

当前 `ProjectFile`：

```go
type ProjectFile struct {
    gorm.Model
    FilePublicID string
    OrgID        uint
    ProjectID    uint
    TaskID       uint
    ResourceID   uint
    ResourceType ProjectFileResourceType
    Uin          uint
}
```

`FilePublicID` 唯一，可继续用于 `artifact.declared` 事件重放幂等；但当前没有版本链信息。

## 5. 数据模型

### 5.1 ProjectFile 新增三个字段

```go
type ProjectFile struct {
    gorm.Model
    FilePublicID string
    OrgID        uint
    ProjectID    uint
    TaskID       uint
    ResourceID   uint
    ResourceType ProjectFileResourceType
    Uin          uint

    RelativePath        string
    InitialFilePublicID string
    VersionNo           int
}
```

建议 GORM 标签：

```go
RelativePath        string `gorm:"column:relative_path;type:varchar(1000);not null;index"`
InitialFilePublicID string `gorm:"column:initial_file_public_id;type:varchar(255);not null;index"`
VersionNo           int    `gorm:"column:version_no;type:integer;not null;default:1"`
```

字段语义：

| 字段 | 说明 | 示例 |
| --- | --- | --- |
| `relative_path` | 项目仓库内的相对路径，直接来自 `ArtifactPayload.RelativePath` | `artifacts/report.docx` |
| `initial_file_public_id` | 版本链第一条记录的 `file_public_id` | `file_v1` |
| `version_no` | 版本链内从 1 开始递增的整数 | `1、2、3` |

不新增以下字段：

- `is_latest`：最新版本由 `MAX(version_no)` 计算。
- `git_commit_sha`：恢复内容从目标版本的 `FileUpload` 读取，Git 只负责提交恢复后的工作区。
- `version_source`、`version_note`、`restored_from_file_public_id`：一期不需要。

### 5.2 索引

建议保留并新增：

```text
unique(file_public_id)
index(org_id, project_id, resource_type, relative_path)
index(org_id, project_id, initial_file_public_id, version_no)
unique(org_id, project_id, initial_file_public_id, version_no)
```

`file_public_id` 继续保证单个产物事件幂等；版本唯一索引防止并发写入相同版本号。

## 6. 版本识别与写入

### 6.1 判定规则

当 `declaredArtifactPersister.PersistDeclaredArtifact` 收到产物时：

1. 读取 `ArtifactPayload.RelativePath` 并规范化为 `/` 分隔的仓库相对路径。
2. 按 `(org_id, project_id, resource_type=artifact, relative_path)` 查询当前最大版本。
3. 如果当前路径不存在且事件包含 `PreviousRelativePath`，按旧路径查询当前最大版本。
4. 如果仍不存在：
   - `initial_file_public_id = 当前 file_public_id`
   - `version_no = 1`
5. 如果存在：
   - `initial_file_public_id = 最新记录.initial_file_public_id`
   - `version_no = 最新记录.version_no + 1`
6. 创建新的 `FileUpload` 和 `ProjectFile`，不修改历史记录。

示例：

| file_public_id | relative_path | initial_file_public_id | version_no |
| --- | --- | --- | --- |
| `file_A` | `artifacts/report.docx` | `file_A` | 1 |
| `file_B` | `artifacts/report.docx` | `file_A` | 2 |
| `file_C` | `artifacts/report.docx` | `file_A` | 3 |

### 6.2 同名与同路径

版本识别使用完整相对路径，不使用 basename：

```text
artifacts/task-a/report.docx
artifacts/task-b/report.docx
```

以上是两个逻辑文件，各自从 V1 开始。

如果两个任务都修改：

```text
artifacts/report.docx
```

由于项目任务共享同一个仓库路径，第二次实际修改的是同一个文件，因此进入同一版本链。`TaskID` 记录每个版本由哪个任务产生，但不参与版本链身份判定。

### 6.3 事务与并发

版本创建必须在同一事务中完成：

```go
func createArtifactVersion(ctx context.Context, tx *gorm.DB, input CreateArtifactVersionInput) error {
    latest, err := getLatestProjectFileByPathForUpdate(
        ctx,
        tx,
        input.OrgID,
        input.ProjectID,
        types.ProjectFileResourceTypeArtifact,
        input.RelativePath,
    )
    if err != nil {
        return err
    }

    initialID := input.FilePublicID
    versionNo := 1
    if latest != nil {
        initialID = latest.InitialFilePublicID
        versionNo = latest.VersionNo + 1
    }

    file := &types.ProjectFile{
        FilePublicID:       input.FilePublicID,
        OrgID:              input.OrgID,
        ProjectID:          input.ProjectID,
        TaskID:             input.TaskID,
        ResourceID:         input.ResourceID,
        ResourceType:       types.ProjectFileResourceTypeArtifact,
        Uin:                input.Uin,
        RelativePath:       input.RelativePath,
        InitialFilePublicID: initialID,
        VersionNo:          versionNo,
    }
    return db.CreateProjectFile(ctx, tx, file)
}
```

实现时应对版本唯一索引冲突重试一次，避免两个 Worker 同时为同一路径计算出相同版本号。

### 6.4 事件重放

收到相同 `artifact_id` 的重复事件时，继续使用 `ProjectFile.FilePublicID` 唯一索引或现有查询直接跳过，不增加版本号。

## 7. finalizer 接入设计

### 7.1 不改变产物发现和上传主流程

保留当前顺序：

```text
Git diff / artifact_declare
       ↓
ArtifactRecord.RelativePath
       ↓
uploadArtifact（唯一存储文件名）
       ↓
ArtifactPayload.RelativePath
       ↓
artifact.declared
       ↓
FileUpload + ProjectFile 版本记录
       ↓
Git commit / push
```

### 7.2 必须补齐 RelativePath 的持久化

`finalizer_impl.go` 已将 `record.RelativePath` 写入 `ArtifactPayload.RelativePath`，Worker 无需新增路径算法。

需要修改的是 Server 端 `PersistDeclaredArtifact`：创建 `ProjectFile` 时把 `item.RelativePath` 写入 `relative_path`，然后计算版本链字段。

### 7.3 Git diff 路径要求

Git diff 回退需要保证：

- 新增和修改文件输出当前相对路径。
- 重命名文件输出新路径，并临时携带旧路径用于继承版本链。
- 路径统一使用 `/`。
- 删除文件不创建新版本。
- `.lework`、日志、临时文件继续排除。

特别需要为 Git rename/copy 的 `--name-status -z` 输出补充测试，确保解析器取到新路径，而不是把旧路径误当成产物路径。

### 7.4 不解析自然语言文件指代

版本模块不需要识别“上一个文件”，也不需要在发布任务前查询文件信息传给 Worker：

1. Worker 根据正常会话上下文和项目工作区执行文件修改。
2. `GitDiffReconcile` 在运行结束时识别实际发生变化的文件。
3. Git diff 或 `artifact_declare` 产出的相对路径进入 `ArtifactPayload.RelativePath`。
4. Server 在产物落库时按该相对路径查询最大版本并续版。

版本归属以“最终实际变化的相对路径”为准，不以用户原始话术、回复中的文件名或预先推断结果为准。无需修改 `message_poster`、Worker command 或 Agent 提示词来支持版本功能。

## 8. 项目文件查询

### 8.1 最新文件列表

项目文件页不再返回所有 `ProjectFile`，而是按 `initial_file_public_id` 分组并取最大 `version_no`。

PostgreSQL 可使用：

```sql
SELECT DISTINCT ON (initial_file_public_id) *
FROM project_file
WHERE org_id = $1
  AND project_id = $2
  AND resource_type != 'plan'
ORDER BY initial_file_public_id, version_no DESC, created_at DESC;
```

如果指定 `task_id`，先按任务过滤，再在过滤结果中按版本链取最大版本：

```sql
SELECT DISTINCT ON (initial_file_public_id) *
FROM project_file
WHERE org_id = $1
  AND project_id = $2
  AND task_id = $3
  AND resource_type = 'artifact'
ORDER BY initial_file_public_id, version_no DESC, created_at DESC;
```

注意：项目级查询显示版本链的全局最新版本；任务级查询显示该任务产生过的各版本链中版本号最大的记录。

### 8.2 最新版本节点

列表节点新增：

```json
{
  "name": "report.docx",
  "path": "artifacts/report.docx",
  "public_id": "file_C",
  "initial_file_public_id": "file_A",
  "version_no": 3,
  "version_label": "V3",
  "version_count": 3,
  "size": 20480,
  "created_at": 1783410540
}
```

`version_count` 可以使用最大 `version_no`，无需额外字段。

### 8.3 历史版本查询

```http
GET /projects/{project_id}/files/{file_public_id}/versions
```

查询步骤：

1. 用 `file_public_id` 查询目标 `ProjectFile`。
2. 读取 `initial_file_public_id`。
3. 按 `(org_id, project_id, initial_file_public_id)` 查询全部版本。
4. 按 `version_no DESC` 返回。

```sql
SELECT *
FROM project_file
WHERE org_id = $1
  AND project_id = $2
  AND initial_file_public_id = $3
ORDER BY version_no DESC;
```

每个版本通过自己的 `file_public_id -> FileUpload` 精确预览和下载，不再按文件名匹配。

## 9. 历史版本恢复

### 9.1 API

```http
POST /projects/{project_id}/files/{file_public_id}/restore
```

路径参数中的 `file_public_id` 是要恢复的具体历史版本。

### 9.2 恢复流程

假设版本链当前为 V1、V2、V3，用户选择恢复 V1：

1. 根据 `file_public_id` 查询 V1 的 `ProjectFile` 和 `FileUpload`。
2. 校验文件属于当前组织和项目。
3. 从 V1 的唯一存储文件读取内容。
4. 将内容写回 `repo/{relative_path}`，覆盖工作区当前内容。
5. 对该相对路径执行 Git add、commit，并在存在远端时 push。
6. 使用现有 `filestore.Upload` 上传恢复后的唯一文件快照，产生新的 `file_public_id`。
7. 使用 `CreateProjectFileVersion` 创建新的版本记录。
8. 新记录沿用版本链的 `initial_file_public_id`，版本号取当前最大版本加 1。

恢复 API 是 Server 侧操作，不重新启动 Worker run，也不调用 Worker 的 `FinalizeRequired`；它复用与 finalizer 相同的工作区、Git、唯一文件上传和版本落库原则。

恢复结果：

| file_public_id | 内容 | initial_file_public_id | version_no |
| --- | --- | --- | --- |
| `file_A` | 原 V1 内容 | `file_A` | 1 |
| `file_B` | V2 内容 | `file_A` | 2 |
| `file_C` | V3 内容 | `file_A` | 3 |
| `file_D` | 恢复后的 V1 内容 | `file_A` | 4 |

项目文件列表按最大版本查询后显示 `file_D / V4`。

### 9.3 为什么不直接修改最新记录的 file_public_id

不建议把 V3 记录的 `file_public_id` 更新为 V1：

- `file_public_id` 当前有唯一索引，不能被两条记录重复引用。
- 更新会破坏 V3 原有内容与记录之间的关系。
- 恢复操作本身也是一次文件变更，应形成一个可追踪的新版本。

因此，“恢复到某版本”在用户体验上替换当前内容，在数据模型上创建 `MAX(version_no)+1` 的新版本。

## 10. API 与 DAO 改造

### 10.1 API

```http
GET  /projects/{project_id}/files
GET  /projects/{project_id}/files/{file_public_id}/versions
GET  /projects/{project_id}/files/{file_public_id}/download
POST /projects/{project_id}/files/{file_public_id}/restore
```

保留现有按 path 下载接口作为兼容入口，但新页面和历史版本必须使用 `file_public_id` 下载。

### 10.2 DAO

新增或调整：

```text
GetProjectFileByFilePublicID
GetLatestProjectFileByRelativePathForUpdate
ListLatestProjectFiles
ListLatestProjectFilesByTask
ListProjectFileVersions
GetMaxProjectFileVersion
CreateProjectFile
```

## 11. 前端交互

### 11.1 项目文件页

- 每条版本链只显示最大版本。
- 展示 `V{version_no} · 文件大小 · 创建时间`。
- 同名不同路径是两条文件记录。
- 查看和下载使用当前版本 `file_public_id`。

### 11.2 任务详情文件栏

- 使用任务过滤后的最新版本结果。
- 卡片展示版本号。
- 点击文件进入精确版本预览。

### 11.3 历史版本面板

- 点击“版本历史记录”图标后展开右侧版本轴。
- 版本按 `version_no DESC` 展示。
- 点击历史版本后，用对应 `file_public_id` 预览和下载。
- 历史版本提供“恢复”操作。
- 恢复成功后刷新列表，当前版本变为新生成的最大版本。

## 12. 数据迁移

### 12.1 新字段默认值

迁移初期字段允许为空，完成回填后再设置 `NOT NULL`：

```text
relative_path           nullable -> backfill -> not null
initial_file_public_id  nullable -> backfill -> not null
version_no              default 1 -> not null
```

### 12.2 旧数据回填

旧数据没有真实相对路径，只能按当前展示规则回填：

```text
artifact    -> artifacts/{FileUpload.original_name}
user_upload -> uploads/{FileUpload.original_name}
```

同一项目、资源类型、回填路径下有多条记录时：

1. 按 `created_at ASC, id ASC` 排序。
2. 第一条的 `file_public_id` 作为 `initial_file_public_id`。
3. 从 1 开始依次设置 `version_no`。

由于历史数据已丢失目录层级，同名但原本不同目录的旧文件可能被合并。迁移前应输出冲突清单供确认；新数据不会有这个问题。

## 13. 测试计划

### 13.1 后端

- 首次上传相对路径生成 V1，初始文件 ID 等于当前文件 ID。
- 相同项目、相同相对路径再次上传生成 V2。
- 同名不同相对路径分别生成 V1。
- 不同任务修改相同项目路径时进入同一版本链。
- 相同 `artifact_id` 事件重放不增加版本。
- 并发写入不产生重复版本号。
- 项目列表按版本链返回最大版本。
- 任务过滤后返回该任务范围内的最大版本。
- 通过任意版本 ID 都能查询完整历史。
- 历史版本预览和下载使用指定 `file_public_id`。
- 恢复 V1 后生成新的最大版本，V2/V3 仍可查询。
- Git diff 新增、修改、重命名路径均能正确进入 `RelativePath`。

### 13.2 前端

- 项目文件页不平铺历史版本。
- 版本号、文件大小、创建时间正确展示。
- 版本历史面板顺序正确。
- 点击版本项切换预览内容。
- 下载的是当前预览版本。
- 恢复成功后列表与预览切换到新版本。

## 14. 实施顺序

1. 给 `ProjectFile` 增加三个字段和索引。
2. 完成旧数据回填和冲突清单。
3. 在 `PersistDeclaredArtifact` 中持久化 `RelativePath` 并计算版本号。
4. 调整项目文件 DAO，按初始文件 ID 聚合最大版本。
5. 新增历史版本、按文件 ID 下载和恢复 API。
6. 接入前端版本展示、历史版本面板和恢复操作。
7. 补充 Git diff、并发版本和恢复测试。

## 15. 验收标准

1. `project_file` 只新增 `relative_path`、`initial_file_public_id`、`version_no`。
2. 相同项目相对路径的多次产物上传形成连续版本号。
3. 项目文件列表只显示每条版本链的最大版本。
4. 历史版本可以通过任意 `file_public_id` 查询、预览和下载。
5. 无需修改 Worker 输入协议；版本归属完全由实际产物相对路径决定。
6. 恢复历史版本会写回工作区、提交 Git、重新上传并形成新版本。
7. 恢复不会修改或删除已有历史记录。
