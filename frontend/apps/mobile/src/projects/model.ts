import type { ProjectSummary, Session } from "@lework/api";

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
export function resolveTarget(project: ProjectSummary, onlineNodeIDs: string[]): TargetResolution {
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

export type TaskTargetReason = "ok" | "no-node" | "no-dir";

/** TaskTarget 是首页「直接发任务」实际落到哪个节点、哪个目录。 */
export interface TaskTarget {
	nodeID: string | null;
	directory: string;
	/** 草稿 scope 与项目身份用的键；项目已存在时就是聚合键。 */
	scopeKey: string;
	reason: TaskTargetReason;
}

/**
 * resolveTaskTarget 决定首页输入的任务发往哪里。
 *
 * 首页只回答「现在要做什么」，不该让用户先选机器：项目已经存在时只看项目
 * 本身（主节点 → 最近在线节点），偏好目录所在节点离线也不影响发送。项目
 * 还没创建时才退回偏好目录，用第一条消息把它创建出来。
 */
export function resolveTaskTarget(
	inbox: InboxResolution,
	dirByNode: Record<string, string>,
	onlineNodeIDs: string[],
): TaskTarget {
	if (inbox.project) {
		const target = resolveTarget(inbox.project, onlineNodeIDs);
		return {
			nodeID: target.nodeID,
			directory: inbox.project.canonical,
			scopeKey: inbox.project.key,
			reason: target.reason === "ok" ? "ok" : "no-node",
		};
	}

	const dir = inbox.nodeID ? (dirByNode[inbox.nodeID] ?? "") : "";
	if (!inbox.nodeID || dir === "") {
		return { nodeID: null, directory: "", scopeKey: "", reason: "no-dir" };
	}

	const online = onlineNodeIDs.includes(inbox.nodeID);
	return {
		nodeID: online ? inbox.nodeID : null,
		directory: dir,
		scopeKey: `inbox:${inbox.nodeID}:${dir}`,
		reason: online ? "ok" : "no-node",
	};
}

/**
 * findProjectForSession 反查会话属于哪个聚合项目。
 *
 * 会话只带 OpenCode 的 projectID，而「global」被多个无提交仓库共享，
 * 因此 global 还要用目录前缀挑出最深的那一个。返回 null 表示项目尚未
 * 聚合出来（例如刚创建完会话、控制面还没来得及刷新）。
 */
export function findProjectForSession(
	session: Session,
	projects: ProjectSummary[],
): ProjectSummary | null {
	const directory = session.directory ?? "";
	let best: ProjectSummary | null = null;

	for (const project of projects) {
		const sameProject = session.projectID
			? session.projectID === "global"
				? project.id === "global" && isPathPrefix(project.canonical, directory)
				: project.key === session.projectID
			: isPathPrefix(project.canonical, directory);
		if (!sameProject) continue;
		if (!best || project.canonical.length > best.canonical.length) best = project;
	}

	return best;
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
		const bucket = out[key];
		if (bucket) {
			bucket.push(ref);
		} else {
			out[key] = [ref];
		}
	}
	return out;
}
