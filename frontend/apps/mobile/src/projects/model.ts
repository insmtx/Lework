import type { ProjectSummary, Session } from "@lework/api";

/** INBOX_ALIAS 是「我的」项目卡的固定标题；用户别名以后可覆盖。 */
export const INBOX_ALIAS = "我的";

/** projectDisplayName 优先用用户别名，否则取 canonical 的最后一段。 */
export function projectDisplayName(project: { canonical: string; alias?: string }): string {
	const alias = project.alias?.trim();
	if (alias) return alias;
	const segments = project.canonical.replace(/\/+$/, "").split("/");
	return segments[segments.length - 1] || project.canonical;
}

/**
 * projectKeyOf 把 OpenCode 的 project id 转成聚合键。
 *
 * OpenCode 把所有无提交的 git 仓库都报成 "global"，只用 id 会让它们互相
 * 污染，因此这时拼上 canonical。与后端 projectKeyOf 必须保持一致。
 */
export function projectKeyOf(project: { id: string; canonical: string }): string {
	return project.id === "global" ? `${project.id}:${project.canonical}` : project.id;
}

/** isPathPrefix 判断 prefix 是否是 path 的目录前缀，按 / 分段避免 /w/rep 匹配 /w/repo。 */
export function isPathPrefix(prefix: string, path: string): boolean {
	const clean = (value: string) => value.replace(/\/+$/, "") || "/";
	const parent = clean(prefix);
	const target = clean(path);
	if (parent === "/") return target.startsWith("/");
	return target === parent || target.startsWith(`${parent}/`);
}

/**
 * orderProjects 把「我的」置顶，其余按最后活动时间倒序。
 *
 * inboxKey 为空时退化为纯时间排序；相同时间用 key 稳定排序，避免列表
 * 在两次刷新之间跳动。
 */
export function orderProjects(
	projects: ProjectSummary[],
	inboxKey?: string | null,
): ProjectSummary[] {
	return [...projects].sort((left, right) => {
		if (inboxKey) {
			const leftInbox = left.key === inboxKey;
			const rightInbox = right.key === inboxKey;
			if (leftInbox !== rightInbox) return leftInbox ? -1 : 1;
		}
		const leftTime = Date.parse(left.last_seen ?? "");
		const rightTime = Date.parse(right.last_seen ?? "");
		if (!Number.isNaN(leftTime) && !Number.isNaN(rightTime) && leftTime !== rightTime) {
			return rightTime - leftTime;
		}
		return left.key.localeCompare(right.key);
	});
}

export interface InboxResolution {
	project: ProjectSummary | null;
	nodeID: string | null;
	unresolved: boolean;
}

/**
 * resolveInbox 在聚合项目里解析「我的」实际指向哪个项目。
 *
 * 「我的」不是 OpenCode 的特殊项目，而是一条偏好：某个节点上的目录。
 * 目录可能位于某个仓库内部，此时「我的」就是那个仓库——因此取 canonical
 * 是目录前缀且最深的项目，而不是要求路径完全相等。
 *
 * onlineNodeIDs 缺省时从项目节点信息里推断，便于纯逻辑测试与调用方简化。
 */
export function resolveInbox(
	projects: ProjectSummary[],
	dirByNode: Record<string, string>,
	onlineNodeIDs?: string[],
): InboxResolution {
	const entries = Object.entries(dirByNode).filter(([, dir]) => dir.trim() !== "");
	if (entries.length === 0) return { project: null, nodeID: null, unresolved: true };

	const online = new Set(
		onlineNodeIDs ??
			projects.flatMap((project) =>
				project.nodes.filter((node) => node.online).map((node) => node.node_id),
			),
	);

	const preferred = entries.find(([nodeID]) => online.has(nodeID)) ?? entries[0];
	if (!preferred) return { project: null, nodeID: null, unresolved: true };

	const [nodeID, dir] = preferred;
	let best: ProjectSummary | null = null;
	for (const project of projects) {
		const onNode = project.nodes.some((node) => node.node_id === nodeID);
		if (!onNode || !isPathPrefix(project.canonical, dir)) continue;
		if (!best || project.canonical.length > best.canonical.length) best = project;
	}

	return { project: best, nodeID, unresolved: best === null };
}

export interface TargetResolution {
	nodeID: string | null;
	reason: "ok" | "no-node";
}

/**
 * resolveTarget 决定在哪个节点上新建会话。
 *
 * 优先用户指定的主节点；主节点离线时退回该项目最近出现过的在线节点；
 * 全离线时返回 no-node，让界面禁用发送而不是静默失败。
 */
export function resolveTarget(
	project: ProjectSummary,
	onlineNodeIDs: string[],
): TargetResolution {
	const online = new Set(onlineNodeIDs);
	if (project.primary_node_id && online.has(project.primary_node_id)) {
		return { nodeID: project.primary_node_id, reason: "ok" };
	}

	let best: { nodeID: string; at: number } | null = null;
	for (const node of project.nodes) {
		if (!online.has(node.node_id)) continue;
		const at = Date.parse(node.last_seen ?? "");
		const stamp = Number.isNaN(at) ? 0 : at;
		if (!best || stamp > best.at) best = { nodeID: node.node_id, at: stamp };
	}
	if (!best) return { nodeID: null, reason: "no-node" };
	return { nodeID: best.nodeID, reason: "ok" };
}

export interface SessionRef {
	nodeID: string;
	session: Session;
}

/**
 * groupSessionsByProject 按项目聚合键给会话分组。
 *
 * 用 projectID 而不是目录前缀：会话可以在项目子目录里创建，前缀过滤会漏。
 */
export function groupSessionsByProject(
	sessions: SessionRef[],
	keyOf: (session: Session) => string,
): Record<string, SessionRef[]> {
	const out: Record<string, SessionRef[]> = {};
	for (const ref of sessions) {
		const key = keyOf(ref.session);
		(out[key] ??= []).push(ref);
	}
	return out;
}
