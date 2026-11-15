import type { NodeView } from "@lework/api";
import { describe, expect, it } from "vitest";

import { sortNodes } from "./order";

function node(name: string, connected: boolean, id = name): NodeView {
	return {
		id,
		name,
		os: "linux",
		arch: "amd64",
		agent_version: "0.1.0",
		capabilities: [],
		connected,
		last_seen: "2026-11-15T00:00:00Z",
	};
}

describe("sortNodes", () => {
	it("在线的排在离线前面", () => {
		const sorted = sortNodes([node("a", false), node("b", true)]);
		expect(sorted.map((item) => item.name)).toEqual(["b", "a"]);
	});

	it("同状态内按名称升序，名称相同时按 ID", () => {
		const sorted = sortNodes([
			node("build", true, "n3"),
			node("alpha", true, "n2"),
			node("alpha", true, "n1"),
		]);
		expect(sorted.map((item) => item.id)).toEqual(["n1", "n2", "n3"]);
	});

	it("不修改传入的数组", () => {
		const input = [node("b", true), node("a", false)];
		const sorted = sortNodes(input);
		expect(input.map((item) => item.name)).toEqual(["b", "a"]);
		expect(sorted).not.toBe(input);
	});
});
