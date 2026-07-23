import type { Message, RuntimeTodoItem } from "@leros/store/types/chat";
import { describe, expect, it } from "vitest";
import { getLatestAssistantTodos } from "./taskProgress";

function assistantMessage(
	id: string,
	sessionId: string,
	todos?: RuntimeTodoItem[],
	options?: Partial<Pick<Message, "status" | "content" | "processSteps">>,
): Message {
	return {
		id,
		conversationId: sessionId,
		role: "assistant",
		content: options?.content ?? "",
		timestamp: Number(id.replace(/\D/g, "")) || 0,
		todos,
		status: options?.status,
		processSteps: options?.processSteps,
	};
}

describe("getLatestAssistantTodos", () => {
	const sessionId = "session-1";

	it("returns streaming assistant todos when present", () => {
		const streamingId = "msg-2";
		const messagesMap = {
			"msg-1": assistantMessage("msg-1", sessionId, [
				{ id: "old", title: "旧进度", status: "completed" },
			]),
			[streamingId]: assistantMessage(streamingId, sessionId, [
				{ id: "new", title: "新进度", status: "in_progress" },
			]),
		};

		expect(
			getLatestAssistantTodos(messagesMap, ["msg-1", streamingId], sessionId, streamingId),
		).toEqual([{ id: "new", title: "新进度", status: "in_progress" }]);
	});

	it("falls back to the latest assistant message that has todos", () => {
		const messagesMap = {
			"msg-1": assistantMessage("msg-1", sessionId, [
				{ id: "step-1", title: "第一步", status: "completed" },
			]),
			"msg-2": assistantMessage("msg-2", sessionId),
		};

		expect(getLatestAssistantTodos(messagesMap, ["msg-1", "msg-2"], sessionId, null)).toEqual([
			{ id: "step-1", title: "第一步", status: "completed" },
		]);
	});

	it("returns undefined when no assistant message has todos", () => {
		const messagesMap = {
			"msg-1": assistantMessage("msg-1", sessionId),
		};

		expect(getLatestAssistantTodos(messagesMap, ["msg-1"], sessionId, null)).toBeUndefined();
	});

	it("forces todos to completed when the assistant run has finished", () => {
		const messagesMap = {
			"msg-1": assistantMessage(
				"msg-1",
				sessionId,
				[
					{ id: "step-1", title: "创建一个 markdown 文件", status: "completed" },
					{ id: "step-2", title: "写入 5 条任务项", status: "completed" },
					{ id: "step-3", title: "标记完成并总结", status: "in_progress" },
				],
				{
					status: "completed",
					content: "已完成所有任务。",
				},
			),
		};

		expect(getLatestAssistantTodos(messagesMap, ["msg-1"], sessionId, null)).toEqual([
			{ id: "step-1", title: "创建一个 markdown 文件", status: "completed" },
			{ id: "step-2", title: "写入 5 条任务项", status: "completed" },
			{ id: "step-3", title: "标记完成并总结", status: "completed" },
		]);
	});

	it("does not force complete todos while the assistant message is still streaming", () => {
		const streamingId = "msg-1";
		const messagesMap = {
			[streamingId]: assistantMessage(
				streamingId,
				sessionId,
				[{ id: "step-1", title: "第一步", status: "in_progress" }],
				{ status: "streaming" },
			),
		};

		expect(getLatestAssistantTodos(messagesMap, [streamingId], sessionId, streamingId)).toEqual([
			{ id: "step-1", title: "第一步", status: "in_progress" },
		]);
	});
});
