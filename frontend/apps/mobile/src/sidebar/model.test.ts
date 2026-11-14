import type { ProjectSummary, Session } from "@lework/api";
import { describe, expect, it } from "vitest";

import { buildSidebarGroups, formatRelativeTime, RECENT_SESSION_LIMIT } from "./model";

function project(overrides: Partial<ProjectSummary> = {}): ProjectSummary {
	return {
		key: "proj-a",
		id: "proj-a",
		canonical: "/work/a",
		nodes: [{ node_id: "node-1", online: true, last_seen: "2026-11-14T10:00:00Z" }],
		last_seen: "2026-11-14T10:00:00Z",
		...overrides,
	};
}

function session(id: string, at: string, overrides: Partial<Session> = {}): Session {
	return {
		id,
		title: id,
		projectID: "proj-a",
		directory: "/work/a",
		updatedAt: at,
		...overrides,
	};
}

describe("buildSidebarGroups", () => {
	it("只保留最近三条会话，但仍统计总数", () => {
		const sessions = [
			{ nodeID: "node-1", session: session("s1", "2026-11-14T10:00:00Z") },
			{ nodeID: "node-1", session: session("s2", "2026-11-14T11:00:00Z") },
			{ nodeID: "node-1", session: session("s3", "2026-11-14T12:00:00Z") },
			{ nodeID: "node-1", session: session("s4", "2026-11-14T13:00:00Z") },
		];

		const [group] = buildSidebarGroups([project()], sessions, ["node-1"]);

		expect(group.recent.map((row) => row.session.id)).toEqual(["s4", "s3", "s2"]);
		expect(group.recent).toHaveLength(RECENT_SESSION_LIMIT);
		expect(group.total).toBe(4);
	});

	it("离线节点的会话不进侧边栏：点进去只会失败", () => {
		const sessions = [
			{ nodeID: "node-1", session: session("on", "2026-11-14T10:00:00Z") },
			{
				nodeID: "node-2",
				session: session("off", "2026-11-14T11:00:00Z", { directory: "/work/a" }),
			},
		];
		const target = project({
			nodes: [
				{ node_id: "node-1", online: true, last_seen: "" },
				{ node_id: "node-2", online: false, last_seen: "" },
			],
		});

		const [group] = buildSidebarGroups([target], sessions, ["node-1"]);

		expect(group.recent.map((row) => row.session.id)).toEqual(["on"]);
		expect(group.total).toBe(1);
	});

	it("项目按最后活动时间倒序，「我的」保持置顶", () => {
		const older = project({
			key: "older",
			canonical: "/work/older",
			last_seen: "2026-11-01T00:00:00Z",
			nodes: [{ node_id: "node-1", online: true, last_seen: "" }],
		});
		const newer = project({
			key: "newer",
			canonical: "/work/newer",
			last_seen: "2026-11-14T00:00:00Z",
			nodes: [{ node_id: "node-1", online: true, last_seen: "" }],
		});
		const inbox = project({
			key: "inbox",
			canonical: "/work/inbox",
			last_seen: "2026-10-01T00:00:00Z",
			nodes: [{ node_id: "node-1", online: true, last_seen: "" }],
		});

		const groups = buildSidebarGroups([older, newer, inbox], [], ["node-1"], "inbox");

		expect(groups.map((group) => group.project.key)).toEqual(["inbox", "newer", "older"]);
	});

	it("按 projectID 归组，不在项目节点上的会话不混入", () => {
		const sessions = [
			{ nodeID: "node-1", session: session("mine", "2026-11-14T10:00:00Z") },
			{
				nodeID: "node-1",
				session: session("other", "2026-11-14T11:00:00Z", { projectID: "proj-b" }),
			},
		];

		const [group] = buildSidebarGroups([project()], sessions, ["node-1"]);

		expect(group.recent.map((row) => row.session.id)).toEqual(["mine"]);
	});
});

describe("formatRelativeTime", () => {
	const now = new Date(2026, 10, 14, 12, 0, 0).getTime();

	it("一分钟内显示刚刚", () => {
		expect(formatRelativeTime(new Date(now - 30_000).toISOString(), now)).toBe("刚刚");
	});

	it("按分钟、小时、天压缩", () => {
		expect(formatRelativeTime(new Date(now - 5 * 60_000).toISOString(), now)).toBe("5 分钟前");
		expect(formatRelativeTime(new Date(now - 3 * 3_600_000).toISOString(), now)).toBe("3 小时前");
		expect(formatRelativeTime(new Date(now - 25 * 3_600_000).toISOString(), now)).toBe("昨天");
		expect(formatRelativeTime(new Date(now - 3 * 86_400_000).toISOString(), now)).toBe("3 天前");
	});

	it("超过一周显示月日", () => {
		const at = new Date(2026, 9, 20, 9, 0, 0).toISOString();
		expect(formatRelativeTime(at, now)).toBe("10 月 20 日");
	});

	it("空值和非法值返回空串", () => {
		expect(formatRelativeTime(undefined, now)).toBe("");
		expect(formatRelativeTime("not-a-date", now)).toBe("");
	});
});
