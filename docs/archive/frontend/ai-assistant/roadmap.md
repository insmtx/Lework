# AI 助手模块 — 实施路线图

## Phase 1: 基础骨架 ✅ 已完成

- [x] chatSlice（状态定义 + Mock 数据 + 流式生成器）
- [x] TopBar（品牌区 + AI 状态 + 用户菜单）
- [x] LeftRail（5 分组层级导航 + 会话搜索）
- [x] ChatHeader（会话标题 + Token 计数器 + 设置按钮）
- [x] MessageTimeline + WelcomeScreen 空状态
- [x] UserMessageBubble（蓝色渐变气泡）
- [x] AIMessageBubble（Markdown + 流式光标）
- [x] ToolCallBlock（折叠/展开 + 状态图标）
- [x] ChatInput（自适应 textarea + 附件粘贴 + 模型选择）
- [x] 重构 Shell / CenterCanvas / LeftRail 布局
- [x] 安装 react-markdown + remark-gfm

**交付**：完整静态页面 + Mock 流式对话 + 工具调用展示

## Phase 2: 流式对话体验优化 ✅ 已完成

- [x] 消息自动滚动到底部
- [x] 重新生成按钮
- [x] Token 计数器随发送动态增加
- [x] 消息操作菜单（复制按钮）
- [x] ChatInput/MessageTimeline flex 布局修复（min-h-0 + flex-1）
- [x] TopBar 移除搜索框
- [x] ChatHeader 动态标题/模型/token
- [-] 模型切换影响 Mock 内容（暂不实现，无后端差异）

## Phase 2+: 布局重构 ✅ 已完成

- [x] 移除右栏（RightRail），新增 ConversationListPanel
- [x] LeftRail「AI 助手」项点击切换 ConversationListPanel
- [x] layoutSlice 新增 conversationListOpen + toggleConversationList

**交付**：流式对话体验完整可交互

## Phase 3: 高级交互

- [ ] ThinkingBlock（思维链展示，可折叠）
- [ ] @ 提及面板（MentionPanel）
- [ ] / 命令面板（CommandPanel）
- [ ] # 引用面板（工作项引用弹窗）
- [ ] 会话重命名 / 归档 / 删除下拉菜单
- [ ] 右侧面板快捷操作点击填充到输入框
- [ ] AutoResizeTextarea 组件独立
- [ ] AttachmentPreview 组件
- [ ] conversationMocks.ts 预置数据补充

**交付**：接近截图完整交互体验

## Phase 4: Polish

- [ ] 键盘快捷键（Esc 关闭面板、↑编辑上一条）
- [ ] 错误状态处理（网络错误、AI 服务异常）
- [ ] 响应式适配（移动端折叠侧边栏）
- [ ] 消息搜索功能
- [ ] 代码高亮集成（shiki / Prism）
- [ ] lib/markdown.ts 渲染配置

**交付**：生产级可用

## Phase 5: 后端对接

- [ ] chat.ts API — SSE 流式接口
- [ ] Mock 数据替换为真实 SSE/WS 数据流
- [ ] topicService 实际调用
- [ ] 认证系统接入
- [ ] 路由系统 + 路由守卫

**交付**：前后端联调完整可用