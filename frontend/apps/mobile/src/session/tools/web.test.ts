import type { ContentPart } from "@lework/api";
import { describe, expect, it } from "vitest";

import { webView } from "./web";

function tool(name: string, state: unknown): ContentPart {
	return { type: "tool", tool: name, state };
}

describe("webfetch", () => {
	it("summarizes the url and shows the response body", () => {
		const view = webView(
			tool("webfetch", {
				status: "completed",
				input: { url: "https://example.com/docs", format: "markdown" },
				output: "# Docs\n",
			}),
		);

		expect(view.summary).toBe("https://example.com/docs");
		expect(view.blocks[0]).toEqual({
			kind: "fields",
			fields: [{ label: "格式", value: "markdown" }],
		});
		expect(view.blocks[1]).toEqual({ kind: "code", text: "# Docs" });
	});

	it("surfaces fetch failures as error text", () => {
		const view = webView(
			tool("webfetch", {
				status: "error",
				input: { url: "https://example.com" },
				error: { message: "HTTP 500" },
			}),
		);

		expect(view.tone).toBe("error");
		expect(view.blocks.at(-1)).toEqual({ kind: "text", text: "HTTP 500", tone: "error" });
	});
});

describe("websearch", () => {
	it("summarizes the query and shows the results", () => {
		const view = webView(
			tool("websearch", {
				status: "completed",
				input: { query: "bun release notes" },
				output: "Bun 1.2 released\nhttps://bun.sh/blog\n",
			}),
		);

		expect(view.summary).toBe("bun release notes");
		expect(view.blocks).toEqual([{ kind: "code", text: "Bun 1.2 released\nhttps://bun.sh/blog" }]);
	});
});
