import { describe, expect, it } from "vitest";

import { diffFromEdit, parseUnifiedDiff } from "./diff";

describe("parseUnifiedDiff", () => {
	it("classifies header, removed, added and context lines", () => {
		const lines = parseUnifiedDiff(
			"--- a/src/a.ts\n+++ b/src/a.ts\n@@ -1 +1 @@\n-old\n+new\n context\n",
		);

		expect(lines.map((line) => line.kind)).toEqual([
			"meta",
			"meta",
			"meta",
			"del",
			"add",
			"context",
		]);
		expect(lines[3]).toEqual({ kind: "del", text: "old" });
		expect(lines[4]).toEqual({ kind: "add", text: "new" });
	});

	it("keeps binary patch markers as metadata", () => {
		expect(parseUnifiedDiff("Binary files a/x and b/x differ\n")).toEqual([
			{ kind: "meta", text: "Binary files a/x and b/x differ" },
		]);
	});
});

describe("diffFromEdit", () => {
	it("renders the replaced block as removals followed by additions", () => {
		const lines = diffFromEdit("a\nb", "a\nc");

		expect(lines).toEqual([
			{ kind: "del", text: "a" },
			{ kind: "del", text: "b" },
			{ kind: "add", text: "a" },
			{ kind: "add", text: "c" },
		]);
	});

	it("drops a trailing newline instead of rendering an empty line", () => {
		expect(diffFromEdit("a\n", "b\n").map((line) => line.text)).toEqual(["a", "b"]);
	});
});
