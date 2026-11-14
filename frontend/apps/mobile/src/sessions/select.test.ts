import type { Session } from "@lework/api";
import { describe, expect, it } from "vitest";
import { selectProjectSessions, selectSiblingSessions, sortSessionsByActivity } from "./select";

function session(overrides: Partial<Session> & { id: string }): Session {
	return { ...overrides } as Session;
}

describe("selectProjectSessions", () => {
	it("优先按 projectID 匹配，包含项目子目录里的会话", () => {
		const refs = [
			{ nodeID: "n1", session: session({ id: "s1", projectID: "repo", directory: "/w/repo/sub" }) },
			{ nodeID: "n1", session: session({ id: "s2", projectID: "other" }) },
		];
		const selected = selectProjectSessions(refs, "repo", "/w/repo");
		expect(selected.map((ref) => ref.session.id)).toEqual(["s1"]);
	});

	it("缺少 projectID 时退回 canonical 前缀", () => {
		const refs = [
			{ nodeID: "n1", session: session({ id: "s1", directory: "/w/repo/src" }) },
			{ nodeID: "n1", session: session({ id: "s2", directory: "/w/rep" }) },
		];
		const selected = selectProjectSessions(refs, "repo", "/w/repo");
		expect(selected.map((ref) => ref.session.id)).toEqual(["s1"]);
	});

	it("global 聚合键要求目录也在该项目下", () => {
		const refs = [
			{
				nodeID: "n1",
				session: session({ id: "s1", projectID: "global", directory: "/w/one/x" }),
			},
			{
				nodeID: "n1",
				session: session({ id: "s2", projectID: "global", directory: "/w/two/x" }),
			},
		];
		const selected = selectProjectSessions(refs, "global:/w/one", "/w/one");
		expect(selected.map((ref) => ref.session.id)).toEqual(["s1"]);
	});
});

describe("selectSiblingSessions", () => {
	it("只留同项目会话，按最近活动倒序", () => {
		const sessions = [
			session({ id: "new", projectID: "repo", updatedAt: "2026-11-12T00:00:00Z" }),
			session({ id: "old", projectID: "repo", updatedAt: "2026-11-01T00:00:00Z" }),
			session({ id: "other", projectID: "other" }),
		];

		expect(selectSiblingSessions(sessions, "repo", "/w/repo").map((item) => item.id)).toEqual([
			"new",
			"old",
		]);
	});

	it("global 项目用目录前缀区分兄弟仓库", () => {
		const sessions = [
			session({ id: "one", projectID: "global", directory: "/w/one/x" }),
			session({ id: "two", projectID: "global", directory: "/w/two/x" }),
		];

		expect(
			selectSiblingSessions(sessions, "global:/w/one", "/w/one").map((item) => item.id),
		).toEqual(["one"]);
	});
});

describe("sortSessionsByActivity", () => {
	it("按更新时间倒序，缺失时间的排在后面", () => {
		const refs = [
			{ nodeID: "n1", session: session({ id: "old", updatedAt: "2026-11-01T00:00:00Z" }) },
			{ nodeID: "n1", session: session({ id: "new", updatedAt: "2026-11-12T00:00:00Z" }) },
			{ nodeID: "n1", session: session({ id: "unknown" }) },
		];
		const sorted = sortSessionsByActivity(refs);
		expect(sorted.map((ref) => ref.session.id)).toEqual(["new", "old", "unknown"]);
	});
});
