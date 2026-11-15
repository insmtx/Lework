import type { ContentPart } from "@lework/api";
import { describe, expect, it } from "vitest";

import { toolView } from "./index";

function tool(name: string, state: unknown): ContentPart {
	return { type: "tool", tool: name, state };
}

describe("toolView", () => {
	it("ignores non-tool parts", () => {
		expect(toolView({ type: "text", text: "hi" })).toBeNull();
	});

	it("routes shell aliases to the terminal renderer", () => {
		for (const name of ["shell", "bash"]) {
			const view = toolView(tool(name, { status: "completed", input: { command: "pwd" } }));
			expect(view?.blocks[0]).toMatchObject({ kind: "terminal", command: "pwd" });
		}
	});

	it("routes file tools to the file renderer", () => {
		const view = toolView(
			tool("read", { status: "completed", input: { path: "a.ts" }, output: "x" }),
		);

		expect(view?.summary).toBe("a.ts");
	});

	it("routes search, web and agent tools", () => {
		expect(toolView(tool("grep", { status: "completed", input: { pattern: "x" } }))?.summary).toBe(
			"x",
		);
		expect(
			toolView(tool("webfetch", { status: "completed", input: { url: "https://a" } }))?.summary,
		).toBe("https://a");
		expect(
			toolView(tool("subagent", { status: "completed", input: { agent: "explore" } }))?.subagent
				?.agent,
		).toBe("explore");
	});

	it("falls back to the generic renderer for unknown tools", () => {
		const view = toolView(tool("mcp_foo", { status: "completed", input: { text: "value" } }));

		expect(view?.summary).toBe("value");
		expect(view?.blocks[0]).toEqual({
			kind: "fields",
			fields: [{ label: "text", value: "value" }],
		});
	});

	it("keeps the raw state for debugging", () => {
		const view = toolView(tool("bash", { status: "completed" }));

		expect(view?.raw).toBe(JSON.stringify({ status: "completed" }, null, 2));
	});

	it("names nameless tools instead of rendering an empty header", () => {
		expect(toolView({ type: "tool", state: {} })?.tool).toBe("tool");
	});
});
