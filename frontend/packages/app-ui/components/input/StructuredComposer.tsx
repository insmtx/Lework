"use client";

import { type SkillInstalledItem, skillMarketplaceApi } from "@leros/store";
import type { ComposerToken } from "@leros/store/types/chat";
import {
	Command,
	CommandEmpty,
	CommandGroup,
	CommandInput,
	CommandItem,
	CommandList,
} from "@leros/ui/components/ui/command";
import { cn } from "@leros/ui/lib/utils";
import { Bot, Sparkles } from "lucide-react";
import {
	forwardRef,
	type MouseEvent,
	useCallback,
	useEffect,
	useImperativeHandle,
	useMemo,
	useRef,
	useState,
} from "react";
import { mockAssistants } from "./mockDirectiveData";

type DirectiveKind = "assistant" | "command";
type TokenKind = "assistant" | "skill";
type SelectionKind = "assistant" | "skill";

type ActiveTrigger = {
	kind: DirectiveKind;
	start: number;
	end: number;
	query: string;
};

type InsertedToken = {
	label: string;
	start: number;
	end: number;
	kind: TokenKind;
};

type AssistantOption = {
	code: string;
	name: string;
	description: string;
};

export type ComposerSkillOption = {
	code: string;
	label: string;
	description: string;
	keywords: string[];
};

type CommandOption = {
	kind: "skill";
	item: ComposerSkillOption;
};

type EditorSnapshot = {
	text: string;
	tokens: InsertedToken[];
};

export type StructuredComposerHandle = {
	openAssistantPicker: () => void;
	openCommandPicker: () => void;
	insertAssistant: (assistantName: string) => void;
	insertSkill: (skillLabel: string) => void;
	removeAssistant: (assistantName: string) => void;
	removeSkill: (skillLabel: string) => void;
	getComposerTokens: () => ComposerToken[];
};

type StructuredComposerProps = {
	value: string;
	onChange: (value: string) => void;
	onSubmit: () => void;
	onPasteFiles: (event: React.ClipboardEvent<HTMLElement>) => void;
	onFocus: () => void;
	onBlur: () => void;
	placeholder: string;
	isProjectVariant: boolean;
	projectSkillOptions?: ComposerSkillOption[];
	directivesDisabled?: boolean;
};

function findTrigger(value: string, cursor: number): ActiveTrigger | null {
	const prefix = value.slice(0, cursor);
	const assistantMatch = prefix.match(/(?:^|\s)@([^\s@/]*)$/);
	if (assistantMatch) {
		const query = assistantMatch[1] ?? "";
		return {
			kind: "assistant",
			start: cursor - query.length - 1,
			end: cursor,
			query,
		};
	}

	const commandMatch = prefix.match(/(?:^|\s)\/([^\s@/]*)$/);
	if (commandMatch) {
		const query = commandMatch[1] ?? "";
		return {
			kind: "command",
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

// 中文注释：空 contenteditable 浏览器常会插入 <br>，同步后变成仅含换行的字符串，需视为空值。
function isEmptyEditorValue(value: string): boolean {
	return value.trim() === "";
}

function installedSkillToOption(skill: SkillInstalledItem): ComposerSkillOption {
	const label = skill.display_name || skill.name;
	return {
		code: skill.name,
		label,
		description: skill.description || skill.category || "已安装技能",
		keywords: [
			label,
			skill.name,
			skill.description,
			skill.category,
			skill.source,
			skill.trust,
		].filter(Boolean),
	};
}

function matchesCommandQuery(
	option: Pick<ComposerSkillOption, "label" | "code">,
	query: string,
): boolean {
	if (!query) return true;
	// 中文注释：技能弹窗搜索只按技能名称匹配，避免描述里的英文命中导致结果看起来不相关。
	return [option.label, option.code].join(" ").toLowerCase().includes(query);
}

function isRecord(value: unknown): value is Record<string, unknown> {
	return typeof value === "object" && value !== null;
}

function stringFromValue(value: unknown): string {
	return typeof value === "string" ? value : "";
}

function skillItemFromValue(value: unknown): SkillInstalledItem | null {
	if (!isRecord(value)) return null;

	const name = stringFromValue(value.name || value.skill_id || value.id);
	if (!name) return null;

	return {
		name,
		display_name: stringFromValue(value.display_name),
		description: stringFromValue(value.description),
		category: stringFromValue(value.category),
		source: stringFromValue(value.source || value.source_type),
		trust: stringFromValue(value.trust),
	};
}

function skillItemsFromValue(value: unknown): SkillInstalledItem[] {
	if (!Array.isArray(value)) return [];
	return value.map(skillItemFromValue).filter((item): item is SkillInstalledItem => item !== null);
}

function normalizeInstalledSkillsPayload(value: unknown): SkillInstalledItem[] {
	if (Array.isArray(value)) return skillItemsFromValue(value);
	if (!isRecord(value)) return [];

	const nestedData = value.data;
	if (isRecord(nestedData)) {
		if (Array.isArray(nestedData.skills)) {
			return skillItemsFromValue(nestedData.skills);
		}
		if (Array.isArray(nestedData.items)) {
			return skillItemsFromValue(nestedData.items);
		}
	}

	if (Array.isArray(value.skills)) return skillItemsFromValue(value.skills);
	if (Array.isArray(value.items)) return skillItemsFromValue(value.items);
	return [];
}

function assistantPickerValue(option: AssistantOption): string {
	return `assistant:${option.code}`;
}

function commandPickerValue(option: CommandOption): string {
	return `${option.kind}:${option.item.code}`;
}

function resolveDisplayTokens(value: string, tokens: InsertedToken[]): InsertedToken[] {
	// 中文注释：只有通过弹窗/按钮明确选中的队友和技能才渲染成 token，普通文本里的 @ 或 / 保持原样。
	return sortTokens(tokens.filter((token) => value.slice(token.start, token.end) === token.label));
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
				start: cursor,
				end: cursor + label.length,
				kind: node.dataset.mentionKind === "skill" ? "skill" : "assistant",
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

function createSkillSparklesIcon(): SVGElement {
	const svg = document.createElementNS("http://www.w3.org/2000/svg", "svg");
	svg.setAttribute("viewBox", "0 0 24 24");
	svg.setAttribute("fill", "none");
	svg.setAttribute("stroke", "currentColor");
	svg.setAttribute("stroke-width", "2");
	svg.setAttribute("stroke-linecap", "round");
	svg.setAttribute("stroke-linejoin", "round");
	svg.setAttribute("class", "size-3");

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

function createMentionRemoveControl(token: InsertedToken): HTMLSpanElement {
	const control = document.createElement("span");
	control.dataset.mentionRemove = "true";
	control.dataset.mentionLabel = token.label;
	control.dataset.mentionKind = token.kind;
	control.setAttribute("role", "button");
	control.setAttribute("tabindex", "-1");
	control.setAttribute(
		"aria-label",
		`移除${token.kind === "skill" ? "技能" : "AI队友"} ${token.label}`,
	);
	control.className =
		"ml-0.5 inline-flex size-3.5 shrink-0 cursor-pointer items-center justify-center rounded-full opacity-65 transition-opacity hover:bg-current/10 hover:opacity-100";
	control.appendChild(createRemoveIcon());
	return control;
}

function buildEditorContent(root: HTMLElement, value: string, tokens: InsertedToken[]) {
	const fragment = document.createDocumentFragment();
	const orderedTokens = sortTokens(tokens);
	let cursor = 0;

	for (const token of orderedTokens) {
		if (token.start > cursor) {
			fragment.appendChild(document.createTextNode(value.slice(cursor, token.start)));
		}

		const mention = document.createElement("span");
		mention.dataset.mentionNode = "true";
		mention.dataset.mentionLabel = token.label;
		mention.dataset.mentionKind = token.kind;
		mention.setAttribute("contenteditable", "false");
		if (token.kind === "skill") {
			mention.className =
				"inline-flex items-center gap-1 rounded-lg bg-violet-50 px-1.5 py-0.5 text-[11px] font-medium leading-none text-violet-700 ring-1 ring-violet-100 align-baseline";
			const iconShell = document.createElement("span");
			iconShell.className =
				"inline-flex size-3.5 shrink-0 items-center justify-center rounded-md bg-white text-violet-600";
			iconShell.appendChild(createSkillSparklesIcon());
			const label = document.createElement("span");
			label.className = "truncate";
			label.textContent = token.label;
			mention.append(iconShell, label, createMentionRemoveControl(token));
		} else {
			mention.className =
				"inline-flex items-center gap-1 rounded-md bg-blue-100 px-1.5 py-0.5 text-[11px] text-blue-700 align-baseline";
			const label = document.createElement("span");
			label.className = "truncate";
			label.textContent = token.label;
			mention.append(label, createMentionRemoveControl(token));
		}
		fragment.appendChild(mention);
		cursor = token.end;
	}

	if (cursor < value.length) {
		fragment.appendChild(document.createTextNode(value.slice(cursor)));
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
			onPasteFiles,
			onFocus,
			onBlur,
			placeholder,
			isProjectVariant,
			projectSkillOptions,
			directivesDisabled = false,
		},
		ref,
	) {
		const editorRef = useRef<HTMLDivElement>(null);
		const pickerRef = useRef<HTMLDivElement>(null);
		const [trigger, setTrigger] = useState<ActiveTrigger | null>(null);
		const [activeIndex, setActiveIndex] = useState(0);
		const [commandSearch, setCommandSearch] = useState("");
		const [tokens, setTokens] = useState<InsertedToken[]>([]);
		const [skillOptions, setSkillOptions] = useState<ComposerSkillOption[]>([]);
		const [skillsLoading, setSkillsLoading] = useState(false);
		const [skillsLoaded, setSkillsLoaded] = useState(false);
		const [skillsError, setSkillsError] = useState<string | null>(null);
		const composingRef = useRef(false);
		const pendingCaretRef = useRef<number | null>(null);
		const dismissedTriggerStartRef = useRef<number | null>(null);
		const shouldAutoScrollPickerRef = useRef(false);
		const valueRef = useRef(value);
		const tokensRef = useRef<InsertedToken[]>([]);

		const assistantOptions = useMemo<AssistantOption[]>(() => mockAssistants, []);
		const displayTokens = useMemo(() => resolveDisplayTokens(value, tokens), [tokens, value]);
		const selectedAssistantNames = useMemo(
			() =>
				dedupeValues(
					displayTokens
						.filter((token) => token.kind === "assistant")
						.map((token) => token.label.replace(/^@/, "")),
				),
			[displayTokens],
		);
		const selectedSkillLabels = useMemo(
			() =>
				dedupeValues(
					displayTokens
						.filter((token) => token.kind === "skill")
						.map((token) => token.label.replace(/^\//, "")),
				),
			[displayTokens],
		);

		const filteredAssistants = useMemo(() => {
			const query = normalizeSearchValue(trigger?.kind === "assistant" ? trigger.query : "");
			return assistantOptions.filter((assistant) => {
				if (selectedAssistantNames.includes(assistant.name)) return false;
				if (!query) return true;
				return [assistant.name, assistant.code, assistant.description]
					.join(" ")
					.toLowerCase()
					.includes(query);
			});
		}, [assistantOptions, selectedAssistantNames, trigger]);

		const filteredSkills = useMemo(() => {
			const query = normalizeSearchValue(trigger?.kind === "command" ? commandSearch : "");
			return skillOptions.filter((skill) => {
				if (selectedSkillLabels.includes(skill.label)) return false;
				return matchesCommandQuery(skill, query);
			});
		}, [commandSearch, selectedSkillLabels, skillOptions, trigger]);

		const commandOptions = useMemo<CommandOption[]>(
			() => filteredSkills.map((item) => ({ kind: "skill" as const, item })),
			[filteredSkills],
		);

		const pickerItemCount =
			trigger?.kind === "assistant" ? filteredAssistants.length : commandOptions.length;

		const activePickerValue = useMemo(() => {
			if (!trigger) return "";
			if (trigger.kind === "assistant") {
				const assistant = filteredAssistants[activeIndex];
				return assistant ? assistantPickerValue(assistant) : "";
			}
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
				buildEditorContent(editor, nextValue, resolveDisplayTokens(nextValue, nextTokens));
				pendingCaretRef.current = null;
				focusAt(nextCaret);
			},
			[focusAt, onChange],
		);

		const getActiveTrigger = useCallback((text: string, caret: number) => {
			const nextTrigger = findTrigger(text, caret);
			if (!nextTrigger) return null;
			if (nextTrigger.start === dismissedTriggerStartRef.current) return null;
			return nextTrigger;
		}, []);

		const dismissTrigger = useCallback((rememberCurrent = true) => {
			setTrigger((current) => {
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
			if (trigger?.kind !== "command") {
				setCommandSearch("");
				return;
			}

			setCommandSearch(trigger.query);
			requestAnimationFrame(() => {
				// 中文注释：通过 / 打开技能选择后，焦点直接进入弹窗搜索框，避免继续输入写回外层编辑器。
				pickerRef.current?.querySelector<HTMLInputElement>('[data-slot="command-input"]')?.focus();
			});
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

			const resolvedTokens = resolveDisplayTokens(value, tokens);
			const snapshot = extractSnapshot(editor);

			if (snapshot.text !== value || !areTokensEqual(snapshot.tokens, resolvedTokens)) {
				// 只在纯文本或 mention 结构失配时重建 DOM，避免每次输入都打断用户的光标位置。
				buildEditorContent(editor, value, resolvedTokens);
			}

			if (pendingCaretRef.current !== null) {
				setCaretOffset(editor, pendingCaretRef.current);
				pendingCaretRef.current = null;
			}
		}, [tokens, value]);

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

		useEffect(() => {
			if (projectSkillOptions) {
				setSkillOptions(projectSkillOptions);
				setSkillsLoaded(true);
				setSkillsError(null);
				setSkillsLoading(false);
				return;
			}
			if (trigger?.kind !== "command" || skillsLoaded) return;

			setSkillsLoading(true);
			setSkillsError(null);
			skillMarketplaceApi
				.installed()
				.then((resp) => {
					const raw = normalizeInstalledSkillsPayload(resp.data);
					setSkillOptions(raw.map(installedSkillToOption));
					setSkillsLoaded(true);
				})
				.catch((err: unknown) => {
					const message = err instanceof Error ? err.message : "技能加载失败";
					setSkillsError(message);
					setSkillOptions([]);
				})
				.finally(() => {
					setSkillsLoading(false);
				});
		}, [projectSkillOptions, skillsLoaded, trigger?.kind]);

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
				!["@", "/"].includes(text[dismissedTriggerStartRef.current] ?? "")
			) {
				dismissedTriggerStartRef.current = null;
			}

			if (!composingRef.current) {
				const caret = getCaretOffset(editor);
				const nextTokens = resolveDisplayTokens(text, snapshot.tokens);
				setTrigger(
					directivesDisabled || isCursorInsideToken(caret, nextTokens)
						? null
						: getActiveTrigger(text, caret),
				);
			}
		}, [directivesDisabled, getActiveTrigger, onChange]);

		const handlePaste = useCallback(
			(event: React.ClipboardEvent<HTMLDivElement>) => {
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
					setTrigger(directivesDisabled ? null : getActiveTrigger(nextValue, nextCaret));
				}

				focusAt(nextCaret);
			},
			[directivesDisabled, focusAt, getActiveTrigger, onChange, onPasteFiles],
		);

		const insertTrigger = useCallback(
			(kind: DirectiveKind) => {
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
				const nextValue = `${currentValue.slice(0, cursor)}${insertion}${currentValue.slice(cursor)}`;
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
			(kind: TokenKind, rawLabel: string) => {
				const editor = editorRef.current;
				const currentValue = valueRef.current;
				const currentTokens = tokensRef.current;
				const cursor = editor ? getCaretOffset(editor) : currentValue.length;
				const needsLeadingSpace = cursor > 0 && !/\s/.test(currentValue[cursor - 1] ?? "");
				const needsTrailingSpace = !/\s/.test(currentValue[cursor] ?? "");
				const insertion = `${needsLeadingSpace ? " " : ""}${rawLabel}${needsTrailingSpace ? " " : ""}`;
				const tokenStart = cursor + (needsLeadingSpace ? 1 : 0);
				const nextValue = `${currentValue.slice(0, cursor)}${insertion}${currentValue.slice(cursor)}`;
				const insertedToken: InsertedToken = {
					label: rawLabel,
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
			[commitProgrammaticEdit, dismissTrigger],
		);

		const removeMentionToken = useCallback(
			(kind: Extract<TokenKind, "assistant" | "skill">, rawLabel: string) => {
				const prefix = kind === "assistant" ? "@" : "/";
				const normalizedLabel = rawLabel.startsWith(prefix) ? rawLabel : `${prefix}${rawLabel}`;
				const currentValue = valueRef.current;
				const currentTokens = resolveDisplayTokens(currentValue, tokensRef.current);
				const target = currentTokens.find(
					(token) => token.kind === kind && token.label === normalizedLabel,
				);
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
								return [{ ...token, start: token.start + delta, end: token.end + delta }];
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
			[commitProgrammaticEdit, dismissTrigger],
		);

		const removeAssistantToken = useCallback(
			(assistantName: string) => removeMentionToken("assistant", assistantName),
			[removeMentionToken],
		);

		const removeSkillToken = useCallback(
			(skillLabel: string) => removeMentionToken("skill", skillLabel),
			[removeMentionToken],
		);

		const handleEditorMouseDown = useCallback(
			(event: MouseEvent<HTMLDivElement>) => {
				const target = event.target;
				const removeControl =
					target instanceof Element ? target.closest('[data-mention-remove="true"]') : null;

				if (removeControl instanceof HTMLElement && editorRef.current?.contains(removeControl)) {
					event.preventDefault();
					event.stopPropagation();

					const kind = removeControl.dataset.mentionKind === "skill" ? "skill" : "assistant";
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
				insertSkill: (skillLabel: string) => insertToolbarToken("skill", `/${skillLabel}`),
				removeAssistant: removeAssistantToken,
				removeSkill: removeSkillToken,
				getComposerTokens: () => resolveDisplayTokens(valueRef.current, tokensRef.current),
			}),
			[insertToolbarToken, insertTrigger, removeAssistantToken, removeSkillToken],
		);

		const selectToken = useCallback(
			(
				kind: SelectionKind,
				option: AssistantOption | ComposerSkillOption,
				activeTrigger: ActiveTrigger,
			) => {
				const isAssistant = kind === "assistant";
				const assistantName = isAssistant ? (option as AssistantOption).name : "";
				const skillLabel = kind === "skill" ? (option as ComposerSkillOption).label : "";
				if (isAssistant && selectedAssistantNames.includes(assistantName)) {
					dismissTrigger(false);
					return;
				}
				if (kind === "skill" && selectedSkillLabels.includes(skillLabel)) {
					dismissTrigger(false);
					return;
				}
				const label = isAssistant
					? `@${(option as AssistantOption).name}`
					: `/${(option as ComposerSkillOption).label}`;
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
				dismissedTriggerStartRef.current = null;
				dismissTrigger(false);
				// 中文注释：内联弹窗选择后立即重建 mention DOM，避免首个技能先落成普通文本。
				commitProgrammaticEdit(
					nextValue,
					nextTokens,
					activeTrigger.start + label.length + trailingSpace.length,
				);
			},
			[commitProgrammaticEdit, dismissTrigger, selectedAssistantNames, selectedSkillLabels],
		);

		const selectActiveItem = useCallback(() => {
			if (!trigger) return;
			if (trigger.kind === "assistant") {
				const assistant = filteredAssistants[activeIndex];
				if (assistant) selectToken("assistant", assistant, trigger);
				return;
			}
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

		const handleKeyDown = useCallback(
			(event: React.KeyboardEvent<HTMLDivElement>) => {
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

				const submitByEnter = event.key === "Enter" && !event.shiftKey;
				// 项目态保留 Ctrl/Cmd + Enter 作为兼容发送快捷键，避免老用户肌肉记忆突然失效。
				const submitByShortcut =
					isProjectVariant && event.key === "Enter" && (event.metaKey || event.ctrlKey);
				if (submitByEnter || submitByShortcut) {
					event.preventDefault();
					onSubmit();
				}
			},
			[dismissTrigger, isProjectVariant, onSubmit, pickerItemCount, selectActiveItem, trigger],
		);

		const inputSpacingClass = isProjectVariant
			? "min-h-[80px] rounded-none px-0 py-0 text-sm leading-6"
			: "min-h-[80px] rounded-2xl px-5 py-4 pb-2 text-xs leading-5";

		return (
			<div className="relative">
				{trigger && (
					<div
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
						className="absolute bottom-full left-0 z-30 mb-2 w-full max-w-[360px] overflow-hidden rounded-2xl border border-slate-200/80 bg-white/95 p-2 shadow-[0_12px_36px_rgba(15,23,42,0.12)] backdrop-blur"
					>
						<Command
							shouldFilter={false}
							value={activePickerValue}
							onValueChange={handlePickerValueChange}
							className="rounded-xl! bg-transparent p-0"
						>
							<div className="flex items-center gap-2 px-2 py-1 text-xs font-medium text-slate-400">
								{trigger.kind === "assistant" ? <>AI 队友</> : <>选择技能</>}
								{trigger.kind === "assistant" && trigger.query && (
									<span className="truncate text-slate-400">{trigger.query}</span>
								)}
							</div>
							{trigger.kind === "command" && (
								<CommandInput
									value={commandSearch}
									onValueChange={setCommandSearch}
									placeholder="搜索技能"
								/>
							)}
							<CommandList className="max-h-60">
								<CommandEmpty className="py-8 text-slate-400">没有匹配项</CommandEmpty>
								{trigger.kind === "assistant" ? (
									<CommandGroup className="p-0">
										{filteredAssistants.map((assistant, index) => (
											<CommandItem
												key={assistant.code}
												value={assistantPickerValue(assistant)}
												data-picker-item-value={assistantPickerValue(assistant)}
												onMouseDown={(event: MouseEvent) => event.preventDefault()}
												onSelect={() => selectToken("assistant", assistant, trigger)}
												className={cn(
													"rounded-xl px-2.5 py-2",
													index === activeIndex && "bg-slate-100",
												)}
											>
												<div className="flex size-7 shrink-0 items-center justify-center rounded-lg bg-blue-50 text-blue-600">
													<Bot className="size-4" />
												</div>
												<div className="min-w-0 flex-1">
													<div className="truncate font-medium text-slate-700">
														{assistant.name}
													</div>
													<div className="truncate text-xs text-slate-400">
														{assistant.description}
													</div>
												</div>
											</CommandItem>
										))}
									</CommandGroup>
								) : (
									<CommandGroup heading="Skills" className="p-0">
										{skillsLoading && (
											<div className="px-2.5 py-2 text-xs text-slate-400">加载 Skills...</div>
										)}
										{!skillsLoading && skillsError && (
											<div className="px-2.5 py-2 text-xs text-red-400">{skillsError}</div>
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
												onMouseDown={(event: MouseEvent) => event.preventDefault()}
												onSelect={() => selectToken("skill", skill, trigger)}
												className={cn(
													"rounded-xl px-2.5 py-2",
													index === activeIndex && "bg-slate-100",
												)}
											>
												<div className="flex size-7 shrink-0 items-center justify-center rounded-lg bg-violet-50 text-violet-600">
													<Sparkles className="size-3.5" />
												</div>
												<div className="min-w-0 flex-1">
													<div className="truncate font-medium">/{skill.label}</div>
													<div className="truncate text-xs text-slate-400">{skill.description}</div>
												</div>
											</CommandItem>
										))}
									</CommandGroup>
								)}
							</CommandList>
						</Command>
					</div>
				)}

				{isEmptyEditorValue(value) && (
					<div
						aria-hidden="true"
						className={cn(
							"pointer-events-none absolute left-0 top-0 z-10 text-slate-400",
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
						composingRef.current = false;
						syncFromEditor();
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
