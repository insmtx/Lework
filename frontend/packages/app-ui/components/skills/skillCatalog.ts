import type { SkillMarketplaceItem } from "@leros/store";

export type SkillCatalogCategory = "all" | "document" | "data" | "code" | "visual";

export const SKILL_CATALOG_CATEGORIES: Array<{
	value: SkillCatalogCategory;
	label: string;
}> = [
	{ value: "all", label: "全部分类" },
	{ value: "document", label: "文档" },
	{ value: "data", label: "数据分析" },
	{ value: "code", label: "代码" },
	{ value: "visual", label: "视觉媒体" },
];

const CATEGORY_KEYWORDS: Record<Exclude<SkillCatalogCategory, "all">, string[]> = {
	document: ["文档", "公文", "报告", "标书", "word", "doc", "docx", "pdf", "document"],
	data: ["数据", "表格", "图表", "分析", "excel", "xlsx", "csv", "data", "chart"],
	code: ["代码", "开发", "编程", "仓库", "code", "coding", "github", "git"],
	visual: ["视觉", "图片", "图像", "视频", "演示", "image", "video", "media", "ppt", "pptx"],
};

function searchableText(skill: SkillMarketplaceItem): string {
	return [skill.name, skill.display_name, skill.description, skill.category, ...(skill.tags ?? [])]
		.filter(Boolean)
		.join(" ")
		.toLocaleLowerCase();
}

export function filterSkillsByCategory(
	skills: SkillMarketplaceItem[],
	category: SkillCatalogCategory,
): SkillMarketplaceItem[] {
	if (category === "all") return skills;
	const keywords = CATEGORY_KEYWORDS[category];
	return skills.filter((skill) => {
		const haystack = searchableText(skill);
		return keywords.some((keyword) => haystack.includes(keyword));
	});
}
