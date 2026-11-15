import type { Message } from "@lework/api";
import { describe, expect, it } from "vitest";

import { reasoningParts, reasoningText, visibleReasoning } from "./process";

function message(parts: Message["parts"]): Message {
	return { id: "msg_1", role: "assistant", parts };
}

describe("reasoningText", () => {
	it("只拼接 reasoning part，忽略正文与工具", () => {
		expect(
			reasoningText(
				message([
					{ type: "text", text: "答案" },
					{ type: "reasoning", text: "先想一步" },
					{ type: "reasoning", text: "再想一步" },
					{ type: "tool", tool: "bash" },
				]),
			),
		).toBe("先想一步\n再想一步");
	});

	it("没有 reasoning 时返回空串", () => {
		expect(reasoningText(message([{ type: "text", text: "答案" }]))).toBe("");
		expect(reasoningText({ id: "m", role: "assistant" })).toBe("");
	});
});

describe("reasoningParts", () => {
	it("过滤掉非 reasoning part", () => {
		const parts = reasoningParts(
			message([
				{ type: "text", text: "答案" },
				{ type: "reasoning", text: "思考" },
			]),
		);
		expect(parts).toHaveLength(1);
		expect(parts[0]?.text).toBe("思考");
	});
});

describe("visibleReasoning", () => {
	const target = message([{ type: "reasoning", text: "第一句。第二句。第三句。" }]);

	it("hidden 完全不返回", () => {
		expect(visibleReasoning(target, "hidden")).toBeNull();
	});

	it("brief 只给一段短摘要", () => {
		expect(visibleReasoning(message([{ type: "reasoning", text: "短思考" }]), "brief")).toBe(
			"短思考",
		);
		const long = "一".repeat(200);
		const brief = visibleReasoning(message([{ type: "reasoning", text: long }]), "brief");
		expect(brief).not.toBeNull();
		expect(brief?.length).toBeLessThan(long.length);
	});

	it("full 返回完整思考", () => {
		expect(visibleReasoning(target, "full")).toBe("第一句。第二句。第三句。");
	});

	it("没有思考内容时三种模式都返回 null", () => {
		const empty = message([{ type: "text", text: "答案" }]);
		expect(visibleReasoning(empty, "hidden")).toBeNull();
		expect(visibleReasoning(empty, "brief")).toBeNull();
		expect(visibleReasoning(empty, "full")).toBeNull();
	});
});
