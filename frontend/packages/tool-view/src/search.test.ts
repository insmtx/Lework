import type { ContentPart } from "@lework/api";
import { describe, expect, it } from "vitest";

import { searchView } from "./search";

function tool(name: string, state: unknown): ContentPart {
	return { type: "tool", tool: name, state };
}

describe("glob", () => {
	it("summarizes the pattern and lists matched files", () => {
		const view = searchView(
			tool("glob", {
				status: "completed",
				input: { pattern: "**/*.test.ts", path: "src" },
				content: [{ type: "text", text: "a.test.ts\nb.test.ts\n" }],
			}),
		);

		expect(view.summary).toBe("**/*.test.ts");
		expect(view.blocks[0]).toEqual({ kind: "fields", fields: [{ label: "path", value: "src" }] });
		expect(view.blocks[1]).toEqual({ kind: "list", items: ["a.test.ts", "b.test.ts"] });
	});
});

describe("grep", () => {
	it("shows the include filter and the matching lines", () => {
		const view = searchView(
			tool("grep", {
				status: "completed",
				input: { pattern: "timeout", include: "*.ts" },
				content: [{ type: "text", text: "src/a.ts:12: timeout exceeded\n" }],
			}),
		);

		expect(view.summary).toBe("timeout");
		expect(view.blocks[0]).toEqual({
			kind: "fields",
			fields: [{ label: "include", value: "*.ts" }],
		});
		expect(view.blocks[1]).toEqual({ kind: "code", text: "src/a.ts:12: timeout exceeded" });
	});

	it("renders an empty result as an empty list instead of blank JSON", () => {
		const view = searchView(
			tool("grep", {
				status: "completed",
				input: { pattern: "nope" },
				output: "",
			}),
		);

		expect(view.blocks).toEqual([{ kind: "list", items: [] }]);
	});
});
