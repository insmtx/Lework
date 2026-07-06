# Lework 统一资源权限设计文档

## 1. 背景与目标

Lework 后续会同时承载 Project、KnowledgeBase、Folder、File、Task、Artifact、Assistant、Workflow 等对象。如果每类对象都单独设计成员表和权限表，权限体系会快速分裂，例如 `ProjectMember`、知识库授权、文件授权、任务授权各自维护一套逻辑。

目标方案采用统一资源权限模型：

- 所有可授权对象都是 `leros_resources`。
- 所有主体和资源的身份关系都是 `leros_resource_bindings`。
- 具体身份能做什么由代码中的 `PermissionPolicy` 维护。
- 所有业务入口统一调用 `PermissionService.Can(...)`、`Explain(...)` 或 `BatchCan(...)`。
- 权限支持资源树继承。

`ProjectMember` 不再作为长期权限来源。当前项目如已有 `project_members`，只作为迁移期兼容数据，最终权限来源应收敛到 `resources + resource_bindings + PermissionPolicy`。

## 2. 核心概念

### 2.1 Resource

`Resource` 表示系统内所有可授权、可占用、可继承权限的对象。

当前和后续资源类型统一放入同一张表：

```text
project
knowledge_base
folder
file
task
artifact
assistant
workflow
```

资源之间通过 `parent_resource_id` 和 `parent_resource_path_ids` 形成树。

一期资源树：

```text
Project(id=1)
  ├── File(id=2, parent=1)
  └── Artifact(id=3, parent=1)
```

一期资源父子关系：

```text
project  -> file, artifact
file     -> none
artifact -> none
```

### 2.2 Principal

`Principal` 表示被授权主体。

当前支持：

```text
user
assistant
```

### 2.3 Identity

`Identity` 表示主体在某个资源上的资源身份，不是传统 RBAC 角色。

当前只内置：

```text
owner
admin
member
```

身份本身只表达“这个主体和这个资源的关系”。它不直接保存动作权限，动作权限由代码态 `PermissionPolicy` 解释。

### 2.4 Binding

`ResourceBinding` 表示：

```text
哪个主体，在哪个资源上，具有什么资源身份。
```

示例：

```text
ProjectA -> Alice -> owner
ProjectB -> Bob   -> admin
ProjectC -> Carol -> member
```

如果某个资源没有直接绑定，`PermissionService` 会沿资源树向上查找可继承绑定。一期成员关系只在 Project 资源上维护，File / Artifact 不提供成员管理入口，也不创建直接成员 binding。

### 2.5 Policy

`PermissionPolicy` 不入库，由代码配置维护：

```text
resource_type -> identity -> allowed actions
```

例如：

```text
project:
  owner -> project:view, project:update, project:delete, project:archive, project:member.create, project:member.update, project:member.delete, project:member.list
  admin -> project:view, project:update, project:member.create, project:member.update, project:member.delete, project:member.list
  member -> project:view, project:member.list, project:member.leave
file:
  owner -> file:view, file:download
  admin -> file:view, file:download
  member -> file:view, file:download
artifact:
  owner -> artifact:view, artifact:download
  admin -> artifact:view, artifact:download
  member -> artifact:view, artifact:download
```

这样新增动作，例如 `project:clone`，只需要改代码配置，不需要改数据库。

## 3. 数据模型

### 3.1 resources

用途：统一表达所有资源，并通过父子关系形成资源树。

```sql
CREATE TABLE IF NOT EXISTS leros_resources (
    id BIGSERIAL PRIMARY KEY,
    org_id INT8 NOT NULL,
    department_id INT8 NULL,
    type VARCHAR(50) NOT NULL,
    biz_id INT8 NOT NULL,
    parent_resource_id INT8 NULL,
    parent_resource_path_ids INT8[] NOT NULL DEFAULT '{}',
    created_at TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP,
    updated_at TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP,
    deleted_at TIMESTAMPTZ NULL,
    CONSTRAINT fk_leros_resources_parent
        FOREIGN KEY (parent_resource_id)
        REFERENCES leros_resources (id)
);

CREATE UNIQUE INDEX IF NOT EXISTS ux_leros_resources_active_biz
    ON leros_resources (org_id, type, biz_id)
    WHERE deleted_at IS NULL;

CREATE INDEX IF NOT EXISTS idx_leros_resources_org_type
    ON leros_resources (org_id, type)
    WHERE deleted_at IS NULL;

CREATE INDEX IF NOT EXISTS idx_leros_resources_parent
    ON leros_resources (parent_resource_id)
    WHERE deleted_at IS NULL;

CREATE INDEX IF NOT EXISTS idx_leros_resources_parent_resource_path_ids
    ON leros_resources USING GIN (parent_resource_path_ids)
    WHERE deleted_at IS NULL;
```

字段说明：

- `org_id`：组织隔离字段，所有鉴权必须先校验组织。
- `department_id`：部门预留字段，当前不参与权限判断。
- `type`：资源类型，PermissionService 不按业务表分支鉴权，只按资源类型和 action 判断。合法值由业务代码控制，不通过数据库 CHECK 限制。
- `biz_id`：业务对象内部 ID，例如 `projects.id`、文件 ID、artifact ID。
- `parent_resource_id`：父资源 ID。根资源为空，例如 Project。
- `parent_resource_path_ids`：按资源树顺序保存祖先资源 ID 数组，用于快速查找资源祖先。例如 Project 下的 File 保存为 `{project_resource_id}`。
一致性规则：

- 同一组织下，同一 `type + biz_id` 只能对应一条有效资源。
- 子资源的 `org_id` 必须与父资源一致。
- 子资源的 `department_id` 应与父资源一致；当前只作为预留字段，不参与鉴权。
- 根资源的 `parent_resource_id` 为空，`parent_resource_path_ids` 为空数组。
- 子资源的 `parent_resource_path_ids` 必须等于父资源的 `parent_resource_path_ids + 父资源 id`。
- 软删除资源不得再作为权限判断对象。
- 业务表创建对象时必须同步创建对应 `leros_resources` 记录。
- 资源类型合法性由 ResourceService 校验；一期只允许创建 `project`、`file`、`artifact`。

### 3.2 resource_bindings

用途：表达主体在资源上的身份。它替代长期意义上的 `ProjectMember` 表。

```sql
CREATE TABLE IF NOT EXISTS leros_resource_bindings (
    id BIGSERIAL PRIMARY KEY,
    org_id INT8 NOT NULL,
    department_id INT8 NULL,
    resource_id INT8 NOT NULL,
    principal_type VARCHAR(50) NOT NULL,
    principal_id INT8 NOT NULL,
    identity VARCHAR(50) NOT NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP,
    updated_at TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP,
    deleted_at TIMESTAMPTZ NULL,
    CONSTRAINT fk_leros_resource_bindings_resource
        FOREIGN KEY (resource_id)
        REFERENCES leros_resources (id)
);

CREATE UNIQUE INDEX IF NOT EXISTS ux_leros_resource_bindings_active_principal
    ON leros_resource_bindings (resource_id, principal_type, principal_id)
    WHERE deleted_at IS NULL;

CREATE INDEX IF NOT EXISTS idx_leros_resource_bindings_principal
    ON leros_resource_bindings (org_id, principal_type, principal_id)
    WHERE deleted_at IS NULL;

CREATE INDEX IF NOT EXISTS idx_leros_resource_bindings_resource
    ON leros_resource_bindings (resource_id)
    WHERE deleted_at IS NULL;
```

字段说明：

- `org_id`：冗余组织字段，用于快速过滤和防止跨组织误查。
- `department_id`：部门预留字段，当前不参与权限判断。
- `resource_id`：绑定的资源。
- `principal_type/principal_id`：被授权主体；`principal_id` 使用主体内部 INT8 ID。
- `identity`：资源身份，当前业务只允许 `owner`、`admin`、`member`。合法值由服务层和 PermissionPolicy 控制，不通过数据库 CHECK 限制。
一致性规则：

- `leros_resource_bindings.org_id` 必须与对应 `leros_resources.org_id` 一致。
- `leros_resource_bindings.department_id` 必须与对应 `leros_resources.department_id` 一致。
- 同一资源上，同一主体只能有一条有效绑定。
- 不在数据库保存 `project:view`、`project:member.update` 等动作。
- `principal_type`、`identity` 的合法值与代码态 `PermissionPolicy` 保持一致，由服务层统一校验。
- 一期 project/file/artifact 不使用“最近绑定覆盖”作为最终规则。
- 一期 file/artifact 使用身份合成：`effective_identity = max(direct_identity, inherited_identity)`。
- “最近绑定覆盖”不作为一期规则；如后续需要更复杂的目录权限，再单独设计覆盖语义。

### 3.3 PermissionPolicy

`PermissionPolicy` 由代码维护，不建表。

示例结构：

```text
project:
  owner:
    - "project:view"
    - "project:update"
    - "project:delete"
    - "project:archive"
    - "project:member.create"
    - "project:member.update"
    - "project:member.delete"
    - "project:member.list"

  admin:
    - "project:view"
    - "project:update"
    - "project:member.create"
    - "project:member.update"
    - "project:member.delete"
    - "project:member.list"

  member:
    - "project:view"
    - "project:member.list"
    - "project:member.leave"

file:
  owner:
    - "file:view"
    - "file:download"

  admin:
    - "file:view"
    - "file:download"

  member:
    - "file:view"
    - "file:download"

artifact:
  owner:
    - "artifact:view"
    - "artifact:download"

  admin:
    - "artifact:view"
    - "artifact:download"

  member:
    - "artifact:view"
    - "artifact:download"
```

规则：

- 新增 action 只改代码配置，不改数据库。
- PermissionPolicy 必须按资源类型限定动作，当前配置 `project`、`file`、`artifact`。
- `owner` 不使用通配符，必须显式列出当前资源类型允许的动作。
- 成员管理动作仅限 `project:member.*`，并且必须结合后端派生上下文判断目标身份，不能只看粗 action。
- 继承得到的 identity 必须按照目标资源类型解释权限。例如 Project 上继承来的 `admin` 访问 File 时，使用 `file.admin` policy，而不是 `project.admin` policy。
- 一期不单独建模 `file:create`、`artifact:create` 动作；File / Artifact 的创建能力随项目业务流程处理，创建成功后同步写入资源树。
- 一期 File / Artifact 不提供 `member.*` 动作；成员只在 Project 下维护，File / Artifact 通过资源树继承 Project 身份并按自身资源类型解释动作。
- 未命中 policy 时默认拒绝。

成员管理的请求输入只表达操作意图，不能作为可信权限上下文。权限相关上下文必须由后端根据数据库计算。

前端可传输入：

```text
target_principal_type // user | assistant，被操作主体类型
target_principal_id   // 被操作主体 ID
requested_identity    // owner | admin | member，创建或更新期望身份；删除时为空
```

后端派生 authContext：

```text
target_identity  // owner | admin | member，被操作人的当前身份；创建时为空
new_identity     // owner | admin | member，创建或更新后的身份；删除时为空
is_self          // 是否操作自己
is_last_owner    // 被操作 owner 是否为项目最后一个 owner
```

成员管理规则：

| 操作者身份 | 动作 | 规则 |
|---|---|---|
| owner | `project:member.create` | 可以创建 owner/admin/member |
| owner | `project:member.update` | 可以更新 owner/admin/member；但不能让项目失去最后一个 owner |
| owner | `project:member.delete` | 可以删除 owner/admin/member；但不能删除最后一个 owner |
| owner | `project:member.list` | 允许 |
| admin | `project:member.create` | 可以创建 admin/member；不能创建 owner |
| admin | `project:member.update` | 可以更新 admin/member；不能操作 owner；不能把任何人提升为 owner |
| admin | `project:member.delete` | 可以删除 admin/member；不能删除 owner |
| admin | `project:member.list` | 允许 |
| member | `project:member.create` | 拒绝 |
| member | `project:member.update` | 拒绝 |
| member | `project:member.delete` | 拒绝 |
| member | `project:member.leave` | 只允许删除自己的 member binding，表示退出项目 |
| member | `project:member.list` | 允许 |

补充规则：

- `is_last_owner=true` 时，任何身份都不能删除该 owner，也不能把该 owner 降级为非 owner。
- admin 永远不能 create/update/delete owner。
- 粗 action 只判断是否进入成员管理流程，最终是否允许必须由后端派生的 authContext 规则决定。
- 一期成员管理只支持 `project:member.*`。
- 一期不支持 `file:member.*`、`artifact:member.*`，也不通过接口创建 File / Artifact 直接成员 binding。

## 4. 权限判断

新增统一服务：

```text
backend/internal/service/permission_service.go
```

核心接口：

```text
Can(ctx, actor, action, resourceRef, requestInput) -> Decision
Explain(ctx, actor, action, resourceRef, requestInput) -> ExplainDecision
BatchCan(ctx, actor, actions, resourceRefs, requestInput) -> []Decision
```

判断顺序：

```text
1. 解析 caller。
2. 校验 caller.OrgID。
3. 根据 resourceRef 查询 resources。
4. 校验 leros_resources.org_id == caller.OrgID。
5. 查找当前资源上的直接 binding。
6. 沿 parent_resource_id 查找可继承的祖先 binding。
7. 汇总直接 binding 与继承 binding。
8. 一期对 file/artifact 使用身份强度取最大值：owner > admin > member。
9. 得到 effective_identity。
10. 如果 action 是项目成员管理动作，后端加载目标 binding、owner 数量和系统配置，派生 authContext。
11. 用 PermissionPolicy 按 target resource type + effective_identity 判断 action。
12. 返回 Decision{Allowed, Reason, Identity, ResourceID, MatchedBindingID, MatchedResourceID}。
```

伪代码：

```text
Can(actor, action, resource):
  assert actor.org_id == resource.org_id

  identities = []

  directBinding = FindBinding(resource.id, actor.principal)
  if directBinding exists:
      identities.append(directBinding.identity)

  current = resource
  while current != nil:
      current = LoadParent(current)
      binding = FindBinding(current.id, actor.principal)
      if binding exists:
          identities.append(binding.identity)

  if identities is empty:
      return deny("no_binding")

  effectiveIdentity = MaxIdentity(identities) // owner > admin > member
  authContext = BuildAuthContext(action, requestInput)
  return PolicyAllows(resource.type, effectiveIdentity, action, authContext)
```

身份合成规则：

- 一期 File / Artifact 不创建直接成员 binding，权限身份来自 Project 继承。
- 身份强度为 `owner > admin > member`。
- `effective_identity` 当前主要取自 Project 继承身份；保留 `max(inherited_identity, direct_identity)` 能力用于后续扩展。
- Project owner/admin 对一期 File / Artifact 始终保有对应控制权。
- 没有任何绑定时默认拒绝。

示例：

```text
Project -> Alice -> owner
FileA   -> 无直接 binding
```

当 Alice 访问 FileA 时，继承身份是 `owner`，最终 `effective_identity=owner`。继承得到的 identity 按 File 的资源类型解释，即使用 `file.owner` policy。

## 5. 创建与同步规则

创建业务对象时必须同步创建资源。

创建项目：

```text
projects.id = 1001
leros_resources(type='project', biz_id=1001, parent_resource_id=null, parent_resource_path_ids='{}')
leros_resource_bindings(resource_id=project_resource.id, principal=user:creator, identity='owner')
```

创建文件：

```text
leros_resources(type='file', biz_id=file.id, parent_resource_id=project_resource.id, parent_resource_path_ids='{project_resource.id}')
```

访问文件：

```text
Can(actor, "file:view", file_resource)
Can(actor, "file:download", file_resource)
```

创建产物：

```text
leros_resources(type='artifact', biz_id=artifact.id, parent_resource_id=project_resource.id, parent_resource_path_ids='{project_resource.id}')
```

访问产物：

```text
Can(actor, "artifact:view", artifact_resource)
Can(actor, "artifact:download", artifact_resource)
```

- Artifact 和 File 不建立父子关系。Artifact 的来源通过业务关系表表达，权限只看 Artifact 自己的 resource 继承链。

产物来源关系可以用业务表表达：

```sql
CREATE TABLE IF NOT EXISTS leros_artifact_sources (
    artifact_id INT8 NOT NULL,
    source_resource_id INT8 NOT NULL,
    source_type VARCHAR(50) NOT NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP
);
```

`ProjectMember` 迁移规则：

- 老数据中的项目 owner/admin/member 迁移为项目资源上的 `leros_resource_bindings`。
- `viewer` 如仍存在，统一迁移为 `member`。
- 迁移完成后，鉴权不再读取 `project_members`。
- `projects.owner_id` 仅保留为业务展示或历史兼容字段，不作为最终鉴权来源。

## 6. 接口与前端

建议接口：

```text
POST /CreateResourceBinding
POST /UpdateResourceBinding
POST /RemoveResourceBinding
POST /ListResourceBindings
POST /ExplainPermission
POST /BatchCheckPermission
```

权限解释请求：

```json
{
  "action": "artifact:view",
  "resource": {
    "type": "artifact",
    "biz_id": 3001
  }
}
```

权限解释响应：

```json
{
  "allowed": true,
  "reason": "identity_policy_allowed",
  "identity": "admin",
  "resource_id": 3,
  "matched_resource_id": 1,
  "matched_binding_id": 88,
  "inherited": true
}
```

前端展示：

- 当前用户在当前资源上的最终身份。
- 当前身份来自哪个资源，是否继承。
- 当前用户可执行动作。
- 权限不足时展示后端 reason 映射文案。
- 成员/协作者管理统一展示 `leros_resource_bindings`，不再按 Project、File、Artifact 分裂。

## 7. 建设范围与验收

后端必须交付：

- `leros_resources` 表、DAO 和资源树维护。
- `leros_resource_bindings` 表、DAO 和绑定管理。
- 代码态 `PermissionPolicy`。
- `PermissionService.Can(...)`、`Explain(...)`、`BatchCan(...)`。
- 项目创建时同步创建 project resource 和 owner binding。
- 文件创建时同步创建 file resource。
- 产物创建时同步创建 artifact resource。
- 至少完成 Project 资源的迁移兼容。
- 不再新增面向权限来源的 `ProjectMember` 能力。

前端必须交付：

- 资源协作者管理入口。
- 当前资源身份、继承来源和可执行动作展示。
- 权限不足展示后端 reason。

安全要求：

- 默认拒绝。
- 组织隔离优先于资源权限。
- 所有业务入口必须走 PermissionService。
- 不允许前端自行判断最终权限。
- 前端不得传可信权限 context；`target_identity`、`is_self`、`is_last_owner` 等必须由后端计算。
- 软删除资源和软删除 binding 不参与权限判断。
- 至少保留一个 project owner binding。
- admin 不能操作 owner；可以管理 admin/member。
- member 只能查看成员列表、自身权限或退出资源。
- 当前暂不建设审计日志。

验收标准：

- Project 可以作为根资源被授权。
- File 可以作为 Project 子资源继承 Project 权限。
- Artifact 可以作为 Project 子资源继承 Project 权限。
- 成员只在 Project 下维护，File / Artifact 不提供成员管理入口。
- 新增 action 不需要改数据库。
- user 和 assistant 都可以作为 principal 被授权。
- 非授权主体默认无法访问资源。
- 软删除资源和软删除 binding 不参与权限判断。

## 8. 扩展与结论

当前不建议：

- 再新增 `ProjectMember`、`KnowledgeMember`、`FilePermission` 这类业务专属权限表。
- 在数据库中保存每个主体的具体 action 列表。
- 让业务服务各自实现权限判断。
- 在第一阶段引入复杂外部权限引擎。
- 一期不接入 KnowledgeBase / Folder 的创建规则、接口和验收。

后续扩展只需要补充：

```text
业务代码中新增资源类型
PermissionPolicy identity/action 配置
业务对象创建时的 resource 同步逻辑
对应业务接口和验收标准
```

最终架构：

```text
用户请求
  |
认证中间件解析 Caller
  |
Service 层业务入口
  |
PermissionService.Can(...)
  |
  +--> Load Resource
  +--> Org Check
  +--> 沿资源树查找 ResourceBinding
  +--> 计算 effective identity
  +--> 查询代码态 PermissionPolicy
  +--> 得到 Decision
  |
允许或拒绝
  |
业务执行 / 返回稳定 reason
```

