"use client";

import {
	Command,
	CommandEmpty,
	CommandGroup,
	CommandItem,
	CommandList,
} from "@leros/ui/components/ui/command";
import { cn } from "@leros/ui/lib/utils";
import { Bot } from "lucide-react";
import {
	forwardRef,
	useCallback,
	useEffect,
	useImperativeHandle,
	useMemo,
	useRef,
	useState,
} from "react";
import { type ChatCommand, mockAssistants, mockChatCommands } from "./mockDirectiveData";

type DirectiveKind = "assistant" | "command";

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
};

type AssistantOption = {
	code: string;
	name: string;
	description: string;
};

type EditorSnapshot = {
	text: string;
	tokens: InsertedToken[];
};

export type StructuredComposerHandle = {
	openAssistantPicker: () => void;
};

type StructuredComposerProps = {
	value: string;
	onChange: (value: string) => void;
	onSubmit: () => void;
	onPaste: (event: React.ClipboardEvent<HTMLElement>) => void;
	onFocus: () => void;
	onBlur: () => void;
	placeholder: string;
	isProjectVariant: boolean;
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
			token.end === target.end
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
		mention.setAttribute("contenteditable", "false");
		mention.className =
			"inline-flex rounded-md bg-blue-100 px-1.5 py-0.5 text-blue-700 align-baseline";
		mention.textContent = token.label;
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
	const workingRange = range.cloneRange();
	workingRange.selectNodeContents(root);
	workingRange.setEnd(range.endContainer, range.endOffset);
	return extractSnapshotFromFragment(workingRange.cloneContents()).text.length;
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
		{ value, onChange, onSubmit, onPaste, onFocus, onBlur, placeholder, isProjectVariant },
		ref,
	) {
		const editorRef = useRef<HTMLDivElement>(null);
		const [trigger, setTrigger] = useState<ActiveTrigger | null>(null);
		const [activeIndex, setActiveIndex] = useState(0);
		const [tokens, setTokens] = useState<InsertedToken[]>([]);
		const composingRef = useRef(false);
		const pendingCaretRef = useRef<number | null>(null);

		const assistantOptions = useMemo<AssistantOption[]>(() => mockAssistants, []);

		const filteredAssistants = useMemo(() => {
			const query = normalizeSearchValue(trigger?.kind === "assistant" ? trigger.query : "");
			if (!query) return assistantOptions;
			return assistantOptions.filter((assistant) =>
				[assistant.name, assistant.code, assistant.description]
					.join(" ")
					.toLowerCase()
					.includes(query),
			);
		}, [assistantOptions, trigger]);

		const filteredCommands = useMemo(() => {
			const query = normalizeSearchValue(trigger?.kind === "command" ? trigger.query : "");
			if (!query) return mockChatCommands;
			return mockChatCommands.filter((command) =>
				[command.label, command.code, command.description, ...command.keywords]
					.join(" ")
					.toLowerCase()
					.includes(query),
			);
		}, [trigger]);

		const pickerItemCount =
			trigger?.kind === "assistant" ? filteredAssistants.length : filteredCommands.length;

		useEffect(() => {
			setActiveIndex(0);
		}, [trigger?.kind, trigger?.query]);

		useEffect(() => {
			const editor = editorRef.current;
			if (!editor) return;

			const validTokens = sortTokens(
				tokens.filter((token) => value.slice(token.start, token.end) === token.label),
			);
			const snapshot = extractSnapshot(editor);

			if (snapshot.text !== value || !areTokensEqual(snapshot.tokens, validTokens)) {
				// 只在纯文本或 mention 结构失配时重建 DOM，避免每次输入都打断用户的光标位置。
				buildEditorContent(editor, value, validTokens);
			}

			if (pendingCaretRef.current !== null) {
				setCaretOffset(editor, pendingCaretRef.current);
				pendingCaretRef.current = null;
			}
		}, [tokens, value]);

		useEffect(() => {
			if (value) return;
			setTokens([]);
			setTrigger(null);
		}, [value]);

		const focusAt = useCallback((cursor: number) => {
			requestAnimationFrame(() => {
				const editor = editorRef.current;
				if (!editor) return;
				editor.focus();
				setCaretOffset(editor, cursor);
			});
		}, []);

		const syncFromEditor = useCallback(() => {
			const editor = editorRef.current;
			if (!editor) return;

			const snapshot = extractSnapshot(editor);
			setTokens(snapshot.tokens);
			onChange(snapshot.text);

			if (!composingRef.current) {
				setTrigger(findTrigger(snapshot.text, getCaretOffset(editor)));
			}
		}, [onChange]);

		const insertTrigger = useCallback(
			(kind: DirectiveKind) => {
				const editor = editorRef.current;
				if (!editor) return;

				const cursor = getCaretOffset(editor);
				const marker = kind === "assistant" ? "@" : "/";
				const needsLeadingSpace = cursor > 0 && !/\s/.test(value[cursor - 1] ?? "");
				const insertion = `${needsLeadingSpace ? " " : ""}${marker}`;
				const markerStart = cursor + (needsLeadingSpace ? 1 : 0);
				const nextValue = `${value.slice(0, cursor)}${insertion}${value.slice(cursor)}`;

				// 工具栏触发的插入不会经过原生 input 事件，这里手动同步 mention 位置信息。
				setTokens((current) => shiftTokensForTextEdit(current, value, nextValue));
				onChange(nextValue);
				pendingCaretRef.current = markerStart + 1;
				setTrigger({ kind, start: markerStart, end: markerStart + 1, query: "" });
				focusAt(markerStart + 1);
			},
			[focusAt, onChange, value],
		);

		useImperativeHandle(
			ref,
			() => ({
				openAssistantPicker: () => insertTrigger("assistant"),
			}),
			[insertTrigger],
		);

		const selectToken = useCallback(
			(
				kind: DirectiveKind,
				option: AssistantOption | ChatCommand,
				activeTrigger: ActiveTrigger,
			) => {
				const isAssistant = kind === "assistant";
				const label = `${isAssistant ? "@" : "/"}${isAssistant ? (option as AssistantOption).name : (option as ChatCommand).label}`;
				const followingText = value.slice(activeTrigger.end);
				const trailingSpace = followingText.startsWith(" ") ? "" : " ";
				const nextValue = `${value.slice(0, activeTrigger.start)}${label}${trailingSpace}${followingText}`;
				if (isAssistant) {
					const insertedToken: InsertedToken = {
						label,
						start: activeTrigger.start,
						end: activeTrigger.start + label.length,
					};
					const delta =
						label.length + trailingSpace.length - (activeTrigger.end - activeTrigger.start);

					setTokens((current) =>
						shiftTokensForInsert(
							current,
							activeTrigger.start,
							activeTrigger.end,
							insertedToken,
							delta,
						),
					);
				} else {
					setTokens((current) => shiftTokensForTextEdit(current, value, nextValue));
				}
				onChange(nextValue);
				setTrigger(null);
				// mention 节点插入后，显式恢复光标到节点后面的正文位置，避免落到不可编辑节点内部。
				pendingCaretRef.current = activeTrigger.start + label.length + trailingSpace.length;
				focusAt(activeTrigger.start + label.length + trailingSpace.length);
			},
			[focusAt, onChange, value],
		);

		const selectActiveItem = useCallback(() => {
			if (!trigger) return;
			if (trigger.kind === "assistant") {
				const assistant = filteredAssistants[activeIndex];
				if (assistant) selectToken("assistant", assistant, trigger);
				return;
			}
			const command = filteredCommands[activeIndex];
			if (command) selectToken("command", command, trigger);
		}, [activeIndex, filteredAssistants, filteredCommands, selectToken, trigger]);

		const handleKeyDown = useCallback(
			(event: React.KeyboardEvent<HTMLDivElement>) => {
				if (trigger) {
					if (event.key === "ArrowDown" || event.key === "ArrowUp") {
						event.preventDefault();
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
						setTrigger(null);
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
			[isProjectVariant, onSubmit, pickerItemCount, selectActiveItem, trigger],
		);

		const inputSpacingClass = isProjectVariant
			? "min-h-[92px] rounded-none px-0 py-0 text-base leading-7"
			: "min-h-[116px] rounded-2xl px-5 py-4 text-sm leading-6";

		return (
			<div className="relative">
				{trigger && (
					<div className="absolute bottom-full left-0 z-30 mb-2 w-full max-w-[360px] overflow-hidden rounded-2xl border border-slate-200/80 bg-white/95 p-1.5 shadow-[0_12px_36px_rgba(15,23,42,0.12)] backdrop-blur">
						<Command shouldFilter={false} className="rounded-xl! bg-transparent p-0">
							<div className="flex items-center gap-2 px-2.5 pb-1.5 pt-1 text-xs font-medium text-slate-400">
								{trigger.kind === "assistant" ? <>AI 队友</> : <>命令</>}
								{trigger.query && <span className="truncate text-slate-400">{trigger.query}</span>}
							</div>
							<CommandList className="max-h-60">
								<CommandEmpty className="py-8 text-slate-400">没有匹配项</CommandEmpty>
								<CommandGroup className="p-0">
									{trigger.kind === "assistant"
										? filteredAssistants.map((assistant, index) => (
												<CommandItem
													key={assistant.code}
													value={assistant.code}
													onMouseDown={(event) => event.preventDefault()}
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
											))
										: filteredCommands.map((command, index) => (
												<CommandItem
													key={command.code}
													value={command.code}
													onMouseDown={(event) => event.preventDefault()}
													onSelect={() => selectToken("command", command, trigger)}
													className={cn(
														"rounded-xl px-2.5 py-2",
														index === activeIndex && "bg-slate-100",
													)}
												>
													<div className="min-w-0 flex-1">
														<div className="font-medium">/{command.label}</div>
														<div className="truncate text-xs text-slate-400">
															{command.description}
														</div>
													</div>
												</CommandItem>
											))}
								</CommandGroup>
							</CommandList>
						</Command>
					</div>
				)}

				{!value && (
					<div
						aria-hidden="true"
						className={cn(
							"pointer-events-none absolute left-0 top-0 text-slate-400",
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
					onPaste={onPaste}
					onFocus={onFocus}
					onBlur={() => {
						onBlur();
						setTimeout(() => setTrigger(null), 100);
					}}
					onCompositionStart={() => {
						composingRef.current = true;
					}}
					onCompositionEnd={() => {
						composingRef.current = false;
						syncFromEditor();
					}}
					className={cn(
						"relative z-10 max-h-[220px] overflow-y-auto whitespace-pre-wrap break-words bg-transparent text-slate-700 caret-slate-700 focus:outline-none",
						inputSpacingClass,
					)}
				/>
			</div>
		);
	},
);
