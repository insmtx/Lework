import { describe, expect, it } from "vitest";

import type { Message } from "../types/chat";
import { applySessionEventToMessage } from "./chatSlice";

function assistantMessage(content = ""): Message {
	return {
		id: "message-1",
		conversationId: "session-1",
		role: "assistant",
		content,
		timestamp: 1,
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
