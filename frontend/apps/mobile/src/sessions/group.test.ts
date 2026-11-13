import { describe, expect, it } from "vitest";
import type { NodeView, Session } from "@lework/api";
import { groupSessions } from "./group";

function node(id: string, connected: boolean): NodeView {
	return {
		id,
		name: id,
		os: "linux",
		arch: "amd64",
		agent_version: "0.1.0",
		capabilities: [],
		connected,
		last_seen: "2026-11-12T00:00:00Z",
	};
}

function session(id: string): Session {
	return { id, title: id };
}

describe("groupSessions", () => {
	it("只保留在线节点且有会话的分组，顺序跟随节点", () => {
		const grouped = groupSessions(
			[node("node_a", true), node("node_b", false), node("node_c", true)],
			{ node_a: [session("ses_1")], node_c: [] },
		);
		expect(grouped.map((item) => item.node.id)).toEqual(["node_a"]);
		expect(grouped[0]?.sessions.map((item) => item.id)).toEqual(["ses_1"]);
	});

	it("缺失会话的节点（拉取失败）不出现也不报错", () => {
		expect(groupSessions([node("node_a", true)], {})).toEqual([]);
	});
});
