import { describe, expect, it } from "vitest";
import type { ControlEvent, OverviewNode } from "@lework/api";
import { applyEvent, createOverviewState } from "./merge";

const base: OverviewNode[] = [
	{
		node_id: "node_1",
		name: "dev-box",
		connected: true,
		sessions: [
			{
				session_id: "ses_1",
				state: "running",
				pending_permissions: 0,
				last_activity: "2026-11-12T00:00:00Z",
			},
		],
	},
];

function asked(id: string, session = "ses_1"): ControlEvent {
	return {
		node_id: "node_1",
		session_id: session,
		seq: 1,
		type: "permission.asked",
		data: { id, action: "bash" },
		time: "2026-11-12T00:01:00Z",
	};
}

function event(overrides: Partial<ControlEvent>): ControlEvent {
	return {
		node_id: "node_1",
		session_id: "ses_1",
		seq: 1,
		type: "message.part.updated",
		time: "2026-11-12T00:01:00Z",
		...overrides,
	};
}

function sessionOf(nodes: OverviewNode[], nodeID = "node_1", sessionID = "ses_1") {
	return nodes.find((n) => n.node_id === nodeID)?.sessions.find((s) => s.session_id === sessionID);
}

describe("applyEvent", () => {
	it("同一权限重复到达只计一次", () => {
		let state = createOverviewState(base);
		state = applyEvent(state, asked("perm_1"));
		state = applyEvent(state, asked("perm_1"));

		const session = sessionOf(state.nodes);
		expect(session?.state).toBe("waiting_permission");
		expect(session?.pending_permissions).toBe(1);
	});

	it("不同权限累加，回复后归零并回到 running", () => {
		let state = createOverviewState(base);
		state = applyEvent(state, asked("perm_1"));
		state = applyEvent(state, asked("perm_2"));
		expect(sessionOf(state.nodes)?.pending_permissions).toBe(2);

		state = applyEvent(state, event({ type: "permission.replied", data: { id: "perm_1" } }));
		expect(sessionOf(state.nodes)?.pending_permissions).toBe(1);
		expect(sessionOf(state.nodes)?.state).toBe("waiting_permission");

		state = applyEvent(state, event({ type: "permission.resolved", data: { id: "perm_2" } }));
		expect(sessionOf(state.nodes)?.pending_permissions).toBe(0);
		expect(sessionOf(state.nodes)?.state).toBe("running");
	});

	it("执行结束进入 idle / error 并清空待审批", () => {
		let state = applyEvent(createOverviewState(base), asked("perm_1"));
		state = applyEvent(state, event({ type: "session.execution.succeeded" }));
		expect(sessionOf(state.nodes)?.state).toBe("idle");
		expect(sessionOf(state.nodes)?.pending_permissions).toBe(0);

		state = applyEvent(state, event({ type: "session.execution.failed" }));
		expect(sessionOf(state.nodes)?.state).toBe("error");
	});

	it("新会话从 running 开始并可更新标题", () => {
		let state = applyEvent(createOverviewState(base), event({
			session_id: "ses_2",
			type: "session.created",
			data: { title: "refactor auth" },
		}));
		expect(sessionOf(state.nodes, "node_1", "ses_2")?.state).toBe("running");
		expect(sessionOf(state.nodes, "node_1", "ses_2")?.title).toBe("refactor auth");
	});

	it("resync 清空该节点会话但保留节点", () => {
		const state = applyEvent(createOverviewState(base), event({ type: "agent.resync_required", session_id: undefined }));
		expect(state.nodes).toHaveLength(1);
		expect(state.nodes[0]?.node_id).toBe("node_1");
		expect(state.nodes[0]?.sessions).toHaveLength(0);
	});

	it("不修改输入并按节点/活跃时间排序", () => {
		const input: OverviewNode[] = [
			{ node_id: "node_2", name: "b", connected: false, sessions: [] },
			{ node_id: "node_1", name: "a", connected: true, sessions: [
				{ session_id: "old", state: "idle", pending_permissions: 0, last_activity: "2026-11-12T00:00:00Z" },
				{ session_id: "new", state: "running", pending_permissions: 0, last_activity: "2026-11-12T00:02:00Z" },
			] },
		];
		const snapshot = structuredClone(input);
		const state = applyEvent(createOverviewState(input), event({ session_id: "new", type: "message.part.updated" }));

		expect(input).toEqual(snapshot);
		expect(state.nodes.map((n) => n.node_id)).toEqual(["node_1", "node_2"]);
		expect(state.nodes[0]?.sessions.map((s) => s.session_id)).toEqual(["new", "old"]);
	});
});
