import "@testing-library/jest-dom/vitest";

import { fetchFilePreviewByPublicId } from "@leros/store";
import { cleanup, render, screen } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { afterEach, describe, expect, it, vi } from "vitest";
import { AIMessageBubble } from "./AIMessageBubble";

vi.mock("@leros/store", () => ({
	formatArtifactTime: () => "",
	formatTime: () => "10:00",
	fetchFilePreviewByPublicId: vi.fn(async () => new Response("完整计划内容")),
	getAssistantMessageFooterSegments: () => [],
	messageArtifactToProjectArtifact: vi.fn(),
	sortProjectArtifactsByNewestFirst: (artifacts: unknown[]) => artifacts,
	useChatStore: (selector: (state: Record<string, unknown>) => unknown) =>
		selector({
			resendMessage: vi.fn(),
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

vi.mock("../layout/ArtifactPreviewDialog", () => ({
	ArtifactPreviewDialog: () => null,
}));

vi.mock("./MessageAttachmentPreviewDialog", () => ({
	MessageAttachmentPreviewDialog: ({
		attachment,
		open,
	}: {
		attachment: { fileUploadId?: string } | null;
		open: boolean;
	}) => (open ? <div>预览文件：{attachment?.fileUploadId}</div> : null),
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

		expect(screen.queryByText("预览文件：file_plan_1")).not.toBeInTheDocument();
		await user.click(screen.getByRole("button", { name: "打开计划" }));
		expect(screen.getByText("预览文件：file_plan_1")).toBeInTheDocument();
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
		expect(screen.queryByText("预览文件：file_plan_1")).not.toBeInTheDocument();
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

		expect(screen.getByText("正在分析问题", { selector: "div" })).toBeInTheDocument();

		rerender(<AIMessageBubble message={message} isStreaming={false} />);

		expect(screen.getByText("正在分析问题", { selector: "div" })).toBeInTheDocument();
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
		expect(screen.queryByText("调用：skill")).not.toBeInTheDocument();

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
});
