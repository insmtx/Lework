"use client";

import type { ComposerToken, Message } from "@leros/store/types/chat";
import { parseSkillChips } from "@leros/store";
import { cn } from "@leros/ui/lib/utils";
import { PanelsTopLeft } from "lucide-react";
import type { ReactNode } from "react";
import { SkillDirectiveBadge } from "../common/SkillDirectiveBadge";

type ContentMark = {
	kind: "skill" | "assistant" | "reference";
	label: string;
	start: number;
	end: number;
};

export function MessageContentWithComposerTokens({
	message,
	className,
	inlineLayout = false,
}: {
	message: Pick<Message, "content" | "metadata">;
	className?: string;
	inlineLayout?: boolean;
}) {
	// 中文注释：部分入口会把 @队友 从实际发送内容中剥离，这里优先使用展示专用文本恢复标签样式。
	const displayContent = message.metadata?.displayContent ?? message.content;
	const marks = collectContentMarks(
		displayContent,
		message.metadata?.displayComposerTokens ?? message.metadata?.composerTokens ?? [],
	);

	// 中文注释：overflow-wrap:anywhere 让数字/emoji 等无空格长串也能在气泡 max-width 内断行。
	const textClassName = inlineLayout
		? "inline break-words [overflow-wrap:anywhere]"
		: "whitespace-pre-wrap break-words [overflow-wrap:anywhere]";

	if (marks.length === 0) {
		return <span className={cn(textClassName, className)}>{displayContent}</span>;
	}
	const mentionClassName = inlineLayout
		? "inline align-middle rounded-md bg-blue-100 px-1.5 py-0.5 text-xs font-medium leading-none text-blue-700"
		: "inline-flex max-w-full items-center rounded-md bg-blue-100 px-1.5 py-0.5 text-xs font-medium leading-none text-blue-700";

	const parts: ReactNode[] = [];
	let cursor = 0;
	marks.forEach((mark, index) => {
		if (mark.start > cursor) {
			parts.push(
				<span key={`text-${index}`} className={textClassName}>
					{displayContent.slice(cursor, mark.start)}
				</span>,
			);
		}
		parts.push(
			mark.kind === "skill" ? (
				<SkillDirectiveBadge key={`token-${index}`} name={mark.label} />
			) : mark.kind === "reference" ? (
				<span
					key={`token-${index}`}
					className="inline-flex max-w-[220px] items-center gap-1.5 rounded-lg border border-slate-200 bg-white/80 px-2 py-1 text-xs font-medium leading-none text-slate-600"
				>
					<PanelsTopLeft className="size-3.5 shrink-0" />
					<span className="truncate">{mark.label}</span>
				</span>
			) : (
				<span key={`token-${index}`} className={mentionClassName}>
					{mark.label}
				</span>
			),
		);
		cursor = mark.end;
	});

	if (cursor < displayContent.length) {
		parts.push(
			<span key="text-tail" className={textClassName}>
				{displayContent.slice(cursor)}
			</span>,
		);
	}

	return (
		<span
			className={cn(
				inlineLayout ? "inline break-words" : "whitespace-pre-wrap break-words [overflow-wrap:anywhere]",
				className,
			)}
		>
			{parts}
		</span>
	);
}

function collectContentMarks(content: string, tokens: ComposerToken[]): ContentMark[] {
	const chips = parseSkillChips(content);
	const marks: ContentMark[] = chips.map((chip) => ({
		kind: "skill",
		label: chip.label || chip.code,
		start: chip.start,
		end: chip.end,
	}));
	for (const token of tokens) {
		if (token.start < 0 || token.end > content.length) continue;
		if (content.slice(token.start, token.end) !== token.label) continue;
		// 技能只认 content 里的 <skill-chip>；metadata 里的 skill token 是旧路径残留，忽略。
		if (token.kind === "skill") continue;
		if (marks.some((mark) => token.start < mark.end && token.end > mark.start)) continue;
		marks.push({
			kind: token.kind,
			label: token.label,
			start: token.start,
			end: token.end,
		});
	}
	return marks.sort((left, right) => left.start - right.start);
}
