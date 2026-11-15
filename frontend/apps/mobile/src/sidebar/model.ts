import type { ProjectSummary, SessionState } from "@lework/api";

import { orderProjects } from "../projects/model";
import {
	type ProjectSessionRef,
	selectProjectSessions,
	sortSessionsByActivity,
} from "../sessions/select";

/** RECENT_SESSION_LIMIT 是侧边栏每个项目最多露出的会话数。 */
export const RECENT_SESSION_LIMIT = 3;

/** SidebarGroup 是侧边栏里的一个项目分组。 */
export interface SidebarGroup {
	project: ProjectSummary;
	/** 该项目最近活动的会话，最多 RECENT_SESSION_LIMIT 条。 */
	recent: ProjectSessionRef[];
	/** 该项目在当前在线节点上的会话总数，决定是否显示「全部会话」。 */
	total: number;
}

/**
 * buildSidebarGroups 把跨节点会话按项目分组，供侧边栏渲染。
 *
 * 侧边栏只回答「最近在哪个项目做了什么」：项目按最后活动时间倒序，
 * 每个项目只留最近几条会话；完整列表在项目页。离线节点的会话不出现，
 * 因为控制面没有缓存会话快照，点进去也只会失败。
 *
 * 名额分配是「先选后截断」：等待审批的会话无条件进前 RECENT_SESSION_LIMIT，
 * 剩余名额按活动时间补。否则排第 4 的待审批会话永远看不见，而它恰恰是
 * 唯一需要人立刻动手的。
 */
export function buildSidebarGroups(
	projects: ProjectSummary[],
	sessions: ProjectSessionRef[],
	onlineNodeIDs: string[],
	inboxKey?: string | null,
	stateOf?: (nodeID: string, sessionID: string) => SessionState | undefined,
): SidebarGroup[] {
	const online = new Set(onlineNodeIDs);

	return orderProjects(projects, inboxKey).map((project) => {
		const projectNodeIDs = new Set(project.nodes.map((node) => node.node_id));
		const candidates = sessions.filter(
			(ref) => online.has(ref.nodeID) && projectNodeIDs.has(ref.nodeID),
		);
		const sorted = sortSessionsByActivity(
			selectProjectSessions(candidates, project.key, project.canonical),
		);

		return {
			project,
			recent: selectRecent(sorted, stateOf),
			total: sorted.length,
		};
	});
}

// selectRecent 在活动序基础上把等待审批的会话提前，并按名额截断。
function selectRecent(
	sorted: ProjectSessionRef[],
	stateOf?: (nodeID: string, sessionID: string) => SessionState | undefined,
): ProjectSessionRef[] {
	if (!stateOf) return sorted.slice(0, RECENT_SESSION_LIMIT);

	const waiting = sorted.filter(
		(ref) => stateOf(ref.nodeID, ref.session.id) === "waiting_permission",
	);
	const rest = sorted.filter((ref) => stateOf(ref.nodeID, ref.session.id) !== "waiting_permission");
	const head = waiting.slice(0, RECENT_SESSION_LIMIT);
	return [...head, ...rest.slice(0, RECENT_SESSION_LIMIT - head.length)];
}

/**
 * formatRelativeTime 把时间戳压缩成侧边栏宽度内能读的一小段。
 *
 * 刚刚 / 分钟 / 小时 / 昨天 / 天，超过一周退回月日；空值与非法值返回空串，
 * 让调用方直接省略，而不是显示 Invalid Date。
 */
export function formatRelativeTime(value: string | undefined, now = Date.now()): string {
	if (!value) return "";
	const at = Date.parse(value);
	if (Number.isNaN(at)) return "";

	const diff = now - at;
	if (diff < 60_000) return "刚刚";

	const minutes = Math.floor(diff / 60_000);
	if (minutes < 60) return `${minutes} 分钟前`;

	const hours = Math.floor(minutes / 60);
	if (hours < 24) return `${hours} 小时前`;

	const days = Math.floor(hours / 24);
	if (days === 1) return "昨天";
	if (days < 7) return `${days} 天前`;

	const date = new Date(at);
	return `${date.getMonth() + 1} 月 ${date.getDate()} 日`;
}
