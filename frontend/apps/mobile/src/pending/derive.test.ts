import type { NodeView, OverviewSession } from "@lework/api";
import { describe, expect, it } from "vitest";
import { pendingRows } from "./derive";

function node(id: string, connected: boolean): NodeView {
	return {
		id,
		name: id,
		os: "linux",
		arch: "amd64",
		agent_version: "test",
		capabilities: [],
		connected,
		last_seen: "2026-11-12T00:00:00Z",
	};
}

function session(id: string, pending: number): OverviewSession {
	return {
		session_id: id,
		title: id,
		state: pending > 0 ? "waiting_permission" : "running",
		pending_permissions: pending,
		last_activity: "2026-11-12T00:00:00Z",
	};
}

describe("pendingRows", () => {
	it("忽略没有审批的会话", () => {
		const rows = pendingRows([{ node: node("n1", true), sessions: [session("s1", 0)] }]);
		expect(rows).toEqual([]);
	});

	it("审批多的在前", () => {
		const rows = pendingRows([
			{
				node: node("n1", true),
				sessions: [session("one", 1), session("three", 3), session("two", 2)],
			},
		]);
		expect(rows.map((row) => row.sessionID)).toEqual(["three", "two", "one"]);
	});

	it("在线节点排在离线节点之前，即使审批更少", () => {
		const rows = pendingRows([
			{ node: node("offline", false), sessions: [session("s-offline", 5)] },
			{ node: node("online", true), sessions: [session("s-online", 1)] },
		]);
		expect(rows.map((row) => row.sessionID)).toEqual(["s-online", "s-offline"]);
	});
});
