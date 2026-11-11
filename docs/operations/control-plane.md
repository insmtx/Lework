# 单节点控制面（I1-B）

I1-B 提供一个单用户、单节点的浏览器工作流：浏览器只访问控制面，控制面通过
节点主动建立的 WebSocket 转发 OpenCode 请求；节点不监听入站端口，也不需要
SSH。

## 能力范围

- 节点列表与会话列表：显示在线状态、OS/架构、最近心跳。
- 会话：按绝对工作目录创建 OpenCode Session，发送 prompt，中断当前运行。
- 会话视图：用户/助手消息、工具卡片、待处理的权限请求。
- Diff：按本轮用户消息读取文件改动，并提供并排视图。
- 文件：按目录浏览、读取文本文件、下载二进制文件；单次读取上限 512 KiB。
- 终端：Session 级持久 PTY，通过同源 WebSocket 传输二进制输入/输出。

I1-B 不包含多节点调度、Git 任务上下文、配置/Action 控制面、TUI 共享和
Spark/一体机部署；这些属于后续阶段。

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
  --db ~/.config/Lework/controlplane.db \
  --enrollment-token "$LEWORK_ENROLLMENT_TOKEN"
```

控制面默认只绑定回环地址。注册令牌用于节点首次注册，不会出现在日志中。
浏览器打开 `http://127.0.0.1:8787/`。

## 一体化启动

裸 `lework` 会在同一进程内同时启动控制面与节点 agent，适合本机单机使用：

```bash
./bundles/lework
```

它只启动后端，不会构建前端静态资源；界面仍需由 `make build` 或
`make frontend-control-plane` 预先嵌入。裸执行不再打印帮助，帮助入口是
`lework -h`；`serve` 与 `agent` 子命令仍可各自独立启动。

合并进程的 flag 只挂在根命令上：`--listen`、`--db`、`--enrollment-token`、
`--token-file`、`--request-timeout`、`--heartbeat-timeout`、`--agent-config`、
`--node-name`、`--workspace-root`（可重复）。

节点 agent 配置按以下优先级合并：

1. 命令行显式覆盖（`--node-name`、`--workspace-root`）。
2. 显式 `--agent-config` 指定的文件；文件必须存在，否则启动失败。
3. 默认路径下的 `agent.yaml`（存在才读取）。
4. 派生默认值：控制面地址来自 `--listen`，令牌来自控制面解析结果，
   节点名取主机名，工作目录取用户主目录。

默认路径 `agent.yaml` 中显式写出的 `control_plane_url` 与 `enrollment_token`
优先于派生值，不会被覆盖。

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
生效的令牌，可用于远程浏览器引导；令牌本身不会写入日志。

令牌会原样用作 Cookie 值与 `Authorization: Bearer` 凭据，所以只接受可打印
ASCII，不能包含空格、双引号、逗号、分号和反斜杠；不满足的显式值会在写盘前
被拒绝，文件里的此类令牌也会在启动时报错。

### 非回环监听与浏览器鉴权

绑定非回环地址（例如 `--listen 0.0.0.0:8787`）后，除 `/healthz` 与
`/api/agent/connect` 外的浏览器 API 都需要注册令牌：

- 首次在浏览器打开 `http://<host>:8787/?token=<token>`，校验通过后写入
  `HttpOnly` 的 `lework_session` cookie，并 302 到去掉 `token` 的同一地址。
- 脚本可改用 `Authorization: Bearer <token>`。
- 直接来自回环的请求仍然放行；但只要出现 `X-Forwarded-For`、`X-Real-IP`
  或 `Forwarded` 任一转发头，就按外部请求处理并要求令牌。

反向代理需注意：代理应转发真实的客户端信息（保留 `X-Forwarded-For` 等转发头），
浏览器也必须携带令牌访问；如果代理把请求转发到本机回环又剥掉了转发头，控制面
会把它当成本机请求放行，此时必须由代理层自行鉴权。`?token=` 会短暂出现在
浏览器历史与代理访问日志中，302 只能缩小暴露窗口。

## 启动节点 Agent

```yaml
# /etc/lework/agent.yaml（Linux）
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
lework agent --config /etc/lework/agent.yaml
```

`workspace_roots` 限制可创建会话和读取文件的目录；所有路径参数必须是绝对
路径，文件读取和 PTY 都会在 Agent 侧做边界校验。节点掉线后，控制面的请求
返回 `409 node_offline`，浏览器可稍后重试。

## 浏览器协议

所有浏览器请求走同源相对路径，不使用旧版 `/v1` 或登录态：

- `GET /api/nodes`、`GET /api/sessions`
- `POST /api/sessions`、`POST /api/sessions/{node}/{session}/prompt`
- `POST /api/sessions/{node}/{session}/interrupt`
- `GET /api/sessions/{node}/{session}/diff?directory=...`
- `GET /api/nodes/{node}/files?directory=...&path=...`
- `GET /api/nodes/{node}/files/content?directory=...&path=...`
- `POST /api/sessions/{node}/{session}/pty`、`DELETE .../pty/{ptyID}`
- `GET /api/pty/{node}/{session}/{ptyID}/ws`
- `GET /api/events`（SSE，含 `resync` 命名事件）

PTY WebSocket 的二进制帧是终端输入/输出；文本帧只承载
`{"type":"resize","cols":...,"rows":...}`、`{"type":"close"}`，以及服务端发出的
`{"type":"exit"}` / `{"type":"error","message":...}`。

## 注意事项

- Diff 依赖 OpenCode 对 git 工作区的快照。非 git 目录可能返回空 diff。
- 终端绑定到 Session，不提供任意路径的通用终端；目录必须位于
  `workspace_roots` 内。
- 节点注册记录与 `last_seen` 存在控制面 SQLite；Session、消息、diff、权限以
  OpenCode 为准，控制面只做透传与索引。节点心跳更新 `last_seen`，但
  `connected` 取实时注册表。
