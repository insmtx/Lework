# Lework 前端架构文档

本文档是 Lework 前端架构的主索引文档，详细文档请查阅子文档。

## 技术栈概览

| 类别 | 技术 | 版本 |
|------|------|------|
| 框架 | React | 19.2.3 |
| 语言 | TypeScript | ^5.9.3 |
| 包管理 | pnpm + Turborepo | pnpm@10 / turbo@2.5 |
| Web 构建 | Next.js 16 + TailwindCSS 4 | App Router + PostCSS |
| Desktop 构建 | Electron 39 + electron-vite + Vite 8 | CSR + @tailwindcss/vite |
| 状态管理 | Zustand 5 (traditional + middleware) | subscribeWithSelector + devtools |
| UI 基础 | @base-ui/react (无样式原语) | v1.x |
| 变体系统 | class-variance-authority (CVA) | v0.7 |
| CSS 合并 | clsx + tailwind-merge | cn() 工具函数 |
| 代码检查 | Biome 2 (替代 ESLint + Prettier) | formatter + linter (共享配置) |
| 测试 | Vitest 4 | --coverage 支持 |
| 依赖版本 | pnpm catalog | 统一 React/TS/Tailwind 版本 |

## 文档索引

| 文档 | 说明 |
|------|------|
| [状态管理架构](./state-management.md) | Zustand Slice 模式 + ActionImpl 类架构详解 |
| [通信层架构](./communication.md) | HTTP、SSE、WebSocket 通信层实现 |
| [核心机制详解](./core-mechanisms.md) | 路由鉴权、AI交互、CRUD模式、插件化、通知、权限模型 |
| [架构设计模式](./design-patterns.md) | Slice 模式、ActionImpl、Reducer、乐观更新、原语包装、连接层抽象 |
| [组件与布局架构](./components-layout.md) | Shell 三栏架构、BasicLayout、BlankLayout、UI 原语、业务组件库 |
| [工程规范](./engineering-standards.md) | NPM Scripts、路径别名、TypeScript/Biome 配置、样式体系 |
| [待完成事项](./todo.md) | 通信层、布局层、路由层、组件/Hooks、页面模块、API层待完成清单 |

## 项目结构概览

```
frontend/
├── apps/                          # 应用入口
│   ├── web/                       # @lework/web — Next.js Web 应用
│   │   ├── app/                   # App Router 页面 (layout + page + globals.css)
│   │   ├── components/            # Web 专属组件/平台适配
│   │   ├── next.config.ts         # transpilePackages 配置
│   │   └── tsconfig.json
│   │
│   └── desktop/                   # @lework/desktop — Electron 桌面应用
│   │   ├── src/
│   │   │   ├── main/              # Electron 主进程
│   │   │   ├── preload/           # Preload 脚本
│   │   │   └── renderer/          # React 渲染进程 (BrowserRouter + routes)
│   │   ├── electron.vite.config.ts
│   │   ├── electron-builder.yml   # 打包配置 (dmg/nsis/AppImage)
│   │   └── tsconfig.web.json / tsconfig.node.json
│   │
├── packages/                      # 共享包
│   ├── ui/                        # @lework/ui — UI 组件库 + Hooks + 工具库
│   │   ├── components/            # ui/ (54 原语) + common/ (theme-provider)
│   │   ├── hooks/                 # use-mobile, use-sse, use-websocket
│   │   ├── lib/                   # request, sse, websocket, utils
│   │   ├── styles/                # tokens.css, base.css (设计系统)
│   │   └── package.json           # 细粒度 exports 路径映射
│   │
│   ├── app-ui/                    # @lework/app-ui — 双端共享应用级业务 UI
│   │   ├── components/            # chat / input / layout / digitalAssistant
│   │   ├── index.ts               # 统一导出应用级组件
│   │   └── package.json           # 子路径 exports
│   │
│   ├── styles/                    # @lework/styles — 双端共享全局样式入口
│   │   ├── globals.css            # Tailwind/shadcn/token/base + shared @source + app shell styles
│   │   └── package.json           # CSS package exports
│   │
│   ├── store/                     # @lework/store — Zustand 状态管理
│   │   ├── appStore.ts            # 合并 layoutSlice + topicSlice + chatSlice
│   │   ├── slices/                # layout / topic / chat 状态切片
│   │   ├── types/                 # api.ts, chat.ts 领域类型
│   │   ├── mocks/                 # chatMocks, streamSimulator
│   │   ├── utils/                 # flattenActions, format
│   │   └── package.json           # 导出路径映射 (含子路径类型导出)
│   │
│   ├── tsconfig/                  # @lework/tsconfig — 共享 TS 配置
│   │   ├── base.json              # strict, ESNext, bundler
│   │   ├── next.json              # Next.js 专用
│   │   └── react-library.json     # React 库专用
│   │
│   ├── biome/                     # @lework/biome — 共享 lint 配置
│   │   └── biome.json             # recommended + 自定义规则
│   │
├── pnpm-workspace.yaml            # 工作空间 + catalog 版本锁定
├── turbo.json                     # Turborepo 任务 (build/dev/typecheck/test/lint)
├── package.json                   # monorepo 根脚本
├── biome.json                     # extends @lework/biome
├── .npmrc                         # shamefully-hoist + no-strict-peer
└── .gitignore
```

## 架构分层概览

```
┌──────────────────────────────────────────────────────────┐
│                Apps (双端应用)                             │
│  @lework/web (Next.js)    @lework/desktop (Electron) │
├──────────────────────────────────────────────────────────┤
│              Packages (共享层)                              │
│  @lework/app-ui (应用级业务UI)    @lework/store (状态) │
│  @lework/styles (共享全局样式入口)                           │
│  @lework/ui (基础UI+Hooks+工具库)                           │
│  @lework/tsconfig (TS配置)  @lework/biome (Lint配置)   │
├──────────────────────────────────────────────────────────┤
│               App 入口 (平台差异)                           │
│  Next.js App Router · Electron BrowserRouter · 资源路径适配 │
├──────────────────────────────────────────────────────────┤
│               Store (Zustand Slice 模式)                  │
│  layout · topic · chat  (合并为 AppStore)                │
├──────────────────────────────────────────────────────────┤
│               UI 原语 (54个基础组件)                       │
│  button · dialog · sheet · tabs · command · etc.         │
├──────────────────────────────────────────────────────────┤
│               Hooks + Lib (共享逻辑)                       │
│  use-mobile · use-sse · use-websocket                    │
│  request · sse · websocket · utils                       │
├──────────────────────────────────────────────────────────┤
│         @base-ui/react + CVA + TailwindCSS 4              │
│  cn() 工具函数  ·  components.json (shadcn)              │
└──────────────────────────────────────────────────────────┘
```

## 双端架构差异

| 维度 | Web (@lework/web) | Desktop (@lework/desktop) |
|------|---------------------|----------------------------|
| 框架 | Next.js 16 (App Router) | Electron 39 + React SPA |
| 路由 | `app/` 目录约定 | react-router-dom BrowserRouter |
| 入口 | `layout.tsx` + `page.tsx` | `App.tsx` + `routes.tsx` |
| 构建 | Next.js build (SSR+CSR) | electron-vite build (CSR) |
| 样式引擎 | @tailwindcss/postcss | @tailwindcss/vite |
| 开发端口 | 3005 | 5175 (renderer) |
| 构建产物 | `.next/` | `out/` + `build/` |
| 打包 | 无 | electron-builder (dmg/nsis/AppImage) |

### Next.js 转译配置

Web 应用需显式配置 `transpilePackages` 以正确引用 workspace 包：

```ts
// apps/web/next.config.ts
const nextConfig: NextConfig = {
  transpilePackages: ["@lework/ui", "@lework/store", "@lework/app-ui"],
};
```

`@lework/styles` 是 CSS-only 包，通过 CSS import 和 package `exports` 的 `style` condition 解析，不需要加入 `transpilePackages`。

### 应用级 UI 共享包

`@lework/app-ui` 承载 Web 与 Desktop 共用的业务组合组件：

- `components/layout`：`Shell`、`LeftRail`、`CenterCanvas`、`WorkbenchPanel`
- `components/chat`：消息气泡、时间轴、欢迎页、工具调用展示
- `components/input`：`ChatInput`
- `components/digitalAssistant`：列表、详情、创建/编辑/删除弹窗

该包位于 `packages/app-ui`，依赖 `@lework/ui` 与 `@lework/store`。应用入口只负责路由、主题、平台资源注入和运行时差异；双端复用的业务 UI 不应再在 `apps/web` 与 `apps/desktop` 中重复实现。

### 全局样式共享包

`@lework/styles` 承载 Web 与 Desktop 共用的全局 CSS 入口：

- 导入 TailwindCSS 4、`tw-animate-css`、`shadcn/tailwind.css`
- 导入 `@lework/ui/styles/tokens.css` 与 `@lework/ui/styles/base.css`
- 集中维护 `packages/ui`、`packages/store`、`packages/app-ui` 的 Tailwind `@source`
- 承载 Lework app shell、侧边栏、聊天区域等跨端一致的全局 class

两个应用的 `globals.css` 只保留薄入口：

```css
@import "@lework/styles/globals.css";

@source "./**/*.{ts,tsx}";
```

新增跨端共享样式或共享包扫描范围时，应修改 `packages/styles/globals.css`，避免 Web 和 Desktop 各自维护相对路径。

### Electron-Vite React 去重

Desktop 渲染进程需 `dedupe` 配置避免 React 双实例：

```ts
// apps/desktop/electron.vite.config.ts
renderer: {
  resolve: {
    dedupe: ["react", "react-dom"],
  },
}
```

## 快速导航

### Web 入口层

`app/layout.tsx` (RootLayout) → `app/page.tsx` (→ `@lework/app-ui` Shell)

- ThemeProvider + Toaster 包裹全局
- Next.js App Router 自动处理路由

### Desktop 入口层

`main/index.ts` (Electron 主进程) → `preload/index.ts` → `renderer/src/main.tsx` → `App.tsx` → `routes.tsx` (→ `@lework/app-ui` Shell)

- BrowserRouter + ThemeProvider + Toaster 包裹渲染进程
- Electron 主进程通过 `electron-vite` 管理

### 页面模块分类

| 分类 | 模块 |
|------|------|
| 核心 | Shell (布局), ChatHeader, MessageTimeline, ChatInput |
| 布局 | LeftRail, ConversationListPanel, CenterCanvas, TopBar |
| 组件 | UserMessageBubble, AIMessageBubble, ToolCallBlock, TypingIndicator |

## 相关文档

- [前端 Monorepo README](../../frontend/README.md)
- [布局风格设计](./orbita-layout.md)
