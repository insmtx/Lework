import { describe, expect, it } from "vitest";

import {
	asNumber,
	asRecord,
	asString,
	clipText,
	formatDuration,
	stripAnsi,
	textOf,
	trimmedText,
} from "./values";

describe("asRecord", () => {
	it("returns plain objects", () => {
		const value = { a: 1 };
		expect(asRecord(value)).toBe(value);
	});

	it("rejects arrays, null and scalars", () => {
		expect(asRecord([])).toBeNull();
		expect(asRecord(null)).toBeNull();
		expect(asRecord("x")).toBeNull();
	});
});

describe("asString", () => {
	it("returns non-empty strings", () => {
		expect(asString(" hi ")).toBe("hi");
	});

	it("rejects blanks and non-strings", () => {
		expect(asString("   ")).toBeUndefined();
		expect(asString(1)).toBeUndefined();
	});
});

describe("asNumber", () => {
	it("accepts numbers and numeric strings", () => {
		expect(asNumber(0)).toBe(0);
		expect(asNumber("2")).toBe(2);
	});

	it("rejects non-numeric values", () => {
		expect(asNumber("abc")).toBeUndefined();
		expect(asNumber(null)).toBeUndefined();
	});
});

describe("textOf", () => {
	it("returns plain strings unchanged", () => {
		expect(textOf("abc")).toBe("abc");
	});

	it("joins text content blocks", () => {
		expect(
			textOf([
				{ type: "text", text: "a" },
				{ type: "text", text: "b" },
			]),
		).toBe("a\nb");
	});

	it("unwraps objects that carry text or output", () => {
		expect(textOf({ type: "text", text: "hi" })).toBe("hi");
		expect(textOf({ output: "out" })).toBe("out");
	});

	it("describes file content blocks instead of dumping them", () => {
		expect(textOf([{ type: "file", path: "src/a.ts" }])).toBe("[文件] src/a.ts");
	});

	it("returns an empty string for missing values", () => {
		expect(textOf(undefined)).toBe("");
	});
});

describe("stripAnsi", () => {
	it("removes color and control sequences", () => {
		expect(stripAnsi("\u001b[31mred\u001b[0m ok\u001b]0;title\u0007")).toBe("red ok");
	});
});

describe("trimmedText", () => {
	it("keeps indentation and strips trailing whitespace", () => {
		expect(trimmedText("  a\n\tb\n\n")).toBe("  a\n\tb");
	});

	it("returns an empty string for non-strings", () => {
		expect(trimmedText(1)).toBe("");
	});
});

describe("clipText", () => {
	it("keeps short text", () => {
		expect(clipText("abc", 10)).toBe("abc");
	});

	it("marks truncated text", () => {
		expect(clipText("abcdef", 3)).toBe("abc\n… 已截断");
	});
});

describe("formatDuration", () => {
	it("formats sub-second and second-scale durations", () => {
		expect(formatDuration(850)).toBe("850ms");
		expect(formatDuration(1234)).toBe("1.2s");
	});

	it("formats minutes", () => {
		expect(formatDuration(65000)).toBe("1m5s");
	});
});
