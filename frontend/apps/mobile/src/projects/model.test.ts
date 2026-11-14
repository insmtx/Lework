import type { ProjectSummary, Session } from "@lework/api";
import { describe, expect, it } from "vitest";
import {
	findProjectForSession,
	groupSessionsByProject,
	isPathPrefix,
	orderProjects,
	projectKeyOf,
	resolveInbox,
	resolveTarget,
	resolveTaskTarget,
} from "./model";

function project(overrides: Partial<ProjectSummary> & { key: string }): ProjectSummary {
	return {
		id: overrides.key,
		canonical: `/w/${overrides.key}`,
		nodes: [{ node_id: "n1", online: true, last_seen: "2026-11-12T00:00:00Z" }],
		last_seen: "2026-11-12T00:00:00Z",
		...overrides,
	};
}

describe("projectKeyOf", () => {
	it("global 拼接 canonical，普通 id 原样返回", () => {
		expect(projectKeyOf({ id: "abc", canonical: "/w/repo" })).toBe("abc");
		expect(projectKeyOf({ id: "global", canonical: "/w/repo" })).toBe("global:/w/repo");
	});
});

describe("isPathPrefix", () => {
	it("按目录分段匹配，不做字符串前缀比较", () => {
		expect(isPathPrefix("/w/rep", "/w/repo/x")).toBe(false);
		expect(isPathPrefix("/w/repo", "/w/repo/src")).toBe(true);
		expect(isPathPrefix("/w/repo", "/w/repo")).toBe(true);
		expect(isPathPrefix("/w/repo/", "/w/repo/src")).toBe(true);
	});
});

describe("resolveInbox", () => {
	it("目录位于仓库内部时解析到该仓库", () => {
		const inbox = project({ key: "repo", canonical: "/w/repo" });
		const main = resolveInbox([inbox], { n1: "/w/repo/inbox" }, ["n1"]);
		expect(main.project?.key).toBe("repo");
		expect(main.unresolved).toBe(false);
	});

	it("目录不属于任何项目时标记未解析", () => {
		const repo = project({ key: "repo", canonical: "/w/repo" });
		const main = resolveInbox([repo], { n1: "/w/other" }, ["n1"]);
		expect(main.project).toBeNull();
		expect(main.nodeID).toBe("n1");
		expect(main.unresolved).toBe(true);
	});

	it("优先在线节点，且不会重复返回", () => {
		const repo = project({
			key: "repo",
			canonical: "/w/repo",
			nodes: [
				{ node_id: "offline", online: false, last_seen: "2026-11-01T00:00:00Z" },
				{ node_id: "online", online: true, last_seen: "2026-11-12T00:00:00Z" },
			],
		});
		const main = resolveInbox([repo], { offline: "/w/repo", online: "/w/repo" }, ["online"]);
		expect(main.nodeID).toBe("online");
		expect(main.project?.key).toBe("repo");
	});
});

describe("resolveTarget", () => {
	it("主节点在线时直接使用", () => {
		const repo = project({ key: "repo", primary_node_id: "n2" });
		expect(resolveTarget(repo, ["n1", "n2"])).toEqual({ nodeID: "n2", reason: "ok" });
	});

	it("主节点离线时退回最近在线的项目节点", () => {
		const repo = project({
			key: "repo",
			primary_node_id: "n2",
			nodes: [
				{ node_id: "n1", online: true, last_seen: "2026-11-10T00:00:00Z" },
				{ node_id: "n3", online: true, last_seen: "2026-11-12T00:00:00Z" },
			],
		});
		expect(resolveTarget(repo, ["n1", "n3"])).toEqual({ nodeID: "n3", reason: "ok" });
	});

	it("全部离线时返回 no-node", () => {
		const repo = project({ key: "repo" });
		expect(resolveTarget(repo, [])).toEqual({ nodeID: null, reason: "no-node" });
	});
});

describe("findProjectForSession", () => {
	it("按 projectID 直接命中项目", () => {
		const session: Session = { id: "s1", projectID: "repo", directory: "/w/repo/sub" };
		const repo = project({ key: "repo", canonical: "/w/repo" });

		expect(findProjectForSession(session, [repo])?.key).toBe("repo");
	});

	it("global 选目录最深的那一个，避免兄弟仓库串台", () => {
		const session: Session = { id: "s1", projectID: "global", directory: "/w/repo/pkg/sub" };
		const shallow = project({ id: "global", key: "global:/w/repo", canonical: "/w/repo" });
		const deep = project({ id: "global", key: "global:/w/repo/pkg", canonical: "/w/repo/pkg" });

		expect(findProjectForSession(session, [shallow, deep])?.key).toBe("global:/w/repo/pkg");
	});

	it("老控制面缺 projectID 时退回目录前缀", () => {
		const session: Session = { id: "s1", directory: "/w/repo/src" };
		const repo = project({ key: "repo", canonical: "/w/repo" });

		expect(findProjectForSession(session, [repo])?.key).toBe("repo");
	});

	it("匹配不到返回 null，调用方可以退回会话自身的 projectID", () => {
		expect(findProjectForSession({ id: "s1", projectID: "missing" }, [])).toBeNull();
	});
});

describe("resolveTaskTarget", () => {
	it("项目已解析时按项目选节点，偏好节点离线也不影响发送", () => {
		const repo = project({
			key: "repo",
			canonical: "/w/repo",
			nodes: [
				{ node_id: "pref", online: false, last_seen: "2026-11-01T00:00:00Z" },
				{ node_id: "other", online: true, last_seen: "2026-11-12T00:00:00Z" },
			],
		});
		const inbox = resolveInbox([repo], { pref: "/w/repo" }, ["other"]);

		expect(resolveTaskTarget(inbox, { pref: "/w/repo" }, ["other"])).toEqual({
			nodeID: "other",
			directory: "/w/repo",
			scopeKey: "repo",
			reason: "ok",
		});
	});

	it("项目还没创建时退回偏好目录，首条消息负责创建项目", () => {
		const inbox = resolveInbox([], { n1: "/w/plain" }, ["n1"]);

		expect(resolveTaskTarget(inbox, { n1: "/w/plain" }, ["n1"])).toEqual({
			nodeID: "n1",
			directory: "/w/plain",
			scopeKey: "inbox:n1:/w/plain",
			reason: "ok",
		});
	});

	it("没有任何可用目录时返回 no-dir", () => {
		const inbox = resolveInbox([], {}, []);

		expect(resolveTaskTarget(inbox, {}, [])).toEqual({
			nodeID: null,
			directory: "",
			scopeKey: "",
			reason: "no-dir",
		});
	});

	it("项目全部节点离线时保留 scopeKey，草稿不会丢", () => {
		const repo = project({
			key: "repo",
			canonical: "/w/repo",
			nodes: [{ node_id: "pref", online: false, last_seen: "2026-11-01T00:00:00Z" }],
		});
		const inbox = resolveInbox([repo], { pref: "/w/repo" }, []);

		expect(resolveTaskTarget(inbox, { pref: "/w/repo" }, [])).toEqual({
			nodeID: null,
			directory: "/w/repo",
			scopeKey: "repo",
			reason: "no-node",
		});
	});
});

describe("orderProjects", () => {
	it("我的永远第一，其余按 last_seen 倒序", () => {
		const older = project({ key: "old", last_seen: "2026-11-01T00:00:00Z" });
		const newer = project({ key: "new", last_seen: "2026-11-12T00:00:00Z" });
		const inbox = project({ key: "inbox", last_seen: "2026-10-01T00:00:00Z" });

		const ordered = orderProjects([older, newer, inbox], "inbox");
		expect(ordered.map((item) => item.key)).toEqual(["inbox", "new", "old"]);
	});
});

describe("groupSessionsByProject", () => {
	it("按 projectID 归组，不按目录前缀", () => {
		const refs = [
			{ nodeID: "n1", session: { id: "s1", projectID: "repo" } as Session },
			{ nodeID: "n2", session: { id: "s2", projectID: "repo" } as Session },
			{ nodeID: "n1", session: { id: "s3", projectID: "global:/w/plain" } as Session },
		];
		const grouped = groupSessionsByProject(refs, (session) => session.projectID ?? "");
		expect(grouped.repo?.map((ref) => ref.session.id)).toEqual(["s1", "s2"]);
		expect(grouped["global:/w/plain"]?.map((ref) => ref.session.id)).toEqual(["s3"]);
	});
});
