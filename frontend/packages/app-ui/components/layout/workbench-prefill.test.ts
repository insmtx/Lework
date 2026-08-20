import { describe, expect, it } from "vitest";
import { buildSkillWorkbenchPrefill } from "./workbench-prefill";

describe("buildSkillWorkbenchPrefill", () => {
	it("uses the stable Skill code for the slash token", () => {
		expect(buildSkillWorkbenchPrefill("demo-code")).toEqual({
			value: "/demo-code ",
			tokens: [
				{
					kind: "skill",
					id: "demo-code",
					label: "/demo-code",
					start: 0,
					end: 10,
				},
			],
		});
	});

	it("uses the display name in content and keeps the Skill code on the token", () => {
		expect(buildSkillWorkbenchPrefill("demo-code", undefined, "演示技能")).toEqual({
			value: "/演示技能 ",
			tokens: [
				{
					kind: "skill",
					id: "demo-code",
					label: "/演示技能",
					start: 0,
					end: 5,
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
