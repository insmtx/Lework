# AI 助手模块 — 交互与渲染规范

## 消息类型渲染策略

| 类型 | 位置 | 样式 | 组件 | 状态 |
|------|------|------|------|------|
| UserMessage | 右对齐 | 蓝色渐变 `from-blue-500 to-blue-600`, 白色文字, 圆角 12px | UserMessageBubble | ✅ |
| AIMessage | 左对齐 | 白色背景 `bg-white`, 灰色边框, 左侧头像 | AIMessageBubble | ✅ |
| DateDivider | 居中 | 小字灰色胶囊 | DateDivider | ✅ |
| TypingIndicator | 左对齐 | 三个跳动圆点 | TypingIndicator | ✅ |
| ThinkingBlock | 左对齐 | 灰色斜体, 可折叠 | ThinkingBlock | ❌ |

## ChatInput 功能规划

**占位文案**："请描述您问题，支持 Ctrl+V 粘贴图片。输入 @ 提及成员，/ 使用命令，# 引用工作项。"

### 输入框功能
- **自动高度**：根据内容行数自动扩展（max 8 行），Shift+Enter 换行
- **粘贴图片**：监听 paste 事件，提取图片文件并上传/预览
- **@ 提及**：输入 `@` 弹出成员选择面板（当前 Mock 为固定列表）
- **/ 命令**：输入 `/` 弹出命令面板（如 `/clear`, `/model`, `/help`）
- **# 引用**：输入 `#` 弹出工作项/知识库引用面板
- **快捷键**：
  - `Enter`：发送消息
  - `Shift + Enter`：换行
  - `Escape`：取消输入/关闭面板
  - `↑`（空输入时）：编辑上一条消息

### 附件预览区
- 位于 textarea 上方
- 支持图片预览（缩略图 + 删除按钮）
- 支持文件列表（图标 + 文件名 + 大小 + 删除）

### 底部工具栏
- **左区**：附件按钮 / 图片按钮 / 表情选择器（可选）
- **右区**：模型选择下拉 / 发送按钮（蓝色主按钮） / 生成中停止按钮（红色边框）

## 工具调用展示

### 结构
```
▼ 工具调用 (2)
  ├─ ▶ vortflow_assign ×2
  │   └─ [展开后显示参数与结果]
  └─ [其他工具...]
```

### 交互
- 默认折叠，显示工具名称 + 调用次数
- 点击展开显示：参数 JSON、执行状态（spinner → checkmark）、返回结果
- 执行中的工具显示脉冲动画
- 失败工具显示红色错误信息

**组件**：`ToolCallBlock`, `ToolCallItem`

## 思维链展示（ThinkingBlock）

### 结构
```
▼ 思考过程
  └─ [灰色斜体文本，展示 AI 推理步骤]
```

### 交互
- 可折叠，默认折叠以节省空间

## Markdown 内容渲染

- 支持标准 Markdown：标题、列表、代码块、表格、引用
- 代码块支持语法高亮（react-markdown + shiki 或 Prism）
- 内联代码：浅色背景
- 链接：蓝色下划线，hover 变色

## 动画与交互规范

### 允许的动画
- `transition: opacity, color, background-color, border-color, box-shadow`
- `transform: scale(0.98 → 1)` 按钮按下反馈
- 工具调用执行中：脉冲动画（`animate-pulse`）仅限图标
- 流式文本：无动画，直接追加内容

### 禁止的动画
- 消息气泡飞入/弹入（干扰阅读）
- 背景粒子/装饰动画
- 页面切换过渡动画（当前单页无需）

### 流式渲染性能
- 消息内容使用 `dangerouslySetInnerHTML` 或 `react-markdown` 渲染
- 流式更新时仅更新文本节点，避免整组件重渲染
- 长消息列表使用虚拟滚动（超过 100 条消息，可选）