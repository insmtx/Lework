/**
 * 发送前把输入框里的技能 mention 序列化成 content 中的芯片标签。
 * ident 在标签的 data-code，展示名在标签内文；不再靠 metadata 传技能 ident。
 *
 * 契约：
 * - 存库 / UI 回放：raw chip HTML
 * - 给模型（后端 PlainText）：chip → catalog code
 * - 给用户看（本文件 skillChipsToPlainText / formatTaskDisplayTitle）：chip → 中文标签
 */
import type { ComposerToken, MessageMetadata } from "../../types/chat";

const SKILL_CODE_RE = /^[A-Za-z][A-Za-z0-9_-]*$/;
const SKILL_CHIP_RE = /<skill-chip\s+[^>]*\bdata-code="([A-Za-z][A-Za-z0-9_-]*)"[^>]*>([^<]*)<\/skill-chip>/gi;

export type ParsedSkillChip = {
	code: string;
	label: string;
	start: number;
	end: number;
};

export function skillCodeFromToken(token: Pick<ComposerToken, "kind" | "id">): string {
	if (token.kind !== "skill") return "";
	const code = token.id?.trim() ?? "";
	return SKILL_CODE_RE.test(code) ? code : "";
}

export function parseSkillChips(content: string): ParsedSkillChip[] {
	const chips: ParsedSkillChip[] = [];
	const pattern = new RegExp(SKILL_CHIP_RE.source, "gi");
	let match = pattern.exec(content);
	while (match) {
		chips.push({
			code: match[1] ?? "",
			label: unescapeHtml(match[2] ?? "").trim(),
			start: match.index,
			end: match.index + match[0].length,
		});
		match = pattern.exec(content);
	}
	return chips;
}

export function hasComposerSkillTokens(content?: string): boolean {
	return parseSkillChips(content ?? "").length > 0;
}

/** 用户可见纯文本：chip → 中文展示名（任务标题、回复预览等）。 */
export function skillChipsToPlainText(content: string): string {
	return content.replace(new RegExp(SKILL_CHIP_RE.source, "gi"), (_raw, code, label) => {
		const text = unescapeHtml(String(label ?? "")).trim();
		return text || String(code ?? "");
	});
}

/** 任务名/项目名/侧边栏摘要：剥技能芯片标签，始终展示中文名。 */
export function formatTaskDisplayTitle(title: string): string {
	return skillChipsToPlainText(title).trim();
}

export function skillChipMarkup(code: string, label: string): string {
	const trimmedCode = code.trim();
	if (!SKILL_CODE_RE.test(trimmedCode)) return "";
	const text = stripSkillSlash(label) || trimmedCode;
	return `<skill-chip data-code="${trimmedCode}">${escapeHtml(text)}</skill-chip>`;
}

/** 把存库的 skill-chip 还原成输入框可见的 /展示名 + token，供编辑回填。 */
export function skillChipsToComposerState(content: string): {
	value: string;
	tokens: ComposerToken[];
} {
	const chips = parseSkillChips(content);
	if (chips.length === 0) {
		return { value: content, tokens: [] };
	}
	let value = "";
	let cursor = 0;
	const tokens: ComposerToken[] = [];
	for (const chip of chips) {
		value += content.slice(cursor, chip.start);
		const label = `/${stripSkillSlash(chip.label || chip.code)}`;
		const start = value.length;
		value += label;
		tokens.push({
			kind: "skill",
			id: chip.code,
			label,
			start,
			end: start + label.length,
		});
		cursor = chip.end;
	}
	value += content.slice(cursor);
	return { value, tokens };
}

export function prepareOutgoingComposer(
	content: string,
	tokens: ComposerToken[],
): { content: string; metadata?: MessageMetadata } {
	const leadingOffset = content.length - content.trimStart().length;
	const trimmed = content.trim();
	const aligned = tokens
		.map((token) => ({
			...token,
			start: token.start - leadingOffset,
			end: token.end - leadingOffset,
		}))
		.filter((token) => {
			if (token.start < 0 || token.end > trimmed.length) return false;
			if (trimmed.slice(token.start, token.end) !== token.label) return false;
			if (token.kind === "skill") return Boolean(skillCodeFromToken(token));
			return true;
		})
		.sort((left, right) => left.start - right.start);

	let serialized = "";
	let cursor = 0;
	const nextTokens: ComposerToken[] = [];
	for (const token of aligned) {
		serialized += trimmed.slice(cursor, token.start);
		if (token.kind === "skill") {
			serialized += skillChipMarkup(skillCodeFromToken(token), token.label);
		} else {
			const start = serialized.length;
			serialized += token.label;
			nextTokens.push({
				...token,
				start,
				end: start + token.label.length,
			});
		}
		cursor = token.end;
	}
	serialized += trimmed.slice(cursor);

	if (!serialized && nextTokens.length === 0) {
		return { content: "" };
	}
	return {
		content: serialized,
		metadata: nextTokens.length > 0 ? { composerTokens: nextTokens } : undefined,
	};
}

function stripSkillSlash(label: string): string {
	return label.trim().replace(/^\/+/, "").trim();
}

function escapeHtml(value: string): string {
	return value
		.replace(/&/g, "&amp;")
		.replace(/</g, "&lt;")
		.replace(/>/g, "&gt;")
		.replace(/"/g, "&quot;");
}

function unescapeHtml(value: string): string {
	return value
		.replace(/&lt;/g, "<")
		.replace(/&gt;/g, ">")
		.replace(/&quot;/g, '"')
		.replace(/&#39;/g, "'")
		.replace(/&amp;/g, "&");
}
