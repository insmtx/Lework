import type { NodeView, Session } from "@lework/api";

export interface NodeSessions {
	node: NodeView;
	sessions: Session[];
}

// groupSessions 把并发拉取的结果拼回渲染顺序：只显示在线且确实有会话的
// 节点。某个节点拉取失败（byNode 中缺失）不会影响其他节点。
export function groupSessions(nodes: NodeView[], byNode: Record<string, Session[]>): NodeSessions[] {
	const out: NodeSessions[] = [];
	for (const node of nodes) {
		if (!node.connected) continue;
		const sessions = byNode[node.id];
		if (!sessions || sessions.length === 0) continue;
		out.push({ node, sessions });
	}
	return out;
}
