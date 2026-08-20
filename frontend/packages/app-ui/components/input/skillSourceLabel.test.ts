import { describe, expect, it } from "vitest";
import { getSkillSourceLabel } from "./skillSourceLabel";

const baseSkill = {
	code: "demo",
	label: "Demo",
	description: "",
	keywords: [],
};

describe("getSkillSourceLabel", () => {
	it.each([
		[{ source: "builtin" as const }, "系统"],
		[{ origin: "builtin_worker" }, "系统"],
		[{ source: "marketplace" as const }, "市场"],
		[{ origin: "marketplace" }, "市场"],
		[{ source: "organization" as const }, "组织"],
	])("maps %s to %s", (overrides, expected) => {
		expect(getSkillSourceLabel({ ...baseSkill, ...overrides })).toBe(expected);
	});
});
