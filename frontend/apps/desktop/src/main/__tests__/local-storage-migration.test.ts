import { describe, expect, it, vi } from "vitest";
import {
	buildRestoreScript,
	parseLocalStorageSnapshot,
	runBestEffortMigration,
} from "../local-storage-migration";

describe("local storage migration", () => {
	it("keeps only string entries and escapes quotes", () => {
		expect(parseLocalStorageSnapshot(JSON.stringify({ a: "1", n: 2 }))).toEqual({ a: "1" });
		expect(buildRestoreScript({ x: "a'b" })).toContain('localStorage.setItem("x", "a\'b")');
	});

	it("swallows migration errors so the app can still start", async () => {
		const errors: unknown[] = [];
		await runBestEffortMigration(
			async () => {
				throw new Error("disk full");
			},
			(error) => errors.push(error),
		);

		expect(errors).toHaveLength(1);
	});

	it("logs migration errors by default", async () => {
		const error = vi.spyOn(console, "error").mockImplementation(() => undefined);

		await runBestEffortMigration(async () => {
			throw new Error("disk full");
		});

		expect(error).toHaveBeenCalled();
		error.mockRestore();
	});

	it("rejects malformed snapshots", () => {
		expect(parseLocalStorageSnapshot("nope")).toEqual({});
	});
});
