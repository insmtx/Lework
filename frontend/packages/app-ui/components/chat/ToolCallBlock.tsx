"use client";

import type { ToolCall } from "@leros/store/types/chat";
import { Button } from "@leros/ui/components/ui/button";
import { cn } from "@leros/ui/lib/utils";
import { Check, ChevronDown, ChevronRight, Loader2, X } from "lucide-react";
import { useState } from "react";

export function ToolCallBlock({ toolCalls }: { toolCalls: ToolCall[] }) {
	const [expanded, setExpanded] = useState(false);

	const totalCalls = toolCalls.length;
	const successCount = toolCalls.filter((tc) => tc.status === "success").length;
	const runningCount = toolCalls.filter((tc) => tc.status === "running").length;

	return (
		<div
			data-slot="tool-call-block"
			className="max-w-[min(780px,92%)] overflow-hidden rounded-lg border border-slate-200/80 bg-white/70 text-slate-500 shadow-sm"
		>
			<button
				type="button"
				onClick={() => setExpanded(!expanded)}
				className="flex w-full cursor-pointer items-center justify-between px-3 py-2 text-sm transition-colors hover:bg-slate-50/90"
			>
				<div className="flex items-center gap-2">
					{expanded ? (
						<ChevronDown className="size-3.5 text-slate-400" />
					) : (
						<ChevronRight className="size-3.5 text-slate-400" />
					)}
					<span className="font-medium text-slate-600">工具调用 ({totalCalls})</span>
					{runningCount > 0 && (
						<span className="relative flex size-2">
							<span className="absolute inline-flex size-full rounded-full bg-yellow-400 opacity-75 animate-ping" />
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
				<div className="space-y-2 border-t border-slate-200 px-3 py-2">
					{toolCalls.map((tc) => (
						<ToolCallItem key={tc.id} toolCall={tc} />
					))}
				</div>
			)}
		</div>
	);
}

function ToolCallItem({ toolCall }: { toolCall: ToolCall }) {
	const [showArgs, setShowArgs] = useState(false);
	const [showResult, setShowResult] = useState(false);
	const hasResult = toolCall.result !== undefined && toolCall.result !== null;

	return (
		<div data-slot="tool-call-item" className="space-y-1">
			<div className="flex items-center justify-between">
				<div className="flex items-center gap-2">
					{toolCall.status === "running" && (
						<Loader2 className="size-3.5 text-yellow-500 animate-spin" />
					)}
					{toolCall.status === "success" && <Check className="size-3.5 text-green-500" />}
					{toolCall.status === "error" && <X className="size-3.5 text-red-500" />}
					{toolCall.status === "pending" && (
						<span className="size-3.5 rounded-full border-2 border-slate-300" />
					)}
					<span className="text-sm font-medium text-slate-700">{toolCall.name}</span>
					{toolCall.duration && (
						<span className="text-xs text-slate-400">{toolCall.duration}ms</span>
					)}
				</div>
				<div className="flex items-center gap-1">
					<Button
						variant="ghost"
						size="icon-xs"
						className="text-slate-400 hover:text-slate-600"
						onClick={() => setShowArgs(!showArgs)}
					>
						<ChevronDown className={cn("size-3 transition-transform", showArgs && "rotate-180")} />
					</Button>
					{hasResult && (
						<Button
							variant="ghost"
							size="icon-xs"
							className="text-slate-400 hover:text-slate-600"
							onClick={() => setShowResult(!showResult)}
						>
							结果
						</Button>
					)}
				</div>
			</div>

			{showArgs && (
				<div className="rounded bg-slate-100 px-2 py-1.5 text-xs text-slate-600 overflow-x-auto">
					<pre className="whitespace-pre-wrap">{JSON.stringify(toolCall.arguments, null, 2)}</pre>
				</div>
			)}

			{showResult && hasResult && (
				<div className="rounded bg-green-50 px-2 py-1.5 text-xs text-green-700 overflow-x-auto">
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
