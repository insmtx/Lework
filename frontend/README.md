# Leros Frontend

Leros 前端工程 — 基于 pnpm + Turborepo 的 monorepo 架构，包含 Web 应用和 Electron 桌面应用，共享应用级 UI、基础 UI 组件库、全局样式与状态管理包。

## 项目结构

```
frontend/
├── apps/                        # 应用入口
│   ├── web/                     # Web 应用（Next.js 16）
│   │   ├── app/                 # App Router 页面
│   │   │   ├── layout.tsx       # RootLayout (ThemeProvider + Toaster)
│   │   │   ├── page.tsx         # 根路径重定向到 /workbench
│   │   │   ├── (shell)/         # 应用壳路由组（LeftRail + 页面内容）
│   │   │   │   ├── layout.tsx   # 路由页面共享 Shell
│   │   │   │   ├── workbench/   # /workbench
│   │   │   │   ├── chat/        # /chat
│   │   │   │   ├── projects/    # /projects/[projectId] 及子路由
│   │   │   │   ├── assistants/  # /assistants
│   │   │   │   ├── tasks/       # /tasks
│   │   │   │   ├── skills/      # /skills
│   │   │   │   ├── knowledge/   # /knowledge
│   │   │   │   └── settings/    # /settings
│   │   │   └── globals.css      # 样式入口（导入 @leros/styles + app 本地 @source）
│   │   ├── components/          # Web 专属组件/平台适配（路由适配、页面桥接）
│   │   │   ├── LerosShell.tsx   # Next Router -> AppNavigation 适配
│   │   │   └── route-pages.tsx  # Web 路由页到共享 app-ui 页面的桥接
│   │   ├── public/              # 静态资源
│   │   ├── next.config.ts       # Next.js 配置 (transpilePackages)
│   │   ├── postcss.config.mjs   # PostCSS 配置
│   │   └── tsconfig.json        # TypeScript 配置
│   │
│   └── desktop/                 # 桌面应用（Electron + Vite）
│   │   ├── src/
│   │   │   ├── main/            # Electron 主进程
│   │   │   ├── preload/         # Preload 脚本
│   │   │   └── renderer/        # 渲染进程（React SPA）
│   │   │       ├── src/
│   │   │       │   ├── App.tsx  # 根组件 (BrowserRouter + ThemeProvider)
│   │   │       │   ├── routes.tsx  # React Router 路由定义 + Desktop 导航适配
│   │   │       │   ├── main.tsx    # 渲染入口
│   │   │       │   ├── globals.css # 样式入口（导入 @leros/styles + app 本地 @source）
│   │   │       │   ├── components/ # Desktop 专属组件/平台适配
│   │   │       │   └── platform/   # 桌面端平台适配（预留）
│   │   │       └── index.html     # 渲染进程 HTML
│   │   ├── resources/           # 打包资源
│   │   ├── electron.vite.config.ts  # Electron-Vite 配置
│   │   ├── electron-builder.yml     # 打包配置 (mac/win/linux)
│   │   └── tsconfig.web.json        # 渲染进程 TS 配置
│   │   └── tsconfig.node.json       # 主进程 TS 配置
│   │
├── packages/                    # 共享包
│   ├── ui/                      # @leros/ui — UI 组件库
│   │   ├── components/
│   │   │   ├── ui/              # 54 个基础 UI 原语组件 (button, dialog, etc.)
│   │   │   └── common/          # 通用业务组件 (theme-provider)
│   │   ├── hooks/               # 共享 Hooks (use-mobile, use-sse, use-websocket)
│   │   ├── lib/                 # 工具库 (request, sse, websocket, utils)
│   │   ├── styles/              # 设计系统样式 (tokens.css, base.css)
│   │   ├── components.json      # shadcn 配置
│   │   └── package.json         # 导出路径映射
│   │
│   ├── app-ui/                  # @leros/app-ui — 双端共享应用级业务 UI
│   │   ├── components/
│   │   │   ├── chat/            # 聊天消息、时间轴、欢迎页、工具调用块
│   │   │   ├── input/           # ChatInput
│   │   │   ├── layout/          # Shell, LeftRail, 页面级布局组件和 AppNavigation 类型
│   │   │   └── digitalAssistant/# DigitalAssistant 列表、详情、弹窗
│   │   ├── index.ts             # 统一导出应用级组件
│   │   └── package.json         # 子路径 exports
│   │
│   ├── styles/                  # @leros/styles — 双端共享全局样式入口
│   │   ├── globals.css          # Tailwind/shadcn/tokens/base + shared @source + app shell styles
│   │   └── package.json         # CSS package exports
│   │
│   ├── store/                   # @leros/store — Zustand 状态管理
│   │   ├── appStore.ts          # Store 入口 (合并 layoutSlice + topicSlice + chatSlice)
│   │   ├── slices/              # 状态切片
│   │   │   ├── layoutSlice.ts   # 布局状态 (工作区、导航、会话列开关)
│   │   │   ├── topicSlice.ts    # Topic CRUD (乐观更新)
│   │   │   └── chatSlice.ts     # 聊天核心 (消息流、工具调用、输入状态)
│   │   ├── types/               # 领域类型 (api.ts, chat.ts)
│   │   ├── mocks/               # Mock 数据 (chatMocks, streamSimulator)
│   │   ├── utils/               # 工具函数 (flattenActions, format)
│   │   └── package.json         # 导出路径映射
│   │
│   ├── tsconfig/                # @leros/tsconfig — 共享 TS 配置
│   │   ├── base.json            # 基础配置 (strict, ESNext, bundler)
│   │   ├── next.json            # Next.js 配置 (继承 base)
│   │   └── react-library.json   # React 库配置 (继承 base)
│   │
│   ├── biome/                   # @leros/biome — 共享 Biome lint 配置
│   │   └── biome.json           # 规则定义 (recommended + 自定义)
│   │
├── pnpm-workspace.yaml          # 工作空间定义 + catalog 依赖版本管理
├── turbo.json                   # Turborepo 任务配置 (build/dev/typecheck/test/lint)
├── package.json                 # 根 package.json (monorepo 脚本)
├── biome.json                   # Biome 入口 (extends @leros/biome)
├── .npmrc                       # pnpm 配置 (shamefully-hoist)
└── .gitignore                   # Git 忽略规则
```

## 包说明

| 包名 | 位置 | 技术栈 | 用途 |
|------|------|--------|------|
| `@leros/web` | `apps/web` | Next.js 16 + React 19 + TailwindCSS 4 | Web 端应用 |
| `@leros/desktop` | `apps/desktop` | Electron 39 + React 19 + Vite 8 | 桌面端应用 |
| `@leros/app-ui` | `packages/app-ui` | React 19 + TailwindCSS 4 | 双端共享应用级业务 UI |
| `@leros/styles` | `packages/styles` | TailwindCSS 4 + shadcn/tailwind.css | 双端共享全局样式入口 |
| `@leros/ui` | `packages/ui` | React 19 + @base-ui/react + CVA + TailwindCSS 4 | 共享 UI 组件库 |
| `@leros/store` | `packages/store` | Zustand 5 + React 19 | 共享状态管理 |
| `@leros/tsconfig` | `packages/tsconfig` | — | 共享 TypeScript 配置 |
| `@leros/biome` | `packages/biome` | Biome 2 | 共享代码检查配置 |

## 本地开发

### 前置要求

- **Node.js** >= 20
- **pnpm** >= 10（项目锁定 `pnpm@10.28.2`）

### 安装依赖

```bash
cd frontend
pnpm install
```

### 启动开发

默认 API 地址为 `http://localhost:8080/v1`。如需连接服务器接口，不要修改源码里的
`packages/store/api/config.ts`，改用应用目录下的 `.env.local`：

```bash
# Web 应用
cp apps/web/.env.example apps/web/.env.local
# 然后设置：
# NEXT_PUBLIC_LEROS_API_BASE_URL=http://192.144.198.60:8080/v1

# Desktop 应用
cp apps/desktop/.env.example apps/desktop/.env.local
# 然后设置：
# VITE_LEROS_API_BASE_URL=http://192.144.198.60:8080/v1
```

```bash
# Web 应用 (端口 3005)
pnpm dev:web

# 桌面应用 (Electron)
pnpm dev:desktop

# 同时启动所有
pnpm dev
```

### 构建

```bash
# 构建所有包
pnpm build

# 构建指定应用
turbo build --filter=@leros/web
turbo build --filter=@leros/desktop
```

### 其他命令

```bash
# 类型检查
pnpm typecheck

# 运行测试
pnpm test

# 代码检查
pnpm lint

# 清理构建产物和依赖
pnpm clean
```

### 添加 UI 组件

使用 shadcn CLI 向 `@leros/ui` 包添加新组件：

```bash
pnpm ui:add
# 然后在交互界面中选择要添加的组件
```

新增组件后，需在 `packages/ui/package.json` 的 `exports` 中添加导出路径，以便其他包可通过 `@leros/ui/components/ui/<name>` 引入。

## 依赖版本管理

项目使用 `pnpm-workspace.yaml` 的 `catalog` 功能统一管理核心依赖版本：

- React / React-DOM: `19.2.3`
- TypeScript: `^5.9.3`
- TailwindCSS: `^4`
- Vitest: `^4.1.0`

各子包通过 `catalog:` 引用统一版本，避免版本不一致问题。

## 架构说明

### 双端共享策略

Web 和 Desktop 应用共享同一套应用级业务 UI（`@leros/app-ui`）、全局样式入口（`@leros/styles`）、基础 UI 组件库（`@leros/ui`）和状态管理（`@leros/store`），但使用不同的框架和路由：

| 维度 | Web (Next.js) | Desktop (Electron) |
|------|---------------|---------------------|
| 渲染 | SSR + CSR | CSR (本地渲染) |
| 路由 | App Router (Next.js) | BrowserRouter (react-router-dom) |
| 样式 | PostCSS + TailwindCSS | Vite + @tailwindcss/vite |
| 端口 | 3005 | 5175 (renderer) |
| 构建输出 | `.next/` | `out/` + `build/` |

### 路由约定

Web 和 Desktop 使用同一套路由语义。Web 通过 Next.js App Router 暴露真实 URL；Desktop 虽然没有可见地址栏，但仍通过 React Router 维护内部 location、history 和页面参数。路由是“当前页面位置”的来源，Zustand 负责数据缓存、加载状态、侧栏折叠、输入状态等应用状态。

| 路由 | 页面 | 说明 |
|------|------|------|
| `/` | Redirect | Web 和 Desktop 均跳转到 `/workbench` |
| `/workbench` | Workbench | 工作台首页 |
| `/chat` | Chat | 通用会话页 |
| `/projects/:projectId` | Project | 项目会话 tab |
| `/projects/:projectId/tasks` | Project | 项目任务 tab |
| `/projects/:projectId/files` | Project | 项目文件 tab |
| `/projects/:projectId/tasks/:taskId?sessionId=xxx` | TaskDetail | 任务详情和任务会话 |
| `/assistants` | Assistants | AI 队友列表 |
| `/tasks` | Placeholder | 全局任务页占位 |
| `/skills` | Placeholder | 技能页占位 |
| `/knowledge` | Placeholder | 知识库页占位 |
| `/settings` | Placeholder | 设置页占位 |

新增页面时应先确定路由语义，再分别补 Web 的 `apps/web/app/(shell)/...` 页面文件和 Desktop 的 `apps/desktop/src/renderer/src/routes.tsx` 路由项。共享页面组件优先放在 `packages/app-ui`，平台路由适配留在对应 `apps/*` 目录。

#### AppNavigation 适配

`packages/app-ui` 不直接依赖 Next Router 或 React Router。跨端导航通过 `AppNavigation` 注入：

```ts
type AppNavigation = {
  currentPath: string;
  goToRoute(route: ViewMode): void;
  goToProject(projectId: string): void;
  goToTaskDetail(projectId: string, taskId: string, sessionId?: string | null): void;
};
```

- Web 适配位于 `apps/web/components/LerosShell.tsx`，使用 `next/navigation`。
- Desktop 适配位于 `apps/desktop/src/renderer/src/routes.tsx`，使用 `react-router-dom`。
- `Shell` 只负责应用壳布局；页面渲染由路由决定。保留的 `currentView` 等 store 字段仅用于过渡兼容，不应作为新增页面的主要导航入口。

### 共享包职责边界

- `@leros/app-ui`：面向 Leros 产品的业务组合组件，例如 `Shell`、`LeftRail`、`ChatInput`、消息时间轴和 DigitalAssistant 面板。该包可以依赖 `@leros/ui` 和 `@leros/store`，但不直接依赖 Web/Desktop 的路由库。
- `@leros/styles`：双端共享全局 CSS 入口，集中维护 TailwindCSS、shadcn 样式、设计 token、基础样式、应用壳样式和 workspace 共享包的 `@source`。
- `@leros/ui`：基础 UI 原语、通用 hooks、请求/SSE/WebSocket 工具和设计系统样式，不依赖应用业务状态。
- `@leros/store`：跨端共享状态、领域类型、API client 与 mock 数据。避免把可视组件放入 store，也避免把新增页面位置只存为全局 view 状态。

新增双端共享业务组件时，应优先放入 `packages/app-ui`；只有 Web 或 Desktop 特有的平台适配才放在对应 `apps/*/components` 下。

### Next.js 转译配置

Web 应用需要显式配置 `transpilePackages` 以正确引用 workspace 包：

```ts
// apps/web/next.config.ts
const nextConfig: NextConfig = {
  transpilePackages: ["@leros/ui", "@leros/store", "@leros/app-ui"],
};
```

`@leros/styles` 是 CSS-only 包，通过 `exports["./globals.css"].style` 暴露样式入口，不需要加入 `transpilePackages`。

### TailwindCSS 扫描共享包

TailwindCSS 4 使用 CSS `@source` 声明扫描 class 来源。共享包扫描由 `@leros/styles` 集中维护，两个应用只保留最薄的样式入口：

```css
/* apps/web/app/globals.css */
@import "@leros/styles/globals.css";

@source "./**/*.{ts,tsx}";

/* apps/desktop/src/renderer/src/globals.css */
@import "@leros/styles/globals.css";

@source "./**/*.{ts,tsx}";
```

`packages/styles/globals.css` 内部负责导入 `tailwindcss`、`tw-animate-css`、`shadcn/tailwind.css`、`@leros/ui` 的 token/base 样式，并扫描 `packages/ui`、`packages/store`、`packages/app-ui`。如果新增了需要 Tailwind 扫描的共享包，应优先在这里补充 `@source`。

### Electron-Vite 配置

Desktop 应用使用 `electron-vite` 构建，renderer 进程需要 `dedupe` 配置避免 React 双实例：

```ts
// apps/desktop/electron.vite.config.ts
renderer: {
  resolve: {
    dedupe: ["react", "react-dom"],
  },
}
```
