"use client";

import type { ToolCall } from "@leros/store/types/chat";
import { Button } from "@leros/ui/components/ui/button";
import { cn } from "@leros/ui/lib/utils";
import { Check, ChevronDown, ChevronRight, Loader2, X } from "lucide-react";
import { useState } from "react";

export function ToolCallBlock({
	toolCalls,
	variant = "default",
}: {
	toolCalls: ToolCall[];
	variant?: "default" | "timeline";
}) {
	const [expanded, setExpanded] = useState(false);
	const isTimelineVariant = variant === "timeline";

	const totalCalls = toolCalls.length;
	const successCount = toolCalls.filter((tc) => tc.status === "success").length;
	const runningCount = toolCalls.filter((tc) => tc.status === "running").length;

	return (
		<div
			data-slot="tool-call-block"
			className={cn(
				"max-w-full overflow-hidden text-slate-500",
				isTimelineVariant
					? "rounded-none border-0 bg-transparent shadow-none"
					: "rounded-lg border border-slate-200/80 bg-white/70 shadow-sm",
			)}
		>
			<button
				type="button"
				onClick={() => setExpanded(!expanded)}
				className={cn(
					"flex w-full cursor-pointer items-center justify-between transition-colors",
					isTimelineVariant
						? "px-0 py-0 text-[13px] hover:bg-transparent"
						: "px-3 py-2 text-sm hover:bg-slate-50/90",
				)}
			>
				<div className="flex items-center gap-2">
					{expanded ? (
						<ChevronDown className="size-3.5 text-slate-400" />
					) : (
						<ChevronRight className="size-3.5 text-slate-400" />
					)}
					<span
						className={cn(
							"font-medium",
							isTimelineVariant
								? "text-[13px] text-[color:var(--leros-chat-text-muted)]"
								: "text-slate-600",
						)}
					>
						工具调用 ({totalCalls})
					</span>
					{runningCount > 0 && (
						<span className="relative flex size-2">
							<span className="absolute inline-flex size-full animate-ping rounded-full bg-yellow-400 opacity-75" />
							<span className="relative inline-flex size-2 rounded-full bg-yellow-500" />
						</span>
					)}
				</div>
				{!expanded && (
					<div className="flex items-center gap-1.5 text-xs">
						{successCount > 0 && <span className="text-green-600">{successCount} 完成</span>}
						{runningCount > 0 && <span className="text-yellow-600">{runningCount} 执行中</span>}
					</div>
				)}
			</button>

			{expanded && (
				<div
					className={cn(
						"space-y-2",
						isTimelineVariant ? "pt-1" : "border-t border-slate-200 px-3 py-2",
					)}
				>
					{toolCalls.map((tc) => (
						<ToolCallItem key={tc.id} toolCall={tc} compact={isTimelineVariant} />
					))}
				</div>
			)}
		</div>
	);
}

function ToolCallItem({ toolCall, compact = false }: { toolCall: ToolCall; compact?: boolean }) {
	const [showArgs, setShowArgs] = useState(false);
	const [showResult, setShowResult] = useState(false);
	const hasResult = toolCall.result !== undefined && toolCall.result !== null;

	const toggleResult = () => {
		setShowResult(!showResult);
	};

	const headerClassName = cn(
		"flex w-full items-center justify-between rounded py-0.5",
		hasResult && "cursor-pointer",
	);

	const headerContent = (
		<>
			<div className="flex items-center gap-2">
				{toolCall.status === "running" && (
					<Loader2 className="size-3.5 animate-spin text-yellow-500" />
				)}
				{toolCall.status === "success" && <Check className="size-3.5 text-green-500" />}
				{toolCall.status === "error" && <X className="size-3.5 text-red-500" />}
				{toolCall.status === "pending" && (
					<span className="size-3.5 rounded-full border-2 border-slate-300" />
				)}
				<span
					className={cn(
						"font-medium",
						compact
							? "text-[13px] text-[color:var(--leros-chat-text-muted)]"
							: "text-sm text-slate-700",
					)}
				>
					{toolCall.name}
				</span>
				{toolCall.duration && <span className="text-xs text-slate-400">{toolCall.duration}ms</span>}
			</div>
			<div className="flex items-center gap-1">
				<Button
					variant="ghost"
					size="icon-xs"
					className="text-slate-400 hover:text-slate-600"
					onClick={(e) => {
						e.stopPropagation();
						setShowArgs(!showArgs);
					}}
				>
					<ChevronDown className={cn("size-3 transition-transform", showArgs && "rotate-180")} />
				</Button>
				{hasResult && (
					<span className={cn("text-xs text-slate-400", showResult && "text-slate-600")}>结果</span>
				)}
			</div>
		</>
	);

	return (
		<div data-slot="tool-call-item" className="space-y-1">
			{hasResult ? (
				<button
					type="button"
					onClick={toggleResult}
					className={cn(headerClassName, "border-0 bg-transparent p-0 text-left")}
				>
					{headerContent}
				</button>
			) : (
				<div className={headerClassName}>{headerContent}</div>
			)}

			{showArgs && (
				<div className="overflow-x-auto rounded bg-slate-100 px-2 py-1.5 text-xs text-slate-600">
					<pre className="whitespace-pre-wrap">{JSON.stringify(toolCall.arguments, null, 2)}</pre>
				</div>
			)}

			{showResult && hasResult && (
				<div className="overflow-x-auto rounded bg-green-50 px-2 py-1.5 text-xs text-green-700">
					<pre className="whitespace-pre-wrap">{formatToolCallValue(toolCall.result)}</pre>
				</div>
			)}
		</div>
	);
}

function formatToolCallValue(value: unknown): string {
	if (typeof value === "string") return value;
	return JSON.stringify(value, null, 2) ?? String(value);
}
