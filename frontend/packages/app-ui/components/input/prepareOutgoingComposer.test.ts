import {
	formatTaskDisplayTitle,
	hasComposerSkillTokens,
	prepareOutgoingComposer,
	skillChipMarkup,
	skillChipsToComposerState,
} from "@leros/store";
import { describe, expect, it } from "vitest";

describe("prepareOutgoingComposer", () => {
	it("serializes skill mentions into content chips and drops skill metadata", () => {
		const label = "/创建 Word 文档";
		const result = prepareOutgoingComposer(`${label} 测试`, [
			{
				kind: "skill",
				id: "create-word-doc",
				label,
				start: 0,
				end: label.length,
			},
		]);
		expect(result.content).toBe(`${skillChipMarkup("create-word-doc", "创建 Word 文档")} 测试`);
		expect(result.metadata).toBeUndefined();
		expect(hasComposerSkillTokens(result.content)).toBe(true);
	});

	it("allows skill-only messages", () => {
		const label = "/文档协作";
		const result = prepareOutgoingComposer(`${label} `, [
			{
				kind: "skill",
				id: "doc-coauthoring",
				label,
				start: 0,
				end: label.length,
			},
		]);
		expect(result.content).toBe(skillChipMarkup("doc-coauthoring", "文档协作"));
		expect(hasComposerSkillTokens(result.content)).toBe(true);
	});

	it("keeps a skill inserted in the middle of the prompt", () => {
		const label = "/文档协作";
		const content = `请先 ${label} 再继续`;
		const start = content.indexOf(label);
		const result = prepareOutgoingComposer(content, [
			{
				kind: "skill",
				id: "doc-coauthoring",
				label,
				start,
				end: start + label.length,
			},
		]);
		expect(result.content).toBe(`请先 ${skillChipMarkup("doc-coauthoring", "文档协作")} 再继续`);
	});

	it("keeps assistant tokens beside skills after trimming", () => {
		const skillLabel = "/文档协作";
		const assistantLabel = "@小明";
		const result = prepareOutgoingComposer(` ${skillLabel} ${assistantLabel} 你好`, [
			{
				kind: "skill",
				id: "doc-coauthoring",
				label: skillLabel,
				start: 1,
				end: 1 + skillLabel.length,
			},
			{
				kind: "assistant",
				id: "asst_1",
				label: assistantLabel,
				start: 1 + skillLabel.length + 1,
				end: 1 + skillLabel.length + 1 + assistantLabel.length,
			},
		]);
		const chip = skillChipMarkup("doc-coauthoring", "文档协作");
		expect(result.content).toBe(`${chip} ${assistantLabel} 你好`);
		expect(result.metadata?.composerTokens).toEqual([
			{
				kind: "assistant",
				id: "asst_1",
				label: assistantLabel,
				start: chip.length + 1,
				end: chip.length + 1 + assistantLabel.length,
			},
		]);
	});
});

describe("formatTaskDisplayTitle", () => {
	it("strips skill chips for sidebar task title preview", () => {
		expect(
			formatTaskDisplayTitle(
				`<skill-chip data-code="bid-backfill">投标文件回填</skill-chip> 测试`,
			),
		).toBe("投标文件回填 测试");
	});

	it("also strips skill chips from project names", () => {
		expect(
			formatTaskDisplayTitle(`<skill-chip data-code="bid-backfill">投标文件回填</skill-chip>`),
		).toBe("投标文件回填");
	});
});

describe("skillChipsToComposerState", () => {
	it("restores slash mentions and skill tokens from stored chips", () => {
		const chip = skillChipMarkup("daily-report", "日报 Skill");
		expect(skillChipsToComposerState(`${chip} 生成日报`)).toEqual({
			value: "/日报 Skill 生成日报",
			tokens: [
				{
					kind: "skill",
					id: "daily-report",
					label: "/日报 Skill",
					start: 0,
					end: "/日报 Skill".length,
				},
			],
		});
	});

	it("leaves legacy slash instructions unchanged", () => {
		expect(skillChipsToComposerState("请使用 /daily-report 生成日报")).toEqual({
			value: "请使用 /daily-report 生成日报",
			tokens: [],
		});
	});
});
