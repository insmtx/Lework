import type { NodeView, OverviewSession } from "@lework/api";

// 待审批列表由客户端 join 总览快照得到：控制面保持无状态，不为推送或
// 徽标维护第二套会话索引。

/** PendingRow 是待审批页的一行：一个会话及其审批数量。 */
export interface PendingRow {
	nodeID: string;
	sessionID: string;
	title?: string;
	count: number;
}

export interface NodeSessions {
	node: NodeView;
	sessions: OverviewSession[];
}

/**
 * pendingRows 汇总所有有审批的会话。
 *
 * 在线节点优先（离线节点的审批无法立即处理），同为在线时审批多的在前；
 * 完全相同的排序再按会话 ID 稳定下来，避免页面每次刷新都重排。
 */
export function pendingRows(nodes: NodeSessions[]): PendingRow[] {
	const rows: PendingRow[] = [];
	const online = new Map<string, boolean>();
	for (const entry of nodes) {
		online.set(entry.node.id, entry.node.connected);
		for (const session of entry.sessions) {
			if (session.pending_permissions <= 0) continue;
			rows.push({
				nodeID: entry.node.id,
				sessionID: session.session_id,
				title: session.title,
				count: session.pending_permissions,
			});
		}
	}

	return rows.sort((left, right) => {
		const leftOnline = online.get(left.nodeID) ?? false;
		const rightOnline = online.get(right.nodeID) ?? false;
		if (leftOnline !== rightOnline) return leftOnline ? -1 : 1;
		if (left.count !== right.count) return right.count - left.count;
		if (left.nodeID !== right.nodeID) return left.nodeID.localeCompare(right.nodeID);
		return left.sessionID.localeCompare(right.sessionID);
	});
}
