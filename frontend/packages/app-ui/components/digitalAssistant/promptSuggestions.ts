import type { DigitalAssistantItem } from "@leros/store";

/** 构造「试试这样问我」提示语，基于队友的擅长领域与名称。 */
export function buildPromptSuggestions(assistant: { name: string; expertise: string[] }): string[] {
	const primary = assistant.expertise[0] || assistant.name;
	const secondary = assistant.expertise[1] || primary;
	const tertiary = assistant.expertise[2] || secondary;

	return uniqueNonEmpty([
		`请帮我处理一个${primary}任务，先梳理目标、风险和执行步骤。`,
		`围绕${secondary}，请给我一份可以直接推进的方案。`,
		`我想提升${tertiary}效果，请给出关键检查项和优化建议。`,
	]).slice(0, 3);
}

/** 召唤队友时的默认开场 prompt（仅在需要主动发首条消息时使用）。 */
export function buildDefaultSummonPrompt(assistant: { name: string; expertise: string[] }): string {
	const domain = assistant.expertise[0] || assistant.name;
	return `请以“${assistant.name}”的身份，先介绍你能如何帮我处理${domain}相关任务，并给出一个可执行的开始方案。`;
}

/** 从队友信息中提取特征关键词（来源标签 + 擅长领域，取前 3 个）。 */
export function buildFeatureKeywords(assistant: {
	source?: string;
	expertise: string[];
}): string[] {
	const source = assistant.source === "template" ? "模板创建" : assistant.source ? "自定义" : "";
	return uniqueNonEmpty([source, ...assistant.expertise]).slice(0, 3);
}

/** 去重并过滤空白字符串。 */
export function uniqueNonEmpty(values: Array<string | undefined>): string[] {
	const seen = new Set<string>();
	const result: string[] = [];

	for (const value of values) {
		const normalized = value?.trim();
		if (!normalized || seen.has(normalized)) continue;
		seen.add(normalized);
		result.push(normalized);
	}

	return result;
}

/** 兼容 DigitalAssistantItem 的便捷重载。 */
export function buildPromptSuggestionsForAssistant(assistant: DigitalAssistantItem): string[] {
	return buildPromptSuggestions(assistant);
}
