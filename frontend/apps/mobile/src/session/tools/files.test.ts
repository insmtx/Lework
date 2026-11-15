import type { ContentPart } from "@lework/api";
import { describe, expect, it } from "vitest";

import { fileView } from "./files";

function tool(name: string, state: unknown): ContentPart {
	return { type: "tool", tool: name, state };
}

describe("read", () => {
	it("summarizes the path and shows the file content", () => {
		const view = fileView(
			tool("read", {
				status: "completed",
				input: { path: "src/auth.ts" },
				output: "1| export function login() {}",
			}),
		);

		expect(view.summary).toBe("src/auth.ts");
		expect(view.blocks).toEqual([
			{ kind: "code", label: "src/auth.ts", text: "1| export function login() {}" },
		]);
	});

	it("shows the paging range when the agent asked for a slice", () => {
		const view = fileView(
			tool("read", {
				status: "completed",
				input: { path: "src/auth.ts", offset: 120, limit: 80 },
				output: "120| ...",
			}),
		);

		expect(view.blocks[0]).toEqual({
			kind: "fields",
			fields: [{ label: "范围", value: "从第 120 行 · 最多 80 行" }],
		});
		expect(view.blocks[1]).toEqual({ kind: "code", label: "src/auth.ts", text: "120| ..." });
	});
});

describe("write", () => {
	it("summarizes the path and shows the written content", () => {
		const view = fileView(
			tool("write", {
				status: "completed",
				input: { path: "docs/example.md", content: "# Example\n" },
				output: "File written",
			}),
		);

		expect(view.summary).toBe("docs/example.md");
		expect(view.blocks[0]).toEqual({ kind: "code", label: "docs/example.md", text: "# Example" });
		expect(view.blocks[1]).toEqual({ kind: "text", text: "File written", tone: "muted" });
	});
});

describe("edit", () => {
	it("turns oldString/newString into a diff", () => {
		const view = fileView(
			tool("edit", {
				status: "completed",
				input: { path: "src/config.ts", oldString: "timeout: 60", newString: "timeout: 30" },
			}),
		);

		expect(view.summary).toBe("src/config.ts");
		expect(view.blocks[0]).toEqual({
			kind: "diff",
			label: "src/config.ts",
			lines: [
				{ kind: "del", text: "timeout: 60" },
				{ kind: "add", text: "timeout: 30" },
			],
		});
	});

	it("prefers the provider patch when metadata carries one", () => {
		const view = fileView(
			tool("edit", {
				status: "completed",
				input: { path: "src/config.ts", oldString: "a", newString: "b" },
				metadata: { diff: "@@ -1 +1 @@\n-a\n+b\n" },
			}),
		);

		expect(view.blocks[0]).toEqual({
			kind: "diff",
			label: "src/config.ts",
			lines: [
				{ kind: "meta", text: "@@ -1 +1 @@" },
				{ kind: "del", text: "a" },
				{ kind: "add", text: "b" },
			],
		});
	});
});

describe("patch", () => {
	it("renders the patch text as a diff and names the affected file", () => {
		const view = fileView(
			tool("patch", {
				status: "completed",
				input: {
					patch: "*** Begin Patch\n*** Update File: src/a.ts\n@@\n-old\n+new\n*** End Patch",
				},
			}),
		);

		expect(view.summary).toBe("src/a.ts");
		expect(view.blocks[0]).toMatchObject({ kind: "diff", label: "src/a.ts" });
	});
});
