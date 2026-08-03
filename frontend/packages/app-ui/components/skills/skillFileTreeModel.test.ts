import { describe, expect, it } from "vitest";
import { buildSkillFileTree } from "./skillFileTreeModel";

describe("buildSkillFileTree", () => {
	it("builds sorted nested directories from a flat revision file index", () => {
		const tree = buildSkillFileTree([
			{ path: "scripts/z.ts", size_bytes: 2, sha256: "z" },
			{ path: "SKILL.md", size_bytes: 1, sha256: "skill" },
			{ path: "references/api.md", size_bytes: 3, sha256: "api" },
			{ path: "scripts/a.ts", size_bytes: 4, sha256: "a" },
		]);

		expect(tree.map((node) => `${node.type}:${node.name}`)).toEqual([
			"directory:references",
			"directory:scripts",
			"file:SKILL.md",
		]);
		expect(tree[1]?.children.map((node) => node.name)).toEqual(["a.ts", "z.ts"]);
		expect(tree[0]?.children[0]?.path).toBe("references/api.md");
	});
});
