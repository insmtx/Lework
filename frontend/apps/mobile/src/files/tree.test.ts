import type { FileInfo, ProjectSummary } from "@lework/api";
import { describe, expect, it } from "vitest";
import { pickBrowseNode, sortEntries } from "./tree";

const project = (nodes: ProjectSummary["nodes"], primary?: string): ProjectSummary => ({
	key: "repo",
	id: "repo",
	canonical: "/top/repo",
	nodes,
	last_seen: "2026-11-12T00:00:00Z",
	primary_node_id: primary,
});

describe("pickBrowseNode", () => {
	it("手动选的节点仍在线时优先", () => {
		const repo = project([
			{ node_id: "n1", online: true, last_seen: "2026-11-10T00:00:00Z" },
			{ node_id: "n2", online: true, last_seen: "2026-11-12T00:00:00Z" },
		]);
		expect(pickBrowseNode(repo, "n1", ["n1", "n2"])).toBe("n1");
	});
	it("手动选的节点离线时回落到默认规则", () => {
		const repo = project([
			{ node_id: "n1", online: false, last_seen: "2026-11-10T00:00:00Z" },
			{ node_id: "n2", online: true, last_seen: "2026-11-12T00:00:00Z" },
		]);
		expect(pickBrowseNode(repo, "n1", ["n2"])).toBe("n2");
	});
	it("全离线时返回 null", () => {
		const repo = project([{ node_id: "n1", online: false, last_seen: "2026-11-10T00:00:00Z" }]);
		expect(pickBrowseNode(repo, "n1", [])).toBeNull();
	});
});

describe("sortEntries", () => {
	it("目录在前，同级按路径排序", () => {
		const entries: FileInfo[] = [
			{ path: "b.ts", type: "file" },
			{ path: "z", type: "directory" },
			{ path: "a.ts", type: "file" },
			{ path: "a", type: "directory" },
		];
		expect(sortEntries(entries).map((entry) => entry.path)).toEqual(["a", "z", "a.ts", "b.ts"]);
	});
	it("不修改入参", () => {
		const entries: FileInfo[] = [
			{ path: "b", type: "file" },
			{ path: "a", type: "file" },
		];
		sortEntries(entries);
		expect(entries.map((entry) => entry.path)).toEqual(["b", "a"]);
	});
});
