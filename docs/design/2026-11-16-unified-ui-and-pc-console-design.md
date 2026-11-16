# 统一移动端与 PC 控制面（OpenCode Workspace）设计

- 日期：2026-11-16
- 范围：`frontend/apps/mobile`、`frontend/apps/web`、`frontend/apps/control-plane`、`frontend/packages/*`
- 决策者：产品（已确认）
- 状态：已确认，进入实现

## 1. 背景与目标

当前仓库有三套前端：

- `apps/mobile`：Expo/React Native，功能主线（节点、OpenCode 会话、审批、项目、设置）。
- `apps/web`：企业级 Web 工作台，视觉与信息架构是产品要保留的 PC 形态。
- `apps/control-plane`：过渡期的浏览器控制台，能力与移动端一致，但体验不再投入。

目标：

1. **统一视觉体验**：以 `apps/web` 的视觉语言为唯一基准（配色、圆角、阴影、logo、
   加载/等待动画），迁移到移动端；两端各自保留适合平台的布局与导航。
2. **统一能力**：`apps/web` 补齐移动端已有的 OpenCode 控制面能力（节点纳管与配对、
   会话、审批、Diff、文件、终端、总览、设置）。
3. **统一“项目”概念**：两端的“项目”最终都是 OpenCode workspace；企业项目下的
   “任务 / 活动 / 文件”对应改为 workspace 下的“会话 / 活动 / 文件”。
4. **最终只留一套后端**：控制面（`lework serve` 的 `/api`）是唯一目标后端；
   企业 `/v1` 与依赖它的页面只作为过渡，后续逐步迁移并删除。

## 2. 非目标

- 这一轮不给移动端增加新功能，只做视觉/体验迁移。
- 不删除 `apps/control-plane`，但把可复用组件抽成共享包，后续由 `apps/web` 取代。
- 不放宽控制面的 Cookie/CORS 安全策略来迁就跨域；PC 通过同源代理访问 `/api`。
- 不把 OpenCode 会话状态复制到控制面；会话、消息、Diff、权限仍以 OpenCode 为准。
- 不在这一轮完成 `/v1` 的删除；旧页面保留为过渡入口。

## 3. 关键决策

### 3.1 设计 token 单一来源

新增 `frontend/packages/design-tokens`（`@lework/design-tokens`），纯 TS、无框架依赖：

- `brand`：靛蓝主色、浅蓝侧栏、蓝色强强调。
- `lework`：与 `packages/styles/globals.css` 中 `--lework-*` 对齐的语义色。
- `radii` / `shadows` / `typography`。
- 移动端 `src/theme/theme.ts` 只做“语义角色 → token”的映射，不再写死十六进制。
- 增加 parity 测试：读取 `packages/styles/globals.css`，断言重叠角色取值一致，
  防止两端再次漂移。Web CSS 仍是 Web 的消费形态，但共享值以 token 包为准。

### 3.2 会话 UI 单一实现

新增 `frontend/packages/control-ui`（`@lework/control-ui`）：

- 从 `apps/control-plane` 抽出与产品无关的部分：会话/消息、审批卡片、工具渲染、
  Diff、文件浏览、终端、`parsePatch`、SSE hook、节点侧栏、会话面板。
- 通过 `ControlApiProvider` 注入 `ApiClient` 与 `eventsUrl` / `ptyUrl`，组件不直接
  读环境变量、不改写 `window.location`。
- `apps/control-plane` 改为消费该包，保证只有一份实现。
- `apps/web` 用同一套组件构建 PC 控制台，外观由 `apps/web` 的 Tailwind/token 决定。

### 3.3 PC 访问控制面

`apps/web` 继续作为 PC 产品壳，新增 Next rewrite：

- `/api/:path*` → `LEWORK_CONTROL_PLANE_URL`（默认 `http://127.0.0.1:8787`）。
- 浏览器端 `createApiClient({ baseUrl: "" })` 走同源，Cookie 会话可用；
  `SameSite=Strict` 不需要放宽。
- 本机访问时控制面按 loopback 直通为 admin，PC 不需要二次登录；远端部署由
  `ControlAuthGate` 在页面内提供控制面账号登录，不需要先跳到另一个界面。
  `apps/control-plane` 也使用同一个 gate，两端行为一致。
- PTY WebSocket 的地址显式注入（`ptyUrl`），避免误用 `window.location.host`；
  生产代理必须保留 `Host`/`Origin` 并支持 `Upgrade`。
- SSE 复用 `@lework/api` 的 URL 构造函数；断线按控制面语义做快照重拉（无重放）。

### 3.4 PC 信息架构

在 `apps/web` 左侧栏新增 OpenCode 分组：

- **工作区**（`/workspaces`）：OpenCode workspace 列表；详情包含
  “会话 / 文件 / 活动”三个页签，primary node 选择、创建会话、发送 prompt。
- **节点**（`/nodes`）：节点列表、认领码/配对、健康状态、OpenCode start/restart/stop、移除。
- **审批**（`/pending`）：跨会话待审批聚合，点击进入对应会话。

企业 `/v1` 页面保留为过渡；当控制面补齐等价能力后删除。旧“项目”入口不再作为
主入口，工作区成为“项目”的规范形态。

## 4. 移动端视觉迁移

- 主色：`#4f46e5`（Web primary）；强强调 `#0b57d0`；浅色选中 `#dce7ff` / `#e8efff`。
- 页面底色：`#f8fafc`；卡片 `#ffffff`；抽屉/导航使用 `#eef3ff` 浅蓝。
- 文本：`#111827` / `#263044` / `#6f778a` / `#8d94a6`。
- 圆角：8 / 12 / 16；阴影向 Web 的轻阴影靠拢（RN 用 elevation/shadow 近似）。
- 聊天：用户气泡改为浅蓝底 + 深色字；AI 气泡接近 Web 的浅色面板。
- 暗色：Web 没有完整暗色参考，移动端保留暗色，但把强调色改为靛蓝系并补测试。

## 5. Logo 与加载动画

- 从 `packages/app-ui/assets/logo.svg` 提取内嵌 PNG，生成：
  - App 图标 / Android adaptive icon / 启动图 / 应用内 logo / 通知小图标。
- `app.json` 增加 icon、splash、adaptiveIcon、通知颜色（`#4f46e5`）。
- 启动时用 splash 覆盖偏好加载，避免暗色设备闪白；加载完成或超时后隐藏。
- 新增移动端加载组件：`BrandSpinner`、`LoadingDots`（对齐 Web 三点动画）、
  `Skeleton`、`LoadingScreen`，替换散落的系统 `ActivityIndicator`。
- 会话等待态使用三点动画；列表/设置使用骨架或品牌 spinner。

## 6. 验证

- `pnpm typecheck`
- `pnpm --filter @lework/mobile test`
- `pnpm --filter @lework/design-tokens test`（token parity/对比度）
- `pnpm --filter @lework/api test`（新增认领/节点端点）
- `pnpm --filter @lework/control-ui test`（纯函数与关键组件）
- `pnpm --filter @lework/control-plane build`（静态导出仍成立）
- `pnpm --filter @lework/web build`
- 需要时补 Go 侧无回归：`go test ./backend/internal/controlplane/...`

## 7. 风险与缓解

| 风险 | 缓解 |
|---|---|
| 两套鉴权混用 | 控制台用独立 `ControlApiProvider`；不与 JWT 合流 |
| Next rewrite 不代理 WS | PTY 地址显式配置；生产 nginx 文档化 Upgrade |
| 组件抽取破坏控制面 | 先抽包，控制面改 import，构建验证后再接 `apps/web` |
| 旧项目/新工作区并存 | 新入口独立路由 `/workspaces`，旧路由暂留，删除 `/v1` 时一起清理 |
| Logo 资源过大 | 提取 PNG 后按尺寸生成，不直接引入 1.65MB SVG |
| 主题迁移导致对比度下降 | token 层 + 主题测试；关键色做 WCAG 断言 |
