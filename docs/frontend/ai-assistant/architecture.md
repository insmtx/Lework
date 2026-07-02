# AI 助手模块 — 架构规划

## 规划概览

**目标**：逐步构建 Lework 的 AI 助手（聊天界面），当前阶段专注于：
- 聊天交互核心功能（流式对话、工具调用展示）
- 纯前端 Mock 数据驱动
- 保持现有技术栈不变

**技术栈**：Vite + SWC / React 19 / Tailwind CSS 4 / @base-ui/react / @tabler/icons-react / Zustand 5 / SSE & WebSocket

**现状保留与扩展**：
- `Shell` 两栏+会话列布局（LeftRail / ConversationListPanel / CenterCanvas）
- `layoutSlice`：工作区、会话列表、会话列开关状态
- `topicSlice`：Topic CRUD（乐观更新模式）
- 通信基础设施：`http`, `SSEClient`, `WSClient`

**扩展方向**：
- 新增 `chatSlice` 接管聊天核心状态
- 重构 `CenterCanvas` 为消息时间轴 + 输入框组合
- 新增 `TopBar` 组件承载全局状态与导航
- 新增 `ChatHeader` 承载会话级操作

## 视觉设计 Token

### 色彩系统
```
Background:    slate-50  (#f8fafc) — 全局画布背景
Surface:       white     (#ffffff) — 卡片、面板、消息气泡
Border:        slate-200 (#e2e8f0) — 分割线、边框
Text Primary:  slate-900 (#0f172a) — 标题、正文
Text Secondary:slate-500 (#64748b) — 次要文本、标签
Text Muted:    slate-400 (#94a3b8) — 占位符、时间戳
Accent:        blue-500  (#3b82f6) — 主按钮、选中态、链接
Accent Light:  blue-50   (#eff6ff) — 选中背景
User Message:  blue-600  (#2563eb) — 用户消息背景
Success:       green-500 (#22c55e) — AI 在线状态、成功态
```

### 字体层级
```
UI 控件:   无衬线, text-xs/sm, font-medium, tracking-wide, uppercase
叙事文本:  无衬线, text-sm, font-serif (AI 回复正文)
标签:      text-xs, uppercase, tracking-wider, text-slate-500
```

### 尺寸规范
```
TopBar 高度:      48px
LeftRail 宽度:    260px (可折叠至 52px)
ConversationListPanel 宽度: 260px (可切换显示/隐藏)
CenterCanvas 最大内容宽度:  720px (消息区域居中)
ChatInput 最大宽度: 800px
消息气泡圆角:     12px (用户) / 8px (AI)
```

## 页面架构

```
App
└── Shell (h-screen, flex, overflow-hidden)
    ├── TopBar (48px, flex, border-b)
    │   ├── Logo "Lework" + 版本号
    │   └── 右侧：AI 状态指示器 / 通知 / 用户头像菜单
    ├── MainArea (flex-1, flex, overflow-hidden)
    │   ├── LeftRail (260px, 可折叠)
    │   │   └── 导航分组（AI 助手、工作台、AI 能力...）
    │   │   └── 点击「AI 助手」展开/收起 ConversationListPanel
    │   ├── ConversationListPanel (260px, 可切换)
    │   │   ├── 搜索框 + 新建会话按钮
    │   │   └── 会话历史列表（选中态高亮、hover 删除）
    │   ├── CenterCanvas (flex-1, flex-col)
    │   │   ├── ChatHeader (会话标题栏)
    │   │   │   ├── AI 头像 + 会话标题 + 下拉
    │   │   │   └── 右侧：Tokens / 搜索 / 设置 / 分享
    │   │   ├── MessageTimeline (flex-1, overflow-y-auto)
    │   │   │   ├── WelcomeScreen (无消息时)
    │   │   │   ├── DateDivider (日期分割线)
    │   │   │   ├── UserMessage (右对齐, 蓝色气泡)
    │   │   │   ├── AIMessage (左对齐, 白色, 头像)
    │   │   │   │   ├── ThinkingBlock (思维链, 可折叠)
    │   │   │   │   ├── ToolCallBlock (工具调用, 可展开)
    │   │   │   │   └── ContentBlock (Markdown 渲染)
    │   │   │   └── TypingIndicator (流式输入指示器)
    │   │   └── ChatInput (底部输入区)
    │   │       ├── 附件预览区
    │   │       ├── textarea (自动高度, 快捷键)
    │   │       └── 底部工具栏 (附件/@/表情/模型/发送)
    └── (可选) CommandPalette / 提及面板 (浮层)
```

### TopBar — 全局状态栏

**功能点**：
- 品牌区：Lework Logo + 版本号
- AI 状态指示器：绿色脉冲点 + "AI 在线"，hover 显示模型信息
- 通知中心：铃铛图标 + 未读红点
- 用户菜单：头像 + 用户名，下拉包含：个人设置、主题切换、退出

**状态需求**：`aiStatus` / `unreadNotifications` / `currentUser`

### LeftRail — 侧边导航

导航数据结构：
```typescript
type NavItem = {
  id: string;
  label: string;
  icon: string;        // Tabler icon name
  type: 'route' | 'group' | 'submenu';
  href?: string;
  children?: NavItem[];
  badge?: number;
  active?: boolean;
};
```

**一级导航分组**：
1. **核心功能** — AI 助手（点击展开/收起会话列）、工作台
2. **AI 能力** — AI 员工、知识库、技能管理
3. **研发协作** — InsFlow / InsGit / Jenkins / InsSketch
4. **团队效率** — 组织管理、汇报中心、计划任务
5. **系统** — 个人设置、权限管理

### ChatHeader — 会话标题栏

**功能点**：
- 左侧：AI 头像 + 当前会话标题
- 中间：`+` 新建会话按钮
- 右侧工具组：Token 计数器 / 搜索 / 会话设置 / 分享 / 更多菜单（重命名、归档、删除）

**状态需求**：`currentConversation` / `tokenUsage` / `modelConfig`

### 右侧浮层面板（未来扩展）

当前布局已移除右栏（RightRail），未来以 Sheet/浮层形式实现：
- **快捷操作**：根据会话上下文动态生成建议按钮，点击填充到输入框
- **文件收件箱**：拖放上传区域 + 关联文件列表
- **工件预览**：AI 生成的文件预览（原始/渲染模式切换）

## 组件文件组织

```
src/
├── components/
│   ├── layout/
│   │   ├── Shell.tsx              # 布局容器 ✅
│   │   ├── TopBar.tsx             # 全局状态栏 ✅
│   │   ├── LeftRail.tsx           # 导航分组 ✅
│   │   ├── ConversationListPanel.tsx # 会话历史列 ✅
│   │   └── CenterCanvas.tsx       # 消息区容器 ✅
│   │   └── RightRail.tsx          # 保留（未使用，未来浮层扩展）
│   ├── chat/
│   │   ├── ChatHeader.tsx         # 会话标题栏 ✅
│   │   ├── MessageTimeline.tsx    # 消息列表容器 ✅
│   │   ├── UserMessageBubble.tsx  # 用户消息 ✅
│   │   ├── AIMessageBubble.tsx    # AI 消息 ✅
│   │   ├── ToolCallBlock.tsx      # 工具调用块 ✅
│   │   ├── ThinkingBlock.tsx      # 思维链块 ❌
│   │   ├── DateDivider.tsx        # 日期分割 ✅
│   │   └── TypingIndicator.tsx    # 输入指示 ✅
│   ├── input/
│   │   ├── ChatInput.tsx          # 输入区容器 ✅
│   │   ├── AutoResizeTextarea.tsx # 自适应文本域 ❌
│   │   ├── AttachmentPreview.tsx  # 附件预览 ❌
│   │   ├── MentionPanel.tsx       # @提及面板 ❌
│   │   └── CommandPanel.tsx       # /命令面板 ❌
│   └── ui/                        # 基础 UI 组件库 ✅
├── store/
│   ├── appStore.ts                # 合并所有 slices ✅
│   ├── slices/
│   │   ├── layoutSlice.ts         # 重构扩展 ✅
│   │   ├── topicSlice.ts          # 保留 ✅
│   │   └── chatSlice.ts           # 聊天核心状态 ✅
│   └── utils/
│       └── flattenActions.ts      # 已有 ✅
├── hooks/
│   ├── useChat.ts                 # 聊天核心逻辑 ❌
│   ├── useStream.ts               # 流式消息 ❌
│   ├── useMention.ts              # @提及逻辑 ❌
│   ├── useCommand.ts              # /命令逻辑 ❌
│   ├── useWebSocket.ts            # 已有 ✅
│   └── useSSE.ts                  # 已有 ✅
├── lib/
│   ├── request.ts                 # 已有 ✅
│   ├── sse.ts                     # 已有 ✅
│   ├── websocket.ts               # 已有 ✅
│   └── markdown.ts                # Markdown 渲染配置 ❌
├── mocks/
│   ├── streamSimulator.ts         # ✅
│   ├── chatMocks.ts               # ✅
│   └── conversationMocks.ts       # ❌
├── types/
│   ├── chat.ts                    # ✅
│   └── api.ts                     # ✅
└── utils/
    └── format.ts                  # ✅
```

## 与现有代码的兼容性

- **保持 `layoutSlice`**：扩展导航结构，不破坏现有工作区/会话逻辑
- **保持 `topicSlice`**：可独立存在，未来可能与聊天会话合并
- **保持 UI 组件库**：所有新增组件使用 @base-ui/react 和 Tailwind
- **保持通信层**：http / SSEClient / WSClient 已就绪，Mock 阶段使用 streamSimulator 替代

## 关键技术决策

| 决策点 | 选择 | 理由 |
|--------|------|------|
| Markdown 渲染 | react-markdown + remark-gfm | 生态成熟，支持插件 |
| 代码高亮 | shiki (按需) / 首期 Prism | Shiki 更精美但体积大 |
| 流式模拟 | setTimeout 逐字输出 | 简单可控，无需后端 |
| 输入框自动高度 | 原生 textarea + scrollHeight | 无需第三方库 |
| 虚拟滚动 | 首期不实现 | 消息量预计 < 100 条 |
| 文件上传预览 | URL.createObjectURL | 纯前端预览 |