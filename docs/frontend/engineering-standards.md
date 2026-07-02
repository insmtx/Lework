# 工程规范

本文档详细描述 Lework 前端的工程规范。

## Monorepo 命令

根目录 (`frontend/`) 通过 Turborepo 管理所有子包任务：

| 命令 | 作用 |
|------|------|
| `pnpm dev:web` | 启动 Web 开发服务器 (Next.js, 端口 3005) |
| `pnpm dev:desktop` | 启动 Desktop 开发 (Electron + Vite, 端口 5175) |
| `pnpm build` | Turborepo 构建所有包 (按依赖拓扑排序) |
| `pnpm typecheck` | Turborepo 类型检查所有包 |
| `pnpm test` | Turborepo 运行所有包测试 |
| `pnpm lint` | Turborepo 运行所有包 Biome 检查 |
| `pnpm clean` | 清理所有构建产物和 node_modules |
| `pnpm ui:add` | shadcn 添加 UI 组件到 @lework/ui |

### 子包单独命令

各子包也支持独立执行命令：

```bash
# Web 应用
cd apps/web && pnpm dev          # Next.js dev (端口 3005)
cd apps/web && pnpm build        # Next.js build
cd apps/web && pnpm typecheck    # tsc --noEmit
cd apps/web && pnpm test         # vitest run

# Desktop 应用
cd apps/desktop && pnpm dev      # electron-vite dev
cd apps/desktop && pnpm build    # electron-vite build + electron-builder
cd apps/desktop && pnpm preview  # electron-vite preview

# UI 包
cd packages/ui && pnpm typecheck # tsc --noEmit
cd packages/ui && pnpm lint      # biome check

# 应用级 UI 包
cd packages/app-ui && pnpm typecheck
cd packages/app-ui && pnpm lint

# Store 包
cd packages/store && pnpm typecheck
cd packages/store && pnpm lint
```

### Turborepo 任务配置

```json
// turbo.json
{
  "tasks": {
    "build": { "dependsOn": ["^build"], "outputs": [".next/**", "dist/**", "out/**"] },
    "dev": { "cache": false, "persistent": true },
    "typecheck": { "dependsOn": ["^typecheck"] },
    "test": { "dependsOn": ["^typecheck"] },
    "lint": {}
  }
}
```

- `build` 任务按依赖拓扑执行（UI/Store 先构建，再构建 Web/Desktop）
- `dev` 任务不缓存，支持持久运行
- `typecheck` / `test` 依赖上游类型检查完成

### 按需过滤执行

```bash
turbo build --filter=@lework/web       # 只构建 Web
turbo dev --filter=@lework/desktop     # 只启动 Desktop
turbo typecheck --filter=@lework/ui    # 只检查 UI 包
turbo typecheck --filter=@lework/app-ui # 只检查应用级 UI 包
```

## 路径别名与导入

### 各子包独立路径别名

| 子包 | 别名 | 映射 |
|------|------|------|
| `apps/web` | `@/*` | 当前目录 (Next.js 约定) |
| `apps/desktop` | `@/*` | `src/renderer/src/*` (electron-vite 配置) |

### Workspace 包导入

通过 pnpm workspace 引用共享包：

```ts
// 导入 UI 组件
import { Button } from "@lework/ui/components/ui/button";
import { cn } from "@lework/ui/lib/utils";
import { ThemeProvider } from "@lework/ui/components/common/theme-provider";

// 导入 Store
import { useAppStore, useChatStore } from "@lework/store";
import type { Message, ToolCall } from "@lework/store/types/chat";

// 导入应用级共享 UI
import { Shell } from "@lework/app-ui/components/layout/Shell";
import { ChatInput } from "@lework/app-ui/components/input/ChatInput";

// 导入 Hooks
import { useMobile } from "@lework/ui/hooks/use-mobile";
import { useSSE } from "@lework/ui/hooks/use-sse";
```

应用入口 CSS 通过样式包导入共享全局样式：

```css
@import "@lework/styles/globals.css";
```

### 共享包职责边界

| 包 | 职责 | 可依赖 |
|----|------|--------|
| `@lework/app-ui` | Lework 产品级业务组合组件，如 Shell、Chat、DigitalAssistant | `@lework/ui`, `@lework/store` |
| `@lework/styles` | 双端共享全局样式入口，集中维护 Tailwind/shadcn/token/base/app shell 样式和共享包 `@source` | `@lework/ui`, CSS 依赖 |
| `@lework/ui` | 基础 UI 原语、通用 hooks、lib 工具、设计系统样式 | 第三方 UI/工具库 |
| `@lework/store` | Zustand 状态、领域类型、API client、mock 数据 | `@lework/ui` 中的纯工具（如需要） |

新增双端共享业务组件时，优先放入 `packages/app-ui`。仅 Web 或 Desktop 独有的入口、路由、资源路径或平台能力适配，才放在对应 `apps/*` 目录。
新增跨端全局样式、设计 token 接入或共享包 Tailwind 扫描规则时，优先放入 `packages/styles/globals.css`，避免 Web 与 Desktop 各自维护一份 CSS。

### @lework/app-ui 导出路径

应用级 UI 包按组件目录暴露子路径：

```json
// packages/app-ui/package.json exports
{
  ".": "./index.ts",
  "./components/*": "./components/*.tsx"
}
```

推荐从明确子路径导入页面入口组件：

```ts
import { Shell } from "@lework/app-ui/components/layout/Shell";
```

### @lework/styles 导出路径

共享样式包只导出 CSS 入口，并显式提供 `style` condition，确保 Next.js/Turbopack 与 Vite 都能通过包名解析样式文件：

```json
// packages/styles/package.json exports
{
  "./globals.css": {
    "style": "./globals.css",
    "default": "./globals.css"
  }
}
```

应用入口 CSS 只负责导入共享样式和声明应用本地源码扫描：

```css
/* apps/web/app/globals.css */
@import "@lework/styles/globals.css";

@source "./**/*.{ts,tsx}";

/* apps/desktop/src/renderer/src/globals.css */
@import "@lework/styles/globals.css";

@source "./**/*.{ts,tsx}";
```

### @lework/ui 导出路径

UI 包使用细粒度 `exports` 映射，确保按需加载：

```json
// packages/ui/package.json exports
{
  "./lib/*": "./lib/*.ts",
  "./components/*": "./components/*.tsx",
  "./components/ui/*": "./components/ui/*.tsx",
  "./components/common/*": "./components/common/*.tsx",
  "./hooks/*": "./hooks/*.ts",
  "./styles/tokens.css": "./styles/tokens.css",
  "./styles/base.css": "./styles/base.css"
}
```

### @lework/store 导出路径

```json
// packages/store/package.json exports
{
  ".": "./index.ts",
  "./types/chat": "./types/chat.ts",
  "./types/layout": "./types/layout.ts",
  "./types/topic": "./types/topic.ts"
}
```

## TypeScript 配置

### 共享配置方案

通过 `@lework/tsconfig` 包提供三套共享配置：

| 配置 | 文件 | 适用范围 |
|------|------|----------|
| `base.json` | strict + ESNext + bundler | 所有子包基线 |
| `next.json` | 继承 base + Next.js 插件 | `apps/web` |
| `react-library.json` | 继承 base + react-jsx | `packages/app-ui`, `packages/ui`, `packages/store` |

### base.json 关键规则

```json
{
  "strict": true,
  "noUnusedLocals": true,
  "noUnusedParameters": true,
  "noImplicitReturns": true,
  "noUncheckedIndexedAccess": true,
  "moduleResolution": "bundler",
  "isolatedModules": true
}
```

### 各子包继承方式

```json
// apps/web/tsconfig.json
{ "extends": "@lework/tsconfig/next.json" }

// packages/ui/tsconfig.json
{ "extends": "@lework/tsconfig/react-library.json" }

// packages/app-ui/tsconfig.json
{ "extends": "@lework/tsconfig/react-library.json" }

// packages/store/tsconfig.json
{ "extends": "@lework/tsconfig/react-library.json" }
```

## Biome 规则

### 共享配置方案

通过 `@lework/biome` 包提供统一 lint + format 配置，根目录 `biome.json` 通过 `extends` 引用：

```json
// frontend/biome.json
{ "extends": ["@lework/biome/biome.json"] }
```

### @lework/biome 规则要点

| 类别 | 规则 | 设置 |
|------|------|------|
| Formatter | 缩进风格 | tab (2 width) |
| Formatter | 引号风格 | double |
| Formatter | 分号 | always |
| Formatter | 行宽 | 100 |
| Formatter | 尾逗号 | all |
| Linter | recommended | 启用 |
| Linter | noUnusedVariables | warn |
| Linter | noUnusedImports | warn |
| Linter | useHookAtTopLevel | error |
| Linter | noExplicitAny | off |
| Linter | noDefaultExport | off |
| CSS | tailwindDirectives | 启用 |

## 依赖版本管理

项目使用 `pnpm-workspace.yaml` 的 `catalog` 功能统一管理核心依赖版本：

```yaml
# pnpm-workspace.yaml
catalog:
  react: "19.2.3"
  react-dom: "19.2.3"
  typescript: "^5.9.3"
  tailwindcss: "^4"
  vitest: "^4.1.0"
```

各子包通过 `catalog:` 引用：

```json
// 子包 package.json
{
  "dependencies": {
    "react": "catalog:",
    "react-dom": "catalog:"
  },
  "devDependencies": {
    "typescript": "catalog:",
    "vitest": "catalog:"
  }
}
```

优势：
- 避免各包 React/TS 版本不一致
- 升级时只需修改 `catalog` 一处
- pnpm 自动解析为实际版本写入 lockfile

## pnpm 配置

```ini
# .npmrc
shamefully-hoist=true
strict-peer-dependencies=false
```

- `shamefully-hoist`：提升所有依赖到根 node_modules，确保 Next.js/Electron 工具链正常
- `strict-peer-dependencies=false`：workspace 包间 peer 依赖（如 React）允许隐式满足

## 样式体系

### TailwindCSS 4 配置

Web 应用使用 PostCSS 方式，Desktop 应用使用 Vite 插件方式（`@tailwindcss/vite`）。两端的全局样式入口保持一致，都导入 `@lework/styles/globals.css`：

```css
/* apps/web/app/globals.css */
@import "@lework/styles/globals.css";

@source "./**/*.{ts,tsx}";

/* apps/desktop/src/renderer/src/globals.css */
@import "@lework/styles/globals.css";

@source "./**/*.{ts,tsx}";
```

共享样式包负责导入 Tailwind、shadcn、动画样式和设计系统基础样式：

```css
/* packages/styles/globals.css */
@import "tailwindcss";
@import "tw-animate-css";
@import "shadcn/tailwind.css";
@import "@lework/ui/styles/tokens.css";
@import "@lework/ui/styles/base.css";

@source "../ui/**/*.tsx";
@source "../store/**/*.ts";
@source "../app-ui/**/*.tsx";
```

如果 `@lework/app-ui`、`@lework/ui` 或新的共享包中新增 Tailwind class，不需要再分别修改 Web/Desktop 的相对路径；只在 `packages/styles/globals.css` 中维护共享包扫描范围。

### 设计系统

| 元素 | Token | 用途 |
|------|-------|------|
| 背景 | `slate-50` | 中性灰页面背景 |
| 表面 | `white` / `tokens.css` | 内容卡片/面板 |
| 分隔 | `slate-200` | 低对比度分隔线 |
| 主操作 | `blue-500/600` | 发送按钮/选中态 |
| 文字主 | `slate-700` | 正文文字 |
| 文字次 | `slate-500` | 标签/辅助文字 |
| 文字弱 | `slate-400` | 占位符/时间 |

### 视觉规则

- UI 控件：无衬线字体，紧凑布局
- 叙述文本 (AI 回复)：衬线字体 (`font-serif`)，优先阅读体验
- 标签/分类：大写字母 + `tracking-wide` 字间距
- 动效：仅 `transition-colors/opacity`，无干扰性动画

## 添加 UI 组件

使用 shadcn CLI 向 `@lework/ui` 包添加新组件：

```bash
pnpm ui:add
# 选择需要的组件
```

新增组件后，需在 `packages/ui/package.json` 的 `exports` 中添加导出路径：

```json
// packages/ui/package.json
{
  "exports": {
    "./components/ui/new-component": "./components/ui/new-component.tsx"
  }
}
```

## 从单项目到 Monorepo 的变更

| 变更项 | 旧值 | 新值 |
|--------|------|------|
| 包管理器 | Bun / npm | pnpm@10 |
| 锁文件 | `bun.lockb` / `package-lock.json` | `pnpm-lock.yaml` |
| 构建编排 | 单项目 Vite | Turborepo 多包拓扑 |
| 格式化缩进 | 空格 (2) | tab (2 width) |
| 引号风格 | 单引号 | 双引号 |
| TS 配置 | 单 `tsconfig.json` | `@lework/tsconfig` 共享三套 |
| Biome 配置 | 单 `biome.json` | `@lework/biome` 共享配置 |
| 依赖版本 | 各包独立 | `catalog` 统一 |
