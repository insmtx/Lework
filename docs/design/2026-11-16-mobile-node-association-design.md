# 移动端「我的电脑」与账号体系设计

> 日期：2026-11-16 ｜ 状态：待确认 ｜ 范围：`backend/internal/controlplane`（账号/节点接口）+ `backend/cmd/lework`（`--reset`）+ `frontend/apps/mobile` + `frontend/packages/api`
>
> 相关文档：`docs/design/2026-11-15-user-accounts-and-node-ownership-design.md`（账号与节点归属的权威设计）、`docs/operations/control-plane.md`
>
> 前序事实：账号/归属/扫码配对已在 PR #470 合入 main。本设计**不改**归属模型，只改「账号怎么来」「手机怎么用」「机器怎么移除」。

## 1. 背景

账号体系上线后，节点归属已经成立：`/api/nodes` 只返回当前账号可见的节点，agent 通过 `Authorization: Pair <pair_key>` 握手 + 5 位认领码加入。

但两侧都还缺一块：

| 现状 | 问题 |
|---|---|
| 账号只能靠 `POST /api/auth/bootstrap` + 手抄 enrollment token + curl 创建 | 没有可用的初始化路径；网页端也没有任何登录/初始化界面；默认注册模式是 `invite` 而邀请码没有任何发放入口 → 新部署实际无法建号 |
| 手机 `Settings` 只有 `baseUrl` + `token` | 没有账号概念；`token` 是部署级 admin 直通，不该长期放在个人手机上 |
| `app/connect.tsx` + `src/pairing/link.ts` 解析 `url` + `token` 深链 | 那是 `lework serve` 启动二维码的内容，而该二维码已按设计删除 → **手机端当前没有任何可用接入路径** |
| `app/nodes.tsx` 只是排序列出 | 没有「添加电脑」，0 台时没有引导 |
| 无移除能力（前端与后端都没有） | 关联是单向门：机器卖了/丢了也摘不掉；agent 侧「请重新配对」没有配套动作 |

## 2. 目标

1. **开箱可用**：控制面首次启动自动创建管理员 `admin` / `admin123`，并让**运行 `lework` 的这台机器**（回环请求）直接以该账号身份使用，不需要登录、不需要 curl。
2. 手机以账号身份使用控制面：登录、注册（邮箱 + 密码，无邀请码）、登出、改密码。
3. 「我的电脑」能添加机器：扫码或输入 5 位码，先预览再确认。
4. 「我的电脑」能移除机器，并把该机器贡献的项目、主节点偏好、「我的目录」偏好一起清干净。
5. 移除后，那台机器用 `lework agent --reset` 重新进入配对，不需要手删文件。

## 3. 非目标 / 反目标

非目标：

- 邀请码机制（用户明确不要）：配置里的 `invite` 模式、`invite_code` 字段、`invites` 表与消费逻辑一并移除。
- 节点共享（`role=member` 的邀请/界面）、组织、RBAC：数据模型保留，界面留到后续。
- 手机端「生成码让别人输」、手机端认领「未归属的历史节点」。
- 节点改名、手机上的 OpenCode 启停/文件浏览。
- 会话级共享、SSO。

反目标：

- 让「服务器地址 + enrollment token」继续当个人日常凭据：它只保留为脚本 / 远程运维的 admin Bearer。
- 把 session token 写进日志、事件、URL 或错误文本。
- agent 收到 401 自动清凭证重配（等于服务端一句话就能让机器换账号）。
- 移除只删归属行，留下项目卡片、主节点偏好、目录偏好等幽灵数据。
- 为了「开箱可用」把默认密码藏起来：默认值必须出现在启动日志与文档里，并给出改密码的路径。

## 4. 业务逻辑

### 4.1 引导与「本机即管理员」

**自动引导**：控制面启动时若 `users` 表为空，创建账号 `admin` / `admin123`（role `admin`、status `active`，argon2id 落库），并打印一条警告：默认账号与密码、以及「请尽快修改」。已有用户时什么都不做（不覆盖、不重置）。

- `users` 唯一键是 email，而 `validateNewAccount` 只要求非空 + 密码长度 → `admin` 可以不带 `@` 直接作为登录标识。登录表单的「账号」字段因此按普通字符串处理，不做邮箱格式校验。
- 删除 `POST /api/auth/bootstrap`：自动引导之后它只会返回 409，留着就是一条骗人的文档路径。enrollment token 仍保留为 admin Bearer（脚本/远程运维）。
- 现有部署（已有用户）不受影响；老文档里 curl 初始化的写法改为「启动即得 admin/admin123」。

**本机即管理员**：`authz.Resolver` 的 `LocalAdmin` 分支不再返回部署级 `KindLocal`，而是解析成 **bootstrap 管理员账号** 的 `Principal{Kind: user, UserID: <admin 的 id>, Role: admin}`。本机浏览器、本机扫码、本机 curl 因此都是「admin 这个账号」，`/api/nodes`、偏好、认领、移除全部按账号语义工作（不再需要 `local_admin` 与 `admin_token` 的特判分支）。

- 回环判定沿用现状（出现任一转发头即按外部请求处理）。
- 找不到 bootstrap 账号时回落到 `KindLocal`（部署者手动删了 admin 也不至于把本机锁死）。
- 同一个 `admin` 也能在手机上登录（同一个账号、密码），因此「本机的节点」与「手机看到的节点」是同一份归属。

**改密码**：新增 `POST /api/auth/password {current_password, new_password}`（登录态，校验旧密码，成功后吊销除当前会话外的会话；至少 8 位）。手机「账号」页提供入口。这是默认密码可接受的前提。

### 4.2 注册 / 登录 / 登出（手机）

**存储**：`Settings` 改为 `{baseUrl, email, sessionToken, notifications, deviceID}`；`sessionToken` 与 `deviceID` 存 SecureStore（沿用 `src/settings/secure-store.ts`）。旧版本只有 `baseUrl + token`：加载时丢弃 `token`、保留 `baseUrl`，进入「未登录但地址已填」状态。

**登录**：`POST /api/auth/login {email, password}` → 200 返回会话 token（浏览器同时收到 cookie，手机不用）。手机保存 token 与账号标识，写入 `Settings` 后刷新应用外壳。

**注册**：`POST /api/auth/register {email, password}` → 后端直接下发会话（注册即登录）。表单只有邮箱 + 密码。注册策略只有 `open`（默认）与 `closed` 两种：`open` 允许任何人不带邀请码注册，`closed` 直接 403 并让手机隐藏注册入口。

**登出**：`DELETE /api/auth/session`，只吊销当前设备这条会话；本地清 `sessionToken` 与账号，保留 `baseUrl`。

**过期**：任何请求 401 → 清 `sessionToken`、跳登录页，提示「登录已过期，请重新登录」。

**换服务器**：「服务器设置」保存新 `baseUrl` 时同时清空登录态并跳登录页。校验方式从「用 token 调 `/api/nodes`」改为「无鉴权调 `GET /api/auth/methods`」。

**注册限流**（默认开放注册的补偿）：同来源 IP 10 次/分钟，超限 429 + `Retry-After: 60`；复用 `claimAttemptLimiter` 实现，键为 RemoteAddr 主机（不信任 `X-Forwarded-For`）。

### 4.3 添加电脑（扫码 / 输码 → 预览 → 确认）

两个入口共用同一个确认页，最终都调用：

1. **预览** `GET /api/claim/{code}` → `{code, node_name, os, arch, agent_version, expires_at}`
2. **确认** `POST /api/claim/{code}` → 成功返回节点视图，归属已写入，agent 的待定连接收到 `pair.result` 并落盘凭证。

UI 顺序固定「先看后确认」：预览页展示节点名、OS/arch、agent 版本、剩余有效时间，以及「确认后这台电脑会加入你的账号」。**预览前不产生任何写入**。

**入口形态**：

- 扫码：复用 `src/pairing/scanner.tsx`（相机权限、每帧去重、Expo 误扫提示保留），解析目标改为 `lework://node/pair?url=<http(s)://host/prefix>&code=<5 位>`。`url` 与当前 `baseUrl` 不同时先提示「这个码属于另一个控制面 <url>」，用户确认后才切换（切换会退出当前登录）。
- 输码：5 位字母数字，去空白、统一大写，输满自动预览。
- 相机权限被拒：不阻塞，直接给「改为输入 5 位码」。

**失败语义**：

| 场景 | 后端 | 手机文案 |
|---|---|---|
| 码不存在 / 已过期 / 已被别人认领 / agent 已断线 | 404 `claim_not_found`（不可区分） | 「码无效或已过期，请让那台机器重新出码」 |
| 猜码超预算 | 429 + `Retry-After` | 「尝试次数过多，请稍后再试」 |
| 未登录 / 会话过期 | 401 | 回登录页 |
| 网络失败 | — | 「网络不通，请检查服务器地址」+ 重试 |

**「agent 已断线」必须真的成立**（当前不成立）：`store.ClaimByCode` 只过滤 `status=pending AND expires_at > now`，agent 断开后码仍然可用，用户确认会得到一个永远离线的节点。本次补上：agent 的待定 WSS 连接断开时，把它对应的 pending claim 一并标记为过期（网关断开路径调用 `ExpirePendingClaim(pairKey)`），不依赖客户端猜 `last_seen` 是否为零值。

**成功后**：提示「已加入 <节点名>」，返回「我的电脑」并刷新；若凭证尚未落盘，卡片显示「离线 · 等待上线」，收到 SSE 生命周期事件或下拉刷新时更新。

### 4.4 「我的电脑」列表

- 数据仍是 `GET /api/nodes`（已按成员过滤）。排序沿用 `src/nodes/order.ts`（在线优先）。
- 顶部「添加」→ 「扫码 / 输入 5 位码」两个入口。
- 卡片沿用现状（名称、在线/离线、OS/arch + agent 版本、最后在线、工作目录）；不展示未归属节点（手机不处理认领历史节点）；每行「⋯」→「移除此电脑」。
- 空状态即主入口：「在那台机器上安装并运行 `lework agent`，终端会给出二维码和 5 位码」，下方放「扫码添加」「输入码」。
- 刷新：下拉刷新保留；订阅 `GET /api/events` 的节点生命周期（`node.connected` / `node.offline` / `node.member.*`）触发静默刷新（Bearer 带头，不用 URL 查询串）。

### 4.5 移除电脑（用户要求：同时清理相关项目）

**权限**：只有该节点的 `owner` 能移除。`nodeRoute` 只校验「是成员」，handler 内再查 `store.NodeRole(nodeID, userID) == owner`；`member` 与 `admin_token` 一律 403。

**新接口**：`DELETE /api/nodes/{id}` → 204。一个事务内完成：

| 表 | 动作 | 说明 |
|---|---|---|
| `node_members` | 删除该节点全部归属行 | 单 owner 现状下就是一行；共享场景随节点一起消失 |
| `nodes` | 删除该行 | 凭证哈希随之消失，agent 下次重连 401 |
| `node_claims` | 删除 `node_id` 指向它的记录 | 待定连接一旦被引用就无意义 |
| `project_index` | 删除 `node_id` 的全部行 | 只由这台机器贡献的项目卡片整张消失；多节点共有项目保留 |
| `project_preferences`（该用户命名空间） | `primary_node_id = ?` 清空，**保留 alias 行** | 不自动改选另一台（会伪造一次「人工选择」） |
| `preferences` | 删除 `u/<userID>/inbox.dir.<nodeID>` | 手机「我的目录」偏好 |
| `devices` | 不动 | 推送按「用户在事件节点的成员关系」过滤，节点没了自然收不到 |

**即时断开**：若该节点连在本实例，`registry.Connection(nodeID).Close()` 让 agent 立刻掉线；其他实例通过 Core NATS 的 `cp.node.<id>.close` 控制消息尽力关闭。即使消息丢失，凭证已作废，重连也进不来——「尽力即时、必然失效」。

**事件**：发布 `node.member.revoked`（与 `node.member.granted` 对称）+ 常规 `node.offline`，SSE 订阅者立即重算可见集合。

**手机端**：确认弹窗写清代价——「移除后这台电脑会立即断开；机器上的 agent 需要执行 `lework agent --reset` 才能重新配对；这台电脑的项目会从列表消失」。成功后从列表移除；若正停在该节点的会话里，退回「我的电脑」并提示。

**会话数据**：控制面不存会话与消息；机器上的 OpenCode 数据不动，重新配对后项目与会话由 agent 重新上报。

### 4.6 移除之后：`lework agent --reset`

现在 agent 拿到 401 只会打印「凭证被拒/节点已被移除，请重新配对」并无限重试，**不会**自动清凭证（刻意如此）。

`lework agent --reset`：删除本地 `node.credential` 与 `node.claim`，然后**照常启动**——没有凭证即进入配对态，打印二维码与短码。允许与 `--code` 组合：`lework agent --reset --code ABC12`。

两条必须处理的例外（现有行为会让「reset 后能配对」变成假话）：

- **配置了 enrollment token 时**：`planPairing` 在「没有凭证 + 有 token」时走旧直通、登记为**未认领**节点，手机根本看不到。→ `--reset` 这一轮**忽略 enrollment token**（只影响本次运行），强制进配对。
- **凭证来自 `LEWORK_NODE_CREDENTIAL` 时**：环境变量删不掉。→ 直接报错「请先 unset LEWORK_NODE_CREDENTIAL」，不做半吊子重置。

其他要点：`--reset` 只清本地文件，不做远端写入；不杀已有进程（同 OS 用户下若已有 agent 在跑，需先停，否则争 OpenCode service 端口），文档写明。**不做** `lework agent pair` 子命令：与 `--reset` 等价，两个入口只会让文档分叉。`lework agent claim`（重印当前待定连接的码）保持不变。

## 5. 接口清单

| 接口 | 状态 | 说明 |
|---|---|---|
| `GET /api/auth/methods` | **新增（公开）** | `{initialized: bool, registration_mode: "open"\|"closed"}`；只暴露这两项，不暴露账号、节点数、版本 |
| `POST /api/auth/login` | 复用 | 返回会话 token |
| `POST /api/auth/register` | 改造 | 去掉 `invite_code`；`open` 直接建号并发会话，`closed` 403；加 10 次/分钟/IP 限流 |
| `POST /api/auth/password` | **新增** | 登录态改密码：校验旧密码，成功后吊销其他会话 |
| `POST /api/auth/bootstrap` | **删除** | 自动引导取代它 |
| `DELETE /api/auth/session` | 复用 | 只吊销当前会话 |
| `GET /api/me` | 复用 | 手机启动时校验登录态 |
| `GET /api/nodes` | 复用 | 已按成员过滤 |
| `GET /api/claim/{code}` | 复用 | 预览 |
| `POST /api/claim/{code}` | 复用 | 确认（agent 断线时现在会 404） |
| `DELETE /api/nodes/{id}` | **新增** | owner only；级联见 §4.5 |
| `GET /api/events` | 复用 | 节点生命周期触发刷新 |

## 6. 移动端结构（改动点）

| 文件 | 变更 |
|---|---|
| `src/settings/settings.ts` | `Settings` 改为 `{baseUrl, email, sessionToken, ...}`；旧数据迁移（丢 token 留 baseUrl） |
| `src/settings/session.ts`（新增） | 登录/注册/登出/改密码/401 处理的纯逻辑，便于单测 |
| `app/login.tsx`（新增） | 登录 ↔ 注册切换、错误文案映射、`registration_mode=closed` 时隐藏注册 |
| `app/settings/index.tsx` | 顶部新增「账号」行（未登录「登录 / 注册」，已登录显示邮箱） |
| `app/settings/account.tsx`（新增） | 邮箱、改密码、登出 |
| `app/settings/server.tsx` | 保存地址改用 `GET /api/auth/methods` 校验；保存后清登录态并跳登录页 |
| `app/nodes.tsx` | 顶部「添加」、每行「⋯ → 移除此电脑」、空状态引导、确认弹窗、SSE 静默刷新 |
| `app/nodes/add.tsx`（新增） | 扫码 / 输码 + 预览确认（一个页面两种状态） |
| `src/pairing/link.ts` | 解析 `lework://node/pair?url&code`；保留「扫到 Expo 二维码」提示 |
| `src/pairing/scanner.tsx` | 回调从 `{baseUrl, token}` 改为 `{baseUrl, code}` |
| `app/connect.tsx` | 重写为「深链带 code → 预览页」（未登录先登录再回跳） |
| `packages/api/src/client.ts` | 新增 `getAuthMethods/login/register/logout/changePassword/me/claimPreview/claimConfirm/deleteNode` |
| `packages/api/src/types.ts` | 新增 `AuthMethods`、`ClaimPreview` |

**状态管理**：沿用 `src/ui/app-shell-context.tsx`（`settings` + `reloadSettings`）。`settings.sessionToken` 是登录态唯一真相，401 处理后写回 SecureStore 再 `reloadSettings()`；不新增第二份全局 store。

## 7. 安全与隐私

- 默认账号 `admin/admin123` 是**有意为之的开箱配置**：启动日志与运维文档明写，并提供改密码入口；绑定非回环地址时启动警告里追加「默认密码，请先修改」。公网部署必须改密码。
- 会话 token 只存 SecureStore，不进日志/URL/SSE 查询串。
- 认领码与配对 key 都是短生命周期凭据：预览与确认共享每账号 10 次/分钟预算；确认成功后预算归还一格。
- 移除只允许 owner；`member` 与 `admin_token` 403。
- 注册限流按来源 IP（不信任转发头）。
- 扫码识别到属于另一个控制面的码时必须显式确认才切换。
- 本机回环映射到 admin 账号：语义与改造前的 `local_admin` 一致（本机 = 部署者），但从此有账号、有审计主体。

## 8. 可测试性

后端：

- 自动引导：空库启动后存在 `admin` 且 role=admin、可用 `admin123` 登录；已有用户时不重复创建、不覆盖密码；`POST /api/auth/bootstrap` 已不存在（404）。
- 本机直通：回环请求解析为 `Kind=user` 且 UserID = admin（能调 `/api/nodes/{id}/claim` 这类要求账号的接口）；带 `X-Forwarded-For` 时退回外部语义；admin 被删后回落 `KindLocal`。
- 注册：`open` 下无邀请码可注册并直接拿到会话；`closed` 403；第 11 次/分钟 429 + `Retry-After`；不同 IP 不受影响。
- 改密码：旧密码错 401、成功后可改后登录、其他设备会话被吊销、当前会话仍有效、短于 8 位 400。
- 移除：owner 204 且级联删净（node_members / nodes / project_index / claim / inbox 偏好；project_preferences 主节点清空但 alias 保留）；member 与 admin_token 403；不存在或重复删除 404；删除后 `/api/nodes` 与 `/api/projects` 不再出现只属于它的项目；在线节点连接被关闭。
- 认领：agent 待定连接断开后，同一 5 位码的预览与确认都返回 404。
- 清理：`invites` 表与 `invite` 模式移除后配置校验报错信息明确；迁移把旧表登记进 `legacyTables` 并在启动时删除。

移动端（jest）：

- `link.ts`：解析 `lework://node/pair?url&code`；缺 code/缺 url 报错；`exps://` 专属提示。
- `session.ts`：登录写 settings；401 清 token；登出清 token 留 baseUrl；换地址清登录态；改密码错误映射。
- 码输入规范化：去空白、大写、长度不足不请求。
- 错误映射：404 / 429 / 401 / 网络 → 四类文案。

手工验收（本机）：

1. 空库启动 → 日志出现 `admin/admin123` 警告 → 本机浏览器不登录即可看到节点列表（admin 身份）→ 手机用 admin/admin123 登录看到同一批节点。
2. 机器 A 跑 `lework agent` → 手机扫码 → 预览 → 确认 → 列表出现 A（在线）→ 在 A 上发任务成功。
3. 机器 B → 输码路径 → 同样成功；B 的 agent 中途 Ctrl-C → 同一码预览/确认均提示「码无效或已过期」。
4. 手机移除 A → A 立刻掉线；手机列表与项目列表里 A 的痕迹消失；A 上 `lework agent --reset` → 重新扫码加回 → 历史项目仍在。
5. 手机注册一个新账号（邮箱+密码）→ 自动登录 → 节点列表为空 → 扫码加入一台机器 → 只能看到自己的节点。
6. 改密码后用新密码登录；旧密码失效。

## 9. 兼容 / 迁移

- 旧手机设置（`baseUrl + token`）：保留地址、丢弃 token，进登录页；不尝试用 token 换会话。
- 已有部署（`users` 非空）：不自动创建 admin；原 bootstrap 创建的账号继续可用，只是 `POST /api/auth/bootstrap` 路径被删除。
- 配置：`registration_mode` 只接受 `open` / `closed`；写 `invite` 直接启动失败并提示迁移。
- 表：`invites` 删除 → 按 AGENTS.md 在 `backend/internal/infra/db/database.go` 的 `legacyTables` 注册，启动时清理；`Invite` 模型、`store/invites.go` 与其测试一并删除。`nodes` / `node_members` / `project_index` / `project_preferences` / `preferences` 结构不变。
- 文档：`docs/operations/control-plane.md` 的初始化章节改为「启动即 admin/admin123 + 改密码」，并标注公网部署要求。

## 10. 待办与遗留

- 节点共享（member 邀请/移除、「退出共享」界面）。
- 手机端出码（`POST /api/node-claims`）与出码后的等待态。
- 节点改名、手机上的 OpenCode 启停/文件浏览。
- 多实例下 `cp.node.<id>.close` 的可靠性（尽力而为；凭证作废已保证最终失效）。
- 首次启动检测到默认密码未改时的强制提示（当前只警告，不阻断）。

## 11. 验收标准

- 空库部署启动即可用：本机不登录、手机用 `admin/admin123` 登录，看到同一份节点列表。
- 扫码与手输 5 位码两条路都能把机器加入账号，预览信息与机器一致；agent 断线时同一码立刻不可用。
- 注册不需要邀请码、注册即登录；`closed` 模式下注册入口消失。
- 移除一台机器后：手机列表、项目列表、主节点偏好、目录偏好都不再残留它；该机器必须 `lework agent --reset` 才能回来。
- 所有失败都有可照做的提示，不出现裸的「请求失败」。
