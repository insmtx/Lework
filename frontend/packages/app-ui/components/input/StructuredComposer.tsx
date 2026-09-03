"use client";

import { Popover as PopoverPrimitive } from "@base-ui/react/popover";
import type { PluginComposerOption } from "@leros/store";
import type { ComposerToken } from "@leros/store/types/chat";
import {
	Command,
	CommandEmpty,
	CommandGroup,
	CommandInput,
	CommandItem,
	CommandList,
	CommandSeparator,
} from "@leros/ui/components/ui/command";
import { cn } from "@leros/ui/lib/utils";
import { Sparkles } from "lucide-react";
import {
	type ClipboardEvent,
	forwardRef,
	type KeyboardEvent,
	type MouseEvent,
	useCallback,
	useEffect,
	useImperativeHandle,
	useMemo,
	useRef,
	useState,
} from "react";
import { CUSTOM_ASSISTANT_DEFAULT_AVATAR_SRC } from "../../assets";
import { createDiceBearAvatarDataUri } from "../avatar/DiceBearAvatar";
import { loadProtectedImageDisplayURL } from "../avatar/ProtectedImage";
import { renderHighlightedText } from "../common/searchText";
import { AssistantAvatar } from "../digitalAssistant/AssistantAvatar";
import { getSkillSourceLabel } from "./skillSourceLabel";

type DirectiveKind = "assistant" | "command" | "project";
type TokenKind = "assistant" | "skill" | "reference";
type SelectionKind = "assistant" | "skill";

type ActiveTrigger = {
	kind: DirectiveKind;
	start: number;
	end: number;
	query: string;
};

type InsertedToken = {
	label: string;
	id?: string;
	start: number;
	end: number;
	kind: TokenKind;
};

export type ComposerAssistantOption = {
	id: string;
	code: string;
	name: string;
	roleName?: string;
	description: string;
	avatarUrl?: string;
};

type AssistantOption = ComposerAssistantOption;

export type ComposerSkillOption = PluginComposerOption;

type CommandOption = {
	kind: "skill";
	item: ComposerSkillOption;
};

type EditorSnapshot = {
	text: string;
	tokens: InsertedToken[];
};

function isComposingKeyboardEvent(event: KeyboardEvent<HTMLDivElement>): boolean {
	return event.nativeEvent.isComposing || event.keyCode === 229;
}

export type StructuredComposerHandle = {
	openAssistantPicker: () => void;
	openCommandPicker: () => void;
	insertAssistant: (assistantName: string) => void;
	insertSkill: (skillLabel: string) => void;
	removeAssistant: (assistantName: string) => void;
	removeSkill: (skillLabel: string) => void;
	setContent: (value: string, tokens?: ComposerToken[]) => void;
	getComposerTokens: () => ComposerToken[];
};

type StructuredComposerProps = {
	value: string;
	onChange: (value: string) => void;
	onSubmit: () => void;
	/** 与发送按钮禁用态对齐，为 true 时 Enter / 项目态 Ctrl/Cmd+Enter 不触发提交。 */
	submitDisabled?: boolean;
	onPasteFiles: (event: ClipboardEvent<HTMLElement>) => void;
	onFocus: () => void;
	onBlur: () => void;
	placeholder: string;
	isProjectVariant: boolean;
	assistantOptions?: ComposerAssistantOption[];
	skillOptions?: ComposerSkillOption[];
	skillsLoading?: boolean;
	onAssistantPickerOpen?: () => Promise<boolean> | undefined;
	onSkillPickerOpen?: () => void;
	directivesDisabled?: boolean;
	assistantDirectivesDisabled?: boolean;
	onProjectTrigger?: (query: string, clearTrigger: () => void, dismissTrigger: () => void) => void;
	assistantSelectionMode?: "single" | "multiple";
	inputSize?: "default" | "compact";
	pickerPlacement?: "top" | "bottom";
	pickerSize?: "default" | "compact";
	prefill?: {
		id: string;
		value: string;
		tokens: ComposerToken[];
	};
	onPrefillConsumed?: (prefillId: string) => void;
};

function findTrigger(value: string, cursor: number): ActiveTrigger | null {
	const prefix = value.slice(0, cursor);
	const assistantMatch = prefix.match(/(?:^|\s)@([^\s@/#]*)$/);
	if (assistantMatch) {
		const query = assistantMatch[1] ?? "";
		return {
			kind: "assistant",
			start: cursor - query.length - 1,
			end: cursor,
			query,
		};
	}

	const commandMatch = prefix.match(/(?:^|\s)\/([^\s@/#]*)$/);
	if (commandMatch) {
		const query = commandMatch[1] ?? "";
		return {
			kind: "command",
			start: cursor - query.length - 1,
			end: cursor,
			query,
		};
	}

	const projectMatch = prefix.match(/(?:^|\s)#([^\s@/#]*)$/);
	if (projectMatch) {
		const query = projectMatch[1] ?? "";
		return {
			kind: "project",
			start: cursor - query.length - 1,
			end: cursor,
			query,
		};
	}

	return null;
}

function normalizeSearchValue(value: string): string {
	return value.trim().toLowerCase();
}

function dedupeValues(values: string[]): string[] {
	return Array.from(new Set(values.filter(Boolean)));
}

function removeTokenAtRange(
	value: string,
	tokens: InsertedToken[],
	target: InsertedToken,
): EditorSnapshot {
	let start = target.start;
	let end = target.end;
	if (value[end] === " ") {
		end += 1;
	} else if (start > 0 && value[start - 1] === " ") {
		start -= 1;
	}

	const nextValue = `${value.slice(0, start)}${value.slice(end)}`;
	const delta = start - end;
	const nextTokens = sortTokens(
		tokens
			.flatMap((token) => {
				if (
					token.kind === target.kind &&
					token.label === target.label &&
					token.start === target.start &&
					token.end === target.end
				) {
					return [];
				}
				if (token.end <= start) return [token];
				if (token.start >= end) {
					return [{ ...token, start: token.start + delta, end: token.end + delta }];
				}
				return [];
			})
			// 中文注释：删除 mention 后，只保留仍与当前文本严格对齐的 token，避免旧位置残留。
			.filter((token) => nextValue.slice(token.start, token.end) === token.label),
	);

	return { text: nextValue, tokens: nextTokens };
}

function stripAssistantTokensFromSnapshot(value: string, tokens: InsertedToken[]): EditorSnapshot {
	const assistantTokens = resolveDisplayTokens(value, tokens)
		.filter((token) => token.kind === "assistant")
		.sort((a, b) => b.start - a.start);
	if (assistantTokens.length === 0) {
		return { text: value, tokens };
	}

	return assistantTokens.reduce<EditorSnapshot>(
		(snapshot, token) => removeTokenAtRange(snapshot.text, snapshot.tokens, token),
		{ text: value, tokens },
	);
}

function stripAssistantTokensExcept(
	value: string,
	tokens: InsertedToken[],
	keepToken: InsertedToken,
): EditorSnapshot {
	const assistantTokens = resolveDisplayTokens(value, tokens)
		.filter(
			(token) =>
				token.kind === "assistant" &&
				!(
					token.label === keepToken.label &&
					token.start === keepToken.start &&
					token.end === keepToken.end
				),
		)
		.sort((a, b) => b.start - a.start);
	if (assistantTokens.length === 0) {
		return { text: value, tokens };
	}

	return assistantTokens.reduce<EditorSnapshot>(
		(snapshot, token) => removeTokenAtRange(snapshot.text, snapshot.tokens, token),
		{ text: value, tokens },
	);
}

// 中文注释：空 contenteditable 浏览器常会插入 <br>，同步后变成仅含换行的字符串，需视为空值。
function isEmptyEditorValue(value: string): boolean {
	return value.trim() === "";
}

function matchesCommandQuery(
	option: Pick<ComposerSkillOption, "label" | "code" | "description">,
	query: string,
): boolean {
	if (!query) return true;
	return [option.label, option.code, option.description].join(" ").toLowerCase().includes(query);
}

function assistantPickerValue(option: AssistantOption): string {
	// 中文注释：同名同角色的队友仍是不同实体，命令菜单必须以唯一 id 区分，避免联动高亮。
	return `assistant:${option.id}`;
}

function commandPickerValue(option: CommandOption): string {
	return `${option.kind}:${option.item.code}`;
}

function resolveVirtualAssistantTokens(
	value: string,
	tokens: InsertedToken[],
	assistantOptions: AssistantOption[] = [],
): InsertedToken[] {
	const result: InsertedToken[] = [];
	const orderedOptions = [...assistantOptions]
		.filter((assistant) => assistant.name.trim())
		.sort((a, b) => b.name.length - a.name.length);

	for (const assistant of orderedOptions) {
		const label = `@${assistant.name}`;
		const pattern = new RegExp(`(^|\\s)${escapeRegExp(label)}(?=\\s|$)`, "g");
		for (const match of value.matchAll(pattern)) {
			const matchedText = match[0] ?? "";
			const start = (match.index ?? 0) + (matchedText.startsWith("@") ? 0 : 1);
			const end = start + label.length;
			const overlapsExistingToken = [...tokens, ...result].some(
				(token) => start < token.end && end > token.start,
			);
			if (overlapsExistingToken) continue;

			result.push({
				label,
				id: assistant.id,
				start,
				end,
				kind: "assistant",
			});
		}
	}

	return result;
}

function resolveDisplayTokens(
	value: string,
	tokens: InsertedToken[],
	assistantOptions: AssistantOption[] = [],
	_skillOptions: ComposerSkillOption[] = [],
): InsertedToken[] {
	const explicitTokens = tokens.filter(
		(token) => value.slice(token.start, token.end) === token.label,
	);
	// 中文注释：召唤队友跳转时可能只恢复了文本，这里按项目队友把 @队友名 补成可发送的结构化 token。
	const assistantTokens = resolveVirtualAssistantTokens(value, explicitTokens, assistantOptions);
	return sortTokens([...explicitTokens, ...assistantTokens]);
}

function escapeRegExp(value: string): string {
	return value.replace(/[.*+?^${}()|[\]\\]/g, "\\$&");
}

function skillTokenCode(label: string): string {
	return label.startsWith("/") ? label.slice(1) : label;
}

function resolveSkillTokenCode(
	token: InsertedToken,
	skillOptions: ComposerSkillOption[] = [],
): string {
	if (token.id) return token.id;
	const raw = skillTokenCode(token.label);
	const byCode = skillOptions.find((skill) => skill.code.toLowerCase() === raw.toLowerCase());
	if (byCode) return byCode.code;
	const byLabel = skillOptions.find((skill) => skill.label === raw);
	return byLabel?.code ?? raw;
}

function formatSkillTokenDisplayLabel(
	label: string,
	skillOptions: ComposerSkillOption[] = [],
	skillCode?: string,
): string {
	if (skillCode) {
		const option = skillOptions.find(
			(skill) => skill.code.toLowerCase() === skillCode.toLowerCase(),
		);
		if (option?.label) return option.label;
	}
	const code = skillTokenCode(label);
	const option = skillOptions.find(
		(skill) => skill.code.toLowerCase() === code.toLowerCase() || skill.label === code,
	);
	return option?.label || code;
}

function formatAssistantTokenDisplayLabel(label: string): string {
	return label.startsWith("@") ? label.slice(1) : label;
}

function isCursorInsideToken(cursor: number, tokens: InsertedToken[]): boolean {
	return tokens.some((token) => cursor > token.start && cursor <= token.end);
}

function sortTokens(tokens: InsertedToken[]): InsertedToken[] {
	return [...tokens].sort((a, b) => a.start - b.start);
}

function areTokensEqual(left: InsertedToken[], right: InsertedToken[]): boolean {
	if (left.length !== right.length) return false;
	return left.every((token, index) => {
		const target = right[index];
		return (
			target &&
			token.label === target.label &&
			token.id === target.id &&
			token.start === target.start &&
			token.end === target.end &&
			token.kind === target.kind
		);
	});
}

function extractSnapshot(root: HTMLElement): EditorSnapshot {
	const tokens: InsertedToken[] = [];

	const walk = (node: Node, cursor: number): { text: string; cursor: number } => {
		if (node.nodeType === Node.TEXT_NODE) {
			const text = node.textContent ?? "";
			return { text, cursor: cursor + text.length };
		}

		if (!(node instanceof HTMLElement)) {
			return { text: "", cursor };
		}

		if (node.dataset.mentionNode === "true") {
			const label = node.dataset.mentionLabel ?? node.textContent ?? "";
			tokens.push({
				label,
				id: node.dataset.mentionId,
				start: cursor,
				end: cursor + label.length,
				kind: parseTokenKind(node.dataset.mentionKind),
			});
			return { text: label, cursor: cursor + label.length };
		}

		if (node.tagName === "BR") {
			return { text: "\n", cursor: cursor + 1 };
		}

		let text = "";
		let nextCursor = cursor;
		for (const child of Array.from(node.childNodes)) {
			const result = walk(child, nextCursor);
			text += result.text;
			nextCursor = result.cursor;
		}
		return { text, cursor: nextCursor };
	};

	let text = "";
	let cursor = 0;
	for (const child of Array.from(root.childNodes)) {
		const result = walk(child, cursor);
		text += result.text;
		cursor = result.cursor;
	}

	return {
		text,
		tokens: sortTokens(tokens),
	};
}

function parseTokenKind(value?: string): TokenKind {
	if (value === "skill" || value === "reference") return value;
	return "assistant";
}

function createSkillSparklesIcon(): SVGElement {
	const svg = document.createElementNS("http://www.w3.org/2000/svg", "svg");
	svg.setAttribute("viewBox", "0 0 24 24");
	svg.setAttribute("fill", "none");
	svg.setAttribute("stroke", "currentColor");
	svg.setAttribute("stroke-width", "2");
	svg.setAttribute("stroke-linecap", "round");
	svg.setAttribute("stroke-linejoin", "round");
	svg.setAttribute("class", "size-3.5");

	const paths = [
		"M9.937 15.5A2 2 0 0 0 8.5 14.063l-6.135-1.582a.5.5 0 0 1 0-.962L8.5 9.936A2 2 0 0 0 9.937 8.5l1.582-6.135a.5.5 0 0 1 .962 0L14.064 8.5A2 2 0 0 0 15.5 9.937l6.135 1.581a.5.5 0 0 1 0 .964L15.5 14.063a2 2 0 0 0-1.437 1.437l-1.582 6.135a.5.5 0 0 1-.962 0z",
		"M20 3v4",
		"M22 5h-4",
		"M4 17v2",
		"M5 18H3",
	];

	for (const d of paths) {
		const path = document.createElementNS("http://www.w3.org/2000/svg", "path");
		path.setAttribute("d", d);
		svg.appendChild(path);
	}

	return svg;
}

function createRemoveIcon(): SVGElement {
	const svg = document.createElementNS("http://www.w3.org/2000/svg", "svg");
	svg.setAttribute("viewBox", "0 0 24 24");
	svg.setAttribute("fill", "none");
	svg.setAttribute("stroke", "currentColor");
	svg.setAttribute("stroke-width", "2");
	svg.setAttribute("stroke-linecap", "round");
	svg.setAttribute("stroke-linejoin", "round");
	svg.setAttribute("class", "size-2.5");

	for (const d of ["M18 6 6 18", "m6 6 12 12"]) {
		const path = document.createElementNS("http://www.w3.org/2000/svg", "path");
		path.setAttribute("d", d);
		svg.appendChild(path);
	}

	return svg;
}

function createReferenceIcon(): SVGElement {
	const svg = document.createElementNS("http://www.w3.org/2000/svg", "svg");
	svg.setAttribute("viewBox", "0 0 24 24");
	svg.setAttribute("fill", "none");
	svg.setAttribute("stroke", "currentColor");
	svg.setAttribute("stroke-width", "2");
	svg.setAttribute("stroke-linecap", "round");
	svg.setAttribute("stroke-linejoin", "round");
	svg.setAttribute("class", "size-3.5");
	for (const d of ["M4 4h6v16H4z", "M14 4h6v7h-6z", "M14 15h6v5h-6z"]) {
		const path = document.createElementNS("http://www.w3.org/2000/svg", "path");
		path.setAttribute("d", d);
		svg.appendChild(path);
	}
	return svg;
}

function findMentionAssistant(
	token: InsertedToken,
	assistantOptions: AssistantOption[],
): AssistantOption | undefined {
	const assistantName = formatAssistantTokenDisplayLabel(token.label);
	return assistantOptions.find(
		(assistant) => (token.id && assistant.id === token.id) || assistant.name === assistantName,
	);
}

function buildAssistantMentionIconShell(
	token: InsertedToken,
	assistantOptions: AssistantOption[],
): HTMLSpanElement {
	const iconShell = document.createElement("span");
	iconShell.dataset.mentionRemove = "true";
	iconShell.dataset.mentionLabel = token.label;
	iconShell.dataset.mentionKind = token.kind;
	iconShell.setAttribute("role", "button");
	iconShell.setAttribute("tabindex", "-1");
	iconShell.setAttribute(
		"aria-label",
		`移除AI队友 ${formatAssistantTokenDisplayLabel(token.label)}`,
	);
	iconShell.className =
		"relative inline-flex size-4 shrink-0 cursor-pointer items-center justify-center overflow-hidden rounded-full bg-white text-blue-600";
	const assistant = findMentionAssistant(token, assistantOptions);
	const assistantName = assistant?.name ?? formatAssistantTokenDisplayLabel(token.label);
	const avatar = document.createElement("img");
	avatar.alt = assistantName;
	avatar.className = "size-4 rounded-full object-cover transition-opacity group-hover:opacity-0";
	avatar.decoding = "async";
	avatar.referrerPolicy = "no-referrer";
	// 中文注释：未上传用固定默认图；有头像但加载失败时回退 DiceBear。
	const emptyFallbackSrc = CUSTOM_ASSISTANT_DEFAULT_AVATAR_SRC;
	const loadErrorFallbackSrc =
		createDiceBearAvatarDataUri(`digital-assistant:${assistantName}`, 32) ?? emptyFallbackSrc;
	const fallbackAvatarSrc = assistant?.avatarUrl ? loadErrorFallbackSrc : emptyFallbackSrc;
	avatar.src = fallbackAvatarSrc;
	avatar.onerror = () => {
		if (avatar.src !== fallbackAvatarSrc) avatar.src = fallbackAvatarSrc;
	};
	if (assistant?.avatarUrl) {
		// 中文注释：头像可能是受保护文件 public_id，解析完成后再替换兜底头像。
		void loadProtectedImageDisplayURL(assistant.avatarUrl)
			.then((src) => {
				avatar.src = src;
			})
			.catch((error) => {
				console.error("load assistant mention avatar error:", error);
			});
	}
	const removeControl = document.createElement("span");
	removeControl.className =
		"absolute inset-0 inline-flex items-center justify-center rounded-full opacity-0 transition-opacity hover:bg-current/10 hover:opacity-100 group-hover:opacity-65";
	removeControl.appendChild(createRemoveIcon());
	iconShell.append(avatar, removeControl);
	return iconShell;
}

function buildSkillMentionIconShell(
	token: InsertedToken,
	skillOptions: ComposerSkillOption[],
): HTMLSpanElement {
	const iconShell = document.createElement("span");
	iconShell.dataset.mentionRemove = "true";
	iconShell.dataset.mentionLabel = token.label;
	iconShell.dataset.mentionKind = token.kind;
	iconShell.setAttribute("role", "button");
	iconShell.setAttribute("tabindex", "-1");
	iconShell.setAttribute(
		"aria-label",
		`移除技能 ${formatSkillTokenDisplayLabel(token.label, skillOptions, token.id)}`,
	);
	iconShell.className =
		"relative inline-flex size-4 shrink-0 cursor-pointer items-center justify-center rounded-md text-violet-600 [&_svg]:block";
	const sparklesIcon = createSkillSparklesIcon();
	sparklesIcon.classList.add("transition-opacity", "group-hover:opacity-0");
	const removeControl = document.createElement("span");
	removeControl.className =
		"absolute inset-0 inline-flex items-center justify-center rounded-full opacity-0 transition-opacity hover:bg-current/10 hover:opacity-100 group-hover:opacity-65";
	removeControl.appendChild(createRemoveIcon());
	iconShell.append(sparklesIcon, removeControl);
	return iconShell;
}

function buildReferenceMentionIconShell(token: InsertedToken): HTMLSpanElement {
	const iconShell = document.createElement("span");
	iconShell.dataset.mentionRemove = "true";
	iconShell.dataset.mentionLabel = token.label;
	iconShell.dataset.mentionKind = token.kind;
	iconShell.setAttribute("role", "button");
	iconShell.setAttribute("tabindex", "-1");
	iconShell.setAttribute("aria-label", `移除文档选区引用 ${token.label}`);
	iconShell.className =
		"relative inline-flex size-4 shrink-0 cursor-pointer items-center justify-center rounded-md text-slate-500 [&_svg]:block";
	const referenceIcon = createReferenceIcon();
	referenceIcon.classList.add("transition-opacity", "group-hover:opacity-0");
	const removeControl = document.createElement("span");
	removeControl.className =
		"absolute inset-0 inline-flex items-center justify-center rounded-full opacity-0 transition-opacity hover:bg-slate-100 hover:opacity-100 group-hover:opacity-70";
	removeControl.appendChild(createRemoveIcon());
	iconShell.append(referenceIcon, removeControl);
	return iconShell;
}

function buildEditorContent(
	root: HTMLElement,
	value: string,
	tokens: InsertedToken[],
	assistantOptions: AssistantOption[],
	skillOptions: ComposerSkillOption[] = [],
) {
	const fragment = document.createDocumentFragment();
	const orderedTokens = sortTokens(tokens);
	let cursor = 0;
	const appendPlainText = (text: string) => {
		const lines = text.split("\n");
		for (const [index, line] of lines.entries()) {
			if (index > 0) {
				fragment.appendChild(document.createElement("br"));
			}
			if (line) {
				fragment.appendChild(document.createTextNode(line));
			}
		}
	};

	for (const token of orderedTokens) {
		if (token.start > cursor) {
			// 中文注释：换行必须恢复为 <br>，否则紧跟的不可编辑 mention 可能被浏览器放到下一空行。
			appendPlainText(value.slice(cursor, token.start));
		}

		const mention = document.createElement("span");
		mention.dataset.mentionNode = "true";
		mention.dataset.mentionLabel = token.label;
		mention.dataset.mentionKind = token.kind;
		if (token.id) {
			mention.dataset.mentionId = token.id;
		}
		mention.setAttribute("contenteditable", "false");
		if (token.kind === "skill") {
			mention.className =
				"group inline-flex items-center gap-1 rounded-lg bg-violet-50 px-1.5 py-0.5 text-xs font-medium leading-5 text-violet-700 ring-1 ring-violet-100 align-baseline";
			const label = document.createElement("span");
			label.className = "truncate";
			label.textContent = formatSkillTokenDisplayLabel(token.label, skillOptions, token.id);
			mention.append(buildSkillMentionIconShell(token, skillOptions), label);
		} else if (token.kind === "reference") {
			mention.className =
				"group inline-flex max-w-[240px] items-center gap-1.5 rounded-lg border border-slate-200 bg-slate-50 px-2 py-1 text-xs font-medium leading-4 text-slate-600 align-middle";
			const label = document.createElement("span");
			label.className = "truncate";
			label.textContent = token.label;
			mention.append(buildReferenceMentionIconShell(token), label);
		} else {
			mention.className =
				"group inline-flex items-center gap-1 rounded-lg bg-blue-50 px-1.5 py-0.5 text-xs font-medium leading-5 text-blue-700 ring-1 ring-blue-100 align-baseline";
			const label = document.createElement("span");
			label.className = "truncate";
			label.textContent = formatAssistantTokenDisplayLabel(token.label);
			mention.append(buildAssistantMentionIconShell(token, assistantOptions), label);
		}
		fragment.appendChild(mention);
		cursor = token.end;
	}

	if (cursor < value.length) {
		appendPlainText(value.slice(cursor));
	}

	root.replaceChildren(fragment);
}

function setCaretOffset(root: HTMLElement, offset: number) {
	const selection = window.getSelection();
	if (!selection) return;

	const range = document.createRange();
	let remaining = offset;

	const placeAtEnd = () => {
		range.selectNodeContents(root);
		range.collapse(false);
		selection.removeAllRanges();
		selection.addRange(range);
	};

	const walk = (node: Node): boolean => {
		if (node.nodeType === Node.TEXT_NODE) {
			const textLength = node.textContent?.length ?? 0;
			if (remaining <= textLength) {
				range.setStart(node, remaining);
				range.collapse(true);
				selection.removeAllRanges();
				selection.addRange(range);
				return true;
			}
			remaining -= textLength;
			return false;
		}

		if (!(node instanceof HTMLElement)) {
			return false;
		}

		if (node.dataset.mentionNode === "true") {
			const labelLength = node.dataset.mentionLabel?.length ?? node.textContent?.length ?? 0;
			if (remaining <= labelLength) {
				range.setStartAfter(node);
				range.collapse(true);
				selection.removeAllRanges();
				selection.addRange(range);
				return true;
			}
			remaining -= labelLength;
			return false;
		}

		if (node.tagName === "BR") {
			if (remaining <= 1) {
				range.setStartAfter(node);
				range.collapse(true);
				selection.removeAllRanges();
				selection.addRange(range);
				return true;
			}
			remaining -= 1;
			return false;
		}

		for (const child of Array.from(node.childNodes)) {
			if (walk(child)) return true;
		}
		return false;
	};

	for (const child of Array.from(root.childNodes)) {
		if (walk(child)) return;
	}

	placeAtEnd();
}

function getCaretOffset(root: HTMLElement): number {
	const selection = window.getSelection();
	if (!selection || selection.rangeCount === 0) return extractSnapshot(root).text.length;

	const range = selection.getRangeAt(0);
	if (!root.contains(range.endContainer)) {
		// 中文注释：工具栏弹窗的搜索框会抢走 selection，此时插入技能应追加到输入框末尾。
		return extractSnapshot(root).text.length;
	}

	const workingRange = range.cloneRange();
	workingRange.selectNodeContents(root);
	workingRange.setEnd(range.endContainer, range.endOffset);
	return extractSnapshotFromFragment(workingRange.cloneContents()).text.length;
}

function getSelectionOffsets(root: HTMLElement): {
	start: number;
	end: number;
} {
	const selection = window.getSelection();
	if (!selection || selection.rangeCount === 0) {
		const textLength = extractSnapshot(root).text.length;
		return { start: textLength, end: textLength };
	}

	const range = selection.getRangeAt(0);
	if (!root.contains(range.startContainer) || !root.contains(range.endContainer)) {
		const textLength = extractSnapshot(root).text.length;
		return { start: textLength, end: textLength };
	}

	const startRange = range.cloneRange();
	startRange.selectNodeContents(root);
	startRange.setEnd(range.startContainer, range.startOffset);

	const endRange = range.cloneRange();
	endRange.selectNodeContents(root);
	endRange.setEnd(range.endContainer, range.endOffset);

	return {
		start: extractSnapshotFromFragment(startRange.cloneContents()).text.length,
		end: extractSnapshotFromFragment(endRange.cloneContents()).text.length,
	};
}

function extractSnapshotFromFragment(fragment: DocumentFragment): EditorSnapshot {
	const wrapper = document.createElement("div");
	wrapper.appendChild(fragment);
	return extractSnapshot(wrapper);
}

function shiftTokensForInsert(
	tokens: InsertedToken[],
	start: number,
	end: number,
	inserted: InsertedToken,
	plainTextDelta: number,
) {
	const nextTokens: InsertedToken[] = [];
	for (const token of tokens) {
		if (token.end <= start) {
			nextTokens.push(token);
			continue;
		}

		if (token.start >= end) {
			nextTokens.push({
				...token,
				start: token.start + plainTextDelta,
				end: token.end + plainTextDelta,
			});
		}
	}

	nextTokens.push(inserted);
	return sortTokens(nextTokens);
}

function shiftTokensForTextEdit(
	tokens: InsertedToken[],
	previousValue: string,
	nextValue: string,
): InsertedToken[] {
	let prefixLength = 0;
	while (
		prefixLength < previousValue.length &&
		prefixLength < nextValue.length &&
		previousValue[prefixLength] === nextValue[prefixLength]
	) {
		prefixLength += 1;
	}

	let suffixLength = 0;
	while (
		suffixLength < previousValue.length - prefixLength &&
		suffixLength < nextValue.length - prefixLength &&
		previousValue[previousValue.length - suffixLength - 1] ===
			nextValue[nextValue.length - suffixLength - 1]
	) {
		suffixLength += 1;
	}

	const previousEditEnd = previousValue.length - suffixLength;
	const delta = nextValue.length - previousValue.length;

	return sortTokens(
		tokens.flatMap((token) => {
			if (previousEditEnd <= token.start) {
				return [{ ...token, start: token.start + delta, end: token.end + delta }];
			}

			if (prefixLength >= token.end) {
				return [token];
			}

			return [];
		}),
	);
}

export const StructuredComposer = forwardRef<StructuredComposerHandle, StructuredComposerProps>(
	function StructuredComposer(
		{
			value,
			onChange,
			onSubmit,
			submitDisabled = false,
			onPasteFiles,
			onFocus,
			onBlur,
			placeholder,
			isProjectVariant,
			assistantOptions = [],
			skillOptions,
			skillsLoading,
			onAssistantPickerOpen,
			onSkillPickerOpen,
			directivesDisabled = false,
			assistantDirectivesDisabled = false,
			onProjectTrigger,
			assistantSelectionMode = "multiple",
			inputSize = "default",
			pickerPlacement = "top",
			pickerSize = "default",
			prefill,
			onPrefillConsumed,
		},
		ref,
	) {
		const editorRef = useRef<HTMLDivElement>(null);
		const pickerRef = useRef<HTMLDivElement>(null);
		const [trigger, setTrigger] = useState<ActiveTrigger | null>(null);
		const [activeIndex, setActiveIndex] = useState(0);
		const [commandSearch, setCommandSearch] = useState("");
		const [assistantSearch, setAssistantSearch] = useState("");
		const [assistantRefreshState, setAssistantRefreshState] = useState<
			"idle" | "loading" | "ready" | "error"
		>("idle");
		const [tokens, setTokens] = useState<InsertedToken[]>([]);
		const composingRef = useRef(false);
		const pendingCaretRef = useRef<number | null>(null);
		const dismissedTriggerStartRef = useRef<number | null>(null);
		const shouldAutoScrollPickerRef = useRef(false);
		const valueRef = useRef(value);
		const tokensRef = useRef<InsertedToken[]>([]);
		const appliedPrefillIdsRef = useRef<Set<string>>(new Set());
		const assistantRefreshCycleRef = useRef(0);

		const availableAssistantOptions = useMemo<AssistantOption[]>(
			() => assistantOptions,
			[assistantOptions],
		);
		const mergedSkillOptions = useMemo(() => skillOptions ?? [], [skillOptions]);
		const displayTokens = useMemo(
			() => resolveDisplayTokens(value, tokens, availableAssistantOptions, mergedSkillOptions),
			[availableAssistantOptions, mergedSkillOptions, tokens, value],
		);
		const selectedAssistantNames = useMemo(
			() =>
				dedupeValues(
					displayTokens
						.filter((token) => token.kind === "assistant")
						.map((token) => token.label.replace(/^@/, "")),
				),
			[displayTokens],
		);
		const selectedSkillCodes = useMemo(
			() =>
				dedupeValues(
					displayTokens
						.filter((token) => token.kind === "skill")
						.map((token) => resolveSkillTokenCode(token, mergedSkillOptions)),
				),
			[displayTokens, mergedSkillOptions],
		);

		const filteredAssistants = useMemo(() => {
			const query = normalizeSearchValue(trigger?.kind === "assistant" ? assistantSearch : "");
			return availableAssistantOptions.filter((assistant) => {
				if (selectedAssistantNames.includes(assistant.name)) return false;
				if (!query) return true;
				return [assistant.name, assistant.roleName, assistant.id, assistant.description]
					.join(" ")
					.toLowerCase()
					.includes(query);
			});
		}, [assistantSearch, availableAssistantOptions, selectedAssistantNames, trigger?.kind]);
		const assistantPickerLoading =
			trigger?.kind === "assistant" &&
			assistantRefreshState !== "ready" &&
			assistantRefreshState !== "error";
		const assistantPickerError =
			trigger?.kind === "assistant" && assistantRefreshState === "error"
				? "AI 队友加载失败，请关闭后重试"
				: null;

		const filteredSkills = useMemo(() => {
			const query = normalizeSearchValue(trigger?.kind === "command" ? commandSearch : "");
			return mergedSkillOptions.filter((skill) => {
				if (selectedSkillCodes.some((code) => code.toLowerCase() === skill.code.toLowerCase())) {
					return false;
				}
				return matchesCommandQuery(skill, query);
			});
		}, [commandSearch, selectedSkillCodes, mergedSkillOptions, trigger]);

		const commandOptions = useMemo<CommandOption[]>(
			() => filteredSkills.map((item) => ({ kind: "skill" as const, item })),
			[filteredSkills],
		);

		const pickerItemCount =
			trigger?.kind === "assistant"
				? filteredAssistants.length
				: trigger?.kind === "command"
					? commandOptions.length
					: 0;

		const activePickerValue = useMemo(() => {
			if (!trigger) return "";
			if (trigger.kind === "assistant") {
				const assistant = filteredAssistants[activeIndex];
				return assistant ? assistantPickerValue(assistant) : "";
			}
			if (trigger.kind !== "command") return "";
			const option = commandOptions[activeIndex];
			return option ? commandPickerValue(option) : "";
		}, [activeIndex, commandOptions, filteredAssistants, trigger]);

		const focusAt = useCallback((cursor: number) => {
			requestAnimationFrame(() => {
				const editor = editorRef.current;
				if (!editor) return;
				editor.focus();
				setCaretOffset(editor, cursor);
			});
		}, []);

		useEffect(() => {
			valueRef.current = value;
		}, [value]);

		useEffect(() => {
			tokensRef.current = tokens;
		}, [tokens]);

		const commitProgrammaticEdit = useCallback(
			(nextValue: string, nextTokens: InsertedToken[], nextCaret: number) => {
				const editor = editorRef.current;

				valueRef.current = nextValue;
				tokensRef.current = nextTokens;
				setTokens(nextTokens);
				onChange(nextValue);

				if (!editor) {
					pendingCaretRef.current = nextCaret;
					return;
				}

				// 中文注释：程序化插入 mention 后立即同步 DOM，避免首个技能在 effect 前被读成普通文本。
				buildEditorContent(
					editor,
					nextValue,
					resolveDisplayTokens(
						nextValue,
						nextTokens,
						availableAssistantOptions,
						mergedSkillOptions,
					),
					availableAssistantOptions,
					mergedSkillOptions,
				);
				pendingCaretRef.current = null;
				focusAt(nextCaret);
			},
			[availableAssistantOptions, focusAt, mergedSkillOptions, onChange],
		);

		const getActiveTrigger = useCallback((text: string, caret: number) => {
			const nextTrigger = findTrigger(text, caret);
			if (!nextTrigger) return null;
			if (nextTrigger.start === dismissedTriggerStartRef.current) return null;
			return nextTrigger;
		}, []);

		const dismissTrigger = useCallback((rememberCurrent = true) => {
			setTrigger((current) => {
				if (current?.kind === "assistant") {
					assistantRefreshCycleRef.current += 1;
					setAssistantRefreshState("idle");
				}
				if (rememberCurrent) {
					dismissedTriggerStartRef.current = current?.start ?? null;
				}
				return null;
			});
		}, []);

		useEffect(() => {
			setActiveIndex(0);
		}, [trigger?.kind, trigger?.query]);

		useEffect(() => {
			if (trigger?.kind !== "command") return;
			onSkillPickerOpen?.();
		}, [onSkillPickerOpen, trigger?.kind]);

		useEffect(() => {
			if (trigger?.kind !== "assistant") return;

			const refreshCycle = ++assistantRefreshCycleRef.current;
			setAssistantRefreshState("loading");
			void Promise.resolve(onAssistantPickerOpen?.())
				.then((succeeded) => {
					if (refreshCycle !== assistantRefreshCycleRef.current) return;
					setAssistantRefreshState(succeeded === false ? "error" : "ready");
				})
				.catch(() => {
					if (refreshCycle === assistantRefreshCycleRef.current) setAssistantRefreshState("error");
				});
		}, [onAssistantPickerOpen, trigger?.kind]);

		useEffect(() => {
			if (trigger?.kind === "command") {
				setAssistantSearch("");
				setCommandSearch(trigger.query);
				requestAnimationFrame(() => {
					// 中文注释：通过 / 打开技能选择后，焦点直接进入弹窗搜索框，避免继续输入写回外层编辑器。
					pickerRef.current
						?.querySelector<HTMLInputElement>('[data-slot="command-input"]')
						?.focus();
				});
				return;
			}

			setCommandSearch("");

			if (trigger?.kind === "assistant") {
				setAssistantSearch(trigger.query);
				requestAnimationFrame(() => {
					pickerRef.current
						?.querySelector<HTMLInputElement>('[data-slot="command-input"]')
						?.focus();
				});
				return;
			}

			setAssistantSearch("");
		}, [trigger]);

		useEffect(() => {
			if (!activePickerValue) return;
			if (!shouldAutoScrollPickerRef.current) return;

			requestAnimationFrame(() => {
				const picker = pickerRef.current;
				if (!picker) return;

				const activeItem = Array.from(
					picker.querySelectorAll<HTMLElement>("[data-picker-item-value]"),
				).find((item) => item.dataset.pickerItemValue === activePickerValue);

				activeItem?.scrollIntoView({ block: "nearest" });
				shouldAutoScrollPickerRef.current = false;
			});
		}, [activePickerValue]);

		useEffect(() => {
			const editor = editorRef.current;
			if (!editor) return;

			const resolvedTokens = resolveDisplayTokens(
				value,
				tokens,
				availableAssistantOptions,
				mergedSkillOptions,
			);
			const snapshot = extractSnapshot(editor);

			if (snapshot.text !== value || !areTokensEqual(snapshot.tokens, resolvedTokens)) {
				// 只在纯文本或 mention 结构失配时重建 DOM，避免每次输入都打断用户的光标位置。
				buildEditorContent(
					editor,
					value,
					resolvedTokens,
					availableAssistantOptions,
					mergedSkillOptions,
				);
			}

			if (pendingCaretRef.current !== null) {
				setCaretOffset(editor, pendingCaretRef.current);
				pendingCaretRef.current = null;
			}
		}, [availableAssistantOptions, mergedSkillOptions, tokens, value]);

		useEffect(() => {
			if (!isEmptyEditorValue(value)) return;
			setTokens([]);
			dismissTrigger(false);
			dismissedTriggerStartRef.current = null;
		}, [dismissTrigger, value]);

		useEffect(() => {
			if (!directivesDisabled) return;
			dismissTrigger(false);
		}, [directivesDisabled, dismissTrigger]);

		const clearProjectTrigger = useCallback(
			(activeTrigger: ActiveTrigger) => {
				const currentValue = valueRef.current;
				if (currentValue[activeTrigger.start] !== "#") return;

				const nextValue = `${currentValue.slice(
					0,
					activeTrigger.start,
				)}${currentValue.slice(activeTrigger.end)}`;
				const nextTokens = shiftTokensForTextEdit(tokensRef.current, currentValue, nextValue);
				// 中文注释：# 只是项目任务选择的触发器，完成选择后不作为正文或 mention 保留。
				commitProgrammaticEdit(nextValue, nextTokens, activeTrigger.start);
				dismissedTriggerStartRef.current = null;
			},
			[commitProgrammaticEdit],
		);

		const dismissProjectTrigger = useCallback((activeTrigger: ActiveTrigger) => {
			// 中文注释：手动关闭项目弹窗后，# 保留为正文且不再重复打开选择器，行为与 / 指令一致。
			dismissedTriggerStartRef.current = activeTrigger.start;
		}, []);

		const notifyProjectTrigger = useCallback(
			(activeTrigger: ActiveTrigger) => {
				if (!onProjectTrigger) return;
				onProjectTrigger(
					activeTrigger.query,
					() => clearProjectTrigger(activeTrigger),
					() => dismissProjectTrigger(activeTrigger),
				);
			},
			[clearProjectTrigger, dismissProjectTrigger, onProjectTrigger],
		);

		const syncFromEditor = useCallback(() => {
			const editor = editorRef.current;
			if (!editor) return;

			const snapshot = extractSnapshot(editor);
			tokensRef.current = snapshot.tokens;
			setTokens(snapshot.tokens);
			// 中文注释：仅空白/换行时归一为空串，避免 placeholder 因 \n 被误判为已输入。
			const text = isEmptyEditorValue(snapshot.text) ? "" : snapshot.text;
			valueRef.current = text;
			onChange(text);

			if (
				dismissedTriggerStartRef.current !== null &&
				!["@", "/", "#"].includes(text[dismissedTriggerStartRef.current] ?? "")
			) {
				dismissedTriggerStartRef.current = null;
			}

			if (!composingRef.current) {
				const caret = getCaretOffset(editor);
				const nextTokens = resolveDisplayTokens(
					text,
					snapshot.tokens,
					availableAssistantOptions,
					mergedSkillOptions,
				);
				if (isCursorInsideToken(caret, nextTokens)) {
					setTrigger(null);
					return;
				}

				const nextTrigger = getActiveTrigger(text, caret);
				if (nextTrigger?.kind === "project" && onProjectTrigger) {
					setTrigger(null);
					notifyProjectTrigger(nextTrigger);
					return;
				}

				const shouldBlock =
					directivesDisabled || (assistantDirectivesDisabled && nextTrigger?.kind === "assistant");
				setTrigger(shouldBlock || nextTrigger?.kind === "project" ? null : nextTrigger);
			}
		}, [
			availableAssistantOptions,
			directivesDisabled,
			assistantDirectivesDisabled,
			getActiveTrigger,
			mergedSkillOptions,
			notifyProjectTrigger,
			onChange,
		]);

		const handlePaste = useCallback(
			(event: ClipboardEvent<HTMLDivElement>) => {
				const clipboardFiles = Array.from(event.clipboardData.files);
				if (clipboardFiles.length > 0) {
					// 粘贴图片/文件时只走附件上传，不把浏览器生成的富文本或文件占位节点塞进输入框。
					event.preventDefault();
					onPasteFiles(event);
					return;
				}

				const pastedText = event.clipboardData.getData("text/plain");
				if (!pastedText) {
					return;
				}

				event.preventDefault();

				const editor = editorRef.current;
				if (!editor) return;

				const { start, end } = getSelectionOffsets(editor);
				const currentValue = valueRef.current;
				const currentTokens = tokensRef.current;
				const nextValue = `${currentValue.slice(0, start)}${pastedText}${currentValue.slice(end)}`;
				const nextCaret = start + pastedText.length;
				const nextTokens = shiftTokensForTextEdit(currentTokens, currentValue, nextValue);

				// 富文本编辑器里外部粘贴默认会带入 HTML/样式，这里统一降级成纯文本，保证展示和发送内容一致。
				valueRef.current = nextValue;
				tokensRef.current = nextTokens;
				setTokens(nextTokens);
				onChange(nextValue);
				pendingCaretRef.current = nextCaret;

				if (!composingRef.current) {
					const nextTrigger = getActiveTrigger(nextValue, nextCaret);
					if (nextTrigger?.kind === "project" && onProjectTrigger) {
						setTrigger(null);
						notifyProjectTrigger(nextTrigger);
					} else {
						setTrigger(directivesDisabled || nextTrigger?.kind === "project" ? null : nextTrigger);
					}
				}

				focusAt(nextCaret);
			},
			[
				directivesDisabled,
				focusAt,
				getActiveTrigger,
				notifyProjectTrigger,
				onChange,
				onPasteFiles,
				onProjectTrigger,
			],
		);

		const insertTrigger = useCallback(
			(kind: Exclude<DirectiveKind, "project">) => {
				if (directivesDisabled) return;
				const editor = editorRef.current;
				if (!editor) return;

				const currentValue = valueRef.current;
				const currentTokens = tokensRef.current;
				const cursor = getCaretOffset(editor);
				const marker = kind === "assistant" ? "@" : "/";
				const needsLeadingSpace = cursor > 0 && !/\s/.test(currentValue[cursor - 1] ?? "");
				const insertion = `${needsLeadingSpace ? " " : ""}${marker}`;
				const markerStart = cursor + (needsLeadingSpace ? 1 : 0);
				const nextValue = `${currentValue.slice(
					0,
					cursor,
				)}${insertion}${currentValue.slice(cursor)}`;
				const nextTokens = shiftTokensForTextEdit(currentTokens, currentValue, nextValue);

				// 工具栏触发的插入不会经过原生 input 事件，这里手动同步 mention 位置信息。
				valueRef.current = nextValue;
				tokensRef.current = nextTokens;
				setTokens(nextTokens);
				onChange(nextValue);
				pendingCaretRef.current = markerStart + 1;
				dismissedTriggerStartRef.current = null;
				setTrigger({ kind, start: markerStart, end: markerStart + 1, query: "" });
				focusAt(markerStart + 1);
			},
			[directivesDisabled, focusAt, onChange],
		);

		const insertToolbarToken = useCallback(
			(kind: TokenKind, rawLabel: string, tokenId?: string) => {
				const editor = editorRef.current;
				const currentSnapshot =
					kind === "assistant" && assistantSelectionMode === "single"
						? stripAssistantTokensFromSnapshot(valueRef.current, tokensRef.current)
						: { text: valueRef.current, tokens: tokensRef.current };
				// 中文注释：鼠标点击工具栏时，Chromium 会把行尾光标占位 <br> 同步为正文；
				// 插入标签前保留一个真实换行，其余末尾空行都视为浏览器占位，避免标签落到第三行。
				const currentValue = currentSnapshot.text.replace(/\n{2,}$/, "\n");
				const currentTokens = currentSnapshot.tokens;
				// 中文注释：单选模式下从工具栏重新选人时，统一替换为新的 AI 员工，并把光标落到末尾。
				const rawCursor =
					editor && !(kind === "assistant" && assistantSelectionMode === "single")
						? getCaretOffset(editor)
						: currentValue.length;
				// 中文注释：压缩浏览器末尾占位换行后，光标偏移也必须对齐新文本，
				// 否则 token 范围失配会退化为普通 / 指令并重新触发技能弹窗。
				const cursor = Math.min(rawCursor, currentValue.length);
				const needsLeadingSpace = cursor > 0 && !/\s/.test(currentValue[cursor - 1] ?? "");
				const needsTrailingSpace = !/\s/.test(currentValue[cursor] ?? "");
				const insertion = `${needsLeadingSpace ? " " : ""}${rawLabel}${
					needsTrailingSpace ? " " : ""
				}`;
				const tokenStart = cursor + (needsLeadingSpace ? 1 : 0);
				const nextValue = `${currentValue.slice(
					0,
					cursor,
				)}${insertion}${currentValue.slice(cursor)}`;
				const insertedToken: InsertedToken = {
					label: rawLabel,
					id: tokenId,
					start: tokenStart,
					end: tokenStart + rawLabel.length,
					kind,
				};

				const nextTokens = shiftTokensForInsert(
					currentTokens,
					cursor,
					cursor,
					insertedToken,
					insertion.length,
				);
				dismissedTriggerStartRef.current = null;
				dismissTrigger(false);
				commitProgrammaticEdit(
					nextValue,
					nextTokens,
					tokenStart + rawLabel.length + (needsTrailingSpace ? 1 : 0),
				);
			},
			[assistantSelectionMode, commitProgrammaticEdit, dismissTrigger],
		);

		const removeMentionToken = useCallback(
			(kind: TokenKind, rawLabel: string) => {
				const prefix = kind === "assistant" ? "@" : kind === "skill" ? "/" : "";
				const normalizedLabel =
					prefix && !rawLabel.startsWith(prefix) ? `${prefix}${rawLabel}` : rawLabel;
				const currentValue = valueRef.current;
				const currentTokens = resolveDisplayTokens(
					currentValue,
					tokensRef.current,
					availableAssistantOptions,
					mergedSkillOptions,
				);
				const target = currentTokens.find((token) => {
					if (token.kind !== kind) return false;
					if (token.label === normalizedLabel) return true;
					if (kind === "skill") {
						const raw = skillTokenCode(normalizedLabel);
						return token.id === raw || token.id === normalizedLabel;
					}
					return false;
				});
				if (!target) return;

				let start = target.start;
				let end = target.end;
				if (currentValue[end] === " ") {
					end += 1;
				} else if (start > 0 && currentValue[start - 1] === " ") {
					start -= 1;
				}
				const nextValue = `${currentValue.slice(0, start)}${currentValue.slice(end)}`;
				const delta = start - end;
				const nextTokens = sortTokens(
					currentTokens
						.flatMap((token) => {
							if (
								token.kind === target.kind &&
								token.label === target.label &&
								token.start === target.start &&
								token.end === target.end
							) {
								return [];
							}
							if (token.end <= start) return [token];
							if (token.start >= end) {
								return [
									{
										...token,
										start: token.start + delta,
										end: token.end + delta,
									},
								];
							}
							return [];
						})
						// 中文注释：已选区删除只影响目标 token，后续同前缀技能需保留 mention 样式。
						.filter((token) => nextValue.slice(token.start, token.end) === token.label),
				);
				// 中文注释：从已选 tag 区域移除时，同步删除输入框里的 mention token 和对应纯文本。
				dismissTrigger(false);
				commitProgrammaticEdit(nextValue, nextTokens, start);
			},
			[availableAssistantOptions, commitProgrammaticEdit, dismissTrigger, mergedSkillOptions],
		);

		const removeAssistantToken = useCallback(
			(assistantName: string) => removeMentionToken("assistant", assistantName),
			[removeMentionToken],
		);

		const removeSkillToken = useCallback(
			(skillLabel: string) => removeMentionToken("skill", skillLabel),
			[removeMentionToken],
		);

		const setContent = useCallback(
			(nextValue: string, nextComposerTokens: ComposerToken[] = []) => {
				const nextTokens = sortTokens(
					nextComposerTokens
						.map((token) => ({
							label: token.label,
							id: token.id,
							start: token.start,
							end: token.end,
							kind: token.kind,
						}))
						.filter((token) => nextValue.slice(token.start, token.end) === token.label),
				);
				dismissedTriggerStartRef.current = null;
				dismissTrigger(false);
				commitProgrammaticEdit(nextValue, nextTokens, nextValue.length);
			},
			[commitProgrammaticEdit, dismissTrigger],
		);

		useEffect(() => {
			if (!prefill) return;
			if (appliedPrefillIdsRef.current.has(prefill.id)) return;
			appliedPrefillIdsRef.current.add(prefill.id);
			setContent(prefill.value, prefill.tokens);
			onPrefillConsumed?.(prefill.id);
		}, [onPrefillConsumed, prefill, setContent]);

		const handleEditorMouseDown = useCallback(
			(event: MouseEvent<HTMLDivElement>) => {
				const target = event.target;
				const removeControl =
					target instanceof Element ? target.closest('[data-mention-remove="true"]') : null;

				if (removeControl instanceof HTMLElement && editorRef.current?.contains(removeControl)) {
					event.preventDefault();
					event.stopPropagation();

					const kind = parseTokenKind(removeControl.dataset.mentionKind);
					const label = removeControl.dataset.mentionLabel;
					if (label) {
						// 中文注释：token 内的 x 只删除对应的 mention 文本，保留输入框其他内容和 token 样式。
						removeMentionToken(kind, label);
					}
					return;
				}

				if (trigger) {
					dismissTrigger();
				}
			},
			[dismissTrigger, removeMentionToken, trigger],
		);

		useImperativeHandle(
			ref,
			() => ({
				openAssistantPicker: () => insertTrigger("assistant"),
				openCommandPicker: () => insertTrigger("command"),
				insertAssistant: (assistantName: string) =>
					insertToolbarToken("assistant", `@${assistantName}`),
				insertSkill: (skillLabel: string) => {
					const option = mergedSkillOptions.find(
						(skill) => skill.code === skillLabel || skill.label === skillLabel,
					);
					const displayLabel = option?.label || skillLabel;
					insertToolbarToken("skill", `/${displayLabel}`, option?.code ?? skillLabel);
				},
				removeAssistant: removeAssistantToken,
				removeSkill: removeSkillToken,
				setContent,
				getComposerTokens: () =>
					resolveDisplayTokens(
						valueRef.current,
						tokensRef.current,
						availableAssistantOptions,
						mergedSkillOptions,
					),
			}),
			[
				availableAssistantOptions,
				insertToolbarToken,
				insertTrigger,
				mergedSkillOptions,
				removeAssistantToken,
				removeSkillToken,
				setContent,
			],
		);

		const selectToken = useCallback(
			(
				kind: SelectionKind,
				option: AssistantOption | ComposerSkillOption,
				activeTrigger: ActiveTrigger,
			) => {
				const isAssistant = kind === "assistant";
				const assistantName = isAssistant ? (option as AssistantOption).name : "";
				const skillCode = kind === "skill" ? (option as ComposerSkillOption).code : "";
				if (isAssistant && selectedAssistantNames.includes(assistantName)) {
					dismissTrigger(false);
					return;
				}
				if (
					kind === "skill" &&
					selectedSkillCodes.some((code) => code.toLowerCase() === skillCode.toLowerCase())
				) {
					dismissTrigger(false);
					return;
				}
				const label = isAssistant
					? `@${(option as AssistantOption).name}`
					: `/${(option as ComposerSkillOption).label || (option as ComposerSkillOption).code}`;
				const currentValue = valueRef.current;
				const currentTokens = tokensRef.current;
				const followingText = currentValue.slice(activeTrigger.end);
				// 中文注释：token 后保留一个正文分隔空格，避免继续输入时被当成同一个 / 或 @ 指令查询。
				const trailingSpace = /^\s/.test(followingText) ? "" : " ";
				const nextValue = `${currentValue.slice(
					0,
					activeTrigger.start,
				)}${label}${trailingSpace}${followingText}`;
				const insertedToken: InsertedToken = {
					label,
					id: isAssistant ? (option as AssistantOption).id : skillCode || undefined,
					start: activeTrigger.start,
					end: activeTrigger.start + label.length,
					kind: isAssistant ? "assistant" : "skill",
				};
				const delta =
					label.length + trailingSpace.length - (activeTrigger.end - activeTrigger.start);
				const nextTokens = shiftTokensForInsert(
					currentTokens,
					activeTrigger.start,
					activeTrigger.end,
					insertedToken,
					delta,
				);
				const singleAssistantResult =
					isAssistant && assistantSelectionMode === "single"
						? stripAssistantTokensExcept(nextValue, nextTokens, insertedToken)
						: null;
				dismissedTriggerStartRef.current = null;
				dismissTrigger(false);
				// 中文注释：内联弹窗选择后立即重建 mention DOM，避免首个技能先落成普通文本。
				commitProgrammaticEdit(
					singleAssistantResult?.text ?? nextValue,
					singleAssistantResult?.tokens ?? nextTokens,
					activeTrigger.start + label.length + trailingSpace.length,
				);
			},
			[
				assistantSelectionMode,
				commitProgrammaticEdit,
				dismissTrigger,
				selectedAssistantNames,
				selectedSkillCodes,
			],
		);

		const selectActiveItem = useCallback(() => {
			if (!trigger) return;
			if (trigger.kind === "assistant") {
				const assistant = filteredAssistants[activeIndex];
				if (assistant) selectToken("assistant", assistant, trigger);
				return;
			}
			if (trigger.kind !== "command") return;
			const option = commandOptions[activeIndex];
			if (option) selectToken("skill", option.item, trigger);
		}, [activeIndex, commandOptions, filteredAssistants, selectToken, trigger]);

		const handlePickerValueChange = useCallback(
			(nextValue: string) => {
				if (!trigger) return;
				if (trigger.kind === "assistant") {
					const index = filteredAssistants.findIndex(
						(assistant) => assistantPickerValue(assistant) === nextValue,
					);
					if (index >= 0) setActiveIndex(index);
					return;
				}

				const index = commandOptions.findIndex(
					(option) => commandPickerValue(option) === nextValue,
				);
				if (index >= 0) setActiveIndex(index);
			},
			[commandOptions, filteredAssistants, trigger],
		);

		const removeAdjacentTokenByKeyboard = useCallback(
			(key: string) => {
				if (key !== "Backspace" && key !== "Delete") return false;
				const editor = editorRef.current;
				if (!editor) return false;

				const selection = getSelectionOffsets(editor);
				if (selection.start !== selection.end) return false;

				const currentValue = valueRef.current;
				const currentTokens = resolveDisplayTokens(
					currentValue,
					tokensRef.current,
					availableAssistantOptions,
					mergedSkillOptions,
				);
				const caret = selection.start;
				const target =
					key === "Backspace"
						? currentTokens.find(
								(token) =>
									token.end === caret ||
									(currentValue[caret - 1] === " " && token.end === caret - 1),
							)
						: currentTokens.find((token) => token.start === caret);
				if (!target) return false;

				let start = target.start;
				let end = target.end;
				if (currentValue[end] === " ") {
					end += 1;
				} else if (start > 0 && currentValue[start - 1] === " ") {
					start -= 1;
				}

				const nextValue = `${currentValue.slice(0, start)}${currentValue.slice(end)}`;
				const delta = start - end;
				const nextTokens = sortTokens(
					currentTokens.flatMap((token) => {
						if (
							token.kind === target.kind &&
							token.label === target.label &&
							token.start === target.start &&
							token.end === target.end
						) {
							return [];
						}
						if (token.end <= start) return [token];
						if (token.start >= end) {
							return [{ ...token, start: token.start + delta, end: token.end + delta }];
						}
						return [];
					}),
				);

				// 中文注释：键盘从 mention 右侧删除时同步吞掉正文分隔空格，避免需要按两次 Backspace。
				dismissTrigger(false);
				commitProgrammaticEdit(nextValue, nextTokens, start);
				return true;
			},
			[availableAssistantOptions, commitProgrammaticEdit, dismissTrigger, mergedSkillOptions],
		);

		const handleKeyDown = useCallback(
			(event: KeyboardEvent<HTMLDivElement>) => {
				const composing = composingRef.current || isComposingKeyboardEvent(event);

				if (composing && (event.key === "Enter" || event.key === "Tab")) {
					return;
				}

				if (trigger) {
					if (event.key === "ArrowDown" || event.key === "ArrowUp") {
						event.preventDefault();
						// 中文注释：只在键盘切换高亮项时自动滚动列表，避免鼠标移入触发 cmdk 高亮时出现列表跳动。
						shouldAutoScrollPickerRef.current = true;
						const direction = event.key === "ArrowDown" ? 1 : -1;
						setActiveIndex((current) => {
							if (pickerItemCount === 0) return 0;
							return (current + direction + pickerItemCount) % pickerItemCount;
						});
						return;
					}

					if ((event.key === "Enter" || event.key === "Tab") && pickerItemCount > 0) {
						event.preventDefault();
						selectActiveItem();
						return;
					}

					if (event.key === "Escape") {
						event.preventDefault();
						dismissTrigger();
						return;
					}
				}

				if (composing && (event.key === "Backspace" || event.key === "Delete")) {
					return;
				}

				if (removeAdjacentTokenByKeyboard(event.key)) {
					event.preventDefault();
					return;
				}

				const submitByEnter = event.key === "Enter" && !event.shiftKey;
				// 项目态保留 Ctrl/Cmd + Enter 作为兼容发送快捷键，避免老用户肌肉记忆突然失效。
				const submitByShortcut =
					isProjectVariant && event.key === "Enter" && (event.metaKey || event.ctrlKey);
				if (submitByEnter || submitByShortcut) {
					event.preventDefault();
					if (!submitDisabled) {
						onSubmit();
					}
				}
			},
			[
				dismissTrigger,
				isProjectVariant,
				onSubmit,
				pickerItemCount,
				removeAdjacentTokenByKeyboard,
				selectActiveItem,
				submitDisabled,
				trigger,
			],
		);

		const inputSpacingClass = isProjectVariant
			? // 中文注释：编辑器滚动区域会裁切标签外扩的 ring，四周各保留 1px 安全间距。
				inputSize === "compact"
				? "min-h-[72px] rounded-none px-px py-px text-sm leading-6"
				: "min-h-[96px] rounded-none px-px py-px text-sm leading-6"
			: "min-h-[80px] rounded-2xl px-5 py-4 pb-2 text-xs leading-5";

		return (
			<div className="relative">
				{trigger && (
					<PopoverPrimitive.Root
						open
						onOpenChange={(nextOpen) => {
							if (!nextOpen) dismissTrigger();
						}}
					>
						<PopoverPrimitive.Portal>
							<PopoverPrimitive.Positioner
								anchor={editorRef}
								positionMethod="fixed"
								side={pickerPlacement}
								sideOffset={8}
								align="start"
								collisionPadding={16}
								data-skill-picker-positioner
								className="z-[60]"
							>
								<PopoverPrimitive.Popup
									ref={pickerRef}
									role="dialog"
									aria-label={trigger.kind === "assistant" ? "选择 AI 队友" : "选择技能"}
									onBlur={() => {
										setTimeout(() => {
											const activeElement = document.activeElement;
											if (
												activeElement &&
												(pickerRef.current?.contains(activeElement) ||
													editorRef.current?.contains(activeElement))
											) {
												return;
											}
											dismissTrigger();
										}, 100);
									}}
									// 圆角容器需留足内边距，避免 overflow-hidden 裁切顶部标题文字
									className={cn(
										"overflow-hidden border border-slate-200/80 bg-white/95 shadow-[0_12px_36px_rgba(15,23,42,0.12)] backdrop-blur",
										pickerSize === "compact"
											? "w-[min(300px,calc(100vw-2rem))] rounded-xl p-1.5"
											: "w-full max-w-[360px] rounded-2xl p-2",
									)}
								>
									<Command
										shouldFilter={false}
										value={activePickerValue}
										onValueChange={handlePickerValueChange}
										className="rounded-xl! bg-transparent p-0"
									>
										<div
											className={cn(
												"px-2 py-1 font-semibold text-slate-800",
												pickerSize === "compact" ? "text-xs" : "text-sm",
											)}
										>
											{trigger.kind === "assistant" ? <>选择 AI 队友</> : <>选择技能</>}
										</div>
										<CommandInput
											value={trigger.kind === "assistant" ? assistantSearch : commandSearch}
											onValueChange={
												trigger.kind === "assistant" ? setAssistantSearch : setCommandSearch
											}
											placeholder={trigger.kind === "assistant" ? "搜索 AI 队友" : "搜索技能"}
											className="placeholder:text-slate-300"
										/>
										<CommandSeparator
											className={cn(
												"mx-1 bg-slate-200/80",
												pickerSize === "compact" ? "my-1" : "my-2",
											)}
										/>
										<CommandList
											className={cn("px-1", pickerSize === "compact" ? "max-h-48" : "max-h-60")}
										>
											{trigger.kind === "assistant" && assistantPickerLoading && (
												<div className="px-2 py-3 text-xs text-slate-400">加载 AI 队友...</div>
											)}
											{trigger.kind === "assistant" && assistantPickerError && (
												<div className="px-2 py-3 text-xs text-red-400">{assistantPickerError}</div>
											)}
											{(!assistantPickerLoading || trigger.kind !== "assistant") &&
												!assistantPickerError && (
													<CommandEmpty className="py-8 text-slate-400">没有匹配项</CommandEmpty>
												)}
											{trigger.kind === "assistant" ? (
												<CommandGroup className="p-0">
													{!assistantPickerLoading &&
														!assistantPickerError &&
														filteredAssistants.map((assistant, index) => (
															<CommandItem
																key={assistant.id}
																value={assistantPickerValue(assistant)}
																data-picker-item-value={assistantPickerValue(assistant)}
																onMouseDown={(event: MouseEvent<HTMLElement>) =>
																	event.preventDefault()
																}
																onSelect={() => selectToken("assistant", assistant, trigger)}
																className={cn(
																	pickerSize === "compact"
																		? "rounded-md px-1.5 py-1"
																		: "rounded-lg px-2 py-1.5",
																	index === activeIndex && "bg-slate-100",
																)}
															>
																<AssistantAvatar
																	name={assistant.name}
																	src={assistant.avatarUrl}
																	size="sm"
																/>
																<div className="min-w-0 flex-1">
																	<div className="truncate font-medium text-slate-700">
																		{renderHighlightedText(assistant.name, assistantSearch)}
																	</div>
																	{/* 中文注释：选择弹窗固定两行，名称在上、角色名称在下。 */}
																	{assistant.roleName ? (
																		<div className="truncate text-xs text-slate-500">
																			{renderHighlightedText(assistant.roleName, assistantSearch)}
																		</div>
																	) : null}
																</div>
															</CommandItem>
														))}
												</CommandGroup>
											) : (
												<CommandGroup className="p-0">
													{skillsLoading && (
														<div className="px-2 py-1.5 text-xs text-slate-400">加载 Skills...</div>
													)}
													{filteredSkills.map((skill, index) => (
														<CommandItem
															key={`skill-${skill.code}`}
															value={commandPickerValue({
																kind: "skill",
																item: skill,
															})}
															data-picker-item-value={commandPickerValue({
																kind: "skill",
																item: skill,
															})}
															onMouseDown={(event: MouseEvent<HTMLElement>) =>
																event.preventDefault()
															}
															onSelect={() => selectToken("skill", skill, trigger)}
															className={cn(
																pickerSize === "compact"
																	? "rounded-md px-1.5 py-1"
																	: "rounded-lg px-2 py-1.5",
																index === activeIndex && "bg-slate-100",
															)}
														>
															<div
																className={cn(
																	"flex shrink-0 items-center justify-center rounded-lg bg-violet-50 text-violet-600",
																	pickerSize === "compact" ? "size-6" : "size-7",
																)}
															>
																<Sparkles
																	className={pickerSize === "compact" ? "size-3" : "size-3.5"}
																/>
															</div>
															<div className="min-w-0 flex-1">
																<div className="flex items-center gap-1.5 truncate font-medium">
																	<span className="truncate">
																		{renderHighlightedText(skill.label, commandSearch)}
																	</span>
																	<span className="shrink-0 rounded bg-slate-100 px-1.5 py-0.5 text-[10px] font-normal leading-none text-slate-500">
																		{getSkillSourceLabel(skill)}
																	</span>
																</div>
																<div className="truncate text-xs text-slate-400">
																	{skill.description}
																</div>
															</div>
														</CommandItem>
													))}
												</CommandGroup>
											)}
										</CommandList>
									</Command>
								</PopoverPrimitive.Popup>
							</PopoverPrimitive.Positioner>
						</PopoverPrimitive.Portal>
					</PopoverPrimitive.Root>
				)}

				{isEmptyEditorValue(value) && (
					<div
						aria-hidden="true"
						className={cn(
							"pointer-events-none absolute left-0 top-0 z-10 text-slate-300",
							inputSpacingClass,
						)}
					>
						{placeholder}
					</div>
				)}

				{/* biome-ignore lint/a11y/useSemanticElements: mention 编辑区必须使用 contenteditable div 承载内联节点。 */}
				<div
					ref={editorRef}
					role="textbox"
					aria-multiline="true"
					tabIndex={0}
					contentEditable
					spellCheck={false}
					aria-label={placeholder}
					suppressContentEditableWarning
					onInput={() => syncFromEditor()}
					onKeyDown={handleKeyDown}
					onPaste={handlePaste}
					onMouseDown={handleEditorMouseDown}
					onFocus={onFocus}
					onBlur={() => {
						onBlur();
						setTimeout(() => {
							const activeElement = document.activeElement;
							if (activeElement && pickerRef.current?.contains(activeElement)) return;
							dismissTrigger();
						}, 100);
					}}
					onCompositionStart={() => {
						composingRef.current = true;
					}}
					onCompositionEnd={() => {
						syncFromEditor();
						// 中文注释：macOS 中文输入法用 Enter 确认候选时，compositionend 可能先于 keydown 触发，延迟重置避免误发送。
						window.setTimeout(() => {
							composingRef.current = false;
						}, 0);
					}}
					className={cn(
						"relative max-h-[220px] overflow-y-auto whitespace-pre-wrap break-words bg-transparent text-slate-700 caret-slate-700 focus:outline-none",
						inputSpacingClass,
					)}
				/>
			</div>
		);
	},
);
