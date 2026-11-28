import { describe, expect, it } from "vitest";
import { cleanName, isSafeRelative, joinPath, parentPath } from "./file-paths";

describe("file paths", () => {
	it("normalizes directory names and joins relative paths", () => {
		expect(cleanName("src/")).toBe("src");
		expect(joinPath("", "src/")).toBe("src");
		expect(joinPath("src", "app.tsx")).toBe("src/app.tsx");
		expect(parentPath("src/deep/app.tsx")).toBe("src/deep");
	});

	it("rejects paths that could escape the workspace", () => {
		expect(isSafeRelative("src/app.tsx")).toBe(true);
		expect(isSafeRelative("/etc/passwd")).toBe(false);
		expect(isSafeRelative("../secret")).toBe(false);
		expect(isSafeRelative("a/../../b")).toBe(false);
	});
});
