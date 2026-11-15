import { describe, expect, it } from "vitest";

import { needsRefetch } from "./structural";

describe("needsRefetch", () => {
	it("delta 不触发重拉", () => {
		expect(needsRefetch("session.text.delta")).toBe(false);
		expect(needsRefetch("session.reasoning.delta")).toBe(false);
		expect(needsRefetch("session.tool.input.delta")).toBe(false);
	});

	it("工具与执行事件触发重拉", () => {
		expect(needsRefetch("session.tool.called")).toBe(true);
		expect(needsRefetch("session.tool.progress")).toBe(true);
		expect(needsRefetch("session.execution.started")).toBe(true);
		expect(needsRefetch("session.execution.succeeded")).toBe(true);
	});

	it("文本结束与审批/会话结构变化触发重拉", () => {
		expect(needsRefetch("session.text.ended")).toBe(true);
		expect(needsRefetch("session.created")).toBe(true);
		expect(needsRefetch("session.updated")).toBe(true);
		expect(needsRefetch("permission.asked")).toBe(true);
		expect(needsRefetch("permission.replied")).toBe(true);
	});

	it("未知类型保守重拉", () => {
		expect(needsRefetch("something.new")).toBe(true);
		expect(needsRefetch("agent.resync_required")).toBe(true);
	});
});
