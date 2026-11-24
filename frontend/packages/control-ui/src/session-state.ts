import type { OverviewNode, SessionState } from "./types";

/**
 * affectsOverview 判断一条事件是否需要重拉总览快照。
 *
 * 只覆盖会改变执行状态的事件族；message.* 是高频流式事件，重拉会把控制面
 * 打成轮询，状态本身仍由服务端投影维护。
 */
export function affectsOverview(type: string): boolean {
	return (
		type.startsWith("session.") ||
		type.startsWith("permission.") ||
		type.startsWith("form.") ||
		type === "agent.resync_required"
	);
}

/** overviewStatesByNode 把总览快照摊平成 nodeID → sessionID → 状态。 */
export function overviewStatesByNode(
	nodes: OverviewNode[],
): Record<string, Record<string, SessionState>> {
	const states: Record<string, Record<string, SessionState>> = {};
	for (const node of nodes) {
		const sessions: Record<string, SessionState> = {};
		for (const session of node.sessions) {
			sessions[session.session_id] = session.state;
		}
		states[node.node_id] = sessions;
	}
	return states;
}

/**
 * sessionStateClass 把执行状态映射成会话行的背景 class。
 *
 * running/waiting 用透明渐变跑马灯，error 用静态淡红底：静止的底色不会
 * 被误读成“还在跑”。idle/offline/未知一律不加背景，避免整列表发绿。
 */
export function sessionStateClass(state?: SessionState): string {
	switch (state) {
		case "running":
			return "lework-session-sweep-running";
		case "waiting_permission":
			return "lework-session-sweep-waiting";
		case "error":
			return "lework-session-sweep-error";
		default:
			return "";
	}
}
