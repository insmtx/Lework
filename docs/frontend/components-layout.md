# 组件与布局架构

本文档详细描述 Lework 前端的组件和布局架构。

## 布局层架构

### Shell 三栏架构 (AI 聊天交互)

```
┌─────────────┬─────────────────────┬──────────────┐
│  LeftRail   │   CenterCanvas      │  RightRail   │
│  (260px)    │   (flex-1)          │  (280px)     │
│             │                     │              │
│  会话列表    │  标题栏              │  快捷操作     │
│  工作区分组  │  消息时间轴          │  文件收件箱   │
│             │  输入框 + 模型选择    │  工件文件     │
└─────────────┴─────────────────────┴──────────────┘
```

**Shell** 使用 Flexbox 三栏布局 (`flex h-screen w-screen overflow-hidden bg-slate-50`)，各栏通过 `border-r/border-l border-slate-200` 分隔。

### LeftRail — 会话导航区

- 工作区（远程/本地）分组，可折叠
- 会话列表，支持创建/删除/切换
- 底部：新建工作区按钮

### CenterCanvas — 聊天交互区

- 标题栏 (当前会话名称)
- 消息时间轴 (用户气泡白色 / AI 回复衬线字体)
- 浮动输入框 (textarea + @提及 + 附件 + 模型选择 + 发送/停止)

### RightRail — 信息区

- 三个 Tab：快捷操作 / 文件收件箱 / 工件文件
- Tab 切换通过 Zustand `activeRightTab` 状态控制

### BasicLayout — 业务页面主布局

- Sidebar：侧边栏导航 (menus.ts 菜单配置 + pluginStore 动态菜单)
- Header：顶部栏 (用户信息 · 通知 · 任务指示器)
- PageTabs：多标签页切换 (tabsStore)
- Content：RouterOutlet (各业务页面)
- AiFloat：全局 AI 浮动助手
- 强制改密码弹窗 (userStore.mustChangePassword)

### BlankLayout — 空白布局

- 登录页、原型编辑器等独立全屏页面

## 布局组件目录

```
packages/app-ui/components/layout/
├── Shell.tsx           # 双端共享三栏布局容器
├── LeftRail.tsx        # 左栏 - 工作台/任务/技能/知识库/AI队友导航
├── CenterCanvas.tsx    # 中栏 - 聊天交互区
├── ConversationListPanel.tsx # 会话列表面板
├── RightRail.tsx       # 右栏 - 快捷/收件/工件
├── WorkbenchPanel.tsx  # 工作台占位/入口面板
└── index.ts            # barrel 导出
```

`Shell`、`LeftRail`、`CenterCanvas`、`ChatInput`、消息时间轴和 DigitalAssistant 相关组件位于 `@lework/app-ui`，由 Web 与 Desktop 共同复用。应用目录只保留入口、路由、薄样式入口和必要的平台适配；跨端全局样式统一维护在 `@lework/styles`。

## UI 原语组件层 (`components/ui/`)

基于 **@base-ui/react** 无样式原语 + **CVA (class-variance-authority)** 变体系统：

```
@base-ui/react (无样式行为原语)
    ↓ 包装
UI 组件 (CVA 变体 + TailwindCSS 样式)
    ↓ 使用
layout 组件 / 业务组件
```

### Button 变体示例

```ts
const buttonVariants = cva('基础样式', {
  variants: {
    variant: ['default', 'outline', 'secondary', 'ghost', 'destructive', 'link'],
    size: ['default', 'xs', 'sm', 'lg', 'icon', 'icon-xs', 'icon-sm', 'icon-lg'],
  },
});
```

### 已实现 UI 组件清单 (56个)

accordion, alert, alert-dialog, aspect-ratio, avatar, badge, breadcrumb, button, button-group, calendar, card, carousel, chart, checkbox, collapsible, combobox, command, context-menu, dialog, drawer, dropdown-menu, empty, field, form, hover-card, input, input-group, input-otp, item, kbd, label, menubar, navigation-menu, pagination, popover, progress, radio-group, resizable, scroll-area, select, separator, sheet, sidebar, skeleton, slider, sonner, spinner, switch, table, tabs, textarea, toggle, toggle-group, tooltip

## 应用级业务组件库 (`packages/app-ui/components/`)

`@lework/app-ui` 基于 `@lework/ui` 原语和 `@lework/store` 状态组合 Lework 产品级业务 UI：

```
packages/app-ui/components/
├── chat/              # AI/User 消息气泡、时间轴、工具调用、欢迎页
├── input/             # ChatInput
├── layout/            # Shell、LeftRail、CenterCanvas、WorkbenchPanel
└── digitalAssistant/  # Assistant 列表、卡片、详情、创建/编辑/删除弹窗
```

新增双端共享业务组件时，优先放入 `packages/app-ui/components`。只有平台专属能力（如 Electron 资源路径、Web 专属路由入口）才放入 `apps/web` 或 `apps/desktop`。

## 全局样式包 (`packages/styles/`)

`@lework/styles` 是 Web 与 Desktop 的共享 CSS 入口，用于集中维护跨端一致的全局样式：

```
packages/styles/
├── globals.css   # Tailwind/shadcn/token/base + shared @source + app shell styles
└── package.json  # 导出 ./globals.css
```

两个应用的入口 CSS 只导入共享样式，并声明自身本地源码扫描：

```css
@import "@lework/styles/globals.css";

@source "./**/*.{ts,tsx}";
```

`packages/styles/globals.css` 内部负责扫描 `packages/ui`、`packages/store`、`packages/app-ui`。新增共享组件或全局 class 时，优先更新该文件，避免 Web/Desktop 产生重复 CSS 配置。

## AI 浮动助手组件 (`components/ai-float/`)

全局侧边 AI 助手组件：

```
components/ai-float/
├── AiFloat.tsx      # 全局侧边 AI 助手组件
├── AiFloatPrompt.tsx # Prompt 传递
└── index.ts
```

## 依赖关系图

```
apps/web/app/page.tsx
  └─ @lework/app-ui Shell
      ├─ LeftRail
      ├─ CenterCanvas
      │   ├─ ChatHeader
      │   ├─ MessageTimeline
      │   └─ ChatInput
      └─ WorkbenchPanel / AssistantListView

apps/desktop/src/renderer/src/routes.tsx
  └─ @lework/app-ui Shell
      └─ 同一套 layout/chat/input/digitalAssistant 组件

@lework/app-ui
  └─ @lework/store (状态)
  └─ @lework/ui (基础 UI 原语)

apps/web/app/globals.css
apps/desktop/src/renderer/src/globals.css
  └─ @lework/styles/globals.css
      ├─ @lework/ui/styles/tokens.css
      ├─ @lework/ui/styles/base.css
      └─ Tailwind @source: ui / store / app-ui
```
