"use client";

import {
	fetchFilePreviewByPublicId,
	formatArtifactTime,
	formatTime,
	formatTokenCount,
	messageArtifactToProjectArtifact,
	sortProjectArtifactsByNewestFirst,
	useAppStore,
	useChatStore,
	useLayoutStore,
} from "@leros/store";
import type {
	Message,
	MessageArtifact,
	MessageProcessStep,
	ToolCall,
} from "@leros/store/types/chat";
import { Button } from "@leros/ui/components/ui/button";
import { Tooltip, TooltipContent, TooltipTrigger } from "@leros/ui/components/ui/tooltip";
import { cn } from "@leros/ui/lib/utils";
import {
	Check,
	ChevronDown,
	ChevronRight,
	Copy,
	CornerRightUp,
	RefreshCw,
	Rows3,
	Wrench,
	Zap,
} from "lucide-react";
import { type ReactNode, useEffect, useMemo, useRef, useState } from "react";
import { CUSTOM_ASSISTANT_DEFAULT_AVATAR_SRC } from "../../assets";
import {
	SHOW_ASSISTANT_MESSAGE_METRICS,
	SHOW_ASSISTANT_MESSAGE_REGENERATE_BUTTON,
} from "../../constants/temporaryUiFlags";
import { DiceBearAvatar } from "../avatar/DiceBearAvatar";
import { ProtectedImage } from "../avatar/ProtectedImage";
import { MarkdownRenderer } from "../common/MarkdownRenderer";
import { openPlanPreview, openProjectArtifactPreview } from "../layout/file-preview-store";
import { ProjectFileTypeIcon } from "../layout/project-file-type-icon";
import { AssistantChatAvatar } from "./AssistantChatAvatar";
import { resolveAssistantMessageDisplay } from "./resolveAssistantMessageDisplay";
import { ThinkingProcessIcon } from "./ThinkingProcessIcon";

const PROCESS_TIMELINE_TEXT_CLASS = "text-[#63748B]";
const PROCESS_TIMELINE_CHEVRON_CLASS = "text-slate-400";

import {
	formatProcessToolCallsLabel,
	ProcessToolCallItems,
	ToolCallStatusSummary,
} from "./ToolCallBlock";

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

/** 消息操作栏旁的轻量 token 指示器：数字为主，悬停看输入/输出拆分。 */
function MessageTokenUsage({ message }: { message: Message }) {
	const totalTokens = message.usage?.totalTokens ?? message.metadata?.tokens;
	if (totalTokens === undefined || totalTokens <= 0) return null;

	const inputTokens = message.usage?.inputTokens;
	const outputTokens = message.usage?.outputTokens;
	const hasBreakdown =
		(inputTokens !== undefined && inputTokens > 0) ||
		(outputTokens !== undefined && outputTokens > 0);
	const totalLabel = formatTokenCount(totalTokens);
	// 中文注释：hover 与复制按钮一致——ghost 的 muted 底 + slate-400→slate-600。
	const badgeClassName =
		"inline-flex h-[26px] items-center gap-1 rounded-[min(var(--radius-md),10px)] px-1.5 text-[11px] tabular-nums text-slate-400 transition-all hover:bg-muted hover:text-slate-600";

	const badgeContent = (
		<>
			<Zap className="size-3.5 shrink-0" aria-hidden="true" />
			<span>{totalLabel}</span>
		</>
	);

	if (!hasBreakdown) {
		return (
			<span className={badgeClassName} title={totalLabel}>
				{badgeContent}
			</span>
		);
	}

	return (
		<Tooltip>
			<TooltipTrigger className={badgeClassName} aria-label={totalLabel}>
				{badgeContent}
			</TooltipTrigger>
			<TooltipContent side="top">
				输入 {formatTokenCount(inputTokens ?? 0)} · 输出 {formatTokenCount(outputTokens ?? 0)}
			</TooltipContent>
		</Tooltip>
	);
}

function resolveReplyPreviewContent(message: Message): string | null {
	const content = message.replyTo?.content?.trim();
	return content || null;
}

function ChatAssistantAvatar({ name, src }: { name: string; src?: string }) {
	const className = "size-8 shrink-0 rounded-full object-cover";
	// 中文注释：未上传头像时用固定默认图；有头像但加载失败时回退 DiceBear。
	const emptyFallback = (
		<img src={CUSTOM_ASSISTANT_DEFAULT_AVATAR_SRC} alt={name} className={className} />
	);
	const loadErrorFallback = (
		<DiceBearAvatar seed={`digital-assistant:${name}`} alt={name} className={className} size={64} />
	);

	if (!src) return emptyFallback;

	// 中文注释：聊天头像只展示图片本身，不复用卡片头像的渐变底和装饰样式。
	return <ProtectedImage src={src} alt={name} className={className} fallback={loadErrorFallback} />;
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
	const { resendMessage, messagesMap } = useChatStore((s) => s);
	const assistants = useAppStore((s) => s.assistants);
	const projectMembers = useLayoutStore((s) =>
		projectId ? s.projects.find((project) => project.id === projectId)?.members : undefined,
	);
	const assistantDisplay = useMemo(
		() =>
			resolveAssistantMessageDisplay({
				message,
				messagesMap,
				assistants,
				projectMembers,
			}),
		[assistants, message, messagesMap, projectMembers],
	);
	const content = message.content;
	const hasContent = content.trim().length > 0;
	const hasProcess = Boolean(message.processSteps?.length);
	const hasArtifacts = message.artifacts && message.artifacts.length > 0;
	const assistantName = assistantDisplay.name;
	const assistantRoleName = assistantDisplay.roleName;
	const replyAuthorName = message.replyTo?.authorName?.trim() || "用户";
	const replyPreviewContent = resolveReplyPreviewContent(message);
	const statusLabel = message.status === "waiting" ? "等待中" : "正在思考";
	const statusText = message.statusText?.trim();

	const copyPlanContent = async (fileID: string) => {
		const response = await fetchFilePreviewByPublicId(fileID);
		const fullContent = await response.text();
		await navigator.clipboard.writeText(fullContent);
	};

	return (
		<div data-slot="ai-message" className="flex items-start gap-3">
			{assistantDisplay.useDefaultBrand ? (
				<AssistantChatAvatar />
			) : (
				<ChatAssistantAvatar name={assistantDisplay.name} src={assistantDisplay.avatarUrl} />
			)}
			<div className="min-w-0 flex-1">
				<div className="mb-1.5 flex items-center gap-2">
					<span className="text-[13px] font-medium text-slate-500">{assistantName}</span>
					{assistantRoleName ? (
						<span className="text-[12px] text-slate-400">{assistantRoleName}</span>
					) : null}
					<span className="text-[13px] text-slate-400">{formatTime(message.timestamp)}</span>
					{isStreaming && (
						<span className="animate-pulse text-[13px] text-blue-500">{statusLabel}</span>
					)}
				</div>

				{replyPreviewContent && (
					<div
						className="mb-2 w-fit max-w-[78%] min-w-0 rounded-lg bg-[#f3f3f4] px-4 py-2"
						title={`${replyAuthorName}：${replyPreviewContent}`}
					>
						<div className="flex min-w-0 items-center border-l-[3px] border-l-neutral-200 pl-4">
							<div className="min-w-0 truncate text-[13px] leading-normal text-neutral-400">
								<span className="inline">
									回复
									<CornerRightUp
										className="mx-0.5 inline size-3.5 -translate-y-px align-text-bottom"
										aria-hidden="true"
									/>
								</span>
								<span>{replyAuthorName}</span>
								<span>：</span>
								<span>{replyPreviewContent}</span>
							</div>
						</div>
					</div>
				)}

				{statusText && (
					<div className="mb-3 flex max-w-[92%] items-center gap-2 rounded-lg border border-blue-100 bg-blue-50/70 px-3 py-2 text-sm leading-6 text-slate-600">
						<span className="size-1.5 animate-pulse rounded-full bg-blue-500" />
						<span>{statusText}</span>
					</div>
				)}

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
							className="prose prose-slate prose-sm max-w-none prose-p:my-1.5 prose-pre:my-2 prose-ul:my-1.5 prose-ol:my-1.5 prose-pre:overflow-x-auto prose-pre:rounded-lg prose-pre:border prose-pre:border-slate-200 prose-pre:bg-slate-50 prose-pre:p-3 prose-pre:text-slate-800 prose-pre:shadow-none [&_pre_code]:bg-transparent [&_pre_code]:p-0 [&_pre_code]:text-[13px] [&_pre_code]:leading-6 [&_pre_code]:text-slate-800 [&_:not(pre)>code]:rounded [&_:not(pre)>code]:bg-slate-100 [&_:not(pre)>code]:px-1.5 [&_:not(pre)>code]:py-0.5 [&_:not(pre)>code]:font-medium [&_:not(pre)>code]:text-slate-800"
							onPlanOpen={openPlanPreview}
							onPlanCopy={copyPlanContent}
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

				{!statusText && !hasContent && !hasProcess && !hasArtifacts && isStreaming && (
					<div className="flex items-center gap-1">
						<span className="size-1.5 animate-pulse rounded-full bg-slate-400" />
						<span className="size-1.5 animate-pulse rounded-full bg-slate-400 [animation-delay:200ms]" />
						<span className="size-1.5 animate-pulse rounded-full bg-slate-400 [animation-delay:400ms]" />
					</div>
				)}

				{!isStreaming && (
					<div className="mt-1.5 flex items-center gap-1">
						<div className="flex items-center gap-0.5">
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
						{SHOW_ASSISTANT_MESSAGE_METRICS ? <MessageTokenUsage message={message} /> : null}
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
		return getLatestProcessPreview(steps, toolCallMap);
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
				className="flex w-full cursor-pointer items-center justify-between gap-3 px-3 py-2 text-left text-sm font-normal transition-colors hover:bg-slate-50/90"
			>
				<div className="flex min-w-0 items-center gap-1">
					{expanded ? (
						<ChevronDown className="size-3.5 shrink-0 text-slate-400" />
					) : (
						<ChevronRight className="size-3.5 shrink-0 text-slate-400" />
					)}
					<Rows3 className={cn("size-3.5 shrink-0", PROCESS_TIMELINE_TEXT_CLASS)} />
					<span className={cn("truncate font-medium", PROCESS_TIMELINE_TEXT_CLASS)}>执行过程</span>
					{isStreaming && (
						<span className="relative flex size-2 shrink-0">
							<span className="absolute inline-flex size-full animate-ping rounded-full bg-blue-400 opacity-75" />
							<span className="relative inline-flex size-2 rounded-full bg-blue-500" />
						</span>
					)}
				</div>
				{isStreaming && preview ? (
					<span
						className={cn(
							"max-w-[54%] min-w-0 shrink truncate text-[13px]",
							PROCESS_TIMELINE_TEXT_CLASS,
						)}
					>
						<ProcessStepPreviewText preview={preview} />
					</span>
				) : !isStreaming ? (
					<span
						className={cn(
							"max-w-[54%] min-w-0 shrink truncate text-[13px]",
							PROCESS_TIMELINE_TEXT_CLASS,
						)}
					>
						已完成
					</span>
				) : null}
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
							{groupProcessSteps(steps).map((item) => {
								if (item.kind === "thinking") {
									return <ThinkingStepItem key={item.id} content={item.content} />;
								}

								const groupedToolCalls = item.toolCallIds
									.map((toolCallId) => toolCallMap.get(toolCallId))
									.filter((toolCall): toolCall is ToolCall => Boolean(toolCall));

								if (!groupedToolCalls.length) return null;

								return <ToolCallsGroupItem key={item.id} toolCalls={groupedToolCalls} />;
							})}
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

type ProcessTimelineItem =
	| {
			kind: "thinking";
			id: string;
			content: string;
	  }
	| {
			kind: "tool_calls";
			id: string;
			toolCallIds: string[];
	  };

function groupProcessSteps(steps: MessageProcessStep[]): ProcessTimelineItem[] {
	const items: ProcessTimelineItem[] = [];
	let currentToolCallIds: string[] = [];

	const flushToolCalls = () => {
		if (currentToolCallIds.length === 0) return;
		items.push({
			kind: "tool_calls",
			id: `tool-calls-${currentToolCallIds[0]}`,
			toolCallIds: currentToolCallIds,
		});
		currentToolCallIds = [];
	};

	for (const step of steps) {
		if (step.type === "thinking") {
			flushToolCalls();
			items.push({ kind: "thinking", id: step.id, content: step.content });
			continue;
		}

		currentToolCallIds.push(step.toolCallId);
	}

	flushToolCalls();
	return items;
}

function ToolCallsGroupItem({ toolCalls }: { toolCalls: ToolCall[] }) {
	const [expanded, setExpanded] = useState(false);
	const successCount = toolCalls.filter((toolCall) => toolCall.status === "success").length;
	const errorCount = toolCalls.filter((toolCall) => toolCall.status === "error").length;
	const runningCount = toolCalls.filter((toolCall) => toolCall.status === "running").length;

	return (
		<div className="min-w-0 pb-.5">
			<button
				type="button"
				onClick={() => setExpanded((value) => !value)}
				className={cn(
					"flex h-5 w-full min-w-0 cursor-pointer items-center justify-between text-left text-[13px] font-normal",
					PROCESS_TIMELINE_TEXT_CLASS,
					expanded ? "gap-2" : "gap-4",
				)}
			>
				<span
					className={cn(
						"flex min-w-0 flex-1 items-center gap-1 overflow-hidden",
						!expanded && "max-w-[calc(100%-7rem)] pr-2",
					)}
				>
					<ProcessStepIconSlot>
						{expanded ? (
							<ChevronDown className={PROCESS_TIMELINE_CHEVRON_CLASS} />
						) : (
							<ChevronRight className={PROCESS_TIMELINE_CHEVRON_CLASS} />
						)}
					</ProcessStepIconSlot>
					<ProcessStepIconSlot>
						<Wrench className={PROCESS_TIMELINE_TEXT_CLASS} />
					</ProcessStepIconSlot>
					<span
						className="min-w-0 flex-1 truncate font-medium leading-5"
						title={expanded ? undefined : formatProcessToolCallsLabel(toolCalls)}
					>
						{expanded ? "工具调用" : formatProcessToolCallsLabel(toolCalls)}
					</span>
				</span>
				{!expanded && (
					<ToolCallStatusSummary
						successCount={successCount}
						errorCount={errorCount}
						runningCount={runningCount}
					/>
				)}
			</button>
			{expanded && <ProcessToolCallItems toolCalls={toolCalls} />}
		</div>
	);
}

function ThinkingStepItem({ content }: { content: string }) {
	const [expanded, setExpanded] = useState(false);
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
	}, [content, expanded]);

	return (
		<div className="min-w-0 pb-.5">
			<button
				type="button"
				onClick={() => setExpanded((value) => !value)}
				className={cn(
					"flex h-5 w-full cursor-pointer items-center gap-1 text-left text-[13px] font-normal",
					PROCESS_TIMELINE_TEXT_CLASS,
				)}
			>
				<ProcessStepIconSlot>
					{expanded ? (
						<ChevronDown className={PROCESS_TIMELINE_CHEVRON_CLASS} />
					) : (
						<ChevronRight className={PROCESS_TIMELINE_CHEVRON_CLASS} />
					)}
				</ProcessStepIconSlot>
				<ProcessStepIconSlot>
					<ThinkingProcessIcon className={PROCESS_TIMELINE_TEXT_CLASS} />
				</ProcessStepIconSlot>
				<span className="font-medium leading-5">思考过程</span>
			</button>
			{expanded && (
				<div className="relative">
					<div
						ref={scrollContainerRef}
						onScroll={(event) => updateBottomFade(event.currentTarget)}
						className="no-scrollbar max-h-[min(45vh,25rem)] overflow-y-auto pr-1"
					>
						<MarkdownRenderer
							content={content}
							className={cn(
								"max-w-none text-[13px] leading-6",
								PROCESS_TIMELINE_TEXT_CLASS,
								"[&_*]:text-[#63748B] [&_ol]:my-1 [&_p]:my-1 [&_pre]:my-1.5 [&_strong]:text-[#63748B] [&_ul]:my-1",
							)}
						/>
					</div>
					{showBottomFade && (
						<div className="pointer-events-none absolute inset-x-0 bottom-0 h-20 bg-gradient-to-t from-white via-white/30 to-white/0" />
					)}
				</div>
			)}
		</div>
	);
}

function ProcessStepIconSlot({ children }: { children: ReactNode }) {
	return (
		<span className="inline-flex size-3.5 shrink-0 items-center justify-center [&>svg]:size-3.5">
			{children}
		</span>
	);
}

type ProcessStepPreview =
	| {
			kind: "thinking";
			text: string;
	  }
	| {
			kind: "tool";
			loadingLabel: string;
			detail?: string;
	  };

function ProcessStepPreviewText({ preview }: { preview: ProcessStepPreview }) {
	if (preview.kind === "thinking") {
		return <span className="truncate">{preview.text}</span>;
	}

	return (
		<span className="flex min-w-0 items-baseline">
			<span className="leros-process-preview-wave shrink-0">{preview.loadingLabel}</span>
			{preview.detail ? (
				<span className={cn("min-w-0 truncate", PROCESS_TIMELINE_TEXT_CLASS)}>
					{" "}
					{preview.detail}
				</span>
			) : null}
		</span>
	);
}

function compactText(value: string): string {
	return value.replace(/\s+/g, " ").trim();
}

function getLatestProcessPreview(
	steps: MessageProcessStep[],
	toolCallMap: Map<string, ToolCall>,
): ProcessStepPreview | null {
	for (let index = steps.length - 1; index >= 0; index -= 1) {
		const step = steps[index];
		if (!step) continue;

		if (step.type === "thinking") {
			const compact = compactText(step.content);
			if (compact) return { kind: "thinking", text: compact };
			continue;
		}

		const preview = getToolCallProcessPreview(toolCallMap.get(step.toolCallId));
		if (preview) return preview;
	}

	return null;
}

function getToolCallStringArg(args: Record<string, unknown>, key: string): string {
	const value = args[key];
	return typeof value === "string" && value.trim() ? value.trim() : "";
}

function getToolCallProcessPreview(toolCall: ToolCall | undefined): ProcessStepPreview | null {
	if (!toolCall?.name?.trim()) return null;

	const args = toolCall.arguments ?? {};

	if (toolCall.name === "websearch") {
		return {
			kind: "tool",
			loadingLabel: "搜索网页中...",
			detail: getToolCallStringArg(args, "query") || undefined,
		};
	}

	if (toolCall.name === "web_fetch" || toolCall.name === "webfetch") {
		const url = getToolCallStringArg(args, "url");
		return {
			kind: "tool",
			loadingLabel: "浏览网页中...",
			detail: url ? decodeURIComponent(url) : undefined,
		};
	}

	if (toolCall.name === "read") {
		return {
			kind: "tool",
			loadingLabel: "读取文件中...",
			detail: getToolCallStringArg(args, "path") || undefined,
		};
	}

	if (toolCall.name === "write") {
		return {
			kind: "tool",
			loadingLabel: "写入文件中...",
			detail: getToolCallStringArg(args, "path") || undefined,
		};
	}

	return {
		kind: "tool",
		loadingLabel: `调用${toolCall.name}中...`,
	};
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
	const visibleArtifacts = useMemo(() => {
		// 中文注释：消息流里已携带 artifact 元数据，直接展示即可，不再额外请求 ListTaskArtifacts。
		return sortProjectArtifactsByNewestFirst(
			artifacts.map((artifact) => ({
				...messageArtifactToProjectArtifact(artifact),
				id: projectId ? `artifacts/${artifact.name}` : artifact.id,
				updatedAt: artifact.updatedAt ?? fallbackTimestamp,
			})),
		);
	}, [artifacts, fallbackTimestamp, projectId]);

	if (visibleArtifacts.length === 0) return null;

	return (
		<div className="grid max-w-[92%] gap-2 sm:grid-cols-2">
			{visibleArtifacts.map((artifact) => (
				<button
					type="button"
					key={artifact.id}
					data-file-preview-trigger
					onClick={() => openProjectArtifactPreview(artifact, projectId)}
					className="group/artifact relative flex min-w-0 items-center gap-3 overflow-hidden rounded-xl border border-slate-200/70 bg-white/90 px-3.5 py-3 text-left shadow-sm transition-colors hover:border-blue-200 hover:bg-blue-50/60"
					title={artifact.versionNo ? `预览 V${artifact.versionNo}` : "预览文件"}
				>
					<div className="pointer-events-none absolute inset-0 z-10 flex items-center justify-center bg-[rgba(15,23,42,0.16)] opacity-0 transition-opacity duration-200 group-hover/artifact:opacity-100">
						<span className="rounded-full bg-[rgba(15,23,42,0.72)] px-3 py-1 text-xs font-medium tracking-[0.02em] text-white shadow-sm">
							点击预览
						</span>
					</div>
					<div className="flex size-9 shrink-0 items-center justify-center rounded-lg bg-blue-50 text-slate-600">
						<MessageArtifactIcon fileName={artifact.name} />
					</div>
					<div className="min-w-0">
						<div className="min-w-0 truncate text-sm font-semibold leading-5 text-slate-700">
							<span className="truncate">{artifact.name}</span>
						</div>
						<div className="mt-0.5 flex min-w-0 items-center gap-1.5 text-[13px] leading-4 text-slate-400">
							{artifact.versionNo ? (
								<span className="shrink-0 rounded bg-blue-50 px-1.5 py-0.5 text-[10px] font-semibold leading-none text-blue-600">
									V{artifact.versionNo}
								</span>
							) : null}
							<span className="min-w-0 truncate">
								{[artifact.size, artifact.updatedAt ? formatArtifactTime(artifact.updatedAt) : ""]
									.filter(Boolean)
									.join(" · ")}
							</span>
						</div>
					</div>
				</button>
			))}
		</div>
	);
}

function MessageArtifactIcon({ fileName }: { fileName: string }) {
	return <ProjectFileTypeIcon fileName={fileName} className="size-4 object-contain" />;
}
