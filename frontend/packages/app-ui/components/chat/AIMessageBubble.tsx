"use client";

import {
	formatArtifactTime,
	formatTime,
	getAssistantMessageFooterSegments,
	mapBackendArtifactToProjectArtifact,
	mergeProjectArtifacts,
	messageArtifactToProjectArtifact,
	type ProjectArtifact,
	useChatStore,
	useLayoutStore,
} from "@leros/store";
import { artifactApi } from "@leros/store/api/artifactApi";
import type {
	Message,
	MessageArtifact,
	MessageProcessStep,
	ToolCall,
} from "@leros/store/types/chat";
import { Button } from "@leros/ui/components/ui/button";
import {
	Brain,
	Check,
	ChevronDown,
	ChevronRight,
	Copy,
	LoaderCircle,
	RefreshCw,
	Rows3,
	Wrench,
} from "lucide-react";
import { useEffect, useMemo, useRef, useState } from "react";
import {
	SHOW_ASSISTANT_MESSAGE_METRICS,
	SHOW_ASSISTANT_MESSAGE_REGENERATE_BUTTON,
} from "../../constants/temporaryUiFlags";
import { MarkdownRenderer } from "../common/MarkdownRenderer";
import { ArtifactPreviewDialog } from "../layout/ArtifactPreviewDialog";
import { ProjectFileTypeIcon } from "../layout/project-file-type-icon";
import { AssistantChatAvatar } from "./AssistantChatAvatar";
import { ToolCallBlock } from "./ToolCallBlock";

// Button 的 size 只支持预设枚举，这里用受支持的尺寸并通过 className 微调成更紧凑的操作按钮。
const compactActionButtonClassName = "size-[26px]";

function CopyButton({ text }: { text: string }) {
	const [copied, setCopied] = useState(false);
	const handleCopy = () => {
		navigator.clipboard.writeText(text);
		setCopied(true);
		setTimeout(() => setCopied(false), 1500);
	};
	return (
		<Button
			variant="ghost"
			size="icon-xs"
			className={`${compactActionButtonClassName} ${
				copied ? "text-green-500" : "text-slate-400 hover:text-slate-600"
			}`}
			onClick={handleCopy}
		>
			{copied ? <Check className="size-3.5" /> : <Copy className="size-3.5" />}
		</Button>
	);
}

export function AIMessageBubble({
	message,
	isStreaming,
	projectId,
}: {
	message: Message;
	isStreaming: boolean;
	projectId?: string;
}) {
	const { resendMessage } = useChatStore((s) => s);
	const content = message.content;
	const hasContent = content.trim().length > 0;
	const hasProcess = Boolean(message.processSteps?.length);
	const hasArtifacts = message.artifacts && message.artifacts.length > 0;
	const metricSegments = SHOW_ASSISTANT_MESSAGE_METRICS
		? getAssistantMessageFooterSegments(message)
		: [];

	return (
		<div data-slot="ai-message" className="group flex items-start gap-3">
			<AssistantChatAvatar />
			<div className="min-w-0 flex-1">
				<div className="mb-1.5 flex items-center gap-2">
					<span className="text-[13px] font-medium text-slate-500">Lework</span>
					<span className="text-[13px] text-slate-400">{formatTime(message.timestamp)}</span>
					{isStreaming && <span className="animate-pulse text-[13px] text-blue-500">生成中</span>}
				</div>

				{hasProcess && message.processSteps && (
					<div className="mb-3">
						<ProcessTimelineBlock
							steps={message.processSteps}
							toolCalls={message.toolCalls ?? []}
							isStreaming={isStreaming}
						/>
					</div>
				)}

				{hasContent && (
					<div className="mb-3 max-w-[92%] text-sm leading-7 text-slate-800">
						<MarkdownRenderer
							content={content}
							className="prose prose-slate prose-sm max-w-none prose-p:my-1.5 prose-pre:my-2 prose-ul:my-1.5 prose-ol:my-1.5"
						/>
						{isStreaming && (
							<span className="ml-0.5 inline-block h-4 w-1.5 animate-pulse rounded-sm bg-slate-400" />
						)}
					</div>
				)}

				{hasArtifacts && message.artifacts && (
					<div className="mb-3">
						<MessageArtifactList
							artifacts={message.artifacts}
							fallbackTimestamp={message.timestamp}
							projectId={projectId}
						/>
					</div>
				)}

				{!hasContent && !hasProcess && !hasArtifacts && isStreaming && (
					<div className="flex items-center gap-1">
						<span className="size-1.5 animate-pulse rounded-full bg-slate-400" />
						<span className="size-1.5 animate-pulse rounded-full bg-slate-400 [animation-delay:200ms]" />
						<span className="size-1.5 animate-pulse rounded-full bg-slate-400 [animation-delay:400ms]" />
					</div>
				)}

				{!isStreaming && (
					<div className="mt-2 flex items-center gap-3">
						{metricSegments.length > 0 && (
							<div className="text-[13px] text-slate-400">{metricSegments.join(" · ")}</div>
						)}
						<div className="flex items-center gap-0.5 opacity-0 transition-opacity group-hover:opacity-100">
							<CopyButton text={content} />
							{SHOW_ASSISTANT_MESSAGE_REGENERATE_BUTTON && (
								<Button
									variant="ghost"
									size="icon-xs"
									className={`${compactActionButtonClassName} text-slate-400 hover:text-slate-600`}
									onClick={() => resendMessage(message.id)}
								>
									<RefreshCw className="size-3.5" />
								</Button>
							)}
						</div>
					</div>
				)}
			</div>
		</div>
	);
}

function ProcessTimelineBlock({
	steps,
	toolCalls,
	isStreaming,
}: {
	steps: MessageProcessStep[];
	toolCalls: ToolCall[];
	isStreaming: boolean;
}) {
	// 中文注释：执行过程默认收起，后续只尊重用户手动展开/收起，不再被流式状态强制覆盖。
	const [expanded, setExpanded] = useState(false);
	const [autoFollow, setAutoFollow] = useState(true);
	const [showBottomFade, setShowBottomFade] = useState(false);
	const scrollContainerRef = useRef<HTMLDivElement>(null);
	const toolCallMap = useMemo(
		() => new Map(toolCalls.map((toolCall) => [toolCall.id, toolCall] as const)),
		[toolCalls],
	);
	const preview = useMemo(() => {
		for (const step of steps) {
			if (step.type === "thinking") {
				const compact = compactText(step.content);
				if (compact) return compact;
				continue;
			}
			const toolCall = toolCallMap.get(step.toolCallId);
			if (!toolCall?.name?.trim()) continue;
			const args = toolCall.arguments ?? {};
			if (typeof args.intent === "string" && args.intent.trim())
				return args.intent.trim();
			if (typeof args.description === "string" && args.description.trim())
				return args.description.trim();
			if (toolCall.name === "write") return "写入文件";
			if (toolCall.name === "read") return "读取文件";
			if (toolCall.name === "websearch") {
				const query = typeof args.query === "string" && args.query.trim() ? args.query.trim() : "";
				return query ? `搜索 ${query}` : "搜索";
			}
			if (toolCall.name === "web_fetch" || toolCall.name === "webfetch") {
				const url = typeof args.url === "string" && args.url.trim() ? decodeURIComponent(args.url.trim()) : "";
				return url ? "浏览 " + url : "浏览";
			}
			return `调用：${toolCall.name}`;
		}
		return "";
	}, [steps, toolCallMap]);

	useEffect(() => {
		const container = scrollContainerRef.current;
		if (!expanded || !container) {
			setShowBottomFade(false);
			return;
		}

		// 仅在内容确实超出可视高度且底部还有内容未展示时，显示轻量渐隐蒙层。
		const distanceToBottom = container.scrollHeight - container.scrollTop - container.clientHeight;
		const hasOverflow = container.scrollHeight > container.clientHeight + 1;
		setShowBottomFade(hasOverflow && distanceToBottom > 8);

		if (!autoFollow) return;

		// 默认跟随最新步骤，只有用户主动上滑离开底部时才暂停自动滚动。
		container.scrollTop = container.scrollHeight;
	}, [autoFollow, expanded, steps, toolCalls]);

	if (!steps.length) return null;

	return (
		<div
			data-slot="process-timeline-block"
			className="max-w-[92%] overflow-hidden rounded-lg border border-slate-200/80 bg-white/70 text-slate-500 shadow-xs"
		>
			<button
				type="button"
				onClick={() => {
					setExpanded((value) => {
						const nextExpanded = !value;
						if (nextExpanded) {
							setAutoFollow(true);
						}
						return nextExpanded;
					});
				}}
				className="flex w-full items-center justify-between gap-3 px-3 py-2 text-left text-sm transition-colors hover:bg-slate-50/90"
			>
				<div className="flex min-w-0 items-center gap-2">
					{expanded ? (
						<ChevronDown className="size-3.5 shrink-0 text-slate-400" />
					) : (
						<ChevronRight className="size-3.5 shrink-0 text-slate-400" />
					)}
					<Rows3 className="size-3.5 shrink-0 text-indigo-500" />
					<span className="truncate font-medium text-slate-600">执行过程</span>
					{isStreaming && (
						<span className="relative flex size-2 shrink-0">
							<span className="absolute inline-flex size-full animate-ping rounded-full bg-blue-400 opacity-75" />
							<span className="relative inline-flex size-2 rounded-full bg-blue-500" />
						</span>
					)}
				</div>
				{!expanded && preview && (
					<span className="max-w-[54%] truncate text-[13px] text-slate-500">{preview}</span>
				)}
			</button>
			{expanded && (
				<div className="border-t border-slate-200 px-5 py-3">
					<div className="relative">
						<div
							ref={scrollContainerRef}
							onScroll={(event) => {
								const container = event.currentTarget;
								const distanceToBottom =
									container.scrollHeight - container.scrollTop - container.clientHeight;
								const hasOverflow = container.scrollHeight > container.clientHeight + 1;

								setAutoFollow(distanceToBottom <= 24);
								setShowBottomFade(hasOverflow && distanceToBottom > 8);
							}}
							className="no-scrollbar max-h-[min(45vh,25rem)] space-y-3 overflow-y-auto pr-1"
						>
							{steps.map((step) => (
								<ProcessStepItem
									key={step.id}
									step={step}
									toolCall={
										step.type === "tool_call" ? toolCallMap.get(step.toolCallId) : undefined
									}
								/>
							))}
						</div>
						{showBottomFade && (
							<div className="pointer-events-none absolute inset-x-0 bottom-0 h-20 bg-gradient-to-t from-white via-white/30 to-white/0" />
						)}
					</div>
				</div>
			)}
		</div>
	);
}

function ProcessStepItem({ step, toolCall }: { step: MessageProcessStep; toolCall?: ToolCall }) {
	return (
		<div className="flex gap-3">
			{/* <div className="flex w-4 shrink-0 justify-center">
        <div className="w-px bg-slate-200" />
      </div> */}
			<div className="min-w-0 flex-1 pb-1">
				{step.type === "thinking" ? (
					<div className="min-w-0">
						<div className="mb-1 flex items-center gap-2 text-[13px] font-medium text-blue-600">
							<Brain className="size-3.5 text-blue-500" />
							<span>思考过程</span>
						</div>
						<MarkdownRenderer
							content={step.content}
							className="max-w-none text-[13px] leading-6 text-[color:var(--leros-chat-text-muted)] [&_*]:text-[color:var(--leros-chat-text-muted)] [&_ol]:my-1 [&_p]:my-1 [&_pre]:my-1.5 [&_strong]:text-[color:var(--leros-chat-text-muted)] [&_ul]:my-1"
						/>
					</div>
				) : toolCall ? (
					<div>
						<div className="mb-1 flex items-center gap-2 text-[13px] font-medium text-blue-600">
							<Wrench className="size-3.5 text-emerald-500" />
							<span>工具调用</span>
						</div>
						<ToolCallBlock toolCalls={[toolCall]} variant="timeline" />
					</div>
				) : null}
			</div>
		</div>
	);
}

function compactText(value: string): string {
	return value.replace(/\s+/g, " ").trim();
}

function MessageArtifactList({
	artifacts,
	fallbackTimestamp,
	projectId,
}: {
	artifacts: MessageArtifact[];
	fallbackTimestamp: number;
	projectId?: string;
}) {
	const [previewArtifact, setPreviewArtifact] = useState<ProjectArtifact | null>(null);
	const [taskArtifacts, setTaskArtifacts] = useState<ProjectArtifact[]>([]);
	const [loadingArtifactId, setLoadingArtifactId] = useState<string | null>(null);
	const activeTaskDetailTaskId = useLayoutStore((s) => s.activeTaskDetailTaskId);
	const artifactKey = useMemo(
		() => artifacts.map((artifact) => artifact.id).join("|"),
		[artifacts],
	);
	const visibleArtifacts = useMemo(() => {
		// 中文注释：旧历史消息里如果没有独立的文件时间，这里回退到所属消息时间，保证卡片稳定展示时间。
		const sessionArtifacts = artifacts.map((artifact) => ({
			...messageArtifactToProjectArtifact(artifact),
			updatedAt: artifact.updatedAt ?? fallbackTimestamp,
		}));
		const artifactIds = new Set(sessionArtifacts.map((artifact) => artifact.id));
		const enrichedTaskArtifacts = taskArtifacts.filter((artifact) => artifactIds.has(artifact.id));
		return mergeProjectArtifacts(enrichedTaskArtifacts, sessionArtifacts);
	}, [artifacts, fallbackTimestamp, taskArtifacts]);

	useEffect(() => {
		if (!activeTaskDetailTaskId) {
			setTaskArtifacts([]);
			return;
		}
		const taskId = activeTaskDetailTaskId;

		let cancelled = false;
		async function fetchTaskArtifacts() {
			setLoadingArtifactId("__list__");
			try {
				const res = await artifactApi.listTaskArtifacts(taskId);
				if (cancelled) return;
				setTaskArtifacts((res.data.data ?? []).map(mapBackendArtifactToProjectArtifact));
			} catch (err) {
				if (cancelled) return;
				console.error("MessageArtifactList fetch task artifacts error:", err);
				setTaskArtifacts([]);
			} finally {
				if (!cancelled) setLoadingArtifactId(null);
			}
		}
		fetchTaskArtifacts();
		return () => {
			cancelled = true;
		};
	}, [activeTaskDetailTaskId, artifactKey]);

	if (visibleArtifacts.length === 0) return null;

	return (
		<>
			<div className="grid max-w-[92%] gap-2 sm:grid-cols-2">
				{visibleArtifacts.map((artifact) => (
					<button
						type="button"
						key={artifact.id}
						onClick={() =>
							setPreviewArtifact({
								...artifact,
								id: projectId ? `artifacts/${artifact.name}` : artifact.id,
							})
						}
					disabled={loadingArtifactId === artifact.id}
					className="group/artifact relative flex min-w-0 items-center gap-3 overflow-hidden rounded-xl border border-slate-200/70 bg-white/90 px-3.5 py-3 text-left shadow-sm transition-colors hover:border-blue-200 hover:bg-blue-50/60"
					title="预览文件"
					>
						<div className="pointer-events-none absolute inset-0 z-10 flex items-center justify-center bg-[rgba(15,23,42,0.16)] opacity-0 transition-opacity duration-200 group-hover/artifact:opacity-100">
							<span className="rounded-full bg-[rgba(15,23,42,0.72)] px-3 py-1 text-xs font-medium tracking-[0.02em] text-white shadow-sm">
								点击预览
							</span>
						</div>
						<div className="flex size-9 shrink-0 items-center justify-center rounded-lg bg-blue-50 text-slate-600">
							{loadingArtifactId === artifact.id ? (
								<LoaderCircle className="size-4 animate-spin" />
							) : (
								<MessageArtifactIcon fileName={artifact.name} />
							)}
						</div>
						<div className="min-w-0">
							<div className="truncate text-sm font-semibold leading-5 text-slate-700">
								{artifact.title || artifact.name}
							</div>
							<div className="mt-0.5 truncate text-[13px] leading-4 text-slate-400">
								{artifact.name}
								{artifact.updatedAt ? ` · ${formatArtifactTime(artifact.updatedAt)}` : ""}
								{artifact.size ? ` · ${artifact.size}` : ""}
							</div>
						</div>
					</button>
				))}
			</div>
			<ArtifactPreviewDialog
				artifact={previewArtifact}
				open={previewArtifact !== null}
				onOpenChange={(open) => {
					if (!open) setPreviewArtifact(null);
				}}
				projectId={projectId}
			/>
		</>
	);
}

function MessageArtifactIcon({ fileName }: { fileName: string }) {
	return <ProjectFileTypeIcon fileName={fileName} className="size-4 object-contain" />;
}
