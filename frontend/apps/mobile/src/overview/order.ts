import type { OverviewNode } from "@lework/api";

// orderNodes 决定总览的阅读顺序：正在等审批的节点永远排最前，
// 因为它们是唯一需要人立刻动手的；其余在线节点优先，最后按 ID 稳定排序。
export function orderNodes(nodes: OverviewNode[]): OverviewNode[] {
	const waiting = (node: OverviewNode) => node.sessions.some((s) => s.state === "waiting_permission");
	return [...nodes].sort((a, b) => {
		const waitingA = waiting(a) ? 0 : 1;
		const waitingB = waiting(b) ? 0 : 1;
		if (waitingA !== waitingB) return waitingA - waitingB;
		if (a.connected !== b.connected) return a.connected ? -1 : 1;
		return a.node_id.localeCompare(b.node_id);
	});
}
