# Lework 产品概览

> 状态：当前主线 ｜ 更新：2026-11-10 ｜ 范围：个人用户、单用户多节点 OpenCode 控制面
>
> 配套架构：`docs/architecture/overview.md` ｜ 路线图：`docs/product/roadmap.md`

## 0. 宣传层与当前阶段

- **宣传层**：Lework 是开源、可自托管、企业级品质的 AI 工作平台。企业级描述的是工程质量与私有化能力，不代表当前已提供服务。
- **当前阶段**：只服务个人用户，交付「单用户、多节点 OpenCode 控制面」。组织、RBAC、多租户、计费不在当前范围。

## 1. 一句话定义

**Lework = 单用户、多节点的 OpenCode 控制面。**

用现代 GUI 管理不同机器上的 `lework agent`，由 agent 管理本机 OpenCode 进程，按需把任务上下文通过 Git 带到目标节点执行，并统一管理 Session、Agent、插件、Action 与配置。

它不是另一个 Agent Runtime，也不是多租户平台。

---

## 2. 为什么做

### 2.1 当前真实痛点

| 痛点 | 具体表现 |
|---|---|
| 多机 TUI 管理 | 每台机器一个终端窗口，切换、查看、审批、恢复都靠人记 |
| 上下文割裂 | 项目在 A，算力在 B；任务要跨机执行时没有统一入口 |
| 配置依赖命令行 | 插件、Agent、MCP、模型、Action 分散在 JSON、CLI 和目录里 |
| 能力不可见 | OpenCode 的 session、工具、diff、权限、worktree 能力没有统一管理面 |
| 状态不可对账 | 断线、重启、切机后，不知道哪些 session 还在跑、哪些已完成 |

### 2.2 产品机会

OpenCode 已经把「执行」做得足够好。缺的是一层**面向个人开发者的控制面**：

- 把多台机器上的 OpenCode 当作资源池；
- 把 Session 当作任务；
- 把 Git branch/worktree 当作上下文隔离与传输协议；
- 把插件、Action、配置变成可视化、可对比、可回滚的对象。

---

## 3. 用户与边界

### 3.1 目标用户

- 独立开发者、个人研究者、小团队中的重度个人用户；
- 拥有 2 台以上机器：笔记本、工作站、服务器、DGX Spark；
- 已经在用 OpenCode，但不想继续用多个 TUI 管理；
- 接受 Git 作为项目与上下文的基础设施。

### 3.2 产品边界

| 做 | 不做 |
|---|---|
| 单用户、单控制面、多节点 | 组织、部门、多租户、RBAC |
| 管理 OpenCode 节点与 Session | 自建 Agent Runtime、自己实现工具执行 |
| Git branch/worktree 任务隔离与跨机同步 | 通用文件同步盘、网盘、远程桌面 |
| 管理插件、Action、MCP、Agent、模型配置 | 官方插件市场、计费、运营后台 |
| 本机 Web 优先，桌面壳后置 | 首版做全平台桌面客户端 |
| Spark USB-C 虚拟网卡接入 | 负责 Spark 上 OpenCode 的启动（由 `lework agent` 管理） |

### 3.3 非目标与反目标

**非目标**

- 不复制 OpenCode 的 session/message/event 状态。
- 不做自己的任务状态机；Task 只是 Session 的控制面视图。
- 不做组织、权限、邀请、审计合规。
- 不做多 Agent 编排平台。

**反目标（即使技术可行也算失败）**

- GUI 只是把 TUI 包一层，操作更慢。
- 多节点状态与 OpenCode 漂移。
- 跨机同步把用户工作区搞脏或丢改动。
- 配置修改无法回滚。
- 为了「统一」而把 OpenCode 能力削平。

---

## 4. 核心概念

| 概念 | 定义 | 说明 |
|---|---|---|
| **Node** | 一台运行 `lework agent` 的机器 | agent 主动连接控制面；由 agent 管理本机 OpenCode 进程 |
| **Agent** | 节点侧常驻进程 `lework agent` | 负责注册、心跳、OpenCode 进程管理、Git、配置、PTY 与本地代理 |
| **Project** | 一个逻辑项目 | 包含 Git remote、默认分支、名称 |
| **Project Location** | Project 在某节点上的工作目录 | `(node_id, directory)`；同一项目可在多节点有 mirror |
| **Task** | 一次要完成的工作 | 产品上等同于 Session；跨节点执行时附加 Git 上下文，不另建任务状态机 |
| **Session** | OpenCode 的会话 | 执行的唯一真相源；Task 通过 session_id 关联 |
| **Cross-node Run** | 一次跨节点 Git 执行 | 只记录分支/worktree/同步状态；不是新的 Agent 执行模型 |
| **Action** | OpenCode 快捷命令 / prompt 封装 | 支持全局与项目级；本质是可复用的提示词与参数 |
| **Context** | 任务执行所需的上下文 | Git 分支/工作区 + 必要配置 + Session 记录 |
| **Agent Channel** | 控制面与 agent 的持久连接 | agent 主动出站；不是 SSH，也不是直接暴露 OpenCode |

### 4.1 关系图

```text
Control Plane
  └── Node A
        └── lework agent
              ├── OpenCode server (child process)
              └── Project Location /work/foo
  └── Node B
        └── lework agent
              ├── OpenCode server (child process)
              └── worktree /work/foo/.lework/task/<id>
```

```text
Project (logical)
  ├── Location A: node=A, dir=/work/foo, role=primary
  ├── Location B: node=B, dir=/work/foo, role=mirror (optional)
  └── Task
        ├── source: Location A
        ├── target: Location B (ephemeral worktree)
        ├── branch: lework/task/<id>
        └── session: OpenCode session on B
```

### 4.2 关键语义

- **Project 不是目录**：目录只是 Project 在某个节点上的 Location。
- **Task 不是新状态机**：Task 的 running/review/merged 只描述 Git 执行上下文，不替代 OpenCode 的 session 状态。
- **Assignee 不是人**：在个人多机场景里，assignee 就是目标节点上的 `lework agent`。
- **Agent 管理进程**：systemd / launchd / Windows Service 只管理 `lework agent`；agent 负责启动、停止、重启和监控 OpenCode。
- **上下文按需同步**：不长期复制整个项目；只为一次任务创建最小执行上下文。

---

## 5. 核心用户流程

### 5.1 添加节点

```text
添加节点
  → 控制面生成一次性 enrollment token 和安装命令
  → 在目标机器执行安装命令
  → systemd 启动 lework agent
  → agent 主动连接控制面，完成注册与心跳
  → agent 启动/接管本机 OpenCode
  → 节点出现在统一节点列表
```

支持场景：

- 本机：agent 连回环控制面；
- 局域网/远程：agent 通过 TLS 出站连接控制面；
- Spark：agent 通过 USB-C 虚拟网卡出站连接。

验收：节点在线状态、agent 版本、OpenCode 版本、项目数、活跃 session 数可见；节点不需要暴露任何入站端口。

### 5.1.1 节点进程管理

```text
OS service manager (systemd / launchd / Windows Service)
  └── lework agent
        └── opencode serve (127.0.0.1, agent-owned password)
```

- 系统服务管理器（systemd / launchd / Windows Service）只负责 agent 常驻；
- I0 由 agent 自持普通 `opencode serve`，用生成或运维配置的密码同时配置 client
  与子进程环境；不使用用户级 `--service` 注册，避免顶掉用户的 TUI 服务；
- TUI 与 Lework 共享同一个 service 仍是目标，需要 I1 完成服务发现、XDG 隔离与
  密码交换；在此之前，TUI 默认看不到 agent 创建的 Session；
- 如果 TUI 使用独立 server，Lework 只显示历史，不接管实时事件；
- OpenCode 只绑定回环，不对外暴露；
- 控制面通过 agent 操作 OpenCode，不直接连 OpenCode。

### 5.2 打开项目与 Session

```text
选择节点
  → 选择项目目录
  → 查看该项目下的 Session
  → 打开 Session：对话、工具调用、审批、diff、文件、终端
```

验收：不打开 OpenCode TUI 也能完成日常查看与操作。

### 5.2.1 接管外部 Session

用户可能仍在 TUI 里直接使用 OpenCode，或在未登记的目录里创建 Session。Lework 不能假装看不见。

```text
节点连接
  → 列出该节点上的全部 OpenCode Session
  → 已登记 Location 的 Session 正常显示
  → 未登记目录的 Session 显示为「未纳管」
  → 用户可一键把目录加入 Project，或保持只读查看
```

原则：**Session 列表以 OpenCode 为准**；Lework 只负责把目录映射到 Project，不隐藏外部 Session。

前提：TUI 与 agent 共享同一个 OpenCode service。若 TUI 使用 `--standalone` 或独立 server，Lework 只能读取持久化历史，无法接管实时审批和 PTY；UI 会标记为「外部独立服务 / 只读」。

### 5.3 在单节点执行任务

```text
选择项目
  → 新建任务（本质是新建 Session）
  → 选择 Agent / 模型 / Action
  → 发送任务
  → 实时看执行
  → 处理审批
  → 查看 diff 与产物
```

### 5.4 A 项目，B 执行

```text
Project 在 A
  → 新建任务，选择目标节点 B
  → A 的 agent 创建任务分支与工作区快照
  → 通过共享 remote 或控制面中转的 Git bundle 把分支带到 B
  → B 的 agent 创建 worktree，启动 OpenCode Session
  → 在 B 执行
  → 结果分支回到 A
  → A 侧 review / merge / 清理
```

关键点：

- Git 操作由两端 agent 执行；控制面只编排，不需要 SSH。
- A 上的未提交改动和未跟踪文件先做快照，不直接污染当前工作区。
- B 不需要预先 clone 项目；agent 维护 per-remote 裸仓库缓存。
- B 上的执行是临时 worktree，不成为长期项目副本。
- 无共享 remote 时，控制面中转 Git bundle，节点之间不需要互相可达。
- 合并先在 A 生成结果 ref/临时分支，用户确认后显式合并，不默认改当前工作区。

### 5.5 管理插件、Action 与配置

```text
选择节点或项目
  → 查看当前有效配置
  → 查看与其它节点的差异
  → 编辑 Agent / 模型 / MCP / Skill / Plugin / Action
  → 预览 diff
  → agent 应用并重启 OpenCode（按需）
  → 失败可回滚
```

- 有运行中 Session 时默认拒绝/排队，避免重启误杀任务；
- 强制重启必须显式确认；
- 回滚只恢复配置，不承诺恢复被中断的 Session。

验收：常见配置不再需要登录节点改 JSON 或敲 CLI。

---

## 6. 信息架构

```text
全局
  ├── 节点总览
  ├── 项目
  ├── Session
  ├── Actions
  └── 设置

节点详情
  ├── 概览 / 健康
  ├── lework agent 状态 / 日志 / 版本
  ├── OpenCode 进程状态 / 重启
  ├── 项目与目录
  ├── Session
  ├── Agent / 模型
  ├── 插件 / MCP / Skill
  ├── Action / 命令
  └── 配置与差异

项目详情
  ├── Session
  ├── 分支 / worktree
  ├── 文件与 diff
  └── 设置

Session 详情
  ├── 对话时间线
  ├── 工具调用
  ├── 权限审批
  ├── 文件变更
  ├── 终端
  └── 任务上下文
```

### 6.1 主界面建议

```text
┌──────────────────────────────────────────────────────────────┐
│ 节点切换 | 项目 | Agent / 模型 | 搜索 / 命令面板              │
├───────────────┬──────────────────────────┬───────────────────┤
│ 节点 / 项目    │ Session 时间线            │ Diff / 文件 / 终端 │
│ Session 列表   │ 消息 / 工具 / 审批        │ 变更 / 产物        │
├───────────────┴──────────────────────────┴───────────────────┤
│ 状态栏：连接 / 版本 / 运行中 / 待审批 / 同步状态              │
└──────────────────────────────────────────────────────────────┘
```

设计原则：键盘优先、命令面板、可并排 diff、实时状态，不把操作藏进多层弹窗。

---

## 7. 产品能力分层

### 7.1 P0：没有它就不是这个产品

| 能力 | 说明 |
|---|---|
| 多节点 agent 注册与连接 | `lework agent` 主动出站；一次性 enrollment；Spark USB-C 入口 |
| 节点健康与版本 | agent 在线/离线、OpenCode 进程、版本、能力探测 |
| OpenCode 进程管理 | 启动、停止、重启、日志、崩溃拉起 |
| 统一 Session 管理 | 跨节点列表、打开、继续、中断、恢复 |
| Session 实时界面 | 消息、工具调用、审批、diff、文件、终端 |
| 项目 Location | `(node_id, directory)` 与 Git remote 绑定 |
| Git 任务上下文 | branch/worktree、快照、跨机取回、review/merge |
| 配置读取与对比 | 至少能看见每个节点的有效配置 |
| 单用户本地安全 | 本地绑定、token 加密、默认不暴露端口 |

### 7.2 P1：必要但不决定第一版闭环

| 能力 | 说明 |
|---|---|
| 配置写入与回滚 | Agent、模型、MCP、Skill、Plugin、Action |
| Action 管理 | 全局/项目级 prompt 封装、参数、预览 |
| 插件安装/更新 | 通过 agent 调用 OpenCode API 或本机命令 |
| 节点配置差异与批量应用 | 多机一致性 |
| 文件/产物浏览与下载 | diff 之外的文件操作 |
| 使用量/成本视图 | token、模型、节点维度 |
| 桌面壳 | 系统托盘、快捷键、自动更新 |
| 多窗口/多 Session 并排 | 重度使用效率 |

### 7.3 P2：不进入本产品

组织、部门、成员、RBAC、邀请、计费、配额、官方插件市场、反馈/飞书、遥测、品牌私有化、知识库、多组织切换、通用工作流编排。

---

## 8. 成功指标

| 指标 | 目标 |
|---|---|
| 添加第二台节点 | 执行一条安装命令，< 5 分钟 |
| 日常查看/继续 Session | 不打开 TUI |
| A→B 任务 | 不需要手工 Git 命令 |
| 断线恢复 | 不丢 Session 状态，不需要重启控制面 |
| 常见配置修改 | 不需要登录节点改 JSON |
| 多节点切换 | 一个窗口完成 |

---

## 9. 关键产品决策

| 决策 | 结论 | 理由 |
|---|---|---|
| 是否自建 Agent Runtime | 否 | OpenCode 已足够好，重复建设会漂移 |
| 旧产品关系 | 干净重开 | 不背旧发布兼容；旧代码只作参考，冲突模块直接删除 |
| 控制面 OS | Linux + macOS + Windows | 本地 Web 控制面跨平台；Go 交叉编译 |
| Agent OS | Linux + macOS + Windows | 节点覆盖三平台；I1 先 Linux，I2/I5 完成三平台验收 |
| 是否保留任务状态机 | 否 | Task 是 Session 视图；只保留 Git 执行上下文状态 |
| 是否做组织/权限 | 否 | 目标用户是个人；节点用 agent token 认证 |
| 默认连接方式 | `lework agent` 主动出站连接 | 节点不暴露入站端口，避免 SSH/端口/防火墙问题 |
| OpenCode 进程 | 由 agent 管理 | systemd/launchd/Windows Service 只管 agent，职责集中 |
| OpenCode service | agent 自持普通 serve；I1 再设计 TUI 共享 | 避免 `--service` 顶掉用户服务；TUI 共享是 I1 目标 |
| 目标节点仓库 | agent 维护 per-remote 缓存 | 目标节点不需要预先 clone |
| 配置重启 | 有活跃 Session 时默认拒绝/排队 | 避免误杀运行中的任务 |
| Git 合并 | 先生成结果 ref/临时分支，再显式合并 | 不默认改 A 的当前工作区 |
| Git 同步策略 | 共享 remote 优先，控制面中转 bundle 兜底 | 节点之间不需要互相可达 |
| 控制面形态 | 本地 Web 优先，桌面壳后置 | 多机连接集中在控制面 |
| Spark 接入 | agent 通过 USB-C 虚拟网卡出站；I2 只留接口 | 与普通节点同一套协议；实机验收放 I5 |
| Agent 安装 | 控制面生成手动安装命令 | 首版不做自动更新，避免签名/回滚复杂度 |
| Action 范围 | 全局 + 项目级 prompt 封装 | 与 OpenCode command 对齐 |

---

## 10. 风险与待验证

1. OpenCode experimental API 的稳定性与版本兼容。
2. OpenCode shared service 在多项目/worktree 并发下的隔离、锁与权限规则表现。
3. 三平台 agent 的进程管理、PTY、路径和安装包差异。
4. 未提交改动/未跟踪文件的快照语义，是否会让用户困惑。
5. 控制面中转 Git bundle 的可靠性与大仓库性能。
6. agent 的安装、升级、权限边界与故障恢复。
7. 多节点事件流无历史重放，重连对账体验是否足够好。
8. 「现代 GUI」的性能目标：大量事件、长会话、多个 diff 时不卡。

---

## 11. 一句话验收

> 一个人，一台控制面，多台运行 `lework agent` 的节点；从统一 GUI 里选项目、派任务、跨机执行、看 diff、合并结果，不需要再管理多个 TUI，也不需要 SSH 或暴露节点端口。
