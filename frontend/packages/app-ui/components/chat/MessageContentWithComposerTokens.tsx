"use client";

import type { Message } from "@leros/store/types/chat";
import { cn } from "@leros/ui/lib/utils";
import { PanelsTopLeft } from "lucide-react";
import type { ReactNode } from "react";
import { SkillDirectiveBadge } from "../common/SkillDirectiveBadge";

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
	const tokens = (message.metadata?.displayComposerTokens ?? message.metadata?.composerTokens ?? [])
		.filter((token) => displayContent.slice(token.start, token.end) === token.label)
		.sort((a, b) => a.start - b.start);

	// 中文注释：overflow-wrap:anywhere 让数字/emoji 等无空格长串也能在气泡 max-width 内断行。
	const textClassName = inlineLayout
		? "inline break-words [overflow-wrap:anywhere]"
		: "whitespace-pre-wrap break-words [overflow-wrap:anywhere]";

	if (tokens.length === 0) {
		// 中文注释：没有明确 token metadata 时，普通内容里的 @ 和 / 必须原样展示，不能靠文本猜样式。
		return <span className={cn(textClassName, className)}>{displayContent}</span>;
	}
	const mentionClassName = inlineLayout
		? "inline align-middle rounded-md bg-blue-100 px-1.5 py-0.5 text-xs font-medium leading-none text-blue-700"
		: "inline-flex max-w-full items-center rounded-md bg-blue-100 px-1.5 py-0.5 text-xs font-medium leading-none text-blue-700";

	const parts: ReactNode[] = [];
	let cursor = 0;
	tokens.forEach((token, index) => {
		if (token.start > cursor) {
			parts.push(
				<span key={`text-${index}`} className={textClassName}>
					{displayContent.slice(cursor, token.start)}
				</span>,
			);
		}
		parts.push(
			token.kind === "skill" ? (
				<SkillDirectiveBadge
					key={`token-${index}`}
					name={token.label.replace(/^\/+/, "")}
					variant="on-blue"
				/>
			) : token.kind === "reference" ? (
				<span
					key={`token-${index}`}
					className="inline-flex max-w-[220px] items-center gap-1.5 rounded-lg border border-slate-200 bg-white/80 px-2 py-1 text-xs font-medium leading-none text-slate-600"
				>
					<PanelsTopLeft className="size-3.5 shrink-0" />
					<span className="truncate">{token.label}</span>
				</span>
			) : (
				<span key={`token-${index}`} className={mentionClassName}>
					{token.label}
				</span>
			),
		);
		cursor = token.end;
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
				inlineLayout ? "inline break-words" : "inline-flex flex-wrap items-center gap-1.5",
				className,
			)}
		>
			{parts}
		</span>
	);
}
