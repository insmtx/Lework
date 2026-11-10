# Lework 路线图

> 更新：2026-11-10 ｜ 主线：个人用户、单用户多节点 OpenCode 控制面

当前进度：I0 完成，I1 的 I1-B（单节点浏览器工作流）已实现；I1 剩余 TUI 共享、安装与三平台抽象，I2–I5 未开始。

## I0：技术验证

**目标**：证明 `lework agent` 能注册、管理 OpenCode，并完整驱动一个节点。

- agent 注册、心跳、token 校验；
- agent 接管/启动 shared OpenCode service；
- 列项目/会话、建会话、发 prompt；
- 订阅事件，显示流式文本、工具、审批；
- 读取 diff、文件、终端；
- 验证 worktree、session export/import、config/plugin/MCP 列表；
- 验证单服务多项目/worktree 并发，以及 TUI 共享同一服务；
- supervisor / PTY / 路径抽象按三平台设计，先 Linux 实测。

**退出标准**：临时页面完成「agent 上线 → 选目录 → 建 Session → 执行 → 看 diff → 审批」。

## I1：单节点垂直切片

**目标**：一个节点的日常使用完全脱离 TUI；先以 Linux 交付，不阻塞三平台抽象。

- agent 连接与节点抽象从第一天存在；
- 项目/会话列表、对话流、工具、审批、diff、文件、终端；
- 新建/继续/中断 session；
- 本地元数据只存节点和 UI 偏好；
- systemd / launchd / Windows Service 的进程管理接口先定型。

**退出标准**：单节点用户不需要打开 OpenCode TUI，节点无需 SSH 或入站端口。

## I2：多节点控制面

**目标**：解决多机 TUI 管理痛点，并完成 macOS/Windows agent 基础支持。

- `lework agent` 安装命令、一次性 enrollment、心跳与断线重连；
- agent 管理 OpenCode 进程；
- macOS launchd、Windows Service 的安装与进程管理；
- Unix PTY 与 Windows ConPTY 基础支持；
- Spark USB-C 入口与预设（本迭代只要求接口与手动地址，不要求实机验收）；
- 健康检查、版本兼容；
- 统一 Session 列表与节点过滤；
- 每节点 token 加密存储。

**退出标准**：两台以上机器在一个 GUI 里管理，节点不暴露入站端口；至少完成 Linux + macOS 或 Linux + Windows 的双平台验证。

## I3：Git 任务上下文

**目标**：A 项目，B 执行，结果回 A。

- Project/Location 模型；
- 两端 agent 执行 Git：快照、fetch、worktree、commit；
- 共享 remote 优先，控制面中转 Git bundle 兜底；
- B worktree 执行、结果回传、A review/merge；
- 冲突、失败、回滚状态。

**退出标准**：跨机任务全流程不需要手工 Git 命令，也不需要节点间互相可达。

## I4：配置与 Action 控制面

**目标**：把命令行配置工作搬进 GUI。

- 读取有效配置并展示差异；
- 管理 Agent、模型、MCP、Skill、Plugin、Action；
- 修改预览、应用、回滚；
- agent 负责本机文件写入与 OpenCode 重启。

**退出标准**：常见配置不再需要登录节点改 JSON。

## I5：产品化与 AIO

**目标**：可交付的个人工具。

- 首次启动向导、节点 bootstrap 说明、备份/恢复、升级；
- Linux/macOS/Windows 三平台安装包、签名与实机验收；
- 日志与诊断；
- A/B 双机 Golden Demo；
- Spark USB-C 实机验收；
- 本地 Web 打包，桌面壳后置。

**退出标准**：新用户安装后能在两机完成一次真实任务并合并。
