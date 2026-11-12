import type { ControlEvent, OverviewNode, OverviewSession, SessionState } from "@lework/api";

// OverviewState 是客户端侧的“快照 + 增量”视图。
//
// seen 记录已经计过数的 permission ID：同一条 permission.asked 可能因重连
// 被重放，没有这层去重，审批数会凭空变大。它只影响展示，回复永远以
// permission ID 提交给服务端。
export interface OverviewState {
	nodes: OverviewNode[];
	seen: Record<string, true>;
}

const SEP = "\u0000";

function permissionKey(nodeID: string, sessionID: string, permissionID: string): string {
	return `${nodeID}${SEP}${sessionID}${SEP}${permissionID}`;
}

function cloneSession(session: OverviewSession): OverviewSession {
	return { ...session };
}

function cloneNode(node: OverviewNode): OverviewNode {
	return { ...node, sessions: node.sessions.map(cloneSession) };
}

function sortNodes(nodes: OverviewNode[]): OverviewNode[] {
	const cloned = nodes.map(cloneNode);
	for (const node of cloned) {
		node.sessions.sort((a, b) => b.last_activity.localeCompare(a.last_activity));
	}
	cloned.sort((a, b) => a.node_id.localeCompare(b.node_id));
	return cloned;
}

export function createOverviewState(nodes: OverviewNode[] = []): OverviewState {
	return { nodes: sortNodes(nodes), seen: {} };
}

function permissionIDOf(data: unknown): string | null {
	if (!data || typeof data !== "object") return null;
	const id = (data as { id?: unknown }).id;
	return typeof id === "string" && id !== "" ? id : null;
}

function titleOf(data: unknown): string | null {
	if (!data || typeof data !== "object") return null;
	const title = (data as { title?: unknown }).title;
	return typeof title === "string" && title !== "" ? title : null;
}

function dropSeen(seen: Record<string, true>, prefix: string): void {
	for (const key of Object.keys(seen)) {
		if (key.startsWith(prefix)) delete seen[key];
	}
}

function applyState(session: OverviewSession, type: string): SessionState {
	switch (type) {
		case "session.execution.failed":
			return "error";
		case "session.execution.succeeded":
		case "session.execution.interrupted":
			return "idle";
		case "permission.asked":
		case "permission.replied":
		case "permission.resolved":
			return "waiting_permission";
		default:
			return session.state || "running";
	}
}

// applyEvent 把一个事件并入快照。返回新的 state，不修改入参。
export function applyEvent(state: OverviewState, event: ControlEvent): OverviewState {
	if (event.type === "agent.resync_required") {
		const prefix = `${event.node_id}${SEP}`;
		const seen = { ...state.seen };
		dropSeen(seen, prefix);
		return {
			nodes: state.nodes.map((node) =>
				node.node_id === event.node_id ? { ...node, sessions: [] } : node,
			),
			seen,
		};
	}

	if (!event.session_id) return state;

	const at = event.time || new Date().toISOString();
	const seen = { ...state.seen };
	const nodes = state.nodes.map(cloneNode);

	let node = nodes.find((candidate) => candidate.node_id === event.node_id);
	if (!node) {
		node = { node_id: event.node_id, name: event.node_id, connected: false, sessions: [] };
		nodes.push(node);
	}

	let session = node.sessions.find((candidate) => candidate.session_id === event.session_id);
	if (!session) {
		session = {
			session_id: event.session_id,
			state: "running",
			pending_permissions: 0,
			last_activity: at,
		};
		node.sessions.push(session);
	}
	session.last_activity = at;

	const key = (permissionID: string) => permissionKey(event.node_id, event.session_id as string, permissionID);

	switch (event.type) {
		case "permission.asked": {
			const permissionID = permissionIDOf(event.data);
			if (permissionID && !seen[key(permissionID)]) {
				seen[key(permissionID)] = true;
				session.pending_permissions += 1;
			}
			session.state = "waiting_permission";
			break;
		}

		case "permission.replied":
		case "permission.resolved": {
			const permissionID = permissionIDOf(event.data);
			if (permissionID) delete seen[key(permissionID)];
			// 没有 ID 的回复无法精确扣减，但也不能让计数卡住；
			// 下一次快照会给出权威值。
			session.pending_permissions = Math.max(0, session.pending_permissions - 1);
			session.state = session.pending_permissions > 0 ? "waiting_permission" : "running";
			break;
		}

		case "session.execution.failed":
		case "session.execution.succeeded":
		case "session.execution.interrupted":
			session.pending_permissions = 0;
			dropSeen(seen, `${event.node_id}${SEP}${event.session_id}${SEP}`);
			session.state = applyState(session, event.type);
			break;

		case "session.created":
		case "session.updated": {
			const title = titleOf(event.data);
			if (title) session.title = title;
			session.state = applyState(session, event.type);
			break;
		}

		default:
			session.state = applyState(session, event.type);
	}

	return { nodes: sortNodes(nodes), seen };
}
