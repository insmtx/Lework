"use client";

import type { ProjectArtifact, ProjectTask } from "@leros/store";
import { formatTokenCount, projectFileApi, useChatStore, useLayoutStore } from "@leros/store";
import { artifactApi } from "@leros/store/api/artifactApi";
import { taskApi } from "@leros/store/api/taskApi";
import type { ApiError } from "@leros/ui/lib/request";
import { cn } from "@leros/ui/lib/utils";
import {
	ArrowDownToLine,
	ArrowLeft,
	ArrowUpFromLine,
	Bot,
	CheckCircle2,
	ChevronRight,
	ChevronsLeft,
	ChevronsRight,
	Zap,
} from "lucide-react";
import { useCallback, useEffect, useMemo, useRef, useState } from "react";
import { SHOW_TASK_TOKEN_USAGE_CARD } from "../../constants/temporaryUiFlags";
import { MessageTimeline } from "../chat/MessageTimeline";
import { ChatInput } from "../input/ChatInput";
import { ArtifactPreviewDialog } from "./ArtifactPreviewDialog";
import type { AppNavigation } from "./LeftRail";
import { getProjectChatLayoutClasses, type ProjectChatLayoutMode } from "./project-chat-layout";
import { ProjectFileTypeIcon, SIDEBAR_COMPACT_LIST_CLASS } from "./project-file-type-icon";
import {
	collectSelectableFiles,
	normalizeProjectFileTree,
	type ProjectFileNode,
	sortProjectFilesByUploadedTimeDesc,
} from "./project-files";
import { TaskTodoProgressPanel } from "./TaskTodoProgressPanel";
import { getLatestAssistantTodos } from "./taskProgress";

const TASK_DETAIL_RIGHT_SIDEBAR_WIDTH_STORAGE_KEY = "leros-task-detail-right-sidebar-width";
const TASK_DETAIL_RIGHT_SIDEBAR_DEFAULT_WIDTH = 352;
const TASK_DETAIL_RIGHT_SIDEBAR_MIN_WIDTH = 300;
const TASK_DETAIL_RIGHT_SIDEBAR_MAX_WIDTH = 440;

function isDirectoryNotFoundError(error: unknown): boolean {
	if (typeof error !== "object" || error === null || !("status" in error)) {
		return false;
	}
	const apiError = error as ApiError;
	return apiError.status === 404 && apiError.message === "directory not found";
}

function truncateBreadcrumbText(text?: string | null, maxLength = 10) {
	if (!text) {
		return "";
	}
	return text.length > maxLength ? `${text.slice(0, maxLength)}...` : text;
}

export function TaskDetailPage({
	projectId,
	taskId,
	sessionId,
	navigation,
}: {
	projectId?: string;
	taskId?: string;
	sessionId?: string | null;
	navigation?: AppNavigation;
}) {
	const {
		activeTaskDetailProjectId,
		activeTaskDetailTaskId,
		activeTaskDetailSessionId,
		projects,
		fetchProjects,
		setTaskDetailRoute,
		switchProject,
	} = useLayoutStore((s) => s);

	const {
		activeSessionId,
		isGenerating,
		messageIds,
		messagesMap,
		pendingBootstrapSessionId,
		streamingMessageId,
		setActiveSession,
		loadConversationMessages,
	} = useChatStore((s) => s);

	const [task, setTask] = useState<ProjectTask | null>(null);
	const [taskFiles, setTaskFiles] = useState<ProjectFileNode[]>([]);
	const [previewArtifact, setPreviewArtifact] = useState<ProjectArtifact | null>(null);
	const [rightSidebarWidth, setRightSidebarWidth] = useState(
		TASK_DETAIL_RIGHT_SIDEBAR_DEFAULT_WIDTH,
	);
	const [rightSidebarCollapsed, setRightSidebarCollapsed] = useState(false);
	const hasLoadedRightSidebarPreferenceRef = useRef(false);

	const resolvedProjectId = projectId ?? activeTaskDetailProjectId;
	const resolvedTaskId = taskId ?? activeTaskDetailTaskId;
	const resolvedSessionId = sessionId ?? activeTaskDetailSessionId;
	const project = projects.find((p) => p.id === resolvedProjectId);
	// 面包屑只做展示截断，完整名称通过 title 保留，避免超长文本撑开头部布局。
	const breadcrumbProjectName = truncateBreadcrumbText(project?.name);
	const breadcrumbTaskTitle = truncateBreadcrumbText(task?.title ?? "任务");

	const latestTodos = useMemo(
		() => getLatestAssistantTodos(messagesMap, messageIds, resolvedSessionId, streamingMessageId),
		[messagesMap, messageIds, resolvedSessionId, streamingMessageId],
	);

	const tokenSummary = useMemo(() => {
		const emptySummary = {
			inputTokens: 0,
			outputTokens: 0,
			totalTokens: 0,
			messageCount: 0,
		};
		if (!SHOW_TASK_TOKEN_USAGE_CARD) {
			return emptySummary;
		}
		// 任务详情右侧成本卡统一按当前会话内 assistant 消息聚合，刷新后可直接从历史消息恢复。
		const initialSummary = emptySummary;

		return messageIds.reduce((summary, id) => {
			const message = messagesMap[id];
			if (
				!message ||
				message.conversationId !== resolvedSessionId ||
				message.role !== "assistant"
			) {
				return summary;
			}

			const inputTokens = message.usage?.inputTokens ?? 0;
			const outputTokens = message.usage?.outputTokens ?? 0;
			const totalTokens = message.usage?.totalTokens ?? message.metadata?.tokens ?? 0;
			return {
				inputTokens: summary.inputTokens + inputTokens,
				outputTokens: summary.outputTokens + outputTokens,
				totalTokens: summary.totalTokens + totalTokens,
				messageCount: summary.messageCount + (totalTokens > 0 ? 1 : 0),
			};
		}, initialSummary);
	}, [resolvedSessionId, messageIds, messagesMap]);

	const flatTaskFiles = useMemo(
		() => sortProjectFilesByUploadedTimeDesc(collectSelectableFiles(taskFiles)),
		[taskFiles],
	);
	const rightSidebarWidthStyle = !rightSidebarCollapsed
		? { width: `${rightSidebarWidth}px` }
		: undefined;
	const taskChatLayoutMode: ProjectChatLayoutMode = rightSidebarCollapsed
		? "sidebar-collapsed"
		: "sidebar-expanded";
	const taskChatLayout = getProjectChatLayoutClasses(taskChatLayoutMode);

	const fetchTaskFiles = useCallback(async () => {
		if (!resolvedProjectId || !resolvedTaskId) return;
		try {
			// 中文注释：先确认当前任务是否真的有产物；没有的话直接按空列表处理，避免对旧任务额外打 artifacts 目录 404。
			const artifactResponse = await artifactApi.listTaskArtifacts(resolvedTaskId);
			if ((artifactResponse.data.data ?? []).length === 0) {
				setTaskFiles([]);
				return;
			}
			const res = await projectFileApi.list({
				projectId: resolvedProjectId,
				path: "artifacts",
			});
			setTaskFiles(normalizeProjectFileTree(res.data.data));
		} catch (err) {
			if (isDirectoryNotFoundError(err)) {
				setTaskFiles([]);
				return;
			}
			console.error("TaskDetailPage fetch task files error:", err);
			setTaskFiles([]);
		}
	}, [resolvedProjectId, resolvedTaskId]);

	useEffect(() => {
		fetchProjects();
	}, [fetchProjects]);

	useEffect(() => {
		if (!projectId || !taskId) return;
		setTaskDetailRoute(projectId, taskId, sessionId ?? null);
	}, [projectId, taskId, sessionId, setTaskDetailRoute]);

	useEffect(() => {
		if (!resolvedSessionId) return;

		setActiveSession(resolvedSessionId);
		// 项目消息刚切页时，先等 store 完成 optimistic 初始化，避免旧历史抢先覆盖 UI。
		if (pendingBootstrapSessionId === resolvedSessionId) return;
		if (activeSessionId === resolvedSessionId && isGenerating) return;
		loadConversationMessages(resolvedSessionId);
	}, [
		resolvedSessionId,
		activeSessionId,
		isGenerating,
		pendingBootstrapSessionId,
		setActiveSession,
		loadConversationMessages,
	]);

	useEffect(() => {
		if (!resolvedTaskId) return;

		taskApi
			.get({ public_id: resolvedTaskId })
			.then((res) => {
				const bt = res.data.data;
				if (bt) {
					setTask({
						id: bt.public_id,
						title: bt.title,
						meta: bt.description ?? bt.task_type ?? "",
						status: (bt.status as ProjectTask["status"]) ?? "todo",
						taskType: bt.task_type,
						deadline: bt.deadline,
						description: bt.description,
					});
				}
			})
			.catch((err) => {
				console.error("TaskDetailPage fetch task error:", err);
			});

		fetchTaskFiles();
	}, [resolvedTaskId, fetchTaskFiles]);

	useEffect(() => {
		if (!resolvedTaskId || isGenerating) return;
		fetchTaskFiles();
	}, [resolvedTaskId, fetchTaskFiles, isGenerating]);

	useEffect(() => {
		if (typeof window === "undefined" || hasLoadedRightSidebarPreferenceRef.current) return;
		hasLoadedRightSidebarPreferenceRef.current = true;

		const savedWidth = window.localStorage.getItem(TASK_DETAIL_RIGHT_SIDEBAR_WIDTH_STORAGE_KEY);
		const parsedWidth = savedWidth ? Number(savedWidth) : NaN;
		if (Number.isFinite(parsedWidth)) {
			setRightSidebarWidth(clampTaskDetailRightSidebarWidth(parsedWidth));
		}
	}, []);

	useEffect(() => {
		if (typeof window === "undefined" || !hasLoadedRightSidebarPreferenceRef.current) return;
		window.localStorage.setItem(
			TASK_DETAIL_RIGHT_SIDEBAR_WIDTH_STORAGE_KEY,
			String(rightSidebarWidth),
		);
	}, [rightSidebarWidth]);

	useEffect(() => {
		// 中文注释：任务详情右侧栏的展开态只属于当前查看上下文，切换任务或会话后应恢复默认展开。
		setRightSidebarCollapsed(false);
	}, [resolvedProjectId, resolvedTaskId, resolvedSessionId]);

	const handleRightSidebarResizeStart = (event: React.PointerEvent<HTMLHRElement>) => {
		if (rightSidebarCollapsed) return;

		const startX = event.clientX;
		const startWidth = rightSidebarWidth;
		const pointerId = event.pointerId;
		const target = event.currentTarget;

		target.setPointerCapture(pointerId);

		const handlePointerMove = (moveEvent: PointerEvent) => {
			// 中文注释：任务页右侧栏挂在主内容右边，向左拖动时应放大宽度。
			setRightSidebarWidth(
				clampTaskDetailRightSidebarWidth(startWidth - (moveEvent.clientX - startX)),
			);
		};

		const handlePointerUp = () => {
			if (target.hasPointerCapture(pointerId)) {
				target.releasePointerCapture(pointerId);
			}
			target.removeEventListener("pointermove", handlePointerMove);
			target.removeEventListener("pointerup", handlePointerUp);
			target.removeEventListener("pointercancel", handlePointerUp);
		};

		target.addEventListener("pointermove", handlePointerMove);
		target.addEventListener("pointerup", handlePointerUp);
		target.addEventListener("pointercancel", handlePointerUp);
	};

	if (!resolvedProjectId || !resolvedTaskId) {
		return (
			<div className="flex h-full flex-1 items-center justify-center bg-[var(--leros-app-bg)] text-[var(--leros-text-muted)]">
				无任务详情
			</div>
		);
	}

	return (
		<div
			data-slot="task-detail-page"
			className="flex h-full min-w-0 flex-1 flex-col bg-[var(--leros-surface)]"
		>
			<header className="flex h-16 shrink-0 items-center justify-between border-b border-[var(--leros-control-border)] bg-[var(--leros-surface-soft)] px-10">
				<div className="flex min-w-0 items-center gap-3 text-[var(--leros-text-muted)]">
					{/* 中文注释：任务详情页面包屑与项目页保持一致，三级结构为 项目 > 项目名 > 任务名。 */}
					<button
						type="button"
						onClick={() => navigation?.goToRoute("projectsHub")}
						className="text-base font-bold text-[var(--leros-text-muted)] transition-colors hover:text-[var(--leros-primary)]"
					>
						项目
					</button>
					<ChevronRight className="size-4 shrink-0 text-[var(--leros-text-subtle)]" />
					{project && (
						<>
							<button
								type="button"
								onClick={() => {
									if (navigation && resolvedProjectId) {
										navigation.goToProject(resolvedProjectId);
										return;
									}
									resolvedProjectId && switchProject(resolvedProjectId);
								}}
								className="max-w-[360px] truncate text-base font-bold text-[var(--leros-text-muted)] transition-colors hover:text-[var(--leros-primary)]"
								title={project.name}
							>
								{breadcrumbProjectName}
							</button>
							<ChevronRight className="size-4 shrink-0 text-[var(--leros-text-subtle)]" />
						</>
					)}
					<h1
						className="max-w-[360px] truncate text-base font-bold text-[var(--leros-text-strong)]"
						title={task?.title}
					>
						{breadcrumbTaskTitle}
					</h1>
				</div>
				<div className="flex items-center gap-3">
					<button
						type="button"
						onClick={() => {
							if (navigation && resolvedProjectId) {
								navigation.goToProject(resolvedProjectId);
								return;
							}
							resolvedProjectId && switchProject(resolvedProjectId);
						}}
						className="flex items-center gap-1.5 rounded-full px-3 py-1.5 text-xs font-semibold text-[var(--leros-text-muted)] transition-colors hover:bg-[var(--leros-primary-softer)] hover:text-[var(--leros-primary)]"
					>
						<ArrowLeft className="size-3.5" />
						返回新建任务
					</button>
				</div>
			</header>

			<div className="min-h-0 min-w-0 flex flex-1">
				<main className="min-w-0 flex min-h-0 flex-1 flex-col">
					{/* 中文注释：任务详情页作为壳层里的 flex item 以及中间主列本身都必须允许收缩，避免小窗口下被聊天内容宽度和右侧栏共同撑出可视区域。 */}
					<MessageTimeline
						emptyState={<TaskChatEmptyState layout={taskChatLayout} />}
						contentShellClassName={taskChatLayout.shell}
						contentClassName={taskChatLayout.timelineInner}
						projectId={resolvedProjectId}
					/>
					<ChatInput variant="project" projectLayoutMode={taskChatLayoutMode} />
				</main>

				{rightSidebarCollapsed && (
					<button
						type="button"
						className="absolute right-6 top-[136px] z-20 inline-flex size-10 items-center justify-center rounded-full border border-[var(--leros-control-border)] bg-[var(--leros-surface)] text-[var(--leros-text-muted)] shadow-sm transition-colors hover:bg-[var(--leros-primary-softer)] hover:text-[var(--leros-primary)]"
						aria-label="展开右侧栏"
						title="展开右侧栏"
						onClick={() => setRightSidebarCollapsed(false)}
					>
						<ChevronsLeft className="size-4" />
					</button>
				)}

				{!rightSidebarCollapsed && (
					<aside
						className="relative flex shrink-0 flex-col border-l border-[var(--leros-control-border)] bg-[var(--leros-surface-soft)] px-5 py-6 transition-[width] duration-200 ease-out"
						style={rightSidebarWidthStyle}
					>
						<div className="no-scrollbar min-h-0 flex-1 space-y-8 overflow-y-auto pr-1">
							<div className="flex items-start justify-between gap-3">
								<div>
									<p className="text-sm font-semibold text-[var(--leros-text-strong)]">任务侧栏</p>
									<p className="mt-1 text-xs text-[var(--leros-text-muted)]">
										查看任务说明、进度和文件概览
									</p>
								</div>
								<button
									type="button"
									className="rounded-full p-1.5 text-[var(--leros-text-muted)] transition-colors hover:bg-[var(--leros-primary-softer)] hover:text-[var(--leros-text-strong)]"
									aria-label="收起右侧栏"
									title="收起右侧栏"
									onClick={() => setRightSidebarCollapsed(true)}
								>
									<ChevronsRight className="size-4" />
								</button>
							</div>
							{SHOW_TASK_TOKEN_USAGE_CARD && (
								<section>
									<TaskTokenUsageCard
										totalTokens={tokenSummary.totalTokens}
										inputTokens={tokenSummary.inputTokens}
										outputTokens={tokenSummary.outputTokens}
										messageCount={tokenSummary.messageCount}
									/>
								</section>
							)}
							{task?.description && (
								<section>
									<h3 className="mb-3 text-xs font-semibold text-[var(--leros-text-muted)]">
										任务描述
									</h3>
									<p className="text-sm leading-relaxed text-[var(--leros-text)]">
										{task.description}
									</p>
								</section>
							)}
							{project && (
								<section>
									<h3 className="mb-3 text-xs font-semibold text-[var(--leros-text-muted)]">
										所属项目
									</h3>
									<div className="rounded-lg border border-[var(--leros-control-border)] bg-[var(--leros-surface)] p-3.5">
										<p className="text-sm font-semibold text-[var(--leros-text-strong)]">
											{project.name}
										</p>
										{project.description && (
											<p className="mt-1 text-xs text-[var(--leros-text-muted)]">
												{project.description}
											</p>
										)}
									</div>
								</section>
							)}
							{latestTodos && latestTodos.length > 0 && (
								<section>
									<h3 className="mb-3 text-xs font-semibold text-[var(--leros-text-muted)]">
										任务进度
									</h3>
									<TaskTodoProgressPanel todos={latestTodos} />
								</section>
							)}
							<section>
								<div className="mb-3 flex items-center justify-between">
									<h3 className="text-xs font-semibold text-[var(--leros-text-muted)]">任务文件</h3>
									<span className="rounded-md bg-[var(--leros-primary-soft)] px-2 py-0.5 text-xs font-semibold text-[var(--leros-primary)]">
										{flatTaskFiles.length} 个
									</span>
								</div>
								<TaskFileList
									files={flatTaskFiles}
									onPreview={(file) =>
										setPreviewArtifact({
											id: file.path,
											name: file.name,
											title: file.name,
											type: "document",
											artifactType: "file",
											mimeType: file.mimeType,
											size: formatBytes(file.size),
											downloadUrl: "",
										})
									}
								/>
							</section>
						</div>
						<hr
							className="absolute left-0 top-0 z-10 h-full w-3 -translate-x-1/2 cursor-col-resize border-0"
							tabIndex={0}
							aria-orientation="vertical"
							aria-label="调整右侧栏宽度"
							aria-valuemin={TASK_DETAIL_RIGHT_SIDEBAR_MIN_WIDTH}
							aria-valuemax={TASK_DETAIL_RIGHT_SIDEBAR_MAX_WIDTH}
							aria-valuenow={rightSidebarWidth}
							onPointerDown={handleRightSidebarResizeStart}
							onKeyDown={(event) => {
								if (event.key === "ArrowLeft") {
									setRightSidebarWidth(clampTaskDetailRightSidebarWidth(rightSidebarWidth + 8));
								}
								if (event.key === "ArrowRight") {
									setRightSidebarWidth(clampTaskDetailRightSidebarWidth(rightSidebarWidth - 8));
								}
							}}
						/>
					</aside>
				)}
			</div>
			<ArtifactPreviewDialog
				artifact={previewArtifact}
				open={previewArtifact !== null}
				onOpenChange={(open) => {
					if (!open) setPreviewArtifact(null);
				}}
				projectId={resolvedProjectId}
			/>
		</div>
	);
}

function clampTaskDetailRightSidebarWidth(width: number) {
	return Math.min(
		TASK_DETAIL_RIGHT_SIDEBAR_MAX_WIDTH,
		Math.max(TASK_DETAIL_RIGHT_SIDEBAR_MIN_WIDTH, width),
	);
}

function TaskTokenUsageCard({
	totalTokens,
	inputTokens,
	outputTokens,
	messageCount,
}: {
	totalTokens: number;
	inputTokens: number;
	outputTokens: number;
	messageCount: number;
}) {
	const totalDisplay = splitTokenMetric(totalTokens);
	const inputDisplay = splitTokenMetric(inputTokens, { compact: true });
	const outputDisplay = splitTokenMetric(outputTokens, { compact: true });

	if (totalTokens <= 0) {
		return (
			<div className="overflow-hidden rounded-2xl border border-slate-200 bg-white shadow-[0_2px_10px_-4px_rgba(15,23,42,0.08)]">
				<div className="flex items-center justify-between border-b border-slate-100 bg-slate-50/70 px-5 py-3.5">
					<div className="flex items-center gap-1.5">
						<Zap className="size-4 text-indigo-500" />
						<span className="text-sm font-semibold text-slate-700">Token 消耗</span>
					</div>
					<span className="rounded-full border border-indigo-100/70 bg-indigo-50 px-2 py-0.5 text-[11px] font-semibold text-indigo-600">
						0
					</span>
				</div>
				<div className="px-5 py-8 text-center text-xs text-slate-400">当前会话暂无消耗数据</div>
			</div>
		);
	}

	return (
		<div className="overflow-hidden rounded-2xl border border-slate-200 bg-white shadow-[0_2px_10px_-4px_rgba(15,23,42,0.08)]">
			<div className="flex items-center justify-between border-b border-slate-100 bg-slate-50/70 px-5 py-3.5">
				<div className="flex items-center gap-1.5">
					<Zap className="size-4 text-indigo-500" />
					<span className="text-sm font-semibold text-slate-700">Token 消耗</span>
				</div>
				<span className="rounded-full border border-indigo-100/70 bg-indigo-50 px-2 py-0.5 text-[11px] font-semibold text-indigo-600">
					{formatTokenCount(totalTokens)}
				</span>
			</div>

			<div className="p-5">
				<div className="mb-6">
					<div className="text-xs font-medium text-slate-500">当前会话累计</div>
					<div className="mt-1 flex items-end gap-0.5">
						<div className="text-4xl font-bold tracking-tight text-slate-900">
							{totalDisplay.value}
						</div>
						{totalDisplay.suffix ? (
							<div className="pb-1 text-xl font-bold text-slate-400">{totalDisplay.suffix}</div>
						) : null}
					</div>
				</div>

				<div className="mb-5 flex rounded-xl border border-slate-100/80 bg-slate-50">
					<div className="flex-1 p-3">
						<div className="mb-1 flex items-center gap-1 text-slate-400">
							<ArrowDownToLine className="size-[13px]" />
							<span className="text-xs font-medium">输入</span>
						</div>
						<div className="flex items-end gap-0.5 text-slate-700">
							<div className="text-lg font-semibold">{inputDisplay.value}</div>
							{inputDisplay.suffix ? (
								<div className="pb-0.5 text-xs font-semibold text-slate-400">
									{inputDisplay.suffix}
								</div>
							) : null}
						</div>
					</div>

					<div className="my-3 w-px bg-slate-200" />

					<div className="flex-1 p-3 pl-4">
						<div className="mb-1 flex items-center gap-1 text-slate-400">
							<ArrowUpFromLine className="size-[13px]" />
							<span className="text-xs font-medium">输出</span>
						</div>
						<div className="flex items-end gap-0.5 text-slate-700">
							<div className="text-lg font-semibold">{outputDisplay.value}</div>
							{outputDisplay.suffix ? (
								<div className="pb-0.5 text-xs font-semibold text-slate-400">
									{outputDisplay.suffix}
								</div>
							) : null}
						</div>
					</div>
				</div>

				<div className="flex items-center gap-1.5 border-t border-slate-100 pt-1 text-xs text-slate-400">
					<CheckCircle2 className="size-[13px] text-emerald-500/80" />
					<span>已统计 {messageCount} 条 AI 回复</span>
				</div>
			</div>
		</div>
	);
}

function splitTokenMetric(
	count: number,
	options?: { compact?: boolean },
): { value: string; suffix: string } {
	// 右侧卡片的输入/输出需要统一展示单位，所以这里允许把小于 1000 的值也压成 K 记法。
	const formatted =
		options?.compact && count > 0 && count < 1000
			? `${(count / 1000).toFixed(1)}K`
			: formatTokenCount(count);
	const match = formatted.match(/^([\d.]+)([A-Z]+)?$/);
	if (!match) return { value: formatted, suffix: "" };
	return {
		value: match[1] ?? formatted,
		suffix: match[2] ?? "",
	};
}

function TaskChatEmptyState({
	layout,
}: {
	layout: ReturnType<typeof getProjectChatLayoutClasses>;
}) {
	return (
		<div className={cn("flex h-full", layout.shell)}>
			<div className={cn(layout.inner, "flex h-full items-center justify-center")}>
				<div className="flex max-w-[320px] flex-col items-center text-center">
					<div className="flex size-12 items-center justify-center rounded-full bg-[var(--leros-primary-softer)] text-[var(--leros-primary)]">
						<Bot className="size-6" />
					</div>
					<h2 className="mt-5 text-lg font-semibold text-[var(--leros-text-strong)]">任务会话</h2>
					<p className="mt-2 text-sm leading-6 text-[var(--leros-text-muted)]">
						在此与 AI 协作完成任务讨论，发送消息即可开始对话。
					</p>
				</div>
			</div>
		</div>
	);
}

function TaskFileList({
	files,
	onPreview,
}: {
	files: ProjectFileNode[];
	onPreview: (file: ProjectFileNode) => void;
}) {
	if (files.length === 0) {
		return (
			<div className="rounded-lg border border-dashed border-[var(--leros-control-border)] px-4 py-8 text-center text-xs text-[var(--leros-text-muted)]">
				暂无文件
			</div>
		);
	}

	return (
		<div className={SIDEBAR_COMPACT_LIST_CLASS}>
			{files.map((file) => (
				<button
					type="button"
					key={file.path}
					onClick={() => onPreview(file)}
					className="group relative flex w-full cursor-pointer items-center gap-3 overflow-hidden rounded-lg border border-[var(--leros-control-border)] bg-[var(--leros-surface)] px-3.5 py-3 text-left shadow-sm transition-colors hover:border-[var(--leros-primary-soft)] hover:bg-[var(--leros-primary-softer)]/35"
					title="预览文件"
				>
					<div className="pointer-events-none absolute inset-0 flex items-center justify-center bg-[rgba(15,23,42,0.16)] opacity-0 transition-opacity duration-200 group-hover:opacity-100">
						<span className="rounded-full bg-[rgba(15,23,42,0.72)] px-3 py-1 text-xs font-medium tracking-[0.02em] text-white shadow-sm">
							点击预览
						</span>
					</div>
					<div className="flex size-10 shrink-0 items-center justify-center rounded-lg bg-[var(--leros-primary-softer)]">
						<ProjectFileTypeIcon fileName={file.name} />
					</div>
					<div className="min-w-0">
						<div className="truncate text-sm font-semibold leading-5 text-[var(--leros-text-strong)]">
							{file.name}
						</div>
						<div className="mt-1 truncate text-xs leading-4 text-[var(--leros-text-muted)]">
							{file.size > 0 ? formatBytes(file.size) : ""}
						</div>
					</div>
				</button>
			))}
		</div>
	);
}

function formatBytes(size: number): string {
	if (!size) return "";
	if (size < 1024) return `${size} B`;
	if (size < 1024 * 1024) return `${(size / 1024).toFixed(1)} KB`;
	return `${(size / (1024 * 1024)).toFixed(1)} MB`;
}
