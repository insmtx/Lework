import type { Message, ModelOption } from "../types/chat";

export const mockModelOptions: ModelOption[] = [
	{ id: "gpt-4", label: "GPT-4", provider: "OpenAI" },
	{ id: "claude-3", label: "Claude-3", provider: "Anthropic" },
	{ id: "deepseek", label: "DeepSeek", provider: "DeepSeek" },
];

export const mockMessages: Record<string, Message[]> = {
	"conv-1": [
		{
			id: "msg-1-1",
			conversationId: "conv-1",
			role: "user",
			content: "帮我审查这个 Pull Request，检查代码质量和潜在问题。",
			timestamp: Date.now() - 120000,
		},
		{
			id: "msg-1-2",
			conversationId: "conv-1",
			role: "assistant",
			content: "",
			timestamp: Date.now() - 90000,
			toolCalls: [
				{
					id: "tc-1",
					name: "github_review",
					arguments: { repo: "insmtx/Leros", pr_number: 42 },
					status: "running",
				},
			],
		},
		{
			id: "msg-1-3",
			conversationId: "conv-1",
			role: "assistant",
			content: `好的，我来帮你审查这个 Pull Request。

## 代码审查总结

### 发现的问题

1. **潜在空指针异常** - 在 \`src/utils/parser.ts\` 第 45 行，未对输入参数进行空值检查。

2. **性能优化建议** - \`src/components/List.tsx\` 中的 renderItem 回调建议使用 useMemo 包裹以避免不必要的重渲染。

### 改进建议

- 添加类型注释以提高代码可维护性
- 考虑提取公共逻辑到独立的工具函数中
- 增加单元测试覆盖率

| 文件 | 问题数 | 严重程度 |
|------|--------|----------|
| parser.ts | 2 | 高 |
| List.tsx | 1 | 中 |
| config.ts | 0 | - |

整体代码质量良好，建议处理上述问题后合并。`,
			timestamp: Date.now() - 60000,
			toolCalls: [
				{
					id: "tc-1",
					name: "github_review",
					arguments: { repo: "insmtx/Leros", pr_number: 42 },
					status: "success",
					result: { issues_found: 3, approved: false },
					duration: 800,
				},
			],
			metadata: {
				model: "GPT-4",
				tokens: 245,
				latency: 1200,
			},
		},
		{
			id: "msg-1-4",
			conversationId: "conv-1",
			role: "user",
			content: "请把第二个建议的具体实现代码给我。",
			timestamp: Date.now() - 30000,
		},
		{
			id: "msg-1-5",
			conversationId: "conv-1",
			role: "assistant",
			content: [
				"以下是 `useMemo` 包裹 renderItem 的实现代码：",
				"",
				"```tsx",
				'import { useMemo } from "react";',
				"",
				"function List({ items, onSelect }) {",
				"  const renderItem = useMemo(",
				"    () => (item) => (",
				"      <ListItem key={item.id} item={item} onClick={() => onSelect(item.id)} />",
				"    ),",
				"    [onSelect]",
				"  );",
				"",
				"  return <VirtualList items={items} renderItem={renderItem} />;",
				"}",
				"```",
				"",
				"这样 `renderItem` 只会在 `onSelect` 变化时重新创建，避免了每次渲染都生成新回调导致的列表项重渲染。",
			].join("\n"),
			timestamp: Date.now() - 10000,
			metadata: {
				model: "GPT-4",
				tokens: 180,
				latency: 800,
			},
		},
	],
	"conv-2": [
		{
			id: "msg-2-1",
			conversationId: "conv-2",
			role: "user",
			content: "将 INS-1034 需求指派给张三，优先级设为高。",
			timestamp: Date.now() - 7200000,
		},
		{
			id: "msg-2-2",
			conversationId: "conv-2",
			role: "assistant",
			content: "",
			timestamp: Date.now() - 7200000 + 1000,
			toolCalls: [
				{
					id: "tc-2",
					name: "vortflow_assign",
					arguments: { task_id: "INS-1034", assignee: "张三", priority: "高" },
					status: "running",
				},
			],
		},
		{
			id: "msg-2-3",
			conversationId: "conv-2",
			role: "assistant",
			content: `需求 **INS-1034** 已成功指派：

- **指派给**：张三
- **优先级**：高
- **状态**：已更新为"进行中"

张三已收到通知，预计明天开始处理。`,
			timestamp: Date.now() - 7200000 + 2000,
			toolCalls: [
				{
					id: "tc-2",
					name: "vortflow_assign",
					arguments: { task_id: "INS-1034", assignee: "张三", priority: "高" },
					status: "success",
					result: { assigned_to: "张三", priority: "高", status: "进行中" },
					duration: 600,
				},
			],
			metadata: {
				model: "Claude-3",
				tokens: 120,
				latency: 500,
			},
		},
	],
	"conv-3": [
		{
			id: "msg-3-1",
			conversationId: "conv-3",
			role: "user",
			content: "解释一下 React 中 useEffect 的清理函数什么时候执行？",
			timestamp: Date.now() - 14400000,
		},
		{
			id: "msg-3-2",
			conversationId: "conv-3",
			role: "assistant",
			content: `React 中 \`useEffect\` 的清理函数在以下时机执行：

### 1. 组件卸载时

当组件从 DOM 中移除时，React 会执行最后一次清理。

### 2. 重新渲染前（依赖变化）

如果 effect 的依赖项发生变化，React 会在执行新 effect **之前** 先清理上一次的 effect。

### 3. 严格模式下的双重执行

在开发环境的 Strict Mode 下，React 会故意双重调用 effect 和清理函数来帮助你发现副作用问题。

**示例**：

\`\`\`tsx
useEffect(() => {
  const subscription = api.subscribe(id, handler);
  return () => {
    subscription.unsubscribe();
  };
}, [id]);
\`\`\`

当 \`id\` 变化时：先 unsubscribe 旧订阅 → 再 subscribe 新订阅。`,
			timestamp: Date.now() - 14400000 + 5000,
			metadata: {
				model: "DeepSeek",
				tokens: 310,
				latency: 900,
			},
		},
	],
};

export const mockStreamContent = [
	"好的，让我来分析这段代码的问题。",
	"",
	"## 分析结果",
	"",
	"经过检查，我发现以下几个关键点：",
	"",
	"1. **变量命名** - 建议使用更具描述性的变量名称",
	"2. **错误处理** - 当前缺少对异常情况的捕获",
	"",
	"```typescript",
	"function process(data: unknown) {",
	"  if (!data) throw new Error('Data is required');",
	"  return transform(data as InputType);",
	"}",
	"```",
	"",
	"### 优化建议",
	"",
	"- 增加输入验证",
	"- 使用 TypeScript 严格模式",
	"- 添加单元测试覆盖边界情况",
	"",
	"以上是我的分析，是否需要我提供更详细的修复方案？",
].join("\n");
