import { describe, expect, it } from "vitest";

import type { Message } from "../types/chat";
import {
	applySessionEventToMessage,
	createAssistantSessionEventsWaitingMessage,
	insertGlobalUserMessageId,
	mapBackendMessage,
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
