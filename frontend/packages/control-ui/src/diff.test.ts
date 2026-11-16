import { describe, expect, it } from "vitest";
import { parsePatch } from "./diff";

describe("parsePatch", () => {
	it("按 hunk 头推算新旧行号", () => {
		const rows = parsePatch(
			["@@ -10,2 +20,3 @@ function main() {", " context", "-removed", "+added", "+also"].join("\n"),
		);

		expect(rows.map((row) => [row.kind, row.oldNo, row.newNo, row.text])).toEqual([
			["hunk", null, null, "@@ -10,2 +20,3 @@ function main() {"],
			["context", 10, 20, "context"],
			["delete", 11, null, "removed"],
			["add", null, 21, "added"],
			["add", null, 22, "also"],
		]);
	});

	it("跳过文件头、扩展头与 no-newline 标记", () => {
		const rows = parsePatch(
			[
				"diff --git a/a.ts b/a.ts",
				"index 111..222 100644",
				"new file mode 100644",
				"--- a/a.ts",
				"+++ b/a.ts",
				"@@ -0,0 +1 @@",
				"+export {};",
				"\\ No newline at end of file",
			].join("\n"),
		);

		expect(rows).toHaveLength(2);
		expect(rows[0]?.kind).toBe("hunk");
		expect(rows[1]).toMatchObject({ kind: "add", newNo: 1, text: "export {};" });
	});

	it("无法识别的行按上下文保留，空行丢弃", () => {
		const rows = parsePatch("@@ -1,0 +1,0 @@\nnot-a-prefixed-line\n\n");

		expect(rows.map((row) => row.text)).toEqual(["@@ -1,0 +1,0 @@", "not-a-prefixed-line"]);
		expect(rows[1]).toMatchObject({ kind: "context", oldNo: null, newNo: null });
	});

	it("没有 hunk 头时行号从 1 开始累计", () => {
		const rows = parsePatch("+first\n second");

		expect(rows[0]).toMatchObject({ kind: "add", oldNo: null, newNo: 1 });
		expect(rows[1]).toMatchObject({ kind: "context", oldNo: 1, newNo: 2 });
	});
});
