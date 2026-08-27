"use client";

import type { BackendProjectFileVersion, DigitalAssistantItem, ProjectTask } from "@leros/store";
import {
	Action,
	formatArtifactTime,
	formatTokenCount,
	projectFileApi,
	sessionApi,
	useAppStore,
	useChatStore,
	useDAStore,
	useLayoutStore,
	useTaskCapabilities,
} from "@leros/store";
import { taskApi } from "@leros/store/api/taskApi";
import { Button } from "@leros/ui/components/ui/button";
import {
	Dialog,
	DialogContent,
	DialogDescription,
	DialogFooter,
	DialogHeader,
	DialogTitle,
} from "@leros/ui/components/ui/dialog";
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
	LoaderCircle,
	Pencil,
	Zap,
} from "lucide-react";
import { useCallback, useEffect, useMemo, useRef, useState } from "react";
import { SHOW_TASK_TOKEN_USAGE_CARD } from "../../constants/temporaryUiFlags";
import { MessageTimeline } from "../chat/MessageTimeline";
import { buildPromptSuggestions } from "../digitalAssistant/promptSuggestions";
import { ChatInput } from "../input/ChatInput";
import { CanGate } from "../permission/CanGate";
import { openProjectFilePreview } from "./file-preview-store";
import { PROJECT_FILE_VERSION_CHANGED_EVENT } from "./file-preview-utils";
import type { AppNavigation } from "./LeftRail";
import { getProjectChatLayoutClasses, type ProjectChatLayoutMode } from "./project-chat-layout";
import { ProjectFileTypeIcon } from "./project-file-type-icon";
import {
	buildProjectFileVersionEntries,
	getCurrentProjectFileVersionEntry,
} from "./project-file-version-sync";
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
// 中文注释：任务详情页文件列表填充右侧栏剩余空间，文件较多时只在该区域内滚动。
const TASK_DETAIL_FILE_LIST_CLASS =
	"min-h-0 flex flex-1 flex-col space-y-3 overflow-y-auto [scrollbar-width:none] [-ms-overflow-style:none] [&::-webkit-scrollbar]:hidden";

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
	/** 路径强制携带的任务会话 ID；缺失时由路由层拦截，不在此页兜底。 */
	sessionId: string;
	navigation?: AppNavigation;
}) {
	const {
		activeTaskDetailProjectId,
		activeTaskDetailTaskId,
		projects,
		fetchProjects,
		fetchProjectDetail,
		setTaskDetailRoute,
		setProjectRoute,
		switchProject,
		updateTask,
	} = useLayoutStore((s) => s);

	const {
		isGenerating,
		messageIds,
		messagesMap,
		pendingBootstrapSessionId,
		streamingMessageId,
		setActiveSession,
		clearLocalMessages,
		closeSseConnection,
		clearPendingBootstrapSession,
		hasSessionMessages,
		allMessagesBelongToSession,
		loadConversationMessages,
		sendTaskRoomMessage,
	} = useChatStore((s) => s);
	const { assistantsLoaded, fetchAssistants } = useDAStore((s) => s);

	const [task, setTask] = useState<ProjectTask | null>(null);
	const [teammate, setTeammate] = useState<DigitalAssistantItem | null>(null);
	const [renameDialogOpen, setRenameDialogOpen] = useState(false);
	const [renameValue, setRenameValue] = useState("");
	const [taskFiles, setTaskFiles] = useState<ProjectFileNode[]>([]);
	const [rightSidebarWidth, setRightSidebarWidth] = useState(
		TASK_DETAIL_RIGHT_SIDEBAR_DEFAULT_WIDTH,
	);
	const [rightSidebarCollapsed, setRightSidebarCollapsed] = useState(false);
	const hasLoadedRightSidebarPreferenceRef = useRef(false);

	const resolvedProjectId = projectId ?? activeTaskDetailProjectId;
	const resolvedTaskId = taskId ?? activeTaskDetailTaskId;
	const resolvedSessionId = sessionId;
	useTaskCapabilities(resolvedTaskId);
	const project = projects.find((p) => p.id === resolvedProjectId);
	const storeTask = useMemo(
		() => project?.tasks.find((item) => item.id === resolvedTaskId) ?? null,
		[project, resolvedTaskId],
	);
	// 面包屑只做展示截断，完整名称通过 title 保留，避免超长文本撑开头部布局。
	const breadcrumbProjectName = truncateBreadcrumbText(project?.name);
	const breadcrumbTaskTitle = truncateBreadcrumbText(task?.title ?? "任务");

	const latestTodos = useMemo(
		() => getLatestAssistantTodos(messagesMap, messageIds, resolvedSessionId, streamingMessageId),
		[messagesMap, messageIds, resolvedSessionId, streamingMessageId],
	);
	const isTaskRunActive = useMemo(() => {
		if (!isGenerating || !resolvedSessionId) return false;
		if (!streamingMessageId) return true;
		return messagesMap[streamingMessageId]?.conversationId === resolvedSessionId;
	}, [isGenerating, messagesMap, resolvedSessionId, streamingMessageId]);

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
		if (!resolvedProjectId) return;
		try {
			// 中文注释：任务文件列表统一走项目文件接口，不再额外调用 ListTaskArtifacts。
			const res = await projectFileApi.list({
				projectId: resolvedProjectId,
				resourceType: "artifact",
				taskId: resolvedTaskId ?? undefined,
			});
			setTaskFiles(normalizeProjectFileTree(res.data.data));
		} catch (err) {
			console.error("TaskDetailPage fetch task files error:", err);
			setTaskFiles([]);
		}
	}, [resolvedProjectId, resolvedTaskId]);

	useEffect(() => {
		if (!resolvedProjectId || !resolvedTaskId) return;
		const handleRestored = (event: Event) => {
			const detail = (event as CustomEvent<{ projectId?: string; taskId?: string }>).detail;
			if (detail?.projectId && detail.projectId !== resolvedProjectId) return;
			if (detail?.taskId && detail.taskId !== resolvedTaskId) return;
			void fetchTaskFiles();
		};
		window.addEventListener(PROJECT_FILE_VERSION_CHANGED_EVENT, handleRestored);
		return () => window.removeEventListener(PROJECT_FILE_VERSION_CHANGED_EVENT, handleRestored);
	}, [resolvedProjectId, resolvedTaskId, fetchTaskFiles]);

	useEffect(() => {
		fetchProjects();
	}, [fetchProjects]);

	useEffect(() => {
		// 中文注释：群聊展示真人队友头像依赖详情接口返回的 members.avatar_url，列表接口不含成员。
		if (!resolvedProjectId) return;
		void fetchProjectDetail(resolvedProjectId);
	}, [resolvedProjectId, fetchProjectDetail]);

	useEffect(() => {
		if (!projectId || !taskId || !sessionId) return;
		setTaskDetailRoute(projectId, taskId, sessionId);
	}, [projectId, taskId, sessionId, setTaskDetailRoute]);

	useEffect(() => {
		if (!resolvedSessionId) return;

		setActiveSession(resolvedSessionId);
		const bootstrapPending = pendingBootstrapSessionId === resolvedSessionId;
		const sessionHasMessages = hasSessionMessages(resolvedSessionId);
		// 中文注释：新建跳转同一次 bootstrap、本地已有乐观等待态时，交给 GlobalEvents，不抢先拉历史。
		if (bootstrapPending && sessionHasMessages) return;
		// 中文注释：发送中（含第二轮 AddMessage 后等待 GlobalEvents）禁止再 load，避免冲掉乐观 waiting / 误开 resume。
		// 离开后再进由 closeSseConnection 清掉 isGenerating，不会误伤场景 2 hydration。
		if (isGenerating && sessionHasMessages && allMessagesBelongToSession(resolvedSessionId)) return;
		// 中文注释：bootstrap 标记存在但消息被误清时，等待 GlobalEvents 回填，避免 loadConversationMessages 与 SSE resume 重复开流。
		if (bootstrapPending && !sessionHasMessages) return;
		if (!sessionHasMessages) {
			clearLocalMessages();
		}
		// 中文注释：本地已有该会话消息时仍后台刷新/按需 resume，但首屏可直接复用，避免每次冷进空等网络。
		loadConversationMessages(resolvedSessionId, {
			resumeStream: !(bootstrapPending && sessionHasMessages),
		});
		// 中文注释：这里不要依赖 messageIds.length，否则群聊中新消息回推会重复触发恢复流，与 GlobalEvents 的 assistant 流式回复并行显示。
	}, [
		resolvedSessionId,
		pendingBootstrapSessionId,
		isGenerating,
		setActiveSession,
		hasSessionMessages,
		allMessagesBelongToSession,
		clearLocalMessages,
		loadConversationMessages,
	]);

	// 真正离开任务详情时关掉 SSE、清 bootstrap，但保留本地消息作同会话再进的首屏缓存。
	// 同 session remount（Strict Mode / 路由重挂）时不动：否则会冲掉场景 1 的 waiting，
	// 被误判成冷进页并对 responding 直接 resume 开 SessionEvents（问答路径应等 GlobalEvents assistant）。
	useEffect(() => {
		const sessionIdOnEffect = resolvedSessionId;
		return () => {
			queueMicrotask(() => {
				const layout = useAppStore.getState();
				if (
					layout.currentView === "taskDetail" &&
					sessionIdOnEffect &&
					layout.activeTaskDetailSessionId === sessionIdOnEffect
				) {
					return;
				}
				closeSseConnection();
				clearPendingBootstrapSession();
			});
		};
	}, [resolvedSessionId, closeSseConnection, clearPendingBootstrapSession]);

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
						sessionId: bt.session?.session_id,
						taskType: bt.task_type,
						deadline: bt.deadline,
						description: bt.description,
						assistantId: bt.session?.assistant_id,
					});
				}
			})
			.catch((err) => {
				console.error("TaskDetailPage fetch task error:", err);
			});

		fetchTaskFiles();
	}, [resolvedTaskId, fetchTaskFiles]);

	useEffect(() => {
		if (!storeTask) return;
		setTask((current) => {
			if (!current || current.id !== storeTask.id) return storeTask;
			if (
				current.title === storeTask.title &&
				current.meta === storeTask.meta &&
				current.status === storeTask.status &&
				current.updatedAt === storeTask.updatedAt &&
				current.sessionId === storeTask.sessionId &&
				current.taskType === storeTask.taskType &&
				current.deadline === storeTask.deadline &&
				current.description === storeTask.description
			) {
				return current;
			}
			return { ...current, ...storeTask };
		});
	}, [storeTask]);

	// 解析当前任务会话绑定的队友：优先用 storeTask.assistantId，缺失时回退到 sessionApi.get。
	// 用于空对话时在 TaskChatEmptyState 中展示「试试这样问我」快捷提示。
	useEffect(() => {
		if (!resolvedSessionId) {
			setTeammate(null);
			return;
		}
		let cancelled = false;
		const resolveTeammate = async () => {
			try {
				if (!assistantsLoaded) {
					await fetchAssistants();
				}
				if (cancelled) return;
				const latest = useAppStore.getState().assistants;

				// 中文注释：store 任务和 session 接口均使用 publicId 字符串匹配。
				if (storeTask?.assistantId) {
					const fromStore =
						latest.find((assistant) => assistant.publicId === storeTask.assistantId) ?? null;
					setTeammate(fromStore);
					return;
				}

				const res = await sessionApi.get({ session_id: resolvedSessionId });
				const assistantId = res.data.data?.assistant_id;
				if (cancelled || !assistantId) {
					setTeammate(null);
					return;
				}
				const found = latest.find((assistant) => assistant.publicId === assistantId) ?? null;
				setTeammate(found);
			} catch (err) {
				console.error("TaskDetailPage resolve teammate error:", err);
				if (!cancelled) setTeammate(null);
			}
		};
		resolveTeammate();
		return () => {
			cancelled = true;
		};
	}, [resolvedSessionId, storeTask?.assistantId, assistantsLoaded, fetchAssistants]);

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

	const handleOpenRenameDialog = () => {
		if (!task?.title) return;
		setRenameValue(task.title);
		setRenameDialogOpen(true);
	};

	const handleConfirmRename = async () => {
		const title = renameValue.trim();
		if (!task || !title) return;

		const updatedTask = await updateTask({ public_id: task.id, title });
		if (!updatedTask) return;

		setTask(updatedTask);
		setRenameDialogOpen(false);
	};

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
			<header className="flex h-12 shrink-0 items-center justify-between border-b border-[var(--leros-control-border)] bg-[var(--leros-surface-soft)] px-5">
				<div className="flex min-w-0 items-center gap-3 text-[var(--leros-text-muted)]">
					{/* 中文注释：任务详情页面包屑与项目页保持一致，三级结构为 项目 > 项目名 > 任务名。 */}
					<button
						type="button"
						onClick={() => navigation?.goToRoute("projectsHub")}
						className="text-sm font-normal text-[var(--leros-text-muted)] transition-colors hover:text-[var(--leros-primary)]"
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
								className="max-w-[360px] truncate text-sm font-normal text-[var(--leros-text-muted)] transition-colors hover:text-[var(--leros-primary)]"
								title={project.name}
							>
								{breadcrumbProjectName}
							</button>
							<ChevronRight className="size-4 shrink-0 text-[var(--leros-text-subtle)]" />
						</>
					)}
					<h1
						className="max-w-[360px] truncate text-sm font-semibold text-[var(--leros-text-strong)]"
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
								navigation.goToProjectTasks(resolvedProjectId);
								return;
							}
							if (resolvedProjectId) {
								switchProject(resolvedProjectId);
								setProjectRoute(resolvedProjectId, "tasks");
							}
						}}
						className="flex items-center gap-1.5 rounded-full px-3 py-1.5 text-sm text-[var(--leros-text-muted)] transition-colors hover:bg-[var(--leros-primary-softer)] hover:text-[var(--leros-primary)]"
					>
						<ArrowLeft className="size-3.5" />
						任务列表
					</button>
				</div>
			</header>

			<div className="min-h-0 min-w-0 flex flex-1">
				<main className="min-w-0 flex min-h-0 flex-1 flex-col">
					{/* 中文注释：任务详情页作为壳层里的 flex item 以及中间主列本身都必须允许收缩，避免小窗口下被聊天内容宽度和右侧栏共同撑出可视区域。 */}
					<MessageTimeline
						emptyState={
							<TaskChatEmptyState
								layout={taskChatLayout}
								teammate={teammate}
								onPickPrompt={(prompt) => {
									if (!resolvedProjectId || !resolvedTaskId || !resolvedSessionId) {
										return;
									}
									void sendTaskRoomMessage(prompt, {
										projectId: resolvedProjectId,
										taskId: resolvedTaskId,
										sessionId: resolvedSessionId,
									});
								}}
							/>
						}
						contentShellClassName={taskChatLayout.shell}
						contentClassName={taskChatLayout.timelineInner}
						projectId={resolvedProjectId}
					/>
					<ChatInput
						variant="project"
						projectLayoutMode={taskChatLayoutMode}
						navigation={navigation}
					/>
				</main>

				<div className="flex w-14 shrink-0 items-start justify-center pt-6">
					<button
						type="button"
						className="inline-flex size-10 items-center justify-center rounded-full border border-[var(--leros-control-border)] bg-[var(--leros-surface)] text-[var(--leros-text-muted)] shadow-sm transition-colors hover:bg-[var(--leros-primary-softer)] hover:text-[var(--leros-primary)]"
						aria-label={rightSidebarCollapsed ? "展开右侧栏" : "收起右侧栏"}
						aria-expanded={!rightSidebarCollapsed}
						title={rightSidebarCollapsed ? "展开右侧栏" : "收起右侧栏"}
						onClick={() => setRightSidebarCollapsed((collapsed) => !collapsed)}
					>
						{rightSidebarCollapsed ? (
							<ChevronsLeft className="size-4" />
						) : (
							<ChevronsRight className="size-4" />
						)}
					</button>
				</div>

				{!rightSidebarCollapsed && (
					<aside
						className="relative flex shrink-0 flex-col border-l border-[var(--leros-control-border)] bg-[var(--leros-surface-soft)] px-5 pt-6 pb-[6px] transition-[width] duration-200 ease-out"
						style={rightSidebarWidthStyle}
					>
						<div className="no-scrollbar min-h-0 flex flex-1 flex-col space-y-8 overflow-y-auto pr-1">
							<div>
								<div>
									<p className="text-sm font-semibold text-[var(--leros-text-strong)]">任务侧栏</p>
									<p className="mt-1 text-xs text-[var(--leros-text-muted)]">
										查看任务说明、进度和文件概览
									</p>
								</div>
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
							{task?.title && (
								<section>
									<div className="mb-3 flex items-center justify-between gap-3">
										<h3 className="text-xs font-semibold text-[var(--leros-text-muted)]">
											任务名称
										</h3>
										<CanGate
											action={Action.TaskUpdate}
											resource={resolvedTaskId ? { type: "task", publicId: resolvedTaskId } : null}
										>
											<button
												type="button"
												onClick={handleOpenRenameDialog}
												className="inline-flex items-center gap-1 rounded-md px-2 py-1 text-xs font-medium text-[var(--leros-text-muted)] transition-colors hover:bg-[var(--leros-primary-softer)] hover:text-[var(--leros-text-strong)]"
												title="重命名任务"
											>
												<Pencil className="size-3.5" />
											</button>
										</CanGate>
									</div>
									<p className="text-sm leading-relaxed text-[var(--leros-text)]">{task.title}</p>
								</section>
							)}
							{latestTodos && latestTodos.length > 0 && (
								<section>
									<h3 className="mb-3 text-xs font-semibold text-[var(--leros-text-muted)]">
										任务进度
									</h3>
									<TaskTodoProgressPanel todos={latestTodos} isRunActive={isTaskRunActive} />
								</section>
							)}
							<section className="flex min-h-0 flex-1 flex-col">
								<div className="mb-3 flex items-center justify-between">
									<h3 className="text-xs font-semibold text-[var(--leros-text-muted)]">任务文件</h3>
								</div>
								<TaskFileList
									files={flatTaskFiles}
									projectId={resolvedProjectId}
									onPreview={(file, version) =>
										openProjectFilePreview(resolvedProjectId, file, {
											...(resolvedTaskId ? { taskId: resolvedTaskId } : {}),
											...(version ? { version } : {}),
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
			<Dialog open={renameDialogOpen} onOpenChange={setRenameDialogOpen}>
				<DialogContent className="sm:max-w-md" showCloseButton={false}>
					<DialogHeader>
						<DialogTitle>重命名任务</DialogTitle>
						<DialogDescription>请输入新的任务名称</DialogDescription>
					</DialogHeader>
					<div className="mt-4 relative">
						<input
							type="text"
							value={renameValue}
							onChange={(event) => setRenameValue(event.target.value)}
							onKeyDown={(event) => {
								if (event.key === "Enter") {
									void handleConfirmRename();
								}
							}}
							placeholder="任务名称"
							maxLength={30}
							autoFocus
							className="w-full rounded-md border border-slate-200 bg-white px-3 py-2 pr-14 text-sm text-slate-800 placeholder:text-slate-400 transition-colors focus:border-blue-300 focus:outline-none"
						/>
						<span className="pointer-events-none absolute right-3 top-1/2 -translate-y-1/2 text-xs text-slate-400">
							{renameValue.length}/30
						</span>
					</div>
					<DialogFooter className="mt-4">
						<Button variant="outline" onClick={() => setRenameDialogOpen(false)}>
							取消
						</Button>
						<Button onClick={() => void handleConfirmRename()} disabled={!renameValue.trim()}>
							确认
						</Button>
					</DialogFooter>
				</DialogContent>
			</Dialog>
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
						<div className="text-4xl font-semibold tracking-tight text-slate-900">
							{totalDisplay.value}
						</div>
						{totalDisplay.suffix ? (
							<div className="pb-1 text-xl font-semibold text-slate-400">{totalDisplay.suffix}</div>
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
	teammate,
	onPickPrompt,
}: {
	layout: ReturnType<typeof getProjectChatLayoutClasses>;
	teammate?: DigitalAssistantItem | null;
	onPickPrompt?: (prompt: string) => void;
}) {
	const promptSuggestions = teammate ? buildPromptSuggestions(teammate) : [];
	return (
		<div className={cn("flex h-full", layout.shell)}>
			<div className={cn(layout.inner, "flex h-full items-center justify-center")}>
				<div className="flex max-w-[420px] flex-col items-center text-center">
					<div className="flex size-12 items-center justify-center rounded-full bg-[var(--leros-primary-softer)] text-[var(--leros-primary)]">
						<Bot className="size-6" />
					</div>
					<h2 className="mt-5 text-lg font-semibold text-[var(--leros-text-strong)]">
						{teammate ? teammate.name : "任务会话"}
					</h2>
					<p className="mt-2 text-sm leading-6 text-[var(--leros-text-muted)]">
						{teammate?.description || "在此与 AI 协作完成任务讨论，发送消息即可开始对话。"}
					</p>
					{promptSuggestions.length > 0 && (
						<div className="mt-6 w-full space-y-2">
							<p className="text-xs font-medium text-[var(--leros-text-subtle)]">试试这样问我</p>
							{promptSuggestions.map((prompt) => (
								<button
									key={prompt}
									type="button"
									className="flex w-full items-center justify-between gap-3 rounded-lg border border-[var(--leros-control-border)] bg-white px-4 py-2.5 text-left text-sm leading-6 text-[var(--leros-text-muted)] transition-colors hover:border-[var(--leros-primary)] hover:text-[var(--leros-text-strong)]"
									onClick={() => onPickPrompt?.(prompt)}
								>
									<span className="min-w-0 flex-1">“{prompt}”</span>
									<ChevronRight
										className="size-4 shrink-0 text-[var(--leros-text-subtle)]"
										aria-hidden="true"
									/>
								</button>
							))}
						</div>
					)}
				</div>
			</div>
		</div>
	);
}

function TaskFileList({
	files,
	projectId,
	onPreview,
}: {
	files: ProjectFileNode[];
	projectId: string;
	onPreview: (file: ProjectFileNode, version?: BackendProjectFileVersion) => void;
}) {
	type VersionLoadState = {
		items: BackendProjectFileVersion[];
		currentPublicId: string;
		loading: boolean;
		error: string | null;
	};

	const [versionStates, setVersionStates] = useState<Record<string, VersionLoadState>>({});

	const loadVersions = useCallback(
		async (file: ProjectFileNode) => {
			if (!projectId || !file.publicId) return;
			const key = file.publicId;
			setVersionStates((current) => ({
				...current,
				[key]: {
					items: current[key]?.items ?? [],
					currentPublicId: current[key]?.currentPublicId ?? file.publicId,
					loading: true,
					error: null,
				},
			}));

			try {
				const response = await projectFileApi.versions(projectId, file.publicId);
				if (response.data.code !== 0) {
					throw new Error(response.data.message || "版本历史加载失败");
				}
				setVersionStates((current) => ({
					...current,
					[key]: {
						items: response.data.data?.items ?? [],
						currentPublicId: response.data.data?.current_file_public_id || file.publicId,
						loading: false,
						error: null,
					},
				}));
			} catch (error) {
				setVersionStates((current) => ({
					...current,
					[key]: {
						items: current[key]?.items ?? [],
						currentPublicId: current[key]?.currentPublicId ?? file.publicId,
						loading: false,
						error: error instanceof Error ? error.message : "版本历史加载失败",
					},
				}));
			}
		},
		[projectId],
	);

	useEffect(() => {
		for (const file of files) {
			const hasHistory = Boolean(file.publicId) && Math.max(file.versionCount, file.versionNo) > 1;
			if (hasHistory && !versionStates[file.publicId]) {
				void loadVersions(file);
			}
		}
	}, [files, loadVersions, versionStates]);

	if (files.length === 0) {
		return (
			<div
				className={cn(
					TASK_DETAIL_FILE_LIST_CLASS,
					"items-center justify-center rounded-lg border border-dashed border-[var(--leros-control-border)] px-4 py-8 text-center text-xs text-[var(--leros-text-muted)]",
				)}
			>
				暂无文件
			</div>
		);
	}

	return (
		<div className={TASK_DETAIL_FILE_LIST_CLASS}>
			{files.map((file) => {
				const availableVersionCount = Math.max(file.versionCount, file.versionNo);
				const hasHistory = Boolean(file.publicId) && availableVersionCount > 1;
				const versionState = versionStates[file.publicId];
				const versions = versionState?.items ?? [];
				const versionEntries = buildProjectFileVersionEntries(versions);
				const currentVersionEntry = getCurrentProjectFileVersionEntry(
					versionEntries,
					versionState?.currentPublicId ?? file.publicId,
				);

				return (
					<div key={file.path} className="space-y-2">
						{versionEntries.length > 0 ? (
							versionEntries.map((entry) => (
								<TaskFileCard
									key={entry.key}
									file={file}
									version={entry.version}
									isCurrent={entry.key === currentVersionEntry?.key}
									onPreview={onPreview}
								/>
							))
						) : (
							<TaskFileCard file={file} isCurrent={!hasHistory} onPreview={onPreview} />
						)}
						{versionState?.loading ? (
							<div className="flex items-center justify-center gap-2 py-1 text-[11px] text-[var(--leros-text-muted)]">
								<LoaderCircle className="size-3 animate-spin" />
								正在加载历史版本
							</div>
						) : versionState?.error ? (
							<button
								type="button"
								onClick={() => void loadVersions(file)}
								className="w-full rounded-md px-2 py-1.5 text-[11px] text-[var(--leros-danger)] hover:bg-[var(--leros-danger)]/5"
							>
								历史版本加载失败，点击重试
							</button>
						) : null}
					</div>
				);
			})}
		</div>
	);
}

function TaskFileCard({
	file,
	version,
	isCurrent,
	onPreview,
}: {
	file: ProjectFileNode;
	version?: BackendProjectFileVersion;
	isCurrent: boolean;
	onPreview: (file: ProjectFileNode, version?: BackendProjectFileVersion) => void;
}) {
	const name = version?.name || file.name;
	const size = version?.size ?? file.size;
	const createdAt = version?.created_at ? version.created_at * 1000 : file.createdAt;
	const versionNo = version?.version_no ?? file.versionNo;

	return (
		<button
			type="button"
			data-file-preview-trigger
			onClick={() => onPreview(file, version)}
			className="group flex w-full cursor-pointer items-center gap-3 rounded-lg border border-[var(--leros-control-border)] bg-[var(--leros-surface)] px-3.5 py-3 text-left shadow-sm transition-colors hover:border-[var(--leros-primary-soft)] hover:bg-[var(--leros-primary-softer)]/35"
			title={versionNo > 0 ? `预览 V${versionNo}` : "预览文件"}
		>
			<div className="flex size-10 shrink-0 items-center justify-center rounded-lg bg-[var(--leros-primary-softer)]">
				<ProjectFileTypeIcon fileName={name} />
			</div>
			<div className="min-w-0 flex-1">
				<div className="min-w-0 truncate text-sm font-semibold leading-5 text-[var(--leros-text-strong)]">
					<span className="truncate">{name}</span>
				</div>
				<div className="mt-1 flex min-w-0 items-center gap-1.5 text-xs leading-4 text-[var(--leros-text-muted)]">
					{versionNo > 0 ? (
						<span className="shrink-0 rounded bg-[var(--leros-primary-softer)] px-1.5 py-0.5 text-[10px] font-semibold leading-none text-[var(--leros-primary)]">
							V{versionNo}
						</span>
					) : null}
					<span className="min-w-0 truncate">
						{[
							size > 0 ? formatBytes(size) : "",
							createdAt ? formatArtifactTime(createdAt) : "",
							version ? (isCurrent ? "最新" : "历史版本") : "",
						]
							.filter(Boolean)
							.join(" · ")}
					</span>
				</div>
			</div>
		</button>
	);
}

function formatBytes(size: number): string {
	if (!size) return "";
	if (size < 1024) return `${size} B`;
	if (size < 1024 * 1024) return `${(size / 1024).toFixed(1)} KB`;
	return `${(size / (1024 * 1024)).toFixed(1)} MB`;
}
