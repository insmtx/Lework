import { describe, expect, it } from "vitest";
import { buildSkillWorkbenchPrefill } from "./workbench-prefill";

describe("buildSkillWorkbenchPrefill", () => {
	it("uses the stable Skill code for the slash token", () => {
		expect(buildSkillWorkbenchPrefill("demo-code")).toEqual({
			value: "/demo-code ",
			tokens: [
				{
					kind: "skill",
					label: "/demo-code",
					start: 0,
					end: 10,
				},
			],
		});
	});

	it("keeps the optional prompt after the code token", () => {
		expect(buildSkillWorkbenchPrefill("skill-creator", "请创建技能").value).toBe(
			"/skill-creator 请创建技能",
		);
	});
});
