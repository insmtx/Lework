import type { SkillMarketplaceItem } from "@leros/store";
import { describe, expect, it } from "vitest";
import { filterSkillsByCategory } from "./skillCatalog";

function skill(name: string, description: string, tags: string[] = []): SkillMarketplaceItem {
	return {
		source_type: "official",
		skill_id: name,
		name,
		display_name: name,
		description,
		version: "1",
		author: "Lework",
		category: "official",
		tags,
		icon: "",
		installs: 0,
		verified: true,
	};
}

describe("filterSkillsByCategory", () => {
	const skills = [
		skill("word-doc", "创建 Word 文档", ["docx"]),
		skill("data-review", "分析经营数据", ["xlsx"]),
		skill("repo-helper", "处理 GitHub 代码仓库", ["code"]),
		skill("slides", "生成演示文稿和视觉内容", ["pptx"]),
	];

	it("keeps the full catalogue for all categories", () => {
		expect(filterSkillsByCategory(skills, "all")).toEqual(skills);
	});

	it("classifies skills by names, descriptions, categories, and tags", () => {
		expect(filterSkillsByCategory(skills, "document").map((item) => item.name)).toEqual([
			"word-doc",
		]);
		expect(filterSkillsByCategory(skills, "data").map((item) => item.name)).toEqual([
			"data-review",
		]);
		expect(filterSkillsByCategory(skills, "code").map((item) => item.name)).toEqual([
			"repo-helper",
		]);
		expect(filterSkillsByCategory(skills, "visual").map((item) => item.name)).toEqual(["slides"]);
	});
});
