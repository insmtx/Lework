import { describe, expect, it } from "vitest";

import type { Message } from "../types/chat";
import {
	allLocalMessagesBelongToSession,
	applySessionEventToMessage,
	attachAssistantReplyTargets,
	createAssistantSessionEventsWaitingMessage,
	insertGlobalUserMessageId,
	isGlobalUserEchoMessage,
	isTaskRoomAssistantPlaceholder,
	mapBackendMessage,
	retainLocalMessagesForSession,
} from "./chatSlice";

function assistantMessage(content = ""): Message {
	return {
		id: "message-1",
		conversationId: "session-1",
		role: "assistant",
		content,
		timestamp: 1,
	};
}

function waitingAssistantMessage(): Message {
	return {
		...assistantMessage(),
		status: "waiting",
		statusText: "正在提交问题并分配 AI 员工...",
	};
}

describe("applySessionEventToMessage plan.published", () => {
	const directive =
		':::plan{"file_id":"file_plan_1","summary_lines":1,"total_lines":2}\nInspect\n:::';
	const event = {
		type: "plan.published",
		payload: {
			file_id: "file_plan_1",
			directive,
			summary_lines: 1,
			total_lines: 2,
		},
	};

	it("appends a direct public SSE plan payload to assistant content", () => {
		const result = applySessionEventToMessage(assistantMessage("Existing"), event, event.type, {
			appendContent: true,
		});

		expect(result.content).toBe(`Existing\n${directive}`);
	});

	it("does not append the same plan directive twice", () => {
		const message = assistantMessage(directive);
		const result = applySessionEventToMessage(message, event, event.type, { appendContent: true });

		expect(result).toBe(message);
	});
});

describe("applySessionEventToMessage waiting status", () => {
	it("clears the waiting status when the run completes", () => {
		const result = applySessionEventToMessage(
			waitingAssistantMessage(),
			{
				type: "run.completed",
				payload: {
					result: { message: "处理完成" },
				},
			},
			"run.completed",
			{ appendContent: true },
		);

		expect(result.status).toBe("completed");
		expect(result.statusText).toBeUndefined();
		expect(result.content).toBe("处理完成");
	});
});

describe("createAssistantSessionEventsWaitingMessage", () => {
	it("keeps the waiting status when restoring an active SessionEvents replay", () => {
		const message = createAssistantSessionEventsWaitingMessage(
			"session-1",
			"msg-assistant-resume-1",
			1,
		);

		expect(message.status).toBe("waiting");
		expect(message.statusText).toBe("AI 员工已接单，正在生成回复...");
		expect(message.conversationId).toBe("session-1");
	});

	it("preserves replyTo when restoring a waiting SessionEvents placeholder", () => {
		const message = createAssistantSessionEventsWaitingMessage(
			"session-1",
			"msg-assistant-resume-1",
			1,
			{
				replyTo: {
					messageId: "user-1",
					authorName: "张三",
					content: "帮我写周报",
				},
			},
		);

		expect(message.replyTo).toEqual({
			messageId: "user-1",
			authorName: "张三",
			content: "帮我写周报",
		});
	});
});

describe("isTaskRoomAssistantPlaceholder", () => {
	it("treats resume and poll placeholders as replaceable stream placeholders", () => {
		expect(
			isTaskRoomAssistantPlaceholder(
				{
					id: "msg-assistant-resume-1",
					conversationId: "session-1",
					role: "assistant",
					content: "",
					timestamp: 1,
					status: "waiting",
				},
				"session-1",
			),
		).toBe(true);
		expect(
			isTaskRoomAssistantPlaceholder(
				{
					id: "msg-assistant-poll-1",
					conversationId: "session-1",
					role: "assistant",
					content: "",
					timestamp: 1,
				},
				"session-1",
			),
		).toBe(true);
	});

	it("ignores completed assistant history messages", () => {
		expect(
			isTaskRoomAssistantPlaceholder(
				{
					id: "msg-assistant-run-1",
					conversationId: "session-1",
					role: "assistant",
					content: "done",
					timestamp: 1,
					status: "completed",
				},
				"session-1",
			),
		).toBe(false);
	});
});

describe("mapBackendMessage", () => {
	it("does not mark completed history chunks as streaming when restoring process steps", () => {
		const result = mapBackendMessage({
			id: "assistant-1",
			session_id: "session-1",
			role: "assistant",
			content: "2026年7月6日，星期一。",
			timestamp: 1,
			message_type: "text",
			sequence: 2,
			created_at: "2026-07-06T09:38:58.904873Z",
			chunks: [
				{
					type: "reasoning.delta",
					session_id: "session-1",
					payload: {
						content: "用户问今天几号，这是一个需要获取当前时间的问题。",
					},
					sequence: 2,
					timestamp: 1,
				},
				{
					type: "message.delta",
					session_id: "session-1",
					payload: {
						content: "2026年7月6日，星期一。",
					},
					sequence: 3,
					timestamp: 2,
				},
			],
		});

		expect(result.status).toBeUndefined();
		expect(result.content).toBe("2026年7月6日，星期一。");
		const processStep = result.processSteps?.[0];
		if (processStep?.type !== "thinking") {
			throw new Error("expected restored reasoning step");
		}
		expect(processStep.content).toBe("用户问今天几号，这是一个需要获取当前时间的问题。");
	});

	it("forces todos to completed when restoring a finished assistant history message", () => {
		const result = mapBackendMessage({
			id: "assistant-1",
			session_id: "session-1",
			role: "assistant",
			content: "已完成所有任务。",
			timestamp: 1,
			message_type: "text",
			sequence: 2,
			created_at: "2026-07-23T07:06:04.790957Z",
			chunks: [
				{
					type: "todo.snapshot",
					session_id: "session-1",
					payload: {
						todos: [
							{ id: "step-1", title: "第一步", status: "completed" },
							{ id: "step-2", title: "第二步", status: "in_progress" },
						],
					},
					sequence: 1,
					timestamp: 1,
				},
			],
		});

		expect(result.todos).toEqual([
			{ id: "step-1", title: "第一步", status: "completed" },
			{ id: "step-2", title: "第二步", status: "completed" },
		]);
	});
});

describe("attachAssistantReplyTargets", () => {
	it("uses display content instead of an internal reference payload", () => {
		const messages: Message[] = [
			{
				id: "475",
				conversationId: "session-1",
				role: "user",
				content: "<reference>{}</reference>",
				metadata: { displayContent: "扩写文档选区：「运动」" },
				timestamp: 1,
			},
			{
				id: "476",
				conversationId: "session-1",
				role: "assistant",
				content: "已完成",
				timestamp: 2,
				runId: "req_475",
			},
		];

		const result = attachAssistantReplyTargets(messages);

		expect(result[1]?.replyTo?.content).toBe("扩写文档选区：「运动」");
	});

	it("links an assistant history message to the user message encoded in run_id", () => {
		const messages: Message[] = [
			{
				id: "475",
				conversationId: "session-1",
				role: "user",
				content: "北京的天气",
				timestamp: 1,
				author: {
					id: "3",
					name: "18435155690",
					type: "user",
				},
			},
			{
				id: "476",
				conversationId: "session-1",
				role: "assistant",
				content: "北京今天雷阵雨。",
				timestamp: 2,
				runId: "req_475",
			},
		];

		const result = attachAssistantReplyTargets(messages);

		expect(result[1]?.replyTo).toEqual({
			messageId: "475",
			authorName: "18435155690",
			content: "北京的天气",
		});
	});

	it("does not treat non-request run_id values as reply targets", () => {
		const messages: Message[] = [
			{
				id: "475",
				conversationId: "session-1",
				role: "user",
				content: "北京的天气",
				timestamp: 1,
			},
			{
				id: "476",
				conversationId: "session-1",
				role: "assistant",
				content: "北京今天雷阵雨。",
				timestamp: 2,
				runId: "run_475",
			},
		];

		const result = attachAssistantReplyTargets(messages);

		expect(result[1]?.replyTo).toBeUndefined();
	});
});

describe("isGlobalUserEchoMessage", () => {
	it("merges optimistic local user messages as echo", () => {
		const local: Message = {
			id: "msg-user-1",
			conversationId: "session-1",
			role: "user",
			content: "hello",
			timestamp: 1,
			author: { id: "current-user", name: "我", type: "user" },
		};
		const incoming: Message = {
			id: "42",
			conversationId: "session-1",
			role: "user",
			content: "hello",
			timestamp: 2,
			author: { id: "7", name: "张三", type: "user" },
		};
		expect(isGlobalUserEchoMessage(local, incoming)).toBe(true);
	});

	it("does not treat another teammate's same-text question as echo of a persisted user message", () => {
		const local: Message = {
			id: "10",
			conversationId: "session-1",
			role: "user",
			content: "hello",
			timestamp: Date.now() - 1_000,
			author: { id: "1", name: "用户", type: "user" },
		};
		const incoming: Message = {
			id: "11",
			conversationId: "session-1",
			role: "user",
			content: "hello",
			timestamp: Date.now(),
			author: { id: "2", name: "用户", type: "user" },
		};
		expect(isGlobalUserEchoMessage(local, incoming)).toBe(false);
	});
});

describe("insertGlobalUserMessageId", () => {
	it("inserts before the current waiting assistant instead of an old streaming history message", () => {
		const incoming: Message = {
			id: "user-2",
			conversationId: "session-1",
			role: "user",
			content: "Next question",
			timestamp: 3,
		};
		const messagesMap: Record<string, Message> = {
			"user-1": {
				id: "user-1",
				conversationId: "session-1",
				role: "user",
				content: "Previous question",
				timestamp: 1,
			},
			"assistant-history": {
				id: "assistant-history",
				conversationId: "session-1",
				role: "assistant",
				content: "Previous answer",
				timestamp: 2,
				status: "streaming",
			},
			"msg-assistant-waiting-1": {
				id: "msg-assistant-waiting-1",
				conversationId: "session-1",
				role: "assistant",
				content: "",
				timestamp: 4,
				status: "waiting",
			},
			[incoming.id]: incoming,
		};

		const result = insertGlobalUserMessageId(
			["user-1", "assistant-history", "msg-assistant-waiting-1"],
			messagesMap,
			incoming,
			"msg-assistant-waiting-1",
		);

		expect(result).toEqual(["user-1", "assistant-history", "user-2", "msg-assistant-waiting-1"]);
	});

	it("inserts before the active streaming assistant when GlobalEvents replaces the waiting placeholder first", () => {
		const incoming: Message = {
			id: "user-2",
			conversationId: "session-1",
			role: "user",
			content: "Next question",
			timestamp: 3,
		};
		const messagesMap: Record<string, Message> = {
			"user-1": {
				id: "user-1",
				conversationId: "session-1",
				role: "user",
				content: "Previous question",
				timestamp: 1,
			},
			"assistant-history": {
				id: "assistant-history",
				conversationId: "session-1",
				role: "assistant",
				content: "Previous answer",
				timestamp: 2,
				status: "streaming",
			},
			"msg-assistant-run-1": {
				id: "msg-assistant-run-1",
				conversationId: "session-1",
				role: "assistant",
				content: "",
				timestamp: 4,
				status: "streaming",
			},
			[incoming.id]: incoming,
		};

		const result = insertGlobalUserMessageId(
			["user-1", "assistant-history", "msg-assistant-run-1"],
			messagesMap,
			incoming,
			"msg-assistant-run-1",
		);

		expect(result).toEqual(["user-1", "assistant-history", "user-2", "msg-assistant-run-1"]);
	});
});

describe("retainLocalMessagesForSession", () => {
	const baseState = {
		messagesMap: {},
		messageIds: [],
		streamingMessageId: null,
		isGenerating: false,
		pendingBootstrapSessionId: null,
		cancellingSessionId: null,
		suppressedReplySessionId: null,
		streamCancelRef: null,
		inputText: "",
		inputAttachments: [],
		inputFocused: false,
		selectedModel: "gpt-4",
		executionMode: "default" as const,
		modelOptions: [],
		activeSessionId: "session-a",
		tokenUsage: { total: 0, currentSession: 0 },
	};

	it("drops messages from other sessions and resets stale streaming state", () => {
		const result = retainLocalMessagesForSession(
			{
				...baseState,
				messageIds: ["user-a", "assistant-a", "assistant-b"],
				messagesMap: {
					"user-a": {
						id: "user-a",
						conversationId: "session-a",
						role: "user",
						content: "问题 A",
						timestamp: 1,
					},
					"assistant-a": {
						id: "assistant-a",
						conversationId: "session-a",
						role: "assistant",
						content: "",
						timestamp: 2,
						status: "streaming",
					},
					"assistant-b": {
						id: "assistant-b",
						conversationId: "session-b",
						role: "assistant",
						content: "",
						timestamp: 3,
						status: "streaming",
					},
				},
				streamingMessageId: "assistant-a",
				isGenerating: true,
				streamCancelRef: () => undefined,
			},
			"session-b",
		);

		expect(result.messageIds).toEqual(["assistant-b"]);
		expect(result.streamingMessageId).toBe("assistant-b");
		expect(result.isGenerating).toBe(true);
	});

	it("clears generating flags when the target session has no active stream", () => {
		const result = retainLocalMessagesForSession(
			{
				...baseState,
				messageIds: ["user-a", "assistant-a"],
				messagesMap: {
					"user-a": {
						id: "user-a",
						conversationId: "session-a",
						role: "user",
						content: "问题 A",
						timestamp: 1,
					},
					"assistant-a": {
						id: "assistant-a",
						conversationId: "session-a",
						role: "assistant",
						content: "",
						timestamp: 2,
						status: "streaming",
					},
				},
				streamingMessageId: "assistant-a",
				isGenerating: true,
			},
			"session-b",
		);

		expect(result.messageIds).toEqual([]);
		expect(result.streamingMessageId).toBeNull();
		expect(result.isGenerating).toBe(false);
	});
});

describe("allLocalMessagesBelongToSession", () => {
	it("returns true when every local message belongs to the session", () => {
		expect(
			allLocalMessagesBelongToSession(
				{
					messageIds: ["user-1", "assistant-1"],
					messagesMap: {
						"user-1": {
							id: "user-1",
							conversationId: "session-1",
							role: "user",
							content: "hello",
							timestamp: 1,
						},
						"assistant-1": {
							id: "assistant-1",
							conversationId: "session-1",
							role: "assistant",
							content: "",
							timestamp: 2,
						},
					},
				} as never,
				"session-1",
			),
		).toBe(true);
	});

	it("returns false when messages from multiple sessions are mixed", () => {
		expect(
			allLocalMessagesBelongToSession(
				{
					messageIds: ["user-1", "assistant-2"],
					messagesMap: {
						"user-1": {
							id: "user-1",
							conversationId: "session-1",
							role: "user",
							content: "hello",
							timestamp: 1,
						},
						"assistant-2": {
							id: "assistant-2",
							conversationId: "session-2",
							role: "assistant",
							content: "",
							timestamp: 2,
						},
					},
				} as never,
				"session-1",
			),
		).toBe(false);
	});
});
