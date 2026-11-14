import type { ProjectSummary, Session } from "@lework/api";
import { describe, expect, it } from "vitest";
import {
	groupSessionsByProject,
	isPathPrefix,
	orderProjects,
	projectKeyOf,
	resolveInbox,
	resolveTarget,
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
