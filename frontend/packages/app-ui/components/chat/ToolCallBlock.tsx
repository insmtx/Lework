"use client";

import type { ToolCall } from "@leros/store/types/chat";
import { cn } from "@leros/ui/lib/utils";
import { Check, ChevronDown, ChevronRight, FileText, Loader2, Settings, X } from "lucide-react";
import { type ReactNode, useEffect, useRef, useState } from "react";

const PROCESS_TIMELINE_TEXT_CLASS = "text-[#63748B]";
const PROCESS_TIMELINE_CHEVRON_CLASS = "text-slate-400";
const TOOL_CALL_SUCCESS_COUNT_CLASS = "text-[#2AC350]";
const TOOL_CALL_ERROR_COUNT_CLASS = "text-[#F33D3D]";
const TOOL_CALL_ERROR_OUTPUT_BG_CLASS = "bg-[#FDF1F1]";

export function ToolCallBlock({
	toolCalls,
	variant = "default",
}: {
	toolCalls: ToolCall[];
	variant?: "default" | "timeline";
}) {
	const [expanded, setExpanded] = useState(false);
	const isTimelineVariant = variant === "timeline";

	const successCount = toolCalls.filter((tc) => tc.status === "success").length;
	const errorCount = toolCalls.filter((tc) => tc.status === "error").length;
	const runningCount = toolCalls.filter((tc) => tc.status === "running").length;
	const summaryLabel = formatToolCallsSummary(toolCalls);

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
				onClick={() => setExpanded((value) => !value)}
				className={cn(
					"flex w-full min-w-0 cursor-pointer items-center justify-between transition-colors",
					isTimelineVariant
						? "px-0 py-0 text-[13px] hover:bg-transparent"
						: "px-3 py-2 text-sm hover:bg-slate-50/90",
				)}
			>
				<div className="flex min-w-0 items-center gap-2">
					{expanded ? (
						<ChevronDown
							className={cn(
								"size-3.5 shrink-0",
								isTimelineVariant
									? PROCESS_TIMELINE_CHEVRON_CLASS
									: "text-[color:var(--leros-chat-text-muted)]",
							)}
						/>
					) : (
						<ChevronRight
							className={cn(
								"size-3.5 shrink-0",
								isTimelineVariant
									? PROCESS_TIMELINE_CHEVRON_CLASS
									: "text-[color:var(--leros-chat-text-muted)]",
							)}
						/>
					)}
					<span
						className={cn(
							"truncate font-medium",
							isTimelineVariant ? cn("text-[13px]", PROCESS_TIMELINE_TEXT_CLASS) : "text-slate-600",
						)}
					>
						{summaryLabel}
					</span>
				</div>
				{!expanded && (
					<ToolCallStatusSummary
						successCount={successCount}
						errorCount={errorCount}
						runningCount={runningCount}
					/>
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

export function ProcessToolCallItems({ toolCalls }: { toolCalls: ToolCall[] }) {
	return (
		<div className="space-y-2 pt-1">
			{toolCalls.map((toolCall) => (
				<ToolCallItem key={toolCall.id} toolCall={toolCall} compact />
			))}
		</div>
	);
}

export function ToolCallStatusSummary({
	successCount,
	errorCount,
	runningCount,
	className,
}: {
	successCount: number;
	errorCount: number;
	runningCount?: number;
	className?: string;
}) {
	const segments: ReactNode[] = [];

	if (successCount > 0) {
		segments.push(
			<span key="success">
				成功<span className={TOOL_CALL_SUCCESS_COUNT_CLASS}>{successCount}</span>
			</span>,
		);
	}

	if (errorCount > 0) {
		segments.push(
			<span key="error">
				失败<span className={TOOL_CALL_ERROR_COUNT_CLASS}>{errorCount}</span>
			</span>,
		);
	}

	if (!segments.length && (runningCount ?? 0) > 0) {
		return <span className={cn("shrink-0 text-xs text-yellow-600", className)}>执行中</span>;
	}

	if (!segments.length) return null;

	return (
		<div className={cn("shrink-0 text-xs", PROCESS_TIMELINE_TEXT_CLASS, className)}>
			{successCount > 0 && (
				<span>
					成功<span className={TOOL_CALL_SUCCESS_COUNT_CLASS}>{successCount}</span>
				</span>
			)}
			{successCount > 0 && errorCount > 0 && ","}
			{errorCount > 0 && (
				<span>
					失败<span className={TOOL_CALL_ERROR_COUNT_CLASS}>{errorCount}</span>
				</span>
			)}
		</div>
	);
}

function ToolCallItem({ toolCall, compact = false }: { toolCall: ToolCall; compact?: boolean }) {
	const [expanded, setExpanded] = useState(false);
	const [argsExpanded, setArgsExpanded] = useState(true);
	const [resultExpanded, setResultExpanded] = useState(true);
	const hasResult = toolCall.result !== undefined && toolCall.result !== null;
	const isErrorResult = toolCall.status === "error";

	const toggleExpanded = () => {
		setExpanded((value) => {
			const nextExpanded = !value;
			if (nextExpanded) {
				setArgsExpanded(true);
				setResultExpanded(true);
			}
			return nextExpanded;
		});
	};

	return (
		<div data-slot="tool-call-item" className="min-w-0">
			<button
				type="button"
				onClick={toggleExpanded}
				className={cn(
					"flex h-5 w-full min-w-0 cursor-pointer items-center gap-1 text-left leading-none",
					compact ? "text-[13px]" : "text-sm",
					compact ? PROCESS_TIMELINE_TEXT_CLASS : "text-[color:var(--leros-chat-text-muted)]",
				)}
			>
				<ToolCallIconSlot>
					{expanded ? (
						<ChevronDown
							className={
								compact
									? PROCESS_TIMELINE_CHEVRON_CLASS
									: "text-[color:var(--leros-chat-text-muted)]"
							}
						/>
					) : (
						<ChevronRight
							className={
								compact
									? PROCESS_TIMELINE_CHEVRON_CLASS
									: "text-[color:var(--leros-chat-text-muted)]"
							}
						/>
					)}
				</ToolCallIconSlot>
				<ToolCallIconSlot>
					{toolCall.status === "running" && (
						<Loader2
							className={cn(
								"animate-spin",
								compact ? PROCESS_TIMELINE_TEXT_CLASS : "text-[color:var(--leros-chat-text-muted)]",
							)}
						/>
					)}
					{toolCall.status === "success" && <Check className="text-green-500" />}
					{toolCall.status === "error" && <X className="text-red-500" />}
					{toolCall.status === "pending" && (
						<span
							className={cn(
								"size-2 rounded-full border-2",
								compact ? "border-[#63748B]" : "border-slate-300",
							)}
						/>
					)}
				</ToolCallIconSlot>
				<span
					className={cn(
						"min-w-0 truncate font-medium leading-5",
						compact ? cn("text-[13px]", PROCESS_TIMELINE_TEXT_CLASS) : "text-sm text-slate-700",
					)}
				>
					{toolCall.name}
				</span>
				{toolCall.duration && (
					<span
						className={cn(
							"shrink-0 text-xs leading-5",
							compact ? PROCESS_TIMELINE_TEXT_CLASS : "text-slate-400",
						)}
					>
						{toolCall.duration}ms
					</span>
				)}
			</button>

			{expanded && (
				<div className="mt-2 space-y-2 pl-5">
					<ToolCallDetailSection
						icon={Settings}
						label="输入参数"
						expanded={argsExpanded}
						onToggle={() => setArgsExpanded((value) => !value)}
						variant="input"
						compact={compact}
					>
						<pre className="whitespace-pre-wrap">{JSON.stringify(toolCall.arguments, null, 2)}</pre>
					</ToolCallDetailSection>

					<ToolCallDetailSection
						icon={FileText}
						label="输出结果"
						expanded={resultExpanded}
						onToggle={() => setResultExpanded((value) => !value)}
						variant="output"
						isError={isErrorResult}
						compact={compact}
					>
						<pre className="whitespace-pre-wrap">
							{hasResult ? formatToolCallValue(toolCall.result) : "等待输出..."}
						</pre>
					</ToolCallDetailSection>
				</div>
			)}
		</div>
	);
}

function ToolCallDetailSection({
	icon: Icon,
	label,
	expanded,
	onToggle,
	variant,
	isError = false,
	compact = false,
	children,
}: {
	icon: typeof Settings;
	label: string;
	expanded: boolean;
	onToggle: () => void;
	variant: "input" | "output";
	isError?: boolean;
	compact?: boolean;
	children: ReactNode;
}) {
	const [showBottomFade, setShowBottomFade] = useState(false);
	const scrollContainerRef = useRef<HTMLDivElement>(null);

	const updateBottomFade = (container: HTMLDivElement) => {
		const distanceToBottom = container.scrollHeight - container.scrollTop - container.clientHeight;
		const hasOverflow = container.scrollHeight > container.clientHeight + 1;
		setShowBottomFade(hasOverflow && distanceToBottom > 8);
	};

	useEffect(() => {
		const container = scrollContainerRef.current;
		if (!expanded || !container) {
			setShowBottomFade(false);
			return;
		}

		updateBottomFade(container);
	}, [expanded, children]);

	const outputTextClassName = compact ? PROCESS_TIMELINE_TEXT_CLASS : "text-slate-600";

	const contentClassName =
		variant === "input"
			? "bg-blue-50 text-slate-600"
			: isError
				? cn(TOOL_CALL_ERROR_OUTPUT_BG_CLASS, outputTextClassName)
				: cn("bg-green-50", outputTextClassName);

	const fadeClassName =
		variant === "input"
			? "from-blue-50 via-blue-50/30 to-blue-50/0"
			: isError
				? "from-[#FDF1F1] via-[#FDF1F1]/30 to-[#FDF1F1]/0"
				: "from-green-50 via-green-50/30 to-green-50/0";

	return (
		<div className="min-w-0">
			<button
				type="button"
				onClick={onToggle}
				className={cn(
					"flex w-full cursor-pointer items-center gap-1 text-left",
					compact
						? cn("text-[13px]", PROCESS_TIMELINE_TEXT_CLASS)
						: "text-sm text-[color:var(--leros-chat-text-muted)]",
				)}
			>
				{expanded ? (
					<ChevronDown
						className={cn(
							"size-3.5 shrink-0",
							compact
								? PROCESS_TIMELINE_CHEVRON_CLASS
								: "text-[color:var(--leros-chat-text-muted)]",
						)}
					/>
				) : (
					<ChevronRight
						className={cn(
							"size-3.5 shrink-0",
							compact
								? PROCESS_TIMELINE_CHEVRON_CLASS
								: "text-[color:var(--leros-chat-text-muted)]",
						)}
					/>
				)}
				<Icon
					className={cn(
						"size-3.5 shrink-0",
						compact ? PROCESS_TIMELINE_TEXT_CLASS : "text-[color:var(--leros-chat-text-muted)]",
					)}
				/>
				<span className="font-medium">{label}</span>
			</button>
			{expanded && (
				<div className="relative mt-1">
					<div
						ref={scrollContainerRef}
						onScroll={(event) => updateBottomFade(event.currentTarget)}
						className={cn(
							"no-scrollbar max-h-[min(45vh,25rem)] overflow-y-auto overflow-x-auto rounded px-2.5 py-2 text-xs leading-5",
							contentClassName,
						)}
					>
						{children}
					</div>
					{showBottomFade && (
						<div
							className={cn(
								"pointer-events-none absolute inset-x-0 bottom-0 h-20 rounded-b bg-gradient-to-t",
								fadeClassName,
							)}
						/>
					)}
				</div>
			)}
		</div>
	);
}

function ToolCallIconSlot({ children }: { children: ReactNode }) {
	return (
		<span className="inline-flex size-3.5 shrink-0 items-center justify-center [&>svg]:size-3.5">
			{children}
		</span>
	);
}

function formatToolCallsSummary(toolCalls: ToolCall[]): string {
	const names = toolCalls.map((toolCall) => toolCall.name.trim()).filter(Boolean);
	if (!names.length) return "工具调用";
	return `工具调用：${names.join("、")}`;
}

export function formatProcessToolCallsLabel(toolCalls: ToolCall[]): string {
	const names = toolCalls.map((toolCall) => toolCall.name.trim()).filter(Boolean);
	if (!names.length) return "工具调用";
	return `工具调用：${names.map((name) => `${name}工具`).join("、")}`;
}

function formatToolCallValue(value: unknown): string {
	if (typeof value === "string") return value;
	return JSON.stringify(value, null, 2) ?? String(value);
}
