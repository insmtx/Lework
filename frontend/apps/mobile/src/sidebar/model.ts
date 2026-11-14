import type { ProjectSummary } from "@lework/api";

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
 */
export function buildSidebarGroups(
	projects: ProjectSummary[],
	sessions: ProjectSessionRef[],
	onlineNodeIDs: string[],
	inboxKey?: string | null,
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
			recent: sorted.slice(0, RECENT_SESSION_LIMIT),
			total: sorted.length,
		};
	});
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
