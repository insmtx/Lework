# 个人控制面（I1-B 起）

I1-B 提供浏览器工作流：浏览器只访问控制面，控制面通过节点主动建立的
WebSocket 转发 OpenCode 请求；节点不监听入站端口，也不需要 SSH。

当前版本在此之上加了账号与节点归属：每个账号只看到、只能调度自己的节点，
节点通过 agent 出码 + 账号确认加入。所属关系落在 `node_members` 关联表，
后续「把节点共享给另一个账号」只需插行。设计见
`docs/design/2026-11-15-user-accounts-and-node-ownership-design.md`。

## 能力范围

- 账号：邮箱 + 密码登录与注册（`registration_mode` 默认 `open`，公网部署请改
  `closed`）；空库首次启动自动创建默认管理员 `admin` / `admin123`。
- 节点列表与会话列表：只显示自己（或共享给自己）的节点，含在线状态、OS/架构、最近心跳。
- 会话：按绝对工作目录创建 OpenCode Session，发送 prompt，中断当前运行。
- 会话视图：用户/助手消息、工具卡片、待处理的权限请求。
- Diff：按本轮用户消息读取文件改动，并提供并排视图。
- 文件：按目录浏览、读取文本文件、下载二进制文件；单次读取上限 512 KiB。
- 终端：Session 级持久 PTY，通过同源 WebSocket 传输二进制输入/输出。

不包含组织/RBAC、会话级共享、OAuth、Git 任务上下文、配置/Action 控制面、
Spark/一体机部署；这些属于后续阶段。本版新增 `lework opencode`，让 TUI 连接
agent 管理的 service（见下文「启动节点 Agent」），属于 I1 的增量能力。

## 构建

静态界面在构建时嵌入 `lework` 二进制：

```bash
make frontend-control-plane   # 构建 frontend/apps/control-plane 并复制到 webui/dist
go build -o ./bundles/lework ./backend/cmd/lework/
```

`make build` 会先执行 `frontend-control-plane`。直接执行 `go build` 且没有
生成静态资源时，浏览器访问 `/` 会看到构建提示页；API 仍可用。

Docker 镜像 `deployments/build/Dockerfile.lework` 包含独立的 Node 构建阶段，
发布镜像不需要在宿主机安装 pnpm。

## 启动控制面

```bash
lework serve \
  --listen 127.0.0.1:8787 \
  --nats-url nats://127.0.0.1:4222 \
  --database-url postgres://lework@127.0.0.1:5432/lework \
  --enrollment-token "$LEWORK_ENROLLMENT_TOKEN"
```

控制面默认只绑定回环地址。浏览器打开 `http://127.0.0.1:8787/` 用账号登录。
注册令牌是部署级管理员凭据（脚本与远程运维的 Bearer），不再用于创建账号，
也不会出现在日志中。

### 初始化与默认账号

空库首次启动时，控制面自动创建管理员 `admin` / `admin123`（role `admin`），
并在启动日志里打印一条警告。它只创建一次：库里已有任何账号时不会再写。

本机（回环）请求直接解析为这个 admin 账号：在本机启动 `lework` 后，浏览器
不用登录就能看到该账号下的节点；账号被删掉时回环请求退回 `KindLocal`（部署级
身份），不会凭空生成账号。

拿到手机或对外提供服务之前，先把默认密码改掉。在运行控制面的本机执行即可，
回环请求已经解析成 admin 账号，不需要额外带令牌：

```bash
curl -sS -X POST http://127.0.0.1:8787/api/auth/password \
  -H 'Content-Type: application/json' \
  -d '{"current_password":"admin123","new_password":"<新密码>"}'
```

从别的机器调用时，带 `Authorization: Bearer <会话 token>`（`POST /api/auth/login`
响应里的 `token`）。

手机上也有同一个入口：设置 → 账号 → 修改密码（要求旧密码，成功后其他设备
的会话会被吊销）。密码至少 8 位。

控制面把节点注册与心跳写入 PostgreSQL，并用 NATS 扇出事件、转发跨实例请求。
两个地址都必填（命令行或配置文件），且 NATS 必须启用 JetStream；缺任意一个
启动都会失败并提示对应配置项。数据库连接串与 NATS 地址可能内嵌账号密码，
控制面只回显配置项名称，不会把 URL 写进日志或错误信息。

## NATS + PostgreSQL（集群依赖）

控制面不再使用本地 SQLite 作为运行存储：节点身份、`last_seen`、设备与项目
索引都在 PostgreSQL 中，任一实例收到心跳都会刷新同一行。事件、跨实例请求
与终端输出走 NATS，因此多实例之间不需要粘性会话。

升级说明：旧版本写在本地 `controlplane.db` 里的节点数据不会自动导入
PostgreSQL。节点重连后会重新注册并刷新 `last_seen`，不需要手工迁移；生产
启动也不再读取该文件。

统一的 `lework.yaml` 可以把两段直接并列（裸 `lework`、`serve`、`agent`
共用同一个 `--config`）：

```yaml
controlplane:
  listen: 127.0.0.1:8787
  token_file: ~/.config/Lework/enrollment.token
  request_timeout: 30s
  heartbeat_timeout: 35s

nats:
  url: nats://<nats-host>:4222

database:
  url: postgres://<user>:<password>@<pg-host>:5432/<database>?sslmode=disable
  debug: false

agent:
  node_name: dev-box
  workspace_roots:
    - /opt/lework/dev-box/workspace
```

命令行 `--nats-url` / `--database-url` 优先于文件里的 `nats:` / `database:`
两段；显式传空值会交给启动校验直接报错，而不是静默回退。

### Subject 约定

控制面只使用 `cp.` 前缀，NATS 账号至少需要这些 subject 的发布/订阅权限：

| Subject | 传输 | 用途 |
|---|---|---|
| `cp.events.node.<node_id>` | JetStream（`CONTROLPLANE_EVENT_STREAM`，`cp.events.node.>`） | 节点/会话状态事件、`node.connected`/`node.offline`、`node.snapshot`、`permission.asked` |
| `cp.live.node.<node_id>` | Core NATS（各实例订阅 `cp.live.>`） | 流式增量（`session.text.delta`、`session.reasoning.delta`、`session.tool.input.delta`、`session.tool.progress`）；不落盘、无订阅者即丢弃，只有 SSE 带 `live=1` 的订阅者会收到 |
| `cp.node.<node_id>.request` | Core NATS | 控制面实例之间转发节点方法调用，队列组按节点 ID 分摊 |
| `cp.pty.<node_id>.<pty_id>` | Core NATS | 终端输入/输出与 `pty.exit`，不落盘、无订阅者即丢弃 |

node/pty ID 中的点号、通配符与空白会被替换为下划线，保证每个标识只占一个
subject token，避免节点伪造其它节点的订阅。

审批推送使用持久化 pull consumer `controlplane-push-consumer`，所有实例用同名
durable 消费同一条流，因此同一条 `permission.asked` 只推一次。执行结束
（`session.execution.succeeded` / `session.execution.failed`）复用同一 consumer，
去重键带上事件时间，JetStream 重投不会重复推送，而同一会话的下一轮结束仍会推。

升级提示：`DeliverNew` 只在 durable 首次创建时生效。如果旧版本已经创建过
`controlplane-push-consumer`，需要先删除该 consumer（或整个 stream）让新版本
重建，否则会沿用旧策略把 stream 里的历史审批重新推送一遍。

### 多实例要求

- 所有实例指向同一个 PostgreSQL 与同一个 NATS（同一个 JetStream 域）。
- 每个实例都需要能主动出站访问 NATS；节点只需要能访问任意一个实例的
  `/api/agent/connect`，不要求代理把同一节点粘到同一实例。
- 不需要共享文件系统或本地状态：会话、消息、diff、权限仍以节点上的
  OpenCode 为准，控制面只保存节点索引与派生投影。
- 新实例启动后靠 owner 每 10s 发布的 `node.snapshot` 收敛视图；启动瞬间可能
  短暂看不到正在跑的会话，属于预期。
- 每个实例都会运行心跳对账器；对账与网关的离线事件都是幂等的，重复执行
  不会让浏览器收到多条同样的状态。
- NATS 的 `max_payload` 必须不小于 agent 单帧上限（8 MiB）；否则节点挂在别的
  实例时，大响应（例如 200 条消息的 `session.messages`）会在转发时被拒绝。
  `deployments/devcj/setup.sh` 与 `deployments/dev/docker-compose.dev.yml`
  已使用 `8388608`。

### 故障表现

| 现象 | 表现 |
|---|---|
| 节点连在别的实例，当前实例没有本地连接 | 请求经 `cp.node.<id>.request` 转发；无人应答（节点确实离线）返回 `409 node_offline` |
| 节点在线但响应太慢 | 超过 `request_timeout` 返回 `504 agent_timeout` |
| 节点进程被杀或网络断开 | 超过 `heartbeat_timeout` 后 `/api/nodes` 把 `connected` 置为 `false`，对账器补发 `node.offline` |
| NATS 不可用 | 启动失败；运行中事件扇出与跨实例转发失败，节点连接还会因事件发布失败而断开 |
| PostgreSQL 不可用 | 启动失败；节点列表、设备与偏好等读取返回 500，心跳写入失败会让节点连接退出 |

## 配置文件

`serve`、`agent` 与裸 `lework` 都接受 `--config`，默认读平台配置目录下的
`lework.yaml`（Linux 是 `/etc/lework/lework.yaml`），不存在时回退同目录的
`agent.yaml`。一个文件容纳两段，控制面与节点 agent 共用：

```yaml
controlplane:
  listen: 127.0.0.1:8787
  token_file: ~/.config/Lework/enrollment.token
  request_timeout: 30s
  heartbeat_timeout: 35s
  public_url: http://le.ckeyer.com      # 已废弃：启动不再打印二维码，保留只为兼容旧配置
  expo_url: http://le.ckeyer.com:8082  # 已废弃，同上

nats:
  url: nats://127.0.0.1:4222

database:
  url: postgres://<user>:<password>@127.0.0.1:5432/<database>?sslmode=disable

agent:
  node_name: dev-box
  workspace_roots:
    - /opt/lework/dev-box/workspace
  opencode:
    port: 4096
    service_mode: shared
```

`nats:` 与 `database:` 是控制面的集群依赖，与 `controlplane:` 并列写在顶层，
不嵌套在 `controlplane:` 段内。

优先级固定为 **命令行 flag > 配置文件 > 派生默认**，文件不会覆盖显式 flag。

- `controlplane:` 段供 `serve` 与裸 `lework` 使用；`serve token` 也会读
  `token_file`，保证两边打印同一个令牌。
- `agent:` 段供 `agent` 与裸 `lework` 使用。在裸 `lework` 组合模式下，
  `control_plane_url`、`enrollment_token`、`node_name`、`workspace_roots`
  留空时分别由 `controlplane.listen`、控制面注册令牌、主机名、
  `/opt/lework/<用户>/workspace` 派生——这就是「能复用就复用」的部分；
  独立运行 `agent` 时前三项仍需显式配置。
- 旧的扁平 `agent.yaml`（顶层直接写 `control_plane_url` 等，没有
  `controlplane:` 段）仍然被识别，不需要一次性迁移。
- 只有 `controlplane:` 段的文件不会被误读成空的 agent 配置，此时 agent 仍会
  回退到默认 `agent.yaml` 或派生默认值。
- 路径字段支持 `~` 与环境变量展开；`enrollment_token` 不建议写进文件，
  用 `token_file` 更安全。

## 一体化启动

裸 `lework` 会在同一进程内同时启动控制面与节点 agent，适合本机单机使用：

```bash
./bundles/lework
```

它只启动后端，不会构建前端静态资源；界面仍需由 `make build` 或
`make frontend-control-plane` 预先嵌入。裸执行不再打印帮助，帮助入口是
`lework -h`；`serve` 与 `agent` 子命令仍可各自独立启动。

合并进程的 flag 只挂在根命令上：`--config`、`--listen`、`--nats-url`、
`--database-url`、`--enrollment-token`、`--token-file`、`--request-timeout`、
`--heartbeat-timeout`、`--public-url`、`--expo-url`、`--no-qr`、`--agent-config`、
`--node-name`、`--workspace-root`（可重复）。

`--config` 指向统一配置文件（默认 `/etc/lework/lework.yaml`，缺失时回退
`agent.yaml`），文件里的 `controlplane:` 与 `agent:` 两段分别补控制面与节点
agent 的配置，详见上文「配置文件」。

节点 agent 配置按以下优先级合并：

1. 命令行显式覆盖（`--node-name`、`--workspace-root`）。
2. 显式 `--agent-config` 指定的文件；文件必须存在，否则启动失败。
3. `--config` 文件的 `agent:` 段（未给 `--agent-config` 时）。
4. 默认路径下的 `agent.yaml`（存在才读取）。
5. 派生默认值：控制面地址来自 `--listen`，令牌来自控制面解析结果，
   节点名取主机名，工作目录取 `/opt/lework/<用户名>/workspace`
   （用户名取运行进程的 OS 用户，首次启动自动创建该目录）。

默认路径 `agent.yaml` 或 `agent:` 段中显式写出的 `control_plane_url` 与
`enrollment_token` 优先于派生值，不会被覆盖。

默认工作目录按 OS 用户分层：`/opt/lework/<用户名>/workspace`。同一台机器上
不同用户各占一层，会话与文件访问仍然受该节点的 `workspace_roots` 约束。只在
没有显式配置时才会创建这个目录；`--workspace-root` 或 `agent.yaml` 里写出的
目录不会被自动创建，也不做迁移。

组合模式通常要求 `--listen` 使用固定端口（如 `127.0.0.1:8787`）；未在 agent
配置中显式指定 `control_plane_url` 时，端口 `0` 会被拒绝，因为 agent 无法通过
派生地址稳定连回自身。若 agent 配置已显式给出 URL，则 `--listen` 的端口不再
参与派生，`0` 可由系统随机分配。

### 注册令牌

`serve` 与裸执行都会自动解析注册令牌：显式 `--enrollment-token`
（或 `LEWORK_ENROLLMENT_TOKEN`）优先，其次是 `--token-file`
（或 `LEWORK_TOKEN_FILE`），文件缺失时生成并写回。

默认令牌文件是 `~/.config/Lework/enrollment.token`（目录 `0700`、文件 `0600`）。
显式传入的令牌也会落盘，保证后续启动一致。`lework serve token` 打印当前
生效的令牌，供脚本与远程运维使用；令牌本身不会写入日志。

这个令牌是**部署级管理员凭据**，不再充当浏览器登录态，也不再用于创建账号：
它只用于脚本/自动化里的 `Authorization: Bearer`。它不会写进任何 Cookie，
也不会出现在配对二维码里。

令牌只接受可打印 ASCII，不能包含空格、双引号、逗号、分号和反斜杠；不满足的
显式值会在写盘前被拒绝，文件里的此类令牌也会在启动时报错。


### 浏览器鉴权（账号登录）

浏览器用邮箱密码走 `POST /api/auth/login`；首次使用直接用空库自动创建的
`admin` / `admin123`（见上文「初始化与默认账号」）。会话写在 `HttpOnly` 的
`lework_session` cookie 里（`SameSite=Strict`），30 天滑动续期；
`DELETE /api/auth/session` 登出。

注册默认开放（`registration_mode: open`）：`POST /api/auth/register` 只需要
`{email, password}`，没有邀请码；注册成功直接返回会话。改成 `closed` 后注册
接口返回 `403 registration_closed`。同一来源 IP 的注册尝试限制为 10 次/分钟，
超出返回 `429`。

登录页需要的公开信息在 `GET /api/auth/methods`：
`{initialized, registration_mode}`。`initialized=false` 表示库里还没有账号，
页面据此提示用默认管理员登录并以 `POST /api/auth/password` 改密码。

脚本与移动端可以用 `Authorization: Bearer <enrollment token>`（部署级管理员），
也可以用登录响应返回的会话 token（移动端不保存 cookie）。同一个邮箱 + 来源 IP
的登录失败超过 10 次/分钟后返回 `429`，正常失败的响应体仍是统一的
`401 invalid_credentials`。

绑定非回环地址（例如 `--listen 0.0.0.0:8787`）时，`local_admin` 默认开启：直接
来自回环的请求仍然视为本机管理员。但只要出现 `X-Forwarded-For`、`X-Real-IP`
或 `Forwarded` 任一转发头，就按外部请求处理并要求账号登录。

反向代理需注意：

- 代理应保留 `X-Forwarded-For` 等转发头，否则控制面会把外部请求当成本机请求
  放行，此时必须由代理层自行鉴权。
- cookie 的 `Secure` 判定是「请求本身是 TLS，或代理声明
  `X-Forwarded-Proto: https`」。控制面自身不终止 TLS，所以只有可信代理才能设置
  这个头；直连用户可以用它伪造 `Secure`，但那只会让 cookie 不被明文发送。
- 控制面启动时不再打印手机配对二维码；由此 `--public-url`、`--expo-url`、
  `--no-qr` 以及配置里的 `public_url` / `expo_url` 仅保留为兼容用的空操作。

## 启动节点 Agent

节点 agent 与控制面共用同一个 `lework.yaml`，但独立运行的 agent 仍然需要
显式的控制面地址（它可能连的是另一台机器上的控制面，不能从本文件的
`controlplane:` 段猜测）。`enrollment_token` 只在旧直通路径里用到，可以不写：
没有本地凭证时 agent 会自己进入配对流程。`workspace_roots` 可以省略，省略时
使用 `/opt/lework/<用户>/workspace` 并自动创建：

```yaml
# /etc/lework/lework.yaml（Linux）
controlplane:
  listen: 127.0.0.1:8787
agent:
  control_plane_url: ws://127.0.0.1:8787/api/agent/connect
  enrollment_token: "<与控制面一致的令牌>"
  node_name: dev-box
  workspace_roots:
    - /home/me/projects
  opencode:
    binary: opencode
    port: 4096
    service_mode: shared
```

```bash
lework agent --config /etc/lework/lework.yaml
```

### 把节点加入账号

第一次启动且本地没有凭证时，agent 会连上 `/api/agent/connect` 并打印二维码与
5 位短码。用账号登录浏览器（或手机）后扫码/输码确认，节点就归属到该账号，
凭证由控制面经这条出站连接下发并写到 `~/.config/Lework/node.credential`
（`0600`；配置在别处时与配置文件同目录）。

机器不在面前时，在网页「添加节点」出码，然后在机器上执行：

```bash
lework agent --code ABC12            # 一次性码，失败即退出
lework agent --config /etc/lework/lework.yaml --code ABC12
```

终端被日志刷走时，agent 已经把配对信息写进 `node.claim`，用下面这条重印
（要求 agent 仍在待定连接中；过期会提示重新配对）：

```bash
lework agent claim
lework agent claim --no-qr           # 只打印链接与短码
```

自动化场景可以预置凭证文件（`--credential-file` 或 `LEWORK_NODE_CREDENTIAL`）。
注意本轮还没有「直接签发凭证」的接口，凭证需要先配对一次再复制
`node.credential`；文件权限保持 `0600`。

凭证失效（控制面里删了该节点、或数据库被重置）时 agent 不会自动重新配对，
只打印提示并继续重试：删除控制面里的旧节点后重新执行上面的配对流程。

手机上也能走同一套配对：设置路径是「我的电脑 → 添加」或「设置 → 账号」附近
的入口，两个按钮分别是「扫描二维码」与「输入配对码」。扫码会解析
`lework://node/pair?url=…&code=…`；码属于另一个控制面时会先提示切换（切换
等于退出当前登录）。列表里的「⋯ → 移除」只有节点所有者能执行。

移除节点后要重新加回来，在机器上先停掉正在运行的 agent，再执行：

```bash
lework agent --reset          # 删除本地凭证与待定配对码，然后正常启动并重新出码
```

`--reset` 只动本地文件，不做远端写入；它刻意忽略 enrollment token，否则
「没有凭证 + 有 token」会走旧直通，登记成手机看不到的未认领节点。凭证来自
`LEWORK_NODE_CREDENTIAL` 时无法删除，命令会直接报错，先 unset 再重试。

旧版本 agent（不认识 `welcome` 里的 `credential`）继续用注册令牌登记：控制面
按名字复用同一条**未认领**记录，不会每次重连多出一个节点。但如果它和一台已
升级的 agent 用了同一个 `node_name`，令牌每次登记都会轮换该记录的凭证，升级后
的那台就会看到「节点凭证被拒绝」。处理方式是升级旧二进制或改掉重名的
`node_name`。已被账号认领的节点不会被注册令牌接管，旧 agent 必须先升级才能让
同一个节点重新上线。

需要在 TUI 里同时操作同一个 service 时，在另一个终端运行：

```bash
lework opencode /home/me/projects
```

该命令读取 agent 发布的服务端点（要求同一 OS 用户），以
`opencode --server <url>` 启动 TUI；控制面与 TUI 共享 Session、消息与审批。
直接运行 `opencode` 或使用 `--standalone` 不会共享。

裸 `lework`（控制面与 agent 同进程）不需要写 `control_plane_url`、
`enrollment_token`、`node_name`，它们会从 `--listen`、注册令牌和主机名派生，
见上文「一体化启动」。

旧的扁平 `agent.yaml`（顶层直接写 `control_plane_url` 等）仍然可用。

`workspace_roots` 限制可创建会话和读取文件的目录；所有路径参数必须是绝对
路径，文件读取和 PTY 都会在 Agent 侧做边界校验。节点掉线后，控制面的请求
返回 `409 node_offline`，浏览器可稍后重试。

## 浏览器协议

所有浏览器请求走同源相对路径，不使用旧版 `/v1`。除 `/api/auth/methods`、
`/api/auth/login`、`/api/auth/register` 与 `/healthz` 外，其余端点都要求
已登录（同源 `lework_session` cookie，或 `Authorization: Bearer`）；节点侧
的出站入口 `/api/agent/connect` 用自己的节点凭证/注册令牌鉴权：

- `GET /api/nodes`、`GET /api/sessions`
- `POST /api/sessions`、`POST /api/sessions/{node}/{session}/prompt`
- `POST /api/sessions/{node}/{session}/interrupt`
- `GET /api/sessions/{node}/{session}/diff?directory=...`
- `GET /api/nodes/{node}/files?directory=...&path=...`
- `GET /api/nodes/{node}/files/content?directory=...&path=...`
- `POST /api/sessions/{node}/{session}/pty`、`DELETE .../pty/{ptyID}`
- `GET /api/pty/{node}/{session}/{ptyID}/ws`
- `GET /api/events`（SSE，含 `resync` 命名事件）；支持 `?node=`、`?session=`
  收窄范围，`?live=1` 额外接收 token 级增量事件（默认不下发）。
  `session` 过滤只丢弃「带 `session_id` 且不匹配」的事件，节点级事件
  （如 `agent.resync_required`）照常送达，否则断档后页面不会重拉。

PTY WebSocket 的二进制帧是终端输入/输出；文本帧只承载
`{"type":"resize","cols":...,"rows":...}`、`{"type":"close"}`，以及服务端发出的
`{"type":"exit"}` / `{"type":"error","message":...}`。

## 注意事项

- Diff 依赖 OpenCode 对 git 工作区的快照。非 git 目录可能返回空 diff。
- 终端绑定到 Session，不提供任意路径的通用终端；目录必须位于
  `workspace_roots` 内。
- 节点注册记录与 `last_seen` 存在 PostgreSQL，所有控制面实例共享；Session、
  消息、diff、权限以 OpenCode 为准，控制面只做透传与索引。节点心跳更新
  `last_seen`，`connected` 由心跳窗口推导，因此连在其它实例上的节点同样显示
  在线。
