import type { ContentPart } from "@lework/api";
import { describe, expect, it } from "vitest";

import { genericView } from "./generic";

function tool(name: string, state: unknown): ContentPart {
	return { type: "tool", tool: name, state };
}

describe("genericView", () => {
	it("flattens input fields instead of printing the whole JSON", () => {
		const view = genericView(
			tool("mcp_context7_resolve", {
				status: "completed",
				input: { library: "react", nested: { a: 1 }, limit: 3 },
				output: "resolved",
			}),
		);

		expect(view.summary).toBe("react");
		expect(view.blocks[0]).toEqual({
			kind: "fields",
			fields: [
				{ label: "library", value: "react" },
				{ label: "nested", value: '{"a":1}' },
				{ label: "limit", value: "3" },
			],
		});
		expect(view.blocks[1]).toEqual({ kind: "code", text: "resolved" });
	});

	it("prefers a descriptive field for the summary", () => {
		const view = genericView(
			tool("custom", { status: "completed", input: { query: "x", description: "查找" } }),
		);

		expect(view.summary).toBe("查找");
	});

	it("renders errors even without input", () => {
		const view = genericView(tool("custom", { status: "error", error: "boom" }));

		expect(view.summary).toBe("");
		expect(view.tone).toBe("error");
		expect(view.blocks).toEqual([{ kind: "text", text: "boom", tone: "error" }]);
	});
});
