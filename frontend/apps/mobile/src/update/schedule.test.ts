import { describe, expect, it } from "vitest";
import { shouldCheckOnResume, UPDATE_RESUME_CHECK_MS } from "./schedule";

describe("shouldCheckOnResume", () => {
	it("checks when there is no previous check", () => {
		expect(shouldCheckOnResume(null, 1_000)).toBe(true);
	});
	it("waits five minutes between resume checks", () => {
		expect(shouldCheckOnResume(1_000, 1_000 + UPDATE_RESUME_CHECK_MS - 1)).toBe(false);
		expect(shouldCheckOnResume(1_000, 1_000 + UPDATE_RESUME_CHECK_MS)).toBe(true);
	});
});
