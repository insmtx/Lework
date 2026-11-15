import type { ContentPart, Message } from "@lework/api";

import type { ProcessDisplay } from "../settings/preferences";

/** BRIEF_REASONING_LIMIT 是摘要模式保留的字符数，够看清思路又不占满屏幕。 */
export const BRIEF_REASONING_LIMIT = 160;

/** reasoningParts 返回消息里的思考片段；没有 parts 时为空数组。 */
export function reasoningParts(message: Message): ContentPart[] {
	if (!message.parts) return [];
	return message.parts.filter((part) => part.type === "reasoning" && (part.text ?? "") !== "");
}

/**
 * reasoningText 拼接所有思考片段。
 *
 * reasoning 属于执行过程，不进正文气泡；这里单独抽取，正文只取 text part。
 */
export function reasoningText(message: Message): string {
	return reasoningParts(message)
		.map((part) => part.text ?? "")
		.filter((text) => text !== "")
		.join("\n");
}

function summarize(text: string): string {
	const flat = text.replace(/\s+/g, " ").trim();
	if (flat.length <= BRIEF_REASONING_LIMIT) return flat;
	return `${flat.slice(0, BRIEF_REASONING_LIMIT).trimEnd()}…`;
}

/**
 * visibleReasoning 按用户选择的展示程度返回可见思考。
 *
 * hidden 完全不展示；brief 压成一段单行摘要，完整内容仍可展开；
 * full 直接返回完整思考。没有思考内容时统一返回 null。
 */
export function visibleReasoning(message: Message, mode: ProcessDisplay): string | null {
	if (mode === "hidden") return null;
	const text = reasoningText(message);
	if (!text) return null;
	return mode === "full" ? text : summarize(text);
}
