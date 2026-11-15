import { describe, expect, it } from "vitest";

import { resolveExpanded, type ToolCardVisibility } from "./collapse";

function state(overrides: Partial<ToolCardVisibility> = {}): ToolCardVisibility {
	return {
		tone: "running",
		hasFollowingContent: false,
		preferExpanded: false,
		manual: false,
		expanded: false,
		ranDuringMount: true,
		...overrides,
	};
}

describe("resolveExpanded", () => {
	it("运行中默认展开，让进度和输出可见", () => {
		expect(resolveExpanded(state())).toBe(true);
	});

	it("用户手动收起后不再被运行状态强行展开", () => {
		expect(resolveExpanded(state({ manual: true, expanded: false }))).toBe(false);
	});

	it("刚执行完且仍是最后一段内容时保持展开", () => {
		expect(resolveExpanded(state({ tone: "ok", expanded: true, hasFollowingContent: false }))).toBe(
			true,
		);
	});

	it("后面出现新内容后自动折叠，把空间让给下一段", () => {
		expect(resolveExpanded(state({ tone: "ok", expanded: true, hasFollowingContent: true }))).toBe(
			false,
		);
	});

	it("打开历史会话时，已完成且不是本次运行产生的卡片保持折叠", () => {
		expect(
			resolveExpanded(
				state({ tone: "ok", expanded: true, ranDuringMount: false, hasFollowingContent: false }),
			),
		).toBe(false);
	});

	it("偏好「默认展开」时完成后仍保持展开", () => {
		expect(
			resolveExpanded(state({ tone: "ok", preferExpanded: true, hasFollowingContent: true })),
		).toBe(true);
	});

	it("用户手动展开过的卡片不会被自动折叠", () => {
		expect(
			resolveExpanded(
				state({
					tone: "ok",
					manual: true,
					expanded: true,
					ranDuringMount: false,
					hasFollowingContent: true,
				}),
			),
		).toBe(true);
	});
});
