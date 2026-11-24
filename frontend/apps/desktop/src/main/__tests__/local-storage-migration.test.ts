import { describe, expect, it } from "vitest";
import { buildRestoreScript, parseLocalStorageSnapshot } from "../local-storage-migration";

describe("local storage migration", () => {
	it("keeps only string entries and escapes quotes", () => {
		expect(parseLocalStorageSnapshot(JSON.stringify({ a: "1", n: 2 }))).toEqual({ a: "1" });
		expect(buildRestoreScript({ x: "a'b" })).toContain('localStorage.setItem("x", "a\'b")');
	});

	it("rejects malformed snapshots", () => {
		expect(parseLocalStorageSnapshot("nope")).toEqual({});
	});
});
