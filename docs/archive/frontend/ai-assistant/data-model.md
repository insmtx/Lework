# AI 助手模块 — 数据模型与状态管理

## 消息数据模型

```typescript
type MessageRole = 'user' | 'assistant' | 'system' | 'tool';
type MessageStatus = 'sending' | 'streaming' | 'complete' | 'error';

type ToolCall = {
  id: string;
  name: string;           // e.g., "vortflow_assign"
  arguments: Record<string, unknown>;
  status: 'pending' | 'running' | 'success' | 'error';
  result?: unknown;
  duration?: number;      // ms
};

type Message = {
  id: string;
  conversationId: string;
  role: MessageRole;
  content: string;        // 最终完整内容
  chunks: string[];       // 流式片段（仅 assistant）
  status: MessageStatus;
  timestamp: number;
  toolCalls?: ToolCall[];
  thinking?: string;      // 思维链 / reasoning
  metadata?: {
    model?: string;
    tokens?: number;
    latency?: number;
  };
};

type Attachment = {
  id: string;
  type: 'image' | 'file';
  name: string;
  size: number;
  url?: string;      // 本地 blob URL 或上传后 URL
  file?: File;       // 原始文件对象
};
```

## ChatInput 状态模型

```typescript
type ChatInputState = {
  text: string;
  attachments: Attachment[];
  isFocused: boolean;
  isGenerating: boolean;
  selectedModel: string;
  mentionPanelOpen: boolean;
  commandPanelOpen: boolean;
  referencePanelOpen: boolean;
};
```

## chatSlice 状态管理

### Store 结构
```typescript
type AppStore = LayoutStore & TopicStore & ChatStore;
```

### ChatState
```typescript
interface ChatState {
  // 消息
  messagesMap: Record<string, Message>;
  messageIds: string[];           // 当前会话的消息 ID 有序列表
  streamingMessageId: string | null;

  // 输入
  inputText: string;
  inputAttachments: Attachment[];
  inputFocused: boolean;
  isGenerating: boolean;
  selectedModel: string;

  // 会话
  currentConversationId: string | null;
  conversations: Conversation[];

  // 工具/面板
  activeToolCalls: ToolCall[];
  mentionQuery: string | null;
  commandQuery: string | null;
}
```

### ChatAction
```typescript
interface ChatAction {
  // 消息流
  sendMessage: (content: string, attachments?: Attachment[]) => Promise<void>;
  appendChunk: (messageId: string, chunk: string) => void;
  finalizeMessage: (messageId: string) => void;
  cancelGeneration: () => void;

  // 输入
  setInputText: (text: string) => void;
  addAttachment: (file: File) => void;
  removeAttachment: (id: string) => void;
  setInputFocused: (focused: boolean) => void;

  // 会话
  createConversation: (title?: string) => string;
  switchConversation: (id: string) => void;
  renameConversation: (id: string, title: string) => void;
  deleteConversation: (id: string) => void;

  // 工具调用
  registerToolCall: (toolCall: ToolCall) => void;
  updateToolCallStatus: (id: string, status: ToolCall['status'], result?: unknown) => void;
}
```

### 实现模式（遵循 Zustand Skill）

- 使用 **Class-based Action Implementation**
- Public Actions：`sendMessage`, `createConversation`
- Internal Actions：`internal_sendMessage`, `internal_streamMessage`
- Dispatch Methods：`#dispatchChat` → `chatReducer`
- Optimistic Update：消息发送后立即渲染，失败时回滚

## 流式消息处理流程

```
用户点击发送
  → chatSlice.sendMessage(content)
    → 1. 乐观更新：立即添加 UserMessage 到 messagesMap
    → 2. 创建空的 AssistantMessage，设置 status='streaming'
    → 3. 调用 mockStream() / 真实 SSE
    → 4. 收到 chunk → appendChunk(messageId, chunk)
    → 5. 收到 tool_call → registerToolCall()
    → 6. 流结束 → finalizeMessage(messageId) / 或 error → 标记 status='error'
```

## Mock 数据方案

### 目录结构
```
src/mocks/
├── chatMocks.ts       # 消息流、工具调用 Mock
├── conversationMocks.ts # 会话列表 Mock
└── streamSimulator.ts # 流式数据生成器
```

### 流式数据模拟器
```typescript
function mockStreamResponse(
  content: string,
  onChunk: (chunk: string) => void,
  onToolCall?: (tool: ToolCall) => void,
  onComplete?: () => void,
): { cancel: () => void };
```

**示例流**：
1. 延迟 500ms 开始
2. 逐字输出文本内容（每字 20ms）
3. 中途插入 ToolCall 事件
4. ToolCall 延迟 800ms 后返回结果
5. 继续输出剩余文本
6. 输出完成标记

### 预置场景
- **代码审查**：用户请求审查 PR → AI 分析 → 调用 `github_review` 工具 → 返回审查报告
- **需求指派**：用户请求指派需求 → AI 调用 `vortflow_assign` → 返回指派结果
- **知识库问答**：引用 `#知识库条目` → AI 检索 → 返回答案