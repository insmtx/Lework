import type { Message } from "@lework/api";
import { describe, expect, it } from "vitest";

import { messageText, rendersMarkdown, shouldRenderMessage, toolParts } from "./messages";

function message(overrides: Partial<Message>): Message {
	return { id: "msg_test", role: "assistant", ...overrides };
}

describe("shouldRenderMessage", () => {
	it("不渲染控制面合成的 idle 结束标记", () => {
		expect(shouldRenderMessage(message({ role: "idle", outcome: "succeeded" }))).toBe(false);
	});

	it("失败的 idle 仍要显示错误，不能被一起隐藏", () => {
		const failed = message({
			role: "idle",
			outcome: "error",
			error: { type: "run_failed", message: "boom" },
		});
		expect(shouldRenderMessage(failed)).toBe(true);
	});

	it("只有 reasoning 的助手消息不占位", () => {
		expect(shouldRenderMessage(message({ parts: [{ type: "reasoning", text: "thinking" }] }))).toBe(
			false,
		);
	});

	it("带工具调用的助手消息必须显示", () => {
		expect(shouldRenderMessage(message({ parts: [{ type: "tool", tool: "bash" }] }))).toBe(true);
	});

	it("没有文本的用户消息不显示", () => {
		expect(shouldRenderMessage(message({ role: "user", text: "" }))).toBe(false);
	});

	it("只有错误、没有文本的助手消息仍要显示", () => {
		expect(
			shouldRenderMessage(message({ error: { type: "model_error", message: "rate limited" } })),
		).toBe(true);
	});
});

describe("rendersMarkdown", () => {
	it("助手回复按 Markdown 渲染", () => {
		expect(rendersMarkdown(message({ role: "assistant" }))).toBe(true);
	});

	it("用户输入保持纯文本，不被 Markdown 改写", () => {
		expect(rendersMarkdown(message({ role: "user" }))).toBe(false);
	});

	it("合成的 idle 结束标记不渲染 Markdown", () => {
		expect(rendersMarkdown(message({ role: "idle" }))).toBe(false);
	});
});

describe("messageText", () => {
	it("优先使用聚合文本", () => {
		expect(
			messageText(
				message({
					text: "hello",
					parts: [{ type: "text", text: "ignored" }],
				}),
			),
		).toBe("hello");
	});

	it("没有聚合文本时拼接 text parts", () => {
		expect(
			messageText(
				message({
					parts: [
						{ type: "reasoning", text: "hidden" },
						{ type: "text", text: "line one" },
						{ type: "text", text: "line two" },
					],
				}),
			),
		).toBe("line one\nline two");
	});
});

describe("toolParts", () => {
	it("只返回工具调用，忽略 reasoning 与 text", () => {
		const parts = toolParts(
			message({
				parts: [
					{ type: "reasoning", text: "thinking" },
					{ type: "tool", tool: "bash", callID: "call_1" },
					{ type: "text", text: "done" },
				],
			}),
		);
		expect(parts.map((part) => part.callID)).toEqual(["call_1"]);
	});

	it("没有 parts 时返回空数组", () => {
		expect(toolParts(message({}))).toEqual([]);
	});
});
