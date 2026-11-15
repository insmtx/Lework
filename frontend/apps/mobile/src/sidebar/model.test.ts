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

	it("项目按组内最新会话时间倒序，而不是节点心跳时间", () => {
		// 控制面给项目盖的 last_seen 来自节点心跳：同一节点上所有项目几乎
		// 同时刷新，只按它排序会永远停在同一个名字序。会话的 updatedAt 才是
		// 「最近在这个项目里做了什么」的真实依据。
		const alpha = project({
			key: "alpha",
			canonical: "/work/alpha",
			last_seen: "2026-11-14T12:00:00Z",
			nodes: [{ node_id: "node-1", online: true, last_seen: "" }],
		});
		const beta = project({
			key: "beta",
			canonical: "/work/beta",
			last_seen: "2026-11-14T12:00:00Z",
			nodes: [{ node_id: "node-1", online: true, last_seen: "" }],
		});
		const sessions = [
			{
				nodeID: "node-1",
				session: session("a1", "2026-11-14T09:00:00Z", {
					projectID: "alpha",
					directory: "/work/alpha",
				}),
			},
			{
				nodeID: "node-1",
				session: session("b1", "2026-11-14T11:00:00Z", {
					projectID: "beta",
					directory: "/work/beta",
				}),
			},
		];

		const groups = buildSidebarGroups([alpha, beta], sessions, ["node-1"]);

		expect(groups.map((group) => group.project.key)).toEqual(["beta", "alpha"]);
		expect(groups[0].lastActivityAt).toBe(Date.parse("2026-11-14T11:00:00Z"));
	});

	it("最新会话不在前三条时，项目排序仍按它的时间", () => {
		// recent 会被「等待审批优先」重排，所以项目序不能取 recent[0]，
		// 必须取全组会话的最大 updatedAt。
		const quiet = project({
			key: "quiet",
			canonical: "/work/quiet",
			last_seen: "2026-11-14T12:00:00Z",
			nodes: [{ node_id: "node-1", online: true, last_seen: "" }],
		});
		const busy = project({
			key: "busy",
			canonical: "/work/busy",
			last_seen: "2026-11-14T12:00:00Z",
			nodes: [{ node_id: "node-1", online: true, last_seen: "" }],
		});
		const quietSession = (id: string, at: string) => ({
			nodeID: "node-1",
			session: session(id, at, { projectID: "quiet", directory: "/work/quiet" }),
		});
		const busySession = (id: string, at: string) => ({
			nodeID: "node-1",
			session: session(id, at, { projectID: "busy", directory: "/work/busy" }),
		});
		const sessions = [
			quietSession("q1", "2026-11-14T13:00:00Z"),
			busySession("b1", "2026-11-14T10:00:00Z"),
			busySession("b2", "2026-11-14T10:30:00Z"),
			busySession("b3", "2026-11-14T11:00:00Z"),
			quietSession("q2", "2026-11-14T09:00:00Z"),
		];
		const stateOf = (_nodeID: string, sessionID: string) =>
			sessionID === "b1" ? ("waiting_permission" as const) : undefined;

		const groups = buildSidebarGroups([quiet, busy], sessions, ["node-1"], undefined, stateOf);

		expect(groups.map((group) => group.project.key)).toEqual(["quiet", "busy"]);
	});

	it("没有会话的项目排在后面，顺序退回 last_seen 与 key", () => {
		const emptyA = project({
			key: "empty-a",
			canonical: "/work/empty-a",
			last_seen: "2026-11-14T12:00:00Z",
			nodes: [{ node_id: "node-1", online: true, last_seen: "" }],
		});
		const emptyB = project({
			key: "empty-b",
			canonical: "/work/empty-b",
			last_seen: "2026-11-14T12:00:00Z",
			nodes: [{ node_id: "node-1", online: true, last_seen: "" }],
		});
		const active = project({
			key: "active",
			canonical: "/work/active",
			last_seen: "2026-11-01T00:00:00Z",
			nodes: [{ node_id: "node-1", online: true, last_seen: "" }],
		});
		const sessions = [
			{
				nodeID: "node-1",
				session: session("a1", "2026-11-14T09:00:00Z", {
					projectID: "active",
					directory: "/work/active",
				}),
			},
		];

		const groups = buildSidebarGroups([emptyB, active, emptyA], sessions, ["node-1"]);

		expect(groups.map((group) => group.project.key)).toEqual(["active", "empty-a", "empty-b"]);
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

	it("等待审批的会话即使排第 4 也进入 recent", () => {
		const sessions = [
			{ nodeID: "node-1", session: session("s1", "2026-11-14T10:00:00Z") },
			{ nodeID: "node-1", session: session("s2", "2026-11-14T11:00:00Z") },
			{ nodeID: "node-1", session: session("s3", "2026-11-14T12:00:00Z") },
			{ nodeID: "node-1", session: session("waiting", "2026-11-14T09:00:00Z") },
		];
		const stateOf = (nodeID: string, sessionID: string) =>
			nodeID === "node-1" && sessionID === "waiting" ? ("waiting_permission" as const) : undefined;

		const [group] = buildSidebarGroups([project()], sessions, ["node-1"], undefined, stateOf);

		// 等待审批的会话无条件入选，剩余名额按活动时间补足。
		expect(group.recent.map((row) => row.session.id)).toEqual(["waiting", "s3", "s2"]);
		expect(group.total).toBe(4);
	});

	it("不重复：被提前的会话不会在补位时出现两次", () => {
		const sessions = [
			{ nodeID: "node-1", session: session("waiting", "2026-11-14T13:00:00Z") },
			{ nodeID: "node-1", session: session("s1", "2026-11-14T10:00:00Z") },
			{ nodeID: "node-1", session: session("s2", "2026-11-14T11:00:00Z") },
			{ nodeID: "node-1", session: session("s3", "2026-11-14T12:00:00Z") },
		];
		const stateOf = (_nodeID: string, sessionID: string) =>
			sessionID === "waiting" ? ("waiting_permission" as const) : undefined;

		const [group] = buildSidebarGroups([project()], sessions, ["node-1"], undefined, stateOf);

		expect(group.recent.map((row) => row.session.id)).toEqual(["waiting", "s3", "s2"]);
	});

	it("没有状态函数时保持原有时间序", () => {
		const sessions = [
			{ nodeID: "node-1", session: session("s1", "2026-11-14T10:00:00Z") },
			{ nodeID: "node-1", session: session("s2", "2026-11-14T11:00:00Z") },
			{ nodeID: "node-1", session: session("s3", "2026-11-14T12:00:00Z") },
		];

		const [group] = buildSidebarGroups([project()], sessions, ["node-1"]);

		expect(group.recent.map((row) => row.session.id)).toEqual(["s3", "s2", "s1"]);
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
