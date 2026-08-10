import "@testing-library/jest-dom/vitest";

import { fetchFilePreviewByPublicId } from "@leros/store";
import { cleanup, render, screen } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { afterEach, describe, expect, it, vi } from "vitest";
import { AIMessageBubble } from "./AIMessageBubble";

vi.mock("@leros/store", () => ({
	formatArtifactTime: () => "",
	formatTime: () => "10:00",
	formatTokenCount: (count: number) => String(count),
	fetchFilePreviewByPublicId: vi.fn(async () => new Response("完整计划内容")),
	messageArtifactToProjectArtifact: vi.fn(),
	sortProjectArtifactsByNewestFirst: (artifacts: unknown[]) => artifacts,
	useChatStore: (selector: (state: Record<string, unknown>) => unknown) =>
		selector({
			resendMessage: vi.fn(),
			messagesMap: {},
		}),
	useAppStore: (selector: (state: Record<string, unknown>) => unknown) =>
		selector({
			assistants: [],
		}),
	useLayoutStore: (selector: (state: Record<string, unknown>) => unknown) =>
		selector({
			projects: [],
		}),
}));

vi.mock("../common/MarkdownRenderer", () => ({
	MarkdownRenderer: ({
		content,
		onPlanOpen,
		onPlanCopy,
	}: {
		content: string;
		onPlanOpen?: (fileId: string) => void;
		onPlanCopy?: (fileId: string) => Promise<void>;
	}) => (
		<div>
			{content}
			{onPlanOpen && (
				<button type="button" onClick={() => onPlanOpen("file_plan_1")}>
					打开计划
				</button>
			)}
			{onPlanCopy && (
				<button type="button" onClick={() => onPlanCopy("file_plan_1")}>
					复制计划
				</button>
			)}
		</div>
	),
}));

const openPlanPreview = vi.fn();

vi.mock("../layout/file-preview-store", () => ({
	openPlanPreview: (...args: unknown[]) => openPlanPreview(...args),
	openProjectArtifactPreview: vi.fn(),
}));

vi.mock("../layout/project-file-type-icon", () => ({
	ProjectFileTypeIcon: () => null,
}));

vi.mock("./AssistantChatAvatar", () => ({
	AssistantChatAvatar: () => <div>avatar</div>,
}));

afterEach(() => {
	cleanup();
	vi.clearAllMocks();
});

describe("AIMessageBubble", () => {
	it("仅在点击计划后打开文件预览", async () => {
		const user = userEvent.setup();
		render(
			<AIMessageBubble
				message={{
					id: "message-plan",
					conversationId: "conversation-1",
					role: "assistant",
					content: "计划概览",
					timestamp: Date.now(),
				}}
				isStreaming={false}
			/>,
		);

		expect(openPlanPreview).not.toHaveBeenCalled();
		await user.click(screen.getByRole("button", { name: "打开计划" }));
		expect(openPlanPreview).toHaveBeenCalledWith("file_plan_1");
	});

	it("点击复制时才读取完整计划，且不打开预览", async () => {
		const user = userEvent.setup();
		render(
			<AIMessageBubble
				message={{
					id: "message-plan-copy",
					conversationId: "conversation-1",
					role: "assistant",
					content: "计划概览",
					timestamp: Date.now(),
				}}
				isStreaming={false}
			/>,
		);

		expect(fetchFilePreviewByPublicId).not.toHaveBeenCalled();
		await user.click(screen.getByRole("button", { name: "复制计划" }));
		expect(fetchFilePreviewByPublicId).toHaveBeenCalledWith("file_plan_1");
		expect(openPlanPreview).not.toHaveBeenCalled();
	});

	it("执行过程默认收起，且流式状态变化不会覆盖用户手动展开", async () => {
		const user = userEvent.setup();
		const message = {
			id: "message-1",
			conversationId: "conversation-1",
			role: "assistant" as const,
			content: "",
			timestamp: Date.now(),
			processSteps: [
				{
					id: "step-1",
					type: "thinking" as const,
					content: "正在分析问题",
				},
			],
			toolCalls: [],
		};

		const { rerender } = render(<AIMessageBubble message={message} isStreaming={true} />);

		expect(screen.getByRole("button", { name: /执行过程/i })).toBeInTheDocument();
		expect(screen.queryByText("正在分析问题", { selector: "div" })).not.toBeInTheDocument();

		await user.click(screen.getByRole("button", { name: /执行过程/i }));

		expect(screen.getByRole("button", { name: /思考过程/i })).toBeInTheDocument();
		expect(screen.queryByText("正在分析问题", { selector: "div" })).not.toBeInTheDocument();

		await user.click(screen.getByRole("button", { name: /思考过程/i }));

		expect(screen.getByText("正在分析问题", { selector: "div" })).toBeInTheDocument();

		rerender(<AIMessageBubble message={message} isStreaming={false} />);

		expect(screen.getByText("正在分析问题", { selector: "div" })).toBeInTheDocument();
	});

	it("执行过程展开后仍展示最新的过程摘要", async () => {
		const user = userEvent.setup();
		const message = {
			id: "message-expanded-preview",
			conversationId: "conversation-1",
			role: "assistant" as const,
			content: "",
			timestamp: Date.now(),
			processSteps: [{ id: "thinking-1", type: "thinking" as const, content: "正在写入最终文档" }],
			toolCalls: [],
		};

		render(<AIMessageBubble message={message} isStreaming={true} />);

		expect(screen.getByText("正在写入最终文档")).toBeInTheDocument();

		await user.click(screen.getByRole("button", { name: /执行过程/i }));

		expect(screen.getByText("正在写入最终文档")).toBeInTheDocument();
	});

	it("工具调用展示为工具名称列表", async () => {
		const user = userEvent.setup();
		const message = {
			id: "message-tool-calls",
			conversationId: "conversation-1",
			role: "assistant" as const,
			content: "",
			timestamp: Date.now(),
			processSteps: [
				{ id: "tool-call-1", type: "tool_call" as const, toolCallId: "tool-call-1" },
				{ id: "tool-call-2", type: "tool_call" as const, toolCallId: "tool-call-2" },
			],
			toolCalls: [
				{
					id: "tool-call-1",
					name: "read",
					arguments: {},
					status: "success" as const,
				},
				{
					id: "tool-call-2",
					name: "write",
					arguments: {},
					status: "running" as const,
				},
			],
		};

		render(<AIMessageBubble message={message} isStreaming={true} />);

		await user.click(screen.getByRole("button", { name: /执行过程/i }));

		expect(screen.getAllByRole("button", { name: /工具调用：read工具、write工具/i })).toHaveLength(
			1,
		);
		expect(screen.getByText("成功").closest("span")).toHaveTextContent("成功1");

		await user.click(screen.getByRole("button", { name: /工具调用：read工具、write工具/i }));

		expect(screen.getByText("read")).toBeInTheDocument();
		expect(screen.getByText("write")).toBeInTheDocument();
		expect(screen.queryByText("工具调用：read工具、write工具")).not.toBeInTheDocument();
		expect(screen.queryByText(/工具调用 \(\d+\)/)).not.toBeInTheDocument();
	});

	it("两次思考之间的工具调用合并为同一层级", async () => {
		const user = userEvent.setup();
		const message = {
			id: "message-merged-tool-calls",
			conversationId: "conversation-1",
			role: "assistant" as const,
			content: "",
			timestamp: Date.now(),
			processSteps: [
				{ id: "thinking-1", type: "thinking" as const, content: "第一次思考" },
				{ id: "tool-call-1", type: "tool_call" as const, toolCallId: "tool-call-1" },
				{ id: "tool-call-2", type: "tool_call" as const, toolCallId: "tool-call-2" },
				{ id: "thinking-2", type: "thinking" as const, content: "第二次思考" },
			],
			toolCalls: [
				{
					id: "tool-call-1",
					name: "read",
					arguments: {},
					status: "success" as const,
				},
				{
					id: "tool-call-2",
					name: "write",
					arguments: {},
					status: "success" as const,
				},
			],
		};

		render(<AIMessageBubble message={message} isStreaming={true} />);

		await user.click(screen.getByRole("button", { name: /执行过程/i }));

		expect(screen.getAllByRole("button", { name: /工具调用：read工具、write工具/i })).toHaveLength(
			1,
		);
		expect(screen.getAllByRole("button", { name: /思考过程/i })).toHaveLength(2);
	});

	it("执行过程收起时展示最新的过程摘要", () => {
		const message = {
			id: "message-2",
			conversationId: "conversation-1",
			role: "assistant" as const,
			content: "",
			timestamp: Date.now(),
			processSteps: [
				{ id: "tool-call-1", type: "tool_call" as const, toolCallId: "tool-call-1" },
				{ id: "thinking-1", type: "thinking" as const, content: "正在整理文档结构" },
			],
			toolCalls: [
				{
					id: "tool-call-1",
					name: "skill",
					arguments: {},
					status: "running" as const,
				},
			],
		};

		const { rerender } = render(<AIMessageBubble message={message} isStreaming={true} />);

		expect(screen.getByText("正在整理文档结构")).toBeInTheDocument();
		expect(screen.queryByText("调用skill中...")).not.toBeInTheDocument();

		rerender(
			<AIMessageBubble
				message={{
					...message,
					processSteps: [
						...message.processSteps.slice(0, -1),
						{ id: "thinking-1", type: "thinking", content: "正在写入最终文档" },
					],
				}}
				isStreaming={true}
			/>,
		);

		expect(screen.getByText("正在写入最终文档")).toBeInTheDocument();
		expect(screen.queryByText("正在整理文档结构")).not.toBeInTheDocument();
	});

	it("执行过程收起时展示工具调用的动态摘要", () => {
		const message = {
			id: "message-tool-preview",
			conversationId: "conversation-1",
			role: "assistant" as const,
			content: "",
			timestamp: Date.now(),
			processSteps: [{ id: "tool-call-1", type: "tool_call" as const, toolCallId: "tool-call-1" }],
			toolCalls: [
				{
					id: "tool-call-1",
					name: "websearch",
					arguments: { query: "2026年下半年黄金价格走势预测 投资" },
					status: "running" as const,
				},
			],
		};

		render(<AIMessageBubble message={message} isStreaming={true} />);

		expect(screen.getByText("搜索网页中...")).toBeInTheDocument();
		expect(screen.getByText("2026年下半年黄金价格走势预测 投资")).toBeInTheDocument();
	});

	it("最新步骤为工具调用时展示调用中摘要", () => {
		const message = {
			id: "message-skill-preview",
			conversationId: "conversation-1",
			role: "assistant" as const,
			content: "",
			timestamp: Date.now(),
			processSteps: [{ id: "tool-call-1", type: "tool_call" as const, toolCallId: "tool-call-1" }],
			toolCalls: [
				{
					id: "tool-call-1",
					name: "skill",
					arguments: {},
					status: "running" as const,
				},
			],
		};

		render(<AIMessageBubble message={message} isStreaming={true} />);

		expect(screen.getByText("调用skill中...")).toBeInTheDocument();
	});

	it("执行过程结束后展示已完成", () => {
		const message = {
			id: "message-completed-preview",
			conversationId: "conversation-1",
			role: "assistant" as const,
			content: "最终回复",
			timestamp: Date.now(),
			processSteps: [
				{ id: "thinking-1", type: "thinking" as const, content: "正在整理文档结构" },
				{ id: "tool-call-1", type: "tool_call" as const, toolCallId: "tool-call-1" },
			],
			toolCalls: [
				{
					id: "tool-call-1",
					name: "skill",
					arguments: {},
					status: "success" as const,
				},
			],
		};

		render(<AIMessageBubble message={message} isStreaming={false} />);

		expect(screen.getByText("已完成")).toBeInTheDocument();
		expect(screen.queryByText("正在整理文档结构")).not.toBeInTheDocument();
		expect(screen.queryByText("调用skill中...")).not.toBeInTheDocument();
	});
});
