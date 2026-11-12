import { describe, expect, it } from "vitest";
import type { OverviewNode } from "@lework/api";
import { orderNodes } from "./order";

function node(id: string, connected: boolean, states: ("running" | "waiting_permission")[]): OverviewNode {
	return {
		node_id: id,
		name: id,
		connected,
		sessions: states.map((state, index) => ({
			session_id: `ses_${index}`,
			state,
			pending_permissions: state === "waiting_permission" ? 1 : 0,
			last_activity: "2026-11-12T00:00:00Z",
		})),
	};
}

describe("orderNodes", () => {
	it("等审批的节点最前，其余在线优先、再按 ID", () => {
		const input = [node("node_c", true, ["running"]), node("node_b", false, ["running"]), node("node_a", false, ["waiting_permission"])];
		const snapshot = structuredClone(input);

		expect(orderNodes(input).map((n) => n.node_id)).toEqual(["node_a", "node_c", "node_b"]);
		expect(input).toEqual(snapshot);
	});
});
