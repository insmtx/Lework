# 前端待完成事项

本文档记录 Lework 前端各模块完成状态。详细规划见 `docs/frontend/` 下的专题文档。

## 通信与状态层

| 模块 | 状态 | 待完成 |
|------|------|--------|
| HTTP 客户端 | ✅ 完成 | baseURL 配置、Auth 拦截器接入 |
| SSE 客户端 | ✅ 完成 | 后端 SSE 端点接入 |
| WebSocket 客户端 | ✅ 完成 | 后端 WS 端点接入 |
| chatSlice | ✅ 完成 | 消息流、输入、Mock 流式生成、resendMessage、tokenUsage |
| layoutSlice | 🔄 桩 | mock 数据替换为后端数据；已扩展 navGroups + collapsedNavGroups + conversationSearchQuery + conversationListOpen |
| topicSlice | 🔄 桩 | topicService 实际调用替换 setTimeout 模拟 |
| 其他 Slice | ❌ 未实现 | userSlice, appSettingsSlice, menuSlice, tabsSlice, configSlice, pluginSlice, notificationSlice, activeTasksSlice |

## 布局与交互层

| 模块 | 状态 | 待完成 |
|------|------|--------|
| Shell 三栏布局 | ✅ 完成 | TopBar + LeftRail + ConversationListPanel + CenterCanvas |
| TopBar | ✅ 完成 | 品牌区 + AI 状态指示器 + 用户菜单 |
| LeftRail | ✅ 完成 | 5 分组层级导航 + 会话搜索 |
| ConversationListPanel | ✅ 完成 | 搜索 + 新建 + 会话历史列表 |
| CenterCanvas | ✅ 完成 | ChatHeader + MessageTimeline + ChatInput |
| ChatHeader | ✅ 完成 | 动态标题/模型/token 计数 |
| MessageTimeline | ✅ 完成 | 消息时间轴 + WelcomeScreen 空状态 + 自动滚动 |
| UserMessageBubble | ✅ 完成 | 蓝色渐变气泡 + hover 复制 |
| AIMessageBubble | ✅ 完成 | Markdown 渲染 + 流式光标 + 复制/重新生成 |
| ToolCallBlock | ✅ 完成 | 报告/展开 + 状态图标 |
| TypingIndicator | ✅ 完成 | 脉冲输入指示器 |
| DateDivider | ✅ 完成 | 日期分割线 |
| WelcomeScreen | ✅ 完成 | 空状态快捷建议网格 |
| ChatInput | ✅ 完成 | 自适应 textarea + 附件粘贴 + 模型选择 |
| ThinkingBlock | ❌ 未实现 | 思维链展示，可折叠 |
| MentionPanel | ❌ 未实现 | @ 成员列表弹窗 |
| CommandPanel | ❌ 未实现 | / 命令列表弹窗 |
| 引用面板 | ❌ 未实现 | # 工作项引用弹窗 |
| 会话管理菜单 | ❌ 未实现 | 重命名 / 归档 / 删除下拉菜单 |
| AiFloat 组件 | ❌ 未实现 | 全局 AI 浮动助手 |
| AiAssistButton | ❌ 未实现 | AI 辅助按钮 |
| BasicLayout | ❌ 未实现 | Sidebar, Header, PageTabs, NotificationPopover |
| BlankLayout | ❌ 未实现 | 登录页、原型编辑器等独立全屏页面 |
| 右侧浮层面板 | ❌ 未实现 | 快捷操作 + 文件收件箱 + 工件预览 |

## 路由与认证层

| 模块 | 状态 | 待完成 |
|------|------|--------|
| 路由系统 | ❌ 未实现 | React Router 配置 + 路由守卫 + 动态路由 |
| 路由守卫 | ❌ 未实现 | 鉴权守卫、角色守卫、插件动态路由 |
| 认证系统 | ❌ 未实现 | Login / OAuth / Session / 强制改密码 |
| 菜单系统 | ❌ 未实现 | menus.ts 配置 + pluginStore 动态菜单 |

## 业务组件与 Hooks

| 模块 | 状态 | 待完成 |
|------|------|--------|
| business 组件库 | ❌ 未实现 | ProTable, DialogForm, SearchToolbar, TableActions 等 19 个组件 |
| useCrudPage | ❌ 未实现 | CRUD 分页列表通用逻辑 |
| useDialogForm | ❌ 未实现 | 对话框表单通用逻辑 |
| useDirtyCheck | ❌ 未实现 | 表单脏检查 + 关闭确认 |
| useInlineAi | ❌ 未实现 | 内联 AI 流式交互 |
| useAiFloat | ❌ 未实现 | AI 浮动助手 Prompt 传递 |
| useNotification | ❌ 未实现 | 通知系统（声音/桌面/标签页闪烁） |
| useChat | ❌ 未实现 | 聊天核心逻辑 Hook |
| useStream | ❌ 未实现 | 流式消息 Hook |
| useMention | ❌ 未实现 | @提及逻辑 |
| useCommand | ❌ 未实现 | /命令逻辑 |

## 页面模块 (30 个业务页面)

| 模块 | 状态 | 待完成 |
|------|------|--------|
| overview | ❌ 未实现 | 概览工作台 |
| chat | ✅ 完成 | Shell 三栏布局 + Mock 流式对话可交互，待接入后端 SSE |
| login | ❌ 未实现 | 登录页 (BlankLayout) |
| profile | ❌ 未实现 | 个人设置页 |
| notifications | ❌ 未实现 | 通知中心 |
| exception | ❌ 未实现 | 403/404/500 异常页 |
| jenkins | ❌ 未实现 | Jenkins CI/CD + moduleConfig.ts |
| knowledge | ❌ 未实现 | 知识库 + moduleConfig.ts |
| reports | ❌ 未实现 | 报表 + moduleConfig.ts |
| schedules | ❌ 未实现 | 定时任务 + moduleConfig.ts |
| ai-employees | ❌ 未实现 | AI 员工配置 |
| skills | ❌ 未实现 | 技能管理 |
| plugins | ❌ 未实现 | 插件管理 |
| marketplace | ❌ 未实现 | 扩展市场 |
| channels | ❌ 未实现 | 通道管理 |
| contacts | ❌ 未实现 | 组织管理 |
| agents | ❌ 未实现 | Agent 路由 |
| ai-config | ❌ 未实现 | AI 模型配置 |
| webhooks | ❌ 未实现 | Webhook 管理 |
| remote-nodes | ❌ 未实现 | 工作节点 |
| logs | ❌ 未实现 | 运行日志 |
| upgrade | ❌ 未实现 | 系统升级 |

## API 层 (19 个模块 API)

| 模块 | 状态 | 待完成 |
|------|------|--------|
| auth.ts | ❌ 未实现 | 登录/登出/Token刷新 |
| chat.ts | ❌ 未实现 | AI 聊天 SSE 流式接口（后端对接） |
| knowledge.ts | ❌ 未实现 | 知识库 API |
| ai-employees.ts | ❌ 未实现 | AI 员工 API |
| plugins.ts | ❌ 未实现 | 插件管理 API |
| skills.ts | ❌ 未实现 | 技能管理 API |
| channels.ts | ❌ 未实现 | 通道管理 API |
| contacts.ts | ❌ 未实现 | 组织管理 API |
| agents.ts | ❌ 未实现 | Agent 路由 API |
| webhooks.ts | ❌ 未实现 | Webhook 管理 API |
| 其他模块 API | ❌ 未实现 | reports, schedules, marketplace, remote-nodes, logs, upgrade, notifications, profile |

## 其他

| 模块 | 状态 | 待完成 |
|------|------|--------|
| 插件化架构 | ❌ 未实现 | moduleConfig.ts 声明 + 动态路由注入 |
| 页面权限模型 | ❌ 未实现 | 路由 meta.requiredRole 控制 |
| Chunk 过期刷新 | ❌ 未实现 | vite:preloadError 监听 + sessionStorage 标记 |
| 键盘快捷键 | ❌ 未实现 | Esc 关闭面板、↑编辑上一条消息 |
| 错误状态处理 | ❌ 未实现 | 网络错误、AI 服务异常 |
| 响应式适配 | ❌ 未实现 | 移动端折叠侧边栏 |
| 消息搜索 | ❌ 未实现 | 搜索当前会话消息 |
| 代码高亮 | ❌ 未实现 | shiki 或 Prism 集成 |
| lib/markdown.ts | ❌ 未实现 | Markdown 渲染配置 |

## 详细规划文档索引

| 文件 | 内容 |
|------|------|
| `ai-assistant/architecture.md` | 页面架构、视觉 Token、组件文件组织、兼容性、技术决策 |
| `ai-assistant/data-model.md` | 消息数据模型、chatSlice 状态管理、流式处理流程 |
| `ai-assistant/interaction.md` | ChatInput 功能、消息渲染策略、工具调用/思维链、动画规范 |
| `ai-assistant/roadmap.md` | 实施路线图（Phase 1~5） |