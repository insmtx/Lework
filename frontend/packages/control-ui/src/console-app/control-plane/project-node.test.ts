import { describe, expect, it } from "vitest";
import type { ProjectSummary } from "../../types";
import { projectNodeCanonical, resolveProjectNode } from "./project-node";

function project(overrides: Partial<ProjectSummary> = {}): ProjectSummary {
	return {
		key: "p",
		id: "p",
		canonical: "/work/demo",
		nodes: [],
		last_seen: "2026-11-28T00:00:00Z",
		...overrides,
	};
}

describe("resolveProjectNode", () => {
	it("prefers an online primary node", () => {
		const resolved = resolveProjectNode(
			project({
				primary_node_id: "n1",
				nodes: [
					{ node_id: "n1", online: true, last_seen: "", canonical: "/a" },
					{ node_id: "n2", online: true, last_seen: "", canonical: "/b" },
				],
			}),
		);
		expect(resolved?.node.node_id).toBe("n1");
		expect(resolved?.canonical).toBe("/a");
	});

	it("falls back to an online node when the primary is offline", () => {
		const resolved = resolveProjectNode(
			project({
				primary_node_id: "n1",
				nodes: [
					{ node_id: "n1", online: false, last_seen: "", canonical: "/a" },
					{ node_id: "n2", online: true, last_seen: "", canonical: "/b" },
				],
			}),
		);
		expect(resolved?.node.node_id).toBe("n2");
	});

	it("keeps the offline primary when everything is offline", () => {
		const resolved = resolveProjectNode(
			project({
				primary_node_id: "n1",
				nodes: [
					{ node_id: "n1", online: false, last_seen: "", canonical: "/a" },
					{ node_id: "n2", online: false, last_seen: "", canonical: "/b" },
				],
			}),
		);
		expect(resolved?.node.node_id).toBe("n1");
		expect(resolved?.online).toBe(false);
	});

	it("returns null without nodes", () => {
		expect(resolveProjectNode(project())).toBeNull();
	});

	it("uses the node canonical and falls back to the project canonical", () => {
		const p = project({
			nodes: [{ node_id: "n1", online: true, last_seen: "" }],
		});
		expect(projectNodeCanonical(p, "n1")).toBe("/work/demo");
		expect(projectNodeCanonical(p, "missing")).toBeNull();
	});
});
