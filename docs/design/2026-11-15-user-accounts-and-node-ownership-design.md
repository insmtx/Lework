# 用户体系与节点归属设计（扫码加入）

> 日期：2026-11-15 ｜ 状态：待确认 ｜ 范围：控制面（`lework serve` / 裸 `lework`）+ `lework agent`
>
> 相关文档：`docs/architecture/as-built.md`、`docs/operations/control-plane.md`、`docs/product/overview.md`

## 1. 目标

把控制面从「一个共享 enrollment token + 全局节点列表」变成**有账号、有归属**的控制面：

- 节点属于某个账号；账号只看到、只能调度自己的节点。
- 加入节点不需要手输长串凭证：`lework agent` 启动后在终端打印二维码 + 5 位短码，登录用户在网页/手机上确认，节点凭证经已建立的出站 WSS 自动下发并落盘。
- 归属关系落在一张关联表里，后续「把节点共享给另一个账号」只需插行，不改表结构。
- 保留手工与自动化的口子：网页出码手输、凭证文件预置、旧 enrollment token 直通。

## 2. 非目标 / 反目标

非目标：

- 不做组织、团队、RBAC、配额与计费；只有 `admin` / `user` 与节点级 `owner` / `member`。
- 首版不做会话级共享（只共享某一次会话链接），但数据模型不阻断它。
- 不做 SSO / OAuth / LDAP；首版是邮箱 + 密码 + 邀请码。
- 不把 Session / 消息 / diff / 权限复制进 PG，OpenCode 仍是唯一状态源。
- 不改 agent 出站 WSS 的拓扑；只在握手与帧类型上增加「待定连接」分支。

反目标：

- 用 `node_name` 当身份（现状），或让节点自己声明归属。
- 让 enrollment token 继续作为浏览器日常登录凭据，或让它出现在配对二维码里。
- 为了让 SSE / 推送「先跑通」而广播他人节点的事件。

## 3. 现状与必须解决的问题

| 现状 | 问题 |
|---|---|
| `store.UpsertNode` 按 `name` 查已有行并复用 ID，`nodes.name` 全局唯一 | 两台机器同名会合并；任何人把 `node_name` 填成别人的名字即可顶替该节点，接管其会话、文件与 PTY |
| `agentgw` 只校验全局 enrollment token，`hello` 里的名字决定身份 | 一个令牌通行所有节点；无法吊销单台机器 |
| `auth.go` 让回环请求直接放行，其余用同一个 token | 没有「我是谁」，只有「你有令牌吗」 |
| `/api/events` 按查询参数过滤，Hub 全量广播 | 多账号下会把他人节点的会话与 token 事件泄漏给所有登录用户 |
| `devices` / `preferences` / `project_preferences` 无用户维度 | 推送会发给所有人；偏好互相覆盖 |
| `pairing.Link` 把 enrollment token 塞进二维码 | 多账号下等于把管理员凭据发给扫码人 |

**结论：本设计的第一块不是登录页，而是节点自己的凭证。** 节点身份从「名字」改为「control plane 签发的凭证」，名字退化成纯展示字段。

## 4. 概念模型

```
User ──< UserSession
  │
  └──< NodeMember >── Node ──< NodeClaim（配对中，未成节点）
                        │
                        └──< Device / ProjectIndex / Preference（按用户维度收敛）
```

- **Node**：一台被认领的机器，`id` 由控制面分配，`credential_hash` 是它唯一的身份凭据。
- **NodeMember**：`(node_id, user_id, role)` 是归属的唯一真相。`nodes` 不存 `owner_id`，避免两处真相。
- **role**：`owner`（可删节点、可授权他人）/ `member`（可调度、可开会话）。首版只写 `owner`，字段先留着。
- 共享：往 `node_members` 插一行 `member`，不需要迁移；`revoke` 就是删行。

## 5. 数据模型（GORM，`backend/internal/controlplane/store`）

```go
type User struct {
    ID           string `gorm:"primaryKey"`            // uuid
    Email        string `gorm:"uniqueIndex;not null"` // 存储前 lower-case
    DisplayName  string
    PasswordHash string // argon2id，格式 argon2id$v=19$m=...,t=...,p=...$salt$hash
    Role         string // admin | user
    Status       string // active | disabled
    CreatedAt, UpdatedAt time.Time
}

type UserSession struct {
    ID        string `gorm:"primaryKey"`
    UserID    string `gorm:"index;not null"`
    TokenHash string `gorm:"uniqueIndex;not null"` // sha256(raw)，raw 只出现在 cookie
    UserAgent string
    ExpiresAt time.Time
    LastSeenAt time.Time
    CreatedAt time.Time
}

// Invite 是注册邀请码；控制面默认 invite-only。
type Invite struct {
    ID, CodeHash, CreatedBy string
    ExpiresAt time.Time
    UsedBy, UsedAt *...
}

type Node struct { // 新增字段，name 去掉唯一约束
    ID             string `gorm:"primaryKey"`
    Name           string `gorm:"index"` // 仅展示；唯一性按 owner 在应用层保证
    OS, Arch, AgentVersion string
    Capabilities, WorkspaceRoots []string `gorm:"serializer:json"`
    CredentialHash string `gorm:"index"`  // sha256(node credential)，空 = 未激活
    ClaimedAt      time.Time
    LastSeen, CreatedAt, UpdatedAt time.Time
}

type NodeMember struct {
    NodeID    string `gorm:"primaryKey;column:node_id"`
    UserID    string `gorm:"primaryKey;column:user_id;index:idx_node_member_user"`
    Role      string // owner | member
    GrantedBy string
    CreatedAt time.Time
}

// NodeClaim 是一次配对中未完成的连接；5 分钟 TTL。
type NodeClaim struct {
    ID         string `gorm:"primaryKey"`
    PairKey    string `gorm:"uniqueIndex"` // agent 生成并在重连时复用，保证码稳定
    Code       string `gorm:"index"`       // 5 位，字母表去掉 0/O/1/I/L
    NodeName, OS, Arch, AgentVersion string
    Status     string // pending | claimed | expired
    ClaimedBy  string // user_id
    NodeID     string // 认领时创建的节点
    InstanceID string // 持有待定连接的实例
    ExpiresAt  time.Time
    CreatedAt  time.Time
}
```

已有表加用户维度，尽量不碰主键：

- `devices.user_id` 可空：`NULL` 表示升级前的历史设备，下次配对/登录后重新绑定；推送只发给成员设备。
- `preferences` / `project_preferences` 用**键名前缀** `u/<user_id>/<原 key>` 做命名空间，不改主键、不改 DDL，SQLite 与 PG 行为一致（仓库里的 store 层自己拼前缀，调用方无感）。

## 6. 加入流程（扫码，主路径）

```text
工作电脑                                  控制面                         手机 / 浏览器
----------                                ------                         -------------
lework agent 启动
  └─ 无凭证 → WSS 连 /api/agent/connect
     hello{mode:"pair", pair_key, name, os}
                                         建 NodeClaim(pending, code=5 位)
                                         回 pair.pending{claim_id, code}
     终端打印二维码 + 短码
     （非 TTY 时只写 claim.json，不打印）
                                                                        登录 → 扫码/输码
                                                                        GET /api/claim/{code}
                                                                        显示节点名/OS/版本/时间
                                                                        点「加入我的账号」
                                         原子 UPDATE claim
                                         status=pending→claimed, claimed_by=user
                                         建 nodes + node_members(owner)（同一事务）
                                         发布 cp.pair.<claim_id>（只带 claim_id）
                                         持有连接的实例收到：
                                           生成节点凭证 → 写 credential_hash
                                           经本地 WS 回 pair.result{node_id, credential}
     落盘 ~/.config/Lework/node.credential (0600)
     断开待定连接 → 带凭证重连（普通节点路径）
                                         正常 welcome，节点上线
```

设计要点：

- **待定连接也必须先鉴权**：握手头用 `Authorization: Pair <pair_key>`，`pair_key` 是 agent 生成的 32 字节随机串，只用来标识这条待定连接、不是凭证（拿到它也只会创建/复用自己的待定连接，不能认领别人的节点）。因此 `/api/agent/connect` 不是公开端点，每 IP ≤3 / 全局 ≤100 的配额可以在升级**之前**按 `pair_key` 与来源 IP 计数。
- **凭证在握手头里**：正常路径沿用 `Authorization: Bearer <node credential>`；未知或已吊销凭证返回 401（不区分原因，避免探测）。
- **凭证下发复用 welcome**：旧 token 直通路径在登记时若节点还没有 `credential_hash`，`welcome` 里带上一次性 `credential` 字段，agent 落盘后立即改用它——不需要新增服务端主动帧，也不强迫老部署重新配对。
- **凭证被拒时不自动重配对**：agent 只打印「凭证失效/节点已被移除，请重新执行 `lework agent pair`」并保持重试；自动清凭证重配对会让一台机器静默换到别人的账号下。
- **名字冲突自动加后缀**：认领时若同一 owner 名下已有同名节点，落库为 `work-mac-2` 而不是拒绝（重装系统会自然产生新节点，旧节点留给用户自己删）。

- **凭证不经过 NATS、不进日志、不进错误文本**。跨实例只传 `claim_id`，凭证由持有 WS 的实例本地生成并通过这条 WS 下发。
- **码的长度**：默认 5 位（31 字符字母表 ≈ 25 bit），TTL 5 分钟；4 位（≈ 20 bit）可配置但要求更短 TTL 与更严限流。抗爆破靠「一次性 + 短 TTL + 每 IP/每账号限流」，不靠码长。
- **原子认领**：`UPDATE ... WHERE id=? AND status='pending' AND expires_at>now()`，`RowsAffected==1` 才继续；两个账号同时认领只有一个成功。
- **幂等重投**：`claimed` 但 `credential_hash` 为空（实例在生成凭证前挂掉）时，agent 用同一 `pair_key` 重连，owner 实例重新生成并下发。
- **确认页必须显示机器信息**：节点名、OS/arch、agent 版本、首次出现时间。扫码即授权，页面不能让「A 机器」被误认成「B 机器」。
- **残留风险**：恶意 agent 可以伪装成一台机器诱使你认领，认领后它成为你账号里的节点。确认页的机器信息 + 本人需要在终端前看到码，是当前的缓解手段；后续可加「节点首次上线需在终端确认指纹」。

## 7. 其他加入方式（口子）

| 方式 | 用途 | 说明 |
|---|---|---|
| `lework agent --code ABC12` | 机器不在你面前、无法扫码 | 网页「添加节点」出 5 位码（TTL 10 分钟，绑当前账号），agent 直接出示，无需确认步骤 |
| `lework agent claim` | 二维码被日志刷走、终端不可见 | 读 `claim.json` 重印二维码与短码；要求 agent 仍在待定连接中 |
| `--credential-file` / `LEWORK_NODE_CREDENTIAL` | 自动化、容器、Ansible | 凭证由 `POST /api/nodes`（owner 调用，返回一次性的节点凭证）或等价的 admin 命令签发，agent 只读文件（0600）；文件内容与扫码得到的凭证同构 |
| `enrollment_token`（保留） | 单机自托管、旧版本 agent、本机管理员 | 视为 **admin 直通**：可注册节点、可看全部节点、可认领历史节点；不进浏览器 cookie，不出现在二维码里 |

`lework agent` 不带任何参数且本地无凭证时进入配对态并打印二维码；`--no-qr` 只打印链接与短码。

## 8. 认证与会话

- **浏览器/手机**：`POST /api/auth/login {email, password}` → 校验 argon2id → 生成 32 字节随机会话 token → `Set-Cookie: lework_session=<raw>; HttpOnly; SameSite=Strict; Secure(按 TLS)`，库里只存 sha256。移动端用同一条 token 走 `Authorization: Bearer`。
- **首个账号**：库里没有任何 `User` 时，登录页提供「用控制面令牌初始化」入口，输入 `lework serve token` 打印的 enrollment token，创建第一个 `admin`。
- **之后注册**：`registration.mode`（默认 `invite`）——`invite` 需要邀请码、`open` 允许自助注册、`closed` 只能由 admin 建号。
- **本机回环**：保留直通，但只在「没有 User 表内容」或 `local_admin` 开启时生效；语义是「本机管理员」，不是任何账号。日常使用走登录，避免「本机浏览器永远绕过授权」。
- **控制面自己的启动二维码**：不再打印。账号体系下手机用账号登录即可，启动时二维码里的权限凭据属于历史包袱；`lework serve token` 只用于「初始化第一个账号」。
- 会话过期：`expires_at` 30 天，滑动续期；`DELETE /api/auth/session` 登出；改密/禁用即删该用户全部会话。

## 9. 授权规则

统一在中间件里解析出 `principal`（`user` / `local_admin`），再按路由分组校验：

| 路由 | 校验 |
|---|---|
| `GET /api/nodes` | 返回 `node_members` 里属于我的节点；`admin` 追加「未认领」节点（无成员行的历史节点），但**永远看不到别人已认领的节点**；`local_admin` 见下 |
| `/api/nodes/{id}/claim`（新增） | 把「未认领」节点认领到当前账号；仅 `admin`（或 `local_admin`）可调用 |
| `/api/nodes/{id}/...`、`/api/sessions/{nodeID}/...`、`/api/pty/{nodeID}/...` | 从 path 取 node id → membership 校验；非成员一律 **404**（不暴露存在性） |
| `POST /api/sessions` | body 里的 node 同样校验 |
| `/api/events` | 订阅时取当前成员的 node 集合，按 `event.NodeID` 过滤；成员变更 30 秒内重算（或订阅 `node.member.*` 事件立即重算） |
| `/api/overview`、`/api/projects` | 只投影我有权限的节点；项目索引按成员过滤 |
| `/api/devices` | 设备绑定 `user_id`；推送只发给该节点成员的设备 |
| `/api/nodes/{id}/members`（新增） | 仅 `owner` 可增删；首版只有 owner 一行 |

`agentgw` 侧：凭证 → 节点身份（查 `credential_hash`），`hello` 里的 `node_name` 只写入展示字段，绝不参与身份判定。旧 token 直通仍按名字复用节点，但**只匹配没有成员行、也没有凭证的历史节点**，永远不会覆盖某个账号已认领的节点。

## 10. 多实例与并发

沿用 `docs/design/2026-11-14-controlplane-nats-pg-design.md` 的基座：

- `node_claims`、`users`、`user_sessions`、`node_members` 全在 PG，任意实例可读写。
- 认领通知走 Core NATS subject `cp.pair.<claim_id>`，只带 claim id；由持有待定连接的实例生成本地凭证并下发。
- 待定连接有配额：每 IP 并发 ≤ 3、全局 ≤ 100，超时 60 秒无帧即断；`code` 尝试每 IP 10 次/分钟、每 claim 20 次后作废。
- 节点上线仍按现有 `node.connected` / `node.offline` 与 PG `last_seen` 判定，不因配对逻辑改变。

## 11. 代码改动（按包）

| 位置 | 改动 |
|---|---|
| `internal/controlplane/store` | 新表 `users` / `user_sessions` / `invites` / `node_members` / `node_claims`；`nodes` 加 `credential_hash`、`claimed_at`，去掉 name 唯一索引；`ListNodesForUser` / `NodeRole` / `ClaimNode` / `LookupNodeByCredential`；`devices` / `project_preferences` 加 `user_id` |
| `internal/controlplane/auth.go` | 拆成 `principal` 解析（cookie / bearer / local_admin）+ 路由级授权；保留 token 常量时间比较路径 |
| `internal/controlplane/authz`（新增） | `RequireUser`、`RequireNode(role)`、`visibleNodes(user)`、`filterEvents(user)`；单测覆盖 404 与成员变更 |
| `internal/controlplane/api/auth.go`（新增） | `POST /api/auth/login`、`POST /api/auth/logout`、`GET /api/me`、`POST /api/auth/bootstrap`、`POST /api/auth/register` |
| `internal/controlplane/api/claims.go`（新增） | `GET /api/claim/{code}`、`POST /api/claim/{code}`、`POST /api/claims`（网页出码）、`GET /api/nodes/{id}/members`、`POST/DELETE /api/nodes/{id}/members` |
| `internal/controlplane/agentgw` | 握手分支：`mode=pair` 的待定连接；凭证校验（sha256 查 `credential_hash`）+ 旧 token 直通；`cp.pair.<id>` 响应端；`hello` 名字只做展示 |
| `internal/controlplane/api/events.go` | 按可见节点集合过滤；resync 时重算 |
| `internal/controlplane/push` | `store.ListDevices` → `ListDevicesForNode(nodeID)` |
| `internal/controlplane/pairing` + `cmd/lework/pairing.go` | 移除启动时的手机配对二维码与 `pairing.Link` 的 token 参数；`lework serve token` 仅保留「初始化第一个账号」用途 |
| `internal/controlplane/config` | `registration.mode`、`session_ttl`、`claim_ttl`、`claim_code_length`、`local_admin` |
| `internal/agentd/config` + `runner` | 凭证文件读写（0600）、`mode=pair` 握手、`pair.result` 落盘、`claim.json` |
| `cmd/lework/agent.go` | 复用 `qrterminal` 打印配对二维码（参照 `pairing.go` 的 `isTerminal` 保护）；`--code`、`--no-qr`、`claim` 子命令 |
| `frontend/apps/control-plane` | 登录/注册页、节点列表分组「我的 / 共享给我」、添加节点弹窗（二维码 + 命令 + 倒计时）、节点成员管理 |
| `docs/` + `AGENTS.md` | 产品定位从「单用户多节点」改为「多账号、节点有归属」；`operations/control-plane.md` 补加入与登录流程 |

## 12. 迁移与兼容

- **旧部署升级**：enrollment token 继续可用（admin 直通）；已有节点没有 `node_members` 行 → 在管理员视图里显示为「未认领」，提供一键认领到当前账号；旧 token 路径在 `welcome` 里补发凭证，认领后 agent 自动改用凭证。
- **旧 agent**：仍用全局 token 注册，节点同样落在「未认领」；升级后按第 6 节扫码或 `--code` 认领，凭证随之写回。
- **DDL**：只有 `nodes.name` 的唯一索引必须显式 `DropIndex`（GORM AutoMigrate 不会删），SQLite（单测）与 PG 两种方言都要覆盖；其余新增列都是可空/带默认值，AutoMigrate 可直接处理；偏好类表不动主键。
- **数据回填**：`devices.user_id` 保持 `NULL`，等设备下次登录/配对时绑定；偏好在前缀化后**不迁移**旧值（旧键保留不读，等于重置显示偏好），避免给不存在的用户造 ownership。
- **不兼容点**：`lework agent` 首次启动不再需要 `enrollment_token` 才能工作；`agent.yaml` 里的该字段仍被读取，但只在「admin 直通」路径使用；控制面启动不再打印手机配对二维码。

## 13. 测试计划

单测：

- 认领：并发双认领只有一个成功；过期码拒绝；`claimed` 但未下发凭证时可重投；码尝试超限作废。
- 授权：非成员访问节点/会话/PTY/文件一律 404；成员被移除后立即失效。
- 凭证：错误凭证 401；凭证 sha256 命中；旧 token 直通仅对 admin 生效；旧 token 注册在 `welcome` 里拿到凭证后，第二次连接不再需要 token。
- 名字：同名重装产生 `-2` 后缀；用户已认领的节点不会被 `hello` 里的同名覆盖。
- SSE：只收到可见节点事件；成员被移除后不再收到该节点事件。
- 登录：argon2id 校验、错误密码不区分「用户不存在」、会话过期、登出即失效。

集成（`-tags integration`，`LEWORK_TEST_*`）：

- 双实例：agent 连 A、浏览器连 B 完成扫码认领（`cp.pair.<id>` 路径）。
- 双实例：认领后节点事件只出现在成员实例的 SSE 里。
- 回归：现有 `go test -p 1 ./...` 全绿；`/api/agent/connect` 旧 token 路径不变。

安全用例（必须有）：

- 日志/错误/事件流里不出现 enrollment token、节点凭证、claim 码。
- `/api/claim/{code}` 未登录 401；登录后不返回 claim 所属账号信息以外的内容。
- 非成员枚举 `/api/nodes/{uuid}` 得到 404 而不是 403。

## 14. 交付顺序

1. **M1 身份**：users / user_sessions / invites + 登录、`/api/me`、首个账号初始化、注册策略。此阶段不改 agent。
2. **M2 归属**：node_members、节点凭证、`agentgw` 身份改造、全部路由授权、SSE 与推送按用户过滤、迁移与「未认领节点」。
3. **M3 扫码加入**：NodeClaim、待定连接、`cp.pair`、agent 侧二维码与凭证落盘、`--code` / `claim` / 凭证文件。
4. **M4 前端与共享**：登录页、节点分组、添加节点弹窗、成员管理（grant/revoke）、文档更新。

后续（不在本次）：会话级共享链接（`session_shares(node_id, session_id, user_id)`，session id 由 OpenCode 提供，控制面只存 ACL）、节点指纹二次确认、OAuth 登录。

## 15. 本设计已定的取舍（可推翻）

| 取舍 | 选择 | 理由 |
|---|---|---|
| 节点归属落点 | 只放 `node_members`，`nodes` 不存 `owner_id` | 一处真相；共享即插行 |
| 凭证生成方 | 控制面生成，经 WS 下发 | 吊销、轮换都在服务端；凭证不经过 NATS |
| 加入主路径 | agent 出码、账号侧确认 | 机器上不需要登录，也不用抄长串 |
| 码长度 | 默认 5 位，可配 4 位 | 靠 TTL + 限流抗爆破 |
| 注册策略 | 默认 invite-only，首个账号用控制面令牌初始化 | 「各自管各自的」不等于对公网开放注册 |
| 手机端 | 长期会话（device 维度）；控制面启动二维码取消，手机用账号登录 | 避免每次用都重新配对 |
| admin 可见范围 | 自己的节点 + 「未认领」历史节点，看不到别人已认领的节点 | 既能迁移旧部署，又不把「各自管各自的」变成互相可见 |
| 凭证失效 | agent 不自动重配对，只报错并等待显式 `pair` | 防止机器静默落进另一个账号 |
| 共享粒度 | 首版只做节点级 | 会话级共享需要额外 ACL 表与事件过滤 |
